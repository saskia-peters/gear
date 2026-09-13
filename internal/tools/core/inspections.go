package core

import (
	"context"
	"errors"
	"fmt"
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