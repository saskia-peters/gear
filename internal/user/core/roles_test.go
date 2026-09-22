package core

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// rolesRepo seeds the Story 2.5 I/O matrix: an active admin holding all three
// `roles.*` codes plus the four base roles (AD-12), with a helfende volunteer
// attached to the 'helfende' group so the immediate-effect test can resolve a
// live set. Group perms are recomputed by the mock on update (live resolution,
// AD-2/FR-6).
func rolesRepo() *mockRepo {
	repo := newMockRepo()
	repo.users["admin@gear.local"] = &User{
		ID: "u-admin", Email: "admin@gear.local", DisplayName: "Vera Waltung",
		FirstName: "Vera", LastName: "Waltung", PasswordHash: "hashed:admin123456", State: StateActive,
	}
	repo.perms["u-admin"] = []string{RoleCreatePermission, RoleEditPermission, RoleAssignPermission}

	repo.users["helfende@gear.local"] = &User{
		ID: "u-helfende", Email: "helfende@gear.local", DisplayName: "Frei Willig",
		FirstName: "Frei", LastName: "Willig", PasswordHash: "hashed:frei123456", State: StateActive,
	}

	repo.seedRoleGroup("g-helfende", "helfende", "Base role: inspects tools", true,
		[]string{"dashboard.view", "inspection.submit"}, "u-helfende")
	repo.seedRoleGroup("g-schirr", "schirrmeister", "Base role: tool caretaker", true,
		[]string{"dashboard.view", "inspection.submit", "tools.manage", "tool_types.manage"})
	repo.seedRoleGroup("g-fuehrende", "fuehrende", "Base role: leadership", true,
		[]string{"dashboard.view", "inspection.submit", "inspection.history.view", "report.export", "tool.reinstate"})
	repo.seedRoleGroup("g-admin", "admin", "Base role: full access", true, BasePermissionCodes)

	return repo
}

func rolesService(t *testing.T, repo *mockRepo) *Service {
	t.Helper()
	svc, _ := newTestService(repo, &mockHasher{})
	return svc
}

func TestListRolesValid(t *testing.T) {
	// LIST_GROUPS + LIST_CATALOG: an admin holding a roles.* code gets every
	// group (base-roles-first, then name) each with its permission codes, plus
	// the 24-code catalog with German labels.
	repo := rolesRepo()
	svc := rolesService(t, repo)

	res, err := svc.ListRoles(context.Background(), repo.users["admin@gear.local"])
	if err != nil {
		t.Fatalf("ListRoles failed: %v", err)
	}
	if len(res.Groups) != 4 {
		t.Fatalf("group count = %d, want 4 base roles", len(res.Groups))
	}
	// Base-roles-first then name: admin, fuehrende, helfende, schirrmeister.
	wantOrder := []string{"admin", "fuehrende", "helfende", "schirrmeister"}
	for i, want := range wantOrder {
		if res.Groups[i].Name != want {
			t.Errorf("group[%d] = %q, want %q", i, res.Groups[i].Name, want)
		}
		if !res.Groups[i].IsBaseRole {
			t.Errorf("group %q is_base_role = false, want true", res.Groups[i].Name)
		}
	}
	// Every group carries its granted codes (the helfende set is the seeded one).
	helfende := res.Groups[2]
	if len(helfende.Permissions) != 2 || helfende.Permissions[0] != "dashboard.view" || helfende.Permissions[1] != "inspection.submit" {
		t.Errorf("helfende permissions = %v, want [dashboard.view inspection.submit]", helfende.Permissions)
	}
	// The catalog is the full 24-code base series with German labels.
	if len(res.AvailablePermissions) != len(BasePermissionCodes) {
		t.Fatalf("catalog count = %d, want %d", len(res.AvailablePermissions), len(BasePermissionCodes))
	}
	byCode := make(map[string]string, len(res.AvailablePermissions))
	for _, e := range res.AvailablePermissions {
		byCode[e.Code] = e.Label
	}
	if byCode["dashboard.view"] != "Dashboard ansehen" {
		t.Errorf("dashboard.view label = %q, want the German label", byCode["dashboard.view"])
	}
	if strings.HasPrefix(byCode["dashboard.view"], "raw:") {
		t.Errorf("catalog label leaked the raw fallback: %q", byCode["dashboard.view"])
	}
}

