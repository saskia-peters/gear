package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// usersAdminRepo seeds the Story 2.6 I/O matrix: an active admin holding
// users.view/users.manage/users.approve + user_groups.manage, and a
// helfende volunteer attached to the 'helfende' role plus an organisational
// team (AD-12). The admin-created user's resolved set is recomputed on
// create/edit (live resolution, AD-2/FR-21).
func usersAdminRepo() *mockRepo {
	repo := rolesRepo()
	repo.users["admin@gear.local"].Email = "admin@gear.local"
	repo.perms["u-admin"] = []string{
		UserViewPermission, UserManagePermission, UserApprovePermission,
		UserGroupsManagePermission, RoleCreatePermission, RoleEditPermission, RoleAssignPermission,
	}

	repo.seedUserGroup("ug-ost", "Gruppe Ost", "Östliche Gruppe", "u-helfende")
	repo.seedRoleGroup("g-schirr", "schirrmeister", "Base role: tool caretaker", true,
		[]string{"dashboard.view", "inspection.submit", "tools.manage", "tool_types.manage"})

	// A qualification vocabulary entry (Story 2.6 display: expiry kind + status).
	// The vocabulary has NO valid-until date (2026-09-08 rework); a per-user
	// valid-until lives only on assignments.
	repo.qualifications["q-ketten"] = &QualificationAssignment{
		ID: "q-ketten", Name: "Kettensäge", Description: "Kettensägen-Führerschein",
		ExpiryKind: QualificationExpiryFixed,
	}
	repo.userQualifications["u-helfende"] = []string{"q-ketten"}

	return repo
}

func usersAdminService(t *testing.T, repo *mockRepo) *Service {
	t.Helper()
	svc, _ := newTestService(repo, &mockHasher{})
	return svc
}

func adminActor(repo *mockRepo) *User {
	return repo.users["admin@gear.local"]
}

// TestAdminModuleAccessCodesCoversSubMountGates pins the outer-gate invariant
// (Effort 2): AdminModuleAccessCodes is a SUPERSET of every admin sub-mount's
// gating code, so a caller admitted by any sub-surface can always pass the
// outer gate (the sub-mounts then apply their own tighter per-action gates).
func TestAdminModuleAccessCodesCoversSubMountGates(t *testing.T) {
	required := []string{
		// users sub-mount (any-of): users.view / users.approve / users.manage /
		// users.qualifications.manage
		UserViewPermission, UserApprovePermission, UserManagePermission, UsersQualificationsManagePermission,
		// user-groups sub-mount: user_groups.manage
		UserGroupsManagePermission,
		// qualifications sub-mount (any-of): qualifications.manage /
		// users.qualifications.manage
		QualificationsManagePermission,
		// roles sub-mount (any-of): roles.create / roles.edit / roles.assign
		RoleCreatePermission, RoleEditPermission, RoleAssignPermission,
		// recovery + admin sub-surfaces
		"admin.recovery.approve",
		// tool + settings + dsgvo surfaces
		"tools.manage", "tool_types.manage",
		"admin.settings.email", "admin.settings.backup",
		"dsgvo.access_report", "dsgvo.delete",
	}
	got := make(map[string]bool)
	for _, c := range AdminModuleAccessCodes() {
		got[c] = true
	}
	for _, want := range required {
		if !got[want] {
			t.Errorf("AdminModuleAccessCodes missing %q — every sub-mount gate code must be reachable through the outer gate", want)
		}
	}
}

func TestListUsersValid(t *testing.T) {
	// LIST_USERS: an admin holding a users.* code gets every user with
	// id/names/email/status, server-authoritative.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	users, err := svc.ListUsers(context.Background(), adminActor(repo), nil)
	if err != nil {
		t.Fatalf("ListUsers failed: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("user count = %d, want 2", len(users))
	}
	// Ordered by last name then first name.
	if users[0].Nachname != "Waltung" || users[0].Vorname != "Vera" {
		t.Errorf("users[0] = %+v, want Vera Waltung first", users[0])
	}
	if users[1].Email != "helfende@gear.local" || users[1].Status != string(StateActive) {
		t.Errorf("users[1] = %+v, want the helfende volunteer", users[1])
	}
	// GROUP_TAGS (Effort 2): the summary carries the organisational team names
	// so the SPA table renders inline group tags. The helfende volunteer is a
	// member of "Gruppe Ost"; the admin is in no team (empty, never null).
	if len(users[1].UserGroups) != 1 || users[1].UserGroups[0] != "Gruppe Ost" {
		t.Errorf("users[1].UserGroups = %v, want [Gruppe Ost]", users[1].UserGroups)
	}
	if users[0].UserGroups == nil || len(users[0].UserGroups) != 0 {
		t.Errorf("users[0].UserGroups = %v, want an empty non-nil list", users[0].UserGroups)
	}
}

func TestListUsersViewOnly(t *testing.T) {
	// LIST (any-of gate): a caller holding ONLY users.view can list — the
	// sub-mount gate passes and the core re-verifies any-of users.*.
	repo := usersAdminRepo()
	repo.users["viewonly@gear.local"] = &User{
		ID: "u-view", Email: "viewonly@gear.local", DisplayName: "Nur Guck",
		FirstName: "Nur", LastName: "Guck", State: StateActive,
	}
	repo.perms["u-view"] = []string{UserViewPermission}
	svc := usersAdminService(t, repo)

	if _, err := svc.ListUsers(context.Background(), repo.users["viewonly@gear.local"], nil); err != nil {
		t.Errorf("ListUsers with users.view failed: %v", err)
	}
}

func TestListUsersForbidden(t *testing.T) {
	// LIST_FORBIDDEN: a caller with no users.* code is denied (defense-in-depth;
	// the gateway answers the uniform hidden-existence 403, FR-19).
	repo := usersAdminRepo()
	repo.users["schirr@gear.local"] = &User{
		ID: "u-schirr", Email: "schirr@gear.local", DisplayName: "Schirr Meister",
		FirstName: "Schirr", LastName: "Meister", State: StateActive,
	}
	repo.perms["u-schirr"] = []string{"tools.manage", "tool_types.manage"}
	svc := usersAdminService(t, repo)

	_, err := svc.ListUsers(context.Background(), repo.users["schirr@gear.local"], nil)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("ListUsers err = %v, want ErrForbidden", err)
	}
}

