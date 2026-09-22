package http

import (
	"bytes"
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

// ============================================================================
// User & Group Administration (Story 2.6) — handler + gate contracts
// ============================================================================

// adminUsersSvc returns a mock service whose caller resolves ALL the
// `users.*` codes PLUS user_groups.manage (so the sub-mount gates pass), with
// the given per-handler behaviour.
func adminUsersSvc(listRes []*core.AdminUserSummary, listErr error, detailFunc func(context.Context, string) (*core.AdminUserDetail, error), createFunc func(context.Context, core.CreateAdminUserInput) (*core.AdminUserWriteResult, error), updateFunc func(context.Context, string, core.UpdateAdminUserInput) (*core.AdminUserWriteResult, error), deactivateFunc func(context.Context, string, bool) (*core.DeactivateUserResult, error)) *mockService {
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{
			core.UserViewPermission, core.UserApprovePermission, core.UserManagePermission,
			core.UserGroupsManagePermission,
		}, nil
	}
	svc.listAdminUsersFunc = func(_ context.Context, _ *core.User) ([]*core.AdminUserSummary, error) {
		return listRes, listErr
	}
	svc.getUserDetailFunc = func(_ context.Context, _ *core.User, userID string) (*core.AdminUserDetail, error) {
		return detailFunc(context.Background(), userID)
	}
	svc.createAdminUserFunc = func(_ context.Context, _ *core.User, in core.CreateAdminUserInput) (*core.AdminUserWriteResult, error) {
		return createFunc(context.Background(), in)
	}
	svc.updateAdminUserFunc = func(_ context.Context, _ *core.User, userID string, in core.UpdateAdminUserInput) (*core.AdminUserWriteResult, error) {
		return updateFunc(context.Background(), userID, in)
	}
	svc.deactivateAdminUserFunc = func(_ context.Context, _ *core.User, userID string, confirmed bool) (*core.DeactivateUserResult, error) {
		return deactivateFunc(context.Background(), userID, confirmed)
	}
	return svc
}

func doAdminUsers(method, path, token string, body []byte, h http.Handler) *httptest.ResponseRecorder {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestListAdminUsersOK(t *testing.T) {
	// LIST_USERS: an admin holding any users.* code gets all users with status.
	svc := adminUsersSvc([]*core.AdminUserSummary{
		{ID: "u-1", Vorname: "Tim", Nachname: "Müller", Email: "tim@gear.local", Status: "active"},
		{ID: "u-2", Vorname: "Lena", Nachname: "Schmidt", Email: "lena@gear.local", Status: "pending_approval"},
	}, nil, nil, nil, nil, nil)
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodGet, "/users", "token", nil, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var wire struct {
		Users []struct {
			ID      string `json:"id"`
			Vorname string `json:"vorname"`
			Status  string `json:"status"`
		} `json:"users"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatalf("decoding list response failed: %v", err)
	}
	if len(wire.Users) != 2 {
		t.Fatalf("user count = %d, want 2", len(wire.Users))
	}
	if wire.Users[0].Vorname != "Tim" || wire.Users[0].Status != "active" {
		t.Errorf("users[0] = %+v, want Tim active", wire.Users[0])
	}
}

func TestListAdminUsersEmptyIsArray(t *testing.T) {
	// LIST_EMPTY: an empty list serializes as a non-null array.
	svc := adminUsersSvc([]*core.AdminUserSummary{}, nil, nil, nil, nil, nil)
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodGet, "/users", "token", nil, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var wire struct {
		Users []json.RawMessage `json:"users"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &wire)
	if wire.Users == nil {
		t.Error("users = null, want a non-null empty array")
	}
}

func TestListAdminUsersForbidden(t *testing.T) {
	// LIST_FORBIDDEN: a caller WITHOUT any users.* code is denied by the users
	// sub-mount's own any-of gate with the uniform hidden-existence 403 (FR-19).
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{"tools.manage", "tool_types.manage"}, nil
	}
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodGet, "/users", "token", nil, h)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", env.Error.Code)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "admin") {
		t.Errorf("403 body hints at the admin module: %s", rec.Body.String())
	}
}

