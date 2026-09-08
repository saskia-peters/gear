package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// otpActor returns an active admin holding users.manage (the issuance gate).
func otpActor() *User {
	return &User{ID: "u-admin", Email: "admin@gear.local", State: StateActive}
}

// otpTargetRepo seeds an active target plus a deactivated and a pending one so
// the I/O matrix rows can be exercised.
func otpTargetRepo() *mockRepo {
	repo := newMockRepo()
	repo.users["target@gear.local"] = &User{
		ID:           "u-target",
		Email:        "target@gear.local",
		FirstName:    "Ziel",
		LastName:     "Person",
		PasswordHash: "hashed:geheim123456",
		State:        StateActive,
	}
	repo.users["deactivated@gear.local"] = &User{
		ID:    "u-deactivated",
		Email: "deactivated@gear.local",
		State: StateDeactivated,
	}
	repo.users["pending@gear.local"] = &User{
		ID:    "u-pending",
		Email: "pending@gear.local",
		State: StatePendingApproval,
	}
	repo.perms["u-admin"] = []string{UserManagePermission}
	return repo
}

func TestIssueOneTimePasswordOKActive(t *testing.T) {
	// ISSUE_OK_ACTIVE (Spec 2.8 I/O matrix): an admin holding users.manage
	// issues an OTP for an ACTIVE user → returned once, must_change_password
	// set, audited, hash stored (never plaintext).
	repo := otpTargetRepo()
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	res, err := svc.IssueOneTimePassword(context.Background(), otpActor(), "u-target", true)
	if err != nil {
		t.Fatalf("IssueOneTimePassword failed: %v", err)
	}
	if res.UserID != "u-target" || res.Email != "target@gear.local" {
		t.Errorf("result identity = %q/%q, want u-target/target@gear.local", res.UserID, res.Email)
	}
	if res.Message != MsgOneTimePasswordIssued {
		t.Errorf("message = %q, want the server-authoritative German confirmation", res.Message)
	}
	if len(res.OneTimePassword) != OneTimePasswordLength {
		t.Fatalf("OTP length = %d, want %d", len(res.OneTimePassword), OneTimePasswordLength)
	}
	// Human-typeable, unambiguous alphabet: no 0/O/1/I, every char in range.
	if strings.ContainsAny(res.OneTimePassword, "01IO") {
		t.Errorf("OTP %q contains an ambiguous character", res.OneTimePassword)
	}
	for _, r := range res.OneTimePassword {
		if !strings.ContainsRune(oneTimePasswordAlphabet, r) {
			t.Errorf("OTP %q contains char outside the alphabet", res.OneTimePassword)
		}
	}
	if !res.ExpiresAt.After(time.Now().UTC()) {
		t.Errorf("expires_at = %v, want a future expiry", res.ExpiresAt)
	}

	target := repo.users["target@gear.local"]
	if target.OneTimePasswordHash != "hashed:"+res.OneTimePassword {
		t.Errorf("stored OTP hash = %q, want the Argon2id hash (never plaintext)", target.OneTimePasswordHash)
	}
	if !target.MustChangePassword {
		t.Error("target must_change_password not set after issuance")
	}
	if !containsString(repo.audit["u-admin"], AuditOperationUserOtpIssue) {
		t.Error("issuance not audited (user.otp.issue expected)")
	}
}

func TestIssueOneTimePasswordWriteVanished(t *testing.T) {
	// Finding: SetUserOneTimePassword reports zero rows (the target vanished
	// between the eligibility read and the write). The issuance must map it to
	// ErrAdminUserNotFound and NEVER return a plaintext OTP that cannot work.
	repo := otpTargetRepo()
	repo.setOtpForceLost = true
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	res, err := svc.IssueOneTimePassword(context.Background(), otpActor(), "u-target", true)
	if !errors.Is(err, ErrAdminUserNotFound) {
		t.Fatalf("err = %v, want ErrAdminUserNotFound on a zero-row OTP write", err)
	}
	if res != nil {
		t.Errorf("a vanished target must never yield a result, got %+v", res)
	}
}