func TestGetUserDetailValid(t *testing.T) {
	// DETAIL_VALID: the detail carries the profile + roles + user groups +
	// direct grants + qualifications (with status).
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)
	repo.perms["u-helfende"] = []string{"dashboard.view", "inspection.submit", "tools.manage"}
	repo.directGrants["u-helfende"] = []string{"report.export"}

	detail, err := svc.GetUserDetail(context.Background(), adminActor(repo), "u-helfende")
	if err != nil {
		t.Fatalf("GetUserDetail failed: %v", err)
	}
	if detail.Vorname != "Frei" || detail.Nachname != "Willig" || detail.Email != "helfende@gear.local" {
		t.Errorf("profile = %+v, want the helfende volunteer", detail)
	}
	if len(detail.Roles) != 1 || detail.Roles[0].Name != "helfende" || !detail.Roles[0].IsBaseRole {
		t.Errorf("roles = %+v, want the helfende role", detail.Roles)
	}
	if len(detail.UserGroups) != 1 || detail.UserGroups[0].Name != "Gruppe Ost" {
		t.Errorf("user_groups = %+v, want Gruppe Ost", detail.UserGroups)
	}
	if len(detail.DirectGrants) != 1 || detail.DirectGrants[0].Code != "report.export" {
		t.Errorf("direct_grants = %+v, want report.export", detail.DirectGrants)
	}
	if len(detail.Qualifications) != 1 || detail.Qualifications[0].Name != "Kettensäge" {
		t.Fatalf("qualifications = %+v, want the Kettensäge assignment", detail.Qualifications)
	}
	// The fixed 10-day-out qualification shows "Gültig".
	if detail.Qualifications[0].Status != QualificationStatusValid {
		t.Errorf("qualification status = %q, want %q", detail.Qualifications[0].Status, QualificationStatusValid)
	}
}

func TestGetUserDetailQualificationStatuses(t *testing.T) {
	// QUAL_VIEW: the per-assignment status is computed server-side — Gültig,
	// Bald ablaufend, Abgelaufen and Unbegrenzt all render correctly.
	now := time.Now().UTC()
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	far := now.Add(90 * 24 * time.Hour)
	soon := now.Add(7 * 24 * time.Hour)
	past := now.Add(-1 * 24 * time.Hour)
	repo.qualifications["q-valid"] = &QualificationAssignment{ID: "q-valid", Name: "Gültig", ExpiryKind: QualificationExpiryFixed, ExpiresAt: &far}
	repo.qualifications["q-soon"] = &QualificationAssignment{ID: "q-soon", Name: "Bald", ExpiryKind: QualificationExpiryFixed, ExpiresAt: &soon}
	repo.qualifications["q-past"] = &QualificationAssignment{ID: "q-past", Name: "Abgelaufen", ExpiryKind: QualificationExpiryFixed, ExpiresAt: &past}
	repo.qualifications["q-unlim"] = &QualificationAssignment{ID: "q-unlim", Name: "Unbegrenzt", ExpiryKind: QualificationExpiryUnlimited}
	repo.userQualifications["u-helfende"] = []string{"q-valid", "q-soon", "q-past", "q-unlim"}

	detail, err := svc.GetUserDetail(context.Background(), adminActor(repo), "u-helfende")
	if err != nil {
		t.Fatalf("GetUserDetail failed: %v", err)
	}
	got := map[string]string{}
	for _, q := range detail.Qualifications {
		got[q.Name] = q.Status
	}
	if got["Gültig"] != QualificationStatusValid {
		t.Errorf("Gültig status = %q, want %q", got["Gültig"], QualificationStatusValid)
	}
	if got["Bald"] != QualificationStatusExpiringSoon {
		t.Errorf("Bald status = %q, want %q", got["Bald"], QualificationStatusExpiringSoon)
	}
	if got["Abgelaufen"] != QualificationStatusExpired {
		t.Errorf("Abgelaufen status = %q, want %q", got["Abgelaufen"], QualificationStatusExpired)
	}
	if got["Unbegrenzt"] != QualificationStatusUnlimited {
		t.Errorf("Unbegrenzt status = %q, want %q", got["Unbegrenzt"], QualificationStatusUnlimited)
	}
}

