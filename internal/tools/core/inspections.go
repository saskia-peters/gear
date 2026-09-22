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

// ReportExportPermission is the server-authoritative gate code for the status
// report export surface (Story 6.2, FR-17/AD-6): `GET /api/v1/tools/report.pdf`.
// Only Fuehrung/Admin holders (the base roles seed report.export) reach it —
// the PDF is a SHAREABLE artifact of the dashboard's current view and must not
// leak to non-holders (a non-holder answers the uniform 403 with NO PDF bytes,
// AD-6). One Go const so the route mount, the core re-check and the
// SPA-facing documentation never drift.
const ReportExportPermission = "report.export"

// DeletedUserDisplayName is the literal name shown for an inspector/actor whose
// user account no longer exists (Story 3.4 DSGVO forward-compat, AD-8): the
// plain FK-less inspector_id/actor_id resolves through the DisplayNameResolver
// seam and a MISSING user row maps to this literal — never a 404, never an
// empty string.
const DeletedUserDisplayName = "Deleted User"

// DeletedUserID is the canonical well-known sentinel uuid the DSGVO account
// deletion rewrites an erased user's inspection/reinstatement references to
// (Story 3.4, FR-24/AD-8): the Tool module's AnonymizeUserReferences UPDATEs
// `inspections.inspector_id` / `reinstatements.actor_id` (plain FK-less uuids,
// AD-8/3.4) to this fixed value in ONE transaction. It is deliberately NOT a
// real user row — the DisplayNameResolver seam maps the absent id to the
// literal DeletedUserDisplayName, so inspection history/status reports render
// "Deleted User" with every timestamp/result/item/OOS state intact. Fixed and
// well-known so the rewrite is explicit and auditable (never a random uuid).
const DeletedUserID = "00000000-0000-0000-0000-00000000dead"

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
	// MsgIdempotencyKeyInvalid is the 400 message for a MISSING or non-UUID
	// idempotency_key on the inspection submit / reinstatement bodies (Story
	// 7.5, NFR-R1): the at-most-once guard REQUIRES the client-supplied key, so
	// a request without a parseable UUID fails fast — the guard must never be
	// silently bypassable.
	MsgIdempotencyKeyInvalid = "Der Idempotenzschlüssel fehlt oder ist ungültig."
)

