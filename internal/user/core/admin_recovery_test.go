package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

// recoveryRepo seeds the FR-27 I/O matrix: two active admins (A and B) plus a
// non-admin volunteer. It is the standard fixture for the dual-admin recovery
// use-cases.
func recoveryRepo() *mockRepo {
	repo := newMockRepo()
	repo.users["admina@gear.local"] = &User{
		ID: "u-admina", Email: "admina@gear.local", DisplayName: "Admin A",
		FirstName: "Admin", LastName: "A", PasswordHash: "hashed:admina123456", State: StateActive,
	}
	repo.users["adminb@gear.local"] = &User{
		ID: "u-adminb", Email: "adminb@gear.local", DisplayName: "Admin B",
		FirstName: "Admin", LastName: "B", PasswordHash: "hashed:adminb123456", State: StateActive,
	}
	repo.users["volunteer@example.com"] = &User{
		ID: "u-volunteer", Email: "volunteer@example.com", DisplayName: "Volunteer",
		FirstName: "Frei", LastName: "Willig", PasswordHash: "hashed:geheim123456", State: StateActive,
	}
	repo.adminGroup["u-admina"] = true
	repo.adminGroup["u-adminb"] = true
	// Defense-in-depth (review finding 1.10): the approve path re-verifies the
	// caller holds `admin.recovery.approve` via ListPermissionsByUser, so the
	// fixture seeds the resolved permission set for both admins.
	repo.perms["u-admina"] = []string{AdminRecoveryApprovePermission}
	repo.perms["u-adminb"] = []string{AdminRecoveryApprovePermission}
	return repo
}

func recoveryService(t *testing.T, repo *mockRepo) (*Service, *mockSessionStore) {
	t.Helper()
	svc, store := newTestService(repo, &mockHasher{})
	return svc, store
}

func TestRequestAdminRecoveryValid(t *testing.T) {
	// RECOVERY_REQUEST: admin A (via a caller with a valid session) requests
	// recovery for their own account. A recovery-marked token is created for A
	// and the request is audited with the caller as actor.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	caller := repo.users["admina@gear.local"]

	res, err := svc.RequestAdminRecovery(context.Background(), caller, "admina@gear.local")
	if err != nil {
		t.Fatalf("RequestAdminRecovery failed: %v", err)
	}
	if res.Message != MsgAdminRecoveryRequested {
		t.Errorf("message = %q, want %q", res.Message, MsgAdminRecoveryRequested)
	}
	if res.TargetEmail != "admina@gear.local" {
		t.Errorf("target_email = %q, want admina@gear.local", res.TargetEmail)
	}
	// Exactly one pending recovery token for A, not yet approved.
	if len(repo.adminRecovery) != 1 {
		t.Fatalf("expected 1 pending recovery token, got %d", len(repo.adminRecovery))
	}
	for _, tok := range repo.adminRecovery {
		if tok.UserID != "u-admina" {
			t.Errorf("recovery token user = %q, want u-admina", tok.UserID)
		}
		if tok.ApprovedByUserID != "" {
			t.Errorf("recovery token must start unapproved, got approver %q", tok.ApprovedByUserID)
		}
		if !tok.ExpiresAt.After(time.Now().UTC().Add(29 * time.Minute)) {
			t.Errorf("recovery token expiry = %v, want ~now+30min", tok.ExpiresAt)
		}
	}
	// Audit with the caller (A) as actor.
	if got := repo.audit["u-admina"]; len(got) != 1 || got[0] != AuditOperationAdminRecoveryRequest {
		t.Errorf("audit = %v, want [admin.recovery.request]", got)
	}
}

func TestRequestAdminRecoveryInvalidatesEarlierRequest(t *testing.T) {
	// A second request for the same target invalidates the earlier recovery
	// request (only the latest stays valid, FR-27).
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	caller := repo.users["admina@gear.local"]

	if _, err := svc.RequestAdminRecovery(context.Background(), caller, "admina@gear.local"); err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	if _, err := svc.RequestAdminRecovery(context.Background(), caller, "admina@gear.local"); err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	if len(repo.adminRecovery) != 1 {
		t.Fatalf("expected exactly 1 recovery token after re-request, got %d", len(repo.adminRecovery))
	}
	if got := repo.audit["u-admina"]; len(got) != 2 {
		t.Errorf("audit = %v, want 2 admin.recovery.request rows", got)
	}
}

func TestRequestAdminRecoveryNonAdminCallerForbidden(t *testing.T) {
	// RECOVERY_NO_PERMISSION: a non-admin caller cannot create a recovery
	// request (403 forbidden upstream).
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	caller := repo.users["volunteer@example.com"]

	_, err := svc.RequestAdminRecovery(context.Background(), caller, "admina@gear.local")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error = %v, want ErrForbidden", err)
	}
	if len(repo.adminRecovery) != 0 {
		t.Error("no recovery token may be created for a forbidden caller")
	}
}

