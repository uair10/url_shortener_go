package middleware

import (
	"log/slog"
	"net/http"
	"uuid"

	"github.com/uair10/url_shortener_go/internal/httpapi/requestid"
	"github.com/uair10/url_shortener_go/internal/infrastructure/logging"
)

// RequestID берёт X-Request-ID от клиента, если он валиден, иначе генерирует новый.
// Кладёт id в контекст, логгер с request_id - в контекст (logging.FromCtx), id - в заголовок ответа.
func RequestID(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(requestid.Header)
			if !requestid.Valid(id) {
				id = uuid.New().String()
			}

			ctx := requestid.With(r.Context(), id)
			ctx = logging.WithLogger(ctx, base.With(slog.String("request_id", id)))

			// Отдаём в заголовке ответа всегда, даже при ошибках.
			w.Header().Set(requestid.Header, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
