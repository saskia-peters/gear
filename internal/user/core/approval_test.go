package core

import (
	"context"
	"errors"
	"testing"
)

// approvalRepo seeds the Story 2.4 I/O matrix: one active admin holding
// `users.approve` plus a set of pending volunteers. The mock repository keys
// users by email and looks them up by ID through userByID.
func approvalRepo() *mockRepo {
	repo := newMockRepo()
	repo.users["admin@gear.local"] = &User{
		ID: "u-admin", Email: "admin@gear.local", DisplayName: "Admin",
		FirstName: "Vera", LastName: "Waltung", PasswordHash: "hashed:admin123456", State: StateActive,
	}
	repo.users["tim@gear.local"] = &User{
		ID: "u-tim", Email: "tim@gear.local", DisplayName: "Tim Müller",
		FirstName: "Tim", LastName: "Müller", PasswordHash: "hashed:tim123456", State: StatePendingApproval,
	}
	repo.users["lena@gear.local"] = &User{
		ID: "u-lena", Email: "lena@gear.local", DisplayName: "Lena Schmidt",
		FirstName: "Lena", LastName: "Schmidt", PasswordHash: "hashed:lena123456", State: StatePendingApproval,
	}
	repo.perms["u-admin"] = []string{UserApprovePermission}
	return repo
}

func approvalService(t *testing.T, repo *mockRepo) *Service {
	t.Helper()
	svc, _ := newTestService(repo, &mockHasher{})
	return svc
}

