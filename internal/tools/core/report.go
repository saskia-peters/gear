package core

import (
	"context"
	"fmt"
	"time"

	admcore "github.com/saskia-peters/gear/internal/admin/core"
)// ReportRow is one row of the status report (Story 6.2, FR-17): the dashboard
// tool WITH its derived status (the same `dashboardStatus` the dashboard list
// renders — the export can never disagree with the on-screen truth, AD-4/AD-5)
// plus the latest inspection's submitted_at + the inspector's display name.
// LastInspectedAt is nil for a never-inspected tool (the PDF renders "–" in
// the Zuletzt geprüft AND Prüfer/in columns); LastInspectorName is the literal
// "Deleted User" when the inspector account no longer exists (Story 3.4
// forward-compat, AD-8).
type ReportRow struct {
	Tool              DashboardTool
	LastInspectedAt   *time.Time
	LastInspectorName string
}

// ExportStatusReport returns the rows of the status-report PDF (Story 6.2,
// FR-17/AD-6/AD-5): every ACTIVE tool's derived status + latest-inspection
// inputs, filtered to `filterCodes` (empty = "Alle"). The server NEVER trusts
// the client's statuses — the SPA only sends the filter codes; each tool's
// status is DERIVED here via the same shared clock function the dashboard uses.
// The schedule catalog is resolved ONCE per call; per tool the existing
// `dashboardStatus` read (Story 6.1) + ONE latest-inspection read run (the
// N+1 accepted at V1 fleet scale, matching the dashboard's deferred note). ALL
// inspector ids are resolved in ONE DisplayNameResolver call — a missing user
// row maps to "Deleted User" (the 6.3 seam), never a per-row user read. The
// `report.export` code is re-checked defense-in-depth (AD-6): a non-holder
// answers ErrForbidden with no report data.
//
// I/O matrix:
//   - REPORT_OK / REPORT_FILTERED: `report.export` holder → the matching rows
//     (empty filterCodes = all tools), oldest first, each with the derived
//     status + latest-inspection inputs.
//   - REPORT_NEVER: a tool with no inspections → LastInspectedAt nil (the PDF
//     renders "–").
//   - REPORT_DELETED_USER: an inspector id with no user row → that name renders
//     "Deleted User".
//   - REPORT_GATED: caller lacks report.export → ErrForbidden (403, no data
//     exposed, AD-6).
func (s *Service) ExportStatusReport(ctx context.Context, actorID string, filterCodes []string) ([]*ReportRow, error) {
	if err := s.requireToolsPermission(ctx, actorID, []string{ReportExportPermission}); err != nil {
		return nil, err
	}

	tools, err := s.store.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("tools core: failed to list tools: %w", err)
	}

	out := make([]*ReportRow, 0, len(tools))
	if len(tools) == 0 {
		// An EMPTY fleet returns the empty list WITHOUT touching the schedule
		// catalog — the report must answer even if the catalog is unreachable
		// (there is nothing to derive), mirroring the dashboard.
		return out, nil
	}

	// The schedule catalog is resolved ONCE per call (the spec's no-N+1
	// invariant). A nil port is a composition-root wiring defect and FAILS
	// LOUDLY — the report cannot derive status without the clock.
	if s.schedules == nil {
		return nil, fmt.Errorf("tools core: schedule catalog port is not wired")
	}
	schedules, err := s.schedules.CurrentSchedules(ctx)
	if err != nil {
		return nil, fmt.Errorf("tools core: failed to resolve schedule catalog: %w", err)
	}

	// The orange-window percentage resolves ONCE per call alongside the
	// catalog snapshot (Story 5-2c, D1) — the per-tool derivation shares it,
	// no N+1 settings read.
	orangeWindowPercent := s.orangeWindowPercent(ctx)

	filterSet := make(map[string]struct{}, len(filterCodes))
	for _, code := range filterCodes {
		filterSet[code] = struct{}{}
	}

	now := time.Now()
	rows := make([]*ReportRow, 0, len(tools))
	inspectorIDByRow := make([]string, 0, len(tools))
	seen := make(map[string]struct{}, len(tools))
	var ids []string
	for _, tool := range tools {
		status := s.dashboardStatus(ctx, tool, schedules, orangeWindowPercent, now)
		if len(filterSet) > 0 {
			if _, ok := filterSet[string(status.Status)]; !ok {
				continue
			}
		}
		row := &ReportRow{Tool: DashboardTool{Tool: *tool, Status: status}}
		latest, err := s.store.GetLatestInspectionByTool(ctx, tool.ID)
		if err != nil {
			return nil, fmt.Errorf("tools core: failed to read latest inspection: %w", err)
		}
		var inspectorID string
		if latest != nil {
			t := latest.SubmittedAt
			row.LastInspectedAt = &t
			// inspector_id is uuid NOT NULL (000027) — always present when a
			// latest inspection exists. The "never-inspected → –" decision is
			// the nil latest above; the seen-set only dedupes ids for the ONE
			// bulk ResolveDisplayNames call.
			inspectorID = latest.InspectorID
			if _, ok := seen[inspectorID]; !ok {
				seen[inspectorID] = struct{}{}
				ids = append(ids, inspectorID)
			}
		}
		rows = append(rows, row)
		inspectorIDByRow = append(inspectorIDByRow, inspectorID)
	}

	// Resolve ALL inspector display names in ONE seam call (no N+1 user reads).
	// An EMPTY id set skips the seam (a nil resolver never fails an empty
	// read). A missing user row maps to "Deleted User" (REPORT_DELETED_USER).
	names := map[string]string{}
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
	for i, row := range rows {
		name := DeletedUserDisplayName
		if n, ok := names[inspectorIDByRow[i]]; ok && n != "" {
			name = n
		}
		row.LastInspectorName = name
	}
	return rows, nil
}

// dashboardStatus derives ONE tool's status on the resilient dashboard read
// path (DASH_RESILIENT, Story 6.1): a missing/invalid effective schedule or a
// status-read error is logged and rendered `red` (NextDue nil) — the tool is
// never falsely claimed serviceable, and the config defect never takes the
// whole list down. `schedules` is the catalog snapshot resolved once per
// ListToolsForDashboard call; `orangeWindowPercent` is the once-per-call
// settings value (Story 5-2c, D1).
func (s *Service) dashboardStatus(ctx context.Context, tool *Tool, schedules []*admcore.Schedule, orangeWindowPercent int, now time.Time) ToolStatus {
	interval, err := scheduleIntervalFor(tool.ScheduleID, tool.DefaultScheduleID, schedules)
	if err != nil {
		s.log().Warn("tools core: dashboard tool has no effective schedule; rendering red",
			"tool", tool.ID, "error", err)
		return ToolStatus{Status: ToolStatusCodeRed}
	}
	statusInput, err := s.store.GetToolInspectionStatus(ctx, tool.ID)
	if err != nil {
		s.log().Warn("tools core: dashboard status read failed; rendering red",
			"tool", tool.ID, "error", err)
		return ToolStatus{Status: ToolStatusCodeRed}
	}
	if statusInput == nil {
		// A (nil, nil) status read is treated as NEVER-INSPECTED → red (the
		// nil anchors must never be dereferenced — no panic, no false green).
		statusInput = &ToolInspectionStatus{}
	}
	return deriveToolStatus(statusInput.LatestFailAt, statusInput.LastSuccessAt, statusInput.LastReinstatedAt,
		interval, orangeWindowPercent, now)
}