func TestQualificationStatusExactBoundary(t *testing.T) {
	// EXACT_BOUNDARY (finding 11): at the exact instant now == expires_at the
	// assignment is EXPIRED, not "Bald ablaufend" — a qualification expiring
	// exactly NOW is no longer valid.
	now := time.Now().UTC()
	cases := []struct {
		name      string
		expiresAt *time.Time
		want      string
	}{
		{"exactly now", func() *time.Time { t := now; return &t }(), QualificationStatusExpired},
		{"one ns past", func() *time.Time { t := now.Add(-time.Nanosecond); return &t }(), QualificationStatusExpired},
		{"one ns before", func() *time.Time { t := now.Add(time.Nanosecond); return &t }(), QualificationStatusExpiringSoon},
		{"soon but future", func() *time.Time { t := now.Add(7 * 24 * time.Hour); return &t }(), QualificationStatusExpiringSoon},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := qualificationStatus(QualificationAssignment{ExpiryKind: QualificationExpiryFixed, ExpiresAt: tc.expiresAt}, now)
			if got != tc.want {
				t.Errorf("status = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGetUserDetailUnknown(t *testing.T) {
	// DETAIL_UNKNOWN: a nonexistent id maps to the uniform 404 sentinel.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	_, err := svc.GetUserDetail(context.Background(), adminActor(repo), "u-nope")
	if !errors.Is(err, ErrAdminUserNotFound) {
		t.Fatalf("GetUserDetail err = %v, want ErrAdminUserNotFound", err)
	}
}

func TestCreateAdminUserValid(t *testing.T) {
	// CREATE_VALID: an admin-created user lands active with the optional
	// assignment sets and is immediately resolvable.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	res, err := svc.CreateAdminUser(context.Background(), adminActor(repo), CreateAdminUserInput{
		Vorname: "Tim", Nachname: "Müller", Email: "tim@gear.local", Status: string(StateActive),
		RoleIDs: []string{"g-helfende"}, UserGroupIDs: []string{"ug-ost"}, DirectGrantCodes: []string{"report.export"},
	})
	if err != nil {
		t.Fatalf("CreateAdminUser failed: %v", err)
	}
	// The result carries the server-authoritative confirmation (finding 8).
	if res.Message != MsgUserCreated {
		t.Errorf("create message = %q, want %q", res.Message, MsgUserCreated)
	}
	detail := res.User
	if detail.ID == "" {
		t.Error("created user has no id")
	}
	if detail.Vorname != "Tim" || detail.Nachname != "Müller" || detail.Email != "tim@gear.local" || detail.Status != string(StateActive) {
		t.Errorf("created profile = %+v, want the submitted fields", detail)
	}
	if len(detail.Roles) != 1 || detail.Roles[0].Name != "helfende" {
		t.Errorf("created roles = %+v, want helfende", detail.Roles)
	}
	if len(detail.UserGroups) != 1 || detail.UserGroups[0].Name != "Gruppe Ost" {
		t.Errorf("created user_groups = %+v, want Gruppe Ost", detail.UserGroups)
	}
	if len(detail.DirectGrants) != 1 || detail.DirectGrants[0].Code != "report.export" {
		t.Errorf("created direct_grants = %+v, want report.export", detail.DirectGrants)
	}
	// The created user is resolvable (live, AD-2).
	u := repo.userByID(detail.ID)
	perms, err := svc.ResolvePermissionSet(context.Background(), u)
	if err != nil {
		t.Fatalf("ResolvePermissionSet failed: %v", err)
	}
	for _, want := range []string{"dashboard.view", "inspection.submit", "report.export"} {
		if !containsStr(perms, want) {
			t.Errorf("created user perms = %v, want %q present", perms, want)
		}
	}
	// Audited (NFR-O1).
	if got := repo.audit["u-admin"]; len(got) != 1 || got[0] != AuditOperationUserCreate {
		t.Errorf("audit after create = %v, want [user.create]", got)
	}
}

func TestCreateAdminUserPending(t *testing.T) {
	// CREATE_PENDING: the form may create a pending account.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	res, err := svc.CreateAdminUser(context.Background(), adminActor(repo), CreateAdminUserInput{
		Vorname: "Tim", Nachname: "Müller", Email: "tim@gear.local", Status: string(StatePendingApproval),
	})
	if err != nil {
		t.Fatalf("CreateAdminUser(pending) failed: %v", err)
	}
	if res.User.Status != string(StatePendingApproval) {
		t.Errorf("status = %q, want pending_approval", res.User.Status)
	}
}

func TestCreateAdminUserDupEmail(t *testing.T) {
	// CREATE_DUP_EMAIL: an existing email in a DIFFERENT case maps to the
	// uniform 409 (case-insensitive uniqueness), nothing created.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	_, err := svc.CreateAdminUser(context.Background(), adminActor(repo), CreateAdminUserInput{
		Vorname: "Tim", Nachname: "Müller", Email: "HELFENDE@GEAR.LOCAL", Status: string(StateActive),
	})
	if !errors.Is(err, ErrAdminUserEmailTaken) {
		t.Fatalf("CreateAdminUser(dup email) err = %v, want ErrAdminUserEmailTaken", err)
	}
}

