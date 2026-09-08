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

// TestPostgresAdminRework covers the Spec 2.9 (Admin Rework Effort 1)
// persistence contract against the REAL postgres repository:
//
//   - three-way permission resolution (RESOLVE_INDIVIDUAL / RESOLVE_VIA_GROUP /
//     RESOLVE_UNION / RESOLVE_REVOKE_VIA_GROUP): a user's set is the additive
//     union of individual roles + roles inherited via user-groups + direct
//     grants, deduplicated, live per request.
//   - user-group ROLE replacement (GROUP_ROLE_ASSIGN / GROUP_ROLE_REMOVE):
//     assigning/removing a role on a group takes effect on members' next
//     resolution.
//   - per-user qualification assignment with valid-until (QUAL_ASSIGN_* /
//     QUAL_EDIT_*): a fixed qualification requires a per-assignment expires_at;
//     an unlimited one never expires; the per-assignment value is read first.
func TestPostgresAdminRework(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://gear:gear@localhost:5432/gear?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

	// The seeded admin group must carry the new `users.qualifications.manage`
	// grant after migration 000013 (review finding 2.9).
	admin, err := repo.GetUserByEmail(ctx, "admin.1@gear.local")
	if err == nil && admin != nil {
		adminPerms, err := repo.ListPermissionsByUser(ctx, admin.ID)
		if err != nil {
			t.Fatalf("ListPermissionsByUser(admin) failed: %v", err)
		}
		found := false
		for _, p := range adminPerms {
			if p == "users.qualifications.manage" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("admin role missing users.qualifications.manage after 000013: %v", adminPerms)
		}
	}

	memberEmail := "rework.member." + stamp + "@gear.local"
	groupName := "rework.group." + stamp
	roleName := "rework.role." + stamp
	qualName := "rework.qual." + stamp

	cleanupPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("creating cleanup pool failed: %v", err)
	}
	t.Cleanup(func() { cleanupPool.Close() })
	t.Cleanup(func() {
		for _, email := range []string{memberEmail} {
			if _, err := cleanupPool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", email); err != nil {
				t.Errorf("cleaning up user %q failed: %v", email, err)
			}
		}
		if _, err := cleanupPool.Exec(context.Background(), "DELETE FROM user_groups WHERE lower(name) = lower($1)", groupName); err != nil {
			t.Errorf("cleaning up group %q failed: %v", groupName, err)
		}
		if _, err := cleanupPool.Exec(context.Background(), "DELETE FROM permission_groups WHERE lower(name) = lower($1)", roleName); err != nil {
			t.Errorf("cleaning up role %q failed: %v", roleName, err)
		}
		if _, err := cleanupPool.Exec(context.Background(), "DELETE FROM qualifications WHERE lower(name) = lower($1)", qualName); err != nil {
			t.Errorf("cleaning up qualification %q failed: %v", qualName, err)
		}
	})

	// Create the member user (active), the team, the custom role, and the
	// qualification.
	member, err := repo.CreateAdminUser(ctx, memberEmail, "Rolf", "Rework", string(core.StateActive), nil, nil, nil)
	if err != nil {
		t.Fatalf("CreateAdminUser failed: %v", err)
	}
	group, err := repo.CreateUserGroup(ctx, groupName, "Spec 2.9 test team")
	if err != nil {
		t.Fatalf("CreateUserGroup failed: %v", err)
	}
	role, err := repo.CreateGroup(ctx, roleName, "Spec 2.9 test role", []string{"dashboard.view", "tools.manage"})
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	fixedExpiry := time.Now().UTC().Add(90 * 24 * time.Hour)
	qual, err := repo.CreateQualification(ctx, qualName, "Fixed-validity qual", core.QualificationExpiryFixed, &fixedExpiry)
	if err != nil {
		t.Fatalf("CreateQualification failed: %v", err)
	}

	// RESOLVE_INDIVIDUAL: before any team role, the member's set is empty.
	perms, err := repo.ListPermissionsByUser(ctx, member.ID)
	if err != nil {
		t.Fatalf("ListPermissionsByUser failed: %v", err)
	}
	if len(perms) != 0 {
		t.Fatalf("initial member perms = %v, want empty", perms)
	}

	// Add the member to the team.
	if _, err := repo.AssignUserGroupMembers(ctx, group.ID, []string{member.ID}); err != nil {
		t.Fatalf("AssignUserGroupMembers failed: %v", err)
	}

	// GROUP_ROLE_ASSIGN: assign the role to the team.
	roles, err := repo.ReplaceUserGroupRoles(ctx, group.ID, []string{role.ID})
	if err != nil {
		t.Fatalf("ReplaceUserGroupRoles failed: %v", err)
	}
	if len(roles) != 1 || roles[0].ID != role.ID {
		t.Fatalf("roles = %+v, want the created role", roles)
	}

	// RESOLVE_VIA_GROUP: the member now inherits the role's codes via the team.
	perms, err = repo.ListPermissionsByUser(ctx, member.ID)
	if err != nil {
		t.Fatalf("ListPermissionsByUser failed: %v", err)
	}
	want := map[string]bool{"dashboard.view": true, "tools.manage": true}
	for _, p := range perms {
		delete(want, p)
	}
	if len(want) != 0 {
		t.Fatalf("member perms after team-role = %v, missing %v", perms, want)
	}

	// RESOLVE_UNION: a direct grant (via UpdateAdminUser, preserving the team
	// membership) adds to the set (deduplicated).
	if _, err := repo.UpdateAdminUser(ctx, member.ID, memberEmail, "Rolf", "Rework", string(core.StateActive), nil, []string{group.ID}, []string{"report.export"}); err != nil {
		t.Fatalf("UpdateAdminUser(direct grant) failed: %v", err)
	}
	perms, err = repo.ListPermissionsByUser(ctx, member.ID)
	if err != nil {
		t.Fatalf("ListPermissionsByUser failed: %v", err)
	}
	got := map[string]bool{}
	for _, p := range perms {
		got[p] = true
	}
	for _, wantCode := range []string{"dashboard.view", "tools.manage", "report.export"} {
		if !got[wantCode] {
			t.Errorf("union missing %s (perms = %v)", wantCode, perms)
		}
	}

	// RESOLVE_REVOKE_VIA_GROUP: removing the role from the team drops it from
	// the member on the next resolution.
	if _, err := repo.ReplaceUserGroupRoles(ctx, group.ID, nil); err != nil {
		t.Fatalf("ReplaceUserGroupRoles(remove) failed: %v", err)
	}
	perms, err = repo.ListPermissionsByUser(ctx, member.ID)
	if err != nil {
		t.Fatalf("ListPermissionsByUser failed: %v", err)
	}
	for _, p := range perms {
		if p == "tools.manage" || p == "dashboard.view" {
			t.Fatalf("team role still inherited after removal: %v", perms)
		}
	}

	// GROUP_ROLE_UNKNOWN: an unknown group maps to ErrUserGroupNotFound.
	if _, err := repo.ReplaceUserGroupRoles(ctx, "00000000-0000-0000-0000-000000000000", []string{role.ID}); !errors.Is(err, core.ErrUserGroupNotFound) {
		t.Errorf("ReplaceUserGroupRoles(unknown group) err = %v, want ErrUserGroupNotFound", err)
	}

	// QUAL_ASSIGN_FIXED_MISSING: assigning a fixed qual without expires_at is
	// rejected.
	if err := repo.AssignQualificationToUser(ctx, member.ID, qual.ID, nil); !errors.Is(err, core.ErrQualificationExpiryRequired) {
		t.Errorf("AssignQualificationToUser(fixed, no expiry) err = %v, want ErrQualificationExpiryRequired", err)
	}

	// QUAL_ASSIGN_FIXED_OK: with a per-assignment expiry it succeeds.
	perUser := time.Now().UTC().Add(60 * 24 * time.Hour)
	if err := repo.AssignQualificationToUser(ctx, member.ID, qual.ID, &perUser); err != nil {
		t.Fatalf("AssignQualificationToUser(fixed, expiry) failed: %v", err)
	}

	// QUAL_EDIT_VALID_UNTIL: move the expiry to the past — the repo persists
	// the per-assignment override (the core derives Status from it).
	past := time.Now().UTC().Add(-1 * 24 * time.Hour)
	if err := repo.UpdateUserQualificationExpiry(ctx, member.ID, qual.ID, &past); err != nil {
		t.Fatalf("UpdateUserQualificationExpiry failed: %v", err)
	}

	detail, err := repo.GetUserDetail(ctx, member.ID)
	if err != nil {
		t.Fatalf("GetUserDetail failed: %v", err)
	}
	if len(detail.Qualifications) != 1 {
		t.Fatalf("qualifications = %d, want 1", len(detail.Qualifications))
	}
	if detail.Qualifications[0].ExpiresAt == nil || !detail.Qualifications[0].ExpiresAt.Truncate(time.Second).Equal(past.Truncate(time.Second)) {
		t.Errorf("per-assignment ExpiresAt = %v, want the past override (within a second)", detail.Qualifications[0].ExpiresAt)
	}

	// QUAL_REVOKE: revocation removes the assignment immediately.
	if err := repo.RevokeQualificationFromUser(ctx, member.ID, qual.ID); err != nil {
		t.Fatalf("RevokeQualificationFromUser failed: %v", err)
	}
	detail, err = repo.GetUserDetail(ctx, member.ID)
	if err != nil {
		t.Fatalf("GetUserDetail failed: %v", err)
	}
	if len(detail.Qualifications) != 0 {
		t.Fatalf("qualifications = %d, want 0 after revoke", len(detail.Qualifications))
	}
}

