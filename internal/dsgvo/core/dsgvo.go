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
	"time"

	toolports "github.com/saskia-peters/gear/internal/tools/ports"
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

// AuditSeverityNormal is the standard audit severity for report generations.
const AuditSeverityNormal = "normal"

// MsgUserNotFound is the German microcopy for an unknown report target (404,
// REPORT_UNKNOWN).
const MsgUserNotFound = "Der Benutzer wurde nicht gefunden."

// ErrForbidden is returned when the actor's live permission set does not hold
// `dsgvo.access_report` (defense-in-depth, AD-6). Handlers map it to the
// uniform 403 with no hint of what is missing (AD-6: no personal data on the
// wire without the code).
var ErrForbidden = errors.New("dsgvo core: forbidden")

// PermissionResolver resolves a user's live permission set (AD-12) for the
// defense-in-depth re-check. The User module's postgres repository implements
// it (ListPermissionsByUser).
type PermissionResolver interface {
	ListPermissionsByUser(ctx context.Context, userID string) ([]string, error)
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
// consumes the User and Tool export ports and the audit seam. It owns no
// tables; its ports extend naturally into Story 3.4's deletion transaction.
type Service struct {
	userPort userports.DSGVOExportPort
	toolPort toolports.DSGVOInspectionExportPort
	perms    PermissionResolver
	audit    AuditWriter
	logger   *slog.Logger
}

// NewService constructs the DSGVO orchestrator. userPort is the User module's
// data-access export seam; toolPort is the Tool module's inspection export
// seam; perms is the User-module repository read-only seam for the
// defense-in-depth permission re-check (AD-6); audit is the User-owned audit
// trail (NFR-O1/NFR-O2). logger may be nil (falls back to slog.Default()).
func NewService(userPort userports.DSGVOExportPort, toolPort toolports.DSGVOInspectionExportPort, perms PermissionResolver, audit AuditWriter, logger *slog.Logger) *Service {
	return &Service{userPort: userPort, toolPort: toolPort, perms: perms, audit: audit, logger: logger}
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