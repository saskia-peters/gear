package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	"github.com/saskia-peters/gear/internal/user/core"
)

const adminModulePermission = "admin.recovery.approve"

type adminValidator struct {
	session *core.Session
	err     error
}

func (v *adminValidator) Validate(_ context.Context, _ string) (*core.Session, error) {
	if v.err != nil {
		return nil, v.err
	}
	return v.session, nil
}

type adminResolver struct {
	perms []string
}

func (r *adminResolver) ListPermissionsByUser(_ context.Context, _ string) ([]string, error) {
	return r.perms, nil
}

// newAdminStatusRouter mounts the REAL AdminRoutes() ungated (the gateway is
// applied at the composition-root mount point). The status/404/405 routes need
// no service, so a nil service is safe here.
func newAdminStatusRouter(t *testing.T) http.Handler {
	t.Helper()
	h := newTestHandler(nil, &stubValidator{})
	return h.AdminRoutes()
}

func TestAdminRoutesStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	newAdminStatusRouter(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var wire map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatalf("decoding status failed: %v", err)
	}
	if wire["module"] != "admin" || wire["status"] != "ok" {
		t.Errorf("status payload = %v, want module=admin status=ok", wire)
	}
}

func TestAdminRoutesNotFoundEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	newAdminStatusRouter(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/unknown", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 404 envelope failed: %v", err)
	}
	if env.Error.Code != "not_found" {
		t.Errorf("code = %q, want not_found", env.Error.Code)
	}
}

func TestAdminRoutesMethodNotAllowedEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	newAdminStatusRouter(t).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 405 envelope failed: %v", err)
	}
	if env.Error.Code != "method_not_allowed" {
		t.Errorf("code = %q, want method_not_allowed", env.Error.Code)
	}
}

// TestAdminRoutesGatedBehindRequireAdminPermission pins the composition-root
// seam (review finding 2.1-4): the REAL AdminRoutes() mounted behind
// RequireAdminPermission with an admin-only permission answers 401 (unauth),
// 403 (non-admin, hidden existence) and 200 (admin). This package can import
// both the real handler and the auth gateway without an import cycle, so it is
// the natural home for the "real handler behind the gateway" seam test.
func TestAdminRoutesGatedBehindRequireAdminPermission(t *testing.T) {
	h := newTestHandler(nil, &stubValidator{})

	// 401: no token.
	surface := auth.RequireAdminPermission(
		&adminValidator{}, &adminResolver{perms: []string{adminModulePermission}},
		adminModulePermission, discardLogger(),
	)(h.AdminRoutes())
	if rec := doAdminStatusRequest(surface, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}

	// 403: authenticated non-admin — hidden existence, no admin hint.
	nonAdmin := auth.RequireAdminPermission(
		&adminValidator{session: &core.Session{User: &core.User{ID: "u-vol", Email: "vol@gear.local", State: core.StateActive}}},
		&adminResolver{perms: []string{"some.other.perm"}},
		adminModulePermission, discardLogger(),
	)(h.AdminRoutes())
	rec := doAdminStatusRequest(nonAdmin, "valid-token")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "admin") {
		t.Errorf("403 body hints at the admin module: %s", rec.Body.String())
	}

	// 200: authenticated admin reaches the real status surface.
	admin := auth.RequireAdminPermission(
		&adminValidator{session: &core.Session{User: &core.User{ID: "u-admin", Email: "admin@gear.local", State: core.StateActive}}},
		&adminResolver{perms: []string{adminModulePermission}},
		adminModulePermission, discardLogger(),
	)(h.AdminRoutes())
	rec = doAdminStatusRequest(admin, "valid-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

func doAdminStatusRequest(h http.Handler, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}