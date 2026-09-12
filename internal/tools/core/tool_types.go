package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	adminports "github.com/saskia-peters/gear/internal/admin/ports"
	userports "github.com/saskia-peters/gear/internal/user/ports"
)

// Tool-Type Management (Story 4.2, FR-8/FR-10/FR-23/AD-6/AD-10): the Tool
// module's exported configuration surface for tool types — name, default
// schedule (FK from the Admin catalog), required qualification (FK from the
// User vocabulary), inspection mode (pass_fail vs checklist) and ordered
// checklist items. Gated by `tool_types.manage` at the route mount; the core
// re-verifies the exact code defense-in-depth (AD-6). Archive is SOFT
// (archived_at set) — no hard delete, FK history preserved. Cross-module FKs
// are validated through the modules' read-only ports (AD-7/AD-10/AD-11): the
// default schedule against the Admin SchedulesPort (active only) and the
// required qualification against the User QualificationCatalogPort — the Tool
// module never joins another module's tables.

// ToolTypesManagePermission is the server-authoritative gate code for the whole
// tool-type surface (AD-6). One Go const so the route mount, the core re-check
// and the SPA-facing documentation never drift.
const ToolTypesManagePermission = "tool_types.manage"

// Audit-operation tags for the tool-type surface (NFR-O1/NFR-O2): creates,
// updates and archives are audited with actor, timestamp and operation.
const (
	AuditOperationToolTypeCreate  = "tool_type.create"
	AuditOperationToolTypeUpdate  = "tool_type.update"
	AuditOperationToolTypeArchive = "tool_type.archive"
)

// AuditSeverityNormal is the standard audit severity for tool-type events.
const AuditSeverityNormal = "normal"

// Inspection-mode values for a tool type (FR-8). Stored verbatim in
// tool_types.inspection_mode.
const (
	InspectionModePassFail  = "pass_fail"
	InspectionModeChecklist = "checklist"
)

// ErrForbidden is returned when the acting user's live permission set does not
// hold tool_types.manage (defense-in-depth, AD-6). Handlers map it to the
// uniform 403 with no hint of what is missing.
var ErrForbidden = errors.New("tools core: forbidden")

// ErrToolTypeNotFound is returned when an update/archive references an id that
// does not exist (or is not a valid uuidv7) — INCLUDING an already-archived
// row, which the surface treats as non-existent (soft archive is irreversible
// in V1, so editing/archiving an archived type answers the 404 sentinel).
// Handlers map it to the uniform 404.
var ErrToolTypeNotFound = errors.New("tools core: tool type not found")

// ErrToolTypeInvalid is the sentinel wrapping a German validation message for
// a 400 invalid_request (empty name, bad inspection mode, missing/invalid
// FK, invalid checklist item).
var ErrToolTypeInvalid = errors.New("tools core: invalid tool type")

// InvalidToolTypeError carries the German validation message for a 400
// invalid_request. It unwraps to ErrToolTypeInvalid so callers can match the
// sentinel while still rendering the field-specific microcopy.
type InvalidToolTypeError struct {
	Message string
}

func (e *InvalidToolTypeError) Error() string { return e.Message }
func (e *InvalidToolTypeError) Unwrap() error { return ErrToolTypeInvalid }

