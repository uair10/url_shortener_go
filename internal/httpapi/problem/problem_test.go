package problem

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Каждый код из таблицы собирается в проблему с его статусом и code.
func TestNewAllCodes(t *testing.T) {
	t.Parallel()

	for code, info := range codes {
		p := New(code, "")
		if p.Code != string(code) || p.Status != info.status || p.Title == "" || p.Type != code.TypeURI() {
			t.Errorf("New(%q) = %+v", code, p)
		}
	}
}

func TestNewUnknownCodePanics(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("New with unknown code did not panic")
		}
	}()
	_ = New("no_such_code", "")
}

func TestError(t *testing.T) {
	t.Parallel()

	if got := AliasTaken("").Error(); got != "alias_taken" {
		t.Errorf("Error() = %q, want %q", got, "alias_taken")
	}
	if got := NotFound("link abc").Error(); got != "not_found: link abc" {
		t.Errorf("Error() = %q, want %q", got, "not_found: link abc")
	}
}

// detail 5xx не уходит клиенту, даже если проблему собрали в обход Internal().
func TestWriteHidesDetailFor5xx(t *testing.T) {
	t.Parallel()

	p := New(CodeInternal, "pq: password authentication failed")
	rec := httptest.NewRecorder()
	p.Write(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if _, ok := body["detail"]; ok {
		t.Errorf("5xx body has detail: %s", rec.Body.String())
	}
	if p.Detail == "" {
		t.Error("Write changed p")
	}
}
