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

// newAdminRolesSurface mounts the REAL AdminRoutes() WITHOUT the outer
// composition-root gateway — the groups sub-mount carries its OWN any-of
// `roles.*` RequireAnyPermission gate (Design Notes spec 2.5), which is exactly
// what this suite exercises. The mock service resolves permissions via the
// resolver adapter the same way the real service would.
func newAdminRolesSurface(t *testing.T, svc ports.Service, validator auth.SessionValidator) http.Handler {
	t.Helper()
	h := newTestHandler(svc, validator)
	return h.AdminRoutes()
}

// rolesSvc returns a mock service whose caller resolves ALL THREE roles.* codes
// (so the any-of gate passes) with the given ListRoles/CreateRole/UpdateRole
// behaviour.
func rolesSvc(listRes *core.RoleListResult, listErr error, createFunc func(context.Context, *core.User, core.CreateRoleInput) (*core.RoleGroup, error), updateFunc func(context.Context, *core.User, string, core.UpdateRoleInput) (*core.RoleGroup, error)) *mockService {
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{core.RoleCreatePermission, core.RoleEditPermission, core.RoleAssignPermission}, nil
	}
	svc.listRolesFunc = func(_ context.Context, _ *core.User) (*core.RoleListResult, error) {
		return listRes, listErr
	}
	svc.createRoleFunc = createFunc
	svc.updateRoleFunc = updateFunc
	return svc
}

func doRoles(method, path, token string, body []byte, h http.Handler) *httptest.ResponseRecorder {
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

const catalogJSON = `[{"code":"dashboard.view","label":"Dashboard ansehen"},{"code":"tools.manage","label":"Geräte verwalten"}]`

func listResultFixture() *core.RoleListResult {
	var catalog []*core.PermissionCatalogEntry
	_ = json.Unmarshal([]byte(catalogJSON), &catalog)
	return &core.RoleListResult{
		Groups: []*core.RoleGroup{
			{ID: "g-helfende", Name: "helfende", Description: "Base role", IsBaseRole: true, Permissions: []string{"dashboard.view", "inspection.submit"}},
			{ID: "g-gerat", Name: "gerätewart", Description: "", IsBaseRole: false, Permissions: []string{"tools.manage"}},
		},
		AvailablePermissions: catalog,
	}
}

func TestListRolesOK(t *testing.T) {
	// LIST_GROUPS + LIST_CATALOG: an admin holding a roles.* code gets 200 with
	// the groups (base roles first incl. the four + custom) each with its codes,
	// and the server-authoritative catalog with German labels.
	svc := rolesSvc(listResultFixture(), nil, nil, nil)
	h := newAdminRolesSurface(t, svc, &stubValidator{})

	rec := doRoles(http.MethodGet, "/groups", "token", nil, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var wire struct {
		Groups []struct {
			ID          string   `json:"id"`
			Name        string   `json:"name"`
			IsBaseRole  bool     `json:"is_base_role"`
			Permissions []string `json:"permissions"`
		} `json:"groups"`
		AvailablePermissions []struct {
			Code  string `json:"code"`
			Label string `json:"label"`
		} `json:"available_permissions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatalf("decoding list response failed: %v", err)
	}
	if len(wire.Groups) != 2 {
		t.Fatalf("group count = %d, want 2", len(wire.Groups))
	}
	if wire.Groups[0].Name != "helfende" || !wire.Groups[0].IsBaseRole {
		t.Errorf("first group = %+v, want the base helfende role", wire.Groups[0])
	}
	if wire.Groups[1].Name != "gerätewart" || wire.Groups[1].IsBaseRole {
		t.Errorf("second group = %+v, want the custom gerätewart role", wire.Groups[1])
	}
	if len(wire.AvailablePermissions) != 2 || wire.AvailablePermissions[0].Label != "Dashboard ansehen" {
		t.Errorf("catalog = %+v, want the German-labeled catalog", wire.AvailablePermissions)
	}
}

func TestListRolesEmptyListIsArray(t *testing.T) {
	// LIST_EMPTY: an empty group list serializes as a non-null array so the
	// client can render the empty state.
	svc := rolesSvc(&core.RoleListResult{}, nil, nil, nil)
	h := newAdminRolesSurface(t, svc, &stubValidator{})

	rec := doRoles(http.MethodGet, "/groups", "token", nil, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var wire struct {
		Groups []json.RawMessage `json:"groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatalf("decoding empty list failed: %v", err)
	}
	if wire.Groups == nil {
		t.Error("groups = null, want a non-null empty array")
	}
}

func TestListRolesForbidden(t *testing.T) {
	// LIST_FORBIDDEN: a caller WITHOUT any roles.* code is denied by the groups
	// sub-mount's own any-of gate with the uniform hidden-existence 403 — no
	// admin hint, no role hint (FR-19). A tools-only schirrmeister holds no
	// roles.* code.
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{"tools.manage", "tool_types.manage"}, nil
	}
	h := newAdminRolesSurface(t, svc, &stubValidator{})

	rec := doRoles(http.MethodGet, "/groups", "token", nil, h)
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
	if strings.Contains(strings.ToLower(rec.Body.String()), "admin") || strings.Contains(strings.ToLower(rec.Body.String()), "rollen") {
		t.Errorf("403 body hints at the roles surface: %s", rec.Body.String())
	}
}

func TestCreateRoleOK(t *testing.T) {
	// CREATE_VALID: creating a named group returns 201 with the stored group.
	svc := rolesSvc(nil, nil, func(_ context.Context, _ *core.User, in core.CreateRoleInput) (*core.RoleGroup, error) {
		return &core.RoleGroup{ID: "g-gerat", Name: in.Name, Description: in.Description, Permissions: in.Permissions}, nil
	}, nil)
	h := newAdminRolesSurface(t, svc, &stubValidator{})

	body := []byte(`{"name":"gerätewart","description":"Pflegt Geräte","permissions":["tools.manage"]}`)
	rec := doRoles(http.MethodPost, "/groups", "token", body, h)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var wire struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatalf("decoding create response failed: %v", err)
	}
	if wire.Name != "gerätewart" || len(wire.Permissions) != 1 || wire.Permissions[0] != "tools.manage" {
		t.Errorf("create response = %+v, want the stored group", wire)
	}
}

