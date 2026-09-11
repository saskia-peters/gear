package core

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// qualificationsRepo seeds the Story 2.7 I/O matrix: an active admin holding
// `qualifications.manage`, a view-only caller WITHOUT it, and a helfende
// volunteer attached to the 'helfende' role (the assignment target).
func qualificationsRepo() *mockRepo {
	repo := usersAdminRepo()
	repo.perms["u-admin"] = append(repo.perms["u-admin"], QualificationsManagePermission)
	return repo
}

func qualificationsAdmin(repo *mockRepo) *User {
	return repo.users["admin@gear.local"]
}

func TestListQualificationsValid(t *testing.T) {
	// LIST_QUALS: an admin holding qualifications.manage gets every
	// qualification with its server-derived status indicator plus the user
	// roster (id + display name), in one round-trip.
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)

	res, err := svc.ListQualifications(context.Background(), qualificationsAdmin(repo))
	if err != nil {
		t.Fatalf("ListQualifications failed: %v", err)
	}
	if len(res.Qualifications) != 1 || res.Qualifications[0].Name != "Kettensäge" {
		t.Fatalf("qualifications = %+v, want the Kettensäge entry", res.Qualifications)
	}
	// The fixed qualification shows the vocabulary badge "Befristet"
	// (2026-09-08 rework: the vocabulary has no date; a per-user valid-until is
	// set at assignment, Spec 2.9).
	if res.Qualifications[0].Status != QualificationStatusFixed {
		t.Errorf("status = %q, want %q", res.Qualifications[0].Status, QualificationStatusFixed)
	}
	// The roster carries id + display name for the assignment editor.
	if len(res.Users) != 2 {
		t.Fatalf("roster users = %d, want 2", len(res.Users))
	}
	names := map[string]string{}
	for _, u := range res.Users {
		names[u.ID] = u.Name
	}
	if names["u-helfende"] == "" {
		t.Errorf("roster missing the helfende volunteer: %+v", res.Users)
	}
}

func TestListQualificationsRosterNameFallback(t *testing.T) {
	// LIST_ROSTER_FALLBACK (finding): a user without first/last names gets a
	// neutral German label — never the raw email (the roster has no need for it).
	repo := qualificationsRepo()
	repo.users["seeded@gear.local"] = &User{
		ID: "u-seeded", Email: "seeded@gear.local", DisplayName: "Seed Admin",
		State: StateActive,
	}
	svc := usersAdminService(t, repo)

	res, err := svc.ListQualifications(context.Background(), qualificationsAdmin(repo))
	if err != nil {
		t.Fatalf("ListQualifications failed: %v", err)
	}
	var seededName string
	for _, u := range res.Users {
		if u.ID == "u-seeded" {
			seededName = u.Name
		}
	}
	if seededName != "Unbekannt" {
		t.Errorf("seeded user roster name = %q, want the neutral German label", seededName)
	}
	for _, u := range res.Users {
		if strings.Contains(u.Name, "@") {
			t.Errorf("roster leaks a raw email: %q", u.Name)
		}
	}
}

func TestListQualificationsForbidden(t *testing.T) {
	// LIST_FORBIDDEN: a caller without qualifications.manage is denied
	// (defense-in-depth; the gateway answers the uniform hidden-existence 403,
	// FR-19).
	repo := qualificationsRepo()
	repo.users["schirr@gear.local"] = &User{
		ID: "u-schirr", Email: "schirr@gear.local", DisplayName: "Schirr Meister",
		FirstName: "Schirr", LastName: "Meister", State: StateActive,
	}
	repo.perms["u-schirr"] = []string{"tools.manage", "tool_types.manage"}
	svc := usersAdminService(t, repo)

	_, err := svc.ListQualifications(context.Background(), repo.users["schirr@gear.local"])
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("ListQualifications err = %v, want ErrForbidden", err)
	}
}

func TestListQualificationsUsersQualsManageAllowed(t *testing.T) {
	// Effort 2: a fuehrende/schirrmeister holding `users.qualifications.manage`
	// (but NOT `qualifications.manage`) can LIST the vocabulary so the user
	// detail can ADD a qualification. Create/update/assignees stay admin-only.
	repo := qualificationsRepo()
	repo.users["schirr@gear.local"] = &User{
		ID: "u-schirr", Email: "schirr@gear.local", DisplayName: "Schirr Meister",
		FirstName: "Schirr", LastName: "Meister", State: StateActive,
	}
	repo.perms["u-schirr"] = []string{"users.view", "users.qualifications.manage"}
	svc := usersAdminService(t, repo)

	res, err := svc.ListQualifications(context.Background(), repo.users["schirr@gear.local"])
	if err != nil {
		t.Fatalf("ListQualifications (users.qualifications.manage holder) failed: %v", err)
	}
	if res == nil || len(res.Qualifications) == 0 {
		t.Fatalf("ListQualifications returned no vocabulary for a users.qualifications.manage holder")
	}
}

