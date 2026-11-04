package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uair10/url_shortener_go/internal/httpapi/requestid"
)

// Тесты SHR-4: middleware request ID, access log, recovery - в сборке, как в проде (Huma + withMiddleware).

// logEntry - поля строки лога, которые проверяют тесты.
type logEntry struct {
	Msg        string   `json:"msg"`
	RequestID  string   `json:"request_id"`
	Method     string   `json:"method"`
	Path       string   `json:"path"`
	Status     int      `json:"status"`
	DurationMS *float64 `json:"duration_ms"`
	Bytes      *int64   `json:"bytes"`
	Stack      string   `json:"stack"`
}

const okBody = "hello"

// bytesOutput - ответ тестовой операции с сырым телом.
type bytesOutput struct {
	Body []byte
}

// newTestHandler собирает прод-сборку (Huma + middleware) с тестовыми операциями и пишет логи в buf.
func newTestHandler(buf *bytes.Buffer) http.Handler {
	logger := slog.New(slog.NewJSONHandler(buf, nil))
	return newTestAPIHandler(logger, testConfig(), func(api huma.API) {
		huma.Get(api, "/ok", func(context.Context, *struct{}) (*bytesOutput, error) {
			return &bytesOutput{Body: []byte(okBody)}, nil
		})
		huma.Get(api, "/panic", func(context.Context, *struct{}) (*struct{}, error) {
			panic("boom secret-panic")
		})
		huma.Get(api, "/fail", func(context.Context, *struct{}) (*struct{}, error) {
			return nil, errors.New("db: password=secret-db")
		})
	})
}

// parseLog разбирает JSON-строки лога.
func parseLog(t *testing.T, buf *bytes.Buffer) []logEntry {
	t.Helper()
	var entries []logEntry
	for line := range bytes.Lines(buf.Bytes()) {
		var e logEntry
		require.NoError(t, json.Unmarshal(line, &e), "log line is not JSON: %s", line)
		entries = append(entries, e)
	}
	return entries
}

// accessLines оставляет только строки access log.
func accessLines(entries []logEntry) []logEntry {
	var out []logEntry
	for _, e := range entries {
		if e.Msg == "request" {
			out = append(out, e)
		}
	}
	return out
}

// BR-4.3: метод, путь без query, статус, длительность, размер ответа, request ID.
// NFR-6: query и тело запроса в лог не попадают.
func TestAccessLogFields(t *testing.T) {
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
			target:     "/ok",
			wantPath:   "/ok",
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
			// BR-4.5 исключает только GET: остальные запросы к /healthz логируются.
			name:       "405 на /healthz и тело не в логе",
			method:     http.MethodPost,
			target:     "/healthz",
			body:       `{"password":"secret-password"}`,
			wantPath:   "/healthz",
			wantStatus: http.StatusMethodNotAllowed,
			secret:     "secret-password",
		},
		{
			name:       "паника - 500",
			method:     http.MethodGet,
			target:     "/panic",
			wantPath:   "/panic",
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.target, strings.NewReader(tt.body))
			newTestHandler(&buf).ServeHTTP(rec, req)

			lines := accessLines(parseLog(t, &buf))
			require.Len(t, lines, 1, "access log lines, log:\n%s", buf.String())
			checkAccessLine(t, lines[0], rec, logEntry{Method: tt.method, Path: tt.wantPath, Status: tt.wantStatus})
			if tt.secret != "" {
				assert.NotContains(t, buf.String(), tt.secret, "secret in log")
			}
		})
	}
}

// checkAccessLine сверяет строку access log с ответом: метод, путь и статус - с want,
// размер - с телом ответа, request_id - с заголовком ответа.
func checkAccessLine(t *testing.T, got logEntry, rec *httptest.ResponseRecorder, want logEntry) {
	t.Helper()
	assert.NotNil(t, got.DurationMS, "duration_ms")
	if assert.NotNil(t, got.Bytes, "bytes") {
		assert.EqualValues(t, rec.Body.Len(), *got.Bytes, "bytes must match response body size")
	}
	assert.NotEmpty(t, got.RequestID, "request_id")
	assert.Equal(t, rec.Header().Get(requestid.Header), got.RequestID, "request_id must match response header")
	assert.Equal(t, want.Method, got.Method, "method")
	assert.Equal(t, want.Path, got.Path, "path")
	assert.Equal(t, want.Status, got.Status, "status")
}

// BR-4.5: GET /healthz не пишется в access log.
func TestAccessLogSkipsHealthz(t *testing.T) {
	t.Parallel()

	for _, target := range []string{"/healthz", "/healthz?probe=k8s"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			rec := httptest.NewRecorder()
			newTestHandler(&buf).ServeHTTP(rec,
				httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody))

			require.Equal(t, http.StatusOK, rec.Code)
			assert.Empty(t, accessLines(parseLog(t, &buf)), "healthz is in access log")
		})
	}
}