func TestRequestAdminRecoveryLastAdminBlocked(t *testing.T) {
	// RECOVERY_LAST_ADMIN: when the target is the last active admin, self-recovery
	// is disabled with ErrLastAdminRecoveryBlocked and audited
	// (admin.recovery.last_admin_blocked).
	repo := recoveryRepo()
	// Only one active admin remains (B is deactivated).
	repo.users["adminb@gear.local"].State = StateDeactivated
	svc, _ := recoveryService(t, repo)
	caller := repo.users["admina@gear.local"]

	_, err := svc.RequestAdminRecovery(context.Background(), caller, "admina@gear.local")
	if !errors.Is(err, ErrLastAdminRecoveryBlocked) {
		t.Fatalf("error = %v, want ErrLastAdminRecoveryBlocked", err)
	}
	if len(repo.adminRecovery) != 0 {
		t.Error("no recovery token may be created for the last admin")
	}
	if got := repo.audit["u-admina"]; len(got) != 1 || got[0] != AuditOperationAdminRecoveryLastAdminBlocked {
		t.Errorf("audit = %v, want [admin.recovery.last_admin_blocked]", got)
	}
}

func TestRequestAdminRecoveryNonActionableTargetIsUniform(t *testing.T) {
	// Anti-enumeration: requesting recovery for a non-existent / non-admin target
	// returns the uniform confirmation and creates NO token.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	caller := repo.users["admina@gear.local"]

	for _, email := range []string{"nobody@gear.local", "volunteer@example.com"} {
		res, err := svc.RequestAdminRecovery(context.Background(), caller, email)
		if err != nil {
			t.Fatalf("RequestAdminRecovery(%q) failed: %v", email, err)
		}
		if res.Message != MsgAdminRecoveryRequested {
			t.Errorf("message = %q, want uniform %q", res.Message, MsgAdminRecoveryRequested)
		}
	}
	if len(repo.adminRecovery) != 0 {
		t.Errorf("no recovery token may be created for a non-actionable target, got %d", len(repo.adminRecovery))
	}
}

func TestApproveAdminRecoveryValid(t *testing.T) {
	// RECOVERY_APPROVE_VALID: admin B approves A's request with a Begründung +
	// confirmation → A gains a single-use hashed 30-min token returned to B,
	// audited as admin.recovery.approve with B as actor.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	adminA := repo.users["admina@gear.local"]
	adminB := repo.users["adminb@gear.local"]

	if _, err := svc.RequestAdminRecovery(context.Background(), adminA, "admina@gear.local"); err != nil {
		t.Fatalf("request failed: %v", err)
	}

	res, err := svc.ApproveAdminRecovery(context.Background(), adminB, "admina@gear.local", "Admin A ist ausgesperrt", true, "")
	if err != nil {
		t.Fatalf("ApproveAdminRecovery failed: %v", err)
	}
	if res.Message != MsgAdminRecoveryApproved {
		t.Errorf("message = %q, want %q", res.Message, MsgAdminRecoveryApproved)
	}
	if res.RecoveryToken == "" {
		t.Error("approve must return the raw single-use token to B")
	}
	// The stored token must be the SHA-256 of the returned raw token, and be
	// approved by B.
	stored := repo.adminRecovery[hashToken(res.RecoveryToken)]
	if stored == nil {
		t.Fatal("stored approved token hash must match SHA-256 of the returned raw token")
	}
	if stored.ApprovedByUserID != "u-adminb" {
		t.Errorf("approved_by = %q, want u-adminb", stored.ApprovedByUserID)
	}
	if got := repo.audit["u-adminb"]; len(got) != 1 || got[0] != AuditOperationAdminRecoveryApprove {
		t.Errorf("audit = %v, want [admin.recovery.approve]", got)
	}
}

func TestApproveAdminRecoveryMissingReason(t *testing.T) {
	// RECOVERY_APPROVE_NO_REASON: a missing Begründung is rejected with
	// ErrRecoveryReasonRequired and no token is issued.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	adminA := repo.users["admina@gear.local"]
	adminB := repo.users["adminb@gear.local"]
	if _, err := svc.RequestAdminRecovery(context.Background(), adminA, "admina@gear.local"); err != nil {
		t.Fatalf("request failed: %v", err)
	}

	_, err := svc.ApproveAdminRecovery(context.Background(), adminB, "admina@gear.local", "  ", true, "")
	if !errors.Is(err, ErrRecoveryReasonRequired) {
		t.Fatalf("error = %v, want ErrRecoveryReasonRequired", err)
	}
	if len(repo.audit["u-adminb"]) != 0 {
		t.Error("no approval audit may be written on a missing reason")
	}
}