// TestQualificationWritesUsersQualsManageForbidden pins the defense-in-depth
// split behind the over-widened qualifications sub-mount (Effort 2): the any-of
// mount ADMITS a fuehrende/schirrmeister holding users.qualifications.manage
// (so they can read the vocabulary), but the core re-check DENIES vocabulary
// WRITES to anyone without `qualifications.manage` — create/update stay
// admin-only.
func TestQualificationWritesUsersQualsManageForbidden(t *testing.T) {
	repo := qualificationsRepo()
	repo.users["schirr@gear.local"] = &User{
		ID: "u-schirr", Email: "schirr@gear.local", DisplayName: "Schirr Meister",
		FirstName: "Schirr", LastName: "Meister", State: StateActive,
	}
	repo.perms["u-schirr"] = []string{"users.view", "users.qualifications.manage"}
	svc := usersAdminService(t, repo)
	actor := repo.users["schirr@gear.local"]

	if _, err := svc.CreateQualification(context.Background(), actor, CreateQualificationInput{
		Name: "Neue Quali", ExpiryKind: QualificationExpiryUnlimited,
	}); !errors.Is(err, ErrForbidden) {
		t.Errorf("CreateQualification (users.qualifications.manage holder) err = %v, want ErrForbidden", err)
	}
	if _, err := svc.UpdateQualification(context.Background(), actor, "q-ketten", UpdateQualificationInput{
		Name: "Kettensäge umbenannt", ExpiryKind: QualificationExpiryFixed,
	}); !errors.Is(err, ErrForbidden) {
		t.Errorf("UpdateQualification (users.qualifications.manage holder) err = %v, want ErrForbidden", err)
	}
}

func TestCreateQualificationUnlimited(t *testing.T) {
	// CREATE_UNLIMITED: {name, expiry_kind:unlimited, no expires_at} creates a
	// qualification that is Unbegrenzt forever (FR-22).
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)

	q, err := svc.CreateQualification(context.Background(), qualificationsAdmin(repo), CreateQualificationInput{
		Name: "Erste Hilfe", Description: "Erste-Hilfe-Kurs", ExpiryKind: QualificationExpiryUnlimited,
	})
	if err != nil {
		t.Fatalf("CreateQualification failed: %v", err)
	}
	if q.Message != MsgQualificationCreated {
		t.Errorf("message = %q, want %q (server-authoritative success text)", q.Message, MsgQualificationCreated)
	}
	if q.Qualification.ExpiryKind != QualificationExpiryUnlimited {
		t.Errorf("expiry kind = %q, want unlimited", q.Qualification.ExpiryKind)
	}
	if q.Qualification.Status != QualificationStatusUnlimited {
		t.Errorf("status = %q, want %q", q.Qualification.Status, QualificationStatusUnlimited)
	}
}

func TestCreateQualificationFixed(t *testing.T) {
	// CREATE_FIXED: {name, expiry_kind:fixed} creates a qualification whose
	// vocabulary status is Befristet (2026-09-08 rework) — the per-user
	// valid-until is set at assignment, not here.
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)

	q, err := svc.CreateQualification(context.Background(), qualificationsAdmin(repo), CreateQualificationInput{
		Name: "Seilwinde", Description: "Seilwinden-Führerschein", ExpiryKind: QualificationExpiryFixed,
	})
	if err != nil {
		t.Fatalf("CreateQualification failed: %v", err)
	}
	if q.Qualification.Status != QualificationStatusFixed {
		t.Errorf("status = %q, want %q", q.Qualification.Status, QualificationStatusFixed)
	}
}

func TestCreateQualificationDupName(t *testing.T) {
	// CREATE_DUP_NAME: an existing name in a DIFFERENT case is a uniform 409
	// (case-insensitive uniqueness, like user groups).
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)

	_, err := svc.CreateQualification(context.Background(), qualificationsAdmin(repo), CreateQualificationInput{
		Name: "KETTENSÄGE", ExpiryKind: QualificationExpiryUnlimited,
	})
	if !errors.Is(err, ErrQualificationNameTaken) {
		t.Fatalf("CreateQualification(dup, case variant) err = %v, want ErrQualificationNameTaken", err)
	}
}

