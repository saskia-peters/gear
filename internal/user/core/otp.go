package core

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"
)

// Admin one-time-password issuance (Spec 2.8, FR-26 Epic 2): an admin holding
// `users.manage` hands an ACTIVE, password-lost user a single-use one-time
// password so they can re-enter and run the forced password-change flow (Story
// 1.8). The OTP is crypto-random and human-typeable, stored as an Argon2id hash
// with a short TTL (never in plaintext, never emailed, never read back —
// NFR-S4) and consumed atomically on the one successful login. OTPs are
// ACTIVE-only: a deactivated or pending_approval account cannot hold one
// (deactivation means "sofort kein Login"; the OTP is a recovery credential,
// not a reactivation path).

// oneTimePasswordAlphabet is the unambiguous alphabet for generated OTPs — no
// 0/O/1/I — so the value is easy to type by hand.
const oneTimePasswordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// OneTimePasswordLength is the number of characters in a generated OTP
// (10 chars from a 32-symbol alphabet ≈ 50 bits of entropy).
const OneTimePasswordLength = 10

// OneTimePasswordTTL bounds how long an issued one-time password stays usable
// (Spec 2.8). After this window the OTP is no longer accepted at login and the
// normal password auth applies.
const OneTimePasswordTTL = 15 * time.Minute

// AuditOperationUserOtpIssue is the operation tag recorded when an admin
// issues a one-time password (NFR-O1/NFR-O2).
const AuditOperationUserOtpIssue = "user.otp.issue"

// ErrOneTimePasswordNotConfirmed is returned when the client did not confirm
// the OTP issuance (400 invalid_request).
var ErrOneTimePasswordNotConfirmed = errors.New("one-time password issuance requires confirmation")

// ErrOneTimePasswordTargetNotEligible is returned when the issuance targets a
// user that is not ACTIVE (deactivated or pending_approval) — OTPs are only for
// active accounts (409 conflict, uniform message, no existence leak FR-19).
var ErrOneTimePasswordTargetNotEligible = errors.New("one-time password targets a non-active user")

// MsgOneTimePasswordIssued is the server-authoritative German confirmation
// returned with the OTP. The SPA displays THIS message in the show-once panel
// rather than hardcoding a parallel warning (Design Notes).
const MsgOneTimePasswordIssued = "Einmal-Passwort erstellt. Nur einmal anzeigen — sicher außerhalb des Systems übermitteln (nicht per E-Mail)."

// MsgOneTimePasswordNotConfirmed is the uniform 400 message for a missing or
// unchecked confirmation.
const MsgOneTimePasswordNotConfirmed = "Bitte bestätige die Erstellung des Einmal-Passworts."

// MsgOneTimePasswordTargetNotEligible is the uniform 409 message for a
// non-active target (no existence leak beyond what the admin already sees,
// FR-19).
const MsgOneTimePasswordTargetNotEligible = "Einmal-Passwörter können nur für aktive Benutzer erstellt werden."

// OneTimePasswordResult is the issuance response. The plaintext OTP appears
// ONLY here — there is deliberately no readback/recovery endpoint, so the
// value can never be recovered later (NFR-S4).
type OneTimePasswordResult struct {
	Message         string    `json:"message"`
	UserID          string    `json:"user_id"`
	Email           string    `json:"email"`
	OneTimePassword string    `json:"one_time_password"`
	ExpiresAt       time.Time `json:"expires_at"`
}

// generateOneTimePassword returns a crypto-random, human-typeable one-time
// password of OneTimePasswordLength characters drawn from the unambiguous
// alphabet (no 0/O/1/I). It is generated via crypto/rand. The alphabet length
// (32) divides 256, so mapping each random byte modulo the alphabet size is
// uniformly distributed — no rejection sampling is required. The value is
// never logged.
func generateOneTimePassword() (string, error) {
	b := make([]byte, OneTimePasswordLength)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("user core: failed to generate one-time password: %w", err)
	}
	for i := range b {
		b[i] = oneTimePasswordAlphabet[int(b[i])%len(oneTimePasswordAlphabet)]
	}
	return string(b), nil
}

// IssueOneTimePassword executes the admin one-time-password issuance use-case
// (Spec 2.8): an admin holding `users.manage` generates a single-use OTP for an
// ACTIVE account, stores its Argon2id hash with a 15-minute expiry and flags
// must_change_password, so the user's next login authenticates the password
// slot with the OTP and falls into the Story 1.8 forced-change flow (no app
// session). The plaintext OTP is returned exactly once and never stored,
// emailed or recoverable (NFR-S4).
//
// Target eligibility: only ACTIVE accounts can hold an OTP — a deactivated or
// pending_approval target maps to ErrOneTimePasswordTargetNotEligible (409,
// uniform message; deactivation means "sofort kein Login" and the OTP is a
// recovery credential, not a reactivation path). An unknown/malformed id maps
// to ErrAdminUserNotFound (404). The action requires client confirmation
// (ErrOneTimePasswordNotConfirmed → 400) and is audited with the actor +
// target (NFR-O1). Gated by `users.manage` (defense-in-depth).
func (s *Service) IssueOneTimePassword(ctx context.Context, actor *User, userID string, confirmed bool) (*OneTimePasswordResult, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireUserManagePermission(ctx, actor); err != nil {
		return nil, err
	}
	if !confirmed {
		return nil, ErrOneTimePasswordNotConfirmed
	}

	target, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrAdminUserNotFound) {
			return nil, ErrAdminUserNotFound
		}
		return nil, fmt.Errorf("user core: failed to load one-time-password target: %w", err)
	}
	// Active-only (Spec 2.8): the OTP is a password-recovery credential for a
	// user who lost their password — never a reactivation path.
	if target.State != StateActive {
		return nil, ErrOneTimePasswordTargetNotEligible
	}

	otp, err := generateOneTimePassword()
	if err != nil {
		return nil, err
	}
	hash, err := s.hasher.Hash(otp)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to hash one-time password: %w", err)
	}
	expiresAt := time.Now().UTC().Add(OneTimePasswordTTL)
	// A zero-row write (the target vanished between the eligibility read and
	// this update) maps to the uniform 404 — a credential that cannot work is
	// never handed over.
	written, err := s.repo.SetUserOneTimePassword(ctx, target.ID, hash, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to persist one-time password: %w", err)
	}
	if !written {
		return nil, ErrAdminUserNotFound
	}

	if err := s.repo.InsertAuditEvent(ctx, actor.ID, AuditOperationUserOtpIssue, "target="+target.Email, AuditSeverityHigh); err != nil {
		s.log().Warn("user one-time-password issue audit write failed", "error", err)
	}
	s.log().Info("user one-time-password issued", "actor", actor.Email, "target", target.Email)

	return &OneTimePasswordResult{
		Message:         MsgOneTimePasswordIssued,
		UserID:          target.ID,
		Email:           target.Email,
		OneTimePassword: otp,
		ExpiresAt:       expiresAt,
	}, nil
}