package problem

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Каждый код собирается в проблему со своим статусом и code.
func TestNewAllCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code   Code
		status int
	}{
		{CodeMalformedJSON, http.StatusBadRequest},
		{CodeValidationFailed, http.StatusUnprocessableEntity},
		{CodeUnauthorized, http.StatusUnauthorized},
		{CodeForbidden, http.StatusForbidden},
		{CodeQuotaExceeded, http.StatusForbidden},
		{CodeNotFound, http.StatusNotFound},
		{CodeMethodNotAllowed, http.StatusMethodNotAllowed},
		{CodeAliasTaken, http.StatusConflict},
		{CodeEmailTaken, http.StatusConflict},
		{CodeLinkGone, http.StatusGone},
		{CodePayloadTooLarge, http.StatusRequestEntityTooLarge},
		{CodeUnsupportedMediaType, http.StatusUnsupportedMediaType},
		{CodeRateLimited, http.StatusTooManyRequests},
		{CodeInternal, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		p := New(tt.code, "")
		if p.Code != string(tt.code) || p.Status != tt.status || p.Title == "" || p.Type != tt.code.TypeURI() {
			t.Errorf("New(%q) = %+v, want status %d", tt.code, p, tt.status)
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
