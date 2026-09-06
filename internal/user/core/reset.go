package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Self-service password reset (FR-26): a "Passwort vergessen" request accepts
// an email and ALWAYS returns the uniform anti-enumeration confirmation
// (UX-DR7). For an active account with a configured email sender, a single-use,
// hashed-at-rest, 30-minute reset token is minted and the sender port is asked
// to deliver a clickable /reset-password/<rawToken> link. When no sender is
// configured (this story's default), the account is flagged must_change_password
// and its live sessions are revoked so the change is enforced immediately (the
// Epic 2 one-time-password fallback). Deactivated/pending accounts receive the
// uniform confirmation but never an actionable token; unknown emails perform
// comparable-cost work (an anonymous audit row) so response timing does not
// leak account existence. Completing a reset replaces the password (Argon2id,
// AD-13), atomically consumes the token, clears the flag and revokes sessions
// (NFR-S2).
//
// Raw-token-in-URL trade-off (review finding 1.8-13): the reset link carries the
// opaque raw token in the URL, so it lives in browser history and could leak via
// the Referer header to offsite links. Mitigations: the SPA sets
// "no-referrer" (web/index.html), the token is single-use and hashed at rest,
// expires after 30 minutes, and completion/use invalidates it — so an exposed
// link is only usable once within a bounded window, and re-issuing a reset
// invalidates earlier links.

// ResetTokenTTL bounds how long a reset token stays usable (FR-26/AD-13).
const ResetTokenTTL = 30 * time.Minute

// ForgotPasswordMinInterval bounds how often a forgot-password request for the
// same normalized email is honored (review finding 1.8-2): a request within the
// window is answered with a uniform 429. The gate is keyed by the email string
// REGARDLESS of whether an account exists, so a 429 is not discriminating
// (anti-enumeration, mirroring the login lockout).
const ForgotPasswordMinInterval = 60 * time.Second

// AuditOperationPasswordResetRequest is the operation tag recorded when a user
// requests a password reset (NFR-O1/NFR-O2).
const AuditOperationPasswordResetRequest = "password.reset.request"

// AuditOperationPasswordResetRequestUnknown is the operation tag recorded when a
// forgot-password request targets an email with no matching account (review
// findings 1.8-3 / 1.8-10). The row is written without an actor so enumeration
// attempts leave a trail (NFR-O1) and the unknown path performs comparable-cost
// work.
const AuditOperationPasswordResetRequestUnknown = "password.reset.request.unknown"

// AuditOperationPasswordResetComplete is the operation tag recorded when a
// password reset is completed via a valid link (NFR-O1/NFR-O2).
const AuditOperationPasswordResetComplete = "password.reset.complete"

// ErrResetTokenInvalid is returned when a reset token is missing, unknown,
// expired or already used. Handlers map it to a 400 invalid_token with German
// microcopy asking for a new link (RESET_EXPIRED/RESET_USED).
var ErrResetTokenInvalid = errors.New("invalid or expired password reset token")

// ErrForgotThrottled is returned when a forgot-password request for an email
// was submitted too recently (review finding 1.8-2). The handler maps it to a
// uniform 429; it never depends on account existence (anti-enumeration).
var ErrForgotThrottled = errors.New("password reset requested too frequently")

// MsgPasswordResetRequested is the uniform anti-enumeration confirmation
// returned for EVERY forgot-password request — whether the account exists, its
// state, and whether SMTP is configured (FR-26/UX-DR7). It must never vary, so
// account existence cannot be probed.
//
// This is the canonical FORGOT-password confirmation and is frozen by spec-1-8.
// The registration flow has its OWN frozen anti-enumeration string
// (UniformSuccessMessage, "…erhältst du eine Bestätigung.", spec-1-3); the two
// are intentionally distinct contracts for distinct flows and must NOT be
// merged (review finding 1.8-9 documents this; both live behind their own
// constant so no flow can accidentally pick up the other's text).
const MsgPasswordResetRequested = "Wenn deine E-Mail registriert ist, erhältst du einen Link."

