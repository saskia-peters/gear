package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	admcore "github.com/saskia-peters/gear/internal/admin/core"
)

// Inspection START (Story 5.1, FR-11/AD-7): the first Epic 5 surface — the
// qualification-gated inspection start. Given a tool id, the core resolves the
// tool AND its type's required_qualification_id (intra-module JOIN on Tool-owned
// tool_types, AD-8/AD-11) and, when the type requires a qualification, checks
// the caller's granted qualifications through the User module's
// QualificationCatalogPort (EXPIRY-AWARE, AD-7/FR-22) — the ONLY cross-module
// resolution; the Tool module never joins user tables. The start is the GATE:
// eligible → the /start response (tool + its type's inspection_mode); ineligible
// (missing or expired qualification) → ErrToolQualificationMissing (403, German
// explanation); unknown/archived tool → ErrToolNotFound (404). No inspection
// record is created here (Stories 5.2/5.4/5.5); the eventual submit re-validates
// independently (AD-6). The route gateway requires `inspection.submit`; the core
// re-checks the exact code defense-in-depth (AD-6).

// InspectionSubmitPermission is the server-authoritative gate code for the
// inspection-start (and eventual submit) surface (AD-6). All base roles hold it
// (the helfende role seeds inspection.submit). One Go const so the route mount,
// the core re-check and the SPA-facing documentation never drift.
const InspectionSubmitPermission = "inspection.submit"

// ToolReinstatePermission is the server-authoritative gate code for the
// reinstatement surface (Story 5.6, FR-15/AD-9/AD-6). Only Fuehrung/Admin
// holders (the base roles seed tool.reinstate) reach it. One Go const so the
// route mount, the core re-check and the SPA-facing documentation never drift.
const ToolReinstatePermission = "tool.reinstate"

// InspectionHistoryViewPermission is the server-authoritative gate code for the
// per-tool inspection/reinstatement history surface (Story 6.3, FR-18/AD-6).
// Schirrmeister/Fuehrung/Admin holders reach it; the SPA skips the history
// fetch for non-holders as a courtesy, but the SERVER is the gate — a
// non-holder answers the uniform 403 with no history data. One Go const so the
// route mount, the core re-check and the SPA-facing documentation never drift.
const InspectionHistoryViewPermission = "inspection.history.view"

// DeletedUserDisplayName is the literal name shown for an inspector/actor whose
// user account no longer exists (Story 3.4 DSGVO forward-compat, AD-8): the
// plain FK-less inspector_id/actor_id resolves through the DisplayNameResolver
// seam and a MISSING user row maps to this literal — never a 404, never an
// empty string.
const DeletedUserDisplayName = "Deleted User"

// Audit-operation tag for a persisted reinstatement (NFR-O1/NFR-O2). The
// reinstatement row itself is the audit source of truth; this is the operation
// ledger row. It DERIVES from ToolReinstatePermission so the audit tag and the
// gate code can never drift — the audit operation for a tool.reinstate write is
// the same code.
const AuditOperationToolReinstate = ToolReinstatePermission

// Audit-operation tag for an ELIGIBLE inspection start (NFR-O1, best-effort).
// A FAILED gate (ineligible attempt) is logged structured but not audited as a
// user action — the deny log line carries the structured record.
const AuditOperationInspectionStart = "inspection.start"

// ErrToolQualificationMissing is returned when the tool's type requires a
// qualification the caller does NOT hold (or holds EXPIRED — live resolution,
// AD-7/FR-22). Handlers map it to the uniform 403 with the German explanation.
// It is distinct from ErrForbidden: the caller IS allowed to start inspections
// in general (inspection.submit held) but not on THIS tool.
var ErrToolQualificationMissing = errors.New("tools core: required qualification missing")

// German microcopy for the inspection-start surface (FR-11/UX-DR8). The
// dashboard control shows this message inline and disables the row's start
// button for the session after the server answers it (the 403 IS the gate,
// AD-6 — never client-side trust).
const MsgToolQualificationMissing = "Erforderliche Qualifikation fehlt."

// ErrToolOutOfService is returned when a start/submit is attempted on a tool
// that is Out of Service (Story 5.6, FR-14/AD-4): an OOS tool is NOT
// inspectable — start AND submit are rejected BEFORE the qualification gate.
// Handlers map it to the uniform 403 with the German explanation.
var ErrToolOutOfService = errors.New("tools core: tool is out of service")

// German microcopy for the OOS not-inspectable block (FR-14/AD-4, UX-DR8).
const MsgToolOutOfService = "Das Gerät ist außer Betrieb und kann nicht geprüft werden."

// InspectionStartResult is the eligible /start response (Story 5.1): the tool
// plus its type's inspection_mode, enough for the stub inspection screen (the
// real screen is Stories 5.2/5.4/5.5). No inspection record is created.
type InspectionStartResult struct {
	ToolID         string
	ToolName       string
	ToolTypeID     string
	ToolTypeName   string
	InspectionMode string
	ChecklistItems []ToolTypeChecklistItem
}

