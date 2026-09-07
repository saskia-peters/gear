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
	"github.com/saskia-peters/gear/internal/user/ports"
)

// newAdminUsersSurface mounts the REAL AdminRoutes() WITHOUT the outer
// composition-root gateway — the users sub-mount carries its OWN
// `users.approve` RequireAdminPermission gate (Design Notes spec 2.4), which is
// exactly what this suite exercises. The mock service resolves permissions via
// the resolver adapter the same way the real service would.
func newAdminUsersSurface(t *testing.T, svc ports.Service, validator auth.SessionValidator) http.Handler {
	t.Helper()
	h := newTestHandler(svc, validator)
	return h.AdminRoutes()
}

func doAdminUsersGET(h http.Handler, token, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func doAdminUsersPOST(h http.Handler, token, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// pendingUsersSvc returns a mock service that resolves `users.approve` for the
// caller (so the inner gate passes) and returns the given pending list.
func pendingUsersSvc(pending []*core.PendingUser, approveRes *core.UserApprovalResult, rejectRes *core.UserApprovalResult) *mockService {
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{core.UserApprovePermission}, nil
	}
	svc.listPendingFunc = func(_ context.Context, _ *core.User) ([]*core.PendingUser, error) {
		return pending, nil
	}
	if approveRes != nil {
		svc.approveUserFunc = func(_ context.Context, _ *core.User, userID string) (*core.UserApprovalResult, error) {
			if userID == "u-tim" {
				return approveRes, nil
			}
			return nil, core.ErrUserNotPending
		}
	}
	if rejectRes != nil {
		svc.rejectUserFunc = func(_ context.Context, _ *core.User, userID string) (*core.UserApprovalResult, error) {
			if userID == "u-tim" {
				return rejectRes, nil
			}
			return nil, core.ErrUserNotPending
		}
	}
	return svc
}

func TestListPendingUsersOK(t *testing.T) {
	// LIST_PENDING: an admin holding users.approve gets the pending requests
	// oldest-first with the submitted profile details (Vorname, Nachname,
	// E-Mail) plus id and created_at.
	pending := []*core.PendingUser{
		{ID: "u-tim", Vorname: "Tim", Nachname: "Müller", Email: "tim@gear.local"},
		{ID: "u-lena", Vorname: "Lena", Nachname: "Schmidt", Email: "lena@gear.local"},
	}
	svc := pendingUsersSvc(pending, nil, nil)
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsersGET(h, "token", "/users/pending")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var wire struct {
		Users []struct {
			ID       string `json:"id"`
			Vorname  string `json:"vorname"`
			Nachname string `json:"nachname"`
			Email    string `json:"email"`
		} `json:"users"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatalf("decoding response failed: %v", err)
	}
	if len(wire.Users) != 2 {
		t.Fatalf("user count = %d, want 2", len(wire.Users))
	}
	if wire.Users[0].Vorname != "Tim" || wire.Users[0].Nachname != "Müller" || wire.Users[0].Email != "tim@gear.local" {
		t.Errorf("first pending = %+v, want Tim Müller tim@gear.local", wire.Users[0])
	}
}

func TestListPendingUsersEmpty(t *testing.T) {
	// LIST_PENDING_EMPTY: no pending users -> 200 with an EMPTY array so the
	// client can show the "Keine ausstehenden Anträge" empty state (UX-DR6/8).
	svc := pendingUsersSvc(nil, nil, nil)
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsersGET(h, "token", "/users/pending")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var wire struct {
		Users []json.RawMessage `json:"users"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatalf("decoding response failed: %v", err)
	}
	if wire.Users == nil || len(wire.Users) != 0 {
		t.Errorf("users = %v, want a non-null empty array", wire.Users)
	}
}

func TestListPendingUsersForbidden(t *testing.T) {
	// LIST_FORBIDDEN: a caller WITHOUT users.approve is denied by the users
	// sub-mount's own gate with the uniform hidden-existence 403 — no admin
	// hint (FR-19). A tools-only schirrmeister holds no users.* code.
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{"tools.manage", "tool_types.manage"}, nil
	}
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsersGET(h, "token", "/users/pending")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 403 envelope failed: %v", err)
	}
	if env.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", env.Error.Code)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "admin") {
		t.Errorf("403 body hints at the admin module: %s", rec.Body.String())
	}
}

