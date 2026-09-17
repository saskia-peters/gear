// Package ports declares the port interfaces of the Tool Maintenance hexagon
// (AD-1). It consumes the User module's auth port (AD-2/AD-7) and configures
// through the Admin module's schedule port (AD-16). The outbound consumer ports
// used by the Tool core are the modules' own read-only ports — the Admin
// module's SchedulesPort and the User module's QualificationCatalogPort — never
// local copies, so the Tool module reads another module's data through that
// module's seam (AD-7/AD-10/AD-11).
package ports

import (
	"context"

	"github.com/saskia-peters/gear/internal/tools/core"
)

// Service is the Tool module's inbound configuration port (Story 4.2 + 4.3,
// AD-10): list/create/update/archive tool types AND the physical tools that
// belong to them. Every method re-checks its own permission code (`tool_types.manage`
// for the type surface, `tools.manage` for the tool surface) against the
// actor's LIVE permission set defense-in-depth (AD-6). Writes are audited with
// actor, timestamp and operation (NFR-O1/NFR-O2). Update REPLACES the
// tool-type checklist-item list fully; the cross-module FKs are validated
// through the modules' read-only ports (the tool-type schedule/qualification
// and the per-tool schedule override), while the tool's type is validated
// intra-module.
type Service interface {
	// ListToolTypes returns every ACTIVE tool type, oldest first, each with its
	// ordered checklist items (GET_LIST_EMPTY / GET_LIST). Archived rows are
	// filtered server-side.
	ListToolTypes(ctx context.Context, actorID string) ([]*core.ToolType, error)
	// CreateToolType persists a new tool type with its ordered checklist items.
	// The default schedule id must be an ACTIVE schedule and the required
	// qualification id must exist (port lookups); a duplicate name answers the
	// German duplicate-name 400. Audited (tool_type.create).
	CreateToolType(ctx context.Context, actorID string, input core.ToolTypeInput) (*core.ToolType, error)
	// UpdateToolType persists a tool type and REPLACES its checklist-item list
	// fully (the surface always submits the whole ordered list). Updating an
	// already-archived type answers ErrToolTypeNotFound (404 sentinel).
	// Audited (tool_type.update).
	UpdateToolType(ctx context.Context, actorID, id string, input core.ToolTypeInput) (*core.ToolType, error)
	// ArchiveToolType soft-archives one type: archived_at is set, the row leaves
	// the active list. Audited. Archiving an already-archived type answers
	// ErrToolTypeNotFound.
	ArchiveToolType(ctx context.Context, actorID, id string) (*core.ToolType, error)
	// ListTools returns every ACTIVE tool, oldest first, each with its tool
	// type's display name (GET_LIST_EMPTY / GET_LIST). Archived rows are
	// filtered server-side.
	ListTools(ctx context.Context, actorID string) ([]*core.Tool, error)
	// ListToolsForDashboard returns every ACTIVE tool, oldest first, each with
	// its tool type's display name AND its DERIVED status — the
	// dashboard.view-gated GEAR-module read (Story 4-3b + 6.1). UNGATED by
	// design: the HTTP mount (`/api/v1/tools`) carries the dashboard.view gate,
	// so a tools.manage-less dashboard.view holder can render the Werkzeugliste.
	// The status is computed on read via the shared clock function (AD-4, never
	// stored); a tool whose effective schedule is missing/invalid or whose
	// status read errors is rendered `red` (DASH_RESILIENT).
	ListToolsForDashboard(ctx context.Context) ([]*core.DashboardTool, error)
	// CreateTool persists a new physical tool. The tool's type must EXIST and
	// be ACTIVE (intra-module ToolTypeExistsActive); an EMPTY schedule override
	// is stored as NULL (the tool inherits its type's default, AD-5) while a
	// non-empty one must be an ACTIVE schedule (SchedulesPort lookup); a
	// duplicate name answers the German duplicate-name 400. Audited
	// (tool.create).
	CreateTool(ctx context.Context, actorID string, input core.ToolInput) (*core.Tool, error)
	// UpdateTool persists a tool. An EMPTY schedule override CLEARS the stored
	// override (schedule_id NULL → inherits the type default again, AD-5);
	// updating an already-archived tool answers ErrToolNotFound (404 sentinel).
	// Audited (tool.update).
	UpdateTool(ctx context.Context, actorID, id string, input core.ToolInput) (*core.Tool, error)
	// ArchiveTool soft-archives one tool: archived_at is set, the row leaves
	// the active list. Audited. Archiving an already-archived tool answers
	// ErrToolNotFound.
	ArchiveTool(ctx context.Context, actorID, id string) (*core.Tool, error)
	// StartInspection is the qualification-gated inspection start (Story 5.1,
	// FR-11/AD-7): it resolves the tool + its type's required_qualification_id
	// (intra-module store read) and — when the type requires a qualification —
	// checks the caller's granted qualifications through the User module's
	// QualificationCatalogPort (expiry-aware, AD-7/FR-22). Eligible → the start
	// result (tool + its type's inspection_mode); missing/expired qualification
	// → ErrToolQualificationMissing (403, German); unknown/archived tool →
	// ErrToolNotFound (404). Re-checks `inspection.submit` defense-in-depth
	// (AD-6). No inspection record is created (Stories 5.2/5.4/5.5).
	StartInspection(ctx context.Context, actorID, toolID string) (*core.InspectionStartResult, error)
	// SubmitInspection persists one inspection (Story 5.3, FR-12/FR-13/FR-14):
	// it re-checks `inspection.submit` AND re-validates the tool-type
	// qualification on submit (never trusts the client, FR-11); unknown/archived
	// tool → ErrToolNotFound (404); a bad mode/result/notes/items contract →
	// ErrInspectionInvalid (400, German); missing/expired qualification → 403.
	// Persists the inspection + its snapshot items transactionally, audits
	// `inspection.submit` and returns the persisted record + the shared derived
	// status (AD-4/AD-5: `oos` on a failed inspection — never stored).
	SubmitInspection(ctx context.Context, actorID, toolID string, input core.InspectionInput) (*core.SubmitInspectionResult, error)
	// ReinstateTool reinstates an OOS tool (Story 5.6, FR-15/AD-9): it re-checks
	// `tool.reinstate` defense-in-depth (AD-6), loads the tool (unknown/archived
	// → ErrToolNotFound 404), validates the MANDATORY reason (empty or > 2000
	// runes → ErrInspectionInvalid 400, German), persists the reinstatement,
	// audits `tool.reinstate` and returns the newly derived not-OOS status
	// (next_due = reinstatement + resolved interval, AD-5). Reinstatement is the
	// SOLE exit from OOS (FR-15).
	ReinstateTool(ctx context.Context, actorID, toolID, reason string) (*core.ReinstateResult, error)
	// ListInspectionHistory returns the per-tool audit trail (Story 6.3, FR-18):
	// every inspection (newest first, each naming the inspector + timestamp +
	// outcome + notes + mode + the snapshotted per-checklist-item results) and
	// every reinstatement (newest first, actor + reason). Re-checks
	// `inspection.history.view` defense-in-depth (AD-6); missing/archived tool
	// → ErrToolNotFound (404 German); inspector/actor display names resolve
	// through the DisplayNameResolver seam (a missing user row maps to the
	// literal "Deleted User", Story 3.4 forward-compat).
	ListInspectionHistory(ctx context.Context, actorID, toolID string) (*core.ToolHistory, error)
}
