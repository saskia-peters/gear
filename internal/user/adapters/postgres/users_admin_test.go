package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saskia-peters/gear/internal/user/core"
)

// TestPostgresUserGroupAdministration covers the Story 2.6 persistence
// contract (AD-2/AD-6/FR-19/FR-21, AD-12) against the REAL postgres
// repository: create a user with assignment sets, list users, load the detail,
// edit (atomic delete-then-insert replacement), deactivate (+ session
// revocation), and organisational user-group CRUD + member assignment.
func TestPostgresUserGroupAdministration(t *testing.T) {
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
	email := "adminuser." + stamp + "@gear.local"
	groupName := "Gruppe Ost." + stamp

	// Cleanup with an INDEPENDENT pool (t.Cleanup runs AFTER the test's deferred
	// pool.Close(); registered first so it closes LAST — LIFO).
	cleanupPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("creating cleanup pool failed: %v", err)
	}
	t.Cleanup(func() { cleanupPool.Close() })
	t.Cleanup(func() {
		if _, err := cleanupPool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", email); err != nil {
			t.Errorf("cleaning up user %q failed: %v", email, err)
		}
		if _, err := cleanupPool.Exec(context.Background(), "DELETE FROM user_groups WHERE name = $1", groupName); err != nil {
			t.Errorf("cleaning up group %q failed: %v", groupName, err)
		}
	})

	// CREATE_VALID: a user with roles, user groups and direct grants, atomically.
	// Find the seeded 'helfende' role and seed an organisational team.
	groups, err := repo.ListGroups(ctx)
	if err != nil {
		t.Fatalf("ListGroups failed: %v", err)
	}
	var helfendeID string
	for _, g := range groups {
		if g.Name == "helfende" {
			helfendeID = g.ID
		}
	}
	if helfendeID == "" {
		t.Fatal("helfende role missing")
	}
	team, err := repo.CreateUserGroup(ctx, groupName, "Östliche Gruppe")
	if err != nil {
		t.Fatalf("CreateUserGroup failed: %v", err)
	}
	created, err := repo.CreateAdminUser(ctx, email, "Tim", "Müller", string(core.StateActive),
		[]string{helfendeID}, []string{team.ID}, []string{"report.export"})
	if err != nil {
		t.Fatalf("CreateAdminUser failed: %v", err)
	}
	if created.State != core.StateActive {
		t.Errorf("created state = %q, want active", created.State)
	}
	if created.PasswordHash != "" {
		t.Errorf("created password hash = %q, want empty (provisioned out-of-band)", created.PasswordHash)
	}

	// LIST_USERS: the new user appears with names, email and state.
	users, err := repo.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers failed: %v", err)
	}
	found := false
	for _, u := range users {
		if u.ID == created.ID {
			found = true
			if u.Vorname != "Tim" || u.Nachname != "Müller" || u.Email != email || u.Status != string(core.StateActive) {
				t.Errorf("list row = %+v, want the created user", u)
			}
		}
	}
	if !found {
		t.Fatal("created user missing from ListUsers")
	}

	// DETAIL_VALID: the detail composes roles + user groups + direct grants.
	detail, err := repo.GetUserDetail(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUserDetail failed: %v", err)
	}
	if len(detail.Roles) != 1 || detail.Roles[0].Name != "helfende" {
		t.Errorf("roles = %+v, want [helfende]", detail.Roles)
	}
	if len(detail.UserGroups) != 1 || detail.UserGroups[0].Name != groupName {
		t.Errorf("user_groups = %+v, want [%s]", detail.UserGroups, groupName)
	}
	if len(detail.DirectGrants) != 1 || detail.DirectGrants[0].Code != "report.export" {
		t.Errorf("direct_grants = %+v, want [report.export]", detail.DirectGrants)
	}

	// CREATE_DUP_EMAIL: a case-variant duplicate maps to the uniform 409.
	if _, err := repo.CreateAdminUser(ctx, strings.ToUpper(email), "Tim", "Müller", string(core.StateActive), nil, nil, nil); !errors.Is(err, core.ErrAdminUserEmailTaken) {
		t.Errorf("CreateAdminUser(dup email) err = %v, want ErrAdminUserEmailTaken", err)
	}

	// DETAIL_UNKNOWN: an unknown id maps to the uniform 404.
	if _, err := repo.GetUserDetail(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, core.ErrAdminUserNotFound) {
		t.Errorf("GetUserDetail(unknown) err = %v, want ErrAdminUserNotFound", err)
	}
	if _, err := repo.GetUserDetail(ctx, "not-a-uuid"); !errors.Is(err, core.ErrAdminUserNotFound) {
		t.Errorf("GetUserDetail(malformed) err = %v, want ErrAdminUserNotFound", err)
	}

	// EDIT_VALID: replacing the assignment sets atomically (delete-then-insert).
	// Drop helfende, add the admin group (so the resolved set changes) and keep
	// the team.
	var adminID string
	for _, g := range groups {
		if g.Name == "admin" {
			adminID = g.ID
		}
	}
	updated, err := repo.UpdateAdminUser(ctx, created.ID, email, "Tim", "Müller", string(core.StateActive),
		[]string{adminID}, []string{team.ID}, []string{})
	if err != nil {
		t.Fatalf("UpdateAdminUser failed: %v", err)
	}
	detail, err = repo.GetUserDetail(ctx, updated.ID)
	if err != nil {
		t.Fatalf("GetUserDetail after edit failed: %v", err)
	}
	if len(detail.Roles) != 1 || detail.Roles[0].Name != "admin" {
		t.Errorf("edited roles = %+v, want [admin] (helfende replaced)", detail.Roles)
	}
	if len(detail.DirectGrants) != 0 {
		t.Errorf("edited direct_grants = %+v, want [] (report.export removed)", detail.DirectGrants)
	}
	if len(detail.UserGroups) != 1 {
		t.Errorf("edited user_groups = %+v, want the team kept", detail.UserGroups)
	}

	// EDIT_UNKNOWN: an unknown id maps to the uniform 404 (never 409 even with a
	// taken email).
	if _, err := repo.UpdateAdminUser(ctx, "00000000-0000-0000-0000-000000000000", "admin.1@gear.local", "A", "B", string(core.StateActive), nil, nil, nil); !errors.Is(err, core.ErrAdminUserNotFound) {
		t.Errorf("UpdateAdminUser(unknown, taken email) err = %v, want ErrAdminUserNotFound", err)
	}

	// EDIT_DUP_EMAIL: an email held by ANOTHER account maps to the uniform 409.
	if _, err := repo.UpdateAdminUser(ctx, created.ID, "admin.1@gear.local", "Tim", "Müller", string(core.StateActive), nil, nil, nil); !errors.Is(err, core.ErrAdminUserEmailTaken) {
		t.Errorf("UpdateAdminUser(dup) err = %v, want ErrAdminUserEmailTaken", err)
	}

	// EDIT_UNKNOWN_ROLE/GROUP/GRANT: nonexistent references map to 400 sentinels.
	if _, err := repo.UpdateAdminUser(ctx, created.ID, email, "Tim", "Müller", string(core.StateActive), []string{"00000000-0000-0000-0000-000000000000"}, nil, nil); !errors.Is(err, core.ErrAdminUserUnknownRole) {
		t.Errorf("UpdateAdminUser(unknown role) err = %v, want ErrAdminUserUnknownRole", err)
	}
	if _, err := repo.UpdateAdminUser(ctx, created.ID, email, "Tim", "Müller", string(core.StateActive), nil, []string{"00000000-0000-0000-0000-000000000000"}, nil); !errors.Is(err, core.ErrAdminUserUnknownUserGroup) {
		t.Errorf("UpdateAdminUser(unknown group) err = %v, want ErrAdminUserUnknownUserGroup", err)
	}
	if _, err := repo.UpdateAdminUser(ctx, created.ID, email, "Tim", "Müller", string(core.StateActive), nil, nil, []string{"bogus.code"}); !errors.Is(err, core.ErrUnknownPermissionCode) {
		t.Errorf("UpdateAdminUser(unknown grant) err = %v, want ErrUnknownPermissionCode", err)
	}

	// ATOMIC: an edit that fails the role check leaves the user FULLY unchanged
	// (the whole transaction rolls back).
	if _, err := repo.UpdateAdminUser(ctx, created.ID, email, "Tim", "Müller", string(core.StateActive), []string{"00000000-0000-0000-0000-000000000000"}, nil, nil); !errors.Is(err, core.ErrAdminUserUnknownRole) {
		t.Fatalf("UpdateAdminUser(bad role) err = %v, want ErrAdminUserUnknownRole", err)
	}
	detail, err = repo.GetUserDetail(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUserDetail after failed edit failed: %v", err)
	}
	if len(detail.Roles) != 1 || detail.Roles[0].Name != "admin" {
		t.Errorf("roles changed despite failed edit: %+v", detail.Roles)
	}

	// DEACTIVATE_VALID: an active user flips to deactivated; the resolved set
	// stays reachable but the account can no longer authenticate (session
	// validation rejects non-active states, Story 1.4).
	deactivated, err := repo.DeactivateUser(ctx, created.ID)
	if err != nil {
		t.Fatalf("DeactivateUser failed: %v", err)
	}
	if deactivated.State != core.StateDeactivated {
		t.Errorf("deactivated state = %q, want deactivated", deactivated.State)
	}
	// DEACTIVATE_NONACTIVE: a deactivated user cannot be deactivated again.
	if _, err := repo.DeactivateUser(ctx, created.ID); !errors.Is(err, core.ErrUserNotActiveForDeactivate) {
		t.Errorf("DeactivateUser(again) err = %v, want ErrUserNotActiveForDeactivate", err)
	}
	// DEACTIVATE_UNKNOWN: an unknown id maps to the uniform 404.
	if _, err := repo.DeactivateUser(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, core.ErrAdminUserNotFound) {
		t.Errorf("DeactivateUser(unknown) err = %v, want ErrAdminUserNotFound", err)
	}

	// GROUP_LIST: the created team is listable, ordered by name.
	allTeams, err := repo.ListUserGroups(ctx)
	if err != nil {
		t.Fatalf("ListUserGroups failed: %v", err)
	}
	foundTeam := false
	for _, g := range allTeams {
		if g.ID == team.ID {
			foundTeam = true
		}
	}
	if !foundTeam {
		t.Fatal("created user group missing from the list")
	}

	// GROUP_DUP_NAME: a case-variant duplicate maps to the uniform 409.
	if _, err := repo.CreateUserGroup(ctx, strings.ToUpper(groupName), ""); !errors.Is(err, core.ErrUserGroupNameTaken) {
		t.Errorf("CreateUserGroup(dup) err = %v, want ErrUserGroupNameTaken", err)
	}

	// GROUP_ASSIGN: replacing the team's members persists the new set.
	volunteer, err := repo.CreateRegisteredUser(ctx, "member."+stamp+"@gear.local", "Frei Willig", "Frei", "Willig", "$argon2id$v=19$dummyhash")
	if err != nil {
		t.Fatalf("CreateRegisteredUser failed: %v", err)
	}
	assigned, err := repo.AssignUserGroupMembers(ctx, team.ID, []string{volunteer.ID})
	if err != nil {
		t.Fatalf("AssignUserGroupMembers failed: %v", err)
	}
	if assigned.ID != team.ID || assigned.Name != groupName {
		t.Errorf("assigned group = %+v, want %s", assigned, groupName)
	}
	// The volunteer now shows the team on their detail; the created user lost it.
	volunteerDetail, err := repo.GetUserDetail(ctx, volunteer.ID)
	if err != nil {
		t.Fatalf("GetUserDetail(volunteer) failed: %v", err)
	}
	if len(volunteerDetail.UserGroups) != 1 || volunteerDetail.UserGroups[0].Name != groupName {
		t.Errorf("volunteer user_groups = %+v, want [%s]", volunteerDetail.UserGroups, groupName)
	}
	createdDetail, err := repo.GetUserDetail(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUserDetail(created) failed: %v", err)
	}
	if len(createdDetail.UserGroups) != 0 {
		t.Errorf("created user_groups after replace = %+v, want []", createdDetail.UserGroups)
	}

	// GROUP_ASSIGN_UNKNOWN: an unknown group maps to the uniform 404.
	if _, err := repo.AssignUserGroupMembers(ctx, "00000000-0000-0000-0000-000000000000", []string{volunteer.ID}); !errors.Is(err, core.ErrUserGroupNotFound) {
		t.Errorf("AssignUserGroupMembers(unknown group) err = %v, want ErrUserGroupNotFound", err)
	}
	// GROUP_ASSIGN_UNKNOWN_MEMBER: an unknown member maps to the uniform 400.
	if _, err := repo.AssignUserGroupMembers(ctx, team.ID, []string{"00000000-0000-0000-0000-000000000000"}); !errors.Is(err, core.ErrUserGroupMemberUnknown) {
		t.Errorf("AssignUserGroupMembers(unknown member) err = %v, want ErrUserGroupMemberUnknown", err)
	}
}

