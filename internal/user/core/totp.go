package core

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// TOTP issuer and label used in the otpauth provisioning URI. The label is
// "G.E.A.R.:{email}" per the spec; the issuer is "G.E.A.R.".
const (
	TOTPIssuer      = "G.E.A.R."
	TOTPIssuerLabel = "G.E.A.R.:"

	// PendingTotpTTL bounds how long a requested enrollment stays valid. The
	// client must confirm with a current code before the pending secret expires
	// (FR-4 / review finding 1.6-1).
	PendingTotpTTL = 10 * time.Minute
)

// totpCodePattern is the exact server-side TOTP format (RFC 6238, 6 digits),
// consistent with the client (review finding 1.6-7).
var totpCodePattern = regexp.MustCompile(`^\d{6}$`)

// ErrTOTPInvalid is returned when a submitted 6-digit TOTP code is wrong or
// expired. It maps to the uniform error envelope and never reveals why (UX-DR7).
var ErrTOTPInvalid = errors.New("invalid or expired TOTP code")

// ErrMFAEnrollmentExpired is returned when the pending enrollment (issued at
// enroll-request) has expired before the confirm step. Maps to a 400.
var ErrMFAEnrollmentExpired = errors.New("TOTP enrollment expired")

// ErrMFANotEnabled is returned when an MFA operation requires an enabled user
// but the account has MFA disabled. Maps to a 400.
var ErrMFANotEnabled = errors.New("MFA is not enabled")

// ErrMFAAlreadyEnabled is returned when enrolling an account that already has
// MFA enabled. Maps to a 400.
var ErrMFAAlreadyEnabled = errors.New("MFA is already enabled")

// ErrMFAUnavailable is returned when an MFA operation cannot run because the
// at-rest encryption key is missing, invalid or rotated (NFR-S4). It maps to a
// clear client message ("MFA ist derzeit nicht verfügbar.") while the real
// cause is logged server-side — never leaked to the client (review finding
// 1.6-3).
var ErrMFAUnavailable = errors.New("MFA is currently unavailable")

// MFAEnrollResult is returned once at enroll-request: the fresh shared secret
// and the otpauth provisioning URI for the authenticator app. The secret is
// shown once for manual entry; the server retains a short-lived encrypted copy
// so the confirm step can validate the code against it (never a client-supplied
// secret).
type MFAEnrollResult struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
}

// generateTotpSecret creates a new random RFC 6238 TOTP secret (HMAC-SHA1,
// 6 digits, 30s period) and its otpauth provisioning URI.
func generateTotpSecret(email string) (*otp.Key, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      TOTPIssuer,
		AccountName: email,
		Period:      30,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return nil, fmt.Errorf("user core: failed to generate TOTP secret: %w", err)
	}
	return key, nil
}

// isValidTotpCodeFormat reports whether code is exactly 6 digits (review
// finding 1.6-7). It runs before any TOTP validation or crypto work.
func isValidTotpCodeFormat(code string) bool {
	return totpCodePattern.MatchString(code)
}

// validTotpCode reports whether code matches the secret at the current time
// (RFC 6238, 30s period, 6 digits, HMAC-SHA1).
func validTotpCode(secret, code string) bool {
	if !isValidTotpCodeFormat(code) {
		return false
	}
	return totp.Validate(code, secret)
}

// MFAStatus reports whether the authenticated user currently has MFA enabled
// (FR-4). It powers the SPA "MFA aktiv" indicator (UX-DR6) and the settings
// surface's enable/disable branch. The handler guarantees a non-nil user; a nil
// user is a programming error, surfaced defensively here (review finding 1.6-10).
func (s *Service) MFAStatus(ctx context.Context, user *User) (bool, error) {
	if user == nil {
		return false, fmt.Errorf("user core: MFAStatus requires an authenticated user")
	}
	return user.IsMFAEnabled, nil
}

// EnrollMFARequest starts the MFA enrollment flow (FR-4): it generates a fresh
// random shared secret, persists a SHORT-LIVED ENCRYPTED copy of it plus an
// expiry as the pending enrollment, and returns the secret plus its
// provisioning URI. The confirm step validates the code against this
// server-issued secret — a client can never enroll with an arbitrary,
// predictable or re-used secret (review finding 1.6-1).
func (s *Service) EnrollMFARequest(ctx context.Context, user *User) (*MFAEnrollResult, error) {
	if user == nil {
		return nil, ErrInvalidCredentials
	}
	if user.IsMFAEnabled {
		return nil, ErrMFAAlreadyEnabled
	}
	key, err := generateTotpSecret(user.Email)
	if err != nil {
		return nil, err
	}
	encrypted, err := s.encryptSecret(key.Secret())
	if err != nil {
		return nil, err
	}
	expiresAt := time.Now().UTC().Add(PendingTotpTTL)
	if err := s.repo.SetUserPendingTotpSecret(ctx, user.ID, encrypted, expiresAt); err != nil {
		return nil, fmt.Errorf("user core: failed to persist pending TOTP enrollment: %w", err)
	}
	return &MFAEnrollResult{
		Secret: key.Secret(),
		URI:    key.URL(),
	}, nil
}

