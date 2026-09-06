package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Dual-admin credential recovery (FR-27/AD-13): a locked-out admin (A) requests
// recovery via the admin panel, which creates a recovery request that ONLY the
// OTHER admin (B) — gated by the `admin.recovery.approve` permission — can
// approve, with a mandatory Begründung (reason) and a confirmation checkbox. On
// approval, B receives a single-use reset token to hand to A out-of-band, and A
// uses that token to set a new password (≥10 chars, FR-2).
//
// Recovery rides the existing password_reset_tokens table (single-use, hashed
// at rest, 30-min expiry, invalidated on use — spine table 19). A
// `recovery_target_admin` marker distinguishes admin-recovery tokens from
// ordinary FR-26 forgot tokens; an admin-recovery token is consumable ONLY via
// CompleteAdminRecovery, and only once it has been approved by a DIFFERENT
// admin. Every recovery event is written to the immutable audit trail
// (NFR-O2) and structured logging (NFR-O1) with a distinct operation code.
//
// Dual-control (review finding 1.10): the request is stamped with the
// requesting admin (requested_by_user_id), and the approving admin (B) may
// never be the requesting admin — a requester cannot approve their own request.
// Defense-in-depth re-verifies at approve time that the approver is still an
// active admin holding `admin.recovery.approve`, that the target is still
// active, and that the last-admin guard still holds. An approving admin with
// MFA enabled must supply a valid TOTP code (MFA step-up).
//
// Raw-token handling (review finding 1.8-13 / FR-27): the raw token is
// generated at APPROVE time — never stored at rest (only its SHA-256 hash is
// persisted) and never returned to the requester (A). It is returned exactly
// once to the approving admin (B), who transmits it to A out-of-band. The
// request step mints a placeholder token so a pending-recovery row exists for B
// to review; approval overwrites it with the real deliverable token and resets
// the expiry to a FRESH 30 minutes.

// ResetTokenTTL bounds how long an admin-recovery token stays usable (FR-27,
// matching the FR-26 reset window).
const AdminRecoveryTokenTTL = 30 * time.Minute

// Audit severity values (NFR-O1/NFR-O2): recovery events are written with
// severity='high' so the compliance trail can be filtered on severity.
const (
	AuditSeverityNormal = "normal"
	AuditSeverityHigh   = "high"
)

// Admin-recovery audit operation codes (NFR-O1/NFR-O2). Distinct from the
// FR-26 forgot-password codes so the recovery path is separately auditable.
const (
	AuditOperationAdminRecoveryRequest          = "admin.recovery.request"
	AuditOperationAdminRecoveryApprove          = "admin.recovery.approve"
	AuditOperationAdminRecoveryDeny             = "admin.recovery.deny"
	AuditOperationAdminRecoveryComplete         = "admin.recovery.complete"
	AuditOperationAdminRecoveryLastAdminBlocked = "admin.recovery.last_admin_blocked"
)

// ErrLastAdminRecoveryBlocked is returned when the target is the LAST active
// admin: self-recovery via any self-service path is deliberately disabled
// (FR-27/AD-13) and the out-of-band manual bootstrap procedure applies. The
// handler maps it to a 400 last_admin_recovery_blocked.
var ErrLastAdminRecoveryBlocked = errors.New("recovery of the last active admin is disabled")

// ErrAdminRecoveryInvalid is returned when an admin-recovery token is missing,
// unknown, expired, not yet approved, or already used. The handler maps it to a
// 400 invalid_token.
var ErrAdminRecoveryInvalid = errors.New("invalid or expired admin recovery token")

// ErrRecoveryReasonRequired is returned when the approving admin submits no
// Begründung (reason). The handler maps it to a 400 invalid_request.
var ErrRecoveryReasonRequired = errors.New("a reason (Begründung) is required to approve recovery")

