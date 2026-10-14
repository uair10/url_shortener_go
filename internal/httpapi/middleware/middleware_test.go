package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/uair10/url_shortener_go/internal/infrastructure/logging"
)

// Без Unwrap у responseRecorder ResponseController не найдёт Flush и вернёт ошибку.
func TestResponseRecorderUnwrap(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	if err := http.NewResponseController(newResponseRecorder(rec)).Flush(); err != nil {
		t.Fatalf("Flush() = %v, want nil", err)
	}
	if !rec.Flushed {
		t.Error("underlying writer was not flushed")
	}
}

func TestResponseRecorderKeepsFirstStatus(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	w := newResponseRecorder(rec)
	w.WriteHeader(http.StatusCreated)
	w.WriteHeader(http.StatusInternalServerError)

	if w.status != http.StatusCreated || rec.Code != http.StatusCreated {
		t.Errorf("status = %d, sent = %d, want %d", w.status, rec.Code, http.StatusCreated)
	}
}

func TestRecoverRepanicsAbortHandler(t *testing.T) {
	t.Parallel()

	h := Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		if err, _ := recover().(error); !errors.Is(err, http.ErrAbortHandler) {
			t.Errorf("recover() = %v, want http.ErrAbortHandler", err)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))
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

	defer func() {
		if err, _ := recover().(error); !errors.Is(err, http.ErrAbortHandler) {
			t.Errorf("recover() = %v, want http.ErrAbortHandler", err)
		}
		if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
			t.Errorf("response = %d %q, want 200 without body", rec.Code, rec.Body.String())
		}

		var found bool
		for line := range bytes.Lines(buf.Bytes()) {
			var entry struct {
				Msg     string `json:"msg"`
				Status  int    `json:"status"`
				Aborted bool   `json:"aborted"`
			}
			if json.Unmarshal(line, &entry) != nil || entry.Msg != "request" {
				continue
			}
			found = true
			if entry.Status != http.StatusOK || !entry.Aborted {
				t.Errorf("access log status = %d aborted = %v, want 200 true", entry.Status, entry.Aborted)
			}
		}
		if !found {
			t.Errorf("no access log line:\n%s", buf.String())
		}
	}()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/", http.NoBody))
}
