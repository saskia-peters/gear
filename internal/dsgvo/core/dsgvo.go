// Package core hosts the DSGVO orchestrator (Story 3.3, FR-24/AD-8): the
// composition-root wiring point that assembles a user's data-access report. It
// owns NO tables — it only composes the User module's read-only export port
// (profile/roles/groups/grants/qualifications/sessions/login-attempts) and the
// Tool module's read-only export port (per-user inspection records), re-checks
// `dsgvo.access_report` defense-in-depth (AD-6) and audits every generation
// (NFR-O1/NFR-O2). No module writes another's SQL (AD-8) — the orchestrator
// calls each module's exported port and nothing else.
package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	toolports "github.com/saskia-peters/gear/internal/tools/ports"
	usercore "github.com/saskia-peters/gear/internal/user/core"
	userports "github.com/saskia-peters/gear/internal/user/ports"
)

// AccessReportPermission is the server-authoritative gate code for the DSGVO
// data-access report surface (AD-6). One Go const so the route mount, the core
// re-check and the SPA-facing documentation never drift.
const AccessReportPermission = "dsgvo.access_report"

// DeletePermission is the server-authoritative gate code for the DSGVO
// account-deletion surface (Story 3.4, AD-6). The DSGVO HTTP surface is
// reachable by ANY of [dsgvo.access_report, dsgvo.delete]; each tab then
// applies its own code.
const DeletePermission = "dsgvo.delete"

// AuditOperationAccessReport is the audit-operation tag for a generated
// data-access report (NFR-O1/NFR-O2). It DERIVES from AccessReportPermission so
// the audit tag and the gate code can never drift.
const AuditOperationAccessReport = AccessReportPermission

// AuditOperationDelete is the audit-operation tag for an account deletion
// (Story 3.4, NFR-O1/NFR-O2). It DERIVES from DeletePermission so the audit tag
// and the gate code can never drift.
const AuditOperationDelete = DeletePermission

// AuditOperationPurge is the audit-operation tag for an on-demand archive
// purge (Story 3.4, FR-24/NFR-O1/NFR-O2). It carries the archive id (actor,
// timestamp, operation) — the purged user is gone from every live table, so the
// archive id is the audit's durable handle.
const AuditOperationPurge = "dsgvo.purge"

// AuditSeverityNormal is the standard audit severity for report generations.
const AuditSeverityNormal = "normal"

// MsgUserNotFound is the German microcopy for an unknown report target (404,
// REPORT_UNKNOWN).
const MsgUserNotFound = "Der Benutzer wurde nicht gefunden."

// MsgDeletedAccountNotFound is the German microcopy for a purge of an unknown /
// already-purged archive id (404, PURGE_MISSING).
const MsgDeletedAccountNotFound = "Der gelöschte Account wurde nicht gefunden."

// MsgDsgvoDeleteSelf is the German 400 microcopy for a self-deletion attempt
// (DELETE_SELF — an admin cannot erase their own account).
const MsgDsgvoDeleteSelf = "Du kannst dein eigenes Konto nicht löschen."

// MsgDsgvoReasonRequired is the German 400 microcopy for an empty/whitespace
// deletion reason (DELETE_EMPTY_REASON — the Begründung is MANDATORY).
const MsgDsgvoReasonRequired = "Bitte gib eine Begründung an."

// MsgDsgvoReasonTooLong is the German 400 microcopy for a reason over
// MaxDeleteReasonRunes (DELETE_LONG_REASON).
const MsgDsgvoReasonTooLong = "Die Begründung ist zu lang (maximal 2000 Zeichen)."

// MsgDeleteConfirmation is the German confirmation of a successful account
// deletion (DELETE_OK), formatted with the target user id.
const MsgDeleteConfirmation = "Konto %s wurde gelöscht."

// MsgPurgeConfirmation is the German confirmation of a successful archive purge
// (PURGE_OK).
const MsgPurgeConfirmation = "Der gelöschte Account wurde endgültig gelöscht."

// MaxDeleteReasonRunes bounds the mandatory deletion reason (Story 3.4,
// FR-24/AD-9): ≤ 2000 runes, counted server-side. The trimmed reason is what is
// validated AND persisted/audited.
const MaxDeleteReasonRunes = 2000

