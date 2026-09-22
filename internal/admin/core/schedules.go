package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Interval units for the named schedule catalog (FR-30/AD-16). Stored verbatim
// in schedules.interval_unit. The reserved weekday/time parts are unused in V1
// (lenient validation, AD-16).
const (
	IntervalUnitYear    = "year"
	IntervalUnitQuarter = "quarter"
	IntervalUnitMonth   = "month"
	IntervalUnitWeek    = "week"
	IntervalUnitDay     = "day"
)

// ErrScheduleNotFound is returned when an update/archive references an id that
// does not exist (or is not a valid uuidv7) — INCLUDING an already-archived
// row, which the surface treats as non-existent (soft archive is irreversible
// in V1, so editing/archiving an archived schedule answers the 404 sentinel).
// Handlers map it to the uniform 404.
var ErrScheduleNotFound = errors.New("admin core: schedule not found")

// ErrSchedulesInvalid is the sentinel wrapping a German validation message for
// a 400 invalid_request (empty name, bad interval unit, non-positive
// interval magnitude).
var ErrSchedulesInvalid = errors.New("admin core: invalid schedule")

// InvalidSchedulesError carries the German validation message for a 400
// invalid_request. It unwraps to ErrSchedulesInvalid so callers can match the
// sentinel while still rendering the field-specific microcopy.
type InvalidSchedulesError struct {
	Message string
}

func (e *InvalidSchedulesError) Error() string { return e.Message }
func (e *InvalidSchedulesError) Unwrap() error { return ErrSchedulesInvalid }

// German microcopy for the schedule-catalog surface (FR-30/UX-DR8).
const (
	MsgScheduleSaved    = "Zeitplan gespeichert."
	MsgScheduleArchived = "Zeitplan archiviert."
	// MsgScheduleNameTaken rejects a create/update whose name another schedule
	// already holds (case-insensitive duplicate-name guard, finding).
	MsgScheduleNameTaken = "Es gibt bereits einen Zeitplan mit diesem Namen."
)

