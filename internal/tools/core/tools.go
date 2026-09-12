package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Tool Management (Story 4.3, FR-9/FR-10/AD-5/AD-6/AD-10): the Tool module's
// exported configuration surface for the PHYSICAL tools that belong to a tool
// type — name (the identifier), exactly one tool type, an OPTIONAL per-tool
// schedule override and the attributes JSONB passthrough. Gated by
// `tools.manage` at the route mount; the core re-verifies the exact code
// defense-in-depth (AD-6). Archive is SOFT (archived_at set) — no hard delete,
// FK history preserved. The tool's type is intra-module (validated against the
// Tool module's own ToolTypeExistsActive — no permission, no cross-module
// join); the schedule override is cross-module and validated through the Admin
// SchedulesPort (active only) — an EMPTY override is stored as SQL NULL and
// means "inherit the tool type's default schedule" (AD-5), mirroring the
// optional-qualification precedent (000023, parseToolTypeID empty →
// pgtype.UUID{} → NULL).

// ToolsManagePermission is the server-authoritative gate code for the whole
// tool surface (AD-6). One Go const so the route mount, the core re-check and
// the SPA-facing documentation never drift.
const ToolsManagePermission = "tools.manage"

// ToolEditPermission is the scoped tool-EDIT gate code (Story 4-3b, AD-6): a
// holder can VIEW + EDIT tools (incl. the inventory number) but NOT create or
// archive them — those stay `tools.manage`. The reads (ListTools/UpdateTool)
// are any-of [tools.manage, tool.edit]; the writes (CreateTool/ArchiveTool) are
// tools.manage-only. One Go const so the route mount, the core re-check and the
// SPA-facing documentation never drift.
const ToolEditPermission = "tool.edit"

// DashboardViewPermission is the server-authoritative gate code for the
// GEAR-module (non-admin) dashboard tool-list surface (Story 4-3b): the
// `/api/v1/tools` mount carries it (all base roles hold it), so the core read
// stays UNGATED by design — see ListToolsForDashboard. One Go const so the
// route mount, the tests and the SPA-facing documentation never drift.
const DashboardViewPermission = "dashboard.view"

// Audit-operation tags for the tool surface (NFR-O1/NFR-O2): creates, updates
// and archives are audited with actor, timestamp and operation.
const (
	AuditOperationToolCreate  = "tool.create"
	AuditOperationToolUpdate  = "tool.update"
	AuditOperationToolArchive = "tool.archive"
)

// ErrToolNotFound is returned when an update/archive references an id that
// does not exist (or is not a valid uuidv7) — INCLUDING an already-archived
// row, which the surface treats as non-existent (soft archive is irreversible
// in V1, so editing/archiving an archived tool answers the 404 sentinel).
// Handlers map it to the uniform 404.
var ErrToolNotFound = errors.New("tools core: tool not found")

// ErrToolInvalid is the sentinel wrapping a German validation message for a
// 400 invalid_request (empty name, missing/invalid type FK, bad override FK).
var ErrToolInvalid = errors.New("tools core: invalid tool")

// InvalidToolError carries the German validation message for a 400
// invalid_request. It unwraps to ErrToolInvalid so callers can match the
// sentinel while still rendering the field-specific microcopy.
type InvalidToolError struct {
	Message string
}

func (e *InvalidToolError) Error() string { return e.Message }
func (e *InvalidToolError) Unwrap() error { return ErrToolInvalid }