// ErrRecoveryNotConfirmed is returned when the approving admin does not check
// the confirmation checkbox. The handler maps it to a 400 invalid_request.
var ErrRecoveryNotConfirmed = errors.New("recovery approval requires explicit confirmation")

// ErrRecoveryMFARequired is returned when the approving admin has MFA enabled
// but did not supply a valid TOTP code (MFA step-up, review finding 1.10). The
// handler maps it to a 403 recovery_mfa_required so the client can prompt for
// a code.
var ErrRecoveryMFARequired = errors.New("an MFA code is required to approve recovery")

// ErrRecoveryDenyReasonRequired is returned when the denying admin submits no
// Begründung (reason) for a deny (review finding 1.10). The handler maps it to
// a 400 invalid_request.
var ErrRecoveryDenyReasonRequired = errors.New("a reason (Begründung) is required to deny recovery")

// MsgAdminRecoveryRequested is the German confirmation returned when an
// admin-recovery request is created (FR-27). The raw token is never returned to
// the requester; the other admin must approve the request first.
const MsgAdminRecoveryRequested = "Deine Wiederherstellungsanfrage wurde erstellt. Der andere Administrator muss sie freigeben."

// MsgAdminRecoveryApproved is the German confirmation returned to the approving
// admin, telling them to transmit the single-use token to the recovered admin
// out-of-band.
const MsgAdminRecoveryApproved = "Freigabe erteilt. Übermittle den Einmal-Token sicher an den Administrator."

// MsgAdminRecoveryComplete is the German confirmation returned after a
// successful admin-recovery completion (FR-27/FR-2).
const MsgAdminRecoveryComplete = "Passwort gesetzt. Der Administrator kann sich jetzt mit dem neuen Passwort anmelden."

// MsgAdminRecoveryInvalid is the German microcopy for an invalid/expired/
// not-yet-approved/used admin-recovery token.
const MsgAdminRecoveryInvalid = "Dieser Wiederherstellungs-Token ist ungültig, abgelaufen oder noch nicht freigegeben."

// MsgLastAdminRecoveryBlocked is the German microcopy for the last-admin guard.
const MsgLastAdminRecoveryBlocked = "Die Wiederherstellung des letzten aktiven Administrators ist deaktiviert. Wende dich an den manuellen Wiederherstellungsprozess."

// MsgRecoveryReasonRequired is the German microcopy for a missing Begründung.
const MsgRecoveryReasonRequired = "Bitte gib eine Begründung für die Freigabe an."

// MsgRecoveryNotConfirmed is the German microcopy for an unchecked confirmation.
const MsgRecoveryNotConfirmed = "Bitte bestätige die Freigabe mit der Checkbox."

// MsgRecoveryMFARequired is the German microcopy asking the approving admin
// (with MFA enabled) to supply a current TOTP code (MFA step-up).
const MsgRecoveryMFARequired = "Zur Freigabe ist ein gültiger Zwei-Faktor-Code erforderlich."

// MsgRecoveryDenyReasonRequired is the German microcopy for a missing deny
// Begründung.
const MsgRecoveryDenyReasonRequired = "Bitte gib eine Begründung für die Ablehnung an."

// AdminRecoveryToken is the domain representation of a pending or approved
// admin-recovery token (FR-27). Only its SHA-256 hash is persisted; the raw
// token is returned exactly once to the approving admin and never stored.
type AdminRecoveryToken struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
	// ApprovedByUserID is the OTHER admin (B) who approved the request. Empty
	// means the request is still pending and its token is NOT usable.
	ApprovedByUserID string
	// RequestedByUserID is the admin (A) who created the request. The approving
	// admin (B) must never equal this value (dual-control).
	RequestedByUserID string
	// User is the owner snapshot resolved by the store (JOIN on users) so the
	// completion step can verify the account is still active.
	User *User
}