func TestCreateQualificationInvalid(t *testing.T) {
	// CREATE_INVALID: empty name, bad expiry kind and over-long description map
	// to the uniform 400 sentinels. There is no vocabulary-level date to
	// validate anymore (2026-09-08 rework) — a fixed qualification is valid
	// without one; the per-user valid-until is required at assignment.
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)

	cases := []struct {
		name  string
		input CreateQualificationInput
		want  error
	}{
		{"empty name", CreateQualificationInput{Name: "  ", ExpiryKind: QualificationExpiryUnlimited}, ErrQualificationInvalidName},
		{"bad expiry kind", CreateQualificationInput{Name: "X", ExpiryKind: "sometimes"}, ErrQualificationInvalidExpiryKind},
		{"long description", CreateQualificationInput{Name: "X", Description: strings.Repeat("x", QualificationDescriptionMaxLength+1), ExpiryKind: QualificationExpiryUnlimited}, ErrQualificationDescriptionTooLong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateQualification(context.Background(), qualificationsAdmin(repo), tc.input)
			if !errors.Is(err, tc.want) {
				t.Errorf("CreateQualification err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestUpdateQualificationValid(t *testing.T) {
	// UPDATE_VALID: editing name/description/expiry kind replaces the row
	// atomically.
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)

	q, err := svc.UpdateQualification(context.Background(), qualificationsAdmin(repo), "q-ketten", UpdateQualificationInput{
		Name: "Kettensäge neu", Description: "Neue Beschreibung", ExpiryKind: QualificationExpiryFixed,
	})
	if err != nil {
		t.Fatalf("UpdateQualification failed: %v", err)
	}
	if q.Message != MsgQualificationUpdated {
		t.Errorf("message = %q, want %q (server-authoritative success text)", q.Message, MsgQualificationUpdated)
	}
	if q.Qualification.Name != "Kettensäge neu" || q.Qualification.Description != "Neue Beschreibung" {
		t.Errorf("updated = %+v, want the new name/description", q.Qualification)
	}
	if q.Qualification.Status != QualificationStatusFixed {
		t.Errorf("status = %q, want %q (fixed vocabulary badge)", q.Qualification.Status, QualificationStatusFixed)
	}
}

func TestUpdateQualificationSwitchesToUnlimited(t *testing.T) {
	// UPDATE_EXPIRY_KIND: switching a fixed qualification to unlimited flips the
	// vocabulary badge to Unbegrenzt (never-expiring, FR-22).
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)

	q, err := svc.UpdateQualification(context.Background(), qualificationsAdmin(repo), "q-ketten", UpdateQualificationInput{
		Name: "Kettensäge", Description: "Unbegrenzt", ExpiryKind: QualificationExpiryUnlimited,
	})
	if err != nil {
		t.Fatalf("UpdateQualification failed: %v", err)
	}
	if q.Qualification.ExpiryKind != QualificationExpiryUnlimited {
		t.Errorf("expiry kind = %q, want unlimited", q.Qualification.ExpiryKind)
	}
	if q.Qualification.Status != QualificationStatusUnlimited {
		t.Errorf("status = %q, want %q", q.Qualification.Status, QualificationStatusUnlimited)
	}
}

func TestUpdateQualificationUnknown(t *testing.T) {
	// UPDATE_UNKNOWN: a nonexistent id maps to the uniform 404 sentinel.
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)

	_, err := svc.UpdateQualification(context.Background(), qualificationsAdmin(repo), "q-nope", UpdateQualificationInput{
		Name: "X", ExpiryKind: QualificationExpiryUnlimited,
	})
	if !errors.Is(err, ErrQualificationNotFound) {
		t.Fatalf("UpdateQualification err = %v, want ErrQualificationNotFound", err)
	}
}

func TestUpdateQualificationUnknownWithTakenName(t *testing.T) {
	// UPDATE_UNKNOWN_TAKEN_NAME (finding 6): an unknown id whose requested name
	// is held by ANOTHER qualification must map to the uniform 404 — never a 409
	// "name taken" (existence-first ordering).
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)
	repo.qualifications["q-andere"] = &QualificationAssignment{ID: "q-andere", Name: "Andere", ExpiryKind: QualificationExpiryUnlimited}

	_, err := svc.UpdateQualification(context.Background(), qualificationsAdmin(repo), "q-nope", UpdateQualificationInput{
		Name: "Andere", ExpiryKind: QualificationExpiryUnlimited,
	})
	if !errors.Is(err, ErrQualificationNotFound) {
		t.Fatalf("UpdateQualification(unknown id, taken name) err = %v, want ErrQualificationNotFound (never 409)", err)
	}
}