// German microcopy for the tool surface (FR-9/UX-DR8).
const (
	MsgToolSaved    = "Werkzeug gespeichert."
	MsgToolArchived = "Werkzeug archiviert."
	MsgToolNotFound = "Das Werkzeug wurde nicht gefunden."
	// MsgToolNameTaken rejects a create/update whose name another tool already
	// holds (case-insensitive duplicate-name guard, mirroring tool_types).
	MsgToolNameTaken = "Es gibt bereits ein Werkzeug mit diesem Namen."
	// MsgToolInvalidType is the 400 message when the tool's type does not exist
	// OR is archived (intra-module ToolTypeExistsActive check).
	MsgToolInvalidType = "Der gewählte Gerätetyp ist ungültig."
	// MsgToolInvalidSchedule is the 400 message when the schedule override id is
	// not an ACTIVE schedule in the Admin catalog (port lookup).
	MsgToolInvalidSchedule = "Der gewählte Zeitplan ist ungültig."
	// MsgToolReferencedGone is the 400 message for a DB-level FK violation
	// (SQLSTATE 23503): a tool type or schedule referenced by the request
	// vanished between the core check and the insert (e.g. archived or deleted
	// concurrently) — the raw pg error must never surface as a 500.
	MsgToolReferencedGone = "Der referenzierte Gerätetyp oder Zeitplan ist nicht mehr verfügbar."
	// MsgToolInventoryNumberRequired rejects an update that CLEARS the inventory
	// number — a tool always has one, it is never empty (UPDATE_CLEAR, 400).
	MsgToolInventoryNumberRequired = "Die Gerätenummer darf nicht leer sein."
	// MsgToolInventoryNumberTooLong is the 400 message for an over-long number
	// (bounded by the DB CHECK <= 16, mirrored here in RUNES).
	MsgToolInventoryNumberTooLong = "Die Gerätenummer ist zu lang (maximal 16 Zeichen)."
	// MsgToolInventoryNumberTaken rejects an update whose inventory number
	// another ACTIVE tool already holds (case-insensitive active-guard; the DB
	// UNIQUE backstop covers EXACT reuse of an archived number, Story 4.5).
	MsgToolInventoryNumberTaken = "Es gibt bereits ein Werkzeug mit dieser Gerätenummer."
	// MsgToolInventoryNumberCollision is the German 400 when the auto-assigned
	// inventory number collided repeatedly (bounded retry loop exhausted) — a
	// manual edit raced the sequence; the create must be retried.
	MsgToolInventoryNumberCollision = "Die Gerätenummer konnte nicht vergeben werden. Bitte versuche es erneut."
)

// InventoryNumberMaxLength bounds the editable inventory number (16 chars —
// 'GEAR' + 6 zero-padded digits leaves headroom for future numbering schemes;
// mirrored by the DB CHECK constraint).
const InventoryNumberMaxLength = 16

