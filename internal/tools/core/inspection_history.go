package core

import (
	"context"
	"errors"
	"fmt"
	"time"
)

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