// TestPostgresAdminUserLiveResolution proves the IMMEDIATE_EFFECT contract
// (AD-2/FR-21): editing an active user's roles changes their RESOLVED
// permission set on the very next resolution — no re-login, no cache. The
// admin group is never touched (a fresh custom role is created and cleaned up).
func TestPostgresAdminUserLiveResolution(t *testing.T) {
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
	email := "live." + stamp + "@gear.local"
	roleName := "livetest." + stamp

	cleanupPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("creating cleanup pool failed: %v", err)
	}
	t.Cleanup(func() { cleanupPool.Close() })
	t.Cleanup(func() {
		if _, err := cleanupPool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", email); err != nil {
			t.Errorf("cleaning up user %q failed: %v", email, err)
		}
		if _, err := cleanupPool.Exec(context.Background(), "DELETE FROM permission_groups WHERE name = $1", roleName); err != nil {
			t.Errorf("cleaning up role %q failed: %v", roleName, err)
		}
	})

	volunteer, err := repo.CreateAdminUser(ctx, email, "Frei", "Willig", string(core.StateActive), nil, nil, nil)
	if err != nil {
		t.Fatalf("CreateAdminUser failed: %v", err)
	}

	resolve := func() []string {
		t.Helper()
		perms, err := repo.ListPermissionsByUser(ctx, volunteer.ID)
		if err != nil {
			t.Fatalf("ListPermissionsByUser failed: %v", err)
		}
		return perms
	}
	if len(resolve()) != 0 {
		t.Fatalf("initial resolved set = %v, want []", resolve())
	}

	// Create a custom role carrying report.export and assign it to the user.
	role, err := repo.CreateGroup(ctx, roleName, "Livetest", []string{"report.export"})
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	if _, err := repo.UpdateAdminUser(ctx, volunteer.ID, email, "Frei", "Willig", string(core.StateActive), []string{role.ID}, nil, nil); err != nil {
		t.Fatalf("UpdateAdminUser failed: %v", err)
	}
	after := resolve()
	if !sameCodeSet(after, []string{"report.export"}) {
		t.Fatalf("resolved after role grant = %v, want [report.export]", after)
	}

	// Remove the role: the very next resolution already reflects the revocation.
	if _, err := repo.UpdateAdminUser(ctx, volunteer.ID, email, "Frei", "Willig", string(core.StateActive), nil, nil, nil); err != nil {
		t.Fatalf("UpdateAdminUser(remove role) failed: %v", err)
	}
	final := resolve()
	if len(final) != 0 {
		t.Errorf("resolved after role removal = %v, want [] (immediate effect)", final)
	}
}