func TestUpdateQualificationDupName(t *testing.T) {
	// UPDATE_DUP_NAME: renaming onto a name held by ANOTHER qualification maps
	// to the uniform 409 conflict. Renaming to the OWN name stays legal.
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)
	repo.qualifications["q-andere"] = &QualificationAssignment{ID: "q-andere", Name: "Andere", ExpiryKind: QualificationExpiryUnlimited}

	if _, err := svc.UpdateQualification(context.Background(), qualificationsAdmin(repo), "q-ketten", UpdateQualificationInput{
		Name: "Andere", ExpiryKind: QualificationExpiryUnlimited,
	}); !errors.Is(err, ErrQualificationNameTaken) {
		t.Fatalf("UpdateQualification(dup) err = %v, want ErrQualificationNameTaken", err)
	}
	if _, err := svc.UpdateQualification(context.Background(), qualificationsAdmin(repo), "q-ketten", UpdateQualificationInput{
		Name: "kettenSÄGE", ExpiryKind: QualificationExpiryUnlimited,
	}); err != nil {
		t.Fatalf("UpdateQualification(own name, case variant) failed: %v", err)
	}
}

func TestAssignQualificationUsersValidAndImmediate(t *testing.T) {
	// ASSIGN_VALID + IMMEDIATE_EFFECT: assigning a volunteer is reflected
	// immediately in their user detail (AD-7/FR-22).
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)

	res, err := svc.AssignQualificationUsers(context.Background(), qualificationsAdmin(repo), "q-ketten", []string{"u-helfende"})
	if err != nil {
		t.Fatalf("AssignQualificationUsers failed: %v", err)
	}
	if res.Message != MsgQualificationAssigneesUpdated {
		t.Errorf("message = %q, want %q", res.Message, MsgQualificationAssigneesUpdated)
	}
	if len(res.Assignees) != 1 || res.Assignees[0].ID != "u-helfende" {
		t.Fatalf("assignees = %+v, want the helfende volunteer", res.Assignees)
	}

	detail, err := svc.GetUserDetail(context.Background(), qualificationsAdmin(repo), "u-helfende")
	if err != nil {
		t.Fatalf("GetUserDetail failed: %v", err)
	}
	if len(detail.Qualifications) != 1 || detail.Qualifications[0].Name != "Kettensäge" {
		t.Errorf("user detail qualifications = %+v, want the assignment on the next request", detail.Qualifications)
	}
}

func TestAssignQualificationUsersRemove(t *testing.T) {
	// ASSIGN_REMOVE: removing a volunteer from the assignee set revokes
	// eligibility immediately (AD-7/FR-22) — the next user detail no longer
	// carries the assignment.
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)
	repo.userQualifications["u-helfende"] = []string{"q-ketten"}

	res, err := svc.AssignQualificationUsers(context.Background(), qualificationsAdmin(repo), "q-ketten", nil)
	if err != nil {
		t.Fatalf("AssignQualificationUsers failed: %v", err)
	}
	if len(res.Assignees) != 0 {
		t.Errorf("assignees = %+v, want empty after removal", res.Assignees)
	}

	detail, err := svc.GetUserDetail(context.Background(), qualificationsAdmin(repo), "u-helfende")
	if err != nil {
		t.Fatalf("GetUserDetail failed: %v", err)
	}
	if len(detail.Qualifications) != 0 {
		t.Errorf("user detail qualifications = %+v, want [] (revoked immediately)", detail.Qualifications)
	}
}

func TestAssignQualificationUsersUnknown(t *testing.T) {
	// ASSIGN_UNKNOWN: an unknown qualification maps to 404; an unknown user id
	// maps to the uniform 400.
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)

	if _, err := svc.AssignQualificationUsers(context.Background(), qualificationsAdmin(repo), "q-nope", []string{"u-helfende"}); !errors.Is(err, ErrQualificationNotFound) {
		t.Fatalf("AssignQualificationUsers(unknown qual) err = %v, want ErrQualificationNotFound", err)
	}
	if _, err := svc.AssignQualificationUsers(context.Background(), qualificationsAdmin(repo), "q-ketten", []string{"u-ghost"}); !errors.Is(err, ErrQualificationAssigneeUnknown) {
		t.Fatalf("AssignQualificationUsers(unknown user) err = %v, want ErrQualificationAssigneeUnknown", err)
	}
}