func TestApproveAdminRecoveryNotConfirmed(t *testing.T) {
	// RECOVERY_APPROVE_NO_CONFIRM: an unchecked confirmation is rejected with
	// ErrRecoveryNotConfirmed and no token is issued.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	adminA := repo.users["admina@gear.local"]
	adminB := repo.users["adminb@gear.local"]
	if _, err := svc.RequestAdminRecovery(context.Background(), adminA, "admina@gear.local"); err != nil {
		t.Fatalf("request failed: %v", err)
	}

	_, err := svc.ApproveAdminRecovery(context.Background(), adminB, "admina@gear.local", "Begründung", false, "")
	if !errors.Is(err, ErrRecoveryNotConfirmed) {
		t.Fatalf("error = %v, want ErrRecoveryNotConfirmed", err)
	}
}

func TestApproveAdminRecoverySelfApprovalForbidden(t *testing.T) {
	// RECOVERY_SELF_APPROVE: admin A cannot approve their own recovery (403).
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	adminA := repo.users["admina@gear.local"]
	if _, err := svc.RequestAdminRecovery(context.Background(), adminA, "admina@gear.local"); err != nil {
		t.Fatalf("request failed: %v", err)
	}

	_, err := svc.ApproveAdminRecovery(context.Background(), adminA, "admina@gear.local", "selbst", true, "")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("error = %v, want ErrForbidden", err)
	}
	if len(repo.audit["u-admina"]) != 1 || repo.audit["u-admina"][0] != AuditOperationAdminRecoveryRequest {
		t.Errorf("audit = %v, want only the request row (no approve)", repo.audit["u-admina"])
	}
}

func TestApproveAdminRecoveryNoPendingRequestInvalid(t *testing.T) {
	// Approving a target with no pending / approvable request maps to
	// ErrAdminRecoveryInvalid.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	adminB := repo.users["adminb@gear.local"]

	_, err := svc.ApproveAdminRecovery(context.Background(), adminB, "admina@gear.local", "Begründung", true, "")
	if !errors.Is(err, ErrAdminRecoveryInvalid) {
		t.Fatalf("error = %v, want ErrAdminRecoveryInvalid", err)
	}
}

func TestCompleteAdminRecoveryValid(t *testing.T) {
	// RECOVERY_SET_PASSWORD: A opens the recovery link and sets a new password;
	// the token is invalidated, ALL sessions revoked, and the completion is
	// audited (admin.recovery.complete).
	repo := recoveryRepo()
	svc, store := recoveryService(t, repo)
	adminA := repo.users["admina@gear.local"]
	adminB := repo.users["adminb@gear.local"]

	if _, err := svc.RequestAdminRecovery(context.Background(), adminA, "admina@gear.local"); err != nil {
		t.Fatalf("request failed: %v", err)
	}
	approved, err := svc.ApproveAdminRecovery(context.Background(), adminB, "admina@gear.local", "Ausgesperrt", true, "")
	if err != nil {
		t.Fatalf("approve failed: %v", err)
	}
	// Two pre-existing sessions that must be revoked (re-auth required).
	if _, err := store.CreateSession(context.Background(), "u-admina", "hash-1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession(1) failed: %v", err)
	}
	if _, err := store.CreateSession(context.Background(), "u-admina", "hash-2", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession(2) failed: %v", err)
	}
	if len(store.sessions) != 2 {
		t.Fatalf("setup: want 2 sessions, got %d", len(store.sessions))
	}

	res, err := svc.CompleteAdminRecovery(context.Background(), approved.RecoveryToken, "neuesadminpass123", "neuesadminpass123")
	if err != nil {
		t.Fatalf("CompleteAdminRecovery failed: %v", err)
	}
	if res.Message != MsgAdminRecoveryComplete {
		t.Errorf("message = %q, want %q", res.Message, MsgAdminRecoveryComplete)
	}
	if repo.users["admina@gear.local"].PasswordHash != "hashed:neuesadminpass123" {
		t.Errorf("stored hash = %q, want hashed:neuesadminpass123", repo.users["admina@gear.local"].PasswordHash)
	}
	if len(repo.adminRecovery) != 0 {
		t.Errorf("used token must be invalidated, got %d", len(repo.adminRecovery))
	}
	if len(store.sessions) != 0 {
		t.Errorf("all sessions must be revoked, got %d", len(store.sessions))
	}
	// Audit: request (A) + approve (B) + complete (A).
	gotA := repo.audit["u-admina"]
	if len(gotA) != 2 || gotA[1] != AuditOperationAdminRecoveryComplete {
		t.Errorf("audit(A) = %v, want [request, complete]", gotA)
	}
	if gotB := repo.audit["u-adminb"]; len(gotB) != 1 || gotB[0] != AuditOperationAdminRecoveryApprove {
		t.Errorf("audit(B) = %v, want [approve]", gotB)
	}
}