// StartInspection is the qualification-gated inspection start (Story 5.1,
// FR-11/AD-7): it resolves the tool + its type's required_qualification_id
// (intra-module store read), then — when the type REQUIRES a qualification —
// resolves the caller's granted qualifications through the User module's
// QualificationCatalogPort (expiry-aware). Eligible → the start result (tool +
// mode); missing/expired qualification → ErrToolQualificationMissing (403);
// unknown/archived tool → ErrToolNotFound (404). The `inspection.submit` code is
// re-checked defense-in-depth (AD-6). No inspection record is created here — the
// start is the gate; submit (Stories 5.4/5.5) re-validates independently (AD-6).
//
// I/O matrix:
//   - START_ELIGIBLE / START_NO_QUAL_TYPE: type has NO required qualification →
//     200 (no port call needed — empty required qualification is eligible).
//   - START_QUALIFIED: type requires Q, caller holds Q (active) → 200.
//   - START_MISSING_QUAL: type requires Q, caller lacks Q → 403.
//   - START_EXPIRED_QUAL: type requires Q, caller's Q assignment expired →
//     403 (expired = not held, live resolution).
//   - START_TOOL_NOT_FOUND: unknown / archived tool id → 404.
//   - START_FORBIDDEN: caller lacks inspection.submit → ErrForbidden (403, no
//     tool data exposed, AD-6).
func (s *Service) StartInspection(ctx context.Context, actorID, toolID string) (*InspectionStartResult, error) {
	if err := s.requireToolsPermission(ctx, actorID, []string{InspectionSubmitPermission}); err != nil {
		return nil, err
	}

	tool, err := s.store.GetToolWithTypeQualification(ctx, toolID)
	if err != nil {
		if errors.Is(err, ErrToolNotFound) {
			return nil, ErrToolNotFound
		}
		return nil, fmt.Errorf("tools core: failed to resolve tool for inspection: %w", err)
	}

	// OOS block (Story 5.6, FR-14/AD-4): an Out-of-Service tool is NOT
	// inspectable — the start is rejected BEFORE the qualification gate (the
	// OOS state is the stronger condition; an OOS tool never proceeds to a
	// qualification check).
	if oos, err := s.toolIsOutOfService(ctx, toolID); err != nil {
		return nil, err
	} else if oos {
		s.log().Warn("inspection start denied: tool out of service",
			"actor", actorID, "tool", toolID)
		return nil, ErrToolOutOfService
	}

	// The type's required_qualification_id is the intra-module gate input
	// (resolved from Tool-owned tool_types). An EMPTY one means every
	// inspection.submit holder is eligible — no port call, no nil-port
	// requirement (mirrors the optional-qualification precedent).
	if tool.RequiredQualificationID != "" {
		if s.qualifications == nil {
			return nil, fmt.Errorf("tools core: qualification catalog port is not wired")
		}
		holds, err := s.qualifications.UserHoldsQualification(ctx, actorID, tool.RequiredQualificationID)
		if err != nil {
			return nil, fmt.Errorf("tools core: failed to resolve qualification eligibility: %w", err)
		}
		if !holds {
			// NFR-O1: a failed gate is logged STRUCTURED — never silent.
			s.log().Warn("inspection start denied: required qualification missing",
				"actor", actorID, "tool", toolID, "qualification", tool.RequiredQualificationID)
			return nil, ErrToolQualificationMissing
		}
	}

	// NFR-O1: an eligible start is audited best-effort (the audit write failure
	// is logged, never rolled back into the start).
	s.auditTool(ctx, actorID, AuditOperationInspectionStart, "action=start target=tool id="+toolID)

	return &InspectionStartResult{
		ToolID:         tool.ID,
		ToolName:       tool.Name,
		ToolTypeID:     tool.ToolTypeID,
		ToolTypeName:   tool.ToolTypeName,
		InspectionMode: tool.InspectionMode,
		ChecklistItems: tool.ChecklistItems,
	}, nil
}

// ============================================================================
// Inspection SUBMIT (Story 5.3, FR-12/FR-13/FR-14/AD-4/AD-5/AD-6/FR-11): the
// persistence seam that Stories 5.4/5.5 feed. Submit re-validates the tool
// type's required qualification server-side (never trusts the client, FR-11),
// validates the mode/result/notes/items contract, persists the inspection + its
// snapshot items transactionally (InspectionStore), audits inspection.submit
// and returns the persisted record + the shared derived status (AD-4/AD-5).
// ============================================================================

// Overall-result values for an inspection (FR-12/FR-13). Stored verbatim in
// inspections.overall_result / inspection_items.result.
const (
	InspectionResultPass = "pass"
	InspectionResultFail = "fail"
)

// Audit-operation tag for a persisted inspection submit (NFR-O1/NFR-O2). The
// record itself is the audit source of truth; this is the operation ledger row.
const AuditOperationInspectionSubmit = "inspection.submit"

// MaxInspectionNotesRunes bounds the inspection note (FR-13 "optional notes",
// 4000 runes — the spec's bound). It is SERVER-AUTHORITATIVE and counts RUNES;
// the SPA textarea maxLength is NOT a JS-identical mirror (JS maxLength counts
// UTF-16 code units, Go counts runes), so the server bound is the gate.
const MaxInspectionNotesRunes = 4000