func TestCreateAdminUserInvalidInput(t *testing.T) {
	// CREATE invalid input: empty name, bad email, bad status, unknown role,
	// unknown group and out-of-series grant all map to their 400 sentinels.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	cases := []struct {
		name  string
		input CreateAdminUserInput
		want  error
	}{
		{"empty name", CreateAdminUserInput{Vorname: " ", Nachname: "X", Email: "a@b.de", Status: string(StateActive)}, ErrAdminUserInvalidName},
		{"bad email", CreateAdminUserInput{Vorname: "A", Nachname: "B", Email: "nope", Status: string(StateActive)}, ErrAdminUserInvalidEmail},
		{"bad status", CreateAdminUserInput{Vorname: "A", Nachname: "B", Email: "a@b.de", Status: "deactivated"}, ErrAdminUserInvalidStatus},
		{"unknown role", CreateAdminUserInput{Vorname: "A", Nachname: "B", Email: "a@b.de", Status: string(StateActive), RoleIDs: []string{"g-nope"}}, ErrAdminUserUnknownRole},
		{"unknown group", CreateAdminUserInput{Vorname: "A", Nachname: "B", Email: "a@b.de", Status: string(StateActive), UserGroupIDs: []string{"ug-nope"}}, ErrAdminUserUnknownUserGroup},
		{"unknown grant", CreateAdminUserInput{Vorname: "A", Nachname: "B", Email: "a@b.de", Status: string(StateActive), DirectGrantCodes: []string{"bogus.code"}}, ErrUnknownPermissionCode},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateAdminUser(context.Background(), adminActor(repo), tc.input)
			if !errors.Is(err, tc.want) {
				t.Fatalf("CreateAdminUser err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCreateAdminUserForbidden(t *testing.T) {
	// CREATE_FORBIDDEN: a view/approve-only holder may list but never create
	// (defense-in-depth: create needs users.manage, AD-6).
	repo := usersAdminRepo()
	repo.users["approveonly@gear.local"] = &User{
		ID: "u-approve", Email: "approveonly@gear.local", DisplayName: "Nur Freigabe",
		FirstName: "Nur", LastName: "Freigabe", State: StateActive,
	}
	repo.perms["u-approve"] = []string{UserApprovePermission}
	svc := usersAdminService(t, repo)

	_, err := svc.CreateAdminUser(context.Background(), repo.users["approveonly@gear.local"], CreateAdminUserInput{
		Vorname: "A", Nachname: "B", Email: "a@b.de", Status: string(StateActive),
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("CreateAdminUser(view-only) err = %v, want ErrForbidden", err)
	}
}

func TestUpdateAdminUserValid(t *testing.T) {
	// EDIT_VALID: editing replaces the profile AND all three assignment sets
	// atomically; the resolved set reflects the change immediately.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)
	repo.perms["u-helfende"] = []string{"dashboard.view", "inspection.submit"}

	res, err := svc.UpdateAdminUser(context.Background(), adminActor(repo), "u-helfende", UpdateAdminUserInput{
		Vorname: "Frei", Nachname: "Willig", Email: "helfende@gear.local", Status: string(StateActive),
		RoleIDs: []string{"g-schirr"}, UserGroupIDs: []string{}, DirectGrantCodes: []string{"report.export"},
	})
	if err != nil {
		t.Fatalf("UpdateAdminUser failed: %v", err)
	}
	// The result carries the server-authoritative confirmation (finding 8).
	if res.Message != MsgUserUpdated {
		t.Errorf("update message = %q, want %q", res.Message, MsgUserUpdated)
	}
	detail := res.User
	if len(detail.Roles) != 1 || detail.Roles[0].Name != "schirrmeister" {
		t.Errorf("edited roles = %+v, want schirrmeister only (helfende replaced)", detail.Roles)
	}
	if len(detail.UserGroups) != 0 {
		t.Errorf("edited user_groups = %+v, want [] (Gruppe Ost removed)", detail.UserGroups)
	}
	if len(detail.DirectGrants) != 1 || detail.DirectGrants[0].Code != "report.export" {
		t.Errorf("edited direct_grants = %+v, want report.export", detail.DirectGrants)
	}
	// Audited (NFR-O1).
	if got := repo.audit["u-admin"]; len(got) != 1 || got[0] != AuditOperationUserUpdate {
		t.Errorf("audit after edit = %v, want [user.update]", got)
	}
}

func TestUpdateAdminUserImmediateEffect(t *testing.T) {
	// IMMEDIATE_EFFECT (AD-2/FR-21): editing an active user's role/group/grant
	// changes their RESOLVED set on the very next resolution — no re-login, no
	// cache. Removing a role revokes its permissions immediately.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)
	volunteer := repo.users["helfende@gear.local"]

	before, err := svc.ResolvePermissionSet(context.Background(), volunteer)
	if err != nil {
		t.Fatalf("resolve before failed: %v", err)
	}
	if !containsStr(before, "dashboard.view") {
		t.Fatalf("helfende before = %v, want dashboard.view present", before)
	}

	// Remove the helfende role entirely (empty role set), keep the team.
	if _, err := svc.UpdateAdminUser(context.Background(), adminActor(repo), "u-helfende", UpdateAdminUserInput{
		Vorname: "Frei", Nachname: "Willig", Email: "helfende@gear.local", Status: string(StateActive),
		RoleIDs: []string{}, UserGroupIDs: []string{"ug-ost"}, DirectGrantCodes: []string{},
	}); err != nil {
		t.Fatalf("UpdateAdminUser failed: %v", err)
	}

	after, err := svc.ResolvePermissionSet(context.Background(), volunteer)
	if err != nil {
		t.Fatalf("resolve after failed: %v", err)
	}
	if containsStr(after, "dashboard.view") {
		t.Errorf("helfende after = %v, want dashboard.view REMOVED (immediate effect)", after)
	}
}

func TestUpdateAdminUserUnknown(t *testing.T) {
	// EDIT_UNKNOWN: a nonexistent id maps to the uniform 404.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	_, err := svc.UpdateAdminUser(context.Background(), adminActor(repo), "u-nope", UpdateAdminUserInput{
		Vorname: "A", Nachname: "B", Email: "a@b.de", Status: string(StateActive),
	})
	if !errors.Is(err, ErrAdminUserNotFound) {
		t.Fatalf("UpdateAdminUser(unknown) err = %v, want ErrAdminUserNotFound", err)
	}
}

func TestUpdateAdminUserDupEmail(t *testing.T) {
	// EDIT_DUP_EMAIL: renaming onto another user's email maps to the uniform
	// 409; keeping the user's OWN email stays legal.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	_, err := svc.UpdateAdminUser(context.Background(), adminActor(repo), "u-helfende", UpdateAdminUserInput{
		Vorname: "Frei", Nachname: "Willig", Email: "admin@gear.local", Status: string(StateActive),
	})
	if !errors.Is(err, ErrAdminUserEmailTaken) {
		t.Fatalf("UpdateAdminUser(dup) err = %v, want ErrAdminUserEmailTaken", err)
	}
	// Own email (same) stays legal.
	if _, err := svc.UpdateAdminUser(context.Background(), adminActor(repo), "u-helfende", UpdateAdminUserInput{
		Vorname: "Frei", Nachname: "Willig", Email: "HELFENDE@gear.local", Status: string(StateActive),
	}); err != nil {
		t.Fatalf("UpdateAdminUser(own email variant) failed: %v", err)
	}
}