func TestCreateRoleDupName(t *testing.T) {
	// CREATE_DUP_NAME: a duplicate name maps to the uniform 409 conflict.
	svc := rolesSvc(nil, nil, func(_ context.Context, _ *core.User, _ core.CreateRoleInput) (*core.RoleGroup, error) {
		return nil, core.ErrRoleNameTaken
	}, nil)
	h := newAdminRolesSurface(t, svc, &stubValidator{})

	rec := doRoles(http.MethodPost, "/groups", "token", []byte(`{"name":"helfende","permissions":[]}`), h)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error.Code != "conflict" || !strings.Contains(env.Error.Message, "Rolle") {
		t.Errorf("409 envelope = %+v, want conflict with German message", env)
	}
}

func TestCreateRoleBadCode(t *testing.T) {
	// CREATE_BAD_CODE: an unknown permission code maps to the uniform 400
	// invalid — nothing created.
	svc := rolesSvc(nil, nil, func(_ context.Context, _ *core.User, _ core.CreateRoleInput) (*core.RoleGroup, error) {
		return nil, core.ErrUnknownPermissionCode
	}, nil)
	h := newAdminRolesSurface(t, svc, &stubValidator{})

	rec := doRoles(http.MethodPost, "/groups", "token", []byte(`{"name":"gerätewart","permissions":["bogus.code"]}`), h)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error.Code != "invalid" {
		t.Errorf("code = %q, want invalid", env.Error.Code)
	}
}

func TestCreateRoleInvalidName(t *testing.T) {
	// CREATE invalid name: empty name maps to 400 invalid_request (German).
	svc := rolesSvc(nil, nil, func(_ context.Context, _ *core.User, _ core.CreateRoleInput) (*core.RoleGroup, error) {
		return nil, core.ErrRoleInvalidName
	}, nil)
	h := newAdminRolesSurface(t, svc, &stubValidator{})

	rec := doRoles(http.MethodPost, "/groups", "token", []byte(`{"name":"  ","permissions":[]}`), h)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", env.Error.Code)
	}
}

