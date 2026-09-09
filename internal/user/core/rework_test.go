package core

import (
	"context"
	"testing"
	"time"
)

// Core unit tests for Spec 2.9 (Admin Rework Effort 1): user-group ROLE
// assignment (ListUserGroupRoles / AssignUserGroupRoles) and per-user
// qualification assignment with valid-until (AssignUserQualification /
// RevokeUserQualification / UpdateUserQualificationExpiry). These cover the
// I/O matrix rows GROUP_ROLE_*, QUAL_ASSIGN_*, QUAL_EDIT_*, QUAL_UNLIMITED_*,
// QUAL_STATUS_*, and FUEHRENDE_READONLY at the domain layer (the permission
// gate re-check is defense-in-depth on top of the HTTP gateway).

// reworkRepo builds a mockRepo with an admin actor (user_groups.manage +
// users.qualifications.manage), a schirrmeister actor (users.view +
// users.qualifications.manage, NO user_groups.manage), a helfende actor (no
// admin codes), a user group, and a fixed + unlimited qualification.
func reworkRepo() *mockRepo {
	repo := rolesRepo()
	repo.users["admin@gear.local"].Email = "admin@gear.local"
	repo.perms["u-admin"] = []string{
		UserViewPermission, UserManagePermission, UserApprovePermission,
		UserGroupsManagePermission, UsersQualificationsManagePermission,
		RoleCreatePermission, RoleEditPermission, RoleAssignPermission,
	}

	// A fuehrende/schirrmeister-style actor: users.view + the new
	// users.qualifications.manage, but NOT user_groups.manage.
	repo.seedUser("u-schirr", "schirr@gear.local", "Sven", "Schirr", StateActive)
	repo.perms["u-schirr"] = []string{UserViewPermission, UsersQualificationsManagePermission}

	// A plain volunteer with no admin codes.
	repo.seedUser("u-helf", "helfende@gear.local", "Hella", "Helfen", StateActive)
	repo.perms["u-helf"] = []string{"dashboard.view", "inspection.submit"}

	repo.seedUserGroup("ug-ost", "Gruppe Ost", "Östliche Gruppe", "u-schirr")
	repo.seedRoleGroup("g-schirr", "schirrmeister", "Base role: tool caretaker", true,
		[]string{"dashboard.view", "inspection.submit", "tools.manage", "tool_types.manage"})

	fixedExpiry := time.Now().UTC().Add(90 * 24 * time.Hour)
	repo.qualifications["q-fixed"] = &QualificationAssignment{
		ID: "q-fixed", Name: "Kettensäge", Description: "Kettensägen-Führerschein",
		ExpiryKind: QualificationExpiryFixed, ExpiresAt: &fixedExpiry,
	}
	repo.qualifications["q-unlim"] = &QualificationAssignment{
		ID: "q-unlim", Name: "Erste Hilfe", Description: "Erste-Hilfe-Kurs",
		ExpiryKind: QualificationExpiryUnlimited,
	}

	return repo
}

func TestListUserGroupRolesValid(t *testing.T) {
	// GROUP_ROLE_LIST: a user_groups.manage holder lists a group's roles.
	repo := reworkRepo()
	svc := usersAdminService(t, repo)

	roles, err := svc.ListUserGroupRoles(context.Background(), adminActor(repo), "ug-ost")
	if err != nil {
		t.Fatalf("ListUserGroupRoles failed: %v", err)
	}
	if len(roles) != 0 {
		t.Fatalf("roles = %d, want 0 (no roles assigned yet)", len(roles))
	}
}

func TestListUserGroupRolesUnknown(t *testing.T) {
	// GROUP_ROLE_UNKNOWN: an unknown group maps to ErrUserGroupNotFound.
	repo := reworkRepo()
	svc := usersAdminService(t, repo)

	if _, err := svc.ListUserGroupRoles(context.Background(), adminActor(repo), "ug-nope"); err != ErrUserGroupNotFound {
		t.Fatalf("err = %v, want ErrUserGroupNotFound", err)
	}
}