// MsgPasswordResetComplete is the German confirmation returned after a
// successful reset (FR-26).
const MsgPasswordResetComplete = "Passwort geändert. Du kannst dich jetzt mit dem neuen Passwort anmelden."

// MsgResetTokenInvalid is the German microcopy for an expired/used/unknown
// reset token (RESET_EXPIRED/RESET_USED).
const MsgResetTokenInvalid = "Dieser Link ist ungültig oder abgelaufen. Bitte fordere einen neuen Link an."

// PasswordResetToken is the domain representation of a single-use reset token
// (FR-26). Only its SHA-256 hash is persisted; the raw token is returned
// exactly once (in the emailed link) and never stored or logged.
type PasswordResetToken struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
	// User is the owner snapshot resolved by the store (JOIN on users) so the
	// completion step can verify the account is still active.
	User *User
}

// ResetEmailSender is the outbound email-delivery port for reset links
// (FR-26/AD-14): the User module never owns SMTP. A concrete sender is wired at
// the composition root; Story 3.1 supplies the real SMTP implementation. When
// no configured sender is present, RequestPasswordReset falls back to the
// must_change_password flag (this story's active default).
type ResetEmailSender interface {
	// SendPasswordResetEmail delivers a single transactional email carrying the
	// full clickable reset link (built from GEAR_APP_ORIGIN, review finding
	// 1.8-6) so a real sender never needs to know the link format. A returned
	// error is logged, never surfaced to the caller (the uniform confirmation
	// is returned regardless, NFR-O1).
	SendPasswordResetEmail(ctx context.Context, email, resetLink string) error
	// Configured reports whether a real delivery path is available. A stub that
	// reports NOT configured keeps the must-change-password fallback active.
	Configured() bool
}

// forgotThrottle is a small in-memory per-email gate (single-process server)
// for forgot-password requests (review finding 1.8-2): a request for the same
// normalized email is honored at most once per minGap. Keyed by the email
// string regardless of account existence, so a 429 is never discriminating.
type forgotThrottle struct {
	mu     sync.Mutex
	minGap time.Duration
	last   map[string]time.Time
}

func newForgotThrottle(minGap time.Duration) *forgotThrottle {
	return &forgotThrottle{minGap: minGap, last: make(map[string]time.Time)}
}