// BR-4.1: валидный X-Request-ID используется как есть, иначе генерируется новый.
func TestRequestIDHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		incoming string
		wantSame bool
	}{
		{"валидный используется как есть", "abc-123-XYZ", true},
		{"ровно 64 символа", strings.Repeat("a", 64), true},
		{"длиннее 64", strings.Repeat("a", 65), false},
		{"без ID генерируется", "", false},
		{"перевод строки (подделка лога)", "abc\nfake-log-line", false},
		{"пробел", "abc 123", false},
		{"подчёркивание", "abc_123", false},
		{"не ASCII", "идентификатор", false},
	}

	handler := newTestHandler(&bytes.Buffer{})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ok", http.NoBody)
			if tt.incoming != "" {
				req.Header.Set(requestid.Header, tt.incoming)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			got := rec.Header().Get(requestid.Header)
			if tt.wantSame {
				assert.Equal(t, tt.incoming, got, "%s must be reused", requestid.Header)
				return
			}
			assert.NotEqual(t, tt.incoming, got, "%s must be regenerated", requestid.Header)
			assert.True(t, requestid.Valid(got), "generated %s = %q is not valid", requestid.Header, got)
		})
	}
}

// BR-4.1: сгенерированные ID разные у разных запросов.
func TestRequestIDGeneratedUnique(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(&bytes.Buffer{})
	seen := make(map[string]bool)
	for range 100 {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ok", http.NoBody))
		id := rec.Header().Get(requestid.Header)
		require.False(t, seen[id], "duplicate request id %q", id)
		seen[id] = true
	}
}

// BR-4.2: X-Request-ID есть в ответе, в каждой строке лога запроса и в теле ошибки.
func TestRequestIDPropagation(t *testing.T) {
	t.Parallel()

	const id = "req-42"

	tests := []struct {
		name       string
		target     string
		wantStatus int
		wantLines  int // строк лога о запросе, включая access log
	}{
		{"успешный ответ", "/ok", http.StatusOK, 1},
		{"404 из роутера", "/unknown", http.StatusNotFound, 1},
		{"ошибка хендлера", "/fail", http.StatusInternalServerError, 2},
		{"паника", "/panic", http.StatusInternalServerError, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.target, http.NoBody)
			req.Header.Set(requestid.Header, id)
			rec := httptest.NewRecorder()
			newTestHandler(&buf).ServeHTTP(rec, req)

			require.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, id, rec.Header().Get(requestid.Header), "response header")
			if rec.Code >= http.StatusBadRequest {
				assert.Equal(t, id, decodeProblem(t, rec)["request_id"], "body request_id")
			}
			checkLogRequestID(t, &buf, id, tt.wantLines)
		})
	}
}

// checkLogRequestID проверяет, что в логе wantLines строк и в каждой есть request_id = id.
func checkLogRequestID(t *testing.T, buf *bytes.Buffer, id string, wantLines int) {
	t.Helper()
	entries := parseLog(t, buf)
	assert.Len(t, entries, wantLines, "log lines, log:\n%s", buf.String())
	for i, e := range entries {
		assert.Equal(t, id, e.RequestID, "log line %d (%q): request_id", i, e.Msg)
	}
}

// BR-4.4: паника в хендлере - 500 internal, стек в лог, сервер продолжает работать.
// Через настоящий HTTP-сервер: проверяем, что процесс и соединения переживают панику.
func TestRecoverPanic(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	srv := httptest.NewServer(newTestHandler(&buf))
	t.Cleanup(srv.Close)

	status, body := doGet(t, srv.URL+"/panic")
	require.Equal(t, http.StatusInternalServerError, status)
	var problem map[string]any
	require.NoError(t, json.Unmarshal(body, &problem), "body is not JSON: %s", body)
	assert.Equal(t, "internal", problem["code"], "field code")
	// NFR-7: ни текст паники, ни стек клиенту не уходят.
	assert.NotContains(t, string(body), "secret-panic", "response leaks panic text")
	assert.NotContains(t, string(body), "goroutine", "response leaks stack")

	assert.True(t, slices.ContainsFunc(parseLog(t, &buf), func(e logEntry) bool {
		return e.Msg == "panic" && strings.Contains(e.Stack, "goroutine")
	}), "no panic line with stack in log:\n%s", buf.String())

	// Следующий запрос к тому же серверу успешен.
	status, body = doGet(t, srv.URL+"/ok")
	assert.Equal(t, http.StatusOK, status, "next request status")
	assert.Equal(t, okBody, string(body), "next request body")
}

// doGet делает GET-запрос и возвращает код и тело ответа.
func doGet(t *testing.T, url string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	require.NoError(t, err, "new request")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "GET %s", url)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "read body")
	return resp.StatusCode, body
}
