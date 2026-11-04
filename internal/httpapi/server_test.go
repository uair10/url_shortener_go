package httpapi

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestServer собирает сервер на заданном порту с логгером, который ничего не пишет.
func newTestServer(t *testing.T, port int) *Server {
	t.Helper()
	cfg := testConfig()
	cfg.Port = port
	return New(slog.New(slog.DiscardHandler), cfg)
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

	handler := newHandler(slog.New(slog.DiscardHandler), testConfig())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, http.NoBody)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code, "HTTP status")
			// Сравниваем JSON, а не байты: энкодер Huma добавляет перевод строки.
			// Лишних полей (например, $schema от Huma) быть не должно: BR-2.6.
			if tt.wantBody != "" {
				assert.JSONEq(t, tt.wantBody, rec.Body.String(), "body")
			}
			if tt.wantContentType != "" {
				assert.Equal(t, tt.wantContentType, rec.Header().Get("Content-Type"), "header Content-Type")
			}
			if tt.wantAllow != "" {
				assert.Equal(t, tt.wantAllow, rec.Header().Get("Allow"), "header Allow")
			}
		})
	}
}

func TestServerListenPortBusy(t *testing.T) {
	t.Parallel()

	// Занимаем порт сами, чтобы сервер не смог его взять.
	lc := net.ListenConfig{}
	busy, err := lc.Listen(t.Context(), "tcp", ":0")
	require.NoError(t, err, "listen")
	t.Cleanup(func() { _ = busy.Close() })
	addr, ok := busy.Addr().(*net.TCPAddr)
	require.True(t, ok, "Addr() = %T, want *net.TCPAddr", busy.Addr())

	assert.Error(t, newTestServer(t, addr.Port).Listen(t.Context()), "Listen() on busy port")
}

func TestServerShutdown(t *testing.T) {
	t.Parallel()

	// Свой listener на :0: порт занят ещё до старта сервера, гонки за порт нет.
	lc := net.ListenConfig{}
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err, "listen")
	server := newTestServer(t, 0)

	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(ln) }()

	// Запрос пройдёт, даже если Serve ещё не начал принимать: соединение ждёт в очереди ОС.
	require.Equal(t, http.StatusOK, get(t, "http://"+ln.Addr().String()+"/healthz"), "GET /healthz")
	require.NoError(t, server.Shutdown(t.Context()), "Shutdown()")

	select {
	case err := <-errCh:
		require.NoError(t, err, "Serve() after Shutdown")
	case <-time.After(2 * time.Second):
		require.Fail(t, "Serve() did not return after Shutdown")
	}
}

// get делает GET-запрос и возвращает код ответа.
func get(t *testing.T, url string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	require.NoError(t, err, "new request")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "GET %s", url)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode
}
