package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	"github.com/saskia-peters/gear/internal/user/core"
	"github.com/saskia-peters/gear/internal/user/ports"
)

// newAdminReworkSurface mounts the REAL AdminRoutes() so the group-roles and
// user-qualification routes (Spec 2.9) are exercised through the actual
// sub-mount gates. The mock service resolves permissions via the resolver
// adapter.
func newAdminReworkSurface(t *testing.T, svc ports.Service, validator auth.SessionValidator) http.Handler {
	t.Helper()
	h := newTestHandler(svc, validator)
	return h.AdminRoutes()
}

// adminReworkPerms returns a full-admin permission set (all the codes a base
// admin resolves after migration 000013).
func adminReworkPerms() []string {
	return append([]string{
		"users.view", "users.approve", "users.manage", "users.qualifications.manage",
		"user_groups.manage", "roles.create", "roles.edit", "roles.assign",
		"qualifications.manage", "admin.recovery.approve",
	}, []string{"dashboard.view", "inspection.submit", "tools.manage", "tool_types.manage"}...)
}

func TestListUserGroupRolesHandlerOK(t *testing.T) {
	// GROUP_ROLE_LIST: a user_groups.manage holder lists a group's roles.
	svc := &mockService{
		listUserGroupRolesFunc: func(_ context.Context, _ *core.User, groupID string) ([]*core.RoleGroupRef, error) {
			return []*core.RoleGroupRef{{ID: "g-schirr", Name: "schirrmeister"}}, nil
		},
	}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return adminReworkPerms(), nil
	}
	h := newAdminReworkSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodGet, "/user-groups/ug-ost/roles", "token", nil, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	roles, ok := body["roles"].([]any)
	if !ok || len(roles) != 1 {
		t.Fatalf("roles = %v, want 1 entry", body["roles"])
	}
}

func TestListUserGroupRolesHandlerForbidden(t *testing.T) {
	// GROUP_ROLE_FORBIDDEN: a caller without user_groups.manage is denied with
	// the uniform hidden-existence 403.
	svc := &mockService{
		listUserGroupRolesFunc: func(_ context.Context, _ *core.User, _ string) ([]*core.RoleGroupRef, error) {
			return []*core.RoleGroupRef{}, nil
		},
	}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{"users.view", "users.qualifications.manage"}, nil
	}
	h := newAdminReworkSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodGet, "/user-groups/ug-ost/roles", "token", nil, h)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "admin") {
		t.Errorf("403 body hints at the admin module: %s", rec.Body.String())
	}
}

func TestAssignUserGroupRolesHandlerOK(t *testing.T) {
	// GROUP_ROLE_ASSIGN: assigning roles to a group returns the new set.
	svc := &mockService{
		assignUserGroupRolesFunc: func(_ context.Context, _ *core.User, _ string, roleIDs []string) ([]*core.RoleGroupRef, error) {
			return []*core.RoleGroupRef{{ID: roleIDs[0], Name: "schirrmeister"}}, nil
		},
	}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return adminReworkPerms(), nil
	}
	h := newAdminReworkSurface(t, svc, &stubValidator{})

	body := []byte(`{"role_ids":["g-schirr"]}`)
	rec := doAdminUsers(http.MethodPost, "/user-groups/ug-ost/roles", "token", body, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestAssignUserQualificationHandlerOK(t *testing.T) {
	// QUAL_ASSIGN_FIXED_OK: assigning a fixed qualification with expires_at.
	svc := &mockService{
		assignUserQualificationFunc: func(_ context.Context, _ *core.User, userID, qualificationID string, _ *time.Time) (*core.UserQualificationAssignResult, error) {
			return &core.UserQualificationAssignResult{Message: core.MsgQualificationAssignedToUser, UserID: userID, QualificationID: qualificationID}, nil
		},
	}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{"users.view", "users.qualifications.manage"}, nil
	}
	h := newAdminReworkSurface(t, svc, &stubValidator{})

	body := []byte(`{"expires_at":"2027-01-01T00:00:00Z"}`)
	rec := doAdminUsers(http.MethodPost, "/users/u-helf/qualifications/q-fixed", "token", body, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestAssignUserQualificationHandlerExpiryRequired(t *testing.T) {
	// QUAL_ASSIGN_FIXED_MISSING: the core returns ErrQualificationExpiryRequired
	// → 400 with the German message.
	svc := &mockService{
		assignUserQualificationFunc: func(_ context.Context, _ *core.User, _, _ string, _ *time.Time) (*core.UserQualificationAssignResult, error) {
			return nil, core.ErrQualificationExpiryRequired
		},
	}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{"users.view", "users.qualifications.manage"}, nil
	}
	h := newAdminReworkSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/users/u-helf/qualifications/q-fixed", "token", nil, h)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", env.Error.Code)
	}
}

