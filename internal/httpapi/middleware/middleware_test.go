package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uair10/url_shortener_go/internal/infrastructure/logging"
)

// Без Unwrap у responseRecorder ResponseController не найдёт Flush и вернёт ошибку.
func TestResponseRecorderUnwrap(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	require.NoError(t, http.NewResponseController(newResponseRecorder(rec)).Flush(), "Flush()")
	assert.True(t, rec.Flushed, "underlying writer was not flushed")
}

func TestResponseRecorderKeepsFirstStatus(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	w := newResponseRecorder(rec)
	w.WriteHeader(http.StatusCreated)
	w.WriteHeader(http.StatusInternalServerError)

	assert.Equal(t, http.StatusCreated, w.status, "recorded status")
	assert.Equal(t, http.StatusCreated, rec.Code, "sent status")
}

func TestRecoverRepanicsAbortHandler(t *testing.T) {
	t.Parallel()

	h := Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	assert.PanicsWithError(t, http.ErrAbortHandler.Error(), func() {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))
	})
}

// Паника после начала ответа: 500 уже не отправить, соединение обрывается,
// в access log - статус, который клиент реально получил.
func TestPanicAfterWriteAborts(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ctx := logging.WithLogger(t.Context(), slog.New(slog.NewJSONHandler(&buf, nil)))
	h := AccessLog(Recover(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		panic("boom")
	})))
	rec := httptest.NewRecorder()

	assert.PanicsWithError(t, http.ErrAbortHandler.Error(), func() {
		h.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/", http.NoBody))
	})
	assert.Equal(t, http.StatusOK, rec.Code, "sent status")
	assert.Empty(t, rec.Body.String(), "sent body")

	type accessEntry struct {
		Msg     string `json:"msg"`
		Status  int    `json:"status"`
		Aborted bool   `json:"aborted"`
	}
	var access []accessEntry
	for line := range bytes.Lines(buf.Bytes()) {
		var e accessEntry
		require.NoError(t, json.Unmarshal(line, &e), "log line is not JSON: %s", line)
		if e.Msg == "request" {
			access = append(access, e)
		}
	}
	assert.Equal(t, []accessEntry{{Msg: "request", Status: http.StatusOK, Aborted: true}}, access,
		"access log lines, log:\n%s", buf.String())
}
