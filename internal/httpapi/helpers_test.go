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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	require.Contains(t, rec.Header().Get("Content-Type"), "application/problem+json",
		"Content-Type of error response, body: %s", rec.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body is not JSON: %s", rec.Body.String())
	return body
}

// problemKeys - все поля ошибки из SPEC, раздел 6. Других (например, $schema от Huma) быть не должно.
var problemKeys = []string{"type", "title", "status", "detail", "code", "instance", "request_id", "errors"} //nolint:gochecknoglobals // неизменяемый список для тестов

// checkProblem проверяет ответ-ошибку на формат из SPEC, раздел 6 (BR-3.1, BR-4.2, BR-5.7)
// и возвращает разобранное тело.
func checkProblem(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) map[string]any {
	t.Helper()
	assert.Equal(t, wantStatus, rec.Code, "HTTP status, body: %s", rec.Body.String())
	body := decodeProblem(t, rec)

	assert.Subset(t, problemKeys, slices.Collect(maps.Keys(body)), "fields of error body outside section 6")
	assert.Equal(t, wantCode, body["code"], "field code")
	assert.EqualValues(t, wantStatus, body["status"], "field status")
	assert.Equal(t, "https://sho.rt/errors/"+strings.ReplaceAll(wantCode, "_", "-"), body["type"], "field type")
	assert.NotEmpty(t, body["title"], "field title")

	id := rec.Header().Get(requestid.Header)
	assert.NotEmpty(t, id, "header %s", requestid.Header)
	assert.Equal(t, id, body["request_id"], "field request_id must match header %s", requestid.Header)
	return body
}

// fieldErrors проверяет формат errors[] (только field и reason, оба непустые)
// и возвращает отсортированный список полей.
func fieldErrors(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, ok := body["errors"].([]any)
	require.True(t, ok, "errors = %#v, want array", body["errors"])

	fields := make([]string, 0, len(raw))
	for i, item := range raw {
		e, ok := item.(map[string]any)
		require.True(t, ok, "errors[%d] = %#v, want object", i, item)
		assert.ElementsMatch(t, []string{"field", "reason"}, slices.Collect(maps.Keys(e)), "errors[%d] keys", i)
		assert.NotEmpty(t, e["field"], "errors[%d].field", i)
		assert.NotEmpty(t, e["reason"], "errors[%d].reason", i)
		field, _ := e["field"].(string)
		fields = append(fields, field)
	}
	slices.Sort(fields)
	return fields
}