func TestGetAdminUserDetailOK(t *testing.T) {
	// DETAIL_VALID: a user detail carries profile + roles + groups + grants +
	// qualifications.
	svc := adminUsersSvc(nil, nil, func(_ context.Context, _ string) (*core.AdminUserDetail, error) {
		return &core.AdminUserDetail{
			ID: "u-tim", Vorname: "Tim", Nachname: "Müller", Email: "tim@gear.local", Status: "active",
			Roles:          []core.RoleGroupRef{{ID: "g-1", Name: "helfende", IsBaseRole: true}},
			UserGroups:     []core.UserGroupRef{{ID: "ug-1", Name: "Gruppe Ost"}},
			DirectGrants:   []core.DirectGrantRef{{PermissionID: "p-1", Code: "report.export"}},
			Qualifications: []core.QualificationAssignment{{ID: "q-1", Name: "Kettensäge", ExpiryKind: "unlimited", Status: "Unbegrenzt"}},
		}, nil
	}, nil, nil, nil)
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodGet, "/users/u-tim", "token", nil, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var raw struct {
		ID            string            `json:"id"`
		Vorname       string            `json:"vorname"`
		Status        string            `json:"status"`
		Roles         []json.RawMessage `json:"roles"`
		UserGroups    []json.RawMessage `json:"user_groups"`
		DirectGrants  []json.RawMessage `json:"direct_grants"`
		Qualifications []json.RawMessage `json:"qualifications"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decoding detail response failed: %v", err)
	}
	if raw.ID != "u-tim" || raw.Vorname != "Tim" || raw.Status != "active" {
		t.Errorf("detail = %s, want Tim active", rec.Body.String())
	}
	if len(raw.Roles) != 1 || len(raw.UserGroups) != 1 || len(raw.DirectGrants) != 1 || len(raw.Qualifications) != 1 {
		t.Errorf("detail counts = roles %d groups %d grants %d quals %d, want all 1",
			len(raw.Roles), len(raw.UserGroups), len(raw.DirectGrants), len(raw.Qualifications))
	}
}

func TestGetAdminUserDetailNotFound(t *testing.T) {
	// DETAIL_UNKNOWN: an unknown id maps to the uniform 404.
	svc := adminUsersSvc(nil, nil, func(_ context.Context, _ string) (*core.AdminUserDetail, error) {
		return nil, core.ErrAdminUserNotFound
	}, nil, nil, nil)
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodGet, "/users/u-nope", "token", nil, h)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error.Code != "not_found" || !strings.Contains(env.Error.Message, "Benutzer") {
		t.Errorf("404 envelope = %+v, want not_found with German message", env)
	}
}

func TestCreateAdminUserOK(t *testing.T) {
	// CREATE_VALID: creating a user returns 201 with the server message and the
	// stored detail (finding 8: the SPA must use the server message).
	svc := adminUsersSvc(nil, nil, nil, func(_ context.Context, in core.CreateAdminUserInput) (*core.AdminUserWriteResult, error) {
		return &core.AdminUserWriteResult{Message: core.MsgUserCreated, User: &core.AdminUserDetail{ID: "u-tim", Vorname: in.Vorname, Nachname: in.Nachname, Email: in.Email, Status: in.Status}}, nil
	}, nil, nil)
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	body := []byte(`{"vorname":"Tim","nachname":"Müller","email":"tim@gear.local","status":"active","role_ids":["g-1"],"user_group_ids":["ug-1"],"direct_grant_codes":["report.export"]}`)
	rec := doAdminUsers(http.MethodPost, "/users", "token", body, h)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var wire struct {
		Message string `json:"message"`
		User    struct {
			ID      string `json:"id"`
			Vorname string `json:"vorname"`
			Email   string `json:"email"`
			Status  string `json:"status"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatalf("decoding create response failed: %v", err)
	}
	if wire.Message != core.MsgUserCreated {
		t.Errorf("create message = %q, want %q", wire.Message, core.MsgUserCreated)
	}
	if wire.User.ID != "u-tim" || wire.User.Vorname != "Tim" || wire.User.Email != "tim@gear.local" || wire.User.Status != "active" {
		t.Errorf("create response = %+v, want the stored user", wire)
	}
}