func TestListRolesAnySingleRoleCode(t *testing.T) {
	// LIST (any-of gate): a caller holding ONLY one of the three roles.* codes
	// can list — the sub-mount gate passes and the core re-verifies any-of.
	for _, code := range []string{RoleCreatePermission, RoleEditPermission, RoleAssignPermission} {
		repo := rolesRepo()
		repo.users["assign@gear.local"] = &User{
			ID: "u-assign", Email: "assign@gear.local", DisplayName: "Zu Weiser",
			FirstName: "Zu", LastName: "Weiser", State: StateActive,
		}
		repo.perms["u-assign"] = []string{code}
		svc := rolesService(t, repo)

		if _, err := svc.ListRoles(context.Background(), repo.users["assign@gear.local"]); err != nil {
			t.Errorf("ListRoles with %q failed: %v", code, err)
		}
	}
}

func TestListRolesForbidden(t *testing.T) {
	// LIST_FORBIDDEN: a caller with no roles.* code is denied (defense-in-depth;
	// the gateway answers the uniform hidden-existence 403).
	repo := rolesRepo()
	repo.users["schirr@gear.local"] = &User{
		ID: "u-schirr", Email: "schirr@gear.local", DisplayName: "Schirr Meister",
		FirstName: "Schirr", LastName: "Meister", State: StateActive,
	}
	repo.perms["u-schirr"] = []string{"tools.manage", "tool_types.manage"}
	svc := rolesService(t, repo)

	_, err := svc.ListRoles(context.Background(), repo.users["schirr@gear.local"])
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("ListRoles err = %v, want ErrForbidden", err)
	}
}

func TestCreateRoleValid(t *testing.T) {
	// CREATE_VALID: {name:"gerätewart", permissions:[tools.manage]} creates a
	// custom group (is_base_role=false) with the granted code.
	repo := rolesRepo()
	svc := rolesService(t, repo)

	group, err := svc.CreateRole(context.Background(), repo.users["admin@gear.local"], CreateRoleInput{
		Name: "gerätewart", Description: "Pflegt die Geräte", Permissions: []string{"tools.manage"},
	})
	if err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}
	if group.ID == "" {
		t.Error("created group has no id")
	}
	if group.Name != "gerätewart" || group.Description != "Pflegt die Geräte" {
		t.Errorf("group = %+v, want the submitted name/description", group)
	}
	if group.IsBaseRole {
		t.Errorf("custom group is_base_role = true, want false")
	}
	if len(group.Permissions) != 1 || group.Permissions[0] != "tools.manage" {
		t.Errorf("group permissions = %v, want [tools.manage]", group.Permissions)
	}
	// The group is now listable (AD-12: assignable via AddUserToGroup).
	res, err := svc.ListRoles(context.Background(), repo.users["admin@gear.local"])
	if err != nil {
		t.Fatalf("ListRoles after create failed: %v", err)
	}
	found := false
	for _, g := range res.Groups {
		if g.Name == "gerätewart" {
			found = true
		}
	}
	if !found {
		t.Error("created group missing from the list")
	}
}

func TestCreateRoleEmptyPerms(t *testing.T) {
	// CREATE_EMPTY_PERMS: permissions: [] is a valid additive empty set — the
	// group is created with no granted codes.
	repo := rolesRepo()
	svc := rolesService(t, repo)

	group, err := svc.CreateRole(context.Background(), repo.users["admin@gear.local"], CreateRoleInput{
		Name: "ohne_rechte", Description: "", Permissions: []string{},
	})
	if err != nil {
		t.Fatalf("CreateRole(empty perms) failed: %v", err)
	}
	if group.Name != "ohne_rechte" || len(group.Permissions) != 0 {
		t.Errorf("group = %+v, want an empty permission set", group)
	}
}

func TestCreateRoleDupName(t *testing.T) {
	// CREATE_DUP_NAME: an existing name in a DIFFERENT case maps to the uniform
	// 409 (case-insensitive uniqueness).
	repo := rolesRepo()
	svc := rolesService(t, repo)

	_, err := svc.CreateRole(context.Background(), repo.users["admin@gear.local"], CreateRoleInput{
		Name: "Helfende", Permissions: []string{"dashboard.view"},
	})
	if !errors.Is(err, ErrRoleNameTaken) {
		t.Fatalf("CreateRole(dup, different case) err = %v, want ErrRoleNameTaken", err)
	}
}