func TestCompleteAdminRecoveryRejectsUnapprovedToken(t *testing.T) {
	// A pending (not-yet-approved) recovery token is NOT usable: the request
	// creates a recovery-marked token that the target never sees raw, and the
	// completion path rejects any token that is not both recovery-marked AND
	// approved.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	adminA := repo.users["admina@gear.local"]
	if _, err := svc.RequestAdminRecovery(context.Background(), adminA, "admina@gear.local"); err != nil {
		t.Fatalf("request failed: %v", err)
	}

	// The mock's ConsumeAdminRecoveryToken rejects a not-yet-approved token
	// regardless of the supplied raw token.
	_, err := svc.CompleteAdminRecovery(context.Background(), "any-raw-token", "neuesadminpass123", "neuesadminpass123")
	if !errors.Is(err, ErrAdminRecoveryInvalid) {
		t.Fatalf("error = %v, want ErrAdminRecoveryInvalid for an unapproved token", err)
	}
}

func TestCompleteAdminRecoverySingleUseAtomic(t *testing.T) {
	// The approved token is single-use: the second completion with the same raw
	// token is rejected and does not overwrite the first completion's password.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	adminA := repo.users["admina@gear.local"]
	adminB := repo.users["adminb@gear.local"]

	if _, err := svc.RequestAdminRecovery(context.Background(), adminA, "admina@gear.local"); err != nil {
		t.Fatalf("request failed: %v", err)
	}
	approved, err := svc.ApproveAdminRecovery(context.Background(), adminB, "admina@gear.local", "Ausgesperrt", true, "")
	if err != nil {
		t.Fatalf("approve failed: %v", err)
	}

	if _, err := svc.CompleteAdminRecovery(context.Background(), approved.RecoveryToken, "neuesadminpass123", "neuesadminpass123"); err != nil {
		t.Fatalf("first completion failed: %v", err)
	}
	if _, err := svc.CompleteAdminRecovery(context.Background(), approved.RecoveryToken, "anderesadminpass456", "anderesadminpass456"); !errors.Is(err, ErrAdminRecoveryInvalid) {
		t.Fatalf("second completion error = %v, want ErrAdminRecoveryInvalid (single-use)", err)
	}
	if repo.users["admina@gear.local"].PasswordHash != "hashed:neuesadminpass123" {
		t.Errorf("stored hash = %q, want the first completion's hash", repo.users["admina@gear.local"].PasswordHash)
	}
}

func TestCompleteAdminRecoveryShortPassword(t *testing.T) {
	// A new password under 10 characters is rejected (FR-2) and nothing changes.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	adminA := repo.users["admina@gear.local"]
	adminB := repo.users["adminb@gear.local"]
	if _, err := svc.RequestAdminRecovery(context.Background(), adminA, "admina@gear.local"); err != nil {
		t.Fatalf("request failed: %v", err)
	}
	approved, err := svc.ApproveAdminRecovery(context.Background(), adminB, "admina@gear.local", "Ausgesperrt", true, "")
	if err != nil {
		t.Fatalf("approve failed: %v", err)
	}

	_, err = svc.CompleteAdminRecovery(context.Background(), approved.RecoveryToken, "kurz", "kurz")
	if !errors.Is(err, ErrShortPassword) {
		t.Fatalf("short password error = %v, want ErrShortPassword", err)
	}
	if repo.users["admina@gear.local"].PasswordHash != "hashed:admina123456" {
		t.Error("no password change may happen on policy violation")
	}
}

func TestCompleteAdminRecoveryExpired(t *testing.T) {
	// An expired approved token is rejected with ErrAdminRecoveryInvalid.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	// Seed an expired APPROVED recovery token directly.
	repo.adminRecovery["hash-expired-approved"] = &AdminRecoveryToken{
		ID: "tok-expired", UserID: "u-admina", TokenHash: "hash-expired-approved",
		ApprovedByUserID: "u-adminb", ExpiresAt: time.Now().UTC().Add(-time.Minute),
		CreatedAt: time.Now().UTC().Add(-2 * time.Hour), User: repo.users["admina@gear.local"],
	}

	_, err := svc.CompleteAdminRecovery(context.Background(), "old-raw-token", "neuesadminpass123", "neuesadminpass123")
	if !errors.Is(err, ErrAdminRecoveryInvalid) {
		t.Fatalf("expired token error = %v, want ErrAdminRecoveryInvalid", err)
	}
}