func TestCreateAdminUserDupEmail(t *testing.T) {
	// CREATE_DUP_EMAIL: a duplicate email maps to the uniform 409.
	svc := adminUsersSvc(nil, nil, nil, func(_ context.Context, _ core.CreateAdminUserInput) (*core.AdminUserWriteResult, error) {
		return nil, core.ErrAdminUserEmailTaken
	}, nil, nil)
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/users", "token", []byte(`{"vorname":"Tim","nachname":"Müller","email":"tim@gear.local","status":"active"}`), h)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error.Code != "conflict" || !strings.Contains(env.Error.Message, "E-Mail") {
		t.Errorf("409 envelope = %+v, want conflict with German message", env)
	}
}

func TestCreateAdminUserInvalid(t *testing.T) {
	// CREATE invalid input: unknown role and unknown grant code map to 400.
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"unknown role", core.ErrAdminUserUnknownRole},
		{"unknown grant", core.ErrUnknownPermissionCode},
		{"bad name", core.ErrAdminUserInvalidName},
		{"bad email", core.ErrAdminUserInvalidEmail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := adminUsersSvc(nil, nil, nil, func(_ context.Context, _ core.CreateAdminUserInput) (*core.AdminUserWriteResult, error) {
				return nil, tc.err
			}, nil, nil)
			h := newAdminUsersSurface(t, svc, &stubValidator{})
			rec := doAdminUsers(http.MethodPost, "/users", "token", []byte(`{"vorname":"Tim","nachname":"Müller","email":"tim@gear.local","status":"active"}`), h)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCreateAdminUserMalformedJSON(t *testing.T) {
	// CREATE malformed body: an unparseable body maps to 400 invalid_request.
	svc := adminUsersSvc(nil, nil, nil, func(_ context.Context, _ core.CreateAdminUserInput) (*core.AdminUserWriteResult, error) {
		t.Fatal("service must not be called for malformed JSON")
		return nil, nil
	}, nil, nil)
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/users", "token", []byte(`{not json`), h)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestCreateAdminUserForbidden(t *testing.T) {
	// CREATE_FORBIDDEN: a users.view-only holder passes the any-of list gate
	// but the core re-verifies create needs users.manage → uniform 403.
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{core.UserViewPermission}, nil
	}
	svc.createAdminUserFunc = func(_ context.Context, _ *core.User, _ core.CreateAdminUserInput) (*core.AdminUserWriteResult, error) {
		return nil, core.ErrForbidden
	}
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/users", "token", []byte(`{"vorname":"Tim","nachname":"Müller","email":"tim@gear.local","status":"active"}`), h)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestUpdateAdminUserOK(t *testing.T) {
	// EDIT_VALID: editing a user returns 200 with the server message and the
	// replaced detail (finding 8).
	svc := adminUsersSvc(nil, nil, nil, nil, func(_ context.Context, userID string, in core.UpdateAdminUserInput) (*core.AdminUserWriteResult, error) {
		return &core.AdminUserWriteResult{Message: core.MsgUserUpdated, User: &core.AdminUserDetail{ID: userID, Vorname: in.Vorname, Nachname: in.Nachname, Email: in.Email, Status: in.Status}}, nil
	}, nil)
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPut, "/users/u-tim", "token", []byte(`{"vorname":"Tim","nachname":"Müller","email":"tim@gear.local","status":"active","role_ids":["g-2"]}`), h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var wire struct {
		Message string `json:"message"`
		User    struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatalf("decoding update response failed: %v", err)
	}
	if wire.Message != core.MsgUserUpdated {
		t.Errorf("update message = %q, want %q", wire.Message, core.MsgUserUpdated)
	}
	if wire.User.ID != "u-tim" || wire.User.Email != "tim@gear.local" {
		t.Errorf("update response = %+v, want the replaced user", wire)
	}
}