// Tool is the domain representation of one physical tool row (FR-9/FR-10).
// ToolTypeID is the intra-module FK to a tool type; ToolTypeName is the JOIN
// display name (read-only, from the Tool-owned tool_types — never a
// cross-module join). ScheduleID is empty when the tool INHERITS its type's
// default schedule (the stored SQL NULL, AD-5) and set otherwise (first-class
// FK to the Admin schedules catalog, never JSONB). Attributes is the
// no-migration JSONB extension surface (FR-10, default '{}'). ArchivedAt is
// nil while the tool is active and set (soft-archive) otherwise.
type Tool struct {
	ID              string
	Name            string
	ToolTypeID      string
	ToolTypeName    string
	ScheduleID      string
	InventoryNumber string
	Attributes      map[string]any
	ArchivedAt      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ToolInput is the shared POST/PUT body (FR-9/FR-10). ScheduleID is the
// OPTIONAL per-tool override: empty → the tool inherits its type's default
// (AD-5). Attributes is the no-migration JSONB extension surface (Story 4.4,
// FR-10/AD-3): it follows the shared update contract — an ABSENT field (nil)
// leaves the stored JSONB unchanged, an EXPLICIT `{}` clears it, a non-empty
// object replaces it wholesale. On CREATE a nil field simply stores the DB
// default `{}`.
// InventoryNumber is IGNORED on create (the server auto-assigns 'GEAR%06d'
// in-SQL, CREATE_IGNORE_CLIENT) and OPTIONAL on update: a non-empty value edits
// the stored number (bounded, unique); an empty value is REJECTED — a tool
// always has an inventory number (UPDATE_CLEAR, 400).
type ToolInput struct {
	Name            string         `json:"name"`
	ToolTypeID      string         `json:"tool_type_id"`
	ScheduleID      string         `json:"schedule_id"`
	InventoryNumber string         `json:"inventory_number"`
	Attributes      map[string]any `json:"attributes"`
}

// ToolStore is the outbound persistence port over the Tool-owned `tools` table
// (AD-10/AD-11). ListTools returns only ACTIVE tools (archived_at IS NULL),
// each with its type display name (JOIN on Tool-owned tool_types).
// ToolExistsActive is the lean update-path check: it reports whether the id
// exists AND is active, without fetching the row (the core uses it to resolve
// the archived sentinel before the duplicate-name guard). Create persists the
// tool (attributes default to '{}' when nil). Update persists the tool,
// refreshing updated_at; an EMPTY ScheduleID is stored as SQL NULL so the tool
// inherits its type's default again (UPDATE_CLEAR_OVERRIDE, AD-5); it refuses
// an already-archived row (and a missing id) with ErrToolNotFound.
// ArchiveTool soft-archives one tool (guarded archived_at IS NULL) — a missing
// or already-archived id answers ErrToolNotFound.
type ToolStore interface {
	ListTools(ctx context.Context) ([]*Tool, error)
	ToolExistsActive(ctx context.Context, id string) (bool, error)
	CreateTool(ctx context.Context, tool *Tool) (*Tool, error)
	UpdateTool(ctx context.Context, tool *Tool) (*Tool, error)
	ArchiveTool(ctx context.Context, id string) (*Tool, error)
}

// toolModuleStore is the combined persistence port the Service consumes: the
// postgres Repository implements BOTH the tool-type store (Story 4.2) and the
// tool store (Story 4.3), so the single `store` dependency of NewService serves
// both surfaces — the tools write path needs ToolTypeExistsActive for the
// intra-module type check, exactly like the shared-constructor signature
// requires.
type toolModuleStore interface {
	ToolTypeStore
	ToolStore
}

// requireToolsPermission re-verifies (defense-in-depth, AD-6) that the actor's
// LIVE permission set holds AT LEAST ONE of the given tool codes. The route
// gateway already enforces the outer any-of gate; the core re-checks the exact
// per-action code list so no future direct caller can skip it. The reads
// (ListTools/UpdateTool) pass any-of [tools.manage, tool.edit]; the writes
// (CreateTool/ArchiveTool) pass tools.manage-only. An empty actor ID never
// passes.
func (s *Service) requireToolsPermission(ctx context.Context, actorID string, required []string) error {
	if actorID == "" {
		return ErrForbidden
	}
	perms, err := s.perms.ListPermissionsByUser(ctx, actorID)
	if err != nil {
		return fmt.Errorf("tools core: failed to resolve actor permissions: %w", err)
	}
	for _, want := range required {
		for _, p := range perms {
			if p == want {
				return nil
			}
		}
	}
	return ErrForbidden
}

// auditTool writes the audit row best-effort (NFR-O1): a failed audit write is
// logged, never rolled back into the triggering operation.
func (s *Service) auditTool(ctx context.Context, actorID, operation, detail string) {
	if s.audit == nil {
		s.log().Warn("tools core: audit writer is not wired", "operation", operation)
		return
	}
	if err := s.audit.InsertAuditEvent(ctx, actorID, operation, detail, AuditSeverityNormal); err != nil {
		s.log().Warn("tools core: tool audit write failed", "operation", operation, "error", err)
	}
}

// ListTools returns every ACTIVE tool, oldest first, each with its type
// display name (GET_LIST_EMPTY / GET_LIST). Archived rows are filtered by the
// store and never reach the surface.
func (s *Service) ListTools(ctx context.Context, actorID string) ([]*Tool, error) {
	if err := s.requireToolsPermission(ctx, actorID, []string{ToolsManagePermission, ToolEditPermission}); err != nil {
		return nil, err
	}
	tools, err := s.store.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("tools core: failed to list tools: %w", err)
	}
	return tools, nil
}

// ListToolsForDashboard returns every ACTIVE tool, oldest first, each with its
// type display name — the dashboard.view-gated GEAR-module read (Story 4-3b).
// Unlike ListTools it deliberately does NOT re-check `tools.manage`: the HTTP
// surface (`/api/v1/tools`, mounted behind `dashboard.view`) carries the gate
// instead, so a tools.manage-less dashboard.view holder (e.g. Helfer*in) can
// render the Werkzeugliste — mirroring how SchedulesPort/QualificationCatalogPort
// expose ungated reads for cross-module consumption. Archived rows are filtered
// by the store and never reach the surface. No status/due-date derivation
// (Story 6.1) — the SPA marks every listed tool "verfügbar" statically.
func (s *Service) ListToolsForDashboard(ctx context.Context) ([]*Tool, error) {
	tools, err := s.store.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("tools core: failed to list tools: %w", err)
	}
	return tools, nil
}