// MaxReinstatementReasonRunes bounds the mandatory reinstatement reason
// (Story 5.6, FR-15/AD-9): ≤ 2000 runes, counted server-side (the spec's
// bound). The trimmed reason is what is validated AND persisted.
const MaxReinstatementReasonRunes = 2000

// ErrInspectionInvalid is the sentinel wrapping a German validation message
// for a 400 invalid_request (bad mode/result, over-long notes, checklist
// mismatch, unexpected items). Handlers match it to the uniform 400.
var ErrInspectionInvalid = errors.New("tools core: invalid inspection")

// InvalidInspectionError carries the German validation message for a 400
// invalid_request. It unwraps to ErrInspectionInvalid so callers can match the
// sentinel while still rendering the field-specific microcopy.
type InvalidInspectionError struct {
	Message string
}

func (e *InvalidInspectionError) Error() string { return e.Message }
func (e *InvalidInspectionError) Unwrap() error { return ErrInspectionInvalid }

// German microcopy for the inspection submit surface (FR-12/FR-13/UX-DR8).
const (
	MsgInspectionInvalid         = "Die Prüfung ist ungültig."
	MsgInspectionModeMismatch    = "Der Prüfmodus passt nicht zum Gerätetyp."
	MsgInspectionResultInvalid   = "Bitte wähle ein gültiges Prüfergebnis."
	MsgInspectionNotesTooLong    = "Die Anmerkung ist zu lang (maximal 4000 Zeichen)."
	MsgInspectionItemsMismatch   = "Die Checkliste ist unvollständig oder enthält unbekannte Punkte."
	MsgInspectionItemsUnexpected = "Pass/Fail-Prüfungen haben keine Checklistenpunkte."
	// MsgReinstatementReasonRequired is the 400 message for an empty/whitespace
	// reinstatement reason (FR-15/AD-9 — the reason is MANDATORY).
	MsgReinstatementReasonRequired = "Bitte gib einen Grund für die Wiederherstellung an."
	// MsgReinstatementReasonTooLong is the 400 message for a reason over
	// MaxReinstatementReasonRunes.
	MsgReinstatementReasonTooLong = "Der Grund ist zu lang (maximal 2000 Zeichen)."
	// MsgToolNotOutOfService is the 400 message when a reinstatement is
	// attempted on a tool that is NOT out of service (Story 5.6, FR-15/AD-9 —
	// reinstatement is the SOLE exit from OOS, so it is only meaningful for an
	// OOS tool; a serviceable tool is rejected).
	MsgToolNotOutOfService = "Das Gerät ist nicht außer Betrieb."
	// MsgToolReinstated is the German confirmation of a successful reinstatement.
	MsgToolReinstated = "Das Gerät wurde wiederhergestellt."
)

// Inspection is the domain representation of one persisted inspection record
// (FR-12/FR-13): the inspector + timestamp + mode + overall result + notes and
// the snapshotted per-item results (FR-12). Items is empty for pass_fail.
type Inspection struct {
	ID            string
	ToolID        string
	InspectorID   string
	Mode          string
	OverallResult string
	Notes         string
	SubmittedAt   time.Time
	Items         []InspectionItem
}

// InspectionItem is one snapshotted checklist result of an inspection (FR-12):
// the item's label + position are copied from the tool type's checklist at
// submit time (history stays self-contained even if the template later
// changes); ItemID is the plain (FK-less) reference for the DSGVO rewrite.
type InspectionItem struct {
	ID           string
	InspectionID string
	ItemID       string
	Label        string
	Position     int
	Result       string
}

// InspectionItemInput is one submitted checklist item (Story 5.3): the client
// only carries the type checklist item's id + the result; the LABEL + POSITION
// snapshot comes from the SERVER-side type checklist (never client text).
type InspectionItemInput struct {
	ItemID string `json:"item_id"`
	Result string `json:"result"`
}

// InspectionInput is the POST /api/v1/tools/{id}/inspection body (Story 5.3).
// For checklist mode, Items must EXACTLY equal the tool type's checklist (all
// answered, no extras); pass_fail mode REJECTS any provided item.
type InspectionInput struct {
	Mode   string                `json:"mode"`
	Result string                `json:"result"`
	Notes  string                `json:"notes"`
	Items  []InspectionItemInput `json:"items"`
}

// SubmitInspectionResult is the submit response: the persisted record plus the
// derived status (AD-4/AD-5) the SPA confirmation and the dashboard consume.
type SubmitInspectionResult struct {
	Inspection *Inspection
	Status     ToolStatus
}

// ToolInspectionStatus is the status-read input bundle (Story 5.3): the LATEST
// FAILED inspection's submitted_at (the OOS anchor, AD-4 — OOS is derived from
// the latest FAILED inspection not since reinstated; a PASS inspection does NOT
// clear it) plus the two clock anchors — the latest PASSING inspection's
// submitted_at and the latest reinstatement's created_at. The write path of
// reinstatements is Story 5.6; this read consumes the table.
type ToolInspectionStatus struct {
	LatestFailAt     *time.Time
	LastSuccessAt    *time.Time
	LastReinstatedAt *time.Time
}

