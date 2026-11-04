package httpapi

import (
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Тесты SHR-5: документация и спецификация, построенная из кода.

// specFile - сгенерированная спецификация в репозитории (BR-5.1), путь от каталога пакета.
const specFile = "../../api/openapi.yaml"

// buildSpec строит спецификацию тем же путём, что и make openapi.
func buildSpec(t *testing.T) *huma.OpenAPI {
	t.Helper()
	return specAPI().OpenAPI()
}

func specYAML(t *testing.T) []byte {
	t.Helper()
	b, err := OpenAPIYAML()
	require.NoError(t, err, "spec to YAML")
	return b
}

// BR-5.3: /openapi.yaml и /docs есть, только если DOCS_ENABLED=true.
func TestDocsRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		path   string
		wantCT string
		want   string // подстрока тела
	}{
		{"spec", "/openapi.yaml", "yaml", "openapi: 3.1"},
		{"ui", "/docs", "text/html", "/openapi"},
	}

	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("docs_enabled=%t", enabled), func(t *testing.T) {
			t.Parallel()

			cfg := testConfig()
			cfg.DocsEnabled = enabled
			handler := newHandler(slog.New(slog.DiscardHandler), cfg)

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()

					rec := serve(t, handler, http.MethodGet, tt.path, "", "")
					if !enabled {
						checkProblem(t, rec, http.StatusNotFound, "not_found")
						return
					}
					require.Equal(t, http.StatusOK, rec.Code, "GET %s", tt.path)
					assert.Contains(t, rec.Header().Get("Content-Type"), tt.wantCT, "Content-Type")
					assert.Contains(t, rec.Body.String(), tt.want, "body")
				})
			}
		})
	}
}

// BR-5.6: api/openapi.yaml совпадает со спецификацией, построенной из кода.
func TestOpenAPIGolden(t *testing.T) {
	t.Parallel()

	want, err := os.ReadFile(specFile)
	require.NoError(t, err, "сгенерируй спецификацию: make openapi")
	assert.Equal(t, string(want), string(specYAML(t)),
		"%s не совпадает с кодом, выполни make openapi и закоммить файл", specFile)
}

// BR-5.6: сборка детерминирована, иначе golden-тест будет мигать.
func TestOpenAPIDeterministic(t *testing.T) {
	t.Parallel()

	first, second := specYAML(t), specYAML(t)
	assert.Equal(t, string(first), string(second), "two builds differ")
}

// BR-5.1, BR-5.2: версия, общая информация, схема авторизации X-API-Key.
func TestOpenAPIDocument(t *testing.T) {
	t.Parallel()

	spec := buildSpec(t)
	assert.True(t, strings.HasPrefix(spec.OpenAPI, "3.1"), "openapi = %q, want 3.1.x", spec.OpenAPI)
	if assert.NotNil(t, spec.Info, "info") {
		assert.NotEmpty(t, spec.Info.Title, "info.title")
		assert.NotEmpty(t, spec.Info.Version, "info.version")
	}

	require.NotNil(t, spec.Components, "components")
	assert.True(t, slices.ContainsFunc(slices.Collect(maps.Values(spec.Components.SecuritySchemes)),
		func(s *huma.SecurityScheme) bool {
			return s.Type == "apiKey" && s.In == "header" && s.Name == "X-API-Key"
		}), "no apiKey security scheme in header X-API-Key")

	if item := spec.Paths["/healthz"]; assert.NotNil(t, item, "path /healthz") {
		assert.NotNil(t, item.Get, "GET /healthz")
	}
}

// BR-5.2, BR-5.7: у каждой операции есть operationId, summary, теги и ошибки,
// и все ответы-ошибки описаны схемой из раздела 6.
func TestOpenAPIOperations(t *testing.T) {
	t.Parallel()

	spec := buildSpec(t)
	require.NotEmpty(t, spec.Paths, "paths")
	for _, path := range slices.Sorted(maps.Keys(spec.Paths)) {
		for method, op := range operations(spec.Paths[path]) {
			name := op.OperationID
			if name == "" {
				name = method + " " + path
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				assert.NotEmpty(t, op.OperationID, "operationId")
				assert.NotEmpty(t, op.Summary, "summary")
				assert.NotEmpty(t, op.Tags, "tags")
				checkErrorResponses(t, spec, op)
			})
		}
	}
}

// operations возвращает операции пути по методам.
func operations(item *huma.PathItem) map[string]*huma.Operation {
	all := map[string]*huma.Operation{
		http.MethodGet: item.Get, http.MethodPut: item.Put, http.MethodPost: item.Post,
		http.MethodDelete: item.Delete, http.MethodPatch: item.Patch, http.MethodHead: item.Head,
		http.MethodOptions: item.Options, http.MethodTrace: item.Trace,
	}
	for method, op := range all {
		if op == nil {
			delete(all, method)
		}
	}
	return all
}

// checkErrorResponses: у операции есть хотя бы один ответ-ошибка, и каждый -
// application/problem+json со схемой из раздела 6.
func checkErrorResponses(t *testing.T, spec *huma.OpenAPI, op *huma.Operation) {
	t.Helper()
	var errorsFound int
	for status, resp := range op.Responses {
		if status != "default" && status < "400" {
			continue
		}
		errorsFound++
		media := resp.Content["application/problem+json"]
		if assert.NotNil(t, media, "response %s: application/problem+json", status) &&
			assert.NotNil(t, media.Schema, "response %s: schema", status) {
			checkProblemSchema(t, spec, status, media.Schema)
		}
	}
	assert.Positive(t, errorsFound, "no error responses described")
}

// checkProblemSchema: схема ошибки содержит поля раздела 6, а errors[] - field и reason.
func checkProblemSchema(t *testing.T, spec *huma.OpenAPI, status string, schema *huma.Schema) {
	t.Helper()
	s := resolve(spec, schema)
	for _, prop := range []string{"type", "title", "status", "detail", "code", "request_id", "errors"} {
		assert.Contains(t, s.Properties, prop, "response %s: error schema property", status)
	}
	errs := s.Properties["errors"]
	if errs == nil || !assert.NotNil(t, errs.Items, "response %s: errors[] items", status) {
		return
	}
	item := resolve(spec, errs.Items)
	assert.ElementsMatch(t, []string{"field", "reason"}, slices.Collect(maps.Keys(item.Properties)),
		"response %s: errors[] properties", status)
}

// resolve раскрывает $ref на схему из components.
func resolve(spec *huma.OpenAPI, s *huma.Schema) *huma.Schema {
	if s.Ref == "" {
		return s
	}
	return spec.Components.Schemas.SchemaFromRef(s.Ref)
}