// CreateTool persists a new tool (CREATE_VALID / CREATE_OVERRIDE /
// CREATE_DUPLICATE / CREATE_INVALID / CREATE_BAD_TYPE / CREATE_BAD_OVERRIDE):
// the name must be non-empty, bounded and unique (case-insensitive); the tool
// type must EXIST and be ACTIVE (intra-module store check); an EMPTY schedule
// override is allowed (stores NULL → inherits the type default, AD-5) while a
// NON-EMPTY one must be an ACTIVE schedule in the Admin catalog (port lookup).
// Attributes (Story 4.4) are validated and stored in the `attributes` JSONB
// column (an absent field stores the DB default '{}'). The write goes through
// the Tool module's configuration port (AD-10). Audited (tool.create).
func (s *Service) CreateTool(ctx context.Context, actorID string, input ToolInput) (*Tool, error) {
	if err := s.requireToolsPermission(ctx, actorID, []string{ToolsManagePermission}); err != nil {
		return nil, err
	}
	if err := validateToolInput(input); err != nil {
		return nil, err
	}
	if err := s.validateToolFKs(ctx, input); err != nil {
		return nil, err
	}

	// Attributes follow the shared JSONB contract (Story 4.4), expressed through
	// the shared helpers: an ABSENT field (attributesUnchanged) on create stores
	// the DB default '{}' — nothing exists yet to keep — while an explicit {}
	// (attributesCleared) or a non-empty object is stored as-is.
	attrs, err := validateAttributes(input.Attributes)
	if err != nil {
		return nil, err
	}
	if attributesUnchanged(attrs) {
		attrs = map[string]any{}
	}

	tool := &Tool{
		Name:       strings.TrimSpace(input.Name),
		ToolTypeID: strings.TrimSpace(input.ToolTypeID),
		ScheduleID: strings.TrimSpace(input.ScheduleID),
		Attributes: attrs,
	}

	if err := s.ensureUniqueToolName(ctx, tool.Name, ""); err != nil {
		return nil, err
	}

	persisted, err := s.store.CreateTool(ctx, tool)
	if err != nil {
		return nil, fmt.Errorf("tools core: failed to persist tool: %w", err)
	}

	s.auditTool(ctx, actorID, AuditOperationToolCreate, "action=create target=tool id="+persisted.ID)
	return persisted, nil
}

