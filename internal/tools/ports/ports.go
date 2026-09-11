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

// Service is the Tool module's inbound configuration port (Story 4.2, AD-10):
// list/create/update/archive tool types. Every method re-checks the
// `tool_types.manage` code against the actor's LIVE permission set
// defense-in-depth (AD-6). Writes are audited with actor, timestamp and
// operation (NFR-O1/NFR-O2). Update REPLACES the checklist-item list fully; the
// cross-module FKs are validated through the modules' read-only ports.
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
}
