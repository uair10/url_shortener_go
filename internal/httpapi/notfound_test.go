package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"

	"github.com/uair10/url_shortener_go/internal/httpapi/problem"
)

func TestRouteErrorsAsProblem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantCode   string
		wantAllow  string
	}{
		{"неизвестный путь", http.MethodGet, "/unknown", http.StatusNotFound, "not_found", ""},
		{"неверный метод", http.MethodPost, "/healthz", http.StatusMethodNotAllowed, "method_not_allowed", "GET, HEAD"},
	}

	handler := newHandler(slog.New(slog.DiscardHandler), testConfig())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, http.NoBody)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantAllow, rec.Header().Get("Allow"), "header Allow")
			checkProblem(t, rec, tt.wantStatus, tt.wantCode)
		})
	}
}

type codeInput struct {
	Code string `path:"code"`
}

// Собственный 404 операции не должен подменяться на "route not found".
func TestHandlerNotFoundNotRewritten(t *testing.T) {
	t.Parallel()

	handler := newTestAPIHandler(slog.New(slog.DiscardHandler), testConfig(), func(api huma.API) {
		huma.Get(api, "/test/links/{code}", func(_ context.Context, in *codeInput) (*struct{}, error) {
			return nil, problem.NotFound("link " + in.Code + " not found")
		})
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test/links/abc", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := checkProblem(t, rec, http.StatusNotFound, "not_found")
	assert.Equal(t, "link abc not found", body["detail"], "field detail")
}
