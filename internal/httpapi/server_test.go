package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/uair10/url_shortener_go/internal/httpapi/requestid"
	"github.com/uair10/url_shortener_go/internal/infrastructure/config"
)

// newTestServer собирает сервер на заданном порту с логгером, который ничего не пишет.
func newTestServer(t *testing.T, port int) *Server {
	t.Helper()
	return New(slog.New(slog.DiscardHandler), config.Server{
		Port:              port,
		ShutdownTimeout:   time.Second,
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       time.Second,
		WriteTimeout:      time.Second,
		IdleTimeout:       time.Second,
		MaxBodyBytes:      1024,
	})
}

func TestServerRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		method          string
		path            string
		wantStatus      int
		wantBody        string
		wantContentType string
		wantAllow       string
	}{
		{
			name:            "healthz отвечает ok",
			method:          http.MethodGet,
			path:            "/healthz",
			wantStatus:      http.StatusOK,
			wantBody:        `{"status":"ok"}`,
			wantContentType: "application/json",
		},
		{
			name:            "healthz с query-строкой",
			method:          http.MethodGet,
			path:            "/healthz?probe=k8s",
			wantStatus:      http.StatusOK,
			wantBody:        `{"status":"ok"}`,
			wantContentType: "application/json",
		},
		{
			// GET-паттерн в ServeMux автоматически обслуживает и HEAD.
			// Тело у HEAD отбрасывает http.Server, а не обработчик, поэтому recorder его видит.
			name:            "HEAD на healthz",
			method:          http.MethodHead,
			path:            "/healthz",
			wantStatus:      http.StatusOK,
			wantContentType: "application/json",
		},
		{
			name:       "неверный метод",
			method:     http.MethodPost,
			path:       "/healthz",
			wantStatus: http.StatusMethodNotAllowed,
			wantAllow:  "GET, HEAD",
		},
		{
			name:       "неизвестный путь",
			method:     http.MethodGet,
			path:       "/unknown",
			wantStatus: http.StatusNotFound,
		},
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
			if tt.wantBody != "" && rec.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
			if got := rec.Header().Get("Content-Type"); tt.wantContentType != "" && got != tt.wantContentType {
				t.Errorf("Content-Type = %q, want %q", got, tt.wantContentType)
			}
			if got := rec.Header().Get("Allow"); tt.wantAllow != "" && got != tt.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tt.wantAllow)
			}
		})
	}
}

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

func TestRecover(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /panic", func(http.ResponseWriter, *http.Request) { panic("boom") })
	handler := withMiddleware(mux, slog.New(slog.NewJSONHandler(&buf, nil)), 1024)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/panic", http.NoBody))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if got := decodeProblem(t, rec)["code"]; got != "internal" {
		t.Errorf("code = %v, want internal", got)
	}

	// Access log пишется и для паники - со статусом 500.
	var logged bool
	for line := range bytes.Lines(buf.Bytes()) {
		var entry accessLog
		if json.Unmarshal(line, &entry) == nil && entry.Msg == "request" {
			logged = true
			if entry.Status != http.StatusInternalServerError {
				t.Errorf("access log status = %d, want 500", entry.Status)
			}
		}
	}
	if !logged {
		t.Errorf("no access log line after panic:\n%s", buf.String())
	}

	// Сервер продолжает работать.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", http.NoBody))
	if rec.Code != http.StatusOK {
		t.Errorf("next request status = %d, want 200", rec.Code)
	}
}

func TestRequestIDHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		incoming string
		wantSame bool
	}{
		{"валидный используется как есть", "abc-123-XYZ", true},
		{"без ID генерируется", "", false},
		{"недопустимые символы", "abc\nfake-log-line", false},
		{"длиннее 64", strings.Repeat("a", 65), false},
	}

	handler := newHandler(slog.New(slog.DiscardHandler), 1024)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", http.NoBody)
			if tt.incoming != "" {
				req.Header.Set(requestid.Header, tt.incoming)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			got := rec.Header().Get(requestid.Header)
			if tt.wantSame && got != tt.incoming {
				t.Errorf("%s = %q, want %q", requestid.Header, got, tt.incoming)
			}
			if !tt.wantSame && (got == tt.incoming || !requestid.Valid(got)) {
				t.Errorf("%s = %q, want new valid id", requestid.Header, got)
			}
		})
	}
}

func TestServerListenPortBusy(t *testing.T) {
	t.Parallel()

	// Занимаем порт сами, чтобы сервер не смог его взять.
	lc := net.ListenConfig{}
	busy, err := lc.Listen(t.Context(), "tcp", ":0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = busy.Close() })
	port := busy.Addr().(*net.TCPAddr).Port

	err = newTestServer(t, port).Listen(t.Context())
	if err == nil {
		t.Fatal("Listen() = nil, want error: port is busy")
	}
}

func TestServerShutdown(t *testing.T) {
	t.Parallel()

	// Свой listener на :0: порт занят ещё до старта сервера, гонки за порт нет.
	lc := net.ListenConfig{}
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := newTestServer(t, 0)

	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(ln) }()

	// Запрос пройдёт, даже если Serve ещё не начал принимать: соединение ждёт в очереди ОС.
	if status := get(t, "http://"+ln.Addr().String()+"/healthz"); status != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want %d", status, http.StatusOK)
	}

	if err := server.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown() = %v, want nil", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Serve() after Shutdown = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve() did not return after Shutdown")
	}
}

// get делает GET-запрос и возвращает код ответа.
func get(t *testing.T, url string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode
}