// TestPostgresAdminUserAuditSeam pins that the qualification persistence seams
// (ListQualifications/AddQualificationToUser/RemoveQualificationFromUser) exist
// and behave: Story 2.7 edits assignments; Story 2.6 only displays. This keeps
// the shared store honest until 2.7 wires the surface.
func TestPostgresAdminUserQualificationSeam(t *testing.T) {
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
	email := "qual." + stamp + "@gear.local"
	qualName := "qual." + stamp

	cleanupPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("creating cleanup pool failed: %v", err)
	}
	t.Cleanup(func() { cleanupPool.Close() })
	t.Cleanup(func() {
		if _, err := cleanupPool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", email); err != nil {
			t.Errorf("cleaning up user %q failed: %v", email, err)
		}
		if _, err := cleanupPool.Exec(context.Background(), "DELETE FROM qualifications WHERE name = $1", qualName); err != nil {
			t.Errorf("cleaning up qualification %q failed: %v", qualName, err)
		}
	})

	user, err := repo.CreateAdminUser(ctx, email, "Frei", "Willig", string(core.StateActive), nil, nil, nil)
	if err != nil {
		t.Fatalf("CreateAdminUser failed: %v", err)
	}

	// Seed a qualification row directly (the vocabulary is not yet managed by
	// an admin surface — Story 2.7 — so the test inserts via SQL).
	var qualID string
	if _, err := pool.Exec(ctx, "INSERT INTO qualifications (name, description, expiry_kind) VALUES ($1, $2, 'unlimited') RETURNING id", qualName, "Test"); err != nil {
		t.Fatalf("seeding qualification failed: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT id FROM qualifications WHERE name = $1", qualName).Scan(&qualID); err != nil {
		t.Fatalf("reading qualification id failed: %v", err)
	}

	q := New(pool)
	if err := q.AddQualificationToUser(ctx, AddQualificationToUserParams{UserID: mustUUID(t, user.ID), QualificationID: mustUUID(t, qualID)}); err != nil {
		t.Fatalf("AddQualificationToUser failed: %v", err)
	}
	detail, err := repo.GetUserDetail(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserDetail failed: %v", err)
	}
	if len(detail.Qualifications) != 1 || detail.Qualifications[0].Name != qualName || detail.Qualifications[0].ExpiryKind != core.QualificationExpiryUnlimited {
		t.Errorf("qualifications = %+v, want the seeded unlimited qualification", detail.Qualifications)
	}
	if err := q.RemoveQualificationFromUser(ctx, RemoveQualificationFromUserParams{UserID: mustUUID(t, user.ID), QualificationID: mustUUID(t, qualID)}); err != nil {
		t.Fatalf("RemoveQualificationFromUser failed: %v", err)
	}
	detail, err = repo.GetUserDetail(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserDetail after removal failed: %v", err)
	}
	if len(detail.Qualifications) != 0 {
		t.Errorf("qualifications after removal = %+v, want []", detail.Qualifications)
	}
}

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u, err := uuidFromString(s)
	if err != nil {
		t.Fatalf("parsing uuid %q failed: %v", s, err)
	}
	return u
}

