package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

// mfaUser returns an active user without MFA enabled.
func mfaUser() *User {
	return &User{
		ID:          "u-mfa",
		Email:       "mfa@example.com",
		DisplayName: "MFA Person",
		FirstName:   "MFA",
		LastName:    "Person",
		State:       StateActive,
	}
}

// enableUserForMFA stores an encrypted secret and flips the flag on the user,
// mirroring what ConfirmMFAEnable persists through the repository.
func enableUserForMFA(t *testing.T, repo *mockRepo, user *User, secret string) {
	t.Helper()
	enc, err := (mockCipher{}).Encrypt(secret)
	if err != nil {
		t.Fatal(err)
	}
	user.TotpSecretEncrypted = enc
	user.IsMFAEnabled = true
	if repo != nil {
		repo.users[user.Email] = user
	}
}

// beginEnrollment runs EnrollMFARequest and returns the result, simulating the
// real two-step flow (the pending secret is persisted to the repo).
func beginEnrollment(t *testing.T, svc *Service, repo *mockRepo, user *User) *MFAEnrollResult {
	t.Helper()
	if repo != nil {
		repo.users[user.Email] = user
	}
	res, err := svc.EnrollMFARequest(context.Background(), user)
	if err != nil {
		t.Fatalf("EnrollMFARequest failed: %v", err)
	}
	// The returned secret must match the pending secret persisted for the user.
	if repo != nil {
		stored := repo.users[user.Email]
		dec, err := (mockCipher{}).Decrypt(stored.PendingTotpSecretEncrypted)
		if err != nil {
			t.Fatalf("pending secret decryption failed: %v", err)
		}
		if dec != res.Secret {
			t.Fatalf("pending secret = %q, want server-issued %q", dec, res.Secret)
		}
		if stored.PendingTotpExpiresAt.Before(time.Now()) {
			t.Fatal("pending enrollment must carry a future expiry")
		}
	}
	return res
}

func currentTotp(t *testing.T, secret string) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("failed to generate TOTP code: %v", err)
	}
	return code
}

func TestEnrollMFARequestReturnsSecretAndURI(t *testing.T) {
	repo := newMockRepo()
	user := mfaUser()
	svc, _ := newTestService(repo, &mockHasher{})

	res := beginEnrollment(t, svc, repo, user)

	if res.Secret == "" {
		t.Fatal("expected a non-empty shared secret")
	}
	wantPrefix := "otpauth://totp/G.E.A.R.:mfa@example.com"
	if !strings.HasPrefix(res.URI, wantPrefix) {
		t.Errorf("URI = %q, want prefix %q", res.URI, wantPrefix)
	}
	if !strings.Contains(res.URI, "issuer=G.E.A.R.") {
		t.Errorf("URI = %q, want issuer=G.E.A.R.", res.URI)
	}
	if !strings.Contains(res.URI, "secret="+res.Secret) {
		t.Errorf("URI must embed the returned secret")
	}
	// The request must NOT enable MFA, but must persist the pending enrollment.
	if user.IsMFAEnabled || user.TotpSecretEncrypted != "" {
		t.Error("enroll request must not enable MFA")
	}
	if user.PendingTotpSecretEncrypted == "" || user.PendingTotpExpiresAt.IsZero() {
		t.Error("enroll request must persist a pending enrollment (FR-4)")
	}
}

func TestEnrollMFARequestAlreadyEnabled(t *testing.T) {
	repo := newMockRepo()
	user := mfaUser()
	enableUserForMFA(t, repo, user, "JBSWY3DPEHPK3PXP")
	svc, _ := newTestService(repo, &mockHasher{})

	_, err := svc.EnrollMFARequest(context.Background(), user)
	if !errors.Is(err, ErrMFAAlreadyEnabled) {
		t.Fatalf("err = %v, want ErrMFAAlreadyEnabled", err)
	}
}

func TestConfirmMFAEnableValidCode(t *testing.T) {
	repo := newMockRepo()
	user := mfaUser()
	svc, _ := newTestService(repo, &mockHasher{})

	res := beginEnrollment(t, svc, repo, user)
	code := currentTotp(t, res.Secret)

	if err := svc.ConfirmMFAEnable(context.Background(), user, res.Secret, code); err != nil {
		t.Fatalf("ConfirmMFAEnable failed: %v", err)
	}
	stored := repo.users[user.Email]
	if !stored.IsMFAEnabled {
		t.Error("MFA flag must be enabled after valid confirm")
	}
	if stored.TotpSecretEncrypted == "" {
		t.Error("expected an encrypted secret to be persisted")
	}
	if strings.Contains(stored.TotpSecretEncrypted, res.Secret) {
		t.Error("plaintext secret must not be stored at rest (NFR-S4)")
	}
	// Decrypt round-trip must recover the exact secret.
	dec, err := (mockCipher{}).Decrypt(stored.TotpSecretEncrypted)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if dec != res.Secret {
		t.Errorf("decrypted secret = %q, want %q", dec, res.Secret)
	}
	// Pending enrollment must be cleared on success.
	if stored.PendingTotpSecretEncrypted != "" || !stored.PendingTotpExpiresAt.IsZero() {
		t.Error("pending enrollment must be cleared after a successful confirm")
	}
}