// ErrForbidden is returned when the actor's live permission set does not hold
// `dsgvo.access_report` / `dsgvo.delete` (defense-in-depth, AD-6). Handlers map
// it to the uniform 403 with no hint of what is missing (AD-6: no personal data
// on the wire without the code).
var ErrForbidden = errors.New("dsgvo core: forbidden")

// ErrDsgvoDeleteSelf is returned when the actor attempts to delete their own
// account (DELETE_SELF). Handlers map it to the German 400.
var ErrDsgvoDeleteSelf = errors.New("dsgvo core: cannot delete own account")

// ErrDsgvoReasonInvalid is the sentinel wrapping a German validation message for
// a 400 invalid deletion reason (DELETE_EMPTY_REASON / DELETE_LONG_REASON).
var ErrDsgvoReasonInvalid = errors.New("dsgvo core: deletion reason invalid")

// DeleteReasonError carries the German validation message for a 400 invalid
// deletion reason. It unwraps to ErrDsgvoReasonInvalid so callers can match the
// sentinel while still rendering the field-specific microcopy.
type DeleteReasonError struct {
	Message string
}

func (e *DeleteReasonError) Error() string { return e.Message }
func (e *DeleteReasonError) Unwrap() error { return ErrDsgvoReasonInvalid }

// PermissionResolver resolves a user's live permission set (AD-12) for the
// defense-in-depth re-check. The User module's postgres repository implements
// it (ListPermissionsByUser).
type PermissionResolver interface {
	ListPermissionsByUser(ctx context.Context, userID string) ([]string, error)
}

// ActorResolver resolves the authenticated actor's *core.User for the deletion
// flow (Story 3.4) — the User lifecycle port's SoftDeleteAndArchive is an
// actor-typed port (like the admin Service methods), so the orchestrator needs
// the actor row to stamp deleted_by. The User module's postgres repository
// implements it (GetUserByID; an unknown/malformed id maps to
// usercore.ErrAdminUserNotFound).
type ActorResolver interface {
	GetUserByID(ctx context.Context, userID string) (*usercore.User, error)
}

// AuditWriter appends to the User-owned audit trail (NFR-O1/NFR-O2). The User
// module's postgres repository implements it; the DSGVO orchestrator never
// authors another module's SQL (AD-8/AD-11).
type AuditWriter interface {
	InsertAuditEvent(ctx context.Context, userID, operation, detail, severity string) error
}

// AccessReport is the assembled DSGVO data-access report (FR-24): the User
// module's data export (profile + roles/groups/grants/qualifications + auth
// history) and the Tool module's inspection export (inspections +
// reinstatements + per-tool summary), stamped with the generation timestamp.
// It is a read-only artifact — the deletion (Story 3.4) is a separate surface.
type AccessReport struct {
	User        *userports.UserDataExport           `json:"user"`
	Tools       *toolports.UserInspectionDataExport `json:"tools"`
	GeneratedAt time.Time                           `json:"generated_at"`
}

// Service is the DSGVO orchestrator (AD-8): the single assembly point that
// consumes the User and Tool module ports and the audit seam. It owns no
// tables; its ports extend naturally into Story 3.4's deletion transaction
// (the User lifecycle port SoftDeleteAndArchive/ListDeletedAccounts/
// PurgeDeletedAccount and the Tool anonymization port).
type Service struct {
	userPort     userports.DSGVOExportPort
	toolPort     toolports.DSGVOInspectionExportPort
	userDeletion userports.DSGVODeletionPort
	toolDeletion toolports.DSGVODeletionPort
	perms        PermissionResolver
	actor        ActorResolver
	audit        AuditWriter
	logger       *slog.Logger
}

