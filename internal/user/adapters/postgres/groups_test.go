package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saskia-peters/gear/internal/user/core"
)

// TestPostgresRoleGroups covers the Story 2.5 role & permission-group
// persistence contract (AD-12): list every group (base roles first, then name)
// with its codes, create a named group atomically (case-insensitive duplicate
// name → 409, unknown code → 400, empty set valid), update a group atomically
// (delete-then-insert, base roles editable, own-name rename legal, unknown id →
// 404) — against the REAL postgres repository.
func TestPostgresRoleGroups(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://gear:gear@localhost:5432/gear?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("skipping db integration test: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("skipping db integration test (db ping failed): %v", err)
	}

	repo := NewRepository(New(pool))
	stamp := time.Now().Format("20060102150405.000000")
	groupName := "gerätewart." + stamp

	// Cleanup deletes must use an INDEPENDENT pool: t.Cleanup callbacks run
	// AFTER the test function's deferred pool.Close(), so the shared pool is
	// already closed by then (shared-suite pattern). Registered first so it
	// closes LAST (t.Cleanup is LIFO).
	cleanupPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("creating cleanup pool failed: %v", err)
	}
	t.Cleanup(func() { cleanupPool.Close() })
	cleanupGroup := func(name string) {
		t.Helper()
		t.Cleanup(func() {
			// Case-insensitive match: the own-name case-variant rename test can
			// leave the group under a differently-cased name.
			if _, err := cleanupPool.Exec(context.Background(), "DELETE FROM permission_groups WHERE lower(name) = lower($1)", name); err != nil {
				t.Errorf("cleaning up permission group %q failed: %v", name, err)
			}
		})
	}

	// LIST_GROUPS: the four seeded base roles are present, base-roles-first then
	// name, each carrying its granted codes (AD-12).
	groups, err := repo.ListGroups(ctx)
	if err != nil {
		t.Fatalf("ListGroups failed: %v", err)
	}
	var helfende *core.RoleGroup
	for _, g := range groups {
		if g.Name == "helfende" {
			helfende = g
		}
	}
	if helfende == nil {
		t.Fatal("helfende base role missing from the list")
	}
	if !helfende.IsBaseRole {
		t.Errorf("helfende is_base_role = false, want true")
	}
	if !sameCodeSet(helfende.Permissions, []string{"dashboard.view", "inspection.submit"}) {
		t.Errorf("helfende permissions = %v, want [dashboard.view inspection.submit]", helfende.Permissions)
	}
	// Base roles sort before any custom group; among equals the order is by name.
	for i := 1; i < len(groups); i++ {
		if groups[i-1].IsBaseRole == groups[i].IsBaseRole {
			continue
		}
		if !groups[i-1].IsBaseRole && groups[i].IsBaseRole {
			t.Error("custom group sorted before a base role")
		}
	}

	// CREATE_VALID: a named group (is_base_role=false) with its permission rows.
	created, err := repo.CreateGroup(ctx, groupName, "Pflegt die Geräte", []string{"tools.manage"})
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	cleanupGroup(groupName)
	if created.IsBaseRole {
		t.Errorf("custom group is_base_role = true, want false")
	}
	if !sameCodeSet(created.Permissions, []string{"tools.manage"}) {
		t.Errorf("created permissions = %v, want [tools.manage]", created.Permissions)
	}
	// The created group shows up in the list (assignable via AddUserToGroup).
	groups, err = repo.ListGroups(ctx)
	if err != nil {
		t.Fatalf("ListGroups after create failed: %v", err)
	}
	foundCreated := false
	for _, g := range groups {
		if g.ID == created.ID {
			foundCreated = true
		}
	}
	if !foundCreated {
		t.Error("created group missing from the list")
	}

	// CREATE_DUP_NAME: the same name in a DIFFERENT case is a uniform 409
	// (case-insensitive uniqueness).
	if _, err := repo.CreateGroup(ctx, strings.ToUpper(groupName), "", nil); !errors.Is(err, core.ErrRoleNameTaken) {
		t.Errorf("CreateGroup(dup, case variant) err = %v, want ErrRoleNameTaken", err)
	}

	// CREATE_BAD_CODE: a code outside the 22 base series → uniform 400, nothing
	// created.
	if _, err := repo.CreateGroup(ctx, "bad."+stamp, "", []string{"bogus.code"}); !errors.Is(err, core.ErrUnknownPermissionCode) {
		t.Errorf("CreateGroup(bad code) err = %v, want ErrUnknownPermissionCode", err)
	}

	// CREATE_EMPTY_PERMS: an empty permission set is a valid additive set.
	emptyName := "empty." + stamp
	empty, err := repo.CreateGroup(ctx, emptyName, "", []string{})
	if err != nil {
		t.Fatalf("CreateGroup(empty perms) failed: %v", err)
	}
	cleanupGroup(emptyName)
	if len(empty.Permissions) != 0 {
		t.Errorf("empty-group permissions = %v, want []", empty.Permissions)
	}

	// UPDATE_VALID: replacing the name/description AND the permission set
	// (delete-then-insert) atomically.
	renamed := "gerätewart2." + stamp
	updated, err := repo.UpdateGroup(ctx, created.ID, renamed, "Neue Beschreibung", []string{"tools.manage", "tool_types.manage"})
	if err != nil {
		t.Fatalf("UpdateGroup failed: %v", err)
	}
	cleanupGroup(renamed)
	if updated.Name != renamed || updated.Description != "Neue Beschreibung" {
		t.Errorf("updated group = %+v, want the new name/description", updated)
	}
	if !sameCodeSet(updated.Permissions, []string{"tools.manage", "tool_types.manage"}) {
		t.Errorf("updated permissions = %v, want [tools.manage tool_types.manage]", updated.Permissions)
	}

	// UPDATE_UNKNOWN: an unknown (and a malformed) id maps to the uniform 404.
	if _, err := repo.UpdateGroup(ctx, "00000000-0000-0000-0000-000000000000", "x", "", nil); !errors.Is(err, core.ErrRoleNotFound) {
		t.Errorf("UpdateGroup(unknown) err = %v, want ErrRoleNotFound", err)
	}
	if _, err := repo.UpdateGroup(ctx, "not-a-uuid", "x", "", nil); !errors.Is(err, core.ErrRoleNotFound) {
		t.Errorf("UpdateGroup(malformed id) err = %v, want ErrRoleNotFound", err)
	}

	// UPDATE_UNKNOWN_PRECEDENCE (review finding): updating a NONEXISTENT id to a
	// name ANOTHER group already holds must yield the uniform 404 not-found —
	// existence is confirmed before the duplicate-name check, so an unknown id
	// never answers 409 "name taken".
	if _, err := repo.UpdateGroup(ctx, "00000000-0000-0000-0000-000000000000", "helfende", "", nil); !errors.Is(err, core.ErrRoleNotFound) {
		t.Errorf("UpdateGroup(unknown id, taken name) err = %v, want ErrRoleNotFound (never 409)", err)
	}
	if _, err := repo.UpdateGroup(ctx, "not-a-uuid", "helfende", "", nil); !errors.Is(err, core.ErrRoleNotFound) {
		t.Errorf("UpdateGroup(malformed id, taken name) err = %v, want ErrRoleNotFound (never 409)", err)
	}

	// UPDATE_DUP_NAME: renaming onto a name another group holds → uniform 409.
	if _, err := repo.UpdateGroup(ctx, created.ID, "helfende", "", nil); !errors.Is(err, core.ErrRoleNameTaken) {
		t.Errorf("UpdateGroup(dup) err = %v, want ErrRoleNameTaken", err)
	}

	// UPDATE (own-name guard): renaming a group to its OWN name in a different
	// case stays legal (the duplicate check excludes the target itself).
	own, err := repo.UpdateGroup(ctx, created.ID, strings.ToUpper(renamed), "Neue Beschreibung", []string{"tools.manage"})
	if err != nil {
		t.Fatalf("UpdateGroup(own-name case variant) failed: %v", err)
	}
	if own.Name != strings.ToUpper(renamed) {
		t.Errorf("own-name rename = %q, want the case variant", own.Name)
	}

	// ATOMIC: an update that fails the code check (bad code) leaves the group
	// FULLY unchanged — name, description AND permission set (the whole
	// transaction rolls back, Story 2.5 all-or-nothing).
	_, err = repo.UpdateGroup(ctx, created.ID, "will_fail."+stamp, "Kaputt", []string{"bogus.code"})
	if !errors.Is(err, core.ErrUnknownPermissionCode) {
		t.Fatalf("UpdateGroup(bad code) err = %v, want ErrUnknownPermissionCode", err)
	}
	groups, err = repo.ListGroups(ctx)
	if err != nil {
		t.Fatalf("ListGroups after failed update failed: %v", err)
	}
	for _, g := range groups {
		if g.ID == created.ID {
			if g.Name != strings.ToUpper(renamed) || g.Description != "Neue Beschreibung" {
				t.Errorf("group changed despite failed update: %+v", g)
			}
			if !sameCodeSet(g.Permissions, []string{"tools.manage"}) {
				t.Errorf("permissions changed despite failed update: %v", g.Permissions)
			}
		}
	}

	// UPDATE_BASE_ROLE: the admin base role is editable (AD-12) and its flag
	// survives the edit; the role is restored afterwards so the shared matrix is
	// left untouched for other tests.
	var admin *core.RoleGroup
	for _, g := range groups {
		if g.Name == "admin" {
			admin = g
		}
	}
	if admin == nil {
		t.Fatal("admin base role missing")
	}
	adminUpdated, err := repo.UpdateGroup(ctx, admin.ID, "admin", admin.Description, []string{"dashboard.view"})
	if err != nil {
		t.Fatalf("UpdateGroup(admin base) failed: %v", err)
	}
	if !adminUpdated.IsBaseRole {
		t.Errorf("admin lost its base-role flag on edit")
	}
	if !sameCodeSet(adminUpdated.Permissions, []string{"dashboard.view"}) {
		t.Errorf("admin permissions = %v, want [dashboard.view]", adminUpdated.Permissions)
	}
	if _, err := repo.UpdateGroup(ctx, admin.ID, "admin", admin.Description, admin.Permissions); err != nil {
		t.Fatalf("restoring admin role failed: %v", err)
	}
}

