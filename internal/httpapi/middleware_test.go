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
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

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
		if err := json.Unmarshal(line, &e); err != nil {
			t.Fatalf("log line is not JSON: %v\n%s", err, line)
		}
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
			if len(lines) != 1 {
				t.Fatalf("access log lines = %d, want 1:\n%s", len(lines), buf.String())
			}
			checkAccessLine(t, lines[0], rec, logEntry{Method: tt.method, Path: tt.wantPath, Status: tt.wantStatus})
			if tt.secret != "" && strings.Contains(buf.String(), tt.secret) {
				t.Errorf("log contains %q:\n%s", tt.secret, buf.String())
			}
		})
	}
}

// checkAccessLine сверяет строку access log с ответом: метод, путь и статус - с want,
// размер - с телом ответа, request_id - с заголовком ответа.
func checkAccessLine(t *testing.T, got logEntry, rec *httptest.ResponseRecorder, want logEntry) {
	t.Helper()
	if got.DurationMS == nil {
		t.Error("duration_ms is missing")
	}
	if got.Bytes == nil {
		t.Error("bytes is missing")
	} else if *got.Bytes != int64(rec.Body.Len()) {
		t.Errorf("bytes = %d, want %d (response body size)", *got.Bytes, rec.Body.Len())
	}
	if id := rec.Header().Get(requestid.Header); got.RequestID == "" || got.RequestID != id {
		t.Errorf("request_id = %q, want %q (from response header)", got.RequestID, id)
	}
	if got.Method != want.Method || got.Path != want.Path || got.Status != want.Status {
		t.Errorf("method, path, status = %s %s %d, want %s %s %d",
			got.Method, got.Path, got.Status, want.Method, want.Path, want.Status)
	}
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

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if lines := accessLines(parseLog(t, &buf)); len(lines) != 0 {
				t.Errorf("healthz is in access log:\n%s", buf.String())
			}
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
			if tt.wantSame && got != tt.incoming {
				t.Errorf("%s = %q, want %q", requestid.Header, got, tt.incoming)
			}
			if !tt.wantSame && (got == tt.incoming || !requestid.Valid(got)) {
				t.Errorf("%s = %q, want new valid id", requestid.Header, got)
			}
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
		if seen[id] {
			t.Fatalf("duplicate request id %q", id)
		}
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

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get(requestid.Header); got != id {
				t.Errorf("%s = %q, want %q", requestid.Header, got, id)
			}
			if rec.Code >= http.StatusBadRequest {
				if got := decodeProblem(t, rec)["request_id"]; got != id {
					t.Errorf("body request_id = %v, want %q", got, id)
				}
			}
			checkLogRequestID(t, &buf, id, tt.wantLines)
		})
	}
}

// checkLogRequestID проверяет, что в логе wantLines строк и в каждой есть request_id = id.
func checkLogRequestID(t *testing.T, buf *bytes.Buffer, id string, wantLines int) {
	t.Helper()
	entries := parseLog(t, buf)
	if len(entries) != wantLines {
		t.Errorf("log lines = %d, want %d:\n%s", len(entries), wantLines, buf.String())
	}
	for _, e := range entries {
		if e.RequestID != id {
			t.Errorf("log line %q: request_id = %q, want %q", e.Msg, e.RequestID, id)
		}
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
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", status)
	}
	var problem map[string]any
	if err := json.Unmarshal(body, &problem); err != nil {
		t.Fatalf("body is not JSON: %v\n%s", err, body)
	}
	if problem["code"] != "internal" {
		t.Errorf("code = %v, want internal", problem["code"])
	}
	// NFR-7: ни текст паники, ни стек клиенту не уходят.
	if bytes.Contains(body, []byte("secret-panic")) || bytes.Contains(body, []byte("goroutine")) {
		t.Errorf("response leaks panic details: %s", body)
	}

	var stackLogged bool
	for _, e := range parseLog(t, &buf) {
		if e.Msg == "panic" && strings.Contains(e.Stack, "goroutine") {
			stackLogged = true
		}
	}
	if !stackLogged {
		t.Errorf("no panic line with stack in log:\n%s", buf.String())
	}

	// Следующий запрос к тому же серверу успешен.
	if status, body := doGet(t, srv.URL+"/ok"); status != http.StatusOK || string(body) != okBody {
		t.Errorf("next request = %d %q, want 200 %q", status, body, okBody)
	}
}

// doGet делает GET-запрос и возвращает код и тело ответа.
func doGet(t *testing.T, url string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, body
}