// AdminRecoveryRequest is the domain representation of a pending
// (not-yet-approved) admin-recovery request for the admin-B review surface
// (FR-27). It never carries the password hash.
type AdminRecoveryRequest struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
	// ApprovedByUserID is empty for pending requests (listings only ever show
	// pending requests).
	ApprovedByUserID string
	// RequestedByUserID is the requesting admin (A).
	RequestedByUserID string
	// User is the target admin snapshot (JOIN on users).
	User *User
}

// AdminRecoveryResult is the confirmation returned by RequestAdminRecovery.
type AdminRecoveryResult struct {
	Message string `json:"message"`
	// TargetEmail echoes the admin the recovery request was created for.
	TargetEmail string `json:"target_email"`
}

// AdminRecoveryApproveResult is the response returned to the approving admin
// (B). RecoveryToken is the single-use raw token to hand to admin A out-of-band;
// it is returned ONLY to the approving admin and never stored/logged.
type AdminRecoveryApproveResult struct {
	Message       string `json:"message"`
	RecoveryToken string `json:"recovery_token"`
}

// AdminRecoveryCompleteResult is the confirmation returned when an approved
// admin-recovery token is consumed to set a new password.
type AdminRecoveryCompleteResult struct {
	Message string `json:"message"`
}

// AdminRecoveryDenyResult is the confirmation returned when a pending
// admin-recovery request is denied (review finding 1.10).
type AdminRecoveryDenyResult struct {
	Message string `json:"message"`
}

// MsgAdminRecoveryDenied is the German confirmation returned after a recovery
// request is denied.
const MsgAdminRecoveryDenied = "Die Wiederherstellungsanfrage wurde abgelehnt."

// RequestAdminRecovery creates a dual-admin recovery request (FR-27). The
// caller (the requesting admin A — a locked-out admin identifies themselves by
// submitting their own email; an authenticated admin may request on another
// admin's behalf) must be an authenticated admin-group member. A recovery-marked
// single-use hashed 30-min token row is created for the target admin,
// invalidating any earlier recovery request, and stamped with the requesting
// admin (requested_by_user_id) so a requester can never approve their own
// request. The raw token is NOT returned to the requester — it becomes usable
// only after the OTHER admin approves it.
//
// Last-admin guard: if the target is the LAST active admin (one or fewer active
// admins remain), self-recovery is blocked with ErrLastAdminRecoveryBlocked and
// audited (admin.recovery.last_admin_blocked); the documented out-of-band manual
// bootstrap applies. Every created request is audited with the caller as actor
// (admin.recovery.request).
func (s *Service) RequestAdminRecovery(ctx context.Context, caller *User, targetEmail string) (*AdminRecoveryResult, error) {
	if caller == nil {
		return nil, ErrInvalidCredentials
	}
	isAdmin, err := s.resolveIsAdmin(ctx, caller.ID)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to resolve caller admin membership: %w", err)
	}
	if !isAdmin {
		return nil, ErrForbidden
	}

	normalized := strings.ToLower(strings.TrimSpace(targetEmail))
	if normalized == "" {
		return nil, ErrMissingFields
	}

	target, err := s.repo.GetUserByEmail(ctx, normalized)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to look up target admin: %w", err)
	}
	// A non-existent or non-active target, or a non-admin target, is answered
	// with the uniform confirmation — the admin-only route must not leak which
	// addresses are admins (anti-enumeration, consistent with FR-26).
	if target == nil || target.State != StateActive {
		s.log().Info("admin recovery requested for non-actionable target", "caller", caller.Email, "target", normalized)
		return &AdminRecoveryResult{Message: MsgAdminRecoveryRequested, TargetEmail: normalized}, nil
	}
	targetIsAdmin, err := s.resolveIsAdmin(ctx, target.ID)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to resolve target admin membership: %w", err)
	}
	if !targetIsAdmin {
		s.log().Info("admin recovery requested for a non-admin target", "caller", caller.Email, "target", normalized)
		return &AdminRecoveryResult{Message: MsgAdminRecoveryRequested, TargetEmail: normalized}, nil
	}

	// Last-admin guard (FR-27/AD-13): recovery of the last active admin is
	// disabled; the out-of-band manual bootstrap applies.
	activeAdmins, err := s.repo.CountActiveAdmins(ctx)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to count active admins: %w", err)
	}
	if activeAdmins <= 1 {
		// Mandatory audit (best-effort, NFR-O2).
		if err := s.repo.InsertAuditEvent(ctx, caller.ID, AuditOperationAdminRecoveryLastAdminBlocked, "", AuditSeverityHigh); err != nil {
			s.log().Warn("admin recovery last-admin audit write failed", "error", err)
		}
		s.log().Warn("admin recovery blocked: last active admin", "caller", caller.Email, "target", normalized)
		return nil, ErrLastAdminRecoveryBlocked
	}

	// Create the recovery request row: a recovery-marked single-use hashed
	// 30-min token (invalidating any earlier request for the target), stamped
	// with the requesting admin so the requester can never approve it. The raw
	// placeholder token is never returned to the requester.
	raw, err := newOpaqueToken()
	if err != nil {
		return nil, fmt.Errorf("user core: failed to generate admin recovery token: %w", err)
	}
	expiresAt := time.Now().UTC().Add(AdminRecoveryTokenTTL)
	if err := s.repo.CreateAdminRecoveryRequest(ctx, target.ID, caller.ID, hashToken(raw), expiresAt); err != nil {
		return nil, fmt.Errorf("user core: failed to create admin recovery request: %w", err)
	}

	// Audit the request with the caller as actor (NFR-O1/NFR-O2), carrying the
	// target email as detail and severity='high'. Best-effort.
	if err := s.repo.InsertAuditEvent(ctx, caller.ID, AuditOperationAdminRecoveryRequest, "target="+normalized, AuditSeverityHigh); err != nil {
		s.log().Warn("admin recovery request audit write failed", "error", err)
	}
	s.log().Info("admin recovery requested", "caller", caller.Email, "target", normalized)

	return &AdminRecoveryResult{Message: MsgAdminRecoveryRequested, TargetEmail: normalized}, nil
}

