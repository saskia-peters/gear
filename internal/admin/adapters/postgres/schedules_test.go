package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/saskia-peters/gear/internal/admin/core"
)

// TestPostgresSchedulesSeed pins the migration 000019 seed catalog (AD-16): the
// EXACT five canonical intervals are present, ACTIVE, and in a deterministic
// order (created_at ASC, name ASC — the seeds share one now() created_at).
func TestPostgresSchedulesSeed(t *testing.T) {
	pool := adminTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM schedules WHERE name LIKE 'Test-%'") })
	if _, err := pool.Exec(ctx, "DELETE FROM schedules WHERE name LIKE 'Test-%'"); err != nil {
		t.Fatalf("cleanup err = %v", err)
	}
	repo := NewRepository(New(pool))

	seeded := []struct {
		name string
		unit string
		magn int
	}{
		{"1 month", core.IntervalUnitMonth, 1},
		{"1 quarter", core.IntervalUnitQuarter, 1},
		{"1 year", core.IntervalUnitYear, 1},
		{"2 weeks", core.IntervalUnitWeek, 2},
		{"3 days", core.IntervalUnitDay, 3},
	}

	got, err := repo.ListSchedules(ctx)
	if err != nil {
		t.Fatalf("ListSchedules err = %v", err)
	}
	// EXACT count: after removing any test rows, the active catalog is exactly
	// the five seed rows (no stray rows may appear or go missing).
	if len(got) != len(seeded) {
		t.Fatalf("list = %d rows, want exactly %d seed rows", len(got), len(seeded))
	}
	for i, want := range seeded {
		s := got[i]
		if s.Name != want.name || s.IntervalUnit != want.unit || s.IntervalMagnitude != want.magn {
			t.Errorf("row %d = %q (%s/%d), want seed %q (%s/%d)", i, s.Name, s.IntervalUnit, s.IntervalMagnitude, want.name, want.unit, want.magn)
		}
		if s.ArchivedAt != nil {
			t.Errorf("seed row %q is archived, want active", s.Name)
		}
	}
}

