package health

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/saskia-peters/gear/internal/platform/httpapi"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

// blockingPinger blocks until the probe's context is cancelled — the driver
// must not wait beyond its own ping timeout (I/O matrix: DB_DOWN must not
// hang).
type blockingPinger struct{}

func (blockingPinger) Ping(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestHealthUp(t *testing.T) {
	h := New(fakePinger{}, discardLogger())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("body status = %q, want ok", body["status"])
	}
}

func TestHealthDown(t *testing.T) {
	const causeErr = "connection refused / host=dbserver.example.com db=gear"
	h := New(fakePinger{err: errors.New(causeErr)}, discardLogger())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Error.Code != "db_unavailable" {
		t.Errorf("code = %q, want db_unavailable", env.Error.Code)
	}
	if env.Error.Details != nil {
		t.Errorf("details = %v, want nil (must not leak the raw probe error)", env.Error.Details)
	}
	if body := rec.Body.String(); strings.Contains(body, "dbserver.example.com") {
		t.Errorf("response leaks sanitized error detail: %q", body)
	}
}

func TestHealthProbeDoesNotHang(t *testing.T) {
	h := New(blockingPinger{}, discardLogger())

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("health probe hung beyond the ping timeout")
	}

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