func TestAdminForgotSelfResetBlocked(t *testing.T) {
	// ADMIN_FORGOT_SELF_RESET: an admin using the FR-26 forgot flow gets the
	// uniform confirmation but NO actionable self-reset token and, with no SMTP
	// configured, is NOT flagged must_change_password (FR-27 overrides FR-26 for
	// admins — admins recover only via the dual-admin path).
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)

	res, err := svc.RequestPasswordReset(context.Background(), "admina@gear.local")
	if err != nil {
		t.Fatalf("RequestPasswordReset failed: %v", err)
	}
	if res.Message != MsgPasswordResetRequested {
		t.Errorf("message = %q, want uniform %q", res.Message, MsgPasswordResetRequested)
	}
	if len(repo.resetTokens) != 0 {
		t.Errorf("an admin must not get a FR-26 self-reset token, got %d", len(repo.resetTokens))
	}
	if repo.mustChange["u-admina"] {
		t.Error("an admin must not be flagged must_change_password via FR-26")
	}
	if repo.users["admina@gear.local"].MustChangePassword {
		t.Error("the admin entity must not carry must_change_password=true")
	}
	// The FR-26 request is still audited for the admin account.
	if got := repo.audit["u-admina"]; len(got) != 1 || got[0] != AuditOperationPasswordResetRequest {
		t.Errorf("audit = %v, want [password.reset.request]", got)
	}
}

// mockEncryptSecret reproduces mockCipher.Encrypt so tests can plant a known
// plaintext TOTP secret in TotpSecretEncrypted and generate a matching code.
func mockEncryptSecret(plaintext string) string {
	out := make([]byte, len(plaintext))
	for i := 0; i < len(plaintext); i++ {
		out[i] = plaintext[i] + 1
	}
	return fmt.Sprintf("%x", out)
}

func TestApproveAdminRecoveryRequesterCannotApproveOwnRequest(t *testing.T) {
	// DUAL_CONTROL (review finding 1.10): a requester can never approve the
	// request they created. A requests recovery for B (requester=A, target=B),
	// then A tries to approve → 403 forbidden. B (a different admin, but the
	// target of A's request) is also blocked by the existing self-approval rule.
	// The real-world happy path — A requests their OWN recovery, B approves —
	// returns 200.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	adminA := repo.users["admina@gear.local"]
	adminB := repo.users["adminb@gear.local"]

	// A requests recovery ON BEHALF OF B (requester = A, target = B).
	if _, err := svc.RequestAdminRecovery(context.Background(), adminA, "adminb@gear.local"); err != nil {
		t.Fatalf("request failed: %v", err)
	}

	// A (the requester) tries to approve the request they created → forbidden.
	if _, err := svc.ApproveAdminRecovery(context.Background(), adminA, "adminb@gear.local", "selbst angefordert", true, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("requester self-approval error = %v, want ErrForbidden", err)
	}
	if len(repo.audit["u-admina"]) != 1 || repo.audit["u-admina"][0] != AuditOperationAdminRecoveryRequest {
		t.Errorf("audit(A) = %v, want only the request row (no approve)", repo.audit["u-admina"])
	}

	// B (the target of A's request) is also blocked: target == approver is the
	// existing self-approval guard.
	if _, err := svc.ApproveAdminRecovery(context.Background(), adminB, "adminb@gear.local", "eigene Freigabe", true, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("self-approval error = %v, want ErrForbidden", err)
	}

	// The clean dual-admin flow: A requests recovery for A (the locked-out admin
	// identifies themselves), then B — a DIFFERENT admin, not the requester and
	// not the target — approves → 200 with the raw token.
	if _, err := svc.RequestAdminRecovery(context.Background(), adminA, "admina@gear.local"); err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	res, err := svc.ApproveAdminRecovery(context.Background(), adminB, "admina@gear.local", "A ist ausgesperrt", true, "")
	if err != nil {
		t.Fatalf("B approving A's own-recovery request failed: %v", err)
	}
	if res.RecoveryToken == "" {
		t.Error("B must receive the raw token")
	}
}

