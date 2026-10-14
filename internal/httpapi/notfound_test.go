package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

	handler := newHandler(slog.New(slog.DiscardHandler), 1024)

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

// Собственный 404 хендлера не должен подменяться на "route not found".
func TestHandlerNotFoundNotRewritten(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.Handle("GET /links/{code}", Handler(func(_ http.ResponseWriter, r *http.Request) error {
		return problem.NotFound("link " + r.PathValue("code") + " not found")
	}))
	handler := withMiddleware(mux, slog.New(slog.DiscardHandler), 1024)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/links/abc", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := decodeProblem(t, rec)["detail"]; got != "link abc not found" {
		t.Errorf("detail = %v, want %q", got, "link abc not found")
	}
}
