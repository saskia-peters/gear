package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusOK, map[string]string{"status": "ok"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q, want application/json", ct)
	}
	if !json.Valid(rec.Body.Bytes()) {
		t.Errorf("body %q is not valid JSON", rec.Body.String())
	}
}

func TestWriteErrorEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusForbidden, "forbidden", "missing permission")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	var env ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", env.Error.Code)
	}
	if env.Error.Message != "missing permission" {
		t.Errorf("message = %q, want missing permission", env.Error.Message)
	}
	if env.Error.Details != nil {
		t.Errorf("details = %v, want nil when not provided", env.Error.Details)
	}
}

func TestWriteErrorEnvelopeWithDetails(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteErrorDetail(rec, http.StatusServiceUnavailable, "db_unavailable", "database unreachable", map[string]string{"cause": "connection refused"})

	var env ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	details, ok := env.Error.Details.(map[string]any)
	if !ok {
		t.Fatalf("details = %#v, want object", env.Error.Details)
	}
	if details["cause"] != "connection refused" {
		t.Errorf("details.cause = %v, want connection refused", details["cause"])
	}
}