// NewService constructs the DSGVO orchestrator. userPort is the User module's
// data-access export seam; toolPort is the Tool module's inspection export
// seam; userDeletion/toolDeletion are the Story 3.4 lifecycle write seams (the
// same module Services as the export ports, wired through their narrow
// deletion ports); perms is the User-module repository read-only seam for the
// defense-in-depth permission re-check (AD-6); actor resolves the authenticated
// actor's *core.User for the deletion port (the User-module repository
// implements it, GetUserByID); audit is the User-owned audit trail
// (NFR-O1/NFR-O2). logger may be nil (falls back to slog.Default()).
func NewService(userPort userports.DSGVOExportPort, toolPort toolports.DSGVOInspectionExportPort, userDeletion userports.DSGVODeletionPort, toolDeletion toolports.DSGVODeletionPort, perms PermissionResolver, actor ActorResolver, audit AuditWriter, logger *slog.Logger) *Service {
	return &Service{userPort: userPort, toolPort: toolPort, userDeletion: userDeletion, toolDeletion: toolDeletion, perms: perms, actor: actor, audit: audit, logger: logger}
}

// log returns the configured logger or slog.Default().
func (s *Service) log() *slog.Logger {
	if s.logger != nil {
		return s.logger
	}
	return slog.Default()
}

// GenerateAccessReport assembles the DSGVO data-access report of one user
// (REPORT_OK, FR-24/AD-8): it re-checks `dsgvo.access_report`
// defense-in-depth (AD-6), composes the User export (profile + roles/groups/
// grants/qualifications + sessions + login-attempt state) and the Tool export
// (inspections + reinstatements + per-tool summary) and audits the generation
// with actor, timestamp, operation and target user id (NFR-O1/NFR-O2).
//
// I/O matrix:
//   - REPORT_OK: `dsgvo.access_report` holder + existing user → 200 with the
//     assembled report (profile, roles/groups/grants/quals, auth history,
//     inspection records + counts).
//   - REPORT_GATED: caller lacks dsgvo.access_report → ErrForbidden (403, no
//     personal data exposed, AD-6).
//   - REPORT_UNKNOWN: unknown target user id → the user export port's
//     ErrAdminUserNotFound (404 German).
//   - REPORT_SECRETS: the assembled report never carries a password hash, TOTP
//     secret or one-time-password hash — the user export strips them (the
//     orchestrator passes the port output through verbatim).
//   - REPORT_NIL_EXPORT: an export port answers (nil, nil) — a wiring/port
//     defect — → a 500-class error, never a `null` user/tools section in the
//     report (the handler maps it to the uniform 500).
//   - ORDERING: the USER export runs FIRST and any error (incl.
//     ErrAdminUserNotFound) short-circuits — a malformed/unknown target id
//     consistently answers 404 and the TOOL export port never sees it.
func (s *Service) GenerateAccessReport(ctx context.Context, actorID, targetUserID string) (*AccessReport, error) {
	if err := s.requireAccessReportPermission(ctx, actorID); err != nil {
		return nil, err
	}
	if s.userPort == nil || s.toolPort == nil {
		return nil, fmt.Errorf("dsgvo core: export ports are not wired")
	}

	userData, err := s.userPort.ExportUserData(ctx, targetUserID)
	if err != nil {
		return nil, err
	}
	if userData == nil {
		// A (nil, nil) user-export return is a wiring/port defect — never let
		// it surface as a `null` user section in the report. Answer a 500-class
		// error so the handler never serializes a half-assembled report.
		return nil, fmt.Errorf("dsgvo core: user export returned a nil export for target %q", targetUserID)
	}
	toolData, err := s.toolPort.ExportUserInspectionData(ctx, targetUserID)
	if err != nil {
		return nil, err
	}
	if toolData == nil {
		// Same guard for the Tool export: a nil tools section must never reach
		// the wire (the handler maps this error to the uniform 500).
		return nil, fmt.Errorf("dsgvo core: tool export returned a nil export for target %q", targetUserID)
	}

	report := &AccessReport{
		User:        userData,
		Tools:       toolData,
		GeneratedAt: time.Now().UTC(),
	}

	s.auditAccessReport(ctx, actorID, targetUserID)
	s.log().Info("dsgvo data-access report generated",
		"actor", actorID, "target", targetUserID,
		"generated_at", report.GeneratedAt)

	return report, nil
}

