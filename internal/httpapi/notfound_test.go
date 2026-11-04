package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/uair10/url_shortener_go/internal/httpapi/problem"
	"github.com/uair10/url_shortener_go/internal/httpapi/requestid"
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

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Allow"); got != tt.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tt.wantAllow)
			}

			body := decodeProblem(t, rec)
			if body["code"] != tt.wantCode {
				t.Errorf("code = %v, want %q", body["code"], tt.wantCode)
			}
			if body["status"] != float64(tt.wantStatus) {
				t.Errorf("status field = %v, want %d", body["status"], tt.wantStatus)
			}
			wantType := "https://sho.rt/errors/" + strings.ReplaceAll(tt.wantCode, "_", "-")
			if body["type"] != wantType {
				t.Errorf("type = %v, want %q", body["type"], wantType)
			}
			if id := rec.Header().Get(requestid.Header); id == "" || body["request_id"] != id {
				t.Errorf("request_id = %v, header %s = %q", body["request_id"], requestid.Header, id)
			}
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
	if body["detail"] != "link abc not found" {
		t.Errorf("detail = %v, want %q", body["detail"], "link abc not found")
	}
}