func TestUpdateAdminUserNotFound(t *testing.T) {
	// EDIT_UNKNOWN: an unknown id maps to the uniform 404.
	svc := adminUsersSvc(nil, nil, nil, nil, func(_ context.Context, _ string, _ core.UpdateAdminUserInput) (*core.AdminUserWriteResult, error) {
		return nil, core.ErrAdminUserNotFound
	}, nil)
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPut, "/users/u-nope", "token", []byte(`{"vorname":"A","nachname":"B","email":"a@b.de","status":"active"}`), h)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestUpdateAdminUserDupEmail(t *testing.T) {
	// EDIT_DUP_EMAIL: an email held by another account maps to the uniform 409.
	svc := adminUsersSvc(nil, nil, nil, nil, func(_ context.Context, _ string, _ core.UpdateAdminUserInput) (*core.AdminUserWriteResult, error) {
		return nil, core.ErrAdminUserEmailTaken
	}, nil)
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPut, "/users/u-tim", "token", []byte(`{"vorname":"A","nachname":"B","email":"admin.1@gear.local","status":"active"}`), h)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestDeactivateAdminUserOK(t *testing.T) {
	// DEACTIVATE_VALID: a confirmed deactivation returns 200 with the German
	// confirmation.
	svc := adminUsersSvc(nil, nil, nil, nil, nil, func(_ context.Context, userID string, _ bool) (*core.DeactivateUserResult, error) {
		return &core.DeactivateUserResult{Message: core.MsgUserDeactivated, UserID: userID, Email: "tim@gear.local"}, nil
	})
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/users/u-tim/deactivate", "token", []byte(`{"confirmed":true}`), h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var wire struct {
		Message string `json:"message"`
		Email   string `json:"email"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &wire)
	if wire.Message != core.MsgUserDeactivated || wire.Email != "tim@gear.local" {
		t.Errorf("deactivate response = %+v, want the confirmation", wire)
	}
}

func TestDeactivateAdminUserNotActive(t *testing.T) {
	// DEACTIVATE_NONACTIVE: a pending/deactivated user maps to the uniform 409.
	svc := adminUsersSvc(nil, nil, nil, nil, nil, func(_ context.Context, _ string, _ bool) (*core.DeactivateUserResult, error) {
		return nil, core.ErrUserNotActiveForDeactivate
	})
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/users/u-tim/deactivate", "token", []byte(`{"confirmed":true}`), h)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestDeactivateAdminUserNotFound(t *testing.T) {
	// DEACTIVATE_UNKNOWN: an unknown id maps to the uniform 404.
	svc := adminUsersSvc(nil, nil, nil, nil, nil, func(_ context.Context, _ string, _ bool) (*core.DeactivateUserResult, error) {
		return nil, core.ErrAdminUserNotFound
	})
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/users/u-nope/deactivate", "token", []byte(`{"confirmed":true}`), h)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestDeactivateAdminUserNotConfirmed(t *testing.T) {
	// DEACTIVATE_NOCONFIRM: a missing confirmation maps to the uniform 400.
	svc := adminUsersSvc(nil, nil, nil, nil, nil, func(_ context.Context, _ string, _ bool) (*core.DeactivateUserResult, error) {
		return nil, core.ErrDeactivationNotConfirmed
	})
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/users/u-tim/deactivate", "token", []byte(`{"confirmed":false}`), h)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

// adminUserGroupsSvc returns a mock service whose caller resolves
// user_groups.manage (so the user-groups sub-mount gate passes).
func adminUserGroupsSvc(listRes []*core.UserGroup, createRes *core.UserGroup, assignFunc func(context.Context, *core.User, string, []string) (*core.UserGroup, error)) *mockService {
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{core.UserGroupsManagePermission}, nil
	}
	svc.listUserGroupsFunc = func(_ context.Context, _ *core.User) ([]*core.UserGroup, error) {
		return listRes, nil
	}
	svc.createUserGroupFunc = func(_ context.Context, _ *core.User, in core.CreateUserGroupInput) (*core.UserGroup, error) {
		return createRes, nil
	}
	svc.assignUserGroupFunc = assignFunc
	return svc
}

func TestListAdminUserGroupsOK(t *testing.T) {
	// GROUP_LIST: a user_groups.manage holder lists the teams.
	svc := adminUserGroupsSvc([]*core.UserGroup{{ID: "ug-1", Name: "Gruppe Ost", Description: "Östliche Gruppe"}}, nil, nil)
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodGet, "/user-groups", "token", nil, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var wire struct {
		Groups []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"user_groups"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &wire)
	if len(wire.Groups) != 1 || wire.Groups[0].Name != "Gruppe Ost" {
		t.Errorf("user_groups = %+v, want [Gruppe Ost]", wire.Groups)
	}
}

func TestListAdminUserGroupsForbidden(t *testing.T) {
	// GROUP_FORBIDDEN: a caller WITHOUT user_groups.manage is denied by the
	// user-groups sub-mount's own gate (uniform 403, FR-19).
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{core.UserViewPermission}, nil
	}
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodGet, "/user-groups", "token", nil, h)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestListAdminUserGroupsEmptyIsArray(t *testing.T) {
	// GROUP_LIST_EMPTY (finding 4): an empty store serializes user_groups as a
	// NON-NULL empty array so the client can render the empty state.
	svc := adminUserGroupsSvc([]*core.UserGroup{}, nil, nil)
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodGet, "/user-groups", "token", nil, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var wire struct {
		Groups []json.RawMessage `json:"user_groups"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &wire)
	if wire.Groups == nil {
		t.Error("user_groups = null, want a non-null empty array")
	}
}

