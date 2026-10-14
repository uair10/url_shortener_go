package httpapi

import (
	"bytes"
	"maps"
	"net/http"

	"github.com/uair10/url_shortener_go/internal/httpapi/problem"
)

// notFoundAsProblem отдаёт 404/405 от самого http.ServeMux в формате problem+json.
//
// Перехватывается только ответ mux, когда маршрут не найден (pattern == "").
// Ответы зарегистрированных хендлеров, в том числе их собственные 404 и 413,
// идут клиенту как есть и без буферизации.
func notFoundAsProblem(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := mux.Handler(r); pattern != "" {
			// Через mux.ServeHTTP, а не найденный хендлер: mux заполняет r.Pattern и PathValue.
			mux.ServeHTTP(w, r)
			return
		}

		// Маршрута нет. Ответ mux маленький (http.Error или редирект), буферизуем его.
		rec := &bufferingRecorder{header: make(http.Header), status: http.StatusOK}
		mux.ServeHTTP(rec, r)

		var p *problem.Problem
		switch rec.status {
		case http.StatusNotFound:
			p = problem.NotFound("route not found: " + r.URL.Path)
		case http.StatusMethodNotAllowed:
			p = problem.MethodNotAllowed("method " + r.Method + " not allowed for " + r.URL.Path)
		default:
			// Например, редирект на очищенный путь (/a/../b -> /b).
			rec.flushTo(w)
			return
		}

		// Заголовки mux сохраняем: в них Allow для 405. Content-Type перезапишет Write.
		copyHeader(w.Header(), rec.header)
		p.Write(w, r)
	})
}

type bufferingRecorder struct {
	header      http.Header
	body        bytes.Buffer
	status      int
	wroteHeader bool
}

func (b *bufferingRecorder) Header() http.Header { return b.header }

func (b *bufferingRecorder) WriteHeader(code int) {
	if b.wroteHeader {
		return
	}
	b.status = code
	b.wroteHeader = true
}

func (b *bufferingRecorder) Write(p []byte) (int, error) {
	if !b.wroteHeader {
		b.WriteHeader(http.StatusOK)
	}
	// bytes.Buffer.Write всегда возвращает nil.
	b.body.Write(p)
	return len(p), nil
}

func (b *bufferingRecorder) flushTo(w http.ResponseWriter) {
	copyHeader(w.Header(), b.header)
	w.WriteHeader(b.status)
	_, _ = w.Write(b.body.Bytes())
}

func copyHeader(dst, src http.Header) {
	maps.Copy(dst, src)
}