// German microcopy for the tool-type surface (FR-8/UX-DR8).
const (
	MsgToolTypeSaved    = "Gerätetyp gespeichert."
	MsgToolTypeArchived = "Gerätetyp archiviert."
	MsgToolTypeNotFound = "Der Gerätetyp wurde nicht gefunden."
	// MsgToolTypeNameTaken rejects a create/update whose name another tool type
	// already holds (case-insensitive duplicate-name guard).
	MsgToolTypeNameTaken = "Es gibt bereits einen Gerätetyp mit diesem Namen."
	// MsgToolTypeInvalidSchedule is the 400 message when the default schedule id
	// is not an ACTIVE schedule in the Admin catalog (port lookup).
	MsgToolTypeInvalidSchedule = "Der gewählte Zeitplan ist ungültig."
	// MsgToolTypeInvalidQualification is the 400 message when the required
	// qualification id is not in the User vocabulary (port lookup).
	MsgToolTypeInvalidQualification = "Die gewählte Qualifikation ist ungültig."
	// MsgToolTypeChecklistRequired rejects a checklist-mode type with NO
	// checklist items (FR-23: a checklist template needs at least one point).
	MsgToolTypeChecklistRequired = "Bitte füge mindestens einen Eintrag zur Checkliste hinzu."
	// MsgToolTypeChecklistDuplicate rejects a checklist that repeats a label
	// (case-insensitive — two identical points would be ambiguous).
	MsgToolTypeChecklistDuplicate = "Die Checkliste enthält doppelte Einträge."
	// MsgToolTypeReferencedGone is the 400 message for a DB-level FK violation
	// (SQLSTATE 23503): a schedule/qualification referenced by the request
	// vanished between the port check and the insert (e.g. archived or deleted
	// concurrently) — the raw pg error must never surface as a 500.
	MsgToolTypeReferencedGone = "Die referenzierte Zeiteinheit oder Qualifikation ist nicht mehr verfügbar."
)

// ToolTypeChecklistItemMaxCount caps the ordered checklist-item list at 100
// entries (a defensive bound; the SPA editor is far smaller).
const ToolTypeChecklistItemMaxCount = 100

// ToolTypeChecklistItem is one ordered checklist entry of a tool type's
// inspection template (FR-23). Position is the explicit order the SPA submits
// and the store persists (full-replacement on update).
type ToolTypeChecklistItem struct {
	ID       string
	Position int
	Label    string
}