func TestConfirmMFAEnableInvalidCode(t *testing.T) {
	repo := newMockRepo()
	user := mfaUser()
	svc, _ := newTestService(repo, &mockHasher{})

	res := beginEnrollment(t, svc, repo, user)

	err := svc.ConfirmMFAEnable(context.Background(), user, res.Secret, "000000")
	if !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("err = %v, want ErrTOTPInvalid", err)
	}
	if user.IsMFAEnabled || user.TotpSecretEncrypted != "" {
		t.Error("invalid confirm must not change the account (FR-4)")
	}
	// Pending enrollment must be cleared after a failed confirm too.
	if user.PendingTotpSecretEncrypted != "" || !user.PendingTotpExpiresAt.IsZero() {
		t.Error("pending enrollment must be cleared after a failed confirm")
	}
}

func TestConfirmMFAEnableRejectsArbitrarySecret(t *testing.T) {
	// FR-4 / review finding 1.6-1: a client cannot enroll with its own secret.
	// Without a server-issued pending enrollment the confirm is rejected.
	repo := newMockRepo()
	user := mfaUser()
	repo.users[user.Email] = user
	svc, _ := newTestService(repo, &mockHasher{})

	key, err := generateTotpSecret(user.Email)
	if err != nil {
		t.Fatal(err)
	}
	code := currentTotp(t, key.Secret())

	err = svc.ConfirmMFAEnable(context.Background(), user, key.Secret(), code)
	if !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("err = %v, want ErrTOTPInvalid when no server-issued enrollment exists", err)
	}
	if user.IsMFAEnabled || user.TotpSecretEncrypted != "" {
		t.Error("MFA must not be enabled with a client-supplied secret")
	}
}

func TestConfirmMFAEnableRejectsMismatchedSecret(t *testing.T) {
	// FR-4 / review finding 1.6-1: even with a valid pending enrollment, a
	// client-supplied secret that differs from the server-issued one is
	// rejected.
	repo := newMockRepo()
	user := mfaUser()
	svc, _ := newTestService(repo, &mockHasher{})

	res := beginEnrollment(t, svc, repo, user)
	code := currentTotp(t, res.Secret)

	err := svc.ConfirmMFAEnable(context.Background(), user, "ARBITRARYSECRET", code)
	if !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("err = %v, want ErrTOTPInvalid for a mismatched secret", err)
	}
	if user.IsMFAEnabled {
		t.Error("MFA must not be enabled with a mismatched client secret")
	}
	// Pending cleared on failure.
	if user.PendingTotpSecretEncrypted != "" {
		t.Error("pending enrollment must be cleared after a mismatched-secret confirm")
	}
}

func TestConfirmMFAEnableExpiredEnrollment(t *testing.T) {
	// FR-4 / review finding 1.6-1: an expired pending enrollment is rejected.
	repo := newMockRepo()
	user := mfaUser()
	svc, _ := newTestService(repo, &mockHasher{})

	res := beginEnrollment(t, svc, repo, user)
	// Backdate the pending expiry so the enrollment is expired.
	repo.users[user.Email].PendingTotpExpiresAt = time.Now().UTC().Add(-time.Minute)
	code := currentTotp(t, res.Secret)

	err := svc.ConfirmMFAEnable(context.Background(), user, res.Secret, code)
	if !errors.Is(err, ErrMFAEnrollmentExpired) {
		t.Fatalf("err = %v, want ErrMFAEnrollmentExpired", err)
	}
	if user.IsMFAEnabled {
		t.Error("MFA must not be enabled from an expired enrollment")
	}
}

func TestConfirmMFAEnableAlreadyEnabled(t *testing.T) {
	repo := newMockRepo()
	user := mfaUser()
	enableUserForMFA(t, repo, user, "JBSWY3DPEHPK3PXP")
	svc, _ := newTestService(repo, &mockHasher{})

	err := svc.ConfirmMFAEnable(context.Background(), user, "JBSWY3DPEHPK3PXP", "123456")
	if !errors.Is(err, ErrMFAAlreadyEnabled) {
		t.Fatalf("err = %v, want ErrMFAAlreadyEnabled", err)
	}
}