func TestIssueOneTimePasswordReIssue(t *testing.T) {
	// RE_ISSUE (Spec 2.8 I/O matrix): a second issuance REPLACES the old OTP —
	// the old value is invalid immediately.
	repo := otpTargetRepo()
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)
	target := repo.users["target@gear.local"]
	target.OneTimePasswordHash = "hashed:OLDDOTP000"
	target.OneTimePasswordExpiresAt = time.Now().UTC().Add(time.Minute)

	first, err := svc.IssueOneTimePassword(context.Background(), otpActor(), "u-target", true)
	if err != nil {
		t.Fatalf("first issue failed: %v", err)
	}
	second, err := svc.IssueOneTimePassword(context.Background(), otpActor(), "u-target", true)
	if err != nil {
		t.Fatalf("second issue failed: %v", err)
	}
	if first.OneTimePassword == second.OneTimePassword {
		t.Error("re-issue produced the same OTP (crypto-rand must not repeat deterministically)")
	}
	if target.OneTimePasswordHash != "hashed:"+second.OneTimePassword {
		t.Errorf("stored hash = %q, want the latest issuance's hash", target.OneTimePasswordHash)
	}
}

func TestIssueOneTimePasswordTargetEligibility(t *testing.T) {
	// ISSUE_DEACTIVATED / ISSUE_PENDING (Spec 2.8): a non-active target maps to
	// the uniform 409 sentinel — OTPs are active-only.
	tests := []struct {
		name string
		id   string
	}{
		{"deactivated", "u-deactivated"},
		{"pending_approval", "u-pending"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := otpTargetRepo()
			hasher := &mockHasher{}
			svc, _ := newTestService(repo, hasher)

			_, err := svc.IssueOneTimePassword(context.Background(), otpActor(), tt.id, true)
			if !errors.Is(err, ErrOneTimePasswordTargetNotEligible) {
				t.Fatalf("err = %v, want ErrOneTimePasswordTargetNotEligible", err)
			}
		})
	}
}

func TestIssueOneTimePasswordUnknownTarget(t *testing.T) {
	// ISSUE_UNKNOWN (Spec 2.8): an unknown or malformed id maps to the uniform
	// 404 sentinel.
	repo := otpTargetRepo()
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	for _, id := range []string{"u-nope", "not-a-uuid"} {
		_, err := svc.IssueOneTimePassword(context.Background(), otpActor(), id, true)
		if !errors.Is(err, ErrAdminUserNotFound) {
			t.Errorf("id %q: err = %v, want ErrAdminUserNotFound", id, err)
		}
	}
}

func TestIssueOneTimePasswordNotConfirmed(t *testing.T) {
	// ISSUE_NOT_CONFIRMED (Spec 2.8): a missing/false confirmation maps to 400.
	repo := otpTargetRepo()
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	_, err := svc.IssueOneTimePassword(context.Background(), otpActor(), "u-target", false)
	if !errors.Is(err, ErrOneTimePasswordNotConfirmed) {
		t.Fatalf("err = %v, want ErrOneTimePasswordNotConfirmed", err)
	}
}

func TestIssueOneTimePasswordForbidden(t *testing.T) {
	// ISSUE_FORBIDDEN (Spec 2.8): a caller without users.manage → 403; a
	// non-active actor → 403.
	repo := otpTargetRepo()
	repo.perms["u-admin"] = []string{UserViewPermission} // view-only holder
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	_, err := svc.IssueOneTimePassword(context.Background(), otpActor(), "u-target", true)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("view-only issue err = %v, want ErrForbidden", err)
	}

	repo = otpTargetRepo()
	svc, _ = newTestService(repo, hasher)
	_, err = svc.IssueOneTimePassword(context.Background(), &User{ID: "u-admin", State: StateDeactivated}, "u-target", true)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-active actor issue err = %v, want ErrForbidden", err)
	}
}

// otpLoginUser decorates the active login-repo user with a valid OTP.
func otpLoginUser(t *testing.T) *mockRepo {
	t.Helper()
	repo := newLoginRepo()
	user := repo.users["active@example.com"]
	user.OneTimePasswordHash = "hashed:OTP-ABCDEFGHIJ"
	user.OneTimePasswordExpiresAt = time.Now().UTC().Add(10 * time.Minute)
	user.MustChangePassword = true
	return repo
}

