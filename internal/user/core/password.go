package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"unicode/utf8"
)

// AuditOperationPasswordChange is the operation tag recorded in the User-owned
// audit trail when a user changes their own password (FR-25, NFR-O1/NFR-O2).
const AuditOperationPasswordChange = "password.change"

// ErrInvalidCurrentPassword is returned when the submitted current password
// does not match the account's stored hash (FR-25). The handler maps it to a
// 400 with code invalid_current_password and German microcopy.
var ErrInvalidCurrentPassword = errors.New("invalid current password")

// MsgInvalidCurrentPassword is the German microcopy for a wrong current
// password (UX-DR8).
const MsgInvalidCurrentPassword = "Das aktuelle Passwort ist falsch."

// MsgPasswordChanged is the German confirmation returned on a successful
// password change (FR-25/UX-DR8).
const MsgPasswordChanged = "Passwort geändert."

// MsgSessionsRevoked is the German success line for the SPA: all other
// sessions were revoked while the current one stays logged in (FR-25).
const MsgSessionsRevoked = "→ Andere Sitzungen beendet"

// MsgSessionsNotRevoked is the German fallback shown when the password was
// changed but other sessions could not be revoked (availability): the change
// succeeded, but the user should re-check their active sessions.
const MsgSessionsNotRevoked = "Das Passwort wurde geändert, aber andere Sitzungen konnten nicht beendet werden."

// ChangePasswordInput captures the self-service password change payload (FR-25).
type ChangePasswordInput struct {
	CurrentPassword    string `json:"current_password"`
	NewPassword        string `json:"new_password"`
	NewPasswordConfirm string `json:"new_password_confirm"`
}

// Validate enforces the new-password policy (≥10 characters, FR-2; bounded
// upper length) and the match between the new password and its confirmation.
// The current password is deliberately not validated here: an empty current
// password flows through to the verify step and maps to ErrInvalidCurrentPassword
// (it is not a distinct input violation and must not be treated as one).
func (in *ChangePasswordInput) Validate() error {
	if utf8.RuneCountInString(in.NewPassword) < 10 {
		return ErrShortPassword
	}
	if utf8.RuneCountInString(in.NewPassword) > 1024 {
		return ErrPasswordTooLong
	}
	if in.NewPassword != in.NewPasswordConfirm {
		return ErrPasswordMismatch
	}
	return nil
}

// ChangePasswordResult is the confirmation returned on a successful password
// change (FR-25). SessionsRevoked reports whether every OTHER session was
// actually revoked (best-effort, NFR-O1): the SPA only shows the "→ Andere
// Sitzungen beendet" confirmation when it is true.
type ChangePasswordResult struct {
	Message         string `json:"message"`
	SessionsRevoked bool   `json:"sessions_revoked"`
}

// ChangePassword executes the self-service "Passwort ändern" use-case (FR-25):
// it confirms the current password, validates the new one, stores the new
// password as an Argon2id hash (AD-13), revokes every OTHER session — the
// current session (identified by rawToken) stays logged in — and appends an
// audit event. Only an authenticated user can change their OWN password
// (ownership of self is the capability, AD-12); the handler guarantees a
// non-nil user.
//
// Availability (NFR-O1): the audit write and session revocation are
// best-effort. A failed audit insert or a failed revocation does NOT roll back
// the successful password change — the failure is logged server-side, success
// is still returned, and the result carries the actual revocation outcome.
//
// An empty rawToken (a caller not acting through a specific session) is
// rejected BEFORE any work: passing it to RevokeOtherSessions would fall back
// to revoking ALL sessions — including the current one — violating FR-25.
func (s *Service) ChangePassword(ctx context.Context, user *User, input ChangePasswordInput, rawToken string) (*ChangePasswordResult, error) {
	if user == nil {
		return nil, ErrInvalidCredentials
	}
	if rawToken == "" {
		return nil, ErrInvalidCredentials
	}

	// Confirm the current password against the stored hash before ANY policy
	// check or change is accepted (FR-25). A malformed/missing stored hash or
	// wrong password maps to ErrInvalidCurrentPassword. Ordering matters: the
	// credential is authenticated first so a wrong current password is never
	// reported as a new-password policy violation (which would leak policy
	// results to an unauthenticated credential).
	ok, err := s.hasher.Verify(input.CurrentPassword, user.PasswordHash)
	if err != nil || !ok {
		return nil, ErrInvalidCurrentPassword
	}

	if err := input.Validate(); err != nil {
		return nil, err
	}

	newHash, err := s.hasher.Hash(input.NewPassword)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to hash new password: %w", err)
	}
	if _, err := s.repo.UpdateUserPassword(ctx, user.ID, newHash); err != nil {
		return nil, fmt.Errorf("user core: failed to persist password change: %w", err)
	}

	// Revoke all other sessions instantly (FR-25); the current session stays
	// logged in. Best-effort: a failure is logged, the change is not rolled
	// back, and the result reports the actual outcome.
	sessionsRevoked := true
	if err := s.RevokeOtherSessions(ctx, user.ID, rawToken); err != nil {
		s.log().Warn("password change session revocation failed", "error", err)
		sessionsRevoked = false
	}

	// Audit event (NFR-O1/NFR-O2): best-effort — a failed audit write must not
	// roll back the password change (availability); the failure is logged.
	if err := s.repo.InsertAuditEvent(ctx, user.ID, AuditOperationPasswordChange, "", AuditSeverityNormal); err != nil {
		s.log().Warn("password change audit write failed", "error", err)
	}

	return &ChangePasswordResult{Message: MsgPasswordChanged, SessionsRevoked: sessionsRevoked}, nil
}

// log returns the Service's structured logger, falling back to slog.Default()
// when none was injected (e.g. tests constructing the Service directly).
func (s *Service) log() *slog.Logger {
	if s.logger != nil {
		return s.logger
	}
	return slog.Default()
}
