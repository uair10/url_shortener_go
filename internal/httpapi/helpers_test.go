package httpapi

import (
	"encoding/json"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/uair10/url_shortener_go/internal/httpapi/requestid"
	"github.com/uair10/url_shortener_go/internal/infrastructure/config"
)

// testMaxBodyBytes - лимит тела в тестах: маленький, чтобы 413 получить коротким телом.
const testMaxBodyBytes = 1024

// testConfig - конфиг сервера для тестов: короткие таймауты, маленький лимит тела, документация включена.
func testConfig() config.Server {
	return config.Server{
		ShutdownTimeout:   time.Second,
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       time.Second,
		WriteTimeout:      time.Second,
		IdleTimeout:       time.Second,
		MaxBodyBytes:      testMaxBodyBytes,
		DocsEnabled:       true,
	}
}

// newTestAPIHandler собирает обработчик так же, как newHandler, но позволяет
// зарегистрировать в том же API тестовые операции.
func newTestAPIHandler(logger *slog.Logger, cfg config.Server, register func(api huma.API)) http.Handler {
	mux := http.NewServeMux()
	api := newAPI(mux, cfg)
	registerRoutes(api)
	if register != nil {
		register(api)
	}
	return withMiddleware(mux, logger, cfg.MaxBodyBytes)
}

// newRequest собирает запрос с контекстом теста.
func newRequest(t *testing.T, method, target, body string) *http.Request {
	t.Helper()
	return httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
}

// doRequest прогоняет запрос через handler и возвращает записанный ответ.
func doRequest(handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// serve выполняет запрос к handler. contentType пустой - заголовок не ставится.
func serve(t *testing.T, handler http.Handler, method, target, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest(t, method, target, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return doRequest(handler, req)
}

// decodeProblem проверяет Content-Type и разбирает тело как problem+json.
func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type = %q, want application/problem+json\n%s", ct, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v\n%s", err, rec.Body.String())
	}
	return body
}

// checkProblem проверяет ответ-ошибку на формат из SPEC, раздел 6 (BR-3.1, BR-4.2, BR-5.7)
// и возвращает разобранное тело.
func checkProblem(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) map[string]any {
	t.Helper()
	if rec.Code != wantStatus {
		t.Errorf("status = %d, want %d\n%s", rec.Code, wantStatus, rec.Body.String())
	}
	body := decodeProblem(t, rec)

	// Все поля ошибки из раздела 6. Других (например, $schema от Huma) быть не должно.
	problemKeys := []string{"type", "title", "status", "detail", "code", "instance", "request_id", "errors"}
	for key := range body {
		if !slices.Contains(problemKeys, key) {
			t.Errorf("unexpected field %q in error body: %s", key, rec.Body.String())
		}
	}
	if body["code"] != wantCode {
		t.Errorf("code = %v, want %q", body["code"], wantCode)
	}
	if body["status"] != float64(wantStatus) {
		t.Errorf("status field = %v, want %d", body["status"], wantStatus)
	}
	wantType := "https://sho.rt/errors/" + strings.ReplaceAll(wantCode, "_", "-")
	if body["type"] != wantType {
		t.Errorf("type = %v, want %q", body["type"], wantType)
	}
	if title, _ := body["title"].(string); title == "" {
		t.Error("title is empty")
	}
	if id := rec.Header().Get(requestid.Header); id == "" || body["request_id"] != id {
		t.Errorf("request_id = %v, header %s = %q", body["request_id"], requestid.Header, id)
	}
	return body
}

// fieldErrors проверяет формат errors[] (только field и reason, оба непустые)
// и возвращает отсортированный список полей.
func fieldErrors(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, ok := body["errors"].([]any)
	if !ok {
		t.Fatalf("errors = %v, want array", body["errors"])
	}
	fields := make([]string, 0, len(raw))
	for _, item := range raw {
		e, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("errors item = %v, want object", item)
		}
		if keys := slices.Sorted(maps.Keys(e)); !slices.Equal(keys, []string{"field", "reason"}) {
			t.Errorf("errors item keys = %v, want [field reason]", keys)
		}
		field, _ := e["field"].(string)
		reason, _ := e["reason"].(string)
		if field == "" || reason == "" {
			t.Errorf("errors item has empty field or reason: %v", e)
		}
		fields = append(fields, field)
	}
	slices.Sort(fields)
	return fields
}
