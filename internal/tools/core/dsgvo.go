package core

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// DSGVO data-access export (Story 3.3, FR-24/AD-8): the read-only export port
// the DSGVO orchestrator consumes through the Tool module's seam
// (tools/ports.DSGVOInspectionExportPort). The export returns the subject's
// inspection records (as inspector, with the snapshotted per-checklist-item
// results), their reinstatement records (as actor) and the TOOL-OWNED summary —
// counts grouped per tool (id + name) so the report reads as "what this user
// did across the fleet". Actor names are NOT resolved — the report subject is
// the exporting user (the design note). All reads are intra-module (Tool-owned
// tables, AD-8/AD-11). It is a pure read — no writes, no state mutation.

// InspectionItemExport is one snapshotted checklist result in the DSGVO export
// (FR-12): label + position are the snapshot taken at submit time; result is
// pass|fail.
type InspectionItemExport struct {
	ID       string `json:"id"`
	ItemID   string `json:"item_id"`
	Label    string `json:"label"`
	Position int    `json:"position"`
	Result   string `json:"result"`
}

// InspectionExport is one inspection record of the DSGVO export: the tool it
// was performed on (id + name), the mode, the overall result, the optional
// notes, the submitted timestamp and the per-checklist-item results.
type InspectionExport struct {
	ID            string                `json:"id"`
	ToolID        string                `json:"tool_id"`
	ToolName      string                `json:"tool_name"`
	Mode          string                `json:"mode"`
	OverallResult string                `json:"overall_result"`
	Notes         string                `json:"notes"`
	SubmittedAt   time.Time             `json:"submitted_at"`
	Items         []InspectionItemExport `json:"items"`
}

// ReinstatementExport is one reinstatement record of the DSGVO export: the
// tool it was performed on (id + name), the mandatory reason and the
// timestamp.
type ReinstatementExport struct {
	ID        string    `json:"id"`
	ToolID    string    `json:"tool_id"`
	ToolName  string    `json:"tool_name"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

// ToolExportSummary is one per-tool group of the DSGVO export summary: how
// many inspections the subject performed on the tool and how many of those
// failed (the OOS-relevant count).
type ToolExportSummary struct {
	ToolID          string `json:"tool_id"`
	ToolName        string `json:"tool_name"`
	InspectionCount int    `json:"inspection_count"`
	FailCount       int    `json:"fail_count"`
}

// UserInspectionDataExport is the Tool module's DSGVO data-access export
// (FR-24): the subject's inspections (newest first, with items) + their
// reinstatement records (newest first) + the per-tool summary. Empty lists
// serialize as `[]`, never null.
type UserInspectionDataExport struct {
	Inspections    []InspectionExport    `json:"inspections"`
	Reinstatements []ReinstatementExport `json:"reinstatements"`
	Summary        []ToolExportSummary   `json:"summary"`
}

// ExportUserInspectionData returns the DSGVO inspection-data export of one user
// (Story 3.3, FR-24/AD-8): every inspection they performed as inspector
// (newest first, EACH with the snapshotted per-checklist-item results — the
// repository attaches them in one grouped round-trip, no N+1), every
// reinstatement they performed as actor (newest first) and the per-tool
// summary (counts + per-tool grouping with the tool's display name). A user
// with no inspection records answers an empty export with zero counts
// (REPORT_NO_INSPECTIONS) — never an error. The export is UNGATED by design —
// the DSGVO orchestrator re-checks `dsgvo.access_report` defense-in-depth
// (AD-6); this method never touches the actor.
func (s *Service) ExportUserInspectionData(ctx context.Context, userID string) (*UserInspectionDataExport, error) {
	inspections, err := s.store.ListInspectionsByInspector(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("tools core: failed to list user inspections for DSGVO export: %w", err)
	}
	reinstatements, err := s.store.ListReinstatementsByActor(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("tools core: failed to list user reinstatements for DSGVO export: %w", err)
	}

	export := &UserInspectionDataExport{
		Inspections:    make([]InspectionExport, 0, len(inspections)),
		Reinstatements: make([]ReinstatementExport, 0, len(reinstatements)),
		Summary:        []ToolExportSummary{},
	}
	if len(inspections) == 0 && len(reinstatements) == 0 {
		// REPORT_NO_INSPECTIONS: no records at all — skip the tool-name seam
		// entirely (a nil/unwired seam must never fail the empty export).
		return export, nil
	}

	// Collect the referenced tool ids ONCE and resolve their display names in a
	// single intra-module read (no N+1). A tool id ABSENT from the result (a
	// concurrent deletion) falls back to the id itself — the report never 404s
	// on a vanished tool row.
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
		addID(insp.ToolID)
	}
	for _, r := range reinstatements {
		addID(r.ToolID)
	}
	resolved, err := s.store.ListToolNamesByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("tools core: failed to resolve tool names for DSGVO export: %w", err)
	}
	for id, name := range resolved {
		names[id] = name
	}
	toolName := func(id string) string {
		if n, ok := names[id]; ok && n != "" {
			return n
		}
		return id
	}

	// Per-tool grouping for the summary (the design note): counts per tool in
	// first-seen order, including the fail count (the OOS-relevant subset).
	type toolGroup struct {
		toolID   string
		toolName string
		count    int
		fails    int
	}
	groups := make([]*toolGroup, 0, len(seen))
	byTool := make(map[string]*toolGroup, len(seen))
	for _, insp := range inspections {
		g := byTool[insp.ToolID]
		if g == nil {
			g = &toolGroup{toolID: insp.ToolID, toolName: toolName(insp.ToolID)}
			byTool[insp.ToolID] = g
			groups = append(groups, g)
		}
		g.count++
		if insp.OverallResult == InspectionResultFail {
			g.fails++
		}
	}

	for _, insp := range inspections {
		items := make([]InspectionItemExport, 0, len(insp.Items))
		for _, item := range insp.Items {
			items = append(items, InspectionItemExport{
				ID:       item.ID,
				ItemID:   item.ItemID,
				Label:    item.Label,
				Position: item.Position,
				Result:   item.Result,
			})
		}
		export.Inspections = append(export.Inspections, InspectionExport{
			ID:            insp.ID,
			ToolID:        insp.ToolID,
			ToolName:      toolName(insp.ToolID),
			Mode:          insp.Mode,
			OverallResult: insp.OverallResult,
			Notes:         insp.Notes,
			SubmittedAt:   insp.SubmittedAt,
			Items:         items,
		})
	}
	for _, r := range reinstatements {
		export.Reinstatements = append(export.Reinstatements, ReinstatementExport{
			ID:        r.ID,
			ToolID:    r.ToolID,
			ToolName:  toolName(r.ToolID),
			Reason:    r.Reason,
			CreatedAt: r.CreatedAt,
		})
	}
	// Summary rows are ordered by tool name for a stable report (the group
	// order is first-seen by submitted_at, which is inspection-order dependent).
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].toolName < groups[j].toolName })
	for _, g := range groups {
		export.Summary = append(export.Summary, ToolExportSummary{
			ToolID:          g.toolID,
			ToolName:        g.toolName,
			InspectionCount: g.count,
			FailCount:       g.fails,
		})
	}
	return export, nil
}