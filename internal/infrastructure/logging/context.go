package logging

import (
	"context"
	"log/slog"
)

type ctxKey struct{}

// WithLogger кладёт логгер в контекст.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromCtx достаёт логгер из контекста.
// Если его нет — возвращает slog.Default(), чтобы вызов не паниковал.
func FromCtx(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}
