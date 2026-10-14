package problem

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/uair10/url_shortener_go/internal/httpapi/requestid"
)

// Problem - ответ об ошибке по RFC 9457, поля - по SPEC, раздел 6.
//
//nolint:errname // Problem - термин RFC 9457, problem.ProblemError читается хуже.
type Problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail,omitempty"`
	Code      string `json:"code"`
	Instance  string `json:"instance,omitempty"`
	RequestID string `json:"request_id,omitempty"`

	// Расширения
	Errors []FieldError `json:"errors,omitempty"`

	RetryAfter int `json:"-"`
}

// FieldError - нарушенное правило валидации одного поля (для 422).
type FieldError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// Error возвращает код и detail, если он есть: в логах удобно искать по коду.
func (p *Problem) Error() string {
	if p.Detail == "" {
		return p.Code
	}
	return p.Code + ": " + p.Detail
}

// Write сериализует проблему в ответ. p не меняется: значения по умолчанию
// заполняются в копии, поэтому одну проблему можно писать из разных горутин.
func (p *Problem) Write(w http.ResponseWriter, r *http.Request) {
	out := *p
	if out.Status == 0 {
		out.Status = http.StatusInternalServerError
	}
	// Для 5xx detail клиенту не отдаём (NFR-7), кто бы и как ни собрал проблему.
	if out.Status >= http.StatusInternalServerError {
		out.Detail = ""
	}
	if out.Title == "" {
		out.Title = http.StatusText(out.Status)
	}
	if out.Type == "" {
		out.Type = "about:blank"
	}
	if out.Instance == "" && r != nil {
		out.Instance = r.URL.Path
	}
	// Автоподстановка RequestID из контекста, если он не задан явно.
	if out.RequestID == "" && r != nil {
		out.RequestID = requestid.From(r.Context())
	}

	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if out.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(out.RetryAfter))
	}

	body, err := json.Marshal(&out)
	if err != nil {
		// Не случится: в Problem только строки, числа и срезы строк.
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(out.Status)
	_, _ = w.Write(body)
}