func TestCreateAdminUserGroupOK(t *testing.T) {
	// GROUP_CREATE_VALID: creating a team returns 201.
	svc := adminUserGroupsSvc(nil, &core.UserGroup{ID: "ug-2", Name: "Fachgruppe Wassergefahren", Description: "Wasser"}, nil)
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/user-groups", "token", []byte(`{"name":"Fachgruppe Wassergefahren","description":"Wasser"}`), h)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestCreateAdminUserGroupDupName(t *testing.T) {
	// GROUP_CREATE_DUP: a duplicate name maps to the uniform 409.
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{core.UserGroupsManagePermission}, nil
	}
	svc.createUserGroupFunc = func(_ context.Context, _ *core.User, _ core.CreateUserGroupInput) (*core.UserGroup, error) {
		return nil, core.ErrUserGroupNameTaken
	}
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/user-groups", "token", []byte(`{"name":"Gruppe Ost"}`), h)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestAssignAdminUserGroupMembersOK(t *testing.T) {
	// GROUP_ASSIGN: replacing a team's members returns the updated group.
	svc := adminUserGroupsSvc(nil, nil, func(_ context.Context, _ *core.User, groupID string, _ []string) (*core.UserGroup, error) {
		return &core.UserGroup{ID: groupID, Name: "Gruppe Ost"}, nil
	})
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/user-groups/ug-1/members", "token", []byte(`{"user_ids":["u-tim"]}`), h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestAssignAdminUserGroupMembersNotFound(t *testing.T) {
	// GROUP_ASSIGN_UNKNOWN: an unknown group maps to the uniform 404.
	svc := adminUserGroupsSvc(nil, nil, func(_ context.Context, _ *core.User, _ string, _ []string) (*core.UserGroup, error) {
		return nil, core.ErrUserGroupNotFound
	})
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/user-groups/ug-nope/members", "token", []byte(`{"user_ids":["u-tim"]}`), h)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestAssignAdminUserGroupMembersEmptyBodyRejected(t *testing.T) {
	// GROUP_ASSIGN empty body (retro finding F12): a missing user_ids field
	// must NOT silently clear every member — 400.
	svc := adminUserGroupsSvc(nil, nil, func(_ context.Context, _ *core.User, _ string, _ []string) (*core.UserGroup, error) {
		t.Fatal("service must not be called for an empty body")
		return nil, nil
	})
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/user-groups/ug-1/members", "token", []byte(`{}`), h)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", env.Error.Code)
	}
}