func TestCreateRoleBadCode(t *testing.T) {
	// CREATE_BAD_CODE: an unknown code maps to the uniform 400, nothing created.
	repo := rolesRepo()
	svc := rolesService(t, repo)

	_, err := svc.CreateRole(context.Background(), repo.users["admin@gear.local"], CreateRoleInput{
		Name: "kaputt", Permissions: []string{"bogus.code"},
	})
	if !errors.Is(err, ErrUnknownPermissionCode) {
		t.Fatalf("CreateRole(bad code) err = %v, want ErrUnknownPermissionCode", err)
	}
}

func TestCreateRoleInvalidName(t *testing.T) {
	// CREATE invalid name: empty and >120-rune names are rejected (400
	// invalid_request, distinct from the 409 duplicate).
	repo := rolesRepo()
	svc := rolesService(t, repo)

	_, err := svc.CreateRole(context.Background(), repo.users["admin@gear.local"], CreateRoleInput{Name: "   ", Permissions: []string{}})
	if !errors.Is(err, ErrRoleInvalidName) {
		t.Fatalf("CreateRole(empty name) err = %v, want ErrRoleInvalidName", err)
	}
	long := strings.Repeat("r", RoleNameMaxLength+1)
	_, err = svc.CreateRole(context.Background(), repo.users["admin@gear.local"], CreateRoleInput{Name: long, Permissions: []string{}})
	if !errors.Is(err, ErrRoleInvalidName) {
		t.Fatalf("CreateRole(long name) err = %v, want ErrRoleInvalidName", err)
	}
}

func TestCreateRoleForbidden(t *testing.T) {
	// CREATE_FORBIDDEN: an assign-only holder may list but never create
	// (defense-in-depth: create needs roles.create, AD-6).
	repo := rolesRepo()
	repo.users["assign@gear.local"] = &User{
		ID: "u-assign", Email: "assign@gear.local", DisplayName: "Zu Weiser",
		FirstName: "Zu", LastName: "Weiser", State: StateActive,
	}
	repo.perms["u-assign"] = []string{RoleAssignPermission}
	svc := rolesService(t, repo)

	_, err := svc.CreateRole(context.Background(), repo.users["assign@gear.local"], CreateRoleInput{
		Name: "nicht_erlaubt", Permissions: []string{},
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("CreateRole(assign-only) err = %v, want ErrForbidden", err)
	}
}

func TestUpdateRoleValid(t *testing.T) {
	// UPDATE_VALID: editing a role replaces its name and permission set.
	repo := rolesRepo()
	svc := rolesService(t, repo)

	group, err := svc.UpdateRole(context.Background(), repo.users["admin@gear.local"], "g-helfende", UpdateRoleInput{
		Name: "helfende", Description: "Geänderte Beschreibung", Permissions: []string{"inspection.submit"},
	})
	if err != nil {
		t.Fatalf("UpdateRole failed: %v", err)
	}
	if group.Description != "Geänderte Beschreibung" {
		t.Errorf("description = %q, want the new description", group.Description)
	}
	if len(group.Permissions) != 1 || group.Permissions[0] != "inspection.submit" {
		t.Errorf("permissions = %v, want [inspection.submit]", group.Permissions)
	}
	if !group.IsBaseRole {
		t.Errorf("helfende lost its base-role flag on update")
	}
}

func TestUpdateBaseRoleAllowed(t *testing.T) {
	// UPDATE_BASE_ROLE: the admin base role is editable — base roles remain the
	// named matrix starting point (AD-12).
	repo := rolesRepo()
	svc := rolesService(t, repo)

	group, err := svc.UpdateRole(context.Background(), repo.users["admin@gear.local"], "g-admin", UpdateRoleInput{
		Name: "admin", Permissions: []string{"dashboard.view", "tools.manage"},
	})
	if err != nil {
		t.Fatalf("UpdateRole(admin base) failed: %v", err)
	}
	if len(group.Permissions) != 2 {
		t.Errorf("admin permissions = %v, want the two granted codes", group.Permissions)
	}
	if !group.IsBaseRole {
		t.Errorf("admin lost its base-role flag")
	}
}

func TestUpdateRoleUnknown(t *testing.T) {
	// UPDATE_UNKNOWN: a nonexistent id maps to the uniform 404.
	repo := rolesRepo()
	svc := rolesService(t, repo)

	_, err := svc.UpdateRole(context.Background(), repo.users["admin@gear.local"], "g-nope", UpdateRoleInput{
		Name: "x", Permissions: []string{},
	})
	if !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("UpdateRole(unknown) err = %v, want ErrRoleNotFound", err)
	}
}