// UpdateTool persists a tool (UPDATE_CLEAR_OVERRIDE / UPDATE_ARCHIVED): name,
// type, override, inventory number and attributes are handled; an EMPTY
// schedule override is stored as SQL NULL so the tool inherits its type's
// default again (AD-5). Attributes follow the shared JSONB contract (Story
// 4.4): an ABSENT field leaves the stored JSONB unchanged, an EXPLICIT `{}`
// clears it, a non-empty object replaces it wholesale.
// The target's existence/active state is resolved BEFORE the FK validation so
// an update of an unknown/archived id answers the 404 sentinel even when the
// submitted body carries a currently-invalid type/schedule (the archived
// sentinel wins over any 400). The type and the (non-empty) override are then
// re-validated. Audited (tool.update).
func (s *Service) UpdateTool(ctx context.Context, actorID, id string, input ToolInput) (*Tool, error) {
	if err := s.requireToolsPermission(ctx, actorID, []string{ToolsManagePermission, ToolEditPermission}); err != nil {
		return nil, err
	}
	if err := validateToolInput(input); err != nil {
		return nil, err
	}

	// Resolve the target BEFORE the unique-name check AND the FK validation so
	// an update of an unknown/archived id answers the 404 sentinel (never a
	// duplicate-name 400 or a body-driven invalid-FK 400).
	exists, err := s.store.ToolExistsActive(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("tools core: failed to resolve tool: %w", err)
	}
	if !exists {
		return nil, ErrToolNotFound
	}

	// Validate the inventory number (Story 4-3b): a tool always has one (an
	// empty value is REJECTED — it can never be cleared, UPDATE_CLEAR), it is
	// bounded, and it is unique case-insensitively among ACTIVE tools. This
	// runs AFTER the existence check so an unknown/archived id answers the 404
	// sentinel even with an invalid inventory number (the sentinel wins over
	// any 400).
	inventoryNumber, err := validateToolInventoryNumber(input.InventoryNumber)
	if err != nil {
		return nil, err
	}
	if err := s.ensureUniqueToolInventoryNumber(ctx, inventoryNumber, id); err != nil {
		return nil, err
	}

	if err := s.validateToolFKs(ctx, input); err != nil {
		return nil, err
	}

	if err := s.ensureUniqueToolName(ctx, strings.TrimSpace(input.Name), id); err != nil {
		return nil, err
	}

	// Attributes follow the shared JSONB contract (Story 4.4), expressed through
	// the shared helpers: an ABSENT field (attributesUnchanged) passes nil so
	// the store's COALESCE keeps the stored JSONB, an EXPLICIT {}
	// (attributesCleared) passes the empty map so it clears, a non-empty object
	// replaces wholesale.
	attrs, err := validateAttributes(input.Attributes)
	if err != nil {
		return nil, err
	}
	switch {
	case attributesUnchanged(input.Attributes):
		attrs = nil
	case attributesCleared(input.Attributes):
		attrs = map[string]any{}
	}

	tool := &Tool{
		ID:              id,
		Name:            strings.TrimSpace(input.Name),
		ToolTypeID:      strings.TrimSpace(input.ToolTypeID),
		ScheduleID:      strings.TrimSpace(input.ScheduleID),
		InventoryNumber: inventoryNumber,
		Attributes:      attrs,
	}

	persisted, err := s.store.UpdateTool(ctx, tool)
	if err != nil {
		if errors.Is(err, ErrToolNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("tools core: failed to persist tool: %w", err)
	}

	s.auditTool(ctx, actorID, AuditOperationToolUpdate, "action=update target=tool id="+id)
	return persisted, nil
}

// ArchiveTool soft-archives one tool (ARCHIVE): archived_at is set, the row
// leaves the active list, and it cannot be edited/unarchived via the surface in
// V1. Archiving an already-archived row answers the 404 sentinel (the row is
// non-existent to the surface). Audited (tool.archive).
func (s *Service) ArchiveTool(ctx context.Context, actorID, id string) (*Tool, error) {
	if err := s.requireToolsPermission(ctx, actorID, []string{ToolsManagePermission}); err != nil {
		return nil, err
	}

	archived, err := s.store.ArchiveTool(ctx, id)
	if err != nil {
		if errors.Is(err, ErrToolNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("tools core: failed to archive tool: %w", err)
	}

	s.auditTool(ctx, actorID, AuditOperationToolArchive, "action=archive target=tool id="+id)
	return archived, nil
}

// validateToolFKs validates the tool's FK references (CREATE_BAD_TYPE /
// CREATE_BAD_OVERRIDE): the tool's type must EXIST and be ACTIVE — checked
// through the Tool module's OWN store ToolTypeExistsActive (intra-module, no
// permission, no cross-module join). The schedule OVERRIDE is OPTIONAL
// (AD-5): an EMPTY id is allowed and stored as NULL (skip the port check); a
// NON-EMPTY id must be an ACTIVE schedule in the Admin catalog (SchedulesPort
// lookup). A nil SchedulesPort is a composition-root wiring defect and FAILS
// LOUDLY (a 500-style internal error), never a silent skip — skipping would
// bypass the AD-16 validation entirely.
func (s *Service) validateToolFKs(ctx context.Context, input ToolInput) error {
	if s.store == nil {
		return fmt.Errorf("tools core: store is not wired")
	}
	exists, err := s.store.ToolTypeExistsActive(ctx, strings.TrimSpace(input.ToolTypeID))
	if err != nil {
		return fmt.Errorf("tools core: failed to resolve tool type: %w", err)
	}
	if !exists {
		return &InvalidToolError{Message: MsgToolInvalidType}
	}

	scheduleID := strings.TrimSpace(input.ScheduleID)
	if scheduleID == "" {
		// Empty override → inherit the type default: skip the port check and
		// store NULL (mirrors the optional-qualification precedent).
		return nil
	}
	if s.schedules == nil {
		return fmt.Errorf("tools core: schedule catalog port is not wired")
	}
	schedules, err := s.schedules.CurrentSchedules(ctx)
	if err != nil {
		return fmt.Errorf("tools core: failed to resolve schedule catalog: %w", err)
	}
	for _, sch := range schedules {
		if sch.ID == scheduleID {
			return nil
		}
	}
	return &InvalidToolError{Message: MsgToolInvalidSchedule}
}

// validateToolInput enforces the create/update invariants (CREATE_INVALID):
// non-empty bounded name (counted in RUNES) and a present tool_type_id. The
// schedule override is OPTIONAL (empty allowed); its validity is checked by
// validateToolFKs.
func validateToolInput(input ToolInput) error {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return &InvalidToolError{Message: "Bitte gib einen Namen für das Werkzeug an."}
	}
	if utf8.RuneCountInString(name) > 255 {
		return &InvalidToolError{Message: "Der Name ist zu lang."}
	}
	if strings.TrimSpace(input.ToolTypeID) == "" {
		return &InvalidToolError{Message: "Bitte wähle einen Gerätetyp aus."}
	}
	return nil
}

// ensureUniqueToolName rejects a name already held by another ACTIVE tool
// (case-insensitive, finding): two ACTIVE tools must not share a name.
// exceptID excludes the tool being updated from the comparison.
//
// The overall duplicate-name semantics are ONE coherent rule split across two
// guards, mirroring the tool_types convention:
//   - This core guard: case-insensitive over the ACTIVE catalog only.
//   - The DB UNIQUE(name) backstop: case-sensitive over ALL rows (active AND
//     archived). An EXACT reuse of an archived name trips it and the store maps
//     SQLSTATE 23505 to the same German duplicate-name 400.
func (s *Service) ensureUniqueToolName(ctx context.Context, name, exceptID string) error {
	tools, err := s.store.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("tools core: failed to check tool name uniqueness: %w", err)
	}
	for _, tool := range tools {
		if tool.ID == exceptID {
			continue
		}
		if strings.EqualFold(tool.Name, name) {
			return &InvalidToolError{Message: MsgToolNameTaken}
		}
	}
	return nil
}