// ApproveAdminRecovery approves a pending admin-recovery request (FR-27). The
// approver (admin B) must be a DIFFERENT admin than the target (self-approval →
// ErrForbidden) AND a DIFFERENT admin than the requesting admin (a requester
// cannot approve their own request → ErrForbidden, review finding 1.10
// dual-control), must provide a mandatory Begründung (reason →
// ErrRecoveryReasonRequired) and must confirm via the checkbox
// (ErrRecoveryNotConfirmed). If the approver has MFA enabled, a valid TOTP code
// is required (MFA step-up → ErrRecoveryMFARequired).
//
// Defense-in-depth at approve time (review finding 1.10): the approver is
// re-verified to still be an active admin holding `admin.recovery.approve`, the
// target is re-verified to still be active, and the last-admin guard is
// re-checked. On approval the request's token is minted fresh (single-use,
// hashed at rest, fresh 30-min expiry), stamped with the approving admin, and
// the RAW token is returned to B — the ONLY caller who may see it — to hand to A
// out-of-band. The approval is audited as high-severity with B as actor and the
// Begründung in the operation detail (admin.recovery.approve). A target with no
// pending request / already approved / expired maps to ErrAdminRecoveryInvalid.
func (s *Service) ApproveAdminRecovery(ctx context.Context, approver *User, targetEmail, reason string, confirmed bool, totpCode string) (*AdminRecoveryApproveResult, error) {
	if approver == nil {
		return nil, ErrInvalidCredentials
	}
	if strings.TrimSpace(reason) == "" {
		return nil, ErrRecoveryReasonRequired
	}
	if !confirmed {
		return nil, ErrRecoveryNotConfirmed
	}

	// Defense-in-depth (review finding 1.10): re-verify the approver is still an
	// active admin holding `admin.recovery.approve` — a stale session must not
	// bypass a revoke/role change.
	if approver.State != StateActive {
		return nil, ErrForbidden
	}
	approverIsAdmin, err := s.resolveIsAdmin(ctx, approver.ID)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to resolve approver admin membership: %w", err)
	}
	if !approverIsAdmin {
		return nil, ErrForbidden
	}
	perms, err := s.repo.ListPermissionsByUser(ctx, approver.ID)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to resolve approver permissions: %w", err)
	}
	if !hasRecoveryApprovePermission(perms) {
		return nil, ErrForbidden
	}

	normalized := strings.ToLower(strings.TrimSpace(targetEmail))
	if normalized == "" {
		return nil, ErrMissingFields
	}
	target, err := s.repo.GetUserByEmail(ctx, normalized)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to look up target admin: %w", err)
	}
	if target == nil {
		return nil, ErrAdminRecoveryInvalid
	}
	// Defense-in-depth: the target must still be active at approve time.
	if target.State != StateActive {
		return nil, ErrAdminRecoveryInvalid
	}
	// Self-approval is forbidden: the approving and recovered admins must be
	// DIFFERENT accounts (FR-27).
	if target.ID == approver.ID {
		s.log().Warn("admin recovery self-approval rejected", "approver", approver.Email)
		return nil, ErrForbidden
	}
	// Dual-control (review finding 1.10): a requester can never approve their own
	// request. The requesting admin is resolved by the store; reject when the
	// approver created the request they are trying to approve.
	pending, err := s.repo.ListAdminRecoveryRequest(ctx)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to resolve pending recovery requests: %w", err)
	}
	for _, req := range pending {
		if req.UserID == target.ID && req.RequestedByUserID == approver.ID {
			s.log().Warn("admin recovery requester self-approval rejected", "approver", approver.Email, "target", normalized)
			return nil, ErrForbidden
		}
	}

	// Last-admin guard re-check at approve time (review finding 1.10): approving
	// the recovery of the last remaining active admin is disabled.
	activeAdmins, err := s.repo.CountActiveAdmins(ctx)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to count active admins at approve: %w", err)
	}
	if activeAdmins <= 1 {
		return nil, ErrLastAdminRecoveryBlocked
	}

	// MFA step-up (review finding 1.10): an approving admin with MFA enabled must
	// supply a valid current TOTP code.
	if approver.IsMFAEnabled {
		if approver.TotpSecretEncrypted == "" {
			return nil, ErrRecoveryMFARequired
		}
		secret, err := s.decryptSecret(approver.TotpSecretEncrypted)
		if err != nil {
			return nil, err
		}
		if !validTotpCode(secret, totpCode) {
			s.log().Warn("admin recovery approve rejected: invalid MFA code", "approver", approver.Email)
			return nil, ErrRecoveryMFARequired
		}
	}

	// Mint the deliverable raw token (generated at approve time, never stored)
	// and stamp the request as approved by this admin. The SQL sets a FRESH
	// 30-minute expiry (review finding 1.10).
	raw, err := newOpaqueToken()
	if err != nil {
		return nil, fmt.Errorf("user core: failed to generate admin recovery token: %w", err)
	}
	if _, err := s.repo.ApproveAdminRecovery(ctx, target.ID, approver.ID, hashToken(raw)); err != nil {
		if errors.Is(err, ErrAdminRecoveryInvalid) {
			s.log().Warn("admin recovery approval rejected", "target", normalized, "error", err)
			return nil, ErrAdminRecoveryInvalid
		}
		return nil, fmt.Errorf("user core: failed to approve admin recovery: %w", err)
	}

	// High-severity audit with the approver (B) as actor, the Begründung and
	// target in the operation detail (NFR-O1/NFR-O2, review finding 1.10).
	// Best-effort.
	detail := fmt.Sprintf("target=%s; reason=%s", normalized, strings.TrimSpace(reason))
	if err := s.repo.InsertAuditEvent(ctx, approver.ID, AuditOperationAdminRecoveryApprove, detail, AuditSeverityHigh); err != nil {
		s.log().Warn("admin recovery approve audit write failed", "error", err)
	}
	s.log().Info("admin recovery approved", "approver", approver.Email, "target", normalized, "reason", reason)

	return &AdminRecoveryApproveResult{Message: MsgAdminRecoveryApproved, RecoveryToken: raw}, nil
}