func TestListPendingValid(t *testing.T) {
	// LIST_PENDING: an admin holding users.approve sees the pending requests,
	// oldest first, with the submitted profile details (Vorname, Nachname,
	// E-Mail) plus id and created_at.
	repo := approvalRepo()
	repo.listPendingUsers = []*PendingUser{
		{ID: "u-tim", Vorname: "Tim", Nachname: "Müller", Email: "tim@gear.local"},
		{ID: "u-lena", Vorname: "Lena", Nachname: "Schmidt", Email: "lena@gear.local"},
	}
	svc := approvalService(t, repo)

	res, err := svc.ListPending(context.Background(), repo.users["admin@gear.local"])
	if err != nil {
		t.Fatalf("ListPending failed: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("pending count = %d, want 2", len(res))
	}
	if res[0].ID != "u-tim" || res[0].Email != "tim@gear.local" {
		t.Errorf("first pending = %+v, want u-tim", res[0])
	}
	if res[0].Vorname != "Tim" || res[0].Nachname != "Müller" {
		t.Errorf("first pending names = %s %s, want Tim Müller", res[0].Vorname, res[0].Nachname)
	}
}

func TestListPendingEmpty(t *testing.T) {
	// LIST_PENDING_EMPTY: no pending users -> empty list (the client shows the
	// "Keine ausstehenden Anträge" empty state).
	repo := approvalRepo()
	svc := approvalService(t, repo)

	res, err := svc.ListPending(context.Background(), repo.users["admin@gear.local"])
	if err != nil {
		t.Fatalf("ListPending failed: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("pending count = %d, want 0", len(res))
	}
}

func TestListPendingForbidden(t *testing.T) {
	// LIST_FORBIDDEN: a caller without users.approve is denied (the gateway
	// returns the uniform hidden-existence 403; the core re-verifies
	// defense-in-depth). A tools-only schirrmeister holds no users.* code.
	repo := approvalRepo()
	repo.users["schirr@gear.local"] = &User{
		ID: "u-schirr", Email: "schirr@gear.local", DisplayName: "Schirr Meister",
		FirstName: "Schirr", LastName: "Meister", State: StateActive,
	}
	repo.perms["u-schirr"] = []string{"tools.manage"}
	svc := approvalService(t, repo)

	_, err := svc.ListPending(context.Background(), repo.users["schirr@gear.local"])
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("ListPending err = %v, want ErrForbidden", err)
	}
}

func TestApproveUserValid(t *testing.T) {
	// APPROVE_VALID: approving a pending user moves them to active, seeds the
	// helfende role, writes the audit (user.approve, actor + target email) and
	// returns the confirmation. Login now succeeds (AD-2/AD-6) — the mock repo
	// flips the state so the login path sees an active account.
	repo := approvalRepo()
	svc := approvalService(t, repo)
	actor := repo.users["admin@gear.local"]
	target := repo.users["tim@gear.local"]

	res, err := svc.ApproveUser(context.Background(), actor, "u-tim")
	if err != nil {
		t.Fatalf("ApproveUser failed: %v", err)
	}
	if res.Message != MsgUserApproved {
		t.Errorf("message = %q, want %q", res.Message, MsgUserApproved)
	}
	if res.Email != "tim@gear.local" || res.UserID != "u-tim" {
		t.Errorf("result = %+v, want target tim", res)
	}
	if target.State != StateActive {
		t.Errorf("target state = %q, want active", target.State)
	}
	// Audit with the actor (admin) as actor and the target email as detail.
	if got := repo.audit["u-admin"]; len(got) != 1 || got[0] != AuditOperationUserApprove {
		t.Errorf("audit = %v, want [user.approve]", got)
	}
	if got := repo.auditDetail["u-admin"]; len(got) != 1 || got[0] != "target=tim@gear.local" {
		t.Errorf("audit detail = %v, want [target=tim@gear.local]", got)
	}
	if got := repo.auditSeverity["u-admin"]; len(got) != 1 || got[0] != AuditSeverityNormal {
		t.Errorf("audit severity = %v, want [normal]", got)
	}

	// LOGIN_AFTER_APPROVE: the approved user can now authenticate (active
	// state resolves the password hash, AD-2/AD-6). The role/permission seed
	// is verified at the repository integration layer; here the state flip is
	// what unlocks login.
	if _, err := svc.Login(context.Background(), LoginInput{Email: "tim@gear.local", Password: "tim123456"}); err != nil {
		t.Errorf("login after approve failed: %v", err)
	}
}

func TestApproveUserNonPending(t *testing.T) {
	// APPROVE_NONPENDING: an already active/deactivated user is NOT changed and
	// maps to the uniform not-found (no existence leak).
	repo := approvalRepo()
	repo.users["active@gear.local"] = &User{
		ID: "u-active", Email: "active@gear.local", DisplayName: "Aktiv",
		FirstName: "Ak", LastName: "Tiv", State: StateActive,
	}
	repo.users["deact@gear.local"] = &User{
		ID: "u-deact", Email: "deact@gear.local", DisplayName: "Deaktiviert",
		FirstName: "De", LastName: "Akt", State: StateDeactivated,
	}
	svc := approvalService(t, repo)

	_, err := svc.ApproveUser(context.Background(), repo.users["admin@gear.local"], "u-active")
	if !errors.Is(err, ErrUserNotPending) {
		t.Fatalf("approve active err = %v, want ErrUserNotPending", err)
	}
	_, err = svc.ApproveUser(context.Background(), repo.users["admin@gear.local"], "u-deact")
	if !errors.Is(err, ErrUserNotPending) {
		t.Fatalf("approve deactivated err = %v, want ErrUserNotPending", err)
	}
	if repo.users["active@gear.local"].State != StateActive || repo.users["deact@gear.local"].State != StateDeactivated {
		t.Errorf("non-pending states changed: active=%q deact=%q", repo.users["active@gear.local"].State, repo.users["deact@gear.local"].State)
	}
	if len(repo.audit["u-admin"]) != 0 {
		t.Errorf("no audit expected for non-pending approve, got %v", repo.audit["u-admin"])
	}
}

func TestApproveUserUnknown(t *testing.T) {
	// APPROVE_UNKNOWN: a nonexistent id maps to the uniform not-found error.
	repo := approvalRepo()
	svc := approvalService(t, repo)

	_, err := svc.ApproveUser(context.Background(), repo.users["admin@gear.local"], "u-nope")
	if !errors.Is(err, ErrUserNotPending) {
		t.Fatalf("approve unknown err = %v, want ErrUserNotPending", err)
	}
}

func TestApproveUserForbidden(t *testing.T) {
	// APPROVE_FORBIDDEN: a caller without users.approve is denied even though
	// the target is pending (defense-in-depth, AD-6).
	repo := approvalRepo()
	repo.users["schirr@gear.local"] = &User{
		ID: "u-schirr", Email: "schirr@gear.local", DisplayName: "Schirr Meister",
		FirstName: "Schirr", LastName: "Meister", State: StateActive,
	}
	repo.perms["u-schirr"] = []string{"tools.manage"}
	svc := approvalService(t, repo)

	_, err := svc.ApproveUser(context.Background(), repo.users["schirr@gear.local"], "u-tim")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("approve err = %v, want ErrForbidden", err)
	}
	if repo.users["tim@gear.local"].State != StatePendingApproval {
		t.Errorf("target state = %q, want unchanged pending_approval", repo.users["tim@gear.local"].State)
	}
}