// TestPostgresPermissionCatalogMatchesBaseCodes pins the drift boundary (review
// finding): the server-authoritative permission catalog (ListAllPermissions,
// read from the `permissions` table) must equal core.BasePermissionCodes — the
// in-code list that create/update validation draws from. If a future migration
// seeds a code without a matching in-code entry (or vice versa), this test
// fails loudly instead of letting the two sources silently drift.
func TestPostgresPermissionCatalogMatchesBaseCodes(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://gear:gear@localhost:5432/gear?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("skipping db integration test: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("skipping db integration test (db ping failed): %v", err)
	}

	repo := NewRepository(New(pool))
	entries, err := repo.ListAllPermissions(ctx)
	if err != nil {
		t.Fatalf("ListAllPermissions failed: %v", err)
	}
	codes := make([]string, 0, len(entries))
	for _, e := range entries {
		codes = append(codes, e.Code)
	}
	if !sameCodeSet(codes, core.BasePermissionCodes) {
		t.Errorf("DB catalog codes = %v, want core.BasePermissionCodes (%d codes)", codes, len(core.BasePermissionCodes))
	}
	if len(entries) != len(core.BasePermissionCodes) {
		t.Errorf("DB catalog count = %d, want %d (matches core.BasePermissionCodes)", len(entries), len(core.BasePermissionCodes))
	}
	for _, e := range entries {
		if strings.TrimSpace(e.Label) == "" {
			t.Errorf("catalog entry %q has an empty label", e.Code)
		}
	}
}