// ToolType is the domain representation of one tool-type row (FR-8/FR-10).
// DefaultScheduleID and RequiredQualificationID are the validated cross-module
// FK ids; InspectionMode is pass_fail | checklist; Attributes is the
// no-migration JSONB extension surface (FR-10, default '{}'); Items holds the
// ordered checklist child rows (empty for pass_fail). ArchivedAt is nil while
// the type is active and set (soft-archive) otherwise.
type ToolType struct {
	ID                     string
	Name                   string
	DefaultScheduleID      string
	RequiredQualificationID string
	InspectionMode         string
	Attributes             map[string]any
	Items                  []ToolTypeChecklistItem
	ArchivedAt             *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// ToolTypeChecklistItemInput is one ordered checklist entry of the POST/PUT
// body. Only the label travels from the client; position is implied by the
// array order and assigned by the store.
type ToolTypeChecklistItemInput struct {
	Label string `json:"label"`
}

// ToolTypeInput is the shared POST/PUT body (FR-8/FR-10). Items is the whole
// ordered checklist-item list — full replacement on update. Attributes is the
// no-migration JSONB extension surface (Story 4.4, FR-10/AD-3): it follows the
// shared update contract — an ABSENT field (nil) leaves the stored JSONB
// unchanged, an EXPLICIT `{}` clears it, a non-empty object replaces it
// wholesale. On CREATE a nil field simply stores the DB default `{}`.
type ToolTypeInput struct {
	Name                   string                       `json:"name"`
	DefaultScheduleID      string                       `json:"default_schedule_id"`
	RequiredQualificationID string                      `json:"required_qualification_id"`
	InspectionMode         string                       `json:"inspection_mode"`
	Items                  []ToolTypeChecklistItemInput `json:"items"`
	Attributes             map[string]any               `json:"attributes"`
}

// ToolTypeStore is the outbound persistence port over the Tool-owned
// tool_types + tool_type_checklist_items tables (AD-10/AD-11).
// ListToolTypes returns only ACTIVE types (archived_at IS NULL), each with its
// ordered checklist items. ToolTypeExistsActive is the lean update-path check:
// it reports whether the id exists AND is active, without fetching the
// checklist items (the core uses it to resolve the archived sentinel before the
// duplicate-name guard). Create persists the type with its checklist items and
// attributes atomically (an absent attributes map stores the DB default '{}').
// Update persists the type AND replaces the checklist items fully (delete-then-
// insert in one transaction), refreshing updated_at; attributes follow the
// shared contract — a NIL map leaves the stored JSONB unchanged (the SQL
// COALESCE keep), `{}` clears it, a non-empty object replaces it. Update
// refuses an already-archived row (and a missing id) with ErrToolTypeNotFound.
// ArchiveToolType soft-archives one type (guarded archived_at IS NULL) — a
// missing or already-archived id answers ErrToolTypeNotFound.
type ToolTypeStore interface {
	ListToolTypes(ctx context.Context) ([]*ToolType, error)
	ToolTypeExistsActive(ctx context.Context, id string) (bool, error)
	CreateToolType(ctx context.Context, toolType *ToolType) (*ToolType, error)
	UpdateToolType(ctx context.Context, toolType *ToolType) (*ToolType, error)
	ArchiveToolType(ctx context.Context, id string) (*ToolType, error)
}

// PermissionResolver resolves a user's live permission set (AD-12) for the
// defense-in-depth re-check. The User module's postgres repository implements
// it (ListPermissionsByUser).
type PermissionResolver interface {
	ListPermissionsByUser(ctx context.Context, userID string) ([]string, error)
}

// AuditWriter appends to the User-owned audit trail (NFR-O1/NFR-O2). The User
// module's postgres repository implements it; the Tool module never authors
// another module's SQL (AD-8/AD-11).
type AuditWriter interface {
	InsertAuditEvent(ctx context.Context, userID, operation, detail, severity string) error
}

// Service is the Tool module's domain service for tool types (Story 4.2,
// AD-10). It consumes the Tool-owned tables through their store and the
// modules' read-only ports for the cross-module FK validation (AD-7/AD-10/
// AD-11), the User module's repository READ-ONLY for the permission re-check
// (AD-12) and the audit trail (NFR-O1/NFR-O2).
type Service struct {
	store          toolModuleStore
	schedules      adminports.SchedulesPort
	qualifications userports.QualificationCatalogPort
	perms          PermissionResolver
	audit          AuditWriter
	logger         *slog.Logger
}

// NewService constructs the Tool service. store is the combined persistence
// port over the Tool-owned tables (tool types + tools); schedules/
// qualifications are the read-only consumer ports used only by the write path
// (FK validation); perms/audit are the User-module repository seams
// (permission re-check + audit trail). logger may be nil (falls back to
// slog.Default()); it is used for structured logging of audit-write failures
// (NFR-O1).
func NewService(store toolModuleStore, schedules adminports.SchedulesPort, qualifications userports.QualificationCatalogPort, perms PermissionResolver, audit AuditWriter, logger *slog.Logger) *Service {
	return &Service{store: store, schedules: schedules, qualifications: qualifications, perms: perms, audit: audit, logger: logger}
}

// log returns the configured logger or slog.Default().
func (s *Service) log() *slog.Logger {
	if s.logger != nil {
		return s.logger
	}
	return slog.Default()
}

// ListToolTypes returns every ACTIVE tool type, oldest first, each with its
// ordered checklist items (GET_LIST_EMPTY / GET_LIST). Archived rows are
// filtered by the store and never reach the surface.
func (s *Service) ListToolTypes(ctx context.Context, actorID string) ([]*ToolType, error) {
	if err := s.requireToolTypesPermission(ctx, actorID); err != nil {
		return nil, err
	}
	types, err := s.store.ListToolTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("tools core: failed to list tool types: %w", err)
	}
	return types, nil
}