func TestApproveUserOK(t *testing.T) {
	// APPROVE_VALID: approving a pending user returns the German confirmation
	// with the target email.
	approveRes := &core.UserApprovalResult{Message: core.MsgUserApproved, UserID: "u-tim", Email: "tim@gear.local"}
	svc := pendingUsersSvc(nil, approveRes, nil)
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsersPOST(h, "token", "/users/u-tim/approve")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var wire struct {
		Message string `json:"message"`
		Email   string `json:"email"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatalf("decoding approve response failed: %v", err)
	}
	if wire.Message != core.MsgUserApproved || wire.Email != "tim@gear.local" {
		t.Errorf("approve response = %+v, want confirmation for tim", wire)
	}
}

func TestApproveUserNotFound(t *testing.T) {
	// APPROVE_NONPENDING / APPROVE_UNKNOWN: an already-resolved or unknown id
	// maps to the uniform 404 not_found — one message for both, no leak
	// (FR-19). The approveUserFunc returns ErrUserNotPending for unknown ids.
	approveRes := &core.UserApprovalResult{Message: core.MsgUserApproved, UserID: "u-tim", Email: "tim@gear.local"}
	svc := pendingUsersSvc(nil, approveRes, nil)
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsersPOST(h, "token", "/users/u-nope/approve")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 404 envelope failed: %v", err)
	}
	if env.Error.Code != "not_found" {
		t.Errorf("code = %q, want not_found", env.Error.Code)
	}
	if !strings.Contains(env.Error.Message, "nicht mehr ausstehend") {
		t.Errorf("message = %q, want the uniform not-pending microcopy", env.Error.Message)
	}
}

func TestRejectUserOK(t *testing.T) {
	// REJECT_VALID: rejecting a pending user returns the German confirmation.
	rejectRes := &core.UserApprovalResult{Message: core.MsgUserRejected, UserID: "u-tim", Email: "tim@gear.local"}
	svc := pendingUsersSvc(nil, nil, rejectRes)
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsersPOST(h, "token", "/users/u-tim/reject")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var wire struct {
		Message string `json:"message"`
		Email   string `json:"email"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatalf("decoding reject response failed: %v", err)
	}
	if wire.Message != core.MsgUserRejected || wire.Email != "tim@gear.local" {
		t.Errorf("reject response = %+v, want confirmation for tim", wire)
	}
}

func TestRejectUserNotFound(t *testing.T) {
	// REJECT_NONPENDING / REJECT_UNKNOWN: uniform 404, no existence leak.
	rejectRes := &core.UserApprovalResult{Message: core.MsgUserRejected, UserID: "u-tim", Email: "tim@gear.local"}
	svc := pendingUsersSvc(nil, nil, rejectRes)
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsersPOST(h, "token", "/users/u-nope/reject")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestApproveRejectMalformedUUID(t *testing.T) {
	// APPROVE_UNKNOWN / REJECT_UNKNOWN (malformed id): a non-UUID {userID} is
	// treated exactly like an unknown id — the uniform 404 not_found, never a
	// 500 internal_error. The repository maps a UUID parse failure to
	// core.ErrUserNotPending (verified in the postgres integration suite), and
	// the handler maps that sentinel to the uniform 404 (no existence leak).
	// A dummy result wires the mock's approve/reject funcs so any id other than
	// "u-tim" returns ErrUserNotPending.
	approveRes := &core.UserApprovalResult{Message: core.MsgUserApproved, UserID: "u-tim", Email: "tim@gear.local"}
	rejectRes := &core.UserApprovalResult{Message: core.MsgUserRejected, UserID: "u-tim", Email: "tim@gear.local"}
	svc := pendingUsersSvc(nil, approveRes, rejectRes)
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	for _, path := range []string{"/users/not-a-uuid/approve", "/users/not-a-uuid/reject"} {
		rec := doAdminUsersPOST(h, "token", path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("POST %s: status = %d, want 404 (body %s)", path, rec.Code, rec.Body.String())
			continue
		}
		var env httpapi.ErrorEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("decoding 404 envelope failed: %v", err)
		}
		if env.Error.Code != "not_found" {
			t.Errorf("POST %s: code = %q, want not_found", path, env.Error.Code)
		}
	}
}

func TestAdminUsersRoutesUnauthorized(t *testing.T) {
	// A missing token is rejected by the users sub-mount's own gateway with the
	// uniform 401 — even before the service is consulted.
	svc := pendingUsersSvc(nil, nil, nil)
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	for _, path := range []string{"/users/pending", "/users/u-tim/approve", "/users/u-tim/reject"} {
		rec := doAdminUsersGET(h, "", path)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without token: status = %d, want 401 (body %s)", path, rec.Code, rec.Body.String())
		}
	}
}

// TestAdminUsersSubPathHiddenForNonApprove pins the composition-root seam for
// the /users surface (review finding 2.1-4 style): the REAL AdminRoutes mounted
// behind the outer admin-only gateway PLUS its inner users.approve gate answers
// 403 hidden-existence for an admin who does NOT hold users.approve — the
// approval surface is not enumerated (FR-19).
func TestAdminUsersSubPathHiddenForNonApprove(t *testing.T) {
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		// An admin-group member who (hypothetically) lacks users.approve.
		return []string{adminModulePermission}, nil
	}
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsersGET(h, "token", "/users/pending")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "admin") {
		t.Errorf("403 body hints at the admin module: %s", rec.Body.String())
	}
}