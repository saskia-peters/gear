package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/saskia-peters/gear/internal/platform/httpapi"
)

func TestRequestLogger(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	h := RequestLogger(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("log output is not valid JSON: %v\n%s", err, buf.String())
	}
	if got["method"] != "GET" {
		t.Errorf("method = %v, want GET", got["method"])
	}
	if got["path"] != "/healthz" {
		t.Errorf("path = %v, want /healthz", got["path"])
	}
	if status := got["status"]; status != float64(http.StatusTeapot) {
		t.Errorf("status = %v, want %d", status, http.StatusTeapot)
	}
	if _, ok := got["duration_ms"]; !ok {
		t.Error("duration_ms missing from log line")
	}
}

func TestRecoveryReturnsJSONEnvelope(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	h := Recovery(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("response is not the JSON error envelope: %v\n%s", err, rec.Body.String())
	}
	if env.Error.Code != "internal_error" {
		t.Errorf("code = %q, want internal_error", env.Error.Code)
	}
}

// deadlineWriter is a minimal ResponseWriter whose SetReadDeadline records that
// http.ResponseController unwrapped the middlewares to reach it.
type deadlineWriter struct {
	*httptest.ResponseRecorder
	deadlineSet bool
}

func (w *deadlineWriter) SetReadDeadline(time.Time) error {
	w.deadlineSet = true
	return nil
}

func TestStatusWriterUnwraps(t *testing.T) {
	under := &deadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	h := RequestLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// The controller must reach through statusWriter to deadlineWriter.
			// Without Unwrap it would report ErrNotSupported and the deadline
			// would never be recorded.
			if err := http.NewResponseController(w).SetReadDeadline(time.Time{}); err != nil {
				t.Errorf("SetReadDeadline through statusWriter: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		}),
	)

	h.ServeHTTP(under, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if !under.deadlineSet {
		t.Error("ResponseController could not reach the underlying ResponseWriter through statusWriter")
	}
}

func TestResponseControllerUnwrapsFullMiddlewareChain(t *testing.T) {
	under := &deadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Mount in the production composition root order: Recovery outer, RequestLogger inner
	chain := Recovery(log)(RequestLogger(log)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if err := http.NewResponseController(w).SetReadDeadline(time.Time{}); err != nil {
				t.Errorf("SetReadDeadline through full chain: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		}),
	))

	chain.ServeHTTP(under, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if !under.deadlineSet {
		t.Error("ResponseController could not reach the underlying ResponseWriter through full middleware chain")
	}
}