func TestLoginOneTimePasswordOKActive(t *testing.T) {
	// LOGIN_OTP_OK_ACTIVE (Spec 2.8): an active user with a valid OTP logs in
	// with it as the password → must_change_password + reset token, NO app
	// session, and the OTP is consumed (single-use). Exactly one Argon2id verify.
	repo := otpLoginUser(t)
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	res, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "OTP-ABCDEFGHIJ"})
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if !res.MustChangePassword {
		t.Error("expected must_change_password=true from the OTP login")
	}
	if res.ResetToken == "" {
		t.Error("expected a single-use reset token for the forced-change flow")
	}
	if res.Token != "" {
		t.Error("an OTP login must NEVER issue an app session token")
	}
	user := repo.users["active@example.com"]
	if user.OneTimePasswordHash != "" {
		t.Error("OTP hash was not cleared after the successful login (single-use)")
	}
	if !user.OneTimePasswordExpiresAt.IsZero() {
		t.Error("OTP expiry was not cleared after the successful login")
	}
	if len(store.sessions) != 0 {
		t.Errorf("expected no session created, got %d", len(store.sessions))
	}
	if hasher.VerifyCalls() != 1 {
		t.Errorf("verify calls = %d, want exactly 1 (UX-DR7 timing)", hasher.VerifyCalls())
	}
	// The token is minted BEFORE the OTP is consumed (Design Notes): assert the
	// reset token actually landed so the user is never stranded.
	if len(repo.resetTokens) != 1 {
		t.Errorf("expected 1 minted reset token, got %d", len(repo.resetTokens))
	}
}

func TestLoginOneTimePasswordForcesChangeEvenIfFlagCleared(t *testing.T) {
	// Belt-and-suspenders: even if must_change_password was somehow cleared
	// after issuance, the OTP login must still land in the forced-change flow
	// and never issue an app session.
	repo := otpLoginUser(t)
	repo.users["active@example.com"].MustChangePassword = false
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	res, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "OTP-ABCDEFGHIJ"})
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if !res.MustChangePassword || res.ResetToken == "" || res.Token != "" {
		t.Errorf("OTP login must force the change flow, got %+v", res)
	}
	if len(store.sessions) != 0 {
		t.Errorf("expected no session, got %d", len(store.sessions))
	}
}

func TestLoginOneTimePasswordWrong(t *testing.T) {
	// LOGIN_OTP_WRONG (Spec 2.8): a wrong OTP answers the uniform 401 and the
	// progressive lockout counts it; the OTP stays valid. Exactly one verify.
	repo := otpLoginUser(t)
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	_, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "wrong-otp"})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
	if hasher.VerifyCalls() != 1 {
		t.Errorf("verify calls = %d, want exactly 1", hasher.VerifyCalls())
	}
	user := repo.users["active@example.com"]
	if user.OneTimePasswordHash == "" {
		t.Error("a wrong OTP must NOT consume the stored OTP")
	}
	if a := repo.attempts["active@example.com"]; a == nil || a.FailedCount != 1 {
		t.Errorf("failed login not counted, attempts = %+v", repo.attempts["active@example.com"])
	}
}

func TestLoginOneTimePasswordExpired(t *testing.T) {
	// LOGIN_OTP_EXPIRED (Spec 2.8): an expired OTP is not accepted; normal
	// password auth applies. Realistically the account is still flagged
	// must_change_password (issuance set it; expiry does not clear it), so the
	// correct-password login lands in the forced-change flow — no session.
	repo := newLoginRepo()
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)
	user := repo.users["active@example.com"]
	user.OneTimePasswordHash = "hashed:OTP-ABCDEFGHIJ"
	user.OneTimePasswordExpiresAt = time.Now().UTC().Add(-time.Minute)
	user.MustChangePassword = true

	// The OTP value itself no longer authenticates.
	_, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "OTP-ABCDEFGHIJ"})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expired OTP must not authenticate, err = %v", err)
	}
	if hasher.VerifyCalls() != 1 {
		t.Errorf("verify calls = %d, want exactly 1", hasher.VerifyCalls())
	}
	if user.OneTimePasswordHash == "" {
		t.Error("an expired OTP is never consumed by a failed login")
	}

	// Normal password auth still applies — and because the account is still
	// flagged must_change_password, it runs the forced-change flow (no session).
	res, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if err != nil {
		t.Fatalf("correct-password login after expired OTP failed: %v", err)
	}
	if !res.MustChangePassword {
		t.Error("the post-expiry correct-password login must still force the password change")
	}
	if res.ResetToken == "" {
		t.Error("expected a reset token for the forced-change flow")
	}
	if res.Token != "" {
		t.Error("the flagged account must not receive an app session")
	}
	if len(store.sessions) != 0 {
		t.Errorf("expected no session, got %d", len(store.sessions))
	}
}

