package core

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// User approval workflow (Story 2.4, FR-20/AD-2/AD-6): self-registered users
// land in `pending_approval` (Story 1.3) and the admin surface lets a
// `users.approve` holder review them — list pending requests, approve, reject.
// Approve moves the user pending_approval → active and seeds the default
// `helfende` base role (AD-2) atomically; reject moves the user
// pending_approval → deactivated so the pending record disappears and the
// account can neither log in nor re-register (the email stays taken, FR-20).
// Every action is audited (NFR-O1/NFR-O2) with the admin as actor and the
// target email as detail — never password material.
//
// The `users.approve` permission is enforced by the gateway at the route mount
// (AD-6/FR-19); the core re-verifies it defense-in-depth, mirroring the
// admin-recovery approve path (review finding 1.10): a stale session must not
// bypass a role/permission revocation (AD-2 live re-resolution).

// UserApprovePermission is the permission code that gates the whole user
// approval surface (AD-12/AD-6/FR-20). It is carried by the seeded admin group.
const UserApprovePermission = "users.approve"

// DefaultUserRoleGroup is the permission group (base role) seeded on approval
// (AD-2, FR-5/FR-20): approved volunteers start as 'helfende'.
const DefaultUserRoleGroup = "helfende"

// User-approval audit operation codes (NFR-O1/NFR-O2). Distinct from the
// admin-recovery codes so the approval path is separately auditable.
const (
	AuditOperationUserApprove = "user.approve"
	AuditOperationUserReject  = "user.reject"
)

// ErrUserNotPending is returned when an approve/reject targets a user that is
// unknown OR no longer in `pending_approval` state (already active, already
// deactivated, or deleted). Handlers map it to the uniform 404 not_found —
// the same shape for both cases so nothing beyond what the admin already sees
// is leaked (FR-19).
var ErrUserNotPending = errors.New("user is not pending approval")

// German user-facing microcopy for the approval surface (UX-DR6/UX-DR8).
const (
	// MsgUserApproved is returned on a successful approval. The person can now
	// log in with their submitted credentials (FR-20/FR-5).
	MsgUserApproved = "Antrag freigegeben. Die Person kann sich jetzt mit ihren Zugangsdaten anmelden."
	// MsgUserRejected is returned on a successful rejection.
	MsgUserRejected = "Antrag abgelehnt. Die Person kann sich nicht anmelden."
	// MsgUserNotPending is the uniform not-found/conflict message for an
	// unknown or already-resolved application (no existence leak).
	MsgUserNotPending = "Der Antrag wurde nicht gefunden oder ist nicht mehr ausstehend."
)

// PendingUser is the domain representation of a pending-approval registration
// for the admin review surface (FR-20). It carries only the submitted profile
// details plus id and created_at — never the password hash or other secret
// material (NFR-O1). Vorname/Nachname wire to the German labels on the client.
type PendingUser struct {
	ID        string    `json:"id"`
	Vorname   string    `json:"vorname"`
	Nachname  string    `json:"nachname"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

// UserApprovalResult is the confirmation returned by approve/reject. Email
// echoes the affected person so the client can show specific feedback; it is a
// recoverable identity, never a secret.
type UserApprovalResult struct {
	Message string `json:"message"`
	UserID  string `json:"user_id"`
	Email   string `json:"email"`
}

// ListPending returns the pending-approval users, oldest first, for the admin
// review surface (FR-20). The caller is gated by `users.approve` upstream at
// the route mount; here it is re-verified for defense-in-depth (AD-2/AD-6).
func (s *Service) ListPending(ctx context.Context, actor *User) ([]*PendingUser, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireUserApprovePermission(ctx, actor); err != nil {
		return nil, err
	}
	return s.repo.ListPendingUsers(ctx)
}

// ApproveUser activates a pending-approval user and seeds the default
// `helfende` base role (FR-20/AD-2). The state transition and the role seed
// are atomic — the repository runs them in one transaction, so there is never
// a half-approved user. An unknown id or a user no longer pending maps to
// ErrUserNotPending (uniform 404, no existence leak). The approval is audited
// with the actor (admin) and the target email as detail (NFR-O1/NFR-O2) —
// best-effort: an audit write failure is logged, never rolled back into the
// transition (the transition must not be silently lost).
func (s *Service) ApproveUser(ctx context.Context, actor *User, userID string) (*UserApprovalResult, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	// Defense-in-depth (mirrors ApproveAdminRecovery, review finding 1.10): a
	// stale session must not approve after the actor was deactivated or lost
	// `users.approve`.
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireUserApprovePermission(ctx, actor); err != nil {
		return nil, err
	}

	user, err := s.repo.ApproveUser(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrUserNotPending) {
			return nil, ErrUserNotPending
		}
		return nil, fmt.Errorf("user core: failed to approve user: %w", err)
	}

	if err := s.repo.InsertAuditEvent(ctx, actor.ID, AuditOperationUserApprove, "target="+user.Email, AuditSeverityNormal); err != nil {
		s.log().Warn("user approve audit write failed", "error", err)
	}
	s.log().Info("user approved", "actor", actor.Email, "target", user.Email)

	return &UserApprovalResult{Message: MsgUserApproved, UserID: user.ID, Email: user.Email}, nil
}

// RejectUser moves a pending-approval user to `deactivated` so the pending
// record disappears and the account can neither log in nor re-register with
// that pending state (FR-20): login requires active state (canAuthenticate,
// AD-2) and the email stays taken, so re-registration answers the uniform
// anti-enumeration confirmation. An unknown id or a user no longer pending
// maps to ErrUserNotPending (uniform 404, no existence leak). The rejection is
// audited with the actor (admin) and the target email as detail
// (user.reject, NFR-O1/NFR-O2) — best-effort, never rolled back into the
// transition.
func (s *Service) RejectUser(ctx context.Context, actor *User, userID string) (*UserApprovalResult, error) {
	if actor == nil {
		return nil, ErrInvalidCredentials
	}
	if actor.State != StateActive {
		return nil, ErrForbidden
	}
	if err := s.requireUserApprovePermission(ctx, actor); err != nil {
		return nil, err
	}

	user, err := s.repo.RejectUser(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrUserNotPending) {
			return nil, ErrUserNotPending
		}
		return nil, fmt.Errorf("user core: failed to reject user: %w", err)
	}

	if err := s.repo.InsertAuditEvent(ctx, actor.ID, AuditOperationUserReject, "target="+user.Email, AuditSeverityNormal); err != nil {
		s.log().Warn("user reject audit write failed", "error", err)
	}
	s.log().Info("user rejected", "actor", actor.Email, "target", user.Email)

	return &UserApprovalResult{Message: MsgUserRejected, UserID: user.ID, Email: user.Email}, nil
}

// requireUserApprovePermission re-verifies (defense-in-depth) that the actor's
// LIVE permission set holds `users.approve` (AD-12/AD-6). The gateway already
// enforces it at the route mount; this guard closes the stale-session window.
func (s *Service) requireUserApprovePermission(ctx context.Context, actor *User) error {
	perms, err := s.repo.ListPermissionsByUser(ctx, actor.ID)
	if err != nil {
		return fmt.Errorf("user core: failed to resolve actor permissions: %w", err)
	}
	if !hasUserApprovePermission(perms) {
		return ErrForbidden
	}
	return nil
}

// hasUserApprovePermission reports whether perms includes `users.approve`.
func hasUserApprovePermission(perms []string) bool {
	for _, p := range perms {
		if p == UserApprovePermission {
			return true
		}
	}
	return false
}