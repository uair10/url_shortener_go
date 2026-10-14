// Package requestid хранит ID запроса в контексте и проверяет ID от клиента.
package requestid

import "context"

type ctxKey struct{}

const (
	// Header - заголовок, в котором ID приходит от клиента и возвращается в ответе.
	Header = "X-Request-ID"

	maxLen = 64
)

// With кладёт id в контекст.
func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// From достаёт id из контекста. Возвращает "" если нет.
func From(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok {
		return v
	}
	return ""
}

// Valid сообщает, можно ли принять id от клиента: от 1 до 64 символов [A-Za-z0-9-].
// Значение уходит в логи и в ответ, поэтому произвольный ввод не пропускаем.
func Valid(id string) bool {
	if id == "" || len(id) > maxLen {
		return false
	}
	for i := range len(id) {
		c := id[i]
		if ('a' > c || c > 'z') && ('A' > c || c > 'Z') && ('0' > c || c > '9') && c != '-' {
			return false
		}
	}
	return true
}