func TestAssignQualificationUsersDeduplicates(t *testing.T) {
	// ASSIGN_DUP_IDS (finding 7): duplicate assignee ids are deduplicated BEFORE
	// the existence-count check, so a duplicate set succeeds with a single
	// assignee instead of a spurious ErrQualificationAssigneeUnknown.
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)

	res, err := svc.AssignQualificationUsers(context.Background(), qualificationsAdmin(repo), "q-ketten", []string{"u-helfende", "u-helfende"})
	if err != nil {
		t.Fatalf("AssignQualificationUsers(duplicate ids) failed: %v", err)
	}
	if len(res.Assignees) != 1 || res.Assignees[0].ID != "u-helfende" {
		t.Fatalf("assignees = %+v, want exactly the one volunteer", res.Assignees)
	}
}

func TestListQualificationAssignees(t *testing.T) {
	// ASSIGNEE_LIST: the current assignees (id + display name) are returned for
	// the editor's pre-checked set.
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)
	repo.userQualifications["u-helfende"] = []string{"q-ketten"}

	assignees, err := svc.ListQualificationAssignees(context.Background(), qualificationsAdmin(repo), "q-ketten")
	if err != nil {
		t.Fatalf("ListQualificationAssignees failed: %v", err)
	}
	if len(assignees) != 1 || assignees[0].ID != "u-helfende" {
		t.Fatalf("assignees = %+v, want the helfende volunteer", assignees)
	}
	if assignees[0].Name == "" {
		t.Errorf("assignee name is empty, want the display name")
	}

	if _, err := svc.ListQualificationAssignees(context.Background(), qualificationsAdmin(repo), "q-nope"); !errors.Is(err, ErrQualificationNotFound) {
		t.Fatalf("ListQualificationAssignees(unknown) err = %v, want ErrQualificationNotFound", err)
	}
}

func TestQualificationStatusDerivation(t *testing.T) {
	// STATUS_UNLIMITED / STATUS_FIXED: the vocabulary list derives the badge
	// from the expiry kind only (2026-09-08 rework) — unlimited → Unbegrenzt,
	// fixed → Befristet. The per-assignment date-driven status (Gültig / Bald
	// ablaufend / Abgelaufen) is covered by the assignment tests in
	// users_admin_test.go.
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)

	repo.qualifications["q-unlim"] = &QualificationAssignment{ID: "q-unlim", Name: "Unbegrenzt", ExpiryKind: QualificationExpiryUnlimited}
	repo.qualifications["q-fixed"] = &QualificationAssignment{ID: "q-fixed", Name: "Befristet", ExpiryKind: QualificationExpiryFixed}

	res, err := svc.ListQualifications(context.Background(), qualificationsAdmin(repo))
	if err != nil {
		t.Fatalf("ListQualifications failed: %v", err)
	}
	got := map[string]string{}
	for _, q := range res.Qualifications {
		got[q.Name] = q.Status
	}
	if got["Unbegrenzt"] != QualificationStatusUnlimited {
		t.Errorf("Unbegrenzt status = %q, want %q (never expires)", got["Unbegrenzt"], QualificationStatusUnlimited)
	}
	if got["Befristet"] != QualificationStatusFixed {
		t.Errorf("Befristet status = %q, want %q (fixed vocabulary badge)", got["Befristet"], QualificationStatusFixed)
	}
}
func TestQualificationExistsCatalogPort(t *testing.T) {
	// The ungated QualificationCatalogPort (AD-7/AD-11): the Tool module's
	// FK-validation seam. It reports whether a qualification id is in the
	// vocabulary, with NO permission re-check (the trusted internal read path).
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)

	exists, err := svc.QualificationExists(context.Background(), "q-ketten")
	if err != nil {
		t.Fatalf("QualificationExists(known) err = %v", err)
	}
	if !exists {
		t.Error("QualificationExists(q-ketten) = false, want true")
	}

	exists, err = svc.QualificationExists(context.Background(), "q-missing")
	if err != nil {
		t.Fatalf("QualificationExists(unknown) err = %v", err)
	}
	if exists {
		t.Error("QualificationExists(q-missing) = true, want false")
	}

	// The port is ungated: even a caller without qualifications.manage may
	// resolve the existence (the Tool write path calls it with the acting
	// admin's actor id only for the permission re-check, never for this).
	exists, err = svc.QualificationExists(context.Background(), "q-ketten")
	if err != nil {
		t.Fatalf("QualificationExists (no-actor path) err = %v", err)
	}
	if !exists {
		t.Error("QualificationExists(q-ketten) = false via ungated path, want true")
	}
}