func TestDeactivateUserValid(t *testing.T) {
	// DEACTIVATE_VALID: an active, confirmed deactivation flips the user to
	// deactivated ("→ Sofort kein Login") and is audited with actor + target
	// (NFR-O1).
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	res, err := svc.DeactivateUser(context.Background(), adminActor(repo), "u-helfende", true)
	if err != nil {
		t.Fatalf("DeactivateUser failed: %v", err)
	}
	if res.Message != MsgUserDeactivated || res.Email != "helfende@gear.local" {
		t.Errorf("result = %+v, want the deactivation confirmation", res)
	}
	if repo.users["helfende@gear.local"].State != StateDeactivated {
		t.Errorf("state = %q, want deactivated", repo.users["helfende@gear.local"].State)
	}
	if got := repo.audit["u-admin"]; len(got) != 1 || got[0] != AuditOperationUserDeactivate {
		t.Errorf("audit after deactivate = %v, want [user.deactivate]", got)
	}
	if got := repo.auditSeverity["u-admin"]; len(got) != 1 || got[0] != AuditSeverityHigh {
		t.Errorf("audit severity after deactivate = %v, want [high]", got)
	}
}

func TestDeactivateUserNotConfirmed(t *testing.T) {
	// DEACTIVATE_NOCONFIRM: a deactivation without client confirmation maps to
	// the uniform 400 — nothing changes.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	_, err := svc.DeactivateUser(context.Background(), adminActor(repo), "u-helfende", false)
	if !errors.Is(err, ErrDeactivationNotConfirmed) {
		t.Fatalf("DeactivateUser(not confirmed) err = %v, want ErrDeactivationNotConfirmed", err)
	}
	if repo.users["helfende@gear.local"].State != StateActive {
		t.Errorf("state = %q, want unchanged active", repo.users["helfende@gear.local"].State)
	}
}

func TestDeactivateUserNonActive(t *testing.T) {
	// DEACTIVATE_NONACTIVE: a pending or already-deactivated user maps to the
	// uniform conflict (never an existence hint, FR-19).
	repo := usersAdminRepo()
	repo.users["pending@gear.local"] = &User{
		ID: "u-pending", Email: "pending@gear.local", DisplayName: "Wartet Noch",
		FirstName: "Wartet", LastName: "Noch", State: StatePendingApproval,
	}
	svc := usersAdminService(t, repo)

	_, err := svc.DeactivateUser(context.Background(), adminActor(repo), "u-pending", true)
	if !errors.Is(err, ErrUserNotActiveForDeactivate) {
		t.Fatalf("DeactivateUser(pending) err = %v, want ErrUserNotActiveForDeactivate", err)
	}
}

func TestDeactivateUserUnknown(t *testing.T) {
	// DEACTIVATE_UNKNOWN: a nonexistent id maps to the uniform 404.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	_, err := svc.DeactivateUser(context.Background(), adminActor(repo), "u-nope", true)
	if !errors.Is(err, ErrAdminUserNotFound) {
		t.Fatalf("DeactivateUser(unknown) err = %v, want ErrAdminUserNotFound", err)
	}
}

func TestDeactivateUserForbidden(t *testing.T) {
	// DEACTIVATE_FORBIDDEN: a view-only holder may list/detail but never
	// deactivate (defense-in-depth: deactivate needs users.manage).
	repo := usersAdminRepo()
	repo.users["viewonly@gear.local"] = &User{
		ID: "u-view", Email: "viewonly@gear.local", DisplayName: "Nur Guck",
		FirstName: "Nur", LastName: "Guck", State: StateActive,
	}
	repo.perms["u-view"] = []string{UserViewPermission}
	svc := usersAdminService(t, repo)

	_, err := svc.DeactivateUser(context.Background(), repo.users["viewonly@gear.local"], "u-helfende", true)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("DeactivateUser(view-only) err = %v, want ErrForbidden", err)
	}
}

func TestListUserGroupsValid(t *testing.T) {
	// GROUP_LIST: a user_groups.manage holder lists the teams ordered by name.
	repo := usersAdminRepo()
	repo.seedUserGroup("ug-west", "Gruppe West", "Westliche Gruppe")
	svc := usersAdminService(t, repo)

	groups, err := svc.ListUserGroups(context.Background(), adminActor(repo))
	if err != nil {
		t.Fatalf("ListUserGroups failed: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("group count = %d, want 2", len(groups))
	}
	if groups[0].Name != "Gruppe Ost" || groups[1].Name != "Gruppe West" {
		t.Errorf("group order = %v, want Gruppe Ost then Gruppe West", groups)
	}
}

func TestListUserGroupsForbidden(t *testing.T) {
	// GROUP_FORBIDDEN: a caller without user_groups.manage is denied.
	repo := usersAdminRepo()
	repo.users["viewonly@gear.local"] = &User{
		ID: "u-view", Email: "viewonly@gear.local", DisplayName: "Nur Guck",
		FirstName: "Nur", LastName: "Guck", State: StateActive,
	}
	repo.perms["u-view"] = []string{UserViewPermission}
	svc := usersAdminService(t, repo)

	_, err := svc.ListUserGroups(context.Background(), repo.users["viewonly@gear.local"])
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("ListUserGroups(view-only) err = %v, want ErrForbidden", err)
	}
}