// TestPostgresPermissionCatalogMatchesBaseCodes is defined in groups_test.go;
// TestPostgresUserGroupAdministration in users_admin_test.go. The new
// `users.qualifications.manage` code (000013) is part of the base series.

// TestPostgresResolvedPermissionSources verifies the user detail's provenance
// (ResolvedPermissions) annotates every resolved code with its source (role /
// user-group / direct), multi-source supported.
func TestPostgresResolvedPermissionSources(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://gear:gear@localhost:5432/gear?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	userEmail := "rework.src." + stamp + "@gear.local"
	groupName := "rework.srcgroup." + stamp
	roleName := "rework.srcrole." + stamp

	cleanupPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("creating cleanup pool failed: %v", err)
	}
	t.Cleanup(func() { cleanupPool.Close() })
	t.Cleanup(func() {
		if _, err := cleanupPool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", userEmail); err != nil {
			t.Errorf("cleaning up user failed: %v", err)
		}
		if _, err := cleanupPool.Exec(context.Background(), "DELETE FROM user_groups WHERE lower(name) = lower($1)", groupName); err != nil {
			t.Errorf("cleaning up group failed: %v", err)
		}
		if _, err := cleanupPool.Exec(context.Background(), "DELETE FROM permission_groups WHERE lower(name) = lower($1)", roleName); err != nil {
			t.Errorf("cleaning up role failed: %v", err)
		}
	})

	u, err := repo.CreateAdminUser(ctx, userEmail, "Src", "User", string(core.StateActive), nil, nil, nil)
	if err != nil {
		t.Fatalf("CreateAdminUser failed: %v", err)
	}
	group, err := repo.CreateUserGroup(ctx, groupName, "provenance team")
	if err != nil {
		t.Fatalf("CreateUserGroup failed: %v", err)
	}
	role, err := repo.CreateGroup(ctx, roleName, "provenance role", []string{"tools.manage"})
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}

	// Direct grant + team membership with a role → tools.manage appears with
	// BOTH sources ('group' and 'direct'). Assign the team first, then the
	// direct grant via UpdateAdminUser PRESERVING the membership.
	if _, err := repo.AssignUserGroupMembers(ctx, group.ID, []string{u.ID}); err != nil {
		t.Fatalf("AssignUserGroupMembers failed: %v", err)
	}
	if _, err := repo.ReplaceUserGroupRoles(ctx, group.ID, []string{role.ID}); err != nil {
		t.Fatalf("ReplaceUserGroupRoles failed: %v", err)
	}
	if _, err := repo.UpdateAdminUser(ctx, u.ID, userEmail, "Src", "User", string(core.StateActive), nil, []string{group.ID}, []string{"tools.manage"}); err != nil {
		t.Fatalf("UpdateAdminUser(direct grant) failed: %v", err)
	}

	detail, err := repo.GetUserDetail(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUserDetail failed: %v", err)
	}
	var directCount, groupCount int
	for _, p := range detail.ResolvedPermissions {
		if p.Code != "tools.manage" {
			continue
		}
		if p.SourceKind == "direct" {
			directCount++
		}
		if p.SourceKind == "group" && strings.EqualFold(p.SourceName, groupName) {
			groupCount++
		}
	}
	if directCount < 1 || groupCount < 1 {
		t.Errorf("tools.manage sources: direct=%d group=%d, want both (provenance=%+v)", directCount, groupCount, detail.ResolvedPermissions)
	}

	// Provenance must never diverge from enforcement (review finding 2.9): the
	// code set in the provenance view equals the live ListPermissionsByUser set.
	enforced, err := repo.ListPermissionsByUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("ListPermissionsByUser failed: %v", err)
	}
	provCodes := map[string]bool{}
	for _, p := range detail.ResolvedPermissions {
		provCodes[p.Code] = true
	}
	if len(provCodes) != len(enforced) {
		t.Errorf("provenance codes = %d, enforced codes = %d — they diverged", len(provCodes), len(enforced))
	}
	for _, code := range enforced {
		if !provCodes[code] {
			t.Errorf("enforced code %s missing from provenance", code)
		}
	}
}