// DenyAdminRecovery denies a pending admin-recovery request (review finding
// 1.10, FR-27): the denying admin (B) must be a DIFFERENT admin than the target
// and than the requester, and must provide a Begründung. On deny the pending
// request is invalidated so it can no longer be approved, and the deny is
// audited as high-severity (admin.recovery.deny) with the reason in the detail.
func (s *Service) DenyAdminRecovery(ctx context.Context, approver *User, targetEmail, reason string) (*AdminRecoveryDenyResult, error) {
	if approver == nil {
		return nil, ErrInvalidCredentials
	}
	if strings.TrimSpace(reason) == "" {
		return nil, ErrRecoveryDenyReasonRequired
	}

	normalized := strings.ToLower(strings.TrimSpace(targetEmail))
	if normalized == "" {
		return nil, ErrMissingFields
	}
	target, err := s.repo.GetUserByEmail(ctx, normalized)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to look up target admin for deny: %w", err)
	}
	if target == nil {
		return nil, ErrAdminRecoveryInvalid
	}
	if target.ID == approver.ID {
		s.log().Warn("admin recovery self-deny rejected", "approver", approver.Email)
		return nil, ErrForbidden
	}
	// Dual-control: a requester can never deny (or approve) their own request.
	pending, err := s.repo.ListAdminRecoveryRequest(ctx)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to resolve pending recovery requests for deny: %w", err)
	}
	for _, req := range pending {
		if req.UserID == target.ID && req.RequestedByUserID == approver.ID {
			s.log().Warn("admin recovery requester self-deny rejected", "approver", approver.Email, "target", normalized)
			return nil, ErrForbidden
		}
	}

	if err := s.repo.DenyAdminRecovery(ctx, target.ID); err != nil {
		return nil, fmt.Errorf("user core: failed to deny admin recovery: %w", err)
	}

	detail := fmt.Sprintf("target=%s; reason=%s", normalized, strings.TrimSpace(reason))
	if err := s.repo.InsertAuditEvent(ctx, approver.ID, AuditOperationAdminRecoveryDeny, detail, AuditSeverityHigh); err != nil {
		s.log().Warn("admin recovery deny audit write failed", "error", err)
	}
	s.log().Info("admin recovery denied", "denier", approver.Email, "target", normalized, "reason", reason)

	return &AdminRecoveryDenyResult{Message: MsgAdminRecoveryDenied}, nil
}