// DeleteAccountResult is the account-deletion response (Story 3.4, DELETE_OK):
// the server-authoritative German confirmation.
type DeleteAccountResult struct {
	Message string `json:"message"`
}

// DeletedAccountRow is one archived account of the "Gelöschte Konten" surface
// (LIST_DELETED): the row the admin sees and purges on demand. It carries the
// archive id (the purge handle), the scrubbed display name + email, the
// deletion timestamp and the reason. No secret material is ever present.
type DeletedAccountRow struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	DeletedAt   time.Time `json:"deleted_at"`
	Reason      string    `json:"reason"`
}

// DeleteAccount erases one user's account per the right of erasure (Story 3.4,
// FR-24/AD-8): it re-checks `dsgvo.delete` defense-in-depth (AD-6), rejects
// SELF-deletion (DELETE_SELF), validates the MANDATORY trimmed reason (≤ 2000
// runes, DELETE_EMPTY_REASON / DELETE_LONG_REASON) and then runs the ordered
// deletion: (1) resolve the actor, (2) verify the target exists (and is not an
// already-deleted tombstone), (3) the Tool lifecycle port rewrites the target's
// inspection/reinstatement references to the canonical DeletedUserID sentinel,
// (4) the User lifecycle port soft-deletes + archives the account, and (5) the
// operation is audited `dsgvo.delete` best-effort + structured-logged
// (NFR-O1/NFR-O2). NO hard delete happens here — the account becomes a
// scrubbed `deleted` tombstone and its personal data moves into the archive
// (the hard purge is a separate admin action). An unknown target (or an
// already-deleted tombstone) maps to ErrAdminUserNotFound (DELETE_UNKNOWN →
// uniform 404). Every check runs BEFORE the first mutation — a failure before
// the Tool rewrite leaves NO side effects (no partial apply).
//
// I/O matrix:
//   - DELETE_OK: `dsgvo.delete` holder + existing non-self user + valid reason
//     → the German confirmation; refs → "Deleted User"; account soft-deleted +
//     archived; re-login blocked.
//   - DELETE_SELF: target == actor (case-INSENSITIVE — a case-variant UUID
//     cannot bypass the guard) → ErrDsgvoDeleteSelf (German 400).
//   - DELETE_EMPTY_REASON / DELETE_LONG_REASON: trimmed reason empty or > 2000
//     runes → *DeleteReasonError (German 400, no write).
//   - DELETE_UNKNOWN: target user absent (or already deleted) →
//     ErrAdminUserNotFound (German 404), resolved BEFORE the Tool rewrite.
//   - DELETE_GATED: caller lacks dsgvo.delete → ErrForbidden (403, no data
//     exposed, AD-6).
//   - NIL_ACTOR: the actor resolver answers (nil, nil) — a wiring defect → a
//     500-class error, never a panic (and never a mutation).
//   - ORDERING: actor resolution + target verification run BEFORE any mutation;
//     the Tool rewrite is the FIRST mutation and the User soft-delete the
//     second; the audit runs last best-effort.
func (s *Service) DeleteAccount(ctx context.Context, actorID, targetUserID, reason string) (*DeleteAccountResult, error) {
	if err := s.requireDeletePermission(ctx, actorID); err != nil {
		return nil, err
	}
	// DELETE_SELF: compare case-INSENSITIVELY so a case-variant UUID cannot
	// bypass the guard (uuidv7 ids are lowercase in practice, but the check must
	// not depend on that).
	if strings.EqualFold(actorID, targetUserID) {
		return nil, ErrDsgvoDeleteSelf
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, &DeleteReasonError{Message: MsgDsgvoReasonRequired}
	}
	if utf8.RuneCountInString(reason) > MaxDeleteReasonRunes {
		return nil, &DeleteReasonError{Message: MsgDsgvoReasonTooLong}
	}
	if s.userDeletion == nil || s.toolDeletion == nil || s.actor == nil {
		return nil, fmt.Errorf("dsgvo core: deletion ports are not wired")
	}

	// (1) Resolve the actor FIRST (for the deleted_by stamp + the nil-actor
	// guard) — before ANY mutation. A (nil, nil) actor-resolver return is a
	// wiring defect → a 500-class error, never a panic.
	actor, err := s.actor.GetUserByID(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if actor == nil {
		return nil, fmt.Errorf("dsgvo core: actor resolver returned a nil actor for %q", actorID)
	}

	// (2) Verify the target exists AND is not an already-deleted tombstone
	// (the surface treats `deleted` as non-existent). This runs BEFORE the Tool
	// rewrite, so an unknown/already-deleted target leaves NO side effects.
	target, err := s.actor.GetUserByID(ctx, targetUserID)
	if err != nil {
		return nil, err // ErrAdminUserNotFound → German 404 (DELETE_UNKNOWN)
	}
	if target == nil || target.State == usercore.StateDeleted {
		return nil, usercore.ErrAdminUserNotFound
	}

	// (3) Tool rewrite: the target's inspection/reinstatement references flip to
	// the canonical DeletedUserID sentinel in ONE transaction. This is the FIRST
	// mutation — every check above already passed.
	if err := s.toolDeletion.AnonymizeUserReferences(ctx, targetUserID); err != nil {
		return nil, err
	}
	// (4) User soft-delete + archive: the account becomes the scrubbed `deleted`
	// tombstone (re-login permanently blocked) and its personal data moves into
	// dsgvo_deleted_accounts.
	if err := s.userDeletion.SoftDeleteAndArchive(ctx, actor, targetUserID, reason); err != nil {
		return nil, err
	}

	// (5) Audit + structured log (NFR-O1/NFR-O2): actor, target id and reason —
	// never a secret (the Never rules exclude secrets from the audit detail).
	s.auditDelete(ctx, actorID, targetUserID, reason)
	s.log().Info("dsgvo account deleted", "actor", actorID, "target", targetUserID)

	return &DeleteAccountResult{Message: fmt.Sprintf(MsgDeleteConfirmation, targetUserID)}, nil
}

// ListDeletedAccounts returns the archived (soft-deleted) accounts for the
// admin "Gelöschte Konten" surface (LIST_DELETED, Story 3.4): it re-checks
// `dsgvo.delete` defense-in-depth (AD-6) and maps the User port's archived rows
// (newest-first) onto the wire rows. An empty history answers an empty list.
func (s *Service) ListDeletedAccounts(ctx context.Context, actorID string) ([]*DeletedAccountRow, error) {
	if err := s.requireDeletePermission(ctx, actorID); err != nil {
		return nil, err
	}
	if s.userDeletion == nil {
		return nil, fmt.Errorf("dsgvo core: user deletion port is not wired")
	}
	accounts, err := s.userDeletion.ListDeletedAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("dsgvo core: failed to list deleted accounts: %w", err)
	}
	rows := make([]*DeletedAccountRow, 0, len(accounts))
	for _, a := range accounts {
		rows = append(rows, &DeletedAccountRow{
			ID:          a.ID,
			Email:       a.Email,
			DisplayName: a.DisplayName,
			DeletedAt:   a.DeletedAt,
			Reason:      a.Reason,
		})
	}
	return rows, nil
}