// Inspection is the domain representation of one persisted inspection record
// (FR-12/FR-13): the inspector + timestamp + mode + overall result + notes and
// the snapshotted per-item results (FR-12). Items is empty for pass_fail.
// IdempotencyKey is the CLIENT-SUPPLIED at-most-once key (Story 7.5) — carried
// on the write and read back so a replay round-trips the committed record
// exactly.
type Inspection struct {
	ID             string
	ToolID         string
	InspectorID    string
	Mode           string
	OverallResult  string
	Notes          string
	IdempotencyKey string
	SubmittedAt    time.Time
	Items          []InspectionItem
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

// LatestInspection is the status-report read input (Story 6.2, FR-17): the
// LATEST inspection of a tool (ANY result — pass OR fail) → its submitted_at
// (the "Zuletzt geprüft" column) + the inspector_id (the "Prüfer/in" column,
// resolved through the DisplayNameResolver seam at the core, AD-8). A tool with
// no inspections answers nil (the PDF renders "–" in both columns).
type LatestInspection struct {
	SubmittedAt time.Time
	InspectorID string
}

// InspectionStore is the outbound persistence port over the Tool-owned
// inspections + inspection_items + reinstatements tables (AD-10/AD-11).
// InsertInspection persists an inspection AND its snapshot items in ONE
// transaction (a failed half-write never leaves a mixed item state). The
// records are immutable once persisted (no update path; history is
// append-only). GetToolInspectionStatus is the derived-status input read: the
// latest-fail + latest pass/reinstatement anchors (nil-safe).
type InspectionStore interface {
	// InsertInspection persists an inspection AND its snapshot items in ONE
	// transaction, with the CLIENT-SUPPLIED idempotency_key (Story 7.5,
	// NFR-R1): the DB UNIQUE (tool_id, idempotency_key) makes the write
	// at-most-once — a retried / concurrent duplicate is absorbed by ON
	// CONFLICT DO NOTHING and answered by REPLAYING the already-persisted
	// record (row + items), never an error. The returned `replayed` flag
	// signals an ABSORBED replay (the DB deduped a retry/race; the repository
	// fetched the winner's record) vs a FRESH insert — the core never
	// re-audits an absorbed replay.
	InsertInspection(ctx context.Context, inspection *Inspection, idempotencyKey string) (*Inspection, bool, error)
	// FindInspectionByToolAndKey is the EARLY replay lookup (Story 7.5): the
	// existing record (WITH its ordered snapshot items) for a tool + the
	// client-supplied idempotency key, or (nil, nil) when none matches. The
	// core calls it right after the permission gate, BEFORE the tool load and
	// the OOS/qualification gates, so a retried submit of an already-committed
	// FAIL inspection (the tool is now OOS, or archived between commit and
	// retry) replays 200 instead of the OOS 403 / the 404.
	FindInspectionByToolAndKey(ctx context.Context, toolID, idempotencyKey string) (*Inspection, error)
	GetToolInspectionStatus(ctx context.Context, toolID string) (*ToolInspectionStatus, error)
	// InsertReinstatement persists one reinstatement row (Story 5.6, FR-15/AD-9):
	// tool, actor and the mandatory reason, created_at = DB now(). One
	// transaction-free single-row insert; the row immediately flips the derived
	// status (a fail before the latest reinstatement is not OOS). The
	// CLIENT-SUPPLIED idempotency_key (Story 7.5) makes the write at-most-once —
	// a retried / concurrent duplicate is absorbed by ON CONFLICT DO NOTHING
	// (the repository then returns the already-persisted row + `replayed=true`,
	// never an error). The returned row is the replay signal: non-nil + true =
	// the insert was absorbed; the core never re-audits an absorbed replay.
	InsertReinstatement(ctx context.Context, toolID, actorID, reason, idempotencyKey string) (*Reinstatement, bool, error)
	// FindReinstatementByToolAndKey is the EARLY replay lookup for the
	// reinstatement write (Story 7.5): the existing reinstatement for a tool +
	// the client-supplied idempotency key, or (nil, nil) when none matches. The
	// core calls it right after the permission gate, BEFORE the tool load and
	// the OOS precondition, so a retried reinstate of an already-committed row
	// (which flipped the tool serviceable) replays 200 instead of the NOT-OOS
	// 400 / the 404.
	FindReinstatementByToolAndKey(ctx context.Context, toolID, idempotencyKey string) (*Reinstatement, error)
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
	// GetLatestInspectionByTool reads the LATEST inspection of a tool (ANY
	// result — Story 6.2, FR-17): the report's "Zuletzt geprüft" + "Prüfer/in"
	// inputs. The submitted_at DESC, id DESC tiebreak is deterministic (the
	// 000027 index inspections_tool_id_submitted_at_idx serves it). A tool with
	// no inspections answers (nil, nil) — never an error (the report renders
	// "–").
	GetLatestInspectionByTool(ctx context.Context, toolID string) (*LatestInspection, error)
	// ListInspectionsByInspector reads every inspection the user performed as
	// inspector (Story 3.3 DSGVO export, FR-24): newest first (submitted_at
	// DESC, id DESC — deterministic, served by the 000029 index
	// inspections_inspector_id_idx), EACH WITH its snapshotted ordered
	// checklist items (the repository fetches the items in one grouped
	// round-trip, mirroring the history surface — no N+1). A user with no
	// inspections answers an EMPTY list, nil-safe.
	ListInspectionsByInspector(ctx context.Context, userID string) ([]*Inspection, error)
	// ListReinstatementsByActor reads every reinstatement the user performed as
	// actor (Story 3.3 DSGVO export, FR-24): newest first (created_at DESC, id
	// DESC — served by the 000029 index reinstatements_actor_id_idx). A user
	// with no reinstatements answers an EMPTY list, nil-safe.
	ListReinstatementsByActor(ctx context.Context, userID string) ([]*Reinstatement, error)
	// AnonymizeUserReferences rewrites every reference to a deleted user's
	// account to the canonical DeletedUserID sentinel (Story 3.4, FR-24/AD-8):
	// `UPDATE inspections SET inspector_id = $sentinel WHERE inspector_id = $user`
	// AND `UPDATE reinstatements SET actor_id = $sentinel WHERE actor_id = $user`,
	// in ONE transaction. IDEMPOTENT: a user with no inspection/reinstatement
	// rows (or an already-anonymized one) is a no-op. The sentinel is absent
	// from users, so the DisplayNameResolver seam renders "Deleted User" while
	// every timestamp/result/item/OOS state stays intact. This is the SOLE
	// write seam of the DSGVO deletion flow the Tool module owns.
	AnonymizeUserReferences(ctx context.Context, userID string) error
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
// The CLIENT-SUPPLIED idempotencyKey makes the write at-most-once (Story 7.5,
// NFR-R1): the key rides on the insert and is unique per tool. An EXISTING
// record for (toolID, idempotencyKey) is a REPLAY — the service returns the
// already-persisted record + the same post-commit derived status, skipping the
// tool load, OOS/qualification/validation gates and the insert (no re-audit, no
// second row). The replay check runs EARLY — right after the permission gate,
// BEFORE the tool load and the OOS gate — so a retried submit of an
// already-committed FAIL inspection (the tool is now OOS) replays 200 (never
// the OOS 403) and an archived tool (archived between commit and retry) replays
// 200 (never the 404). A CONCURRENT_RACE that slips past the early check is
// absorbed by the DB UNIQUE constraint; the repository reports `replayed` and
// the core treats it identically (no re-audit).
//
// I/O matrix:
//   - SUBMIT_PASSFAIL / SUBMIT_CHECKLIST: valid body → 200 with the record +
//     derived status (Green when fresh; `oos` on a failed inspection).
//   - REPLAY: same tool + key, first attempt already committed → 200 with the
//     EXISTING record + derived status, no second row — even when the tool is
//     now OOS or archived.
//   - SUBMIT_GATED: caller lacks inspection.submit OR the tool's required
//     qualification → ErrForbidden / ErrToolQualificationMissing (403, no
//     record persisted).
//   - SUBMIT_ARCHIVED / SUBMIT_UNKNOWN: archived / nonexistent tool →
//     ErrToolNotFound (404).
//   - SUBMIT_INVALID: bad mode / bad result / notes > 4000 runes / checklist
//     mismatch (unanswered or extra item) / items on a pass_fail mode →
//     ErrInspectionInvalid (400, no record persisted).
func (s *Service) SubmitInspection(ctx context.Context, actorID, toolID string, input InspectionInput, idempotencyKey string) (*SubmitInspectionResult, error) {
	// The idempotency key is REQUIRED and must be a parseable UUID (Story 7.5,
	// defense-in-depth): the HTTP adapter validates + normalizes first; this is
	// the domain backstop so a direct caller can never reach the store with an
	// empty/malformed key (which would surface an opaque internal error).
	if !validIdempotencyKey(idempotencyKey) {
		return nil, &InvalidInspectionError{Message: MsgIdempotencyKeyInvalid}
	}

	if err := s.requireToolsPermission(ctx, actorID, []string{InspectionSubmitPermission}); err != nil {
		return nil, err
	}

	// EARLY REPLAY CHECK (Story 7.5): an already-committed record for this tool
	// + key is a retried submit — return the existing record + the same
	// post-commit derived status. It runs BEFORE the tool load so an archived
	// tool (archived between commit and retry) can NEVER 404 a replay. A replay
	// NEVER re-audits and NEVER double-inserts.
	existing, err := s.store.FindInspectionByToolAndKey(ctx, toolID, idempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("tools core: failed to resolve inspection replay: %w", err)
	}
	if existing != nil {
		s.log().Info("inspection submit replayed",
			"actor", actorID, "tool", toolID, "key", idempotencyKey)
		return &SubmitInspectionResult{Inspection: existing, Status: s.replayInspectionStatus(ctx, toolID, existing)}, nil
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
		ToolID:         tool.ID,
		InspectorID:    actorID,
		Mode:           input.Mode,
		OverallResult:  input.Result,
		Notes:          input.Notes,
		IdempotencyKey: idempotencyKey,
		Items:          items,
	}
	persisted, replayed, err := s.store.InsertInspection(ctx, submitted, idempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("tools core: failed to persist inspection: %w", err)
	}
	// A CONCURRENT_RACE (Story 7.5): the DB unique constraint absorbed the
	// duplicate (ON CONFLICT DO NOTHING → the repository replayed the winner's
	// row). From the client's perspective this is a REPLAY — NEVER re-audit,
	// and derive exactly like the early-replay path (an absorbed request must
	// not add an audit row for a write it did not perform).
	if replayed {
		s.log().Info("inspection submit replayed (concurrent)",
			"actor", actorID, "tool", toolID, "key", idempotencyKey)
		return &SubmitInspectionResult{Inspection: persisted, Status: s.replayInspectionStatus(ctx, toolID, persisted)}, nil
	}

	s.auditTool(ctx, actorID, AuditOperationInspectionSubmit, "action=submit target=tool id="+toolID)

	// Derive the status best-effort AFTER the record committed: a failed status
	// read must NOT surface as an error (a client retry would duplicate the
	// already-committed record). Log the failure and derive from the persisted
	// record alone — a pass anchors the clock at its submitted_at (green when
	// fresh), a fail reads as `oos` (its submitted_at is the latest fail and no
	// reinstatement is known to follow).
	return &SubmitInspectionResult{Inspection: persisted, Status: s.deriveInspectionStatus(ctx, toolID, persisted, interval)}, nil
}

// deriveInspectionStatus derives the status AFTER an inspection record exists
// (AD-4/AD-5), best-effort: a post-commit status-read failure is logged and
// the status is derived from the persisted record alone — a client retry must
// never surface an error for an already-committed record. The interval is
// passed in (the fresh path resolved it BEFORE the insert and fails loudly;
// the idempotent replay resolves it best-effort). Shared by the fresh submit's
// post-commit block and the Story 7.5 replay, so the two always derive
// identically.
func (s *Service) deriveInspectionStatus(ctx context.Context, toolID string, persisted *Inspection, interval time.Duration) ToolStatus {
	// The orange-window percentage (Story 5-2c, D1) resolves ONCE per request;
	// a settings failure falls back to the default 25 so the post-commit
	// derivation never fails (it is best-effort by design).
	orangeWindowPercent := s.orangeWindowPercent(ctx)
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
		return deriveToolStatus(latestFailAt, lastSuccessAt, nil, interval, orangeWindowPercent, time.Now())
	}
	return deriveToolStatus(statusInput.LatestFailAt, statusInput.LastSuccessAt, statusInput.LastReinstatedAt,
		interval, orangeWindowPercent, time.Now())
}

// replayScheduleInterval resolves the effective inspection-schedule interval
// for a replay / post-commit derivation BEST-EFFORT (Story 7.5): the replay
// check runs BEFORE the tool load (an archived tool must never 404 a replay),
// so the interval is resolved via a tool reload; a tool that cannot be loaded
// (archived/unknown) or a schedule resolution failure returns an error the
// caller logs and falls back from (the derive renders `oos` for a fail and a
// conservative red/orange for a pass — never falsely serviceable).
func (s *Service) replayScheduleInterval(ctx context.Context, toolID string) (time.Duration, error) {
	tool, err := s.store.GetToolWithTypeQualification(ctx, toolID)
	if err != nil {
		return 0, err
	}
	return s.resolveToolScheduleInterval(ctx, tool)
}

// replayInspectionStatus is the idempotent-replay derivation (Story 7.5): the
// record already committed, so a schedule resolution failure must NEVER surface
// as an error (a retried client would fail for nothing) — it is logged and the
// status derives from the existing record alone with a zero interval. Used by
// both the early-replay check and the absorbed-concurrent-replay path, so every
// replay derives identically.
func (s *Service) replayInspectionStatus(ctx context.Context, toolID string, existing *Inspection) ToolStatus {
	interval, err := s.replayScheduleInterval(ctx, toolID)
	if err != nil {
		s.log().Warn("tools core: schedule resolution failed during replay; deriving from the record alone",
			"tool", toolID, "error", err)
		interval = 0
	}
	return s.deriveInspectionStatus(ctx, toolID, existing, interval)
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
// The CLIENT-SUPPLIED idempotencyKey makes the write at-most-once (Story 7.5,
// NFR-R1): the key rides on the insert and is unique per tool. An EXISTING
// reinstatement for (toolID, idempotencyKey) is a REPLAY — the service returns
// the same post-commit derived status, skipping the tool load, OOS
// precondition, reason validation and the insert (no re-audit, no second row).
// The replay check runs EARLY — right after the permission gate, BEFORE the
// tool load and the OOS precondition — so a retried reinstate of an
// already-committed row (which flipped the tool serviceable — the NOT-OOS 400
// would otherwise reject it) replays 200, and an archived tool (archived
// between commit and retry) replays 200 (never the 404). A CONCURRENT_RACE
// that slips past the early check is absorbed by the DB UNIQUE constraint; the
// repository reports `replayed` and the core treats it identically (no
// re-audit).
//
// I/O matrix:
//   - REINSTATE_OK: OOS holder + valid reason → 200 with the not-OOS derived status.
//   - REPLAY: same tool + key, first attempt already committed → 200 with the
//     same not-OOS derived status, no second row — even when the tool is now
//     serviceable or archived.
//   - REINSTATE_NOT_OOS: tool is NOT out of service → ErrInspectionInvalid (400,
//     MsgToolNotOutOfService), no write.
//   - REINSTATE_GATED: caller lacks tool.reinstate → ErrForbidden (403, no write).
//   - REINSTATE_ARCHIVED / REINSTATE_UNKNOWN: archived / nonexistent tool →
//     ErrToolNotFound (404).
//   - REINSTATE_EMPTY / REINSTATE_LONG: empty or > 2000-rune reason →
//     ErrInspectionInvalid (400, no write).
func (s *Service) ReinstateTool(ctx context.Context, actorID, toolID, reason, idempotencyKey string) (*ReinstateResult, error) {
	// The idempotency key is REQUIRED and must be a parseable UUID (Story 7.5,
	// defense-in-depth): the HTTP adapter validates + normalizes first; this is
	// the domain backstop so a direct caller can never reach the store with an
	// empty/malformed key (which would surface an opaque internal error).
	if !validIdempotencyKey(idempotencyKey) {
		return nil, &InvalidInspectionError{Message: MsgIdempotencyKeyInvalid}
	}

	if err := s.requireToolsPermission(ctx, actorID, []string{ToolReinstatePermission}); err != nil {
		return nil, err
	}

	// EARLY REPLAY CHECK (Story 7.5): an already-committed reinstatement for
	// this tool + key is a retried reinstate — return the same post-commit
	// derived status. It runs BEFORE the tool load so an archived tool
	// (archived between commit and retry) can NEVER 404 a replay, and BEFORE
	// the OOS precondition (the committed row flipped the tool serviceable, so
	// the NOT-OOS 400 would otherwise reject the retry). A replay NEVER
	// re-audits and NEVER double-inserts.
	existing, err := s.store.FindReinstatementByToolAndKey(ctx, toolID, idempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("tools core: failed to resolve reinstatement replay: %w", err)
	}
	if existing != nil {
		s.log().Info("reinstatement replayed",
			"actor", actorID, "tool", toolID, "key", idempotencyKey)
		return &ReinstateResult{Status: s.deriveReinstatementStatus(ctx, toolID)}, nil
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

	_, replayed, err := s.store.InsertReinstatement(ctx, tool.ID, actorID, reason, idempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("tools core: failed to persist reinstatement: %w", err)
	}
	// A CONCURRENT_RACE (Story 7.5): the DB unique constraint absorbed the
	// duplicate (ON CONFLICT DO NOTHING → the repository replayed the winner's
	// row). From the client's perspective this is a REPLAY — NEVER re-audit an
	// absorbed request (it must not add an audit row for a write it did not
	// perform).
	if replayed {
		s.log().Info("reinstatement replayed (concurrent)",
			"actor", actorID, "tool", toolID, "key", idempotencyKey)
		return &ReinstateResult{Status: s.deriveReinstatementStatus(ctx, toolID)}, nil
	}

	s.auditTool(ctx, actorID, AuditOperationToolReinstate, "action=reinstate target=tool id="+tool.ID)

	// Derive the new status (AD-4/AD-5) BEST-EFFORT after the row committed: a
	// schedule/status resolution failure AFTER the write must NOT surface as an
	// error (a client retry would DUPLICATE the reinstatement). Log the failure
	// and answer a conservative NON-OOS status — the row committed, so the tool
	// is out of OOS (green with nil next_due; the SPA refetches the real status
	// from the dashboard list). The row is the audit source of truth regardless.
	return &ReinstateResult{Status: s.deriveReinstatementStatus(ctx, toolID)}, nil
}

// deriveReinstatementStatus is the post-commit reinstatement derivation
// (AD-4/AD-5), best-effort: a schedule or status resolution failure AFTER the
// row committed must never surface as an error (a client retry would duplicate
// the row) — both are logged and a conservative NON-OOS status is answered
// (the row committed, so the tool is out of OOS). Shared by the fresh write,
// the idempotent replay and the absorbed-concurrent-replay (Story 7.5) so they
// all derive identically. The interval is resolved best-effort via a tool
// reload (the replay runs before the tool load), so it also covers an archived
// tool.
func (s *Service) deriveReinstatementStatus(ctx context.Context, toolID string) ToolStatus {
	interval, err := s.replayScheduleInterval(ctx, toolID)
	if err != nil {
		s.log().Warn("tools core: schedule resolution failed after reinstatement; answering conservative non-OOS",
			"tool", toolID, "error", err)
		return ToolStatus{Status: ToolStatusCodeGreen}
	}
	// The orange-window percentage (Story 5-2c, D1) resolves ONCE per request;
	// a settings failure falls back to the default 25 so the post-commit
	// derivation never fails (it is best-effort by design).
	orangeWindowPercent := s.orangeWindowPercent(ctx)
	statusInput, err := s.store.GetToolInspectionStatus(ctx, toolID)
	if err != nil {
		s.log().Warn("tools core: status read failed after reinstatement; answering conservative non-OOS",
			"tool", toolID, "error", err)
		return ToolStatus{Status: ToolStatusCodeGreen}
	}
	if statusInput == nil {
		statusInput = &ToolInspectionStatus{}
	}
	return deriveToolStatus(statusInput.LatestFailAt, statusInput.LastSuccessAt, statusInput.LastReinstatedAt,
		interval, orangeWindowPercent, time.Now())
}