func TestConfirmMFAEnableRejectsMalformedCode(t *testing.T) {
	// Review finding 1.6-7: a code that is not exactly 6 digits is rejected.
	repo := newMockRepo()
	user := mfaUser()
	svc, _ := newTestService(repo, &mockHasher{})

	res := beginEnrollment(t, svc, repo, user)

	err := svc.ConfirmMFAEnable(context.Background(), user, res.Secret, "12ab")
	if !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("err = %v, want ErrTOTPInvalid for a malformed code", err)
	}
	if user.IsMFAEnabled {
		t.Error("MFA must not be enabled with a malformed code")
	}
}

func TestLoginNoMFAIsSingleStep(t *testing.T) {
	repo := newLoginRepo()
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	res, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if res.MFARequired {
		t.Error("MFARequired must be false when MFA is disabled")
	}
	if res.Token == "" {
		t.Error("expected a session token in the single-step login")
	}
	if len(store.sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(store.sessions))
	}
}

func TestLoginMFAChallengeIssuesNoSession(t *testing.T) {
	repo := newLoginRepo()
	user := repo.users["active@example.com"]
	enableUserForMFA(t, repo, user, "JBSWY3DPEHPK3PXP")
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	res, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if !res.MFARequired {
		t.Error("MFARequired must be true for an MFA-enabled account without a code")
	}
	if res.Token != "" {
		t.Error("challenge step must not issue a session token")
	}
	if len(store.sessions) != 0 {
		t.Errorf("expected NO session on challenge, got %d", len(store.sessions))
	}
}

func TestLoginMFAValidCodeIssuesSession(t *testing.T) {
	repo := newLoginRepo()
	user := repo.users["active@example.com"]
	enableUserForMFA(t, repo, user, "JBSWY3DPEHPK3PXP")
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	code := currentTotp(t, "JBSWY3DPEHPK3PXP")
	res, err := svc.Login(context.Background(), LoginInput{
		Email:    "active@example.com",
		Password: "geheim123456",
		TotpCode: code,
	})
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if res.MFARequired {
		t.Error("MFARequired must be false after a valid code")
	}
	if res.Token == "" {
		t.Error("expected a session token after a valid TOTP code")
	}
	if len(store.sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(store.sessions))
	}
}

func TestLoginMFAInvalidCodeRejected(t *testing.T) {
	repo := newLoginRepo()
	user := repo.users["active@example.com"]
	enableUserForMFA(t, repo, user, "JBSWY3DPEHPK3PXP")
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	_, err := svc.Login(context.Background(), LoginInput{
		Email:    "active@example.com",
		Password: "geheim123456",
		TotpCode: "000000",
	})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials for a wrong TOTP code", err)
	}
	if len(store.sessions) != 0 {
		t.Errorf("expected NO session for a wrong TOTP code, got %d", len(store.sessions))
	}
}

func TestLoginMFAExpiredCodeRejected(t *testing.T) {
	repo := newLoginRepo()
	user := repo.users["active@example.com"]
	enableUserForMFA(t, repo, user, "JBSWY3DPEHPK3PXP")
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	// Generate a code from a different (expired) time window.
	old, err := totp.GenerateCode("JBSWY3DPEHPK3PXP", time.Now().Add(-2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Login(context.Background(), LoginInput{
		Email:    "active@example.com",
		Password: "geheim123456",
		TotpCode: old,
	})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials for an expired TOTP code", err)
	}
}

func TestLoginMFAEnabledWithoutStoredSecretFailsClosed(t *testing.T) {
	repo := newLoginRepo()
	user := repo.users["active@example.com"]
	user.IsMFAEnabled = true // flag set but no secret stored
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	_, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials (fail closed)", err)
	}
	if len(store.sessions) != 0 {
		t.Errorf("expected no session when MFA flag has no stored secret")
	}
}

func TestLoginRejectsMalformedTotpCodeAsInvalidInput(t *testing.T) {
	// Review finding 1.6-7: a non-6-digit totp_code is invalid input (400), even
	// for an account without MFA (uniform, no enumeration).
	repo := newLoginRepo()
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	_, err := svc.Login(context.Background(), LoginInput{
		Email:    "active@example.com",
		Password: "geheim123456",
		TotpCode: "12ab",
	})
	if !errors.Is(err, ErrInvalidLoginInput) {
		t.Fatalf("err = %v, want ErrInvalidLoginInput for a malformed totp_code", err)
	}
}

