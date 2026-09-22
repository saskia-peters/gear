package spa

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFixture builds a temp SPA dir with an index.html, a hashed asset and a
// plain asset, returning the dir + a cleanup func.
func writeFixture(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	must := func(rel, content string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("index.html", "<!doctype html><html><body>GEAR SPA</body></html>")
	must(filepath.Join("assets", "app-0123456789abcdef.js"), "console.log('hashed')")
	must("favicon.ico", "icon")
	return dir, func() {}
}

func newTestHandler(t *testing.T, dir string) *Handler {
	t.Helper()
	return New(dir, slog.New(slog.NewTextHandler(os.Stderr, nil)))
}

func TestSPAServesIndex(t *testing.T) {
	// SPA_ROOT: GET / → index.html, Cache-Control: no-store.
	dir, _ := writeFixture(t)
	h := newTestHandler(t, dir)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "GEAR SPA") {
		t.Errorf("body = %q, want the SPA index", rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestSPAFallbackClientRoute(t *testing.T) {
	// SPA_FALLBACK: GET /tools/xyz (a client route) → index.html (200).
	dir, _ := writeFixture(t)
	h := newTestHandler(t, dir)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tools/xyz", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "GEAR SPA") {
		t.Errorf("body = %q, want the SPA index fallback", rec.Body.String())
	}
}

func TestSPAHashedAssetImmutable(t *testing.T) {
	// ASSET_HASHED: a Vite content-hashed asset → immutable cache header.
	// Vite hashes are base62-ish (e.g. index-CNRF9D2C.js), NOT hex.
	dir, _ := writeFixture(t)
	h := newTestHandler(t, dir)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/app-0123456789abcdef.js", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q, want immutable", got)
	}
	if !strings.Contains(rec.Body.String(), "console.log") {
		t.Errorf("body = %q, want the hashed asset content", rec.Body.String())
	}
}

func TestSPAHashedAssetNonHexViteHash(t *testing.T) {
	// ASSET_HASHED (real Vite shape): index-CNRF9D2C.js (base62 hash, not hex)
	// under /assets → immutable.
	dir, _ := writeFixture(t)
	full := filepath.Join(dir, "assets", "index-CNRF9D2C.js")
	if err := os.WriteFile(full, []byte("//vite"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newTestHandler(t, dir)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/index-CNRF9D2C.js", nil)
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q, want immutable for a Vite hash", got)
	}
}

func TestSPAApiPathIs404(t *testing.T) {
	// API_UNKNOWN: an /api path must NOT be served by the SPA — the JSON 404
	// envelope answers it (uniform with the router). The SPA never swallows an
	// API path.
	dir, _ := writeFixture(t)
	h := newTestHandler(t, dir)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nonexistent", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "GEAR SPA") {
		t.Errorf("body = %q, an /api path must never render the SPA", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not_found") {
		t.Errorf("body = %q, want the JSON 404 envelope", rec.Body.String())
	}
}

func TestSPAMissingBuildIs404(t *testing.T) {
	// Missing web/dist (dev without Vite): the handler answers a clear 404,
	// never a panic.
	h := newTestHandler(t, filepath.Join(t.TempDir(), "does-not-exist"))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestSPAPathTraversalBlocked(t *testing.T) {
	// A traversal attempt must never escape the SPA dir. The encoded form is
	// rejected by the HTTP parser (400) before the handler; use a traversal
	// that reaches the handler to assert the guard.
	dir, _ := writeFixture(t)
	h := newTestHandler(t, dir)
	for _, path := range []string{"/../etc/passwd", "/assets/../../index.html"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "root:") {
			t.Fatalf("path %q: served content outside the SPA dir", path)
		}
	}
}