// validateToolInventoryNumber enforces the editable-inventory invariants
// (UPDATE_INVENTORY / UPDATE_CLEAR / Story 4-3b): the number must be NON-EMPTY
// (a tool always has one — clearing is rejected, 400), bounded to
// InventoryNumberMaxLength RUNES and returned trimmed. Validity of the format
// is intentionally free-form text (char, not number-only — the only hard shape
// is the auto-assigned 'GEAR%06d').
func validateToolInventoryNumber(inventoryNumber string) (string, error) {
	inv := strings.TrimSpace(inventoryNumber)
	if inv == "" {
		return "", &InvalidToolError{Message: MsgToolInventoryNumberRequired}
	}
	if utf8.RuneCountInString(inv) > InventoryNumberMaxLength {
		return "", &InvalidToolError{Message: MsgToolInventoryNumberTooLong}
	}
	return inv, nil
}

// ensureUniqueToolInventoryNumber rejects an edit whose inventory number
// another ACTIVE tool already holds (case-insensitive, finding): two ACTIVE
// tools must not share a number. exceptID excludes the tool being updated from
// the comparison.
//
// The overall duplicate-inventory semantics are ONE coherent rule split across
// two guards, mirroring the duplicate-name convention:
//   - This core guard: case-insensitive over the ACTIVE catalog only.
//   - The DB UNIQUE index (tools_inventory_number_key) backstop: EXACT over ALL
//     rows (active AND archived) — an archived tool's number stays "taken"
//     (the Story 4.5 import backstop). The repository maps SQLSTATE 23505 on
//     that index to the same German duplicate-inventory 400.
func (s *Service) ensureUniqueToolInventoryNumber(ctx context.Context, inventoryNumber, exceptID string) error {
	tools, err := s.store.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("tools core: failed to check inventory-number uniqueness: %w", err)
	}
	for _, tool := range tools {
		if tool.ID == exceptID {
			continue
		}
		if strings.EqualFold(tool.InventoryNumber, inventoryNumber) {
			return &InvalidToolError{Message: MsgToolInventoryNumberTaken}
		}
	}
	return nil
}