// InspectionStore is the outbound persistence port over the Tool-owned
// inspections + inspection_items + reinstatements tables (AD-10/AD-11).
// InsertInspection persists an inspection AND its snapshot items in ONE
// transaction (a failed half-write never leaves a mixed item state). The
// records are immutable once persisted (no update path; history is
// append-only). GetToolInspectionStatus is the derived-status input read: the
// latest-fail + latest pass/reinstatement anchors (nil-safe).
type InspectionStore interface {
	InsertInspection(ctx context.Context, inspection *Inspection) (*Inspection, error)
	GetToolInspectionStatus(ctx context.Context, toolID string) (*ToolInspectionStatus, error)
	// InsertReinstatement persists one reinstatement row (Story 5.6, FR-15/AD-9):
	// tool, actor and the mandatory reason, created_at = DB now(). One
	// transaction-free single-row insert; the row immediately flips the derived
	// status (a fail before the latest reinstatement is not OOS).
	InsertReinstatement(ctx context.Context, toolID, actorID, reason string) error
	// ListInspectionsByTool reads the FULL inspection history of a tool (Story
	// 6.3, FR-18): every inspection row, reverse-chronological (submitted_at
	// DESC, id DESC — deterministic), EACH WITH its snapshotted ordered checklist
	// items (the repository fetches the items in one round-trip and groups them,
	// never an N+1). A tool with no inspections answers an EMPTY list, nil-safe.
	// The items read is part of this method — the Tool module never resolves
	// user tables (the inspector name resolves through the DisplayNameResolver
	// seam at the core, AD-8/AD-11).
	ListInspectionsByTool(ctx context.Context, toolID string) ([]*Inspection, error)
	// ListReinstatementsByTool reads the full reinstatement ledger of a tool
	// (Story 6.3, FR-18): every row, newest first (created_at DESC, id DESC —
	// deterministic). A tool with no reinstatements answers an EMPTY list,
	// nil-safe.
	ListReinstatementsByTool(ctx context.Context, toolID string) ([]*Reinstatement, error)
}

// SubmitInspection persists one inspection (SUBMIT_PASSFAIL / SUBMIT_CHECKLIST,
// FR-12/FR-13/FR-14/AD-4/AD-5): it re-checks `inspection.submit`
// defense-in-depth (AD-6), re-validates the tool-type qualification on submit
// (never trusts the client, FR-11), loads the tool + its type via
// GetToolWithTypeQualification (archived/unknown → ErrToolNotFound), validates
// the mode/result/notes/items contract and persists the inspection + its
// snapshot items transactionally. Audited (inspection.submit). Returns the
// persisted record + the shared derived status (OOS on a failed inspection).
//
// I/O matrix:
//   - SUBMIT_PASSFAIL / SUBMIT_CHECKLIST: valid body → 200 with the record +
//     derived status (Green when fresh; `oos` on a failed inspection).
//   - SUBMIT_GATED: caller lacks inspection.submit OR the tool's required
//     qualification → ErrForbidden / ErrToolQualificationMissing (403, no
//     record persisted).
//   - SUBMIT_ARCHIVED / SUBMIT_UNKNOWN: archived / nonexistent tool →
//     ErrToolNotFound (404).
//   - SUBMIT_INVALID: bad mode / bad result / notes > 4000 runes / checklist
//     mismatch (unanswered or extra item) / items on a pass_fail mode →
//     ErrInspectionInvalid (400, no record persisted).
func (s *Service) SubmitInspection(ctx context.Context, actorID, toolID string, input InspectionInput) (*SubmitInspectionResult, error) {
	if err := s.requireToolsPermission(ctx, actorID, []string{InspectionSubmitPermission}); err != nil {
		return nil, err
	}

	tool, err := s.store.GetToolWithTypeQualification(ctx, toolID)
	if err != nil {
		if errors.Is(err, ErrToolNotFound) {
			return nil, ErrToolNotFound
		}
		return nil, fmt.Errorf("tools core: failed to resolve tool for inspection: %w", err)
	}

	// OOS block (Story 5.6, FR-14/AD-4): an Out-of-Service tool is NOT
	// inspectable — the submit is rejected BEFORE the qualification re-check,
	// and nothing is persisted (an OOS tool never proceeds to a qualification
	// check or a write).
	if oos, err := s.toolIsOutOfService(ctx, toolID); err != nil {
		return nil, err
	} else if oos {
		s.log().Warn("inspection submit denied: tool out of service",
			"actor", actorID, "tool", toolID)
		return nil, ErrToolOutOfService
	}

	// Qualification re-check on submit (FR-11/AD-6): the client's eligibility
	// is never trusted — the 403 IS the gate, re-validated independently of the
	// start. An empty required qualification means every inspection.submit
	// holder is eligible.
	if tool.RequiredQualificationID != "" {
		if s.qualifications == nil {
			return nil, fmt.Errorf("tools core: qualification catalog port is not wired")
		}
		holds, err := s.qualifications.UserHoldsQualification(ctx, actorID, tool.RequiredQualificationID)
		if err != nil {
			return nil, fmt.Errorf("tools core: failed to resolve qualification eligibility: %w", err)
		}
		if !holds {
			s.log().Warn("inspection submit denied: required qualification missing",
				"actor", actorID, "tool", toolID, "qualification", tool.RequiredQualificationID)
			return nil, ErrToolQualificationMissing
		}
	}

	// Normalize the free-form fields ONCE, before validation: the trimmed
	// Result/Notes are what get validated AND persisted, so `" pass "` is a
	// valid pass and the notes rune-count measures the stored value.
	input.Result = strings.TrimSpace(input.Result)
	input.Notes = strings.TrimSpace(input.Notes)

	items, err := validateInspectionInput(input, tool)
	if err != nil {
		return nil, err
	}

	interval, err := s.resolveToolScheduleInterval(ctx, tool)
	if err != nil {
		return nil, err
	}

	submitted := &Inspection{
		ToolID:        tool.ID,
		InspectorID:   actorID,
		Mode:          input.Mode,
		OverallResult: input.Result,
		Notes:         input.Notes,
		Items:         items,
	}
	persisted, err := s.store.InsertInspection(ctx, submitted)
	if err != nil {
		return nil, fmt.Errorf("tools core: failed to persist inspection: %w", err)
	}

	s.auditTool(ctx, actorID, AuditOperationInspectionSubmit, "action=submit target=tool id="+toolID)

	// Derive the status best-effort AFTER the record committed: a failed status
	// read must NOT surface as an error (a client retry would duplicate the
	// already-committed record). Log the failure and derive from the persisted
	// record alone — a pass anchors the clock at its submitted_at (green when
	// fresh), a fail reads as `oos` (its submitted_at is the latest fail and no
	// reinstatement is known to follow).
	var status ToolStatus
	statusInput, err := s.store.GetToolInspectionStatus(ctx, toolID)
	if err != nil {
		s.log().Warn("tools core: inspection status read failed after commit; deriving from the record alone",
			"tool", toolID, "error", err)
		var latestFailAt, lastSuccessAt *time.Time
		if persisted.OverallResult == InspectionResultFail {
			t := persisted.SubmittedAt
			latestFailAt = &t
		} else {
			t := persisted.SubmittedAt
			lastSuccessAt = &t
		}
		status = deriveToolStatus(latestFailAt, lastSuccessAt, nil, interval, time.Now())
	} else {
		status = deriveToolStatus(statusInput.LatestFailAt, statusInput.LastSuccessAt, statusInput.LastReinstatedAt,
			interval, time.Now())
	}

	return &SubmitInspectionResult{Inspection: persisted, Status: status}, nil
}