func TestAssignUserQualificationHandlerForbidden(t *testing.T) {
	// QUAL_ASSIGN_FORBIDDEN: the users sub-mount (any users.*) lets a
	// users.view-only holder THROUGH, but the core's users.qualifications.manage
	// defense-in-depth returns 403.
	svc := &mockService{
		assignUserQualificationFunc: func(_ context.Context, _ *core.User, _, _ string, _ *time.Time) (*core.UserQualificationAssignResult, error) {
			return nil, core.ErrForbidden
		},
	}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{"users.view"}, nil
	}
	h := newAdminReworkSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/users/u-helf/qualifications/q-fixed", "token", []byte(`{}`), h)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestRevokeUserQualificationHandlerOK(t *testing.T) {
	// QUAL_REVOKE: a DELETE revokes the assignment.
	svc := &mockService{
		revokeUserQualificationFunc: func(_ context.Context, _ *core.User, userID, qualificationID string) (*core.UserQualificationAssignResult, error) {
			return &core.UserQualificationAssignResult{Message: core.MsgQualificationRevokedFromUser, UserID: userID, QualificationID: qualificationID}, nil
		},
	}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{"users.view", "users.qualifications.manage"}, nil
	}
	h := newAdminReworkSurface(t, svc, &stubValidator{})

	req := httptest.NewRequest(http.MethodDelete, "/users/u-helf/qualifications/q-fixed", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestUpdateUserQualificationExpiryHandlerOK(t *testing.T) {
	// QUAL_EDIT_VALID_UNTIL: a PUT updates the per-assignment valid-until.
	svc := &mockService{
		updateUserQualificationExpiryFunc: func(_ context.Context, _ *core.User, userID, qualificationID string, _ *time.Time) (*core.UserQualificationAssignResult, error) {
			return &core.UserQualificationAssignResult{Message: core.MsgQualificationValidUntilUpdated, UserID: userID, QualificationID: qualificationID}, nil
		},
	}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{"users.view", "users.qualifications.manage"}, nil
	}
	h := newAdminReworkSurface(t, svc, &stubValidator{})

	body := []byte(`{"expires_at":"2027-06-01T00:00:00Z"}`)
	req := httptest.NewRequest(http.MethodPut, "/users/u-helf/qualifications/q-fixed/expiry", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestListUsersStatusFilter(t *testing.T) {
	// USER_LIST_STATUS: the route + handler accept a valid ?status= and yield
	// 200 (the service's mock ignores status; the real service filters via the
	// repo — pinned by the core test).
	svc := &mockService{
		listAdminUsersFunc: func(_ context.Context, _ *core.User) ([]*core.AdminUserSummary, error) {
			return []*core.AdminUserSummary{{ID: "u-1", Email: "a@gear.local", Status: "active"}}, nil
		},
	}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{"users.view"}, nil
	}
	h := newAdminReworkSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodGet, "/users?status=active", "token", nil, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestListUsersStatusFilterInvalidReturns400(t *testing.T) {
	// USER_LIST_STATUS invalid (review finding 2.9): an unknown ?status= maps to
	// the uniform 400 envelope via ErrAdminUserInvalidStatus.
	svc := &mockService{
		listAdminUsersFunc: func(_ context.Context, _ *core.User) ([]*core.AdminUserSummary, error) {
			return nil, core.ErrAdminUserInvalidStatus
		},
	}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{"users.view"}, nil
	}
	h := newAdminReworkSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodGet, "/users?status=bogus", "token", nil, h)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", env.Error.Code)
	}
}

