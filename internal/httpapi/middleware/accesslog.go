package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/uair10/url_shortener_go/internal/infrastructure/logging"
)

// AccessLog пишет access log: метод, путь (без query), статус, длительность.
// request_id добавляет логгер из контекста.
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
		slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
	}

	// Ответ оборван паникой (обычно http.ErrAbortHandler): отмечаем и пропускаем панику дальше.
	if v := recover(); v != nil {
		logging.FromCtx(r.Context()).InfoContext(r.Context(), "request", append(attrs, slog.Bool("aborted", true))...)
		panic(v)
	}
	logging.FromCtx(r.Context()).InfoContext(r.Context(), "request", attrs...)
}
