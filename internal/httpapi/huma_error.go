package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/danielgtaylor/huma/v2"

	"github.com/uair10/url_shortener_go/internal/infrastructure/logging"

	"github.com/uair10/url_shortener_go/internal/httpapi/problem"
	"github.com/uair10/url_shortener_go/internal/httpapi/requestid"
)

var installHumaErrorsOnce sync.Once //nolint:gochecknoglobals // huma.NewError глобальна, ставим один раз

// installHumaErrors один раз подменяет глобальные переменные пакета huma.
// Вызывается из newAPI (через sync.Once) - безопасно для параллельных тестов.
func installHumaErrors() {
	installHumaErrorsOnce.Do(func() {
		// NewError используется Huma при генерации схемы в OpenAPI (4xx/5xx).
		// Возвращаем Problem, чтобы спека описывала наш формат.
		huma.NewError = func(status int, message string, errs ...error) huma.StatusError {
			return newProblemFromHuma(nil, status, message, errs)
		}

		// NewErrorWithContext используется Huma при формировании реальных ответов.
		// Здесь доступен huma.Context, из него достаём *http.Request → request_id и logger.
		huma.NewErrorWithContext = func(ctx huma.Context, status int, message string, errs ...error) huma.StatusError {
			rctx := ctx.Context() // это r.Context() в humago

			p := newProblemFromHuma(ctx, status, message, errs)

			if p.Status >= http.StatusInternalServerError {
				logAttrs := []any{
					slog.Int("status", p.Status),
					slog.String("code", p.Code),
					slog.String("detail", message),
				}
				if err := errors.Join(errs...); err != nil {
					logAttrs = append(logAttrs, slog.String("error", err.Error()))
				}
				logging.FromCtx(rctx).Error("internal error", logAttrs...)
			}
			return p
		}
	})
}

// newProblemFromHuma переводит ошибку Huma в *problem.Problem.
//
// Правила:
//   - status берём тот, что прислала Huma (400/404/405/408/413/415/422/500);
//   - code = codeFromStatus(status), чтобы code и status были согласованы;
//   - errors[] заполняем ТОЛЬКО при 422 (валидация). При 400 (битый JSON)
//     и 415 (не JSON) Huma тоже кладёт ErrorDetail в errs, но это не поля формы;
//   - detail не отдаём при 5xx;
//   - instance и request_id подставляем из ctx, если он есть.
func newProblemFromHuma(ctx huma.Context, status int, message string, errs []error) *problem.Problem {
	p := problem.New(codeFromStatus(status), "")
	p.Status = status // сохраняем исходный (408, 418 и т.п.)

	if status < http.StatusInternalServerError {
		p.Detail = message
	}

	// errors[] — только для 422.
	if status == http.StatusUnprocessableEntity {
		var fields []problem.FieldError
		for _, err := range errs {
			var detail *huma.ErrorDetail
			if !errors.As(err, &detail) {
				continue
			}
			fields = append(fields, problem.FieldError{
				Field:  trimLocationPrefix(detail.Location),
				Reason: detail.Message,
			})
		}
		p.Errors = fields
	}

	// instance и request_id.
	if ctx != nil {
		p.Instance = ctx.URL().Path
		if rid := requestid.From(ctx.Context()); rid != "" {
			p.RequestID = rid
		}
	}
	return p
}

// trimLocationPrefix срезает "body." из ErrorDetail.Location. Префиксы query./path./header.
// остаются, чтобы query-параметр не путался с полем тела (SPEC, раздел 6).
func trimLocationPrefix(s string) string {
	return strings.TrimPrefix(s, "body.")
}

// codeFromStatus возвращает Code по HTTP-статусу ошибки, которую собрала Huma.
// Huma отдаёт только 400, 404, 405, 408, 413, 415, 422, 500. Статусы с несколькими
// доменными кодами (403, 409) сюда не приходят: их отдают конструкторы problem.*.
// Остальное - общий bad_request или internal, чтобы code и status не противоречили.
func codeFromStatus(status int) problem.Code {
	switch status {
	case http.StatusBadRequest:
		return problem.CodeMalformedJSON
	case http.StatusNotFound:
		return problem.CodeNotFound
	case http.StatusMethodNotAllowed:
		return problem.CodeMethodNotAllowed
	case http.StatusRequestTimeout:
		return problem.CodeRequestTimeout
	case http.StatusRequestEntityTooLarge:
		return problem.CodePayloadTooLarge
	case http.StatusUnsupportedMediaType:
		return problem.CodeUnsupportedMediaType
	case http.StatusUnprocessableEntity:
		return problem.CodeValidationFailed
	}
	if status >= http.StatusBadRequest && status < http.StatusInternalServerError {
		return problem.CodeBadRequest
	}
	return problem.CodeInternal
}