func TestDisableMFAValidCode(t *testing.T) {
	repo := newMockRepo()
	user := mfaUser()
	enableUserForMFA(t, repo, user, "JBSWY3DPEHPK3PXP")
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	code := currentTotp(t, "JBSWY3DPEHPK3PXP")
	if err := svc.DisableMFA(context.Background(), user, code); err != nil {
		t.Fatalf("DisableMFA failed: %v", err)
	}
	if user.IsMFAEnabled {
		t.Error("MFA flag must be cleared after valid disable")
	}
	if user.TotpSecretEncrypted != "" {
		t.Error("encrypted secret must be cleared after disable (FR-4)")
	}
}

func TestDisableMFAInvalidCode(t *testing.T) {
	repo := newMockRepo()
	user := mfaUser()
	enableUserForMFA(t, repo, user, "JBSWY3DPEHPK3PXP")
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	err := svc.DisableMFA(context.Background(), user, "000000")
	if !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("err = %v, want ErrTOTPInvalid", err)
	}
	if !user.IsMFAEnabled {
		t.Error("MFA must stay enabled after an invalid disable code")
	}
}

func TestDisableMFANotEnabled(t *testing.T) {
	repo := newMockRepo()
	user := mfaUser()
	repo.users[user.Email] = user
	svc, _ := newTestService(repo, &mockHasher{})

	err := svc.DisableMFA(context.Background(), user, "123456")
	if !errors.Is(err, ErrMFANotEnabled) {
		t.Fatalf("err = %v, want ErrMFANotEnabled", err)
	}
}

func TestMFAOperationsWithoutCipherReturnClearError(t *testing.T) {
	// A service with a nil cipher must fail with a clear error, not a panic,
	// when the encryption key is missing/misconfigured (NFR-S4).
	repo := newMockRepo()
	user := mfaUser()
	repo.users[user.Email] = user
	svc := &Service{repo: repo, hasher: &mockHasher{}}

	if _, err := svc.encryptSecret("JBSWY3DPEHPK3PXP"); err == nil {
		t.Fatal("expected encryptSecret to fail without a cipher")
	}
	if _, err := svc.decryptSecret("enc:x"); err == nil {
		t.Fatal("expected decryptSecret to fail without a cipher")
	}
}

func TestEnrollMFARequestWithoutCipherMapsToUnavailable(t *testing.T) {
	// Review finding 1.6-3: a missing encryption key surfaces as
	// ErrMFAUnavailable (distinct, non-generic), not a panic.
	repo := newMockRepo()
	user := mfaUser()
	repo.users[user.Email] = user
	svc := &Service{repo: repo, hasher: &mockHasher{}}

	_, err := svc.EnrollMFARequest(context.Background(), user)
	if !errors.Is(err, ErrMFAUnavailable) {
		t.Fatalf("err = %v, want ErrMFAUnavailable", err)
	}
	if user.IsMFAEnabled {
		t.Error("MFA must not be enabled when encryption is not configured")
	}
}

func TestMFAStatus(t *testing.T) {
	repo := newMockRepo()
	svc, _ := newTestService(repo, &mockHasher{})

	on, err := svc.MFAStatus(context.Background(), mfaUser())
	if err != nil {
		t.Fatalf("MFAStatus failed: %v", err)
	}
	if on {
		t.Error("MFAStatus must report disabled for a user without MFA")
	}

	mfa := mfaUser()
	mfa.IsMFAEnabled = true
	on, err = svc.MFAStatus(context.Background(), mfa)
	if err != nil {
		t.Fatalf("MFAStatus failed: %v", err)
	}
	if !on {
		t.Error("MFAStatus must report enabled for an MFA-enabled user")
	}

	if _, err := svc.MFAStatus(context.Background(), nil); err == nil {
		t.Error("MFAStatus with a nil user must return an error (defensive guard)")
	}
}

func TestLoginWrongTOTPDoesNotAffectLockout(t *testing.T) {
	// Review finding 1.6-11a: a wrong-TOTP rejection must leave the email's
	// FailedCount at 0 with no lockout window (TOTP failures must NOT
	// accumulate into the password lockout, FR-3).
	repo := newLoginRepo()
	user := repo.users["active@example.com"]
	enableUserForMFA(t, repo, user, "JBSWY3DPEHPK3PXP")
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	_, err := svc.Login(context.Background(), LoginInput{
		Email:    "active@example.com",
		Password: "geheim123456",
		TotpCode: "000000",
	})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
	att := repo.attempts["active@example.com"]
	if att != nil && (att.FailedCount != 0 || !att.LockoutUntil.IsZero()) {
		t.Errorf("TOTP failure must not accumulate into the password lockout, got %+v", att)
	}
	if att != nil && att.FailedCount != 0 {
		t.Errorf("failed count = %d, want 0 after a TOTP-only failure", att.FailedCount)
	}
}