// ReinstateResult is the reinstate response (Story 5.6, FR-15/AD-9): the newly
// derived status. Reinstatement resets the clock, so the derivation answers
// not-OOS with `next_due = lastReinstatedAt + interval`.
type ReinstateResult struct {
	Status ToolStatus
}

// Reinstatement is the history-surface READ shape for one reinstatement ledger
// row (Story 6.3, FR-18): the actor + the mandatory reason + created_at. It is
// a read-side DTO type only — the 5.6 write path persists reinstatements
// through the store's InsertReinstatement(toolID, actorID, reason) and the
// status derivation consults the latest created_at directly; neither uses this
// type. The Tool module never resolves the actor's user table (AD-8/AD-11);
// the display name resolves through the DisplayNameResolver seam at the core.
type Reinstatement struct {
	ID        string
	ToolID    string
	ActorID   string
	Reason    string
	CreatedAt time.Time
}

// ============================================================================
// Tool history (Story 6.3, FR-18/AD-6/AD-8): the per-tool audit trail of
// inspections + reinstatements, newest first. The endpoint is gated by
// `inspection.history.view` — the SERVER is the gate (a non-holder answers the
// uniform 403 with no data; the SPA skips the fetch as a courtesy only). The
// inspector/actor display names resolve through the narrow DisplayNameResolver
// seam in ONE bulk call (no N+1 user reads — the Tool module never joins user
// tables, AD-8/AD-11); a missing user row maps to the literal "Deleted User"
// (Story 3.4 forward-compat).
// ============================================================================

// ToolHistoryInspection is one inspection row of the history payload: the
// inspector display name (resolved through the seam) + the record's fields +
// the snapshotted ordered checklist items (empty for pass_fail).
type ToolHistoryInspection struct {
	ID            string
	InspectorID   string
	InspectorName string
	Mode          string
	OverallResult string
	Notes         string
	SubmittedAt   time.Time
	Items         []InspectionItem
}

// ToolHistoryReinstatement is one reinstatement row of the history payload: the
// actor display name (resolved through the seam) + the record's fields.
type ToolHistoryReinstatement struct {
	ID        string
	ActorID   string
	ActorName string
	Reason    string
	CreatedAt time.Time
}

// ToolHistory is the ListInspectionHistory response: the two newest-first lists
// (inspections submitted_at DESC/id DESC; reinstatements created_at DESC/id
// DESC). Empty lists serialize as `[]`, never null.
type ToolHistory struct {
	Inspections    []*ToolHistoryInspection
	Reinstatements []*ToolHistoryReinstatement
}

