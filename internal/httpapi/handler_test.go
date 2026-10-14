package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uair10/url_shortener_go/internal/httpapi/problem"
	"github.com/uair10/url_shortener_go/internal/infrastructure/logging"
)

// decodeProblem проверяет Content-Type и разбирает тело как problem+json.
func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v\n%s", err, rec.Body.String())
	}
	return body
}

func TestHandlerErrors(t *testing.T) {
	t.Parallel()

	const secret = "db password is hunter2"

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"проблема отдаётся как есть", problem.Validation([]problem.FieldError{{Field: "url", Reason: "required"}}), 422, "validation_failed"},
		{"неизвестная ошибка - 500 без текста", errors.New(secret), 500, "internal"},
		{"обёрнутая проблема", fmt.Errorf("create link: %w", problem.AliasTaken("")), 409, "alias_taken"},
		{"свой таймаут хендлера, клиент ждёт - 500", fmt.Errorf("query: %w", context.DeadlineExceeded), 500, "internal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := Handler(func(http.ResponseWriter, *http.Request) error { return tt.err })
			ctx := logging.WithLogger(t.Context(), slog.New(slog.DiscardHandler))
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", http.NoBody)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := decodeProblem(t, rec)["code"]; got != tt.wantCode {
				t.Errorf("code = %v, want %q", got, tt.wantCode)
			}
			if strings.Contains(rec.Body.String(), secret) {
				t.Errorf("body leaks error text: %s", rec.Body.String())
			}
		})
	}
}

func TestHandlerClientGone(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(logging.WithLogger(t.Context(), slog.New(slog.DiscardHandler)))
	h := Handler(func(_ http.ResponseWriter, r *http.Request) error {
		cancel()
		return r.Context().Err()
	})

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != statusClientClosedRequest {
		t.Errorf("status = %d, want %d", rec.Code, statusClientClosedRequest)
	}
	if rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "" {
		t.Errorf("response written for gone client: %q", rec.Body.String())
	}
}