// TestPostgresDeactivateRevokesSessions pins the finding-3 verification gap:
// deactivation must revoke the target's sessions in the same transaction, so
// "→ Sofort kein Login" holds even for an already-issued session token. If the
// DeleteSessionsByUser call were removed, this test would fail.
func TestPostgresDeactivateRevokesSessions(t *testing.T) {
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
	email := "sessrev." + stamp + "@gear.local"
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", email)
	})

	user, err := repo.CreateAdminUser(ctx, email, "Frei", "Willig", string(core.StateActive), nil, nil, nil)
	if err != nil {
		t.Fatalf("CreateAdminUser failed: %v", err)
	}

	// Issue a session for the user and prove it resolves BEFORE deactivation.
	tokenHash := "sessrev." + stamp + ".hash"
	if _, err := repo.CreateSession(ctx, user.ID, tokenHash, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if _, err := repo.GetSessionByTokenHash(ctx, tokenHash); err != nil {
		t.Fatalf("session must resolve before deactivation: %v", err)
	}

	if _, err := repo.DeactivateUser(ctx, user.ID); err != nil {
		t.Fatalf("DeactivateUser failed: %v", err)
	}

	// The session is GONE after deactivation (finding 3): the row was deleted in
	// the same transaction as the state flip.
	if _, err := repo.GetSessionByTokenHash(ctx, tokenHash); err == nil {
		t.Error("session still resolves after deactivation — DeleteSessionsByUser was not applied")
	}
}