func TestLoginMFAChallengeResetsPriorFailedCount(t *testing.T) {
	// Review finding 1.6-11b: a prior FailedCount is reset after a
	// successful-password MFA challenge (the challenge step clears the counter).
	repo := newLoginRepo()
	user := repo.users["active@example.com"]
	enableUserForMFA(t, repo, user, "JBSWY3DPEHPK3PXP")
	hasher := &mockHasher{}
	svc, _ := newTestService(repo, hasher)

	// Simulate prior password failures that crossed into a lockout window.
	repo.attempts["active@example.com"] = &LoginAttempts{
		Email:        "active@example.com",
		FailedCount:  3,
		LockoutUntil: time.Now().UTC().Add(30 * time.Second),
	}

	// A valid password must NOT be rejected by lockout (it only gates BEFORE the
	// verify). Bypass the lockout by waiting it out is unrealistic; instead the
	// challenge step must reset the counter even while the window is active.
	// The lockout gate runs first, so clear the window to focus on the counter.
	repo.attempts["active@example.com"] = &LoginAttempts{
		Email:       "active@example.com",
		FailedCount: 3,
	}

	res, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if !res.MFARequired {
		t.Fatal("expected an MFA challenge")
	}
	att := repo.attempts["active@example.com"]
	if att == nil || att.FailedCount != 0 {
		t.Errorf("FailedCount = %+v, want 0 after a successful-password MFA challenge", att)
	}
}

func TestRevokeOtherSessions(t *testing.T) {
	repo := newLoginRepo()
	user := repo.users["active@example.com"]
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	t1, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if err != nil {
		t.Fatal(err)
	}
	t2, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"})
	if err != nil {
		t.Fatal(err)
	}
	if len(store.sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(store.sessions))
	}

	// Revoking except t1 keeps only t1.
	if err := svc.RevokeOtherSessions(context.Background(), user.ID, t1.Token); err != nil {
		t.Fatalf("RevokeOtherSessions failed: %v", err)
	}
	if len(store.sessions) != 1 {
		t.Fatalf("expected 1 remaining session, got %d", len(store.sessions))
	}
	if _, ok := store.sessions[hashOfToken(t1.Token)]; !ok {
		t.Error("the excepted session must survive")
	}
	if _, ok := store.sessions[hashOfToken(t2.Token)]; ok {
		t.Error("the non-excepted session must be revoked")
	}
}

func TestRevokeAllSessions(t *testing.T) {
	repo := newLoginRepo()
	user := repo.users["active@example.com"]
	hasher := &mockHasher{}
	svc, store := newTestService(repo, hasher)

	for i := 0; i < 3; i++ {
		if _, err := svc.Login(context.Background(), LoginInput{Email: "active@example.com", Password: "geheim123456"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(store.sessions))
	}

	if err := svc.RevokeAllSessions(context.Background(), user.ID); err != nil {
		t.Fatalf("RevokeAllSessions failed: %v", err)
	}
	if len(store.sessions) != 0 {
		t.Errorf("expected all sessions revoked, got %d", len(store.sessions))
	}
}

func TestUserJSONNeverSerializesTotpSecrets(t *testing.T) {
	// Review finding 1.6-14: neither the active nor the pending encrypted TOTP
	// secret may ever be serialized to a client.
	user := mfaUser()
	user.TotpSecretEncrypted = "encrypted-active-secret"
	user.PendingTotpSecretEncrypted = "encrypted-pending-secret"
	user.PendingTotpExpiresAt = time.Now().UTC().Add(time.Minute)

	raw, err := json.Marshal(user)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(raw)
	if strings.Contains(wire, "encrypted-active-secret") || strings.Contains(wire, "encrypted-pending-secret") {
		t.Errorf("User JSON must never carry the (encrypted) TOTP secret, got %s", wire)
	}
}

func TestSessionUserJSONNeverSerializesTotpSecrets(t *testing.T) {
	// Review finding 1.6-14: the user snapshot attached to a session must never
	// serialize the TOTP secret either.
	user := mfaUser()
	user.TotpSecretEncrypted = "encrypted-active-secret"
	sess := &Session{User: user}

	raw, err := json.Marshal(sess)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "encrypted-active-secret") {
		t.Errorf("Session JSON must never carry the (encrypted) TOTP secret, got %s", raw)
	}
}

// hashOfToken mirrors SessionManager's hashing for assertions.
func hashOfToken(token string) string {
	return hashToken(token)
}