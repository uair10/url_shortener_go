package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// accessLog - поля строки access log, которые проверяют тесты.
type accessLog struct {
	Msg        string   `json:"msg"`
	Method     string   `json:"method"`
	Path       string   `json:"path"`
	Status     int      `json:"status"`
	DurationMS *float64 `json:"duration_ms"`
}

func TestLogRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		target     string
		body       string
		wantPath   string
		wantStatus int
		secret     string // не должно попасть в лог
	}{
		{
			name:       "успешный запрос",
			method:     http.MethodGet,
			target:     "/healthz",
			wantPath:   "/healthz",
			wantStatus: http.StatusOK,
		},
		{
			name:       "404 и query-строка не в логе",
			method:     http.MethodGet,
			target:     "/unknown?token=secret-token",
			wantPath:   "/unknown",
			wantStatus: http.StatusNotFound,
			secret:     "secret-token",
		},
		{
			name:       "405 и тело не в логе",
			method:     http.MethodPost,
			target:     "/healthz",
			body:       `{"password":"secret-password"}`,
			wantPath:   "/healthz",
			wantStatus: http.StatusMethodNotAllowed,
			secret:     "secret-password",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			handler := newHandler(slog.New(slog.NewJSONHandler(&buf, nil)), 1024)

			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.target, strings.NewReader(tt.body))
			handler.ServeHTTP(httptest.NewRecorder(), req)

			var got accessLog
			if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
				t.Fatalf("log is not one JSON line: %v\n%s", err, buf.String())
			}
			if got.DurationMS == nil {
				t.Error("duration_ms is missing")
			}
			got.DurationMS = nil // время каждый раз разное, сравниваем остальное

			want := accessLog{Msg: "request", Method: tt.method, Path: tt.wantPath, Status: tt.wantStatus}
			if got != want {
				t.Errorf("log entry = %+v, want %+v", got, want)
			}
			if tt.secret != "" && strings.Contains(buf.String(), tt.secret) {
				t.Errorf("log contains %q: %s", tt.secret, buf.String())
			}
		})
	}
}

// Без Unwrap у statusRecorder ResponseController не найдёт Flush и вернёт ошибку.
func TestStatusRecorderUnwrap(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	wrapped := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}

	if err := http.NewResponseController(wrapped).Flush(); err != nil {
		t.Fatalf("Flush() = %v, want nil", err)
	}
	if !rec.Flushed {
		t.Error("underlying writer was not flushed")
	}
}
