package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
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
	// The fixed 90-day-out qualification shows "Gültig" (FR-22/AD-7).
	if res.Qualifications[0].Status != QualificationStatusValid {
		t.Errorf("status = %q, want %q", res.Qualifications[0].Status, QualificationStatusValid)
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
	if q.Qualification.ExpiryKind != QualificationExpiryUnlimited || q.Qualification.ExpiresAt != nil {
		t.Errorf("expiry model = kind %q at %v, want unlimited with no date", q.Qualification.ExpiryKind, q.Qualification.ExpiresAt)
	}
	if q.Qualification.Status != QualificationStatusUnlimited {
		t.Errorf("status = %q, want %q", q.Qualification.Status, QualificationStatusUnlimited)
	}
}

func TestCreateQualificationFixed(t *testing.T) {
	// CREATE_FIXED: {name, expiry_kind:fixed, expires_at future} creates a
	// qualification whose status derives from expires_at.
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)
	future := time.Now().UTC().Add(90 * 24 * time.Hour)

	q, err := svc.CreateQualification(context.Background(), qualificationsAdmin(repo), CreateQualificationInput{
		Name: "Seilwinde", Description: "Seilwinden-Führerschein", ExpiryKind: QualificationExpiryFixed, ExpiresAt: &future,
	})
	if err != nil {
		t.Fatalf("CreateQualification failed: %v", err)
	}
	if q.Qualification.Status != QualificationStatusValid {
		t.Errorf("status = %q, want %q", q.Qualification.Status, QualificationStatusValid)
	}
}

func TestCreateQualificationSameDayExpiry(t *testing.T) {
	// CREATE_FIXED_SAME_DAY (finding 10): a qualification expiring LATER today
	// (end-of-day local serializes to a strictly-future instant) is accepted —
	// client and server agree that "today" is a valid expiry.
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)
	now := time.Now().UTC()
	// End of the current UTC day (a "later today" expiry); guard the
	// midnight-rollover window so the test never flakes.
	endOfToday := now.Truncate(24 * time.Hour).Add(24*time.Hour).Add(-time.Second)
	if !endOfToday.After(now) {
		endOfToday = now.Add(30 * time.Minute)
	}

	q, err := svc.CreateQualification(context.Background(), qualificationsAdmin(repo), CreateQualificationInput{
		Name: "Noch Heute", Description: "", ExpiryKind: QualificationExpiryFixed, ExpiresAt: &endOfToday,
	})
	if err != nil {
		t.Fatalf("CreateQualification(later today) failed: %v", err)
	}
	if q.Qualification.Status != QualificationStatusExpiringSoon && q.Qualification.Status != QualificationStatusValid {
		t.Errorf("status = %q, want a non-expired indicator for a later-today expiry", q.Qualification.Status)
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
	// CREATE_INVALID: empty name, bad expiry kind, fixed without a future
	// expires_at all map to the uniform 400 sentinels.
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)
	now := time.Now().UTC()

	cases := []struct {
		name  string
		input CreateQualificationInput
		want  error
	}{
		{"empty name", CreateQualificationInput{Name: "  ", ExpiryKind: QualificationExpiryUnlimited}, ErrQualificationInvalidName},
		{"bad expiry kind", CreateQualificationInput{Name: "X", ExpiryKind: "sometimes"}, ErrQualificationInvalidExpiryKind},
		{"fixed without date", CreateQualificationInput{Name: "X", ExpiryKind: QualificationExpiryFixed}, ErrQualificationInvalidExpiresAt},
		{"fixed with past date", CreateQualificationInput{Name: "X", ExpiryKind: QualificationExpiryFixed, ExpiresAt: &now}, ErrQualificationInvalidExpiresAt},
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
	// UPDATE_VALID: editing name/description/expiry replaces the row atomically.
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)
	future := time.Now().UTC().Add(7 * 24 * time.Hour)

	q, err := svc.UpdateQualification(context.Background(), qualificationsAdmin(repo), "q-ketten", UpdateQualificationInput{
		Name: "Kettensäge neu", Description: "Neue Beschreibung", ExpiryKind: QualificationExpiryFixed, ExpiresAt: &future,
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
	if q.Qualification.Status != QualificationStatusExpiringSoon {
		t.Errorf("status = %q, want %q (7 days out)", q.Qualification.Status, QualificationStatusExpiringSoon)
	}
}

func TestUpdateQualificationSwitchesToUnlimitedClearsExpiry(t *testing.T) {
	// UPDATE_EXPIRY_MODEL: switching a fixed qualification to unlimited clears
	// the stored expires_at (never-expiring; the Bald ablaufend/Abgelaufen
	// states never apply, FR-22).
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)

	q, err := svc.UpdateQualification(context.Background(), qualificationsAdmin(repo), "q-ketten", UpdateQualificationInput{
		Name: "Kettensäge", Description: "Unbegrenzt", ExpiryKind: QualificationExpiryUnlimited,
	})
	if err != nil {
		t.Fatalf("UpdateQualification failed: %v", err)
	}
	if q.Qualification.ExpiresAt != nil {
		t.Errorf("expires_at = %v, want cleared on unlimited", q.Qualification.ExpiresAt)
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
	// STATUS_UNLIMITED / STATUS_FIXED_EXPIRED / STATUS_FIXED_SOON: the list
	// surface derives the correct status for unlimited, expired and soon-to-
	// expire qualifications (FR-22/AD-7, reusing the Story 2.6 derivation).
	now := time.Now().UTC()
	repo := qualificationsRepo()
	svc := usersAdminService(t, repo)

	far := now.Add(90 * 24 * time.Hour)
	soon := now.Add(7 * 24 * time.Hour)
	past := now.Add(-1 * 24 * time.Hour)
	repo.qualifications["q-unlim"] = &QualificationAssignment{ID: "q-unlim", Name: "Unbegrenzt", ExpiryKind: QualificationExpiryUnlimited}
	repo.qualifications["q-far"] = &QualificationAssignment{ID: "q-far", Name: "Gültig", ExpiryKind: QualificationExpiryFixed, ExpiresAt: &far}
	repo.qualifications["q-soon"] = &QualificationAssignment{ID: "q-soon", Name: "Bald", ExpiryKind: QualificationExpiryFixed, ExpiresAt: &soon}
	repo.qualifications["q-past"] = &QualificationAssignment{ID: "q-past", Name: "Abgelaufen", ExpiryKind: QualificationExpiryFixed, ExpiresAt: &past}

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
	if got["Gültig"] != QualificationStatusValid {
		t.Errorf("Gültig status = %q, want %q", got["Gültig"], QualificationStatusValid)
	}
	if got["Bald"] != QualificationStatusExpiringSoon {
		t.Errorf("Bald status = %q, want %q", got["Bald"], QualificationStatusExpiringSoon)
	}
	if got["Abgelaufen"] != QualificationStatusExpired {
		t.Errorf("Abgelaufen status = %q, want %q", got["Abgelaufen"], QualificationStatusExpired)
	}
}