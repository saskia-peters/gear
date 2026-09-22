package router

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/saskia-peters/gear/internal/platform/httpapi"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func assertJSONEnvelope(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d", rec.Code, wantStatus)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want JSON", ct)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body is not the JSON error envelope (%v), got plain text %q", err, rec.Body.String())
	}
	if env.Error.Code != wantCode {
		t.Errorf("code = %q, want %q", env.Error.Code, wantCode)
	}
}

func TestRouterHealthzUp(t *testing.T) {
	r := New(fakePinger{}, discardLogger())

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRouterHealthzDownReturnsEnvelope(t *testing.T) {
	r := New(fakePinger{err: errors.New("connection refused")}, discardLogger())

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	assertJSONEnvelope(t, rec, http.StatusServiceUnavailable, "db_unavailable")
}

func TestRouterUnknownRouteReturnsJSON404(t *testing.T) {
	r := New(fakePinger{}, discardLogger())

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/no/such/route", nil))

	assertJSONEnvelope(t, rec, http.StatusNotFound, "not_found")
}

func TestRouterWrongMethodReturnsJSON405(t *testing.T) {
	r := New(fakePinger{}, discardLogger())

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))

	assertJSONEnvelope(t, rec, http.StatusMethodNotAllowed, "method_not_allowed")
}

func TestRouterWithAuth(t *testing.T) {
	authRouter := chi.NewRouter()
	authRouter.Post("/register", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"ok"}`))
	})

	r := New(fakePinger{}, discardLogger(), WithAuth(authRouter))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
