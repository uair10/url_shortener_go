package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/uair10/url_shortener_go/internal/httpapi/problem"
	"github.com/uair10/url_shortener_go/internal/infrastructure/logging"
)

// statusClientClosedRequest - нестандартный статус nginx: клиент закрыл соединение до ответа.
// Клиент его не увидит, он нужен access log, чтобы там не было неявного 200.
const statusClientClosedRequest = 499

// Handler - хендлер, который возвращает ошибку вместо того, чтобы писать её сам.
// Ошибку ServeHTTP превращает в problem+json.
// Если хендлер возвращает ошибку, он не должен ничего писать в w: иначе problem+json
// допишется в тело уже начатого ответа.
type Handler func(w http.ResponseWriter, r *http.Request) error

// ServeHTTP реализует http.Handler.
func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	err := h(w, r)
	if err == nil {
		return
	}

	ctx := r.Context()
	logger := logging.FromCtx(ctx)

	// Клиент ушёл - ответ никто не прочитает, только логируем.
	// Смотрим на контекст запроса, а не на тип err: context.DeadlineExceeded может прийти
	// от собственного таймаута хендлера (например, к БД), пока клиент ещё ждёт.
	// Такая ошибка ниже станет 500.
	if ctx.Err() != nil {
		logger.WarnContext(ctx, "client gone",
			slog.Any("err", err),
			slog.String("path", r.URL.Path),
		)
		w.WriteHeader(statusClientClosedRequest)
		return
	}

	if p, ok := errors.AsType[*problem.Problem](err); ok {
		// Уровень лога - по статусу.
		switch {
		case p.Status >= 500:
			logger.ErrorContext(ctx, "request failed",
				slog.String("code", p.Code),
				slog.Int("status", p.Status),
				slog.String("detail", p.Detail),
				slog.Any("err", err),
			)
		case p.Status >= 400:
			logger.InfoContext(ctx, "request rejected",
				slog.String("code", p.Code),
				slog.Int("status", p.Status),
				slog.String("detail", p.Detail),
			)
		}
		p.Write(w, r)
		return
	}

	// Неизвестная ошибка -> 500, детали только в лог.
	logger.ErrorContext(ctx, "unhandled error",
		slog.Any("err", err),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	)
	problem.Internal().Write(w, r)
}