// CreateToolType persists a new tool type (CREATE_VALID / CREATE_DUPLICATE /
// CREATE_INVALID / CREATE_BAD_SCHEDULE / CREATE_BAD_QUALIFICATION): the name
// must be non-empty and unique (case-insensitive), the inspection mode must be
// pass_fail|checklist, the default schedule id must be an ACTIVE schedule
// (port lookup), the required qualification id must exist (port lookup), and
// the checklist items (checklist mode) must be non-empty bounded labels.
// Attributes (Story 4.4) are validated and stored in the `attributes` JSONB
// column (an absent field stores the DB default '{}'). The write goes through
// the Tool module's configuration port (AD-10). Audited (tool_type.create).
func (s *Service) CreateToolType(ctx context.Context, actorID string, input ToolTypeInput) (*ToolType, error) {
	if err := s.requireToolTypesPermission(ctx, actorID); err != nil {
		return nil, err
	}
	if err := validateToolTypeInput(input); err != nil {
		return nil, err
	}
	if err := s.validateToolTypeFKs(ctx, input); err != nil {
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

	toolType := &ToolType{
		Name:                    strings.TrimSpace(input.Name),
		DefaultScheduleID:       strings.TrimSpace(input.DefaultScheduleID),
		RequiredQualificationID: strings.TrimSpace(input.RequiredQualificationID),
		InspectionMode:          input.InspectionMode,
		Attributes:              attrs,
		Items:                   buildItems(input.InspectionMode, input.Items),
	}

	if err := s.ensureUniqueToolTypeName(ctx, toolType.Name, ""); err != nil {
		return nil, err
	}

	persisted, err := s.store.CreateToolType(ctx, toolType)
	if err != nil {
		return nil, fmt.Errorf("tools core: failed to persist tool type: %w", err)
	}

	s.auditToolType(ctx, actorID, AuditOperationToolTypeCreate, "action=create target=tool_type id="+persisted.ID)
	return persisted, nil
}

// UpdateToolType persists a tool type (UPDATE_REPLACE_ITEMS): name, schedule,
// qualification, inspection mode are replaced and the checklist items are
// REPLACED fully (the SPA always submits the whole ordered list) in one
// transaction, updated_at refreshed. Attributes follow the shared JSONB
// contract (Story 4.4): an ABSENT field leaves the stored JSONB unchanged, an
// EXPLICIT `{}` clears it, a non-empty object replaces it wholesale. Updating
// an already-archived type answers the 404 sentinel (UPDATE_ARCHIVED).
// Cross-module FKs are re-validated. Audited (tool_type.update).
func (s *Service) UpdateToolType(ctx context.Context, actorID, id string, input ToolTypeInput) (*ToolType, error) {
	if err := s.requireToolTypesPermission(ctx, actorID); err != nil {
		return nil, err
	}
	if err := validateToolTypeInput(input); err != nil {
		return nil, err
	}
	if err := s.validateToolTypeFKs(ctx, input); err != nil {
		return nil, err
	}

	// Resolve the target BEFORE the unique-name check so an update of an
	// unknown/archived id answers the 404 sentinel (never a duplicate-name 400).
	// The lean existence check does not fetch the checklist items (unused here).
	exists, err := s.store.ToolTypeExistsActive(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("tools core: failed to resolve tool type: %w", err)
	}
	if !exists {
		return nil, ErrToolTypeNotFound
	}

	if err := s.ensureUniqueToolTypeName(ctx, strings.TrimSpace(input.Name), id); err != nil {
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

	toolType := &ToolType{
		ID:                      id,
		Name:                    strings.TrimSpace(input.Name),
		DefaultScheduleID:       strings.TrimSpace(input.DefaultScheduleID),
		RequiredQualificationID: strings.TrimSpace(input.RequiredQualificationID),
		InspectionMode:          input.InspectionMode,
		Attributes:              attrs,
		Items:                   buildItems(input.InspectionMode, input.Items),
	}

	persisted, err := s.store.UpdateToolType(ctx, toolType)
	if err != nil {
		if errors.Is(err, ErrToolTypeNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("tools core: failed to persist tool type: %w", err)
	}

	s.auditToolType(ctx, actorID, AuditOperationToolTypeUpdate, "action=update target=tool_type id="+id)
	return persisted, nil
}

// ArchiveToolType soft-archives one tool type (ARCHIVE): archived_at is set,
// the row leaves the active list, and it cannot be edited/unarchived via the
// surface in V1. Archiving an already-archived row answers the 404 sentinel
// (the row is non-existent to the surface). Audited (tool_type.archive).
func (s *Service) ArchiveToolType(ctx context.Context, actorID, id string) (*ToolType, error) {
	if err := s.requireToolTypesPermission(ctx, actorID); err != nil {
		return nil, err
	}

	archived, err := s.store.ArchiveToolType(ctx, id)
	if err != nil {
		if errors.Is(err, ErrToolTypeNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("tools core: failed to archive tool type: %w", err)
	}

	s.auditToolType(ctx, actorID, AuditOperationToolTypeArchive, "action=archive target=tool_type id="+id)
	return archived, nil
}

// requireToolTypesPermission re-verifies (defense-in-depth, AD-6) that the
// actor's LIVE permission set holds tool_types.manage. The route gateway
// already enforces it; the core re-checks so no future direct caller can skip
// it. An empty actor ID never passes.
func (s *Service) requireToolTypesPermission(ctx context.Context, actorID string) error {
	if actorID == "" {
		return ErrForbidden
	}
	perms, err := s.perms.ListPermissionsByUser(ctx, actorID)
	if err != nil {
		return fmt.Errorf("tools core: failed to resolve actor permissions: %w", err)
	}
	for _, p := range perms {
		if p == ToolTypesManagePermission {
			return nil
		}
	}
	return ErrForbidden
}

// auditToolType writes the audit row best-effort (NFR-O1): a failed audit
// write is logged, never rolled back into the triggering operation.
func (s *Service) auditToolType(ctx context.Context, actorID, operation, detail string) {
	if err := s.audit.InsertAuditEvent(ctx, actorID, operation, detail, AuditSeverityNormal); err != nil {
		s.log().Warn("tools core: tool type audit write failed", "operation", operation, "error", err)
	}
}

// validateToolTypeFKs validates the cross-module FK references through the
// modules' read-only ports (AD-7/AD-10/AD-11): the default schedule must be an
// ACTIVE schedule in the Admin catalog (CREATE_BAD_SCHEDULE) and the required
// qualification must exist in the User vocabulary (CREATE_BAD_QUALIFICATION).
// A nil port is a composition-root wiring defect and FAILS LOUDLY (a
// 500-style internal error), never a silent skip — skipping would bypass the
// AD-7/AD-16 validation entirely.
func (s *Service) validateToolTypeFKs(ctx context.Context, input ToolTypeInput) error {
	if s.schedules == nil {
		return fmt.Errorf("tools core: schedule catalog port is not wired")
	}
	if s.qualifications == nil {
		return fmt.Errorf("tools core: qualification catalog port is not wired")
	}

	schedules, err := s.schedules.CurrentSchedules(ctx)
	if err != nil {
		return fmt.Errorf("tools core: failed to resolve schedule catalog: %w", err)
	}
	found := false
	for _, sch := range schedules {
		if sch.ID == strings.TrimSpace(input.DefaultScheduleID) {
			found = true
			break
		}
	}
	if !found {
		return &InvalidToolTypeError{Message: MsgToolTypeInvalidSchedule}
	}

	if strings.TrimSpace(input.RequiredQualificationID) != "" {
		exists, err := s.qualifications.QualificationExists(ctx, strings.TrimSpace(input.RequiredQualificationID))
		if err != nil {
			return fmt.Errorf("tools core: failed to resolve qualification catalog: %w", err)
		}
		if !exists {
			return &InvalidToolTypeError{Message: MsgToolTypeInvalidQualification}
		}
	}
	return nil
}

// validateToolTypeInput enforces the create/update invariants (CREATE_INVALID):
// non-empty bounded name (counted in RUNES), a valid inspection mode, and — in
// checklist mode — a NON-EMPTY bounded ordered checklist-item list of
// non-empty bounded, case-insensitively-unique labels. The cross-module FK ids
// must be present (their validity is checked against the ports by
// validateToolTypeFKs).
func validateToolTypeInput(input ToolTypeInput) error {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return &InvalidToolTypeError{Message: "Bitte gib einen Namen für den Gerätetyp an."}
	}
	if utf8.RuneCountInString(name) > 255 {
		return &InvalidToolTypeError{Message: "Der Name ist zu lang."}
	}
	if strings.TrimSpace(input.DefaultScheduleID) == "" {
		return &InvalidToolTypeError{Message: "Bitte wähle einen Standard-Zeitplan aus."}
	}
	// required_qualification_id is OPTIONAL (most tools need no specific
	// qualification — any Helfer*in may inspect them, FR-8/FR-11): a present
	// value must reference an existing vocabulary row (validated against the
	// User QualificationCatalogPort by validateToolTypeFKs), an empty one is
	// allowed and stored as NULL.
	switch input.InspectionMode {
	case InspectionModePassFail, InspectionModeChecklist:
	default:
		return &InvalidToolTypeError{Message: "Bitte wähle einen gültigen Prüfmodus (Pass/Fail oder Checkliste)."}
	}
	if input.InspectionMode == InspectionModeChecklist {
		if len(input.Items) == 0 {
			return &InvalidToolTypeError{Message: MsgToolTypeChecklistRequired}
		}
		if len(input.Items) > ToolTypeChecklistItemMaxCount {
			return &InvalidToolTypeError{Message: "Die Checkliste enthält zu viele Einträge."}
		}
		seen := make(map[string]struct{}, len(input.Items))
		for i, item := range input.Items {
			label := strings.TrimSpace(item.Label)
			if label == "" {
				return &InvalidToolTypeError{Message: fmt.Sprintf("Eintrag %d der Checkliste braucht einen Text.", i+1)}
			}
			if utf8.RuneCountInString(label) > 255 {
				return &InvalidToolTypeError{Message: fmt.Sprintf("Der Text von Eintrag %d ist zu lang.", i+1)}
			}
			key := strings.ToLower(label)
			if _, dup := seen[key]; dup {
				return &InvalidToolTypeError{Message: MsgToolTypeChecklistDuplicate}
			}
			seen[key] = struct{}{}
		}
	}
	return nil
}

// buildItems converts the ordered input items into the persisted domain items,
// assigning explicit positions from the array order. In pass_fail mode the
// items list is always empty (no checklist template).
func buildItems(mode string, inputs []ToolTypeChecklistItemInput) []ToolTypeChecklistItem {
	if mode != InspectionModeChecklist {
		return nil
	}
	items := make([]ToolTypeChecklistItem, 0, len(inputs))
	for i, in := range inputs {
		items = append(items, ToolTypeChecklistItem{
			Position: i,
			Label:    strings.TrimSpace(in.Label),
		})
	}
	return items
}

// ensureUniqueToolTypeName rejects a name already held by another ACTIVE tool
// type (case-insensitive, finding): two ACTIVE types must not share a name.
// exceptID excludes the type being updated from the comparison.
//
// The overall duplicate-name semantics are ONE coherent rule split across two
// guards:
//   - This core guard: case-insensitive over the ACTIVE catalog only — a
//     case-variant of an active name is rejected here.
//   - The DB UNIQUE(name) backstop: case-sensitive over ALL rows (active AND
//     archived). An EXACT reuse of an archived name trips it and the store maps
//     SQLSTATE 23505 to the same German duplicate-name 400 (an archived row is
//     out of the active surface but still reserves its exact name forever).
//   - A CASE-VARIANT of an archived name is deliberately ALLOWED (the DB
//     UNIQUE is case-sensitive and the archived row is out of the surface).
func (s *Service) ensureUniqueToolTypeName(ctx context.Context, name, exceptID string) error {
	types, err := s.store.ListToolTypes(ctx)
	if err != nil {
		return fmt.Errorf("tools core: failed to check tool type name uniqueness: %w", err)
	}
	for _, tt := range types {
		if tt.ID == exceptID {
			continue
		}
		if strings.EqualFold(tt.Name, name) {
			return &InvalidToolTypeError{Message: MsgToolTypeNameTaken}
		}
	}
	return nil
}