func TestApproveAdminRecoverySetsFreshExpiry(t *testing.T) {
	// REFRESH_EXPIRY (review finding 1.10): approval resets the token expiry to
	// a FRESH 30 minutes, so a long-pending request does not expire
	// immediately on approval.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	adminA := repo.users["admina@gear.local"]
	adminB := repo.users["adminb@gear.local"]

	if _, err := svc.RequestAdminRecovery(context.Background(), adminA, "admina@gear.local"); err != nil {
		t.Fatalf("request failed: %v", err)
	}
	// Seed a request created ~29 minutes ago so it is near-expiry.
	for hash, t := range repo.adminRecovery {
		t.CreatedAt = time.Now().UTC().Add(-29 * time.Minute)
		t.ExpiresAt = time.Now().UTC().Add(1 * time.Minute)
		_ = hash
	}

	approved, err := svc.ApproveAdminRecovery(context.Background(), adminB, "admina@gear.local", "Ausgesperrt", true, "")
	if err != nil {
		t.Fatalf("approve failed: %v", err)
	}
	stored := repo.adminRecovery[hashToken(approved.RecoveryToken)]
	if stored == nil {
		t.Fatal("approved token must exist")
	}
	// The stored expiry must be a fresh ~30 minutes from now, not the old ~1 min.
	expected := time.Now().UTC().Add(29 * time.Minute)
	if stored.ExpiresAt.Before(expected) {
		t.Errorf("approved token expiry = %v, want fresh ~now+30min (>= %v)", stored.ExpiresAt, expected)
	}
}

func TestCompleteAdminRecoveryShortPasswordKeepsToken(t *testing.T) {
	// VALIDATE_BEFORE_CONSUME (review finding 1.10): a short password is
	// rejected (400 ErrShortPassword) WITHOUT burning the token, so the admin
	// can retry with a valid password.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	adminA := repo.users["admina@gear.local"]
	adminB := repo.users["adminb@gear.local"]
	if _, err := svc.RequestAdminRecovery(context.Background(), adminA, "admina@gear.local"); err != nil {
		t.Fatalf("request failed: %v", err)
	}
	approved, err := svc.ApproveAdminRecovery(context.Background(), adminB, "admina@gear.local", "Ausgesperrt", true, "")
	if err != nil {
		t.Fatalf("approve failed: %v", err)
	}
	token := approved.RecoveryToken

	if _, err := svc.CompleteAdminRecovery(context.Background(), token, "kurz", "kurz"); !errors.Is(err, ErrShortPassword) {
		t.Fatalf("short password error = %v, want ErrShortPassword", err)
	}
	// The token must still be present (usable) after the policy rejection.
	if len(repo.adminRecovery) != 1 {
		t.Fatalf("token must remain usable after a policy rejection, got %d pending", len(repo.adminRecovery))
	}
	// Now complete with a valid password → success.
	if _, err := svc.CompleteAdminRecovery(context.Background(), token, "neuesadminpass123", "neuesadminpass123"); err != nil {
		t.Fatalf("valid completion after rejection failed: %v", err)
	}
	if repo.users["admina@gear.local"].PasswordHash != "hashed:neuesadminpass123" {
		t.Errorf("stored hash = %q, want hashed:neuesadminpass123", repo.users["admina@gear.local"].PasswordHash)
	}
}

func TestApproveAdminRecoveryRequiresMFAWhenEnabled(t *testing.T) {
	// MFA_STEP_UP (review finding 1.10): an approving admin (B) with MFA enabled
	// must supply a valid TOTP code; a missing/invalid code → ErrRecoveryMFARequired.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	adminA := repo.users["admina@gear.local"]
	adminB := repo.users["adminb@gear.local"]
	if _, err := svc.RequestAdminRecovery(context.Background(), adminA, "admina@gear.local"); err != nil {
		t.Fatalf("request failed: %v", err)
	}

	const secret = "JBSWY3DPEHPK3PXP"
	adminB.IsMFAEnabled = true
	adminB.TotpSecretEncrypted = mockEncryptSecret(secret)

	// Missing code → ErrRecoveryMFARequired.
	if _, err := svc.ApproveAdminRecovery(context.Background(), adminB, "admina@gear.local", "Ausgesperrt", true, ""); !errors.Is(err, ErrRecoveryMFARequired) {
		t.Fatalf("missing MFA code error = %v, want ErrRecoveryMFARequired", err)
	}
	// Wrong code → ErrRecoveryMFARequired.
	if _, err := svc.ApproveAdminRecovery(context.Background(), adminB, "admina@gear.local", "Ausgesperrt", true, "000000"); !errors.Is(err, ErrRecoveryMFARequired) {
		t.Fatalf("wrong MFA code error = %v, want ErrRecoveryMFARequired", err)
	}
	// Valid code → approved.
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode failed: %v", err)
	}
	res, err := svc.ApproveAdminRecovery(context.Background(), adminB, "admina@gear.local", "Ausgesperrt", true, code)
	if err != nil {
		t.Fatalf("approve with valid MFA code failed: %v", err)
	}
	if res.RecoveryToken == "" {
		t.Error("must return a raw token after successful MFA step-up")
	}
}