// ConfirmMFAEnable completes enrollment: it validates the submitted 6-digit
// code against the SERVER-ISSUED pending secret (decrypted from the store) and
// rejects a client-supplied secret that does not match it, plus expired
// enrollments (FR-4 / review finding 1.6-1). On success the pending secret is
// promoted to the active encrypted secret and MFA is enabled. The pending state
// is cleared on success or failure.
func (s *Service) ConfirmMFAEnable(ctx context.Context, user *User, secret, code string) error {
	if user == nil {
		return ErrInvalidCredentials
	}
	if user.IsMFAEnabled {
		return ErrMFAAlreadyEnabled
	}
	clearPending := func() {
		_ = s.repo.ClearUserPendingTotpSecret(ctx, user.ID)
	}

	if user.PendingTotpSecretEncrypted == "" {
		// No server-issued enrollment exists — a client-supplied secret alone
		// can never enable MFA.
		return ErrTOTPInvalid
	}
	if time.Now().UTC().After(user.PendingTotpExpiresAt) {
		clearPending()
		return ErrMFAEnrollmentExpired
	}

	pending, err := s.decryptSecret(user.PendingTotpSecretEncrypted)
	if err != nil {
		return err
	}
	if secret != pending {
		// Reject a client-supplied secret that does not match the server-issued
		// one. The rejection is identical to a bad code so nothing is revealed.
		clearPending()
		return ErrTOTPInvalid
	}
	if !validTotpCode(pending, code) {
		clearPending()
		return ErrTOTPInvalid
	}

	// Promote the already-encrypted pending secret to the active secret and
	// enable MFA (SetUserTotpSecret also clears the pending columns).
	if err := s.repo.SetUserTotpSecret(ctx, user.ID, user.PendingTotpSecretEncrypted); err != nil {
		return fmt.Errorf("user core: failed to persist TOTP secret: %w", err)
	}
	return nil
}

// DisableMFA turns MFA off: it requires re-authentication with a valid current
// TOTP code before clearing the stored secret (FR-4). A wrong/expired code
// leaves MFA enabled and returns ErrTOTPInvalid.
func (s *Service) DisableMFA(ctx context.Context, user *User, code string) error {
	if user == nil {
		return ErrInvalidCredentials
	}
	if !user.IsMFAEnabled || user.TotpSecretEncrypted == "" {
		return ErrMFANotEnabled
	}
	secret, err := s.decryptSecret(user.TotpSecretEncrypted)
	if err != nil {
		return err
	}
	if !validTotpCode(secret, code) {
		return ErrTOTPInvalid
	}
	if err := s.repo.ClearUserTotpSecret(ctx, user.ID); err != nil {
		return fmt.Errorf("user core: failed to clear TOTP secret: %w", err)
	}
	return nil
}

// RevokeOtherSessions invalidates every session of the user except the one
// identified by rawToken (NFR-S2). Used after enabling MFA so sessions issued
// before enrollment cannot bypass the second factor (review finding 1.6-2).
func (s *Service) RevokeOtherSessions(ctx context.Context, userID, rawToken string) error {
	return s.sessions.RevokeOtherSessions(ctx, userID, rawToken)
}

// RevokeAllSessions invalidates every session of the user (NFR-S2). Used after
// disabling MFA so all pre-existing sessions must re-authenticate (review
// finding 1.6-2).
func (s *Service) RevokeAllSessions(ctx context.Context, userID string) error {
	return s.sessions.RevokeAllSessions(ctx, userID)
}

// encryptSecret seals the plaintext TOTP secret with the SecretCipher. A
// missing or misconfigured key is surfaced as ErrMFAUnavailable (a clear,
// distinct error) rather than a panic or a generic 500 (NFR-S4 / review
// finding 1.6-3).
func (s *Service) encryptSecret(plaintext string) (string, error) {
	if s.cipher == nil {
		return "", fmt.Errorf("%w: TOTP secret encryption is not configured", ErrMFAUnavailable)
	}
	enc, err := s.cipher.Encrypt(plaintext)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrMFAUnavailable, err)
	}
	return enc, nil
}

// decryptSecret reverses encryptSecret for a stored ciphertext. Decryption
// failures (rotated/missing key, tampered ciphertext) surface as
// ErrMFAUnavailable.
func (s *Service) decryptSecret(encoded string) (string, error) {
	if s.cipher == nil {
		return "", fmt.Errorf("%w: TOTP secret decryption is not configured", ErrMFAUnavailable)
	}
	dec, err := s.cipher.Decrypt(encoded)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrMFAUnavailable, err)
	}
	return dec, nil
}