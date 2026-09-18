package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// seedDsgvoTarget builds a fully-populated report target on the mock repo
// (Story 3.3, FR-24): an active user with the profile row (attributes +
// timestamps + every secret column set), a role, an organisational user-group
// membership, a direct grant, a qualification assignment, two sessions and a
// login-attempt record. The export must assemble all of them — and never the
// secrets.
func seedDsgvoTarget(repo *mockRepo) {
	repo.users["export@gear.local"] = &User{
		ID:                     "u-export",
		Email:                  "export@gear.local",
		PendingEmail:           "neu@gear.local",
		DisplayName:            "Dat Auskunft",
		FirstName:              "Dat",
		LastName:               "Auskunft",
		PasswordHash:           "$argon2id$…secret-hash…",
		State:                  StateActive,
		IsMFAEnabled:           true,
		TotpSecretEncrypted:    "enc:totp-secret",
		PendingTotpSecretEncrypted: "enc:pending-totp",
		PendingTotpExpiresAt:   time.Now().UTC().Add(time.Hour),
		MustChangePassword:     false,
		OneTimePasswordHash:    "$argon2id$…otp-hash…",
		OneTimePasswordExpiresAt: time.Now().UTC().Add(15 * time.Minute),
		Attributes:             map[string]any{"geburtsjahr": "1990", "mitglied_seit": "2020"},
		CreatedAt:              time.Now().UTC().Add(-365 * 24 * time.Hour),
		UpdatedAt:              time.Now().UTC().Add(-time.Hour),
	}
	repo.seedRoleGroup("g-helfende", "helfende", "Base role: inspects tools", true,
		[]string{"dashboard.view", "inspection.submit"}, "u-export")
	repo.seedUserGroup("ug-ost", "Gruppe Ost", "Östliche Gruppe", "u-export")
	repo.directGrants["u-export"] = []string{"report.export"}
	repo.qualifications["q-ketten"] = &QualificationAssignment{
		ID: "q-ketten", Name: "Kettensäge", Description: "Kettensägen-Führerschein",
		ExpiryKind: QualificationExpiryFixed,
	}
	repo.userQualifications["u-export"] = []string{"q-ketten"}
	repo.userSessions["u-export"] = []UserSessionExport{
		{ID: "sess-2", CreatedAt: time.Now().UTC().Add(-time.Hour), ExpiresAt: time.Now().UTC().Add(7 * time.Hour)},
		{ID: "sess-1", CreatedAt: time.Now().UTC().Add(-24 * time.Hour), ExpiresAt: time.Now().UTC().Add(6 * time.Hour)},
	}
	repo.attempts["export@gear.local"] = &LoginAttempts{
		Email:        "export@gear.local",
		FailedCount:  2,
		LockoutUntil: time.Now().UTC().Add(30 * time.Second),
		UpdatedAt:    time.Now().UTC().Add(-time.Minute),
	}
}