func TestAssignUserGroupRolesHandlerEmptyBodyRejected(t *testing.T) {
	// GROUP_ROLE_ASSIGN empty body (review finding 2.9): a missing role_ids
	// field must NOT silently clear every role — 400.
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return adminReworkPerms(), nil
	}
	h := newAdminReworkSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/user-groups/ug-ost/roles", "token", []byte(`{}`), h)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", env.Error.Code)
	}
}

func TestUpdateUserQualificationExpiryHandlerUnlimitedRejected(t *testing.T) {
	// QUAL_EDIT_UNLIMITED_REJECTED (review finding 2.9): updating a user's
	// per-assignment expiry on an unlimited qualification → 400.
	svc := &mockService{
		updateUserQualificationExpiryFunc: func(_ context.Context, _ *core.User, _, _ string, _ *time.Time) (*core.UserQualificationAssignResult, error) {
			return nil, core.ErrQualificationInvalidExpiresAt
		},
	}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{"users.view", "users.qualifications.manage"}, nil
	}
	h := newAdminReworkSurface(t, svc, &stubValidator{})

	body := []byte(`{"expires_at":"2027-06-01T00:00:00Z"}`)
	req := httptest.NewRequest(http.MethodPut, "/users/u-helf/qualifications/q-unlim/expiry", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestRevokeUserQualificationHandlerNotFound(t *testing.T) {
	// QUAL_REVOKE_UNASSIGNED (review finding 2.9): revoking an unassigned pair
	// maps to the uniform 404.
	svc := &mockService{
		revokeUserQualificationFunc: func(_ context.Context, _ *core.User, _, _ string) (*core.UserQualificationAssignResult, error) {
			return nil, core.ErrQualificationAssignmentNotFound
		},
	}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{"users.view", "users.qualifications.manage"}, nil
	}
	h := newAdminReworkSurface(t, svc, &stubValidator{})

	req := httptest.NewRequest(http.MethodDelete, "/users/u-helf/qualifications/q-fixed", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestAssignUserQualificationHandlerDateOnly(t *testing.T) {
	// QUAL_ASSIGN date-only (review finding 2.9): a "YYYY-MM-DD" expires_at is
	// accepted (parsed at midnight UTC), not rejected with a generic JSON error.
	var got *time.Time
	svc := &mockService{
		assignUserQualificationFunc: func(_ context.Context, _ *core.User, _, _ string, expiresAt *time.Time) (*core.UserQualificationAssignResult, error) {
			got = expiresAt
			return &core.UserQualificationAssignResult{Message: core.MsgQualificationAssignedToUser, UserID: "u-helf", QualificationID: "q-fixed"}, nil
		},
	}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{"users.view", "users.qualifications.manage"}, nil
	}
	h := newAdminReworkSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodPost, "/users/u-helf/qualifications/q-fixed", "token", []byte(`{"expires_at":"2027-01-01"}`), h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if got == nil {
		t.Fatal("expires_at not parsed from date-only input")
	}
	if !got.Equal(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("expires_at = %v, want 2027-01-01T00:00:00Z", got)
	}
}

func TestListUsersStatusFilterUnauthorizedMount(t *testing.T) {
	// The users sub-mount also opens to `users.qualifications.manage` holders
	// (Spec 2.9): a fuehrende/schirrmeister (users.view + the new code, no
	// users.manage) can LIST users but NOT deactivate (core denies).
	svc := &mockService{
		listAdminUsersFunc: func(_ context.Context, _ *core.User) ([]*core.AdminUserSummary, error) {
			return []*core.AdminUserSummary{}, nil
		},
	}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{"users.view", "users.qualifications.manage"}, nil
	}
	h := newAdminReworkSurface(t, svc, &stubValidator{})

	rec := doAdminUsers(http.MethodGet, "/users", "token", nil, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a users.view holder (body %s)", rec.Code, rec.Body.String())
	}
}