func TestRejectUserValid(t *testing.T) {
	// REJECT_VALID: rejecting a pending user moves them to deactivated (the
	// pending record disappears, login is blocked, re-registration is blocked
	// because the email stays taken) and writes the audit (user.reject, target
	// email, severity normal).
	repo := approvalRepo()
	svc := approvalService(t, repo)
	actor := repo.users["admin@gear.local"]
	target := repo.users["tim@gear.local"]

	res, err := svc.RejectUser(context.Background(), actor, "u-tim")
	if err != nil {
		t.Fatalf("RejectUser failed: %v", err)
	}
	if res.Message != MsgUserRejected {
		t.Errorf("message = %q, want %q", res.Message, MsgUserRejected)
	}
	if target.State != StateDeactivated {
		t.Errorf("target state = %q, want deactivated", target.State)
	}

	// Cannot log in: the deactivated account resolves the dummy hash (uniform
	// invalid credentials, UX-DR7) — login never succeeds for a rejected user.
	if _, err := svc.Login(context.Background(), LoginInput{Email: "tim@gear.local", Password: "tim12345678"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("login after reject err = %v, want ErrInvalidCredentials", err)
	}

	// Cannot re-register: the email stays taken, so registration answers the
	// uniform anti-enumeration confirmation without creating a duplicate.
	reg, err := svc.Register(context.Background(), RegisterInput{
		FirstName: "Tim", LastName: "Müller", Email: "tim@gear.local",
		Password: "tim12345678", PasswordConfirm: "tim12345678",
	})
	if err != nil {
		t.Fatalf("re-register failed: %v", err)
	}
	if reg.Status != string(StatePendingApproval) {
		t.Errorf("re-register status = %q, want pending_approval", reg.Status)
	}

	if got := repo.audit["u-admin"]; len(got) != 1 || got[0] != AuditOperationUserReject {
		t.Errorf("audit = %v, want [user.reject]", got)
	}
	if got := repo.auditDetail["u-admin"]; len(got) != 1 || got[0] != "target=tim@gear.local" {
		t.Errorf("audit detail = %v, want [target=tim@gear.local]", got)
	}
}

func TestRejectUserNonPendingAndUnknown(t *testing.T) {
	// REJECT_NONPENDING / REJECT_UNKNOWN: an active, deactivated or unknown
	// target maps to the uniform not-found error with no state change.
	repo := approvalRepo()
	repo.users["active@gear.local"] = &User{
		ID: "u-active", Email: "active@gear.local", DisplayName: "Aktiv",
		FirstName: "Ak", LastName: "Tiv", State: StateActive,
	}
	svc := approvalService(t, repo)

	_, err := svc.RejectUser(context.Background(), repo.users["admin@gear.local"], "u-active")
	if !errors.Is(err, ErrUserNotPending) {
		t.Fatalf("reject active err = %v, want ErrUserNotPending", err)
	}
	_, err = svc.RejectUser(context.Background(), repo.users["admin@gear.local"], "u-nope")
	if !errors.Is(err, ErrUserNotPending) {
		t.Fatalf("reject unknown err = %v, want ErrUserNotPending", err)
	}
	if len(repo.audit["u-admin"]) != 0 {
		t.Errorf("no audit expected for non-pending reject, got %v", repo.audit["u-admin"])
	}
}

func TestApprovalAuditBestEffort(t *testing.T) {
	// NFR-O1: an audit-write failure is logged, never rolled back into the
	// transition — the approved user stays active and the call still succeeds.
	repo := approvalRepo()
	repo.auditErr = errors.New("audit down")
	svc := approvalService(t, repo)

	res, err := svc.ApproveUser(context.Background(), repo.users["admin@gear.local"], "u-tim")
	if err != nil {
		t.Fatalf("ApproveUser with failing audit failed: %v", err)
	}
	if res.Email != "tim@gear.local" {
		t.Errorf("result = %+v, want target tim", res)
	}
	if repo.users["tim@gear.local"].State != StateActive {
		t.Errorf("target state = %q, want active (transition must not be lost)", repo.users["tim@gear.local"].State)
	}
}