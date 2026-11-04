package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/uair10/url_shortener_go/internal/httpapi/problem"
	"github.com/uair10/url_shortener_go/internal/httpapi/requestid"
)

// Тесты ошибок Huma: формат раздела 6 (BR-3.x, BR-5.7) на тестовых операциях.

// secretValue не должен попасть ни в тело ответа (BR-3.7), ни, где сказано, в лог.
const secretValue = "shr_SecretValue123"

// echoInput - тестовая операция с query-параметром и телом с ограничениями формата.
type echoInput struct {
	Limit int `default:"20" maximum:"100" minimum:"1" query:"limit"`
	Body  echoBody
}

type echoBody struct {
	URL   string `json:"url"             maxLength:"64" minLength:"1"`
	Count int    `json:"count,omitempty" minimum:"0"`
}

type echoOutput struct {
	Body echoOutBody
}

type echoOutBody struct {
	URL string `json:"url"`
}

// headerInput - тестовая операция с проверяемым заголовком.
type headerInput struct {
	Key string `header:"X-Test-Key" pattern:"^[a-z]+$" required:"true"`
}

// registerTestOps регистрирует операции, на которых проверяются ошибки.
func registerTestOps(api huma.API) {
	huma.Post(api, "/test/echo", func(_ context.Context, in *echoInput) (*echoOutput, error) {
		return &echoOutput{Body: echoOutBody{URL: in.Body.URL}}, nil
	})
	huma.Get(api, "/test/header", func(context.Context, *headerInput) (*struct{}, error) {
		return &struct{}{}, nil
	})
	huma.Get(api, "/test/problem", func(context.Context, *struct{}) (*struct{}, error) {
		return nil, problem.AliasTaken("alias go-docs is taken")
	})
	huma.Get(api, "/test/wrapped", func(context.Context, *struct{}) (*struct{}, error) {
		return nil, fmt.Errorf("create link: %w", problem.AliasTaken(""))
	})
	huma.Get(api, "/test/validation", func(context.Context, *struct{}) (*struct{}, error) {
		return nil, problem.Validation([]problem.FieldError{{Field: "url", Reason: "domain is blocked"}})
	})
	huma.Get(api, "/test/unknown", func(context.Context, *struct{}) (*struct{}, error) {
		return nil, errors.New("db: password=" + secretValue)
	})
}

func newErrorsTestHandler(buf *bytes.Buffer) http.Handler {
	return newTestAPIHandler(slog.New(slog.NewJSONHandler(buf, nil)), testConfig(), registerTestOps)
}

// BR-3.3, BR-3.6, раздел 6: ошибки, которые Huma находит сам, - со своим статусом, кодом и полями.
func TestRequestErrors(t *testing.T) {
	t.Parallel()

	const jsonCT = "application/json"
	tooLarge := `{"url":"` + strings.Repeat("a", testMaxBodyBytes) + `"}`

	tests := []struct {
		name        string
		target      string
		contentType string
		body        string
		wantStatus  int
		wantCode    string
		wantFields  []string // nil - поле errors не проверяется
	}{
		{"битый JSON", "/test/echo", jsonCT, `{"url":`, 400, "malformed_json", nil},
		{"не JSON", "/test/echo", "text/plain", "url=x", 415, "unsupported_media_type", nil},
		{"тело больше лимита (NFR-2)", "/test/echo", jsonCT, tooLarge, 413, "payload_too_large", nil},
		{"лишнее поле", "/test/echo", jsonCT, `{"url":"x","extra":1}`, 422, "validation_failed", []string{"extra"}},
		{"неверный тип", "/test/echo", jsonCT, `{"url":123}`, 422, "validation_failed", []string{"url"}},
		{"нарушено ограничение", "/test/echo", jsonCT, `{"url":""}`, 422, "validation_failed", []string{"url"}},
		{
			"все нарушения сразу (BR-3.3)", "/test/echo", jsonCT, `{"url":"","count":-1,"extra":true}`,
			422, "validation_failed",
			[]string{"count", "extra", "url"},
		},
		{"query не число", "/test/echo?limit=abc", jsonCT, `{"url":"x"}`, 422, "validation_failed", []string{"query.limit"}},
		{"query вне диапазона", "/test/echo?limit=0", jsonCT, `{"url":"x"}`, 422, "validation_failed", []string{"query.limit"}},
		{"query и тело вместе", "/test/echo?limit=101", jsonCT, `{"url":""}`, 422, "validation_failed", []string{"query.limit", "url"}},
	}

	handler := newErrorsTestHandler(&bytes.Buffer{})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := serve(t, handler, http.MethodPost, tt.target, tt.contentType, tt.body)
			body := checkProblem(t, rec, tt.wantStatus, tt.wantCode)
			if tt.wantFields == nil {
				return
			}
			if got := fieldErrors(t, body); !slices.Equal(got, tt.wantFields) {
				t.Errorf("error fields = %v, want %v", got, tt.wantFields)
			}
		})
	}
}