func TestListAdminUserGroupMembersOK(t *testing.T) {
	// GROUP_MEMBERS_LIST: the current member ids of a group are returned as an
	// array (finding 6 drives the member editor's pre-checked set).
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{core.UserGroupsManagePermission}, nil
	}
	svc.listUserGroupMembersFunc = func(_ context.Context, _ *core.User, _ string) ([]string, error) {
		return []string{"u-tim", "u-lena"}, nil
	}
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodGet, "/user-groups/ug-1/members", "token", nil, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var wire struct {
		UserIDs []string `json:"user_ids"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatalf("decoding members response failed: %v", err)
	}
	if len(wire.UserIDs) != 2 || wire.UserIDs[0] != "u-tim" {
		t.Errorf("user_ids = %v, want [u-tim u-lena]", wire.UserIDs)
	}
}

func TestListAdminUserGroupMembersEmptyIsArray(t *testing.T) {
	// GROUP_MEMBERS_EMPTY: a group with no members returns a non-null array.
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{core.UserGroupsManagePermission}, nil
	}
	svc.listUserGroupMembersFunc = func(_ context.Context, _ *core.User, _ string) ([]string, error) {
		return []string{}, nil
	}
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodGet, "/user-groups/ug-1/members", "token", nil, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var wire struct {
		UserIDs []json.RawMessage `json:"user_ids"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &wire)
	if wire.UserIDs == nil {
		t.Error("user_ids = null, want a non-null empty array")
	}
}

func TestListAdminUserGroupMembersNotFound(t *testing.T) {
	// GROUP_MEMBERS_UNKNOWN: an unknown group maps to the uniform 404.
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{core.UserGroupsManagePermission}, nil
	}
	svc.listUserGroupMembersFunc = func(_ context.Context, _ *core.User, _ string) ([]string, error) {
		return nil, core.ErrUserGroupNotFound
	}
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodGet, "/user-groups/ug-nope/members", "token", nil, h)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestDeleteAdminUserGroupOK(t *testing.T) {
	// GROUP_DELETE_VALID (finding 5): deleting a team returns 200 with the
	// German confirmation.
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{core.UserGroupsManagePermission}, nil
	}
	svc.deleteUserGroupFunc = func(_ context.Context, _ *core.User, _ string) error {
		return nil
	}
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodDelete, "/user-groups/ug-1", "token", nil, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var wire struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &wire)
	if wire.Message != core.MsgUserGroupDeleted {
		t.Errorf("delete message = %q, want %q", wire.Message, core.MsgUserGroupDeleted)
	}
}

