package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/uair10/url_shortener_go/internal/infrastructure/logging"
)

// AccessLog пишет access log: метод, путь (без query), статус, длительность, размер ответа.
// request_id добавляет логгер из контекста. GET /healthz не логируется.
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := newResponseRecorder(w)
		// Через defer: строка пишется и когда сквозь middleware идёт http.ErrAbortHandler.
		defer logRequest(r, rec, time.Now())
		next.ServeHTTP(rec, r)
	})
}

// logRequest вызывается через defer: recover() работает только в функции, которую defer вызвал напрямую.
func logRequest(r *http.Request, rec *responseRecorder, start time.Time) {
	attrs := []any{
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Int("status", rec.status),
		slog.Float64("duration_ms", float64(time.Since(start))/float64(time.Millisecond)),
		slog.Int64("bytes", rec.bytes),
	}
	logger := logging.FromCtx(r.Context())
	// Ответ оборван паникой (обычно http.ErrAbortHandler): отмечаем и пропускаем панику дальше.
	if v := recover(); v != nil {
		logger.InfoContext(r.Context(), "request", append(attrs, slog.Bool("aborted", true))...)
		panic(v)
	}
	// Пробы Kubernetes дергают /healthz каждые несколько секунд - это шум.
	if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
		return
	}
	logger.InfoContext(r.Context(), "request", attrs...)
}