// PurgeDeletedAccount hard-deletes an archived account AND its (now-scrubbed)
// users tombstone on admin demand (PURGE_OK, Story 3.4, FR-24): it re-checks
// `dsgvo.delete` defense-in-depth (AD-6) and delegates to the User lifecycle
// port, which deletes the archive row + the users tombstone in ONE transaction
// (the ONLY hard delete; the users delete CASCADEs sessions/reset tokens and
// auto-SET-NULLs the audit/recovery refs). The operation is audited
// `dsgvo.purge` (actor, archive id) best-effort (NFR-O1/NFR-O2). An unknown /
// already-purged archive id maps to the user port's
// ErrDeletedAccountNotFound (PURGE_MISSING → German 404).
func (s *Service) PurgeDeletedAccount(ctx context.Context, actorID, archiveID string) error {
	if err := s.requireDeletePermission(ctx, actorID); err != nil {
		return err
	}
	if s.userDeletion == nil {
		return fmt.Errorf("dsgvo core: user deletion port is not wired")
	}
	if err := s.userDeletion.PurgeDeletedAccount(ctx, archiveID); err != nil {
		return err
	}
	s.auditPurge(ctx, actorID, archiveID)
	s.log().Info("dsgvo deleted account purged", "actor", actorID, "archive", archiveID)
	return nil
}