func TestCreateUserGroupValid(t *testing.T) {
	// GROUP_CREATE_VALID: creating a team persists it with a unique name.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	group, err := svc.CreateUserGroup(context.Background(), adminActor(repo), CreateUserGroupInput{
		Name: "Fachgruppe Wassergefahren", Description: "Wasser-Einsätze",
	})
	if err != nil {
		t.Fatalf("CreateUserGroup failed: %v", err)
	}
	if group.ID == "" || group.Name != "Fachgruppe Wassergefahren" {
		t.Errorf("group = %+v, want the created team", group)
	}
	if got := repo.audit["u-admin"]; len(got) != 1 || got[0] != AuditOperationUserGroupCreate {
		t.Errorf("audit after group create = %v, want [user_group.create]", got)
	}
}

func TestCreateUserGroupDupName(t *testing.T) {
	// GROUP_CREATE_DUP: a duplicate name in a DIFFERENT case maps to 409.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	_, err := svc.CreateUserGroup(context.Background(), adminActor(repo), CreateUserGroupInput{Name: "gruppe ost"})
	if !errors.Is(err, ErrUserGroupNameTaken) {
		t.Fatalf("CreateUserGroup(dup) err = %v, want ErrUserGroupNameTaken", err)
	}
}

func TestCreateUserGroupInvalidName(t *testing.T) {
	// GROUP_CREATE invalid name: empty name maps to the uniform 400.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	_, err := svc.CreateUserGroup(context.Background(), adminActor(repo), CreateUserGroupInput{Name: "   "})
	if !errors.Is(err, ErrUserGroupInvalidName) {
		t.Fatalf("CreateUserGroup(empty) err = %v, want ErrUserGroupInvalidName", err)
	}
	long := strings.Repeat("g", UserGroupNameMaxLength+1)
	_, err = svc.CreateUserGroup(context.Background(), adminActor(repo), CreateUserGroupInput{Name: long})
	if !errors.Is(err, ErrUserGroupInvalidName) {
		t.Fatalf("CreateUserGroup(long) err = %v, want ErrUserGroupInvalidName", err)
	}
}

func TestAssignUserGroupMembersValid(t *testing.T) {
	// GROUP_ASSIGN: replacing a team's members persists the new set; membership
	// grants no permission (AD-12) — the resolved permission set is untouched.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)
	repo.users["tim@gear.local"] = &User{
		ID: "u-tim", Email: "tim@gear.local", DisplayName: "Tim Müller",
		FirstName: "Tim", LastName: "Müller", State: StateActive,
	}

	group, err := svc.AssignUserGroupMembers(context.Background(), adminActor(repo), "ug-ost", []string{"u-tim"})
	if err != nil {
		t.Fatalf("AssignUserGroupMembers failed: %v", err)
	}
	if group.ID != "ug-ost" {
		t.Errorf("group = %+v, want ug-ost", group)
	}
	// The helfende volunteer was removed; Tim is now a member (group state).
	found := false
	for _, gid := range repo.userGroupMembers["u-tim"] {
		if gid == "ug-ost" {
			found = true
		}
	}
	if !found {
		t.Error("tim is not a member of ug-ost after assignment")
	}
	for _, gid := range repo.userGroupMembers["u-helfende"] {
		if gid == "ug-ost" {
			t.Error("helfende still a member of ug-ost after assignment (set replaced)")
		}
	}
	// AD-12: organisational membership never changes the resolved permission set.
	perms, err := svc.ResolvePermissionSet(context.Background(), repo.userByID("u-tim"))
	if err != nil {
		t.Fatalf("ResolvePermissionSet failed: %v", err)
	}
	if len(perms) != 0 {
		t.Errorf("tim perms = %v, want [] (team membership grants no access)", perms)
	}
}

func TestAssignUserGroupMembersUnknownGroup(t *testing.T) {
	// GROUP_ASSIGN_UNKNOWN: an unknown group maps to the uniform 404.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	_, err := svc.AssignUserGroupMembers(context.Background(), adminActor(repo), "ug-nope", []string{"u-helfende"})
	if !errors.Is(err, ErrUserGroupNotFound) {
		t.Fatalf("AssignUserGroupMembers(unknown group) err = %v, want ErrUserGroupNotFound", err)
	}
}

func TestAssignUserGroupMembersUnknownMember(t *testing.T) {
	// GROUP_ASSIGN_UNKNOWN_MEMBER: an unknown member maps to the uniform 400.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	_, err := svc.AssignUserGroupMembers(context.Background(), adminActor(repo), "ug-ost", []string{"u-nope"})
	if !errors.Is(err, ErrUserGroupMemberUnknown) {
		t.Fatalf("AssignUserGroupMembers(unknown member) err = %v, want ErrUserGroupMemberUnknown", err)
	}
}