// toolIsOutOfService reports whether a tool is currently Out of Service (Story
// 5.6, FR-14/AD-4): OOS iff the LATEST FAILED inspection's submitted_at is
// at-or-after the latest reinstatement (or none exists) — the same rule the
// derived-status function applies, but the CHEAP check needs only ONE
// GetToolInspectionStatus read (no schedule resolution). This is the
// not-inspectable gate for start/submit.
func (s *Service) toolIsOutOfService(ctx context.Context, toolID string) (bool, error) {
	status, err := s.store.GetToolInspectionStatus(ctx, toolID)
	if err != nil {
		return false, fmt.Errorf("tools core: failed to resolve tool inspection status: %w", err)
	}
	if status == nil {
		return false, nil
	}
	return status.LatestFailAt != nil && (status.LastReinstatedAt == nil || !status.LatestFailAt.Before(*status.LastReinstatedAt)), nil
}

// ReinstateTool reinstates an OOS tool (Story 5.6, FR-15/AD-9/AD-5): it
// re-checks `tool.reinstate` defense-in-depth (AD-6), loads the tool
// (missing/archived → ErrToolNotFound), rejects a NON-OOS tool (400 German —
// reinstatement is the SOLE exit from OOS, so it is only meaningful for an OOS
// tool, REINSTATE_NOT_OOS), validates the mandatory reason (trimmed non-empty,
// ≤ 2000 runes → ErrInspectionInvalid 400), persists the reinstatement, audits
// `tool.reinstate` and returns the newly derived status. Reinstatement resets
// the clock — `next_due = lastReinstatedAt + interval` (AD-5).
//
// I/O matrix:
//   - REINSTATE_OK: OOS holder + valid reason → 200 with the not-OOS derived status.
//   - REINSTATE_NOT_OOS: tool is NOT out of service → ErrInspectionInvalid (400,
//     MsgToolNotOutOfService), no write.
//   - REINSTATE_GATED: caller lacks tool.reinstate → ErrForbidden (403, no write).
//   - REINSTATE_ARCHIVED / REINSTATE_UNKNOWN: archived / nonexistent tool →
//     ErrToolNotFound (404).
//   - REINSTATE_EMPTY / REINSTATE_LONG: empty or > 2000-rune reason →
//     ErrInspectionInvalid (400, no write).
func (s *Service) ReinstateTool(ctx context.Context, actorID, toolID, reason string) (*ReinstateResult, error) {
	if err := s.requireToolsPermission(ctx, actorID, []string{ToolReinstatePermission}); err != nil {
		return nil, err
	}

	tool, err := s.store.GetToolWithTypeQualification(ctx, toolID)
	if err != nil {
		if errors.Is(err, ErrToolNotFound) {
			return nil, ErrToolNotFound
		}
		return nil, fmt.Errorf("tools core: failed to resolve tool for reinstatement: %w", err)
	}

	// OOS precondition (FR-15/AD-9): reinstatement is the SOLE exit from OOS,
	// so it is only meaningful for an OOS tool. A serviceable tool is rejected
	// with a German 400 and NOTHING is written — a redundant/erroneous
	// reinstatement must never advance the clock or add a ledger row.
	oos, err := s.toolIsOutOfService(ctx, toolID)
	if err != nil {
		return nil, err
	}
	if !oos {
		s.log().Warn("reinstatement rejected: tool not out of service",
			"actor", actorID, "tool", toolID)
		return nil, &InvalidInspectionError{Message: MsgToolNotOutOfService}
	}

	// The reason is MANDATORY (FR-15/AD-9): trimmed ONCE before validation, the
	// trimmed value is what is validated AND persisted. Nothing is written on a
	// validation failure.
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, &InvalidInspectionError{Message: MsgReinstatementReasonRequired}
	}
	if utf8.RuneCountInString(reason) > MaxReinstatementReasonRunes {
		return nil, &InvalidInspectionError{Message: MsgReinstatementReasonTooLong}
	}

	if err := s.store.InsertReinstatement(ctx, tool.ID, actorID, reason); err != nil {
		return nil, fmt.Errorf("tools core: failed to persist reinstatement: %w", err)
	}

	s.auditTool(ctx, actorID, AuditOperationToolReinstate, "action=reinstate target=tool id="+tool.ID)

	// Derive the new status (AD-4/AD-5) BEST-EFFORT after the row committed: a
	// schedule/status resolution failure AFTER the write must NOT surface as an
	// error (a client retry would DUPLICATE the reinstatement). Log the failure
	// and answer a conservative NON-OOS status — the row committed, so the tool
	// is out of OOS (green with nil next_due; the SPA refetches the real status
	// from the dashboard list). The row is the audit source of truth regardless.
	interval, err := s.resolveToolScheduleInterval(ctx, tool)
	if err != nil {
		s.log().Warn("tools core: schedule resolution failed after reinstatement; answering conservative non-OOS",
			"tool", tool.ID, "error", err)
		return &ReinstateResult{Status: ToolStatus{Status: ToolStatusCodeGreen}}, nil
	}
	statusInput, err := s.store.GetToolInspectionStatus(ctx, tool.ID)
	if err != nil {
		s.log().Warn("tools core: status read failed after reinstatement; answering conservative non-OOS",
			"tool", tool.ID, "error", err)
		return &ReinstateResult{Status: ToolStatus{Status: ToolStatusCodeGreen}}, nil
	}
	if statusInput == nil {
		statusInput = &ToolInspectionStatus{}
	}
	status := deriveToolStatus(statusInput.LatestFailAt, statusInput.LastSuccessAt, statusInput.LastReinstatedAt,
		interval, time.Now())
	return &ReinstateResult{Status: status}, nil
}