func TestUpdateRoleDupName(t *testing.T) {
	// UPDATE_DUP_NAME: renaming onto a name another group holds → uniform 409.
	repo := rolesRepo()
	svc := rolesService(t, repo)

	_, err := svc.UpdateRole(context.Background(), repo.users["admin@gear.local"], "g-helfende", UpdateRoleInput{
		Name: "schirrmeister", Permissions: []string{"inspection.submit"},
	})
	if !errors.Is(err, ErrRoleNameTaken) {
		t.Fatalf("UpdateRole(dup) err = %v, want ErrRoleNameTaken", err)
	}
}

func TestUpdateRoleRenameToOwnNameCaseVariant(t *testing.T) {
	// UPDATE (own-name guard): renaming a group to its OWN name in a different
	// case stays legal (the duplicate guard excludes the target itself).
	repo := rolesRepo()
	svc := rolesService(t, repo)

	group, err := svc.UpdateRole(context.Background(), repo.users["admin@gear.local"], "g-helfende", UpdateRoleInput{
		Name: "Helfende", Permissions: []string{"dashboard.view", "inspection.submit"},
	})
	if err != nil {
		t.Fatalf("UpdateRole(own-name variant) failed: %v", err)
	}
	if group.Name != "Helfende" {
		t.Errorf("name = %q, want the case-variant rename", group.Name)
	}
}

func TestUpdateRoleForbidden(t *testing.T) {
	// UPDATE_FORBIDDEN: a create-only holder may list but never edit
	// (defense-in-depth: update needs roles.edit, AD-6).
	repo := rolesRepo()
	repo.users["createonly@gear.local"] = &User{
		ID: "u-create", Email: "createonly@gear.local", DisplayName: "Neue Rollen",
		FirstName: "Neue", LastName: "Rollen", State: StateActive,
	}
	repo.perms["u-create"] = []string{RoleCreatePermission}
	svc := rolesService(t, repo)

	_, err := svc.UpdateRole(context.Background(), repo.users["createonly@gear.local"], "g-helfende", UpdateRoleInput{
		Name: "helfende", Permissions: []string{"inspection.submit"},
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("UpdateRole(create-only) err = %v, want ErrForbidden", err)
	}
}

func TestUpdateRoleImmediateEffect(t *testing.T) {
	// IMMEDIATE_EFFECT (AD-2/AD-6/FR-6): editing the helfende role's checks
	// changes a helfende user's RESOLVED set on the very next resolution — no
	// re-login, no cache. The mock's ListPermissionsByUser is live per request
	// (recomputed on update), mirroring the postgres adapter.
	repo := rolesRepo()
	svc := rolesService(t, repo)
	actor := repo.users["admin@gear.local"]
	volunteer := repo.users["helfende@gear.local"]

	before, err := svc.ResolvePermissionSet(context.Background(), volunteer)
	if err != nil {
		t.Fatalf("resolve before failed: %v", err)
	}
	if !containsStr(before, "dashboard.view") {
		t.Fatalf("helfende before = %v, want dashboard.view present", before)
	}

	if _, err := svc.UpdateRole(context.Background(), actor, "g-helfende", UpdateRoleInput{
		Name: "helfende", Permissions: []string{"inspection.submit"},
	}); err != nil {
		t.Fatalf("UpdateRole(helfende) failed: %v", err)
	}

	after, err := svc.ResolvePermissionSet(context.Background(), volunteer)
	if err != nil {
		t.Fatalf("resolve after failed: %v", err)
	}
	if containsStr(after, "dashboard.view") {
		t.Errorf("helfende after = %v, want dashboard.view REMOVED (immediate effect)", after)
	}
	if !containsStr(after, "inspection.submit") {
		t.Errorf("helfende after = %v, want inspection.submit present", after)
	}
}

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func TestCreateRoleTrimsDescription(t *testing.T) {
	// DESCRIPTION_TRIM: the description is trimmed server-side and the trimmed
	// value is what gets persisted — leading/trailing whitespace never reaches
	// the repo (review finding).
	repo := rolesRepo()
	svc := rolesService(t, repo)

	group, err := svc.CreateRole(context.Background(), repo.users["admin@gear.local"], CreateRoleInput{
		Name: "gerätewart", Description: "  Pflegt die Geräte  ", Permissions: []string{},
	})
	if err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}
	if group.Description != "Pflegt die Geräte" {
		t.Errorf("description = %q, want the trimmed value", group.Description)
	}
}