// ListAdminRecoveryRequest returns the pending (not-yet-approved) recovery
// requests for the admin-B review surface (FR-27). The caller is gated by
// `admin.recovery.approve` upstream; here it is re-verified for defense-in-depth.
func (s *Service) ListAdminRecoveryRequest(ctx context.Context, caller *User) ([]*AdminRecoveryRequest, error) {
	if caller == nil {
		return nil, ErrInvalidCredentials
	}
	perms, err := s.repo.ListPermissionsByUser(ctx, caller.ID)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to resolve caller permissions: %w", err)
	}
	if !hasRecoveryApprovePermission(perms) {
		return nil, ErrForbidden
	}
	return s.repo.ListAdminRecoveryRequest(ctx)
}

// CompleteAdminRecovery consumes an APPROVED admin-recovery token to set a new
// password (FR-27/FR-2). The new password is validated FIRST (FR-2: ≥10, ≤1024,
// matching confirmation) BEFORE the token is consumed (review finding 1.10) — a
// policy-violating password never burns the token. A missing/unknown/expired/
// not-yet-approved token is rejected with ErrAdminRecoveryInvalid. On success
// the new password (Argon2id hash, AD-13) replaces the old hash, ALL sessions of
// the target are revoked (re-auth required, NFR-S2) and the completion is
// audited as high-severity (admin.recovery.complete, NFR-O1/NFR-O2).
//
// Availability (NFR-O1): session revocation and the audit write are best-effort
// — a failure is logged and does NOT roll back the successful password change.
func (s *Service) CompleteAdminRecovery(ctx context.Context, rawToken, newPassword, confirm string) (*AdminRecoveryCompleteResult, error) {
	if rawToken == "" {
		return nil, ErrAdminRecoveryInvalid
	}

	// Validate the new password BEFORE consuming the token (review finding
	// 1.10): a short/oversized/mismatched password is rejected without burning
	// the single-use token, so the admin can retry with a valid password.
	input := ChangePasswordInput{NewPassword: newPassword, NewPasswordConfirm: confirm}
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Atomic single-use consumption of an APPROVED recovery token (FR-27): the
	// store deletes the token in the same statement that returns it, and only
	// matches recovery-marked + approved tokens, so neither a concurrent
	// completion nor the FR-26 path can consume it twice or early. A genuine
	// store error is propagated (review finding 1.10); only ErrNoRows maps to
	// ErrAdminRecoveryInvalid.
	token, err := s.repo.ConsumeAdminRecoveryToken(ctx, hashToken(rawToken))
	if err != nil {
		if errors.Is(err, ErrAdminRecoveryInvalid) {
			return nil, ErrAdminRecoveryInvalid
		}
		return nil, fmt.Errorf("user core: failed to consume admin recovery token: %w", err)
	}
	if token == nil {
		return nil, ErrAdminRecoveryInvalid
	}
	if time.Now().UTC().After(token.ExpiresAt) {
		return nil, ErrAdminRecoveryInvalid
	}
	// Defense-in-depth: an account deactivated after issuance must not complete
	// a recovery.
	if token.User == nil || token.User.State != StateActive {
		return nil, ErrAdminRecoveryInvalid
	}

	newHash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return nil, fmt.Errorf("user core: failed to hash admin recovery password: %w", err)
	}
	if _, err := s.repo.UpdateUserPassword(ctx, token.UserID, newHash); err != nil {
		return nil, fmt.Errorf("user core: failed to persist admin recovery password: %w", err)
	}

	// Re-auth required (NFR-S2): revoke ALL sessions of the target admin.
	if err := s.RevokeAllSessions(ctx, token.UserID); err != nil {
		s.log().Warn("admin recovery session revocation failed", "error", err)
	}

	// High-severity audit (NFR-O1/NFR-O2): best-effort. The actor is the target
	// admin (A) who completed the recovery.
	if err := s.repo.InsertAuditEvent(ctx, token.UserID, AuditOperationAdminRecoveryComplete, "target="+token.User.Email, AuditSeverityHigh); err != nil {
		s.log().Warn("admin recovery complete audit write failed", "error", err)
	}
	s.log().Info("admin recovery completed", "target", token.User.Email)

	return &AdminRecoveryCompleteResult{Message: MsgAdminRecoveryComplete}, nil
}

// hasRecoveryApprovePermission reports whether perms includes the
// `admin.recovery.approve` permission (AD-12).
func hasRecoveryApprovePermission(perms []string) bool {
	for _, p := range perms {
		if p == AdminRecoveryApprovePermission {
			return true
		}
	}
	return false
}

// AdminRecoveryApprovePermission is the permission code required to approve or
// deny an admin-recovery request (FR-27/AD-12). It is carried by the seeded
// admin group.
const AdminRecoveryApprovePermission = "admin.recovery.approve"