func TestExportUserDataFull(t *testing.T) {
	// REPORT_OK: an existing user yields the full export — profile + roles +
	// user groups + direct grants + qualifications (with the derived display
	// status) + sessions (newest first) + login-attempt state.
	repo := newMockRepo()
	seedDsgvoTarget(repo)
	svc := rolesService(t, repo)

	export, err := svc.ExportUserData(context.Background(), "u-export")
	if err != nil {
		t.Fatalf("ExportUserData err = %v, want success", err)
	}

	if export.Profile.ID != "u-export" || export.Profile.Email != "export@gear.local" {
		t.Errorf("profile identity = %+v", export.Profile)
	}
	if export.Profile.PendingEmail != "neu@gear.local" {
		t.Errorf("profile pending_email = %q, want neu@gear.local", export.Profile.PendingEmail)
	}
	if export.Profile.DisplayName != "Dat Auskunft" || export.Profile.FirstName != "Dat" || export.Profile.LastName != "Auskunft" {
		t.Errorf("profile names = %+v", export.Profile)
	}
	if export.Profile.State != StateActive || !export.Profile.IsMFAEnabled {
		t.Errorf("profile state/mfa = %+v", export.Profile)
	}
	if export.Profile.Attributes["geburtsjahr"] != "1990" || export.Profile.Attributes["mitglied_seit"] != "2020" {
		t.Errorf("profile attributes = %v, want the stored set", export.Profile.Attributes)
	}
	if export.Profile.CreatedAt.IsZero() || export.Profile.UpdatedAt.IsZero() {
		t.Errorf("profile timestamps missing: created=%v updated=%v", export.Profile.CreatedAt, export.Profile.UpdatedAt)
	}

	if len(export.Roles) != 1 || export.Roles[0].Name != "helfende" {
		t.Errorf("roles = %+v, want the helfende role", export.Roles)
	}
	if len(export.UserGroups) != 1 || export.UserGroups[0].Name != "Gruppe Ost" {
		t.Errorf("user_groups = %+v, want Gruppe Ost", export.UserGroups)
	}
	if len(export.DirectGrants) != 1 || export.DirectGrants[0].Code != "report.export" {
		t.Errorf("direct_grants = %+v, want the report.export grant", export.DirectGrants)
	}
	if len(export.Qualifications) != 1 {
		t.Fatalf("qualifications = %+v, want one assignment", export.Qualifications)
	}
	if export.Qualifications[0].Name != "Kettensäge" || export.Qualifications[0].Status == "" {
		t.Errorf("qualification = %+v, want the Kettensäge assignment with a derived status", export.Qualifications[0])
	}

	// Sessions newest-first (sess-2 before sess-1), no token material.
	if len(export.Sessions) != 2 || export.Sessions[0].ID != "sess-2" || export.Sessions[1].ID != "sess-1" {
		t.Errorf("sessions = %+v, want [sess-2, sess-1] newest first", export.Sessions)
	}
	if export.Sessions[0].ExpiresAt.IsZero() || export.Sessions[0].CreatedAt.IsZero() {
		t.Errorf("session timestamps missing: %+v", export.Sessions[0])
	}

	if export.LoginAttempts == nil {
		t.Fatal("login_attempts = nil, want the tracked state")
	}
	if export.LoginAttempts.Email != "export@gear.local" || export.LoginAttempts.FailedCount != 2 {
		t.Errorf("login_attempts = %+v, want email + failed_count 2", export.LoginAttempts)
	}
	if export.LoginAttempts.LockoutUntil == nil || export.LoginAttempts.LockoutUntil.IsZero() {
		t.Errorf("login_attempts lockout_until = %v, want the active window", export.LoginAttempts.LockoutUntil)
	}
}

func TestExportUserDataNoSecrets(t *testing.T) {
	// REPORT_SECRETS: the export carries no authenticator — no password hash,
	// no TOTP secret (stored or pending), no one-time-password hash. The JSON
	// payload must not contain any of them.
	repo := newMockRepo()
	seedDsgvoTarget(repo)
	svc := rolesService(t, repo)

	export, err := svc.ExportUserData(context.Background(), "u-export")
	if err != nil {
		t.Fatalf("ExportUserData err = %v, want success", err)
	}
	raw, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export err = %v", err)
	}
	body := string(raw)
	for _, secret := range []string{
		"argon2id", "secret-hash", "enc:totp-secret", "enc:pending-totp", "otp-hash",
		"password_hash", "totp_secret", "one_time_password",
	} {
		if strings.Contains(body, secret) {
			t.Errorf("export JSON leaks secret material %q: %s", secret, body)
		}
	}
}

func TestExportUserDataNoAuthHistory(t *testing.T) {
	// REPORT_NO_AUTH: a user with no sessions/attempts gets an EMPTY sessions
	// list and a NIL login-attempt state — the SPA renders the German empty
	// note, never an error.
	repo := newMockRepo()
	repo.users["empty@gear.local"] = &User{
		ID: "u-empty", Email: "empty@gear.local", DisplayName: "Ohne Historie",
		FirstName: "Ohne", LastName: "Historie", State: StateActive,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	svc := rolesService(t, repo)

	export, err := svc.ExportUserData(context.Background(), "u-empty")
	if err != nil {
		t.Fatalf("ExportUserData err = %v, want success", err)
	}
	if export.Sessions == nil || len(export.Sessions) != 0 {
		t.Errorf("sessions = %+v, want an empty non-nil list", export.Sessions)
	}
	if export.LoginAttempts != nil {
		t.Errorf("login_attempts = %+v, want nil (no tracked attempts)", export.LoginAttempts)
	}
	if export.Profile.Attributes == nil {
		t.Error("profile attributes = nil, want a concrete empty object")
	}
}

func TestExportUserDataUnknownUser(t *testing.T) {
	// REPORT_UNKNOWN: an unknown target id maps to ErrAdminUserNotFound (the
	// uniform 404 sentinel).
	repo := newMockRepo()
	svc := rolesService(t, repo)

	_, err := svc.ExportUserData(context.Background(), "u-gibtsnicht")
	if !errors.Is(err, ErrAdminUserNotFound) {
		t.Fatalf("err = %v, want ErrAdminUserNotFound", err)
	}
}