func TestCreateRoleDescriptionTooLong(t *testing.T) {
	// DESCRIPTION_TOO_LONG: a description beyond the cap maps to the uniform 400
	// (ErrRoleInvalidDescription), nothing created.
	repo := rolesRepo()
	svc := rolesService(t, repo)

	long := strings.Repeat("b", RoleDescriptionMaxLength+1)
	_, err := svc.CreateRole(context.Background(), repo.users["admin@gear.local"], CreateRoleInput{
		Name: "gerätewart", Description: long, Permissions: []string{},
	})
	if !errors.Is(err, ErrRoleInvalidDescription) {
		t.Fatalf("CreateRole(too-long description) err = %v, want ErrRoleInvalidDescription", err)
	}
}

func TestUpdateRoleTrimsDescription(t *testing.T) {
	// DESCRIPTION_TRIM on update: the trimmed description replaces the stored
	// one.
	repo := rolesRepo()
	svc := rolesService(t, repo)

	group, err := svc.UpdateRole(context.Background(), repo.users["admin@gear.local"], "g-helfende", UpdateRoleInput{
		Name: "helfende", Description: "  Geändert  ", Permissions: []string{"inspection.submit"},
	})
	if err != nil {
		t.Fatalf("UpdateRole failed: %v", err)
	}
	if group.Description != "Geändert" {
		t.Errorf("description = %q, want the trimmed value", group.Description)
	}
}

func TestRoleAuditTrail(t *testing.T) {
	// AUDIT (NFR-O1/NFR-O2, review finding): role create/update write
	// role.create / role.update audit rows — actor (admin) as actor, group name
	// + permission count as detail (never sensitive), severity normal — like the
	// approval path.
	repo := rolesRepo()
	svc := rolesService(t, repo)
	actor := repo.users["admin@gear.local"]

	if _, err := svc.CreateRole(context.Background(), actor, CreateRoleInput{
		Name: "gerätewart", Description: "", Permissions: []string{"tools.manage"},
	}); err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}
	if got := repo.audit["u-admin"]; len(got) != 1 || got[0] != AuditOperationRoleCreate {
		t.Errorf("audit after create = %v, want [role.create]", got)
	}
	if got := repo.auditDetail["u-admin"]; len(got) != 1 || got[0] != "group=gerätewart permissions=1" {
		t.Errorf("audit detail after create = %v, want group=gerätewart permissions=1", got)
	}
	if got := repo.auditSeverity["u-admin"]; len(got) != 1 || got[0] != AuditSeverityNormal {
		t.Errorf("audit severity after create = %v, want [normal]", got)
	}

	if _, err := svc.UpdateRole(context.Background(), actor, "g-helfende", UpdateRoleInput{
		Name: "helfende", Permissions: []string{"inspection.submit"},
	}); err != nil {
		t.Fatalf("UpdateRole failed: %v", err)
	}
	if got := repo.audit["u-admin"]; len(got) != 2 || got[1] != AuditOperationRoleUpdate {
		t.Errorf("audit after update = %v, want [role.create role.update]", got)
	}
	if got := repo.auditDetail["u-admin"]; len(got) != 2 || got[1] != "group=helfende permissions=1" {
		t.Errorf("audit detail after update = %v, want [.. group=helfende permissions=1]", got)
	}
}

func TestRoleAuditBestEffort(t *testing.T) {
	// NFR-O1: an audit-write failure is logged, never rolled back into the
	// create — the group still lands and the call succeeds.
	repo := rolesRepo()
	repo.auditErr = errors.New("audit down")
	svc := rolesService(t, repo)

	group, err := svc.CreateRole(context.Background(), repo.users["admin@gear.local"], CreateRoleInput{
		Name: "gerätewart", Permissions: []string{"tools.manage"},
	})
	if err != nil {
		t.Fatalf("CreateRole with failing audit failed: %v", err)
	}
	if group.Name != "gerätewart" {
		t.Errorf("group = %+v, want the created group despite the audit failure", group)
	}
}