func TestAssignUserGroupMembersForbidden(t *testing.T) {
	// GROUP_ASSIGN_FORBIDDEN: a caller without user_groups.manage is denied.
	repo := usersAdminRepo()
	repo.users["viewonly@gear.local"] = &User{
		ID: "u-view", Email: "viewonly@gear.local", DisplayName: "Nur Guck",
		FirstName: "Nur", LastName: "Guck", State: StateActive,
	}
	repo.perms["u-view"] = []string{UserViewPermission}
	svc := usersAdminService(t, repo)

	_, err := svc.AssignUserGroupMembers(context.Background(), repo.users["viewonly@gear.local"], "ug-ost", nil)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("AssignUserGroupMembers(view-only) err = %v, want ErrForbidden", err)
	}
}

func TestUsersAdminTrimsFields(t *testing.T) {
	// TRIM: the admin-created user's names/email are trimmed server-side.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	res, err := svc.CreateAdminUser(context.Background(), adminActor(repo), CreateAdminUserInput{
		Vorname: "  Tim  ", Nachname: "  Müller ", Email: "  TIM@GEAR.LOCAL ", Status: string(StateActive),
	})
	if err != nil {
		t.Fatalf("CreateAdminUser failed: %v", err)
	}
	detail := res.User
	if detail.Vorname != "Tim" || detail.Nachname != "Müller" || detail.Email != "tim@gear.local" {
		t.Errorf("trimmed profile = %+v, want trimmed values", detail)
	}
}

func TestUsersAdminAuditBestEffort(t *testing.T) {
	// NFR-O1: an audit-write failure is logged, never rolled back into the
	// create — the user still lands and the call succeeds.
	repo := usersAdminRepo()
	repo.auditErr = errors.New("audit down")
	svc := usersAdminService(t, repo)

	res, err := svc.CreateAdminUser(context.Background(), adminActor(repo), CreateAdminUserInput{
		Vorname: "Tim", Nachname: "Müller", Email: "tim@gear.local", Status: string(StateActive),
	})
	if err != nil {
		t.Fatalf("CreateAdminUser with failing audit failed: %v", err)
	}
	if res.User.Email != "tim@gear.local" {
		t.Errorf("detail = %+v, want the created user despite the audit failure", res.User)
	}
}

func TestCreateAdminUserDedupesAssignmentSets(t *testing.T) {
	// DEDUPE: duplicate assignment ids/codes in the input collapse to one.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	res, err := svc.CreateAdminUser(context.Background(), adminActor(repo), CreateAdminUserInput{
		Vorname: "Tim", Nachname: "Müller", Email: "tim@gear.local", Status: string(StateActive),
		RoleIDs: []string{"g-helfende", "g-helfende"}, DirectGrantCodes: []string{"report.export", "report.export"},
	})
	if err != nil {
		t.Fatalf("CreateAdminUser failed: %v", err)
	}
	detail := res.User
	if len(detail.Roles) != 1 || len(detail.DirectGrants) != 1 {
		t.Errorf("roles = %+v, grants = %+v, want deduplicated sets", detail.Roles, detail.DirectGrants)
	}
}

func TestDeactivateUserSelf(t *testing.T) {
	// SELF_DEACTIVATE (finding 7): an admin must never be able to deactivate
	// their OWN account and lock themselves out of the module they manage — the
	// uniform 409 conflict with a clear German message.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	_, err := svc.DeactivateUser(context.Background(), adminActor(repo), "u-admin", true)
	if !errors.Is(err, ErrSelfDeactivation) {
		t.Fatalf("DeactivateUser(self) err = %v, want ErrSelfDeactivation", err)
	}
	if repo.users["admin@gear.local"].State != StateActive {
		t.Errorf("own account state = %q, want unchanged active", repo.users["admin@gear.local"].State)
	}
}

func TestAssignUserGroupMembersDedupesIDs(t *testing.T) {
	// DEDUPE (finding 10): duplicate member ids in the input collapse to one, so
	// the existence-count check never trips a spurious ErrUserGroupMemberUnknown.
	repo := usersAdminRepo()
	repo.users["tim@gear.local"] = &User{
		ID: "u-tim", Email: "tim@gear.local", DisplayName: "Tim Müller",
		FirstName: "Tim", LastName: "Müller", State: StateActive,
	}
	svc := usersAdminService(t, repo)

	group, err := svc.AssignUserGroupMembers(context.Background(), adminActor(repo), "ug-ost", []string{"u-tim", "u-tim", "u-tim"})
	if err != nil {
		t.Fatalf("AssignUserGroupMembers(dup ids) failed: %v", err)
	}
	if group.ID != "ug-ost" {
		t.Errorf("group = %+v, want ug-ost", group)
	}
	members, err := svc.ListUserGroupMembers(context.Background(), adminActor(repo), "ug-ost")
	if err != nil {
		t.Fatalf("ListUserGroupMembers failed: %v", err)
	}
	if len(members) != 1 || members[0] != "u-tim" {
		t.Errorf("members = %v, want [u-tim] (deduplicated)", members)
	}
}

func TestAssignUserGroupsValid(t *testing.T) {
	// Effort 2 USER-detail group assignment: replacing a user's group set
	// persists the memberships and returns the refreshed detail + message.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	res, err := svc.AssignUserGroups(context.Background(), adminActor(repo), "u-helfende", []string{"ug-ost"})
	if err != nil {
		t.Fatalf("AssignUserGroups failed: %v", err)
	}
	if res.Message != MsgUserGroupsUpdated {
		t.Errorf("message = %q, want %q", res.Message, MsgUserGroupsUpdated)
	}
	if len(res.User.UserGroups) != 1 || res.User.UserGroups[0].ID != "ug-ost" {
		t.Errorf("user groups = %+v, want [ug-ost]", res.User.UserGroups)
	}
}