// allow reports whether a request for email may proceed at now, recording the
// honored time. A nil receiver or a minGap <= 0 disables throttling.
func (t *forgotThrottle) allow(email string, now time.Time) bool {
	if t == nil || t.minGap <= 0 {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if last, ok := t.last[email]; ok && now.Sub(last) < t.minGap {
		return false
	}
	t.last[email] = now
	return true
}

// ResetRequestResult is the uniform confirmation returned by the forgot
// endpoint (FR-26). It is identical for every request (UX-DR7).
type ResetRequestResult struct {
	Message string `json:"message"`
}

// ResetCompleteResult is the confirmation returned when a reset is completed.
type ResetCompleteResult struct {
	Message string `json:"message"`
}

// throttleForgot enforces the forgot-request rate gate (review finding 1.8-2).
// It runs BEFORE the account lookup so unknown and known emails are gated
// identically.
func (s *Service) throttleForgot(email string) error {
	if s.forgotThrottle == nil {
		return nil
	}
	if !s.forgotThrottle.allow(email, time.Now().UTC()) {
		return ErrForgotThrottled
	}
	return nil
}

// resetLink builds the clickable /reset-password/<rawToken> link delivered by
// the sender port (review finding 1.8-6). The app origin comes from
// GEAR_APP_ORIGIN (composition root); when unset, a relative link is produced.
func (s *Service) resetLink(rawToken string) string {
	base := strings.TrimRight(s.appOrigin, "/")
	if base == "" {
		return "/reset-password/" + rawToken
	}
	return base + "/reset-password/" + rawToken
}

// RequestPasswordReset executes the forgot-password use-case (FR-26). It
// normalizes the email, enforces the per-email rate gate, looks up the account
// and ALWAYS returns the uniform anti-enumeration confirmation (UX-DR7):
//   - unknown email → uniform confirmation plus an anonymous audit row
//     (comparable-cost work; review findings 1.8-3 / 1.8-10);
//   - active account + configured sender → single-use hashed 30-min token
//     minted (invalidating earlier ones) and a clickable link requested via the
//     port; a send failure is logged and the uniform confirmation still returned;
//   - active account + no configured sender → must_change_password flagged and
//     live sessions revoked (review finding 1.8-11), no email;
//   - deactivated/pending account → uniform confirmation, no actionable token.
//
// Expired tokens of the user are lazily purged on every request (review finding
// 1.8-7). Every request for an existing account is written to the audit trail
// (NFR-O1/NFR-O2); a failed audit write never rolls back the request
// (availability).
func (s *Service) RequestPasswordReset(ctx context.Context, email string) (*ResetRequestResult, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))

	// Rate gate BEFORE the lookup (review finding 1.8-2): keyed by the email
	// string whether or not an account exists, so a 429 is not discriminating.
	if err := s.throttleForgot(normalized); err != nil {
		return nil, err
	}

	user, err := s.repo.GetUserByEmail(ctx, normalized)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to look up user for reset: %w", err)
	}

	if user != nil {
		// Lazy purge of expired tokens (review finding 1.8-7). Best-effort.
		if err := s.repo.DeleteExpiredPasswordResetTokens(ctx, user.ID); err != nil {
			s.log().Warn("expired password reset token purge failed", "error", err)
		}

		switch user.State {
		case StateActive:
			if s.resetSender != nil && s.resetSender.Configured() {
				raw, err := s.mintResetToken(ctx, user.ID)
				if err != nil {
					return nil, fmt.Errorf("user core: failed to issue reset token: %w", err)
				}
				if err := s.resetSender.SendPasswordResetEmail(ctx, user.Email, s.resetLink(raw)); err != nil {
					// Send failure is logged (NFR-O1); the user still receives the
					// uniform confirmation so no state is leaked.
					s.log().Warn("password reset email send failed", "email", user.Email, "error", err)
				}
			} else {
				// SMTP not configured (early deployment): flag the account so the
				// next login forces a mandatory password change. No email is sent.
				if err := s.repo.SetUserMustChangePassword(ctx, user.ID); err != nil {
					return nil, fmt.Errorf("user core: failed to flag forced password change: %w", err)
				}
				// Enforce the change immediately (review finding 1.8-11): revoke
				// live sessions so the account cannot keep using the app — the
				// next login re-authenticates and is forced to change. Best-effort.
				if err := s.RevokeAllSessions(ctx, user.ID); err != nil {
					s.log().Warn("password reset must-change session revocation failed", "error", err)
				}
				s.log().Info("password reset requested without email delivery; must_change_password flagged", "email", user.Email)
			}
		case StatePendingApproval, StateDeactivated:
			// No actionable reset: the account cannot regain access this way and
			// must not reveal its state (anti-enumeration).
		}

		// Audit the request for any existing account (NFR-O1/NFR-O2). Best-effort:
		// a failed audit write is logged, never rolled back.
		if err := s.repo.InsertAuditEvent(ctx, user.ID, AuditOperationPasswordResetRequest); err != nil {
			s.log().Warn("password reset request audit write failed", "error", err)
		}
	} else {
		// Unknown email: perform comparable-cost work so response timing does not
		// leak account existence (review findings 1.8-3 / 1.8-10): write an
		// anonymous audit row (no actor) so enumeration attempts leave a trail
		// (NFR-O1). Best-effort.
		if err := s.repo.InsertAuditEventAnonymous(ctx, AuditOperationPasswordResetRequestUnknown); err != nil {
			s.log().Warn("password reset request (unknown) audit write failed", "error", err)
		}
	}

	return &ResetRequestResult{Message: MsgPasswordResetRequested}, nil
}