// TestPostgresUserGroupDeleteAndMembers covers the finding-5/6 persistence
// contract: ListUserGroupMembers returns the current member ids, DeleteUserGroup
// removes a team (and its memberships) atomically, and both map an unknown id
// to the uniform 404.
func TestPostgresUserGroupDeleteAndMembers(t *testing.T) {
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
	groupName := "Gruppe West." + stamp
	memberEmail := "memberdel." + stamp + "@gear.local"

	cleanupPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("creating cleanup pool failed: %v", err)
	}
	t.Cleanup(func() { cleanupPool.Close() })
	t.Cleanup(func() {
		if _, err := cleanupPool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", memberEmail); err != nil {
			t.Errorf("cleaning up member %q failed: %v", memberEmail, err)
		}
		if _, err := cleanupPool.Exec(context.Background(), "DELETE FROM user_groups WHERE name = $1", groupName); err != nil {
			t.Errorf("cleaning up group %q failed: %v", groupName, err)
		}
	})

	group, err := repo.CreateUserGroup(ctx, groupName, "Westliche Gruppe")
	if err != nil {
		t.Fatalf("CreateUserGroup failed: %v", err)
	}
	member, err := repo.CreateRegisteredUser(ctx, memberEmail, "Frei Willig", "Frei", "Willig", "$argon2id$v=19$dummyhash")
	if err != nil {
		t.Fatalf("CreateRegisteredUser failed: %v", err)
	}

	// MEMBERS_LIST before assignment: empty.
	members, err := repo.ListUserGroupMembers(ctx, group.ID)
	if err != nil {
		t.Fatalf("ListUserGroupMembers failed: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("members before assignment = %v, want []", members)
	}

	// Assign the member and read the set back (finding 6 pre-check source).
	if _, err := repo.AssignUserGroupMembers(ctx, group.ID, []string{member.ID}); err != nil {
		t.Fatalf("AssignUserGroupMembers failed: %v", err)
	}
	members, err = repo.ListUserGroupMembers(ctx, group.ID)
	if err != nil {
		t.Fatalf("ListUserGroupMembers after assign failed: %v", err)
	}
	if len(members) != 1 || members[0] != member.ID {
		t.Errorf("members after assign = %v, want [%s]", members, member.ID)
	}

	// MEMBERS_UNKNOWN: an unknown group maps to the uniform 404.
	if _, err := repo.ListUserGroupMembers(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, core.ErrUserGroupNotFound) {
		t.Errorf("ListUserGroupMembers(unknown) err = %v, want ErrUserGroupNotFound", err)
	}

	// DELETE: the group disappears and its memberships cascade.
	if err := repo.DeleteUserGroup(ctx, group.ID); err != nil {
		t.Fatalf("DeleteUserGroup failed: %v", err)
	}
	groups, err := repo.ListUserGroups(ctx)
	if err != nil {
		t.Fatalf("ListUserGroups failed: %v", err)
	}
	for _, g := range groups {
		if g.ID == group.ID {
			t.Error("deleted group still listed")
		}
	}
	// The member's memberships are gone too.
	detail, err := repo.GetUserDetail(ctx, member.ID)
	if err != nil {
		t.Fatalf("GetUserDetail(member) failed: %v", err)
	}
	if len(detail.UserGroups) != 0 {
		t.Errorf("member user_groups after group delete = %+v, want [] (cascade)", detail.UserGroups)
	}

	// DELETE_UNKNOWN: deleting an unknown group maps to the uniform 404.
	if err := repo.DeleteUserGroup(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, core.ErrUserGroupNotFound) {
		t.Errorf("DeleteUserGroup(unknown) err = %v, want ErrUserGroupNotFound", err)
	}
}