// TestPostgresRoleUpdateImmediateEffect proves the IMMEDIATE_EFFECT contract
// (AD-2/AD-6/FR-6): editing the helfende role's checks changes a helfende
// user's RESOLVED permission set on the very next resolution — no re-login, no
// cache. The helfende role is restored afterwards so the shared base matrix is
// left untouched for the other integration tests.
func TestPostgresRoleUpdateImmediateEffect(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://gear:gear@localhost:5432/gear?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("skipping db integration test: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("skipping db integration test (db ping failed): %v", err)
	}

	repo := NewRepository(New(pool))
	stamp := time.Now().Format("20060102150405.000000")
	email := "immediate." + stamp + "@gear.local"
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", email)
	})
	volunteer, err := repo.CreateRegisteredUser(ctx, email, "Frei Willig", "Frei", "Willig", "$argon2id$v=19$dummyhash")
	if err != nil {
		t.Fatalf("CreateRegisteredUser failed: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_permission_groups (user_id, permission_group_id)
		SELECT $1, g.id FROM permission_groups g WHERE g.name = 'helfende'`, volunteer.ID); err != nil {
		t.Fatalf("assigning helfende failed: %v", err)
	}

	resolve := func() []string {
		t.Helper()
		perms, err := repo.ListPermissionsByUser(ctx, volunteer.ID)
		if err != nil {
			t.Fatalf("ListPermissionsByUser failed: %v", err)
		}
		return perms
	}

	before := resolve()
	if !sameCodeSet(before, []string{"dashboard.view", "inspection.submit"}) {
		t.Fatalf("helfende before = %v, want [dashboard.view inspection.submit]", before)
	}

	// Locate the helfende role and edit it to DROP dashboard.view.
	groups, err := repo.ListGroups(ctx)
	if err != nil {
		t.Fatalf("ListGroups failed: %v", err)
	}
	var helfende *core.RoleGroup
	for _, g := range groups {
		if g.Name == "helfende" {
			helfende = g
		}
	}
	if helfende == nil {
		t.Fatal("helfende role missing")
	}
	original := append([]string(nil), helfende.Permissions...)
	// Restore the base matrix on every path (immediate, before any Fatal).
	defer func() {
		if _, err := repo.UpdateGroup(context.Background(), helfende.ID, "helfende", helfende.Description, original); err != nil {
			t.Errorf("restoring helfende role failed: %v", err)
		}
	}()

	if _, err := repo.UpdateGroup(ctx, helfende.ID, "helfende", helfende.Description, []string{"inspection.submit"}); err != nil {
		t.Fatalf("UpdateGroup(helfende) failed: %v", err)
	}

	// IMMEDIATE_EFFECT: the very next resolution already reflects the edit.
	after := resolve()
	if !sameCodeSet(after, []string{"inspection.submit"}) {
		t.Errorf("helfende after = %v, want [inspection.submit] (immediate effect)", after)
	}
	if sameCodeSet(after, original) {
		t.Errorf("helfende resolution unchanged after the edit: %v", after)
	}
}