func TestLoginOneTimePasswordConsumedReuse(t *testing.T) {
	// LOGIN_OTP_CONSUMED (Spec 2.8): after one successful OTP login the OTP is
	// gone — reusing it answers the uniform 401.
	repo := otpLoginUser(t)
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	res, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "OTP-ABCDEFGHIJ"})
	if err != nil || !res.MustChangePassword {
		t.Fatalf("first OTP login failed: %v", err)
	}
	if _, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "OTP-ABCDEFGHIJ"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("reused OTP must be rejected, err = %v", err)
	}
}

func TestLoginOneTimePasswordConcurrentConsumeCASLost(t *testing.T) {
	// Concurrent-consume CAS (Spec 2.8 Design Notes): when a racing login has
	// already consumed the OTP (ClearUserOneTimePassword reports false), this
	// login must fail even though the OTP value verified — and the just-minted
	// reset token must be discarded so the losing login never strands an
	// orphaned, undelivered credential.
	repo := otpLoginUser(t)
	repo.consumeOtpForceLost = true
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	_, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "OTP-ABCDEFGHIJ"})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials when the OTP claim is lost", err)
	}
	if len(repo.resetTokens) != 0 {
		t.Errorf("lost claim left %d orphaned reset tokens behind, want 0", len(repo.resetTokens))
	}
}

func TestLoginOneTimePasswordWithMFA(t *testing.T) {
	// OTP + MFA combined (Spec 2.8 × FR-4): the OTP authenticates the password
	// slot, but a two-step MFA login still applies. First attempt (OTP, no TOTP
	// code) → MFA challenge with the OTP UNCONSUMED; second attempt (OTP + valid
	// TOTP code) → forced-change flow (no session) and the OTP is consumed once.
	repo := otpLoginUser(t)
	user := repo.users["active@example.com"]
	user.IsMFAEnabled = true
	enc, err := (mockCipher{}).Encrypt("JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatalf("encrypting TOTP secret failed: %v", err)
	}
	user.TotpSecretEncrypted = enc

	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	// Step 1: no TOTP code → MFA challenge, OTP still valid.
	res, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "OTP-ABCDEFGHIJ"})
	if err != nil {
		t.Fatalf("first (OTP-only) login failed: %v", err)
	}
	if !res.MFARequired {
		t.Fatalf("expected MFARequired challenge, got %+v", res)
	}
	if user.OneTimePasswordHash == "" {
		t.Fatal("the OTP must NOT be consumed by the MFA challenge step")
	}
	if len(store.sessions) != 0 {
		t.Errorf("expected no session after the MFA challenge, got %d", len(store.sessions))
	}

	// Step 2: OTP + valid TOTP code → forced change, OTP consumed exactly once.
	code := currentTotp(t, "JBSWY3DPEHPK3PXP")
	res2, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "OTP-ABCDEFGHIJ", TotpCode: code})
	if err != nil {
		t.Fatalf("second (OTP+MFA) login failed: %v", err)
	}
	if !res2.MustChangePassword {
		t.Error("expected must_change_password on the OTP+MFA login")
	}
	if res2.ResetToken == "" {
		t.Error("expected a single-use reset token for the forced-change flow")
	}
	if res2.Token != "" {
		t.Error("an OTP login must NEVER issue an app session token")
	}
	if user.OneTimePasswordHash != "" {
		t.Error("the OTP must be consumed after the successful OTP+MFA login")
	}
	if len(store.sessions) != 0 {
		t.Errorf("expected no session, got %d", len(store.sessions))
	}
	if hasher.VerifyCalls() != 2 {
		t.Errorf("verify calls = %d, want 2 (one per attempt, UX-DR7)", hasher.VerifyCalls())
	}
}

func TestLoginOneTimePasswordNeverReAdmitsDeactivated(t *testing.T) {
	// Active-only login gate (Spec 2.8): the OTP never re-admits a deactivated
	// account — the verify runs against the dummy hash and answers 401.
	repo := newLoginRepo()
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)
	user := repo.users["deactivated@example.com"]
	user.OneTimePasswordHash = "hashed:OTP-ABCDEFGHIJ"
	user.OneTimePasswordExpiresAt = time.Now().UTC().Add(10 * time.Minute)

	_, err := svc.Login(context.Background(), LoginInput{Email: "deactivated@example.com", Password: "OTP-ABCDEFGHIJ"})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials for a deactivated account with an OTP", err)
	}
	if hasher.VerifyCalls() != 1 {
		t.Errorf("verify calls = %d, want exactly 1 (dummy-hash timing)", hasher.VerifyCalls())
	}
	if user.OneTimePasswordHash == "" {
		t.Error("the OTP must not be consumed by a rejected deactivated login")
	}
}