// TestPostgresSchedulesStore exercises the CRUD + soft-archive round-trip over
// the dev database (migration 000019 applied), mirroring the backup store test.
// It covers GET_LIST_EMPTY, GET_LIST (active filter), CREATE_VALID, GET by id,
// UPDATE_VALID (updated_at refresh + archived-refusal), ARCHIVE (archived_at
// set + leaves active list) and ARCHIVE_ARCHIVED (404 sentinel).
func TestPostgresSchedulesStore(t *testing.T) {
	pool := adminTestPool(t)
	ctx := context.Background()
	// Close the pool AFTER the row-cleanup DELETE below runs (t.Cleanup runs in
	// LIFO order: registering the close first means the DELETE runs before it).
	t.Cleanup(func() { pool.Close() })
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM schedules WHERE name LIKE 'Test-%'") })

	queries := New(pool)
	repo := NewRepository(queries)

	if _, err := pool.Exec(ctx, "DELETE FROM schedules WHERE name LIKE 'Test-%'"); err != nil {
		t.Fatalf("cleanup err = %v", err)
	}

	// GET_LIST: only the seed rows are active; no archived rows yet. The list
	// is never empty because of the seed catalog.
	initial, err := repo.ListSchedules(ctx)
	if err != nil {
		t.Fatalf("ListSchedules(initial) err = %v", err)
	}
	seedCount := len(initial)

	// CREATE_VALID: a custom schedule is persisted with the generated uuidv7 id,
	// the reserved weekday/time columns stay NULL, archived_at is NULL.
	created, err := repo.CreateSchedule(ctx, &core.Schedule{
		Name: "Test-Jaehrlich", IntervalUnit: core.IntervalUnitYear, IntervalMagnitude: 1,
	})
	if err != nil {
		t.Fatalf("CreateSchedule err = %v", err)
	}
	if created.ID == "" || len(created.ID) != 36 {
		t.Errorf("id = %q, want a generated uuid", created.ID)
	}
	if created.Name != "Test-Jaehrlich" || created.IntervalUnit != core.IntervalUnitYear || created.IntervalMagnitude != 1 {
		t.Errorf("created = %+v, want persisted values", created)
	}
	if created.WeekdaySet != nil {
		t.Errorf("weekday_set = %v, want NULL in V1", created.WeekdaySet)
	}
	if created.TimeOfDay != nil {
		t.Errorf("time_of_day = %v, want NULL in V1", *created.TimeOfDay)
	}
	if created.ArchivedAt != nil {
		t.Error("new schedule must be active (archived_at NULL)")
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Errorf("timestamps missing: %+v", created)
	}
	createdAt := created.CreatedAt
	firstUpdatedAt := created.UpdatedAt

	// GET_LIST: the created row is present alongside the seeds.
	list, err := repo.ListSchedules(ctx)
	if err != nil {
		t.Fatalf("ListSchedules err = %v", err)
	}
	if len(list) != seedCount+1 {
		t.Fatalf("list = %d, want %d (seeds + 1)", len(list), seedCount+1)
	}

	// UPDATE_VALID: name/interval replaced, updated_at refreshed.
	updated, err := repo.UpdateSchedule(ctx, &core.Schedule{
		ID: created.ID, Name: "Test-Zwei-Jahre", IntervalUnit: core.IntervalUnitYear, IntervalMagnitude: 2,
	})
	if err != nil {
		t.Fatalf("UpdateSchedule err = %v", err)
	}
	if updated.Name != "Test-Zwei-Jahre" || updated.IntervalMagnitude != 2 {
		t.Errorf("updated = %+v", updated)
	}
	if !updated.UpdatedAt.After(firstUpdatedAt) || !updated.UpdatedAt.After(createdAt) {
		t.Errorf("updated_at = %v, want after create's %v", updated.UpdatedAt, createdAt)
	}

	// UPDATE missing id → ErrScheduleNotFound.
	if _, err := repo.UpdateSchedule(ctx, &core.Schedule{ID: "00000000-0000-0000-0000-000000000000", Name: "x"}); !errors.Is(err, core.ErrScheduleNotFound) {
		t.Fatalf("UpdateSchedule(missing) err = %v, want ErrScheduleNotFound", err)
	}
	// UPDATE malformed id → ErrScheduleNotFound (no existence hint).
	if _, err := repo.UpdateSchedule(ctx, &core.Schedule{ID: "not-a-uuid", Name: "x"}); !errors.Is(err, core.ErrScheduleNotFound) {
		t.Fatalf("UpdateSchedule(malformed) err = %v, want ErrScheduleNotFound", err)
	}

	// ARCHIVE: archived_at set, the row leaves the active list.
	archived, err := repo.ArchiveSchedule(ctx, created.ID)
	if err != nil {
		t.Fatalf("ArchiveSchedule err = %v", err)
	}
	if archived.ArchivedAt == nil {
		t.Fatal("archived_at = nil, want set")
	}
	list, err = repo.ListSchedules(ctx)
	if err != nil {
		t.Fatalf("ListSchedules(after archive) err = %v", err)
	}
	if len(list) != seedCount {
		t.Fatalf("list = %d, want %d (archived row filtered out)", len(list), seedCount)
	}

	// ARCHIVE_ARCHIVED: archiving the already-archived row → 404 sentinel.
	if _, err := repo.ArchiveSchedule(ctx, created.ID); !errors.Is(err, core.ErrScheduleNotFound) {
		t.Fatalf("ArchiveSchedule(archived) err = %v, want ErrScheduleNotFound", err)
	}
	// ARCHIVE missing id → ErrScheduleNotFound.
	if _, err := repo.ArchiveSchedule(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, core.ErrScheduleNotFound) {
		t.Fatalf("ArchiveSchedule(missing) err = %v, want ErrScheduleNotFound", err)
	}

	// UPDATE_ARCHIVED: updating the archived row → 404 sentinel (the row is
	// non-existent to the surface).
	if _, err := repo.UpdateSchedule(ctx, &core.Schedule{
		ID: created.ID, Name: "Test-Darf-Nicht", IntervalUnit: core.IntervalUnitDay, IntervalMagnitude: 1,
	}); !errors.Is(err, core.ErrScheduleNotFound) {
		t.Fatalf("UpdateSchedule(archived) err = %v, want ErrScheduleNotFound", err)
	}

	// The archived row is still readable by id (history preserved for FK refs,
	// AD-16) but carries the archived timestamp. The store has no single-row
	// reader by design (finding: GetSchedule was dead code and was removed), so
	// this pins the DB row directly.
	var archivedAt time.Time
	if err := pool.QueryRow(ctx, "SELECT archived_at FROM schedules WHERE id = $1", created.ID).Scan(&archivedAt); err != nil {
		t.Fatalf("reading archived row by id err = %v", err)
	}
	if !archivedAt.After(createdAt) {
		t.Errorf("archived_at = %v, want after create %v", archivedAt, createdAt)
	}

	// Re-creating the EXACT archived name is rejected with the German
	// duplicate-name 400 (the DB UNIQUE constraint is the backstop the active
	// catalog guard cannot see) — never a 500.
	_, err = repo.CreateSchedule(ctx, &core.Schedule{
		Name: "Test-Zwei-Jahre", IntervalUnit: core.IntervalUnitYear, IntervalMagnitude: 1,
	})
	var inv *core.InvalidSchedulesError
	if !errors.As(err, &inv) {
		t.Fatalf("recreate archived name err = %v, want *InvalidSchedulesError (German duplicate)", err)
	}
	if inv.Message != core.MsgScheduleNameTaken {
		t.Errorf("message = %q, want %q", inv.Message, core.MsgScheduleNameTaken)
	}

	// UPDATE to an ARCHIVED name: renaming an ACTIVE schedule to a name the
	// active-only uniqueness guard cannot see (it is archived) trips the DB
	// UNIQUE constraint and must map to the German duplicate-name 400 — never a
	// raw pg error / 500.
	active, err := repo.CreateSchedule(ctx, &core.Schedule{
		Name: "Test-Aktiv", IntervalUnit: core.IntervalUnitMonth, IntervalMagnitude: 1,
	})
	if err != nil {
		t.Fatalf("CreateSchedule(active) err = %v", err)
	}
	_, err = repo.UpdateSchedule(ctx, &core.Schedule{
		ID: active.ID, Name: "Test-Zwei-Jahre", IntervalUnit: core.IntervalUnitYear, IntervalMagnitude: 1,
	})
	inv = nil
	if !errors.As(err, &inv) {
		t.Fatalf("update to archived name err = %v, want *InvalidSchedulesError (German duplicate)", err)
	}
	if inv.Message != core.MsgScheduleNameTaken {
		t.Errorf("message = %q, want %q", inv.Message, core.MsgScheduleNameTaken)
	}
}