// ListInspectionHistory returns the per-tool audit trail (Story 6.3, FR-18):
// every inspection (newest first, each naming the inspector + timestamp +
// outcome + notes + mode + the snapshotted per-checklist-item results) and
// every reinstatement (newest first, actor + reason). It re-checks
// `inspection.history.view` defense-in-depth (AD-6), loads the tool
// (missing/archived → ErrToolNotFound 404 German), reads both lists and
// resolves ALL inspector/actor display names in ONE DisplayNameResolver call —
// a missing user row maps to the literal "Deleted User", never a 404 or an
// empty string (Story 3.4 forward-compat, AD-8).
//
// I/O matrix:
//   - HIST_OK: `inspection.history.view` holder + existing tool → 200 with both
//     newest-first lists.
//   - HIST_EMPTY: tool with no records → 200 with empty arrays (no resolver
//     call — there are no user ids to resolve).
//   - HIST_GATED: caller lacks inspection.history.view → ErrForbidden (403, no
//     data exposed, AD-6).
//   - HIST_UNKNOWN / HIST_ARCHIVED: missing or archived tool id →
//     ErrToolNotFound (404 German).
//   - HIST_DELETED_USER: an inspector/actor id with no user row → that name
//     renders "Deleted User".
func (s *Service) ListInspectionHistory(ctx context.Context, actorID, toolID string) (*ToolHistory, error) {
	if err := s.requireToolsPermission(ctx, actorID, []string{InspectionHistoryViewPermission}); err != nil {
		return nil, err
	}

	// The tool-exists check (HIST_UNKNOWN / HIST_ARCHIVED → 404 German): the
	// history reads run only for a known ACTIVE tool. The header itself is the
	// dashboard's job (the SPA resolves it from the dashboard list); this
	// endpoint only needs the existence gate.
	if _, err := s.store.GetToolWithTypeQualification(ctx, toolID); err != nil {
		if errors.Is(err, ErrToolNotFound) {
			return nil, ErrToolNotFound
		}
		return nil, fmt.Errorf("tools core: failed to resolve tool for history: %w", err)
	}

	inspections, err := s.store.ListInspectionsByTool(ctx, toolID)
	if err != nil {
		return nil, fmt.Errorf("tools core: failed to list inspection history: %w", err)
	}
	reinstatements, err := s.store.ListReinstatementsByTool(ctx, toolID)
	if err != nil {
		return nil, fmt.Errorf("tools core: failed to list reinstatement history: %w", err)
	}

	// Collect the user ids ONCE across both lists and resolve the display names
	// in ONE seam call (no N+1 user reads). An EMPTY set skips the seam (the
	// resolver is only needed when there is something to name) — a nil
	// (unwired) resolver never fails an empty-history read.
	names := map[string]string{}
	ids := make([]string, 0, len(inspections)+len(reinstatements))
	seen := make(map[string]struct{}, len(inspections)+len(reinstatements))
	addID := func(id string) {
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for _, insp := range inspections {
		addID(insp.InspectorID)
	}
	for _, r := range reinstatements {
		addID(r.ActorID)
	}
	if len(ids) > 0 {
		if s.displayNames == nil {
			return nil, fmt.Errorf("tools core: display-name resolver is not wired")
		}
		resolved, err := s.displayNames.ResolveDisplayNames(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("tools core: failed to resolve display names: %w", err)
		}
		for id, name := range resolved {
			names[id] = name
		}
	}

	out := &ToolHistory{
		Inspections:    make([]*ToolHistoryInspection, 0, len(inspections)),
		Reinstatements: make([]*ToolHistoryReinstatement, 0, len(reinstatements)),
	}
	for _, insp := range inspections {
		// HIST_DELETED_USER: a missing user row leaves the name map without a
		// key → the literal "Deleted User" (never a 404, never an empty string).
		name := DeletedUserDisplayName
		if n, ok := names[insp.InspectorID]; ok && n != "" {
			name = n
		}
		out.Inspections = append(out.Inspections, &ToolHistoryInspection{
			ID:            insp.ID,
			InspectorID:   insp.InspectorID,
			InspectorName: name,
			Mode:          insp.Mode,
			OverallResult: insp.OverallResult,
			Notes:         insp.Notes,
			SubmittedAt:   insp.SubmittedAt,
			Items:         insp.Items,
		})
	}
	for _, r := range reinstatements {
		name := DeletedUserDisplayName
		if n, ok := names[r.ActorID]; ok && n != "" {
			name = n
		}
		out.Reinstatements = append(out.Reinstatements, &ToolHistoryReinstatement{
			ID:        r.ID,
			ActorID:   r.ActorID,
			ActorName: name,
			Reason:    r.Reason,
			CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}

// resolveToolScheduleInterval resolves the tool's EFFECTIVE inspection
// schedule interval (AD-5/AD-16): the per-tool override when set, else the
// tool type's default — both plain FK ids resolved through the Admin module's
// read-only SchedulesPort (the Tool module never joins the Admin tables,
// AD-8/AD-11). A nil port is a composition-root wiring defect and FAILS
// LOUDLY; an effective schedule missing from the ACTIVE catalog (archived /
// vanished) is the same — the clock must never silently resolve to a wrong
// interval. The catalog is read here and the pure scheduleIntervalFor helper
// resolves the effective interval against that snapshot.
func (s *Service) resolveToolScheduleInterval(ctx context.Context, tool *ToolWithTypeQualification) (time.Duration, error) {
	if s.schedules == nil {
		return 0, fmt.Errorf("tools core: schedule catalog port is not wired")
	}
	schedules, err := s.schedules.CurrentSchedules(ctx)
	if err != nil {
		return 0, fmt.Errorf("tools core: failed to resolve schedule catalog: %w", err)
	}
	return scheduleIntervalFor(tool.ScheduleID, tool.DefaultScheduleID, schedules)
}

// scheduleIntervalFor resolves a tool's EFFECTIVE schedule interval
// (AD-5/AD-16) against a catalog snapshot: the per-tool OVERRIDE when set,
// else the tool type's DEFAULT — both plain FK ids. It is the PURE helper
// shared by the inspection submit path (resolveToolScheduleInterval) and the
// resilient dashboard read (ListToolsForDashboard, Story 6.1) — the dashboard
// resolves the catalog ONCE per call and feeds this helper per tool (no N+1
// catalog reads, the spec's invariant). A missing/invalid effective schedule
// surfaces as an error: the submit path FAILS LOUDLY, while the dashboard
// renders that single tool `red` (never falsely serviceable).
func scheduleIntervalFor(overrideID, defaultID string, schedules []*admcore.Schedule) (time.Duration, error) {
	scheduleID := overrideID
	if scheduleID == "" {
		scheduleID = defaultID
	}
	for _, sch := range schedules {
		if sch.ID == scheduleID {
			interval := scheduleInterval(sch.IntervalUnit, sch.IntervalMagnitude)
			if interval <= 0 {
				return 0, fmt.Errorf("tools core: effective schedule %q has an invalid interval", scheduleID)
			}
			return interval, nil
		}
	}
	return 0, fmt.Errorf("tools core: effective schedule %q not in the active catalog", scheduleID)
}

// validateInspectionInput enforces the submit contract (SUBMIT_INVALID):
// the mode must match the tool type's, the overall result pass|fail, the notes
// ≤ 4000 runes, and the checklist item set must EXACTLY equal the type's
// checklist (all answered, no extras, each pass|fail — matched by item id,
// with the LABEL + POSITION snapshot taken server-side from the type). A
// pass_fail mode REJECTS any provided items. Nothing is persisted on a
// validation failure.
func validateInspectionInput(input InspectionInput, tool *ToolWithTypeQualification) ([]InspectionItem, error) {
	if input.Mode != tool.InspectionMode {
		return nil, &InvalidInspectionError{Message: MsgInspectionModeMismatch}
	}
	if input.Result != InspectionResultPass && input.Result != InspectionResultFail {
		return nil, &InvalidInspectionError{Message: MsgInspectionResultInvalid}
	}
	if utf8.RuneCountInString(input.Notes) > MaxInspectionNotesRunes {
		return nil, &InvalidInspectionError{Message: MsgInspectionNotesTooLong}
	}

	if input.Mode == InspectionModePassFail {
		if len(input.Items) != 0 {
			return nil, &InvalidInspectionError{Message: MsgInspectionItemsUnexpected}
		}
		return nil, nil
	}

	// Checklist mode: build an index of the submitted items by item id; the
	// set must exactly equal the type's ordered checklist. An EMPTY type
	// checklist (a retired/misconfigured template) rejects the submit — an
	// empty-snapshot inspection must not bypass the checklist contract. An
	// oversized body is rejected up-front (fail-fast, before the map build).
	if len(tool.ChecklistItems) == 0 {
		return nil, &InvalidInspectionError{Message: MsgInspectionItemsMismatch}
	}
	if len(input.Items) > len(tool.ChecklistItems) {
		return nil, &InvalidInspectionError{Message: MsgInspectionItemsMismatch}
	}
	submitted := make(map[string]string, len(input.Items))
	for _, item := range input.Items {
		if item.Result != InspectionResultPass && item.Result != InspectionResultFail {
			return nil, &InvalidInspectionError{Message: MsgInspectionResultInvalid}
		}
		submitted[strings.TrimSpace(item.ItemID)] = item.Result
	}
	items := make([]InspectionItem, 0, len(tool.ChecklistItems))
	for _, typeItem := range tool.ChecklistItems {
		result, ok := submitted[typeItem.ID]
		if !ok {
			return nil, &InvalidInspectionError{Message: MsgInspectionItemsMismatch}
		}
		items = append(items, InspectionItem{
			ItemID:   typeItem.ID,
			Label:    typeItem.Label,
			Position: typeItem.Position,
			Result:   result,
		})
	}
	if len(items) != len(input.Items) {
		return nil, &InvalidInspectionError{Message: MsgInspectionItemsMismatch}
	}
	return items, nil
}
