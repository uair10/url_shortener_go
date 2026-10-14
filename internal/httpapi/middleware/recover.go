package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/uair10/url_shortener_go/internal/httpapi/problem"
	"github.com/uair10/url_shortener_go/internal/infrastructure/logging"
)

// Recover ловит панику в хендлере: пишет стек в лог и отвечает 500 internal.
// Если ответ уже начат, 500 не отправить: соединение обрывается через http.ErrAbortHandler,
// чтобы клиент не принял обрезанный ответ за успешный.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := newResponseRecorder(w)
		defer recoverPanic(rec, r)
		next.ServeHTTP(rec, r)
	})
}

// recoverPanic вызывается через defer: recover() работает только в функции, которую defer вызвал напрямую.
func recoverPanic(w *responseRecorder, r *http.Request) {
	v := recover()
	if v == nil {
		return
	}
	// ErrAbortHandler - штатный способ оборвать ответ, http.Server разберётся с ним сам.
	if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
		panic(v)
	}

	logging.FromCtx(r.Context()).ErrorContext(r.Context(), "panic",
		slog.Any("panic", v),
		slog.String("stack", string(debug.Stack())),
	)

	if w.wroteHeader {
		panic(http.ErrAbortHandler)
	}
	problem.Internal().Write(w, r)
}