func TestAssignUserGroupsEmptyClears(t *testing.T) {
	// Effort 2: an empty set removes every membership.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	res, err := svc.AssignUserGroups(context.Background(), adminActor(repo), "u-helfende", nil)
	if err != nil {
		t.Fatalf("AssignUserGroups(empty) failed: %v", err)
	}
	if len(res.User.UserGroups) != 0 {
		t.Errorf("user groups = %+v, want none after clearing", res.User.UserGroups)
	}
}

func TestAssignUserGroupsUnknownUser(t *testing.T) {
	// Effort 2: an unknown user maps to the uniform 404.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	if _, err := svc.AssignUserGroups(context.Background(), adminActor(repo), "u-nope", []string{"ug-ost"}); err != ErrAdminUserNotFound {
		t.Fatalf("err = %v, want ErrAdminUserNotFound", err)
	}
}

func TestAssignUserGroupsUnknownGroup(t *testing.T) {
	// Effort 2: an unknown group id maps to the uniform 400.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	if _, err := svc.AssignUserGroups(context.Background(), adminActor(repo), "u-helfende", []string{"ug-nope"}); err != ErrAdminUserUnknownUserGroup {
		t.Fatalf("err = %v, want ErrAdminUserUnknownUserGroup", err)
	}
}

func TestAssignUserGroupsForbidden(t *testing.T) {
	// Effort 2: a caller without user_groups.manage is denied.
	repo := usersAdminRepo()
	repo.perms["u-helfende"] = []string{"users.view"}
	svc := usersAdminService(t, repo)

	if _, err := svc.AssignUserGroups(context.Background(), repo.users["helfende@gear.local"], "u-helfende", []string{"ug-ost"}); err != ErrForbidden {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestListUserGroupMembersValid(t *testing.T) {
	// GROUP_MEMBERS_LIST: the current member ids of a group are returned.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	members, err := svc.ListUserGroupMembers(context.Background(), adminActor(repo), "ug-ost")
	if err != nil {
		t.Fatalf("ListUserGroupMembers failed: %v", err)
	}
	if len(members) != 1 || members[0] != "u-helfende" {
		t.Errorf("members = %v, want [u-helfende]", members)
	}
}

func TestListUserGroupMembersUnknownGroup(t *testing.T) {
	// GROUP_MEMBERS_UNKNOWN: an unknown group maps to the uniform 404.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	_, err := svc.ListUserGroupMembers(context.Background(), adminActor(repo), "ug-nope")
	if !errors.Is(err, ErrUserGroupNotFound) {
		t.Fatalf("ListUserGroupMembers(unknown) err = %v, want ErrUserGroupNotFound", err)
	}
}

func TestListUserGroupMembersForbidden(t *testing.T) {
	// GROUP_MEMBERS_FORBIDDEN: a caller without user_groups.manage is denied.
	repo := usersAdminRepo()
	repo.users["viewonly@gear.local"] = &User{
		ID: "u-view", Email: "viewonly@gear.local", DisplayName: "Nur Guck",
		FirstName: "Nur", LastName: "Guck", State: StateActive,
	}
	repo.perms["u-view"] = []string{UserViewPermission}
	svc := usersAdminService(t, repo)

	_, err := svc.ListUserGroupMembers(context.Background(), repo.users["viewonly@gear.local"], "ug-ost")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("ListUserGroupMembers(view-only) err = %v, want ErrForbidden", err)
	}
}

func TestDeleteUserGroupValid(t *testing.T) {
	// GROUP_DELETE_VALID: deleting a team removes it (and its memberships) and
	// is audited. Deleting a team never changes access (AD-12).
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	if err := svc.DeleteUserGroup(context.Background(), adminActor(repo), "ug-ost"); err != nil {
		t.Fatalf("DeleteUserGroup failed: %v", err)
	}
	if _, ok := repo.userGroups["ug-ost"]; ok {
		t.Error("ug-ost still present after delete")
	}
	// The helfende volunteer is no longer a member of the deleted team.
	for _, gid := range repo.userGroupMembers["u-helfende"] {
		if gid == "ug-ost" {
			t.Error("helfende still a member of the deleted group")
		}
	}
	if got := repo.audit["u-admin"]; len(got) != 1 || got[0] != AuditOperationUserGroupDelete {
		t.Errorf("audit after group delete = %v, want [user_group.delete]", got)
	}
}

func TestDeleteUserGroupUnknown(t *testing.T) {
	// GROUP_DELETE_UNKNOWN: an unknown group maps to the uniform 404.
	repo := usersAdminRepo()
	svc := usersAdminService(t, repo)

	err := svc.DeleteUserGroup(context.Background(), adminActor(repo), "ug-nope")
	if !errors.Is(err, ErrUserGroupNotFound) {
		t.Fatalf("DeleteUserGroup(unknown) err = %v, want ErrUserGroupNotFound", err)
	}
}

func TestDeleteUserGroupForbidden(t *testing.T) {
	// GROUP_DELETE_FORBIDDEN: a caller without user_groups.manage is denied.
	repo := usersAdminRepo()
	repo.users["viewonly@gear.local"] = &User{
		ID: "u-view", Email: "viewonly@gear.local", DisplayName: "Nur Guck",
		FirstName: "Nur", LastName: "Guck", State: StateActive,
	}
	repo.perms["u-view"] = []string{UserViewPermission}
	svc := usersAdminService(t, repo)

	err := svc.DeleteUserGroup(context.Background(), repo.users["viewonly@gear.local"], "ug-ost")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("DeleteUserGroup(view-only) err = %v, want ErrForbidden", err)
	}
}