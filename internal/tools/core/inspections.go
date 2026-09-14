package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
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

// ToolInspectionStatus is the status-read input bundle (Story 5.3): the latest
// inspection (with its snapshot items) plus the two clock anchors — the latest
// PASSING inspection's submitted_at and the latest reinstatement's created_at.
// The write path of reinstatements is Story 5.6; this read consumes the table.
type ToolInspectionStatus struct {
	Latest           *Inspection
	LastSuccessAt    *time.Time
	LastReinstatedAt *time.Time
}

// InspectionStore is the outbound persistence port over the Tool-owned
// inspections + inspection_items + reinstatements tables (AD-10/AD-11).
// InsertInspection persists an inspection AND its snapshot items in ONE
// transaction (a failed half-write never leaves a mixed item state). The
// records are immutable once persisted (no update path; history is
// append-only). GetToolInspectionStatus is the derived-status input read: the
// latest inspection + the latest pass/reinstatement anchors (nil-safe).
type InspectionStore interface {
	InsertInspection(ctx context.Context, inspection *Inspection) (*Inspection, error)
	GetToolInspectionStatus(ctx context.Context, toolID string) (*ToolInspectionStatus, error)
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
	// record alone — a pass reads as green-from-submittedAt, a fail as `oos`.
	var status ToolStatus
	statusInput, err := s.store.GetToolInspectionStatus(ctx, toolID)
	if err != nil {
		s.log().Warn("tools core: inspection status read failed after commit; deriving from the record alone",
			"tool", toolID, "error", err)
		// Best-effort from the persisted record alone: a pass anchors the clock
		// at its own submitted_at (→ green when fresh); a fail is OOS (its
		// submitted_at is the latest and no reinstatement is known to follow).
		var lastSuccessAt *time.Time
		if persisted.OverallResult == InspectionResultPass {
			t := persisted.SubmittedAt
			lastSuccessAt = &t
		}
		status = deriveToolStatus(persisted, lastSuccessAt, nil, interval, time.Now(), OrangeWindowDays)
	} else {
		status = deriveToolStatus(statusInput.Latest, statusInput.LastSuccessAt, statusInput.LastReinstatedAt,
			interval, time.Now(), OrangeWindowDays)
	}

	return &SubmitInspectionResult{Inspection: persisted, Status: status}, nil
}

// resolveToolScheduleInterval resolves the tool's EFFECTIVE inspection
// schedule interval (AD-5/AD-16): the per-tool override when set, else the
// tool type's default — both plain FK ids resolved through the Admin module's
// read-only SchedulesPort (the Tool module never joins the Admin tables,
// AD-8/AD-11). A nil port is a composition-root wiring defect and FAILS
// LOUDLY; an effective schedule missing from the ACTIVE catalog (archived /
// vanished) is the same — the clock must never silently resolve to a wrong
// interval.
func (s *Service) resolveToolScheduleInterval(ctx context.Context, tool *ToolWithTypeQualification) (time.Duration, error) {
	if s.schedules == nil {
		return 0, fmt.Errorf("tools core: schedule catalog port is not wired")
	}
	scheduleID := tool.ScheduleID
	if scheduleID == "" {
		scheduleID = tool.DefaultScheduleID
	}
	schedules, err := s.schedules.CurrentSchedules(ctx)
	if err != nil {
		return 0, fmt.Errorf("tools core: failed to resolve schedule catalog: %w", err)
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