func TestListUserGroupRolesForbidden(t *testing.T) {
	// GROUP_ROLE_FORBIDDEN: a caller without user_groups.manage is denied.
	repo := reworkRepo()
	svc := usersAdminService(t, repo)

	if _, err := svc.ListUserGroupRoles(context.Background(), repo.users["schirr@gear.local"], "ug-ost"); err != ErrForbidden {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestAssignUserGroupRolesValid(t *testing.T) {
	// GROUP_ROLE_ASSIGN: assigning roles to a group persists them; members
	// inherit them via the live resolution (RESOLVE_VIA_GROUP).
	repo := reworkRepo()
	svc := usersAdminService(t, repo)

	roles, err := svc.AssignUserGroupRoles(context.Background(), adminActor(repo), "ug-ost", []string{"g-schirr"})
	if err != nil {
		t.Fatalf("AssignUserGroupRoles failed: %v", err)
	}
	if len(roles) != 1 || roles[0].ID != "g-schirr" {
		t.Fatalf("roles = %+v, want the schirrmeister role", roles)
	}

	// The member u-schirr (in Gruppe Ost) now inherits the role's codes.
	perms, err := repo.ListPermissionsByUser(context.Background(), "u-schirr")
	if err != nil {
		t.Fatalf("ListPermissionsByUser failed: %v", err)
	}
	joined := make(map[string]bool, len(perms))
	for _, p := range perms {
		joined[p] = true
	}
	for _, want := range []string{"dashboard.view", "tools.manage", "tool_types.manage"} {
		if !joined[want] {
			t.Errorf("inherited set missing %s (perms = %v)", want, perms)
		}
	}
}

func TestAssignUserGroupRolesUnknownRole(t *testing.T) {
	// GROUP_ROLE_ASSIGN unknown role: maps to ErrAdminUserUnknownRole.
	repo := reworkRepo()
	svc := usersAdminService(t, repo)

	if _, err := svc.AssignUserGroupRoles(context.Background(), adminActor(repo), "ug-ost", []string{"g-nope"}); err != ErrAdminUserUnknownRole {
		t.Fatalf("err = %v, want ErrAdminUserUnknownRole", err)
	}
}

func TestAssignUserGroupRolesRemoveRevokes(t *testing.T) {
	// RESOLVE_REVOKE_VIA_GROUP: removing a role from a group revokes it from
	// members on the next resolution.
	repo := reworkRepo()
	svc := usersAdminService(t, repo)

	if _, err := svc.AssignUserGroupRoles(context.Background(), adminActor(repo), "ug-ost", []string{"g-schirr"}); err != nil {
		t.Fatalf("assign failed: %v", err)
	}
	if _, err := svc.AssignUserGroupRoles(context.Background(), adminActor(repo), "ug-ost", nil); err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	perms, err := repo.ListPermissionsByUser(context.Background(), "u-schirr")
	if err != nil {
		t.Fatalf("ListPermissionsByUser failed: %v", err)
	}
	for _, p := range perms {
		if p == "tools.manage" {
			t.Fatalf("tools.manage still inherited after role removal: %v", perms)
		}
	}
}

func TestAssignUserGroupRolesForbidden(t *testing.T) {
	// GROUP_ROLE_FORBIDDEN: a schirrmeister without user_groups.manage cannot
	// assign roles.
	repo := reworkRepo()
	svc := usersAdminService(t, repo)

	if _, err := svc.AssignUserGroupRoles(context.Background(), repo.users["schirr@gear.local"], "ug-ost", []string{"g-schirr"}); err != ErrForbidden {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestAssignUserQualificationUnlimited(t *testing.T) {
	// QUAL_ASSIGN_UNLIMITED: an unlimited qualification needs no expires_at
	// and is Unbegrenzt forever.
	repo := reworkRepo()
	svc := usersAdminService(t, repo)
	actor := repo.users["schirr@gear.local"] // users.qualifications.manage holder

	res, err := svc.AssignUserQualification(context.Background(), actor, "u-helf", "q-unlim", nil)
	if err != nil {
		t.Fatalf("AssignUserQualification failed: %v", err)
	}
	if res.Message == "" || res.QualificationID != "q-unlim" {
		t.Fatalf("res = %+v, want a message + qualification id", res)
	}

	detail, err := svc.GetUserDetail(context.Background(), actor, "u-helf")
	if err != nil {
		t.Fatalf("GetUserDetail failed: %v", err)
	}
	if len(detail.Qualifications) != 1 {
		t.Fatalf("qualifications = %d, want 1", len(detail.Qualifications))
	}
	if detail.Qualifications[0].Status != QualificationStatusUnlimited {
		t.Fatalf("status = %q, want Unbegrenzt", detail.Qualifications[0].Status)
	}
}

func TestAssignUserQualificationFixedRequiresExpiry(t *testing.T) {
	// QUAL_ASSIGN_FIXED_MISSING: a fixed qualification REQUIRES expires_at.
	repo := reworkRepo()
	svc := usersAdminService(t, repo)
	actor := repo.users["schirr@gear.local"]

	if _, err := svc.AssignUserQualification(context.Background(), actor, "u-helf", "q-fixed", nil); err != ErrQualificationExpiryRequired {
		t.Fatalf("err = %v, want ErrQualificationExpiryRequired", err)
	}
}

func TestAssignUserQualificationFixedWithExpiry(t *testing.T) {
	// QUAL_ASSIGN_FIXED_OK: a fixed qualification with expires_at is accepted
	// and its status derives from the per-assignment date.
	repo := reworkRepo()
	svc := usersAdminService(t, repo)
	actor := repo.users["schirr@gear.local"]

	future := time.Now().UTC().Add(60 * 24 * time.Hour)
	if _, err := svc.AssignUserQualification(context.Background(), actor, "u-helf", "q-fixed", &future); err != nil {
		t.Fatalf("AssignUserQualification failed: %v", err)
	}

	detail, err := svc.GetUserDetail(context.Background(), actor, "u-helf")
	if err != nil {
		t.Fatalf("GetUserDetail failed: %v", err)
	}
	if detail.Qualifications[0].Status != QualificationStatusValid {
		t.Fatalf("status = %q, want Gültig", detail.Qualifications[0].Status)
	}
}

func TestAssignUserQualificationUnlimitedRejectsExpiry(t *testing.T) {
	// QUAL_UNLIMITED_NEVER_EXPIRES: an unlimited qualification must NOT carry
	// a per-user expires_at.
	repo := reworkRepo()
	svc := usersAdminService(t, repo)
	actor := repo.users["schirr@gear.local"]

	future := time.Now().UTC().Add(60 * 24 * time.Hour)
	if _, err := svc.AssignUserQualification(context.Background(), actor, "u-helf", "q-unlim", &future); err != ErrQualificationInvalidExpiresAt {
		t.Fatalf("err = %v, want ErrQualificationInvalidExpiresAt", err)
	}
}

func TestAssignUserQualificationForbidden(t *testing.T) {
	// QUAL_ASSIGN_FORBIDDEN: a caller without users.qualifications.manage
	// cannot assign.
	repo := reworkRepo()
	svc := usersAdminService(t, repo)

	if _, err := svc.AssignUserQualification(context.Background(), repo.users["helfende@gear.local"], "u-helf", "q-unlim", nil); err != ErrForbidden {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestAssignUserQualificationUnknownUser(t *testing.T) {
	// QUAL_ASSIGN_UNKNOWN_USER: an unknown user maps to 404.
	repo := reworkRepo()
	svc := usersAdminService(t, repo)
	actor := repo.users["schirr@gear.local"]

	if _, err := svc.AssignUserQualification(context.Background(), actor, "u-nope", "q-unlim", nil); err != ErrAdminUserNotFound {
		t.Fatalf("err = %v, want ErrAdminUserNotFound", err)
	}
}

func TestRevokeUserQualificationValid(t *testing.T) {
	// QUAL_REVOKE: revocation is immediate (gone from the user detail).
	repo := reworkRepo()
	svc := usersAdminService(t, repo)
	actor := repo.users["schirr@gear.local"]

	if _, err := svc.AssignUserQualification(context.Background(), actor, "u-helf", "q-unlim", nil); err != nil {
		t.Fatalf("assign failed: %v", err)
	}
	if _, err := svc.RevokeUserQualification(context.Background(), actor, "u-helf", "q-unlim"); err != nil {
		t.Fatalf("revoke failed: %v", err)
	}

	detail, err := svc.GetUserDetail(context.Background(), actor, "u-helf")
	if err != nil {
		t.Fatalf("GetUserDetail failed: %v", err)
	}
	if len(detail.Qualifications) != 0 {
		t.Fatalf("qualifications = %d, want 0 after revoke", len(detail.Qualifications))
	}
}

func TestRevokeUserQualificationForbidden(t *testing.T) {
	// QUAL_REVOKE_FORBIDDEN: a caller without the code cannot revoke.
	repo := reworkRepo()
	svc := usersAdminService(t, repo)

	if _, err := svc.RevokeUserQualification(context.Background(), repo.users["helfende@gear.local"], "u-helf", "q-unlim"); err != ErrForbidden {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestUpdateUserQualificationExpiryValid(t *testing.T) {
	// QUAL_EDIT_VALID_UNTIL: editing a user's per-assignment expires_at
	// persists and updates the status on the next fetch.
	repo := reworkRepo()
	svc := usersAdminService(t, repo)
	actor := repo.users["schirr@gear.local"]

	future := time.Now().UTC().Add(60 * 24 * time.Hour)
	if _, err := svc.AssignUserQualification(context.Background(), actor, "u-helf", "q-fixed", &future); err != nil {
		t.Fatalf("assign failed: %v", err)
	}

	past := time.Now().UTC().Add(-1 * 24 * time.Hour)
	if _, err := svc.UpdateUserQualificationExpiry(context.Background(), actor, "u-helf", "q-fixed", &past); err != nil {
		t.Fatalf("UpdateUserQualificationExpiry failed: %v", err)
	}

	detail, err := svc.GetUserDetail(context.Background(), actor, "u-helf")
	if err != nil {
		t.Fatalf("GetUserDetail failed: %v", err)
	}
	if detail.Qualifications[0].Status != QualificationStatusExpired {
		t.Fatalf("status = %q, want Abgelaufen after moving expiry to the past", detail.Qualifications[0].Status)
	}
}

func TestUpdateUserQualificationExpiryNotFound(t *testing.T) {
	// QUAL_EDIT_NOT_FOUND: updating an unassigned pair maps to 404.
	repo := reworkRepo()
	svc := usersAdminService(t, repo)
	actor := repo.users["schirr@gear.local"]

	future := time.Now().UTC().Add(60 * 24 * time.Hour)
	if _, err := svc.UpdateUserQualificationExpiry(context.Background(), actor, "u-helf", "q-fixed", &future); err != ErrQualificationAssignmentNotFound {
		t.Fatalf("err = %v, want ErrQualificationAssignmentNotFound", err)
	}
}

func TestUpdateUserQualificationExpiryFixedRequiresDate(t *testing.T) {
	// QUAL_EDIT_FIXED_NIL (retro finding F13): a fixed qualification's per-user
	// valid-until cannot be cleared to permanent-"valid" — updating with a nil
	// date is rejected (400), the same rule that applies at assign time.
	repo := reworkRepo()
	svc := usersAdminService(t, repo)
	actor := repo.users["schirr@gear.local"]
	repo.userQualifications["u-helf"] = []string{"q-fixed"}

	if _, err := svc.UpdateUserQualificationExpiry(context.Background(), actor, "u-helf", "q-fixed", nil); err != ErrQualificationExpiryRequired {
		t.Fatalf("UpdateUserQualificationExpiry(fixed, nil) err = %v, want ErrQualificationExpiryRequired", err)
	}
	// The assignment still exists with a date (never silently cleared).
	detail, err := svc.GetUserDetail(context.Background(), actor, "u-helf")
	if err != nil {
		t.Fatalf("GetUserDetail failed: %v", err)
	}
	if len(detail.Qualifications) != 1 || detail.Qualifications[0].ExpiresAt == nil {
		t.Errorf("assignment must keep its expiry after a rejected clear, got %+v", detail.Qualifications)
	}
}

func TestFuehrendeSchirrmeisterReadOnlyDetail(t *testing.T) {
	// FUEHRENDE_READONLY: a fuehrende/schirrmeister (users.view +
	// users.qualifications.manage, no users.manage) can VIEW the detail and
	// assign qualifications, but CANNOT deactivate or edit the user.
	repo := reworkRepo()
	svc := usersAdminService(t, repo)
	actor := repo.users["schirr@gear.local"]

	// View works.
	detail, err := svc.GetUserDetail(context.Background(), actor, "u-helf")
	if err != nil {
		t.Fatalf("GetUserDetail failed: %v", err)
	}
	if detail.Email != "helfende@gear.local" {
		t.Fatalf("detail email = %q, want the helfende user", detail.Email)
	}

	// Assign qualification works (users.qualifications.manage).
	if _, err := svc.AssignUserQualification(context.Background(), actor, "u-helf", "q-unlim", nil); err != nil {
		t.Fatalf("assign qualification failed: %v", err)
	}

	// Deactivate is forbidden (users.manage only).
	if _, err := svc.DeactivateUser(context.Background(), actor, "u-helf", true); err != ErrForbidden {
		t.Fatalf("DeactivateUser err = %v, want ErrForbidden (read-only detail for non-admin)", err)
	}
}

func TestRevokeUserQualificationUnassignedNotFound(t *testing.T) {
	// QUAL_REVOKE_UNASSIGNED: revoking an unassigned pair maps to 404 (review
	// finding 2.9 — consistent with UpdateUserQualificationExpiry).
	repo := reworkRepo()
	svc := usersAdminService(t, repo)
	actor := repo.users["schirr@gear.local"]

	if _, err := svc.RevokeUserQualification(context.Background(), actor, "u-helf", "q-unlim"); err != ErrQualificationAssignmentNotFound {
		t.Fatalf("err = %v, want ErrQualificationAssignmentNotFound", err)
	}
}

func TestUpdateUserQualificationExpiryOnUnlimitedRejected(t *testing.T) {
	// QUAL_EDIT_UNLIMITED_REJECTED: an unlimited qualification must never carry
	// a per-assignment expiry — updating it is rejected (review finding 2.9).
	repo := reworkRepo()
	svc := usersAdminService(t, repo)
	actor := repo.users["schirr@gear.local"]

	if _, err := svc.AssignUserQualification(context.Background(), actor, "u-helf", "q-unlim", nil); err != nil {
		t.Fatalf("assign failed: %v", err)
	}
	future := time.Now().UTC().Add(60 * 24 * time.Hour)
	if _, err := svc.UpdateUserQualificationExpiry(context.Background(), actor, "u-helf", "q-unlim", &future); err != ErrQualificationInvalidExpiresAt {
		t.Fatalf("err = %v, want ErrQualificationInvalidExpiresAt", err)
	}
}

func TestAssignUserQualificationReassignUpdatesExpiry(t *testing.T) {
	// QUAL_REASSIGN: re-assigning an already-assigned qualification UPDATES the
	// per-assignment expires_at instead of silently no-oping (review finding
	// 2.9).
	repo := reworkRepo()
	svc := usersAdminService(t, repo)
	actor := repo.users["schirr@gear.local"]

	first := time.Now().UTC().Add(60 * 24 * time.Hour)
	if _, err := svc.AssignUserQualification(context.Background(), actor, "u-helf", "q-fixed", &first); err != nil {
		t.Fatalf("first assign failed: %v", err)
	}
	second := time.Now().UTC().Add(30 * 24 * time.Hour)
	if _, err := svc.AssignUserQualification(context.Background(), actor, "u-helf", "q-fixed", &second); err != nil {
		t.Fatalf("re-assign failed: %v", err)
	}

	detail, err := svc.GetUserDetail(context.Background(), actor, "u-helf")
	if err != nil {
		t.Fatalf("GetUserDetail failed: %v", err)
	}
	if len(detail.Qualifications) != 1 {
		t.Fatalf("qualifications = %d, want 1 (no duplicate rows)", len(detail.Qualifications))
	}
	if detail.Qualifications[0].ExpiresAt == nil || !detail.Qualifications[0].ExpiresAt.Truncate(time.Second).Equal(second.Truncate(time.Second)) {
		t.Errorf("ExpiresAt = %v, want the second (renewed) expiry", detail.Qualifications[0].ExpiresAt)
	}
}

func TestListUsersStatusFilter(t *testing.T) {
	// USER_LIST_STATUS (review finding 2.9): a valid status narrows the result;
	// an invalid status maps to ErrAdminUserInvalidStatus.
	repo := reworkRepo()
	repo.seedUser("u-pending", "pending@gear.local", "Pia", "Pending", StatePendingApproval)
	repo.perms["u-pending"] = []string{}
	svc := usersAdminService(t, repo)

	active := string(StateActive)
	users, err := svc.ListUsers(context.Background(), adminActor(repo), &active)
	if err != nil {
		t.Fatalf("ListUsers(active) failed: %v", err)
	}
	for _, u := range users {
		if u.Status != active {
			t.Errorf("user %s has status %q, want active-only filter", u.Email, u.Status)
		}
	}

	pending := string(StatePendingApproval)
	users, err = svc.ListUsers(context.Background(), adminActor(repo), &pending)
	if err != nil {
		t.Fatalf("ListUsers(pending) failed: %v", err)
	}
	if len(users) != 1 || users[0].Email != "pending@gear.local" {
		t.Fatalf("pending users = %+v, want only the pending user", users)
	}

	bogus := "bogus"
	if _, err := svc.ListUsers(context.Background(), adminActor(repo), &bogus); err != ErrAdminUserInvalidStatus {
		t.Fatalf("err = %v, want ErrAdminUserInvalidStatus", err)
	}
}

// seedUser is a small helper adding a user to the mock repo (keyed by email).
func (m *mockRepo) seedUser(id, email, firstName, lastName string, state UserState) *User {
	u := &User{
		ID: id, Email: email, FirstName: firstName, LastName: lastName,
		DisplayName: firstName + " " + lastName, State: state, PasswordHash: "hashed:test",
	}
	if m.users == nil {
		m.users = make(map[string]*User)
	}
	m.users[email] = u
	return u
}