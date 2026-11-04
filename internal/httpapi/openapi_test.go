package httpapi

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
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
	if err != nil {
		t.Fatalf("spec to YAML: %v", err)
	}
	return b
}

// BR-5.3: /openapi.yaml и /docs есть, только если DOCS_ENABLED=true.
func TestDocsRoutes(t *testing.T) {
	t.Parallel()

	tests := []docsCase{
		{"/openapi.yaml", "yaml", "openapi: 3.1"},
		{"/docs", "text/html", "/openapi"},
	}

	for _, enabled := range []bool{true, false} {
		cfg := testConfig()
		cfg.DocsEnabled = enabled
		handler := newHandler(slog.New(slog.DiscardHandler), cfg)
		state := "выключено"
		if enabled {
			state = "включено"
		}

		for _, tt := range tests {
			t.Run(tt.path+" "+state, func(t *testing.T) {
				t.Parallel()

				rec := serve(t, handler, http.MethodGet, tt.path, "", "")
				if !enabled {
					checkProblem(t, rec, http.StatusNotFound, "not_found")
					return
				}
				checkDocsResponse(t, rec, tt)
			})
		}
	}
}

type docsCase struct {
	path   string
	wantCT string
	want   string // подстрока тела
}

func checkDocsResponse(t *testing.T, rec *httptest.ResponseRecorder, tt docsCase) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, tt.wantCT) {
		t.Errorf("Content-Type = %q, want %q", ct, tt.wantCT)
	}
	if !strings.Contains(rec.Body.String(), tt.want) {
		t.Errorf("body has no %q", tt.want)
	}
}

// BR-5.6: api/openapi.yaml совпадает со спецификацией, построенной из кода.
func TestOpenAPIGolden(t *testing.T) {
	t.Parallel()

	want, err := os.ReadFile(specFile)
	if err != nil {
		t.Fatalf("read %s: %v (сгенерируй: make openapi)", specFile, err)
	}
	got := specYAML(t)
	if bytes.Equal(got, want) {
		return
	}
	t.Errorf("%s не совпадает с кодом, выполни make openapi и закоммить файл\n%s",
		specFile, firstDiff(string(want), string(got)))
}

// BR-5.6: сборка детерминирована, иначе golden-тест будет мигать.
func TestOpenAPIDeterministic(t *testing.T) {
	t.Parallel()

	if a, b := specYAML(t), specYAML(t); !bytes.Equal(a, b) {
		t.Errorf("two builds differ:\n%s", firstDiff(string(a), string(b)))
	}
}

// firstDiff показывает первую отличающуюся строку.
func firstDiff(want, got string) string {
	wantLines, gotLines := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := range max(len(wantLines), len(gotLines)) {
		var w, g string
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g {
			return fmt.Sprintf("line %d:\n  file: %s\n  code: %s", i+1, w, g)
		}
	}
	return ""
}

// BR-5.1, BR-5.2: версия, общая информация, схема авторизации X-API-Key.
func TestOpenAPIDocument(t *testing.T) {
	t.Parallel()

	spec := buildSpec(t)
	if !strings.HasPrefix(spec.OpenAPI, "3.1") {
		t.Errorf("openapi = %q, want 3.1.x", spec.OpenAPI)
	}
	if spec.Info == nil || spec.Info.Title == "" || spec.Info.Version == "" {
		t.Errorf("info = %+v, want title and version", spec.Info)
	}

	var apiKey bool
	if spec.Components != nil {
		for _, s := range spec.Components.SecuritySchemes {
			if s.Type == "apiKey" && s.In == "header" && s.Name == "X-API-Key" {
				apiKey = true
			}
		}
	}
	if !apiKey {
		t.Error("no apiKey security scheme in header X-API-Key")
	}

	if item := spec.Paths["/healthz"]; item == nil || item.Get == nil {
		t.Error("GET /healthz is not described")
	}
}

// BR-5.2, BR-5.7: у каждой операции есть operationId, summary, теги и ошибки,
// и все ответы-ошибки описаны схемой из раздела 6.
func TestOpenAPIOperations(t *testing.T) {
	t.Parallel()

	spec := buildSpec(t)
	if len(spec.Paths) == 0 {
		t.Fatal("no paths in spec")
	}
	for path, item := range spec.Paths {
		for method, op := range operations(item) {
			name := method + " " + path
			if op.OperationID == "" || op.Summary == "" || len(op.Tags) == 0 {
				t.Errorf("%s: operationId=%q summary=%q tags=%v, want all set", name, op.OperationID, op.Summary, op.Tags)
			}
			checkErrorResponses(t, spec, name, op)
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
func checkErrorResponses(t *testing.T, spec *huma.OpenAPI, name string, op *huma.Operation) {
	t.Helper()
	var errorsFound int
	for status, resp := range op.Responses {
		if status != "default" && status < "400" {
			continue
		}
		errorsFound++
		media := resp.Content["application/problem+json"]
		if media == nil || media.Schema == nil {
			t.Errorf("%s %s: no application/problem+json schema", name, status)
			continue
		}
		checkProblemSchema(t, spec, name+" "+status, media.Schema)
	}
	if errorsFound == 0 {
		t.Errorf("%s: no error responses described", name)
	}
}

// checkProblemSchema: схема ошибки содержит поля раздела 6, а errors[] - field и reason.
func checkProblemSchema(t *testing.T, spec *huma.OpenAPI, name string, schema *huma.Schema) {
	t.Helper()
	s := resolve(spec, schema)
	for _, prop := range []string{"type", "title", "status", "detail", "code", "request_id", "errors"} {
		if s.Properties[prop] == nil {
			t.Errorf("%s: error schema has no %q", name, prop)
		}
	}
	errs := s.Properties["errors"]
	if errs == nil || errs.Items == nil {
		return
	}
	item := resolve(spec, errs.Items)
	props := make([]string, 0, len(item.Properties))
	for p := range item.Properties {
		props = append(props, p)
	}
	slices.Sort(props)
	if !slices.Equal(props, []string{"field", "reason"}) {
		t.Errorf("%s: errors[] properties = %v, want [field reason]", name, props)
	}
}

// resolve раскрывает $ref на схему из components.
func resolve(spec *huma.OpenAPI, s *huma.Schema) *huma.Schema {
	if s.Ref == "" {
		return s
	}
	return spec.Components.Schemas.SchemaFromRef(s.Ref)
}