func TestRecoveryAuditCarriesReasonAndHighSeverity(t *testing.T) {
	// AUDIT_DETAIL (review finding 1.10): the approve audit row carries the
	// Begründung (and target) in the operation detail and severity='high'.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	adminA := repo.users["admina@gear.local"]
	adminB := repo.users["adminb@gear.local"]
	if _, err := svc.RequestAdminRecovery(context.Background(), adminA, "admina@gear.local"); err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if _, err := svc.ApproveAdminRecovery(context.Background(), adminB, "admina@gear.local", "Ausgesperrt durch Angriff", true, ""); err != nil {
		t.Fatalf("approve failed: %v", err)
	}

	gotDetail := repo.auditDetail["u-adminb"]
	if len(gotDetail) != 1 || !strings.Contains(gotDetail[0], "Ausgesperrt durch Angriff") || !strings.Contains(gotDetail[0], "target=admina@gear.local") {
		t.Errorf("audit detail = %v, want reason+target in the approve detail", gotDetail)
	}
	gotSeverity := repo.auditSeverity["u-adminb"]
	if len(gotSeverity) != 1 || gotSeverity[0] != AuditSeverityHigh {
		t.Errorf("audit severity = %v, want high", gotSeverity)
	}
}

func TestDenyAdminRecovery(t *testing.T) {
	// DENY (review finding 1.10): admin B denies A's pending request with a
	// Begründung; the request is invalidated and audited as high-severity.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	adminA := repo.users["admina@gear.local"]
	adminB := repo.users["adminb@gear.local"]
	if _, err := svc.RequestAdminRecovery(context.Background(), adminA, "admina@gear.local"); err != nil {
		t.Fatalf("request failed: %v", err)
	}

	res, err := svc.DenyAdminRecovery(context.Background(), adminB, "admina@gear.local", "unberechtigte Anfrage")
	if err != nil {
		t.Fatalf("DenyAdminRecovery failed: %v", err)
	}
	if res.Message != MsgAdminRecoveryDenied {
		t.Errorf("message = %q, want %q", res.Message, MsgAdminRecoveryDenied)
	}
	if len(repo.adminRecovery) != 0 {
		t.Errorf("pending request must be invalidated on deny, got %d", len(repo.adminRecovery))
	}
	got := repo.audit["u-adminb"]
	if len(got) != 1 || got[0] != AuditOperationAdminRecoveryDeny {
		t.Errorf("audit(B) = %v, want [admin.recovery.deny]", got)
	}
	if sev := repo.auditSeverity["u-adminb"]; len(sev) != 1 || sev[0] != AuditSeverityHigh {
		t.Errorf("deny audit severity = %v, want high", sev)
	}
}

func TestDenyAdminRecoveryRequiresReason(t *testing.T) {
	// DENY_NO_REASON: a missing deny Begründung is rejected.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	adminA := repo.users["admina@gear.local"]
	adminB := repo.users["adminb@gear.local"]
	if _, err := svc.RequestAdminRecovery(context.Background(), adminA, "admina@gear.local"); err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if _, err := svc.DenyAdminRecovery(context.Background(), adminB, "admina@gear.local", "  "); !errors.Is(err, ErrRecoveryDenyReasonRequired) {
		t.Fatalf("missing deny reason error = %v, want ErrRecoveryDenyReasonRequired", err)
	}
}

func TestListAdminRecoveryRequest(t *testing.T) {
	// LIST (review finding 1.10): listing returns the pending requests for the
	// admin-B review surface and does not expose the password hash.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	adminA := repo.users["admina@gear.local"]
	adminB := repo.users["adminb@gear.local"]
	if _, err := svc.RequestAdminRecovery(context.Background(), adminA, "admina@gear.local"); err != nil {
		t.Fatalf("request failed: %v", err)
	}

	reqs, err := svc.ListAdminRecoveryRequest(context.Background(), adminB)
	if err != nil {
		t.Fatalf("ListAdminRecoveryRequest failed: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 pending request, got %d", len(reqs))
	}
	if reqs[0].UserID != "u-admina" {
		t.Errorf("request user = %q, want u-admina", reqs[0].UserID)
	}
	if reqs[0].User != nil && reqs[0].User.PasswordHash != "" {
		t.Errorf("listing must never expose the password hash, got %q", reqs[0].User.PasswordHash)
	}
}

