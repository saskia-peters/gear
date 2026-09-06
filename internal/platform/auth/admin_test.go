package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/saskia-peters/gear/internal/platform/httpapi"
	"github.com/saskia-peters/gear/internal/user/core"
)

// newAdminSurface mirrors internal/user/adapters/http.AdminRoutes: the minimal
// admin-status surface that lives behind the /api/v1/admin gateway. The auth
// package test cannot import the http adapter (import cycle), so it pins the
// same handler shape here.
func newAdminSurface() http.Handler {
	r := chi.NewRouter()
	r.NotFound(httpapi.NotFoundHandler())
	r.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		httpapi.WriteJSON(w, http.StatusOK, map[string]string{"module": "admin", "status": "ok"})
	})
	return r
}

// newAdminRouter builds the admin-module route group exactly as the composition
// root does: the admin-status surface mounted behind RequireAdminPermission with
// the admin-only permission code (protectedCode = admin.recovery.approve). log
// is the denial-specific structured logger (nil disables the extra denial log).
func newAdminRouter(v SessionValidator, r PermissionResolver, log *slog.Logger) http.Handler {
	return RequireAdminPermission(v, r, protectedCode, log)(newAdminSurface())
}

// doAdminRequest hits GET / on the admin-route group (the path chi serves for
// /api/v1/admin after prefix-stripping), with an optional bearer token.
func doAdminRequest(h http.Handler, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAdminRouteAdminAllowed(t *testing.T) {
	v := &mockValidator{session: &core.Session{User: activeUser()}}
	r := &mockResolver{perms: []string{protectedCode}}
	h := newAdminRouter(v, r, nil)

	rec := doAdminRequest(h, "valid-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var wire map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &wire)
	if wire["module"] != "admin" || wire["status"] != "ok" {
		t.Errorf("admin status payload = %v, want module=admin status=ok", wire)
	}
}

func TestAdminRouteNonAdminForbiddenHiddenExistence(t *testing.T) {
	v := &mockValidator{session: &core.Session{User: activeUser()}}
	r := &mockResolver{perms: []string{"some.other.perm"}}
	h := newAdminRouter(v, r, nil)

	rec := doAdminRequest(h, "valid-token")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding forbidden envelope failed: %v", err)
	}
	if env.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", env.Error.Code)
	}
	if env.Error.Message != "Keine Berechtigung." {
		t.Errorf("message = %q, want the generic forbidden message", env.Error.Message)
	}
	// FR-19 anti-enumeration: the 403 body must carry no hint that an admin
	// module exists.
	if strings.Contains(strings.ToLower(rec.Body.String()), "admin") {
		t.Errorf("403 body hints at the admin module: %s", rec.Body.String())
	}
	// The admin-route 403 is byte-identical to any other forbidden route (the
	// generic protected demo route): hidden existence end-to-end.
	protectedRec := doRequest(newProtectedRouter(v, r), "valid-token")
	if protectedRec.Code != http.StatusForbidden {
		t.Fatalf("protected route status = %d, want 403", protectedRec.Code)
	}
	if protectedRec.Body.String() != rec.Body.String() {
		t.Errorf("admin 403 body differs from the generic forbidden body\nadmin:     %s\ngeneric:   %s",
			rec.Body.String(), protectedRec.Body.String())
	}
}

func TestAdminRouteUnauthenticated(t *testing.T) {
	v := &mockValidator{}
	r := &mockResolver{perms: []string{protectedCode}}

	// No token at all.
	h := newAdminRouter(v, r, nil)
	if rec := doAdminRequest(h, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}

	// Invalid/unknown token: 401, no admin-existence hint.
	v2 := &mockValidator{err: core.ErrSessionNotFound}
	h2 := newAdminRouter(v2, r, nil)
	rec := doAdminRequest(h2, "garbage-token")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("invalid token: status = %d, want 401", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error.Code != "unauthorized" {
		t.Errorf("code = %q, want unauthorized", env.Error.Code)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "admin") {
		t.Errorf("401 body hints at the admin module: %s", rec.Body.String())
	}
}

func TestAdminRouteNestedSubpathHidden(t *testing.T) {
	// FR-19 hidden existence on ANY admin sub-path (review finding 2.1-8b): the
	// gateway runs before the router, so a non-admin hitting an admin sub-path
	// gets the same 403 as the root — never a 404 that would hint at the admin
	// surface.
	v := &mockValidator{session: &core.Session{User: activeUser()}}
	r := &mockResolver{perms: []string{"some.other.perm"}}
	h := newAdminRouter(v, r, nil)

	req := httptest.NewRequest(http.MethodGet, "/recovery/pending", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "admin") {
		t.Errorf("403 body hints at the admin module: %s", rec.Body.String())
	}
}

func TestAdminRoutePermissionRevokedImmediate(t *testing.T) {
	// Live re-resolution (AD-2/FR-21): the resolver's permission set is read on
	// EVERY request — never a cached snapshot — so revoking the admin role must
	// deny the very next request with the same session token.
	v := &mockValidator{session: &core.Session{User: activeUser()}}
	r := &mockResolver{perms: []string{protectedCode}}
	h := newAdminRouter(v, r, nil)

	if rec := doAdminRequest(h, "valid-token"); rec.Code != http.StatusOK {
		t.Fatalf("admin before revocation: status = %d, want 200", rec.Code)
	}

	r.perms = []string{}
	rec := doAdminRequest(h, "valid-token")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin after revocation: status = %d, want 403", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", env.Error.Code)
	}
}