func TestDeleteAdminUserGroupNotFound(t *testing.T) {
	// GROUP_DELETE_UNKNOWN (finding 5): an unknown group maps to the uniform 404.
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{core.UserGroupsManagePermission}, nil
	}
	svc.deleteUserGroupFunc = func(_ context.Context, _ *core.User, _ string) error {
		return core.ErrUserGroupNotFound
	}
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodDelete, "/user-groups/ug-nope", "token", nil, h)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestDeleteAdminUserGroupForbidden(t *testing.T) {
	// GROUP_DELETE_FORBIDDEN (finding 5): a caller without user_groups.manage is
	// denied by the user-groups sub-mount's own gate.
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{core.UserViewPermission}, nil
	}
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodDelete, "/user-groups/ug-1", "token", nil, h)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestDeactivateAdminUserSelf(t *testing.T) {
	// SELF_DEACTIVATE (finding 7): an admin deactivating their own account maps
	// to the uniform 409 with a clear German message.
	svc := adminUsersSvc(nil, nil, nil, nil, nil, func(_ context.Context, _ string, _ bool) (*core.DeactivateUserResult, error) {
		return nil, core.ErrSelfDeactivation
	})
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/users/u-admin/deactivate", "token", []byte(`{"confirmed":true}`), h)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error.Code != "conflict" || !strings.Contains(env.Error.Message, "eigenes Konto") {
		t.Errorf("409 envelope = %+v, want conflict with the self-deactivation message", env)
	}
}

func TestAdminUserRoutesUnauthorized(t *testing.T) {
	// A missing token is rejected by the users/user-groups sub-mount gates with
	// the uniform 401 — even before the service is consulted.
	svc := adminUsersSvc(nil, nil, nil, nil, nil, nil)
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/users"},
		{http.MethodPost, "/users"},
		{http.MethodGet, "/users/u-tim"},
		{http.MethodPut, "/users/u-tim"},
		{http.MethodPost, "/users/u-tim/deactivate"},
		{http.MethodGet, "/user-groups"},
		{http.MethodPost, "/user-groups"},
		{http.MethodGet, "/user-groups/ug-1/members"},
		{http.MethodPost, "/user-groups/ug-1/members"},
		{http.MethodDelete, "/user-groups/ug-1"},
	} {
		rec := doAdminUsers(tc.method, tc.path, "", nil, h)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token: status = %d, want 401 (body %s)", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

// TestUsersViewOnlyCanListNotManage pins the any-of design note: a caller
// holding ONLY users.view can list and view detail but cannot create/edit/
// deactivate (defense-in-depth re-verification, AD-6).
func TestUsersViewOnlyCanListNotManage(t *testing.T) {
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{core.UserViewPermission}, nil
	}
	svc.listAdminUsersFunc = func(_ context.Context, _ *core.User) ([]*core.AdminUserSummary, error) {
		return []*core.AdminUserSummary{{ID: "u-1", Vorname: "Tim", Nachname: "Müller", Email: "tim@gear.local", Status: "active"}}, nil
	}
	svc.createAdminUserFunc = func(_ context.Context, _ *core.User, _ core.CreateAdminUserInput) (*core.AdminUserWriteResult, error) {
		return nil, core.ErrForbidden
	}
	svc.updateAdminUserFunc = func(_ context.Context, _ *core.User, _ string, _ core.UpdateAdminUserInput) (*core.AdminUserWriteResult, error) {
		return nil, core.ErrForbidden
	}
	svc.deactivateAdminUserFunc = func(_ context.Context, _ *core.User, _ string, _ bool) (*core.DeactivateUserResult, error) {
		return nil, core.ErrForbidden
	}
	h := newAdminUsersSurface(t, svc, &stubValidator{})

	if rec := doAdminUsers(http.MethodGet, "/users", "token", nil, h); rec.Code != http.StatusOK {
		t.Errorf("view-only list: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := doAdminUsers(http.MethodPost, "/users", "token", []byte(`{"vorname":"A","nachname":"B","email":"a@b.de","status":"active"}`), h); rec.Code != http.StatusForbidden {
		t.Errorf("view-only create: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := doAdminUsers(http.MethodPut, "/users/u-1", "token", []byte(`{"vorname":"A","nachname":"B","email":"a@b.de","status":"active"}`), h); rec.Code != http.StatusForbidden {
		t.Errorf("view-only edit: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := doAdminUsers(http.MethodPost, "/users/u-1/deactivate", "token", []byte(`{"confirmed":true}`), h); rec.Code != http.StatusForbidden {
		t.Errorf("view-only deactivate: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}