// CompletePasswordReset executes the reset-complete use-case (FR-26): the raw
// token is hashed and CONSUMED atomically (single-use, review finding 1.8-5);
// a missing/unknown/expired token or a no-longer-active account is rejected
// with ErrResetTokenInvalid (RESET_EXPIRED/RESET_USED). On success the new
// password (≥10 chars, FR-2; ≤1024; matching confirmation) replaces the old
// Argon2id hash (AD-13), must_change_password is cleared, ALL sessions are
// revoked (re-auth required, NFR-S2) and the completion is audited
// (NFR-O1/NFR-O2).
//
// Availability (NFR-O1): flag clearing, session revocation and the audit write
// are best-effort — a failure is logged and does NOT roll back the successful
// password change. The token itself is already consumed atomically before any
// of that work, so a rejected policy violation does NOT resurrect the token.
func (s *Service) CompletePasswordReset(ctx context.Context, rawToken, newPassword, confirm string) (*ResetCompleteResult, error) {
	if rawToken == "" {
		return nil, ErrResetTokenInvalid
	}

	// Atomic single-use consumption (review finding 1.8-5): the store deletes
	// the token in the same statement that returns it, so two concurrent
	// completions with the same token cannot both succeed — the loser sees no
	// row and is rejected here. Because the token is consumed up front, a
	// subsequent policy violation (short/mismatched password) does not leave a
	// reusable token behind.
	token, err := s.repo.ConsumePasswordResetToken(ctx, hashToken(rawToken))
	if err != nil || token == nil {
		return nil, ErrResetTokenInvalid
	}
	if time.Now().UTC().After(token.ExpiresAt) {
		return nil, ErrResetTokenInvalid
	}
	// Defense-in-depth: an account deactivated after issuance must not complete
	// a reset.
	if token.User == nil || token.User.State != StateActive {
		return nil, ErrResetTokenInvalid
	}

	// Validate the new password BEFORE any further mutation (FR-2): reuses the
	// change-password policy (≥10, ≤1024, match) and its sentinels.
	input := ChangePasswordInput{NewPassword: newPassword, NewPasswordConfirm: confirm}
	if err := input.Validate(); err != nil {
		return nil, err
	}

	newHash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to hash reset password: %w", err)
	}
	if _, err := s.repo.UpdateUserPassword(ctx, token.UserID, newHash); err != nil {
		return nil, fmt.Errorf("user core: failed to persist reset password: %w", err)
	}

	// Clear the mandatory-change flag (single-use token already consumed above).
	if err := s.repo.ClearUserMustChangePassword(ctx, token.UserID); err != nil {
		s.log().Warn("password reset must-change clear failed", "error", err)
	}

	// Re-auth required (NFR-S2): revoke ALL sessions of the user.
	if err := s.RevokeAllSessions(ctx, token.UserID); err != nil {
		s.log().Warn("password reset session revocation failed", "error", err)
	}

	// Audit event (NFR-O1/NFR-O2): best-effort.
	if err := s.repo.InsertAuditEvent(ctx, token.UserID, AuditOperationPasswordResetComplete); err != nil {
		s.log().Warn("password reset complete audit write failed", "error", err)
	}

	return &ResetCompleteResult{Message: MsgPasswordResetComplete}, nil
}

// mintResetToken generates an opaque 32-byte reset token, stores ONLY its
// SHA-256 hash with a 30-minute expiry (invalidating earlier tokens of the
// user) and returns the raw token exactly once. It is used both by the forgot
// flow (email link) and by the forced-change path at login (FR-26).
func (s *Service) mintResetToken(ctx context.Context, userID string) (string, error) {
	raw, err := newOpaqueToken()
	if err != nil {
		return "", err
	}
	expiresAt := time.Now().UTC().Add(ResetTokenTTL)
	if err := s.repo.CreatePasswordResetToken(ctx, userID, hashToken(raw), expiresAt); err != nil {
		return "", err
	}
	return raw, nil
}