// requireAccessReportPermission re-verifies (defense-in-depth, AD-6) that the
// actor's LIVE permission set holds `dsgvo.access_report`. An empty actor ID
// or a missing code maps to ErrForbidden.
func (s *Service) requireAccessReportPermission(ctx context.Context, actorID string) error {
	if actorID == "" {
		return ErrForbidden
	}
	if s.perms == nil {
		return fmt.Errorf("dsgvo core: permission resolver is not wired")
	}
	perms, err := s.perms.ListPermissionsByUser(ctx, actorID)
	if err != nil {
		return fmt.Errorf("dsgvo core: failed to resolve actor permissions: %w", err)
	}
	for _, p := range perms {
		if p == AccessReportPermission {
			return nil
		}
	}
	return ErrForbidden
}

// auditAccessReport writes the audit row best-effort (NFR-O1/NFR-O2): the
// actor, the operation (dsgvo.access_report), the target user id and the
// standard severity. A failed audit write is logged, never rolled back into
// the report.
func (s *Service) auditAccessReport(ctx context.Context, actorID, targetUserID string) {
	if s.audit == nil {
		s.log().Warn("dsgvo core: audit writer is not wired", "operation", AuditOperationAccessReport)
		return
	}
	if err := s.audit.InsertAuditEvent(ctx, actorID, AuditOperationAccessReport, "target="+targetUserID, AuditSeverityNormal); err != nil {
		s.log().Warn("dsgvo core: access-report audit write failed", "operation", AuditOperationAccessReport, "error", err)
	}
}

// requireDeletePermission re-verifies (defense-in-depth, AD-6) that the actor's
// LIVE permission set holds `dsgvo.delete`. An empty actor ID or a missing code
// maps to ErrForbidden.
func (s *Service) requireDeletePermission(ctx context.Context, actorID string) error {
	if actorID == "" {
		return ErrForbidden
	}
	if s.perms == nil {
		return fmt.Errorf("dsgvo core: permission resolver is not wired")
	}
	perms, err := s.perms.ListPermissionsByUser(ctx, actorID)
	if err != nil {
		return fmt.Errorf("dsgvo core: failed to resolve actor permissions: %w", err)
	}
	for _, p := range perms {
		if p == DeletePermission {
			return nil
		}
	}
	return ErrForbidden
}

// auditDelete writes the account-deletion audit row best-effort (Story 3.4,
// NFR-O1/NFR-O2): actor, operation (dsgvo.delete), target id + reason. The
// detail carries NO secret material (the Never rules exclude secrets). A failed
// audit write is logged, never rolled back into the deletion.
func (s *Service) auditDelete(ctx context.Context, actorID, targetUserID, reason string) {
	if s.audit == nil {
		s.log().Warn("dsgvo core: audit writer is not wired", "operation", AuditOperationDelete)
		return
	}
	if err := s.audit.InsertAuditEvent(ctx, actorID, AuditOperationDelete, "target="+targetUserID+" reason="+reason, AuditSeverityNormal); err != nil {
		s.log().Warn("dsgvo core: delete audit write failed", "operation", AuditOperationDelete, "error", err)
	}
}

// auditPurge writes the purge audit row best-effort (Story 3.4, NFR-O1/NFR-O2):
// actor, operation (dsgvo.purge) and the archive id (the durable handle — the
// purged user is gone from every live table). A failed audit write is logged,
// never rolled back into the purge.
func (s *Service) auditPurge(ctx context.Context, actorID, archiveID string) {
	if s.audit == nil {
		s.log().Warn("dsgvo core: audit writer is not wired", "operation", AuditOperationPurge)
		return
	}
	if err := s.audit.InsertAuditEvent(ctx, actorID, AuditOperationPurge, "archive="+archiveID, AuditSeverityNormal); err != nil {
		s.log().Warn("dsgvo core: purge audit write failed", "operation", AuditOperationPurge, "error", err)
	}
}