func TestCreateRoleMalformedJSON(t *testing.T) {
	// CREATE malformed body: an unparseable JSON body maps to 400
	// invalid_request.
	svc := rolesSvc(nil, nil, func(_ context.Context, _ *core.User, _ core.CreateRoleInput) (*core.RoleGroup, error) {
		t.Fatal("service must not be called for malformed JSON")
		return nil, nil
	}, nil)
	h := newAdminRolesSurface(t, svc, &stubValidator{})

	rec := doRoles(http.MethodPost, "/groups", "token", []byte(`{not json`), h)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestCreateRoleForbidden(t *testing.T) {
	// CREATE_FORBIDDEN: an assign-only holder passes the any-of LIST gate but
	// the core re-verifies create needs roles.create → uniform 403.
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{core.RoleAssignPermission}, nil
	}
	svc.createRoleFunc = func(_ context.Context, _ *core.User, _ core.CreateRoleInput) (*core.RoleGroup, error) {
		return nil, core.ErrForbidden
	}
	h := newAdminRolesSurface(t, svc, &stubValidator{})

	rec := doRoles(http.MethodPost, "/groups", "token", []byte(`{"name":"gerätewart","permissions":[]}`), h)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestUpdateRoleOK(t *testing.T) {
	// UPDATE_VALID: editing a role returns 200 with the replaced group.
	svc := rolesSvc(nil, nil, nil, func(_ context.Context, _ *core.User, id string, in core.UpdateRoleInput) (*core.RoleGroup, error) {
		return &core.RoleGroup{ID: id, Name: in.Name, Description: in.Description, Permissions: in.Permissions}, nil
	})
	h := newAdminRolesSurface(t, svc, &stubValidator{})

	rec := doRoles(http.MethodPut, "/groups/g-helfende", "token", []byte(`{"name":"helfende","description":"Neu","permissions":["inspection.submit"]}`), h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var wire struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatalf("decoding update response failed: %v", err)
	}
	if wire.ID != "g-helfende" || wire.Name != "helfende" || len(wire.Permissions) != 1 {
		t.Errorf("update response = %+v, want the replaced group", wire)
	}
}

func TestUpdateRoleNotFound(t *testing.T) {
	// UPDATE_UNKNOWN: an unknown id maps to the uniform 404.
	svc := rolesSvc(nil, nil, nil, func(_ context.Context, _ *core.User, _ string, _ core.UpdateRoleInput) (*core.RoleGroup, error) {
		return nil, core.ErrRoleNotFound
	})
	h := newAdminRolesSurface(t, svc, &stubValidator{})

	rec := doRoles(http.MethodPut, "/groups/not-a-uuid", "token", []byte(`{"name":"x","permissions":[]}`), h)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error.Code != "not_found" || !strings.Contains(env.Error.Message, "Rolle") {
		t.Errorf("404 envelope = %+v, want not_found with German message", env)
	}
}

func TestUpdateRoleDupName(t *testing.T) {
	// UPDATE_DUP_NAME: renaming onto a taken name maps to the uniform 409.
	svc := rolesSvc(nil, nil, nil, func(_ context.Context, _ *core.User, _ string, _ core.UpdateRoleInput) (*core.RoleGroup, error) {
		return nil, core.ErrRoleNameTaken
	})
	h := newAdminRolesSurface(t, svc, &stubValidator{})

	rec := doRoles(http.MethodPut, "/groups/g-helfende", "token", []byte(`{"name":"schirrmeister","permissions":[]}`), h)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestUpdateRoleForbidden(t *testing.T) {
	// UPDATE_FORBIDDEN: a create-only holder passes the any-of LIST gate but the
	// core re-verifies update needs roles.edit → uniform 403.
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{core.RoleCreatePermission}, nil
	}
	svc.updateRoleFunc = func(_ context.Context, _ *core.User, _ string, _ core.UpdateRoleInput) (*core.RoleGroup, error) {
		return nil, core.ErrForbidden
	}
	h := newAdminRolesSurface(t, svc, &stubValidator{})

	rec := doRoles(http.MethodPut, "/groups/g-helfende", "token", []byte(`{"name":"helfende","permissions":[]}`), h)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestRolesRoutesUnauthorized(t *testing.T) {
	// A missing token is rejected by the groups sub-mount's own gateway with the
	// uniform 401 — even before the service is consulted.
	svc := rolesSvc(listResultFixture(), nil, nil, nil)
	h := newAdminRolesSurface(t, svc, &stubValidator{})

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/groups"},
		{http.MethodPost, "/groups"},
		{http.MethodPut, "/groups/g-helfende"},
	} {
		rec := doRoles(tc.method, tc.path, "", nil, h)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token: status = %d, want 401 (body %s)", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

// TestRolesAssignOnlyCanListNotEdit pins the any-of design note: a caller
// holding ONLY roles.assign reaches the list (200) but cannot create/edit — the
// server re-verifies the exact code per action (AD-6, defense-in-depth).
func TestRolesAssignOnlyCanListNotEdit(t *testing.T) {
	svc := &mockService{}
	svc.resolvePermissionFunc = func(_ context.Context, _ *core.User) ([]string, error) {
		return []string{core.RoleAssignPermission}, nil
	}
	svc.listRolesFunc = func(_ context.Context, _ *core.User) (*core.RoleListResult, error) {
		return listResultFixture(), nil
	}
	svc.createRoleFunc = func(_ context.Context, _ *core.User, _ core.CreateRoleInput) (*core.RoleGroup, error) {
		return nil, core.ErrForbidden
	}
	svc.updateRoleFunc = func(_ context.Context, _ *core.User, _ string, _ core.UpdateRoleInput) (*core.RoleGroup, error) {
		return nil, core.ErrForbidden
	}
	h := newAdminRolesSurface(t, svc, &stubValidator{})

	if rec := doRoles(http.MethodGet, "/groups", "token", nil, h); rec.Code != http.StatusOK {
		t.Errorf("assign-only list: status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := doRoles(http.MethodPost, "/groups", "token", []byte(`{"name":"x","permissions":[]}`), h); rec.Code != http.StatusForbidden {
		t.Errorf("assign-only create: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := doRoles(http.MethodPut, "/groups/g-helfende", "token", []byte(`{"name":"x","permissions":[]}`), h); rec.Code != http.StatusForbidden {
		t.Errorf("assign-only update: status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}