// Schedule is the domain representation of one schedule-catalog row
// (FR-30/AD-16). WeekdaySet and TimeOfDay are the reserved nullable
// composite-timing fields: stored but unused/never validated in V1. ArchivedAt
// is nil while the schedule is active and set (soft-archive) otherwise.
type Schedule struct {
	ID                string
	Name              string
	IntervalUnit      string
	IntervalMagnitude int
	WeekdaySet        []string
	TimeOfDay         *string
	ArchivedAt        *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// ScheduleInput is the shared POST/PUT body. weekday_set/time_of_day are NOT
// part of the input: they are reserved nullable columns the surface stores as
// NULL and ignores in V1 (AD-16).
type ScheduleInput struct {
	Name              string `json:"name"`
	IntervalUnit      string `json:"interval_unit"`
	IntervalMagnitude int    `json:"interval_magnitude"`
}

// SchedulesStore is the outbound persistence port over the Admin-owned
// schedules table (AD-11/AD-16). ListSchedules returns only ACTIVE rows
// (archived_at IS NULL). UpdateSchedule/ArchiveSchedule refuse an
// already-archived row (and a missing id) with ErrScheduleNotFound — the
// archived row is treated as non-existent by the surface.
type SchedulesStore interface {
	ListSchedules(ctx context.Context) ([]*Schedule, error)
	CreateSchedule(ctx context.Context, schedule *Schedule) (*Schedule, error)
	UpdateSchedule(ctx context.Context, schedule *Schedule) (*Schedule, error)
	ArchiveSchedule(ctx context.Context, id string) (*Schedule, error)
}

// ListSchedules returns every ACTIVE schedule, oldest first (GET_LIST /
// GET_LIST_EMPTY). Archived rows are filtered by the store (archived_at IS
// NULL) and never reach the surface.
func (s *Service) ListSchedules(ctx context.Context, actorID string) ([]*Schedule, error) {
	if err := s.requireSchedulePermission(ctx, actorID); err != nil {
		return nil, err
	}
	return s.CurrentSchedules(ctx)
}

// CurrentSchedules implements the read-only SchedulesPort (AD-16) that the
// future Tool module (tool-type default / per-tool choice, Story 4.2) will
// consume — it reads the active catalog here, never a copy. No actor, no
// permission re-check: this is the trusted internal read path.
func (s *Service) CurrentSchedules(ctx context.Context) ([]*Schedule, error) {
	schedules, err := s.schedulesStore.ListSchedules(ctx)
	if err != nil {
		return nil, fmt.Errorf("admin core: failed to list schedules: %w", err)
	}
	return schedules, nil
}

// CreateSchedule persists a new schedule (CREATE_VALID / CREATE_DUPLICATE /
// CREATE_INVALID): name must be non-empty and unique (case-insensitive), the
// unit must be in the catalog set, the magnitude must be positive; the
// reserved weekday/time fields stay NULL. Audited (schedule.create).
func (s *Service) CreateSchedule(ctx context.Context, actorID string, input ScheduleInput) (*Schedule, error) {
	if err := s.requireSchedulePermission(ctx, actorID); err != nil {
		return nil, err
	}
	if err := validateSchedule(input); err != nil {
		return nil, err
	}

	schedule := &Schedule{
		Name:              strings.TrimSpace(input.Name),
		IntervalUnit:      input.IntervalUnit,
		IntervalMagnitude: input.IntervalMagnitude,
	}

	if err := s.ensureUniqueScheduleName(ctx, schedule.Name, ""); err != nil {
		return nil, err
	}

	persisted, err := s.schedulesStore.CreateSchedule(ctx, schedule)
	if err != nil {
		return nil, fmt.Errorf("admin core: failed to persist schedule: %w", err)
	}

	s.auditSchedule(ctx, actorID, AuditOperationScheduleCreate, "action=create target=schedule id="+persisted.ID)
	return persisted, nil
}

// UpdateSchedule persists a schedule (UPDATE_VALID): name/interval are
// replaced, updated_at refreshed; weekday/time stay untouched (NULL in V1).
// Updating an already-archived row answers the 404 sentinel (soft archive is
// irreversible — the archived row is non-existent to the surface,
// UPDATE_ARCHIVED). Audited (schedule.update).
func (s *Service) UpdateSchedule(ctx context.Context, actorID, id string, input ScheduleInput) (*Schedule, error) {
	if err := s.requireSchedulePermission(ctx, actorID); err != nil {
		return nil, err
	}
	if err := validateSchedule(input); err != nil {
		return nil, err
	}

	schedule := &Schedule{
		ID:                id,
		Name:              strings.TrimSpace(input.Name),
		IntervalUnit:      input.IntervalUnit,
		IntervalMagnitude: input.IntervalMagnitude,
	}

	if err := s.ensureUniqueScheduleName(ctx, schedule.Name, id); err != nil {
		return nil, err
	}

	persisted, err := s.schedulesStore.UpdateSchedule(ctx, schedule)
	if err != nil {
		if errors.Is(err, ErrScheduleNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("admin core: failed to persist schedule: %w", err)
	}

	s.auditSchedule(ctx, actorID, AuditOperationScheduleUpdate, "action=update target=schedule id="+id)
	return persisted, nil
}

// ArchiveSchedule soft-archives one schedule (ARCHIVE): archived_at is set, the
// row leaves the active list, and it cannot be edited/unarchived via the
// surface in V1. Archiving an already-archived row answers the 404 sentinel
// (ARCHIVE_ARCHIVED — the row is non-existent to the surface). Audited
// (schedule.archive).
func (s *Service) ArchiveSchedule(ctx context.Context, actorID, id string) (*Schedule, error) {
	if err := s.requireSchedulePermission(ctx, actorID); err != nil {
		return nil, err
	}

	archived, err := s.schedulesStore.ArchiveSchedule(ctx, id)
	if err != nil {
		if errors.Is(err, ErrScheduleNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("admin core: failed to archive schedule: %w", err)
	}

	s.auditSchedule(ctx, actorID, AuditOperationScheduleArchive, "action=archive target=schedule id="+id)
	return archived, nil
}

// requireSchedulePermission re-verifies (defense-in-depth, AD-6) that the
// actor's LIVE permission set holds schedules.manage. The route gateway already
// enforces it; the core re-checks so no future direct caller can skip it. An
// empty actor ID never passes.
func (s *Service) requireSchedulePermission(ctx context.Context, actorID string) error {
	if actorID == "" {
		return ErrForbidden
	}
	perms, err := s.perms.ListPermissionsByUser(ctx, actorID)
	if err != nil {
		return fmt.Errorf("admin core: failed to resolve actor permissions: %w", err)
	}
	for _, p := range perms {
		if p == SchedulesPermission {
			return nil
		}
	}
	return ErrForbidden
}

// auditSchedule writes the audit row best-effort (NFR-O1): a failed audit
// write is logged, never rolled back into the triggering operation.
func (s *Service) auditSchedule(ctx context.Context, actorID, operation, detail string) {
	if err := s.audit.InsertAuditEvent(ctx, actorID, operation, detail, AuditSeverityNormal); err != nil {
		s.log().Warn("admin core: schedule audit write failed", "operation", operation, "error", err)
	}
}

// validateSchedule enforces the create/update invariants (CREATE_INVALID):
// non-empty bounded name (counted in RUNES, so a client-side maxLength in
// UTF-16 units and a server-side byte count can never diverge for umlaut-heavy
// names), valid interval unit, positive bounded magnitude. The reserved
// weekday_set/time_of_day fields are deliberately NOT validated
// (stored-but-ignored in V1, lenient validation AD-16).
func validateSchedule(input ScheduleInput) error {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return &InvalidSchedulesError{Message: "Bitte gib einen Namen für den Zeitplan an."}
	}
	if utf8.RuneCountInString(name) > 255 {
		return &InvalidSchedulesError{Message: "Der Name ist zu lang."}
	}
	switch input.IntervalUnit {
	case IntervalUnitYear, IntervalUnitQuarter, IntervalUnitMonth, IntervalUnitWeek, IntervalUnitDay:
	default:
		return &InvalidSchedulesError{Message: "Bitte wähle eine gültige Zeiteinheit (Jahr, Quartal, Monat, Woche oder Tag)."}
	}
	if input.IntervalMagnitude < 1 {
		return &InvalidSchedulesError{Message: "Bitte gib eine positive Intervallgröße an."}
	}
	// The magnitude upper bound is APP-LEVEL ONLY (a defensive cap keeping the
	// value inside the DB's int32 range and the SPA's number input sane). The
	// shipped DB CHECK is just `interval_magnitude > 0`; widening it would
	// require editing the applied migration 000019, which is not allowed — so
	// the cap lives here and in the SPA's <input max>, and is deliberately
	// generous (a million units of the chosen interval is beyond any V1 need).
	if input.IntervalMagnitude > 1000000 {
		return &InvalidSchedulesError{Message: "Die Intervallgröße ist zu groß."}
	}
	return nil
}

// ensureUniqueScheduleName rejects a name already held by another ACTIVE
// schedule (case-insensitive, finding): two schedules must not share a name.
// exceptID excludes the schedule being updated from the comparison. The
// comparison runs over the active catalog only (archived rows are out of the
// surface and cannot be reused by a new row anyway — the DB's UNIQUE name
// constraint is the final backstop).
func (s *Service) ensureUniqueScheduleName(ctx context.Context, name, exceptID string) error {
	schedules, err := s.schedulesStore.ListSchedules(ctx)
	if err != nil {
		return fmt.Errorf("admin core: failed to check schedule name uniqueness: %w", err)
	}
	for _, sch := range schedules {
		if sch.ID == exceptID {
			continue
		}
		if strings.EqualFold(sch.Name, name) {
			return &InvalidSchedulesError{Message: MsgScheduleNameTaken}
		}
	}
	return nil
}
