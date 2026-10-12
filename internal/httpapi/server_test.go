package httpapi

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