// Валидный запрос проходит: тестовая операция рабочая, ошибки выше - не случайность.
func TestRequestValid(t *testing.T) {
	t.Parallel()

	rec := serve(t, newErrorsTestHandler(&bytes.Buffer{}), http.MethodPost, "/test/echo?limit=5",
		"application/json", `{"url":"https://go.dev"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"url":"https://go.dev"`) {
		t.Errorf("body = %s, want url echoed", rec.Body.String())
	}
}

// BR-3.7: значения из запроса не возвращаются в ответе об ошибке
// (Huma по умолчанию кладёт их в errors[].value).
func TestErrorBodyHasNoRequestValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target string
		header string
		body   string
	}{
		{"лишнее поле", "/test/echo", "", `{"url":"x","api_key":"` + secretValue + `"}`},
		{"нарушено ограничение", "/test/echo", "", `{"url":"` + secretValue + strings.Repeat("a", 64) + `"}`},
		{"битый JSON", "/test/echo", "", `{"url":"` + secretValue + `"`},
		{"заголовок", "/test/header", secretValue, ""},
	}

	handler := newErrorsTestHandler(&bytes.Buffer{})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			method := http.MethodPost
			if tt.header != "" {
				method = http.MethodGet
			}
			req := newRequest(t, method, tt.target, tt.body)
			if tt.header != "" {
				req.Header.Set("X-Test-Key", tt.header)
			}
			rec := doRequest(handler, req)

			if rec.Code < http.StatusBadRequest {
				t.Fatalf("status = %d, want error", rec.Code)
			}
			if strings.Contains(rec.Body.String(), secretValue) {
				t.Errorf("error body echoes request value: %s", rec.Body.String())
			}
		})
	}
}

// BR-3.4, BR-5.7: ошибка, которую вернула операция, - в формате раздела 6.
// *problem.Problem отдаётся как есть (и обёрнутый тоже), неизвестная ошибка - 500 без текста.
func TestOperationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		target     string
		wantStatus int
		wantCode   string
		wantDetail string
	}{
		{"проблема как есть", "/test/problem", 409, "alias_taken", "alias go-docs is taken"},
		{"обёрнутая проблема", "/test/wrapped", 409, "alias_taken", ""},
		{"проблема валидации из домена", "/test/validation", 422, "validation_failed", ""},
		{"неизвестная ошибка", "/test/unknown", 500, "internal", ""},
	}

	handler := newErrorsTestHandler(&bytes.Buffer{})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := serve(t, handler, http.MethodGet, tt.target, "", "")
			body := checkProblem(t, rec, tt.wantStatus, tt.wantCode)
			if tt.wantDetail != "" && body["detail"] != tt.wantDetail {
				t.Errorf("detail = %v, want %q", body["detail"], tt.wantDetail)
			}
			if strings.Contains(rec.Body.String(), secretValue) {
				t.Errorf("body leaks error text (NFR-7): %s", rec.Body.String())
			}
		})
	}

	t.Run("поля ошибки валидации из домена", func(t *testing.T) {
		t.Parallel()

		rec := serve(t, handler, http.MethodGet, "/test/validation", "", "")
		if got := fieldErrors(t, decodeProblem(t, rec)); !slices.Equal(got, []string{"url"}) {
			t.Errorf("error fields = %v, want [url]", got)
		}
	})
}

// BR-3.4: текст неизвестной ошибки - в логе, с request ID запроса (BR-4.2).
func TestUnknownErrorLogged(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	req := newRequest(t, http.MethodGet, "/test/unknown", "")
	req.Header.Set(requestid.Header, "req-500")
	rec := doRequest(newErrorsTestHandler(&buf), req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var logged bool
	for line := range bytes.Lines(buf.Bytes()) {
		if bytes.Contains(line, []byte(secretValue)) && bytes.Contains(line, []byte(`"request_id":"req-500"`)) {
			logged = true
		}
	}
	if !logged {
		t.Errorf("no log line with error text and request_id:\n%s", buf.String())
	}
}
