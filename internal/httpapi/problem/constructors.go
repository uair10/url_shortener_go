package problem

import "strconv"

// New собирает Problem по коду. status и title берутся из таблицы,
// detail можно передать пустым - тогда в ответе его не будет.
// Незарегистрированный код - ошибка программиста, New паникует.
func New(code Code, detail string) *Problem {
	info, ok := codes[code]
	if !ok {
		panic("problem: unknown code " + strconv.Quote(string(code)))
	}
	return &Problem{
		Type:   code.TypeURI(),
		Title:  info.title,
		Status: info.status,
		Detail: detail,
		Code:   string(code),
	}
}

// WithInstance позволяет переопределить instance (по умолчанию - URL.Path).
func (p *Problem) WithInstance(instance string) *Problem {
	p.Instance = instance
	return p
}

// WithRetryAfter - только для rate_limited.
func (p *Problem) WithRetryAfter(seconds int) *Problem {
	p.RetryAfter = seconds
	return p
}

// Validation - 422 с полем errors.
func Validation(errs []FieldError) *Problem {
	p := New(CodeValidationFailed, "")
	p.Errors = errs
	return p
}

// MalformedJSON - 400: тело не парсится, лишние поля, неверный тип поля.
func MalformedJSON(detail string) *Problem { return New(CodeMalformedJSON, detail) }

// InvalidQuery - 400: неверные query-параметры.
func InvalidQuery(detail string) *Problem { return New(CodeInvalidQuery, detail) }

// Unauthorized - 401: нет ключа или ключ неверный.
func Unauthorized(detail string) *Problem { return New(CodeUnauthorized, detail) }

// Forbidden - 403: нет прав.
func Forbidden(detail string) *Problem { return New(CodeForbidden, detail) }

// QuotaExceeded - 403: превышена квота активных ссылок.
func QuotaExceeded(detail string) *Problem { return New(CodeQuotaExceeded, detail) }

// NotFound - 404: ресурса нет или он чужой.
func NotFound(detail string) *Problem { return New(CodeNotFound, detail) }

// MethodNotAllowed - 405: неверный метод.
func MethodNotAllowed(detail string) *Problem { return New(CodeMethodNotAllowed, detail) }

// AliasTaken - 409: код/alias занят.
func AliasTaken(detail string) *Problem { return New(CodeAliasTaken, detail) }

// EmailTaken - 409: email уже зарегистрирован.
func EmailTaken(detail string) *Problem { return New(CodeEmailTaken, detail) }

// LinkGone - 410: ссылка удалена, деактивирована, истекла или заблокирована.
func LinkGone(detail string) *Problem { return New(CodeLinkGone, detail) }

// PayloadTooLarge - 413: тело больше лимита.
func PayloadTooLarge(detail string) *Problem { return New(CodePayloadTooLarge, detail) }

// UnsupportedMediaType - 415: тело не JSON.
func UnsupportedMediaType(detail string) *Problem {
	return New(CodeUnsupportedMediaType, detail)
}

// RateLimited - 429 с заголовком Retry-After.
func RateLimited(retryAfterSeconds int) *Problem {
	return New(CodeRateLimited, "").WithRetryAfter(retryAfterSeconds)
}

// Internal - 500 без detail: текст исходной ошибки клиенту не отдаём, только в лог (NFR-7).
func Internal() *Problem { return New(CodeInternal, "") }