func TestApproveAdminRecoveryTargetNotAdminOrActiveBlocked(t *testing.T) {
	// DEFENSE_IN_DEPTH (review finding 1.10): approve re-verifies the target is
	// still active; a deactivated target cannot be approved.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	adminA := repo.users["admina@gear.local"]
	adminB := repo.users["adminb@gear.local"]
	if _, err := svc.RequestAdminRecovery(context.Background(), adminA, "admina@gear.local"); err != nil {
		t.Fatalf("request failed: %v", err)
	}
	// Deactivate the target after the request.
	adminA.State = StateDeactivated
	if _, err := svc.ApproveAdminRecovery(context.Background(), adminB, "admina@gear.local", "Ausgesperrt", true, ""); !errors.Is(err, ErrAdminRecoveryInvalid) {
		t.Fatalf("approving a deactivated target error = %v, want ErrAdminRecoveryInvalid", err)
	}
}

func TestApproveAdminRecoveryDeactivatedApproverBlocked(t *testing.T) {
	// DEFENSE_IN_DEPTH (review finding 1.10): an approver whose account was
	// deactivated after their session was issued cannot approve.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	adminA := repo.users["admina@gear.local"]
	adminB := repo.users["adminb@gear.local"]
	if _, err := svc.RequestAdminRecovery(context.Background(), adminA, "admina@gear.local"); err != nil {
		t.Fatalf("request failed: %v", err)
	}
	adminB.State = StateDeactivated
	if _, err := svc.ApproveAdminRecovery(context.Background(), adminB, "admina@gear.local", "Ausgesperrt", true, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("deactivated approver error = %v, want ErrForbidden", err)
	}
}

func TestApproveAdminRecoveryApproverMissingPermissionBlocked(t *testing.T) {
	// DEFENSE_IN_DEPTH (review finding 1.10): an approver that no longer holds
	// `admin.recovery.approve` (permission revoked after session issuance)
	// cannot approve.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	adminA := repo.users["admina@gear.local"]
	adminB := repo.users["adminb@gear.local"]
	if _, err := svc.RequestAdminRecovery(context.Background(), adminA, "admina@gear.local"); err != nil {
		t.Fatalf("request failed: %v", err)
	}
	repo.perms["u-adminb"] = nil
	if _, err := svc.ApproveAdminRecovery(context.Background(), adminB, "admina@gear.local", "Ausgesperrt", true, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("approver without permission error = %v, want ErrForbidden", err)
	}
}

func TestApproveAdminRecoveryPropagatesStoreError(t *testing.T) {
	// ERROR_PROPAGATION (review finding 1.10): a GENUINE store failure on
	// approval must propagate, not be swallowed into ErrAdminRecoveryInvalid.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	adminA := repo.users["admina@gear.local"]
	adminB := repo.users["adminb@gear.local"]
	if _, err := svc.RequestAdminRecovery(context.Background(), adminA, "admina@gear.local"); err != nil {
		t.Fatalf("request failed: %v", err)
	}
	repo.adminRecoveryErr = errors.New("db connection lost")
	_, err := svc.ApproveAdminRecovery(context.Background(), adminB, "admina@gear.local", "Ausgesperrt", true, "")
	if err == nil || errors.Is(err, ErrAdminRecoveryInvalid) {
		t.Fatalf("approve error = %v, want the genuine store error to propagate", err)
	}
	if !strings.Contains(err.Error(), "db connection lost") {
		t.Errorf("approve error = %v, want the db connection lost cause", err)
	}
}

func TestCompleteAdminRecoveryPropagatesStoreError(t *testing.T) {
	// ERROR_PROPAGATION (review finding 1.10): a GENUINE store failure on token
	// consumption must propagate, not be swallowed into ErrAdminRecoveryInvalid.
	repo := recoveryRepo()
	svc, _ := recoveryService(t, repo)
	adminA := repo.users["admina@gear.local"]
	adminB := repo.users["adminb@gear.local"]
	if _, err := svc.RequestAdminRecovery(context.Background(), adminA, "admina@gear.local"); err != nil {
		t.Fatalf("request failed: %v", err)
	}
	approved, err := svc.ApproveAdminRecovery(context.Background(), adminB, "admina@gear.local", "Ausgesperrt", true, "")
	if err != nil {
		t.Fatalf("approve failed: %v", err)
	}
	repo.adminRecoveryErr = errors.New("db connection lost")
	_, err = svc.CompleteAdminRecovery(context.Background(), approved.RecoveryToken, "neuesadminpass123", "neuesadminpass123")
	if err == nil || errors.Is(err, ErrAdminRecoveryInvalid) {
		t.Fatalf("consume error = %v, want the genuine store error to propagate", err)
	}
	if !strings.Contains(err.Error(), "db connection lost") {
		t.Errorf("consume error = %v, want the db connection lost cause", err)
	}
}
