package problem

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		t.Run(string(tt.code), func(t *testing.T) {
			t.Parallel()

			p := New(tt.code, "")
			assert.Equal(t, string(tt.code), p.Code, "Code")
			assert.Equal(t, tt.status, p.Status, "Status")
			assert.NotEmpty(t, p.Title, "Title")
			assert.Equal(t, tt.code.TypeURI(), p.Type, "Type")
		})
	}
}

func TestNewUnknownCodePanics(t *testing.T) {
	t.Parallel()

	assert.Panics(t, func() { _ = New("no_such_code", "") })
}

func TestError(t *testing.T) {
	t.Parallel()

	require.EqualError(t, AliasTaken(""), "alias_taken")
	require.EqualError(t, NotFound("link abc"), "not_found: link abc")
}

// detail 5xx не уходит клиенту, даже если проблему собрали в обход Internal().
func TestWriteHidesDetailFor5xx(t *testing.T) {
	t.Parallel()

	p := New(CodeInternal, "pq: password authentication failed")
	rec := httptest.NewRecorder()
	p.Write(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body is not JSON: %s", rec.Body.String())
	assert.NotContains(t, body, "detail", "5xx body has detail")
	assert.NotEmpty(t, p.Detail, "Write changed p")
}
