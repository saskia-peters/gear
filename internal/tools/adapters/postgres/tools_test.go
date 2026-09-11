package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saskia-peters/gear/internal/tools/core"
)

// activeTestTools filters a ListTools result to the test-% rows (case-
// insensitive) that this suite owns, so the count/order assertions stay
// correct even when real user-created tools exist in the shared dev DB.
func activeTestTools(tools []*core.Tool) []*core.Tool {
	out := tools[:0]
	for _, t := range tools {
		if strings.HasPrefix(strings.ToLower(t.Name), "test-") {
			out = append(out, t)
		}
	}
	return out
}

// seedToolRefs inserts one Test- schedule (Admin-owned catalog) and one Test-
// tool type (Tool-owned, referencing the schedule) and returns their ids. The
// rows are cleaned up by the caller. Cleanup order matters: `tools` rows must
// be deleted BEFORE the `tool_types`/`schedules` rows they FK-reference.
func seedToolRefs(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (toolTypeID, scheduleID string) {
	t.Helper()
	// Case-insensitive cleanup (case-variants leak past a case-sensitive LIKE).
	if _, err := pool.Exec(ctx, "DELETE FROM tools WHERE lower(name) LIKE 'test-%'"); err != nil {
		t.Fatalf("cleanup tools err = %v", err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM tool_types WHERE lower(name) LIKE 'test-%'"); err != nil {
		t.Fatalf("cleanup tool_types err = %v", err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM schedules WHERE lower(name) LIKE 'test-%'"); err != nil {
		t.Fatalf("cleanup schedules err = %v", err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM qualifications WHERE lower(name) LIKE 'test-%'"); err != nil {
		t.Fatalf("cleanup qualifications err = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM tools WHERE lower(name) LIKE 'test-%'")
		_, _ = pool.Exec(ctx, "DELETE FROM tool_types WHERE lower(name) LIKE 'test-%'")
		_, _ = pool.Exec(ctx, "DELETE FROM schedules WHERE lower(name) LIKE 'test-%'")
		_, _ = pool.Exec(ctx, "DELETE FROM qualifications WHERE lower(name) LIKE 'test-%'")
	})

	if err := pool.QueryRow(ctx,
		"INSERT INTO schedules (name, interval_unit, interval_magnitude) VALUES ('Test-Zeitplan', 'year', 1) RETURNING id",
	).Scan(&scheduleID); err != nil {
		t.Fatalf("seeding schedule err = %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO tool_types (name, default_schedule_id, required_qualification_id, inspection_mode)
		 VALUES ('Test-Geraetetyp', $1, NULL, 'pass_fail') RETURNING id`, scheduleID,
	).Scan(&toolTypeID); err != nil {
		t.Fatalf("seeding tool type err = %v", err)
	}
	return toolTypeID, scheduleID
}

// TestPostgresToolsStore exercises the CRUD + override-clear + soft-archive
// round-trip over the dev database (migration 000024 applied), plus the
// optional-override NULL semantics. It covers GET_LIST_EMPTY, GET_LIST (active
// filter + JOINed type name), CREATE_VALID (empty override → SQL NULL),
// CREATE_OVERRIDE (FK override), UPDATE_CLEAR_OVERRIDE (override → NULL),
// UPDATE_ARCHIVED / ARCHIVE_ARCHIVED (404 sentinel), ARCHIVE (archived_at set +
// leaves active list) and the German duplicate-name 400.
func TestPostgresToolsStore(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	// Close the pool AFTER the row-cleanup DELETEs below run (t.Cleanup runs in
	// LIFO order: registering the close first means the DELETE runs before it).
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	toolTypeID, scheduleID := seedToolRefs(t, ctx, pool)

	// GET_LIST_EMPTY: after cleanup, there are no TEST- rows (real user-created
	// tools may exist in the shared dev DB — the assertions below count only
	// the test-% rows, the documented isolation convention).
	initial, err := repo.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools(initial) err = %v", err)
	}
	if len(activeTestTools(initial)) != 0 {
		t.Fatalf("initial = %d test rows, want 0", len(activeTestTools(initial)))
	}

	// CREATE_VALID: a tool WITHOUT a schedule override is persisted with SQL
	// NULL schedule_id (AD-5: inherits the type default) and the JOINed type
	// name.
	created, err := repo.CreateTool(ctx, &core.Tool{
		Name:       "Test-Bohrmaschine-01",
		ToolTypeID: toolTypeID,
		ScheduleID: "",
	})
	if err != nil {
		t.Fatalf("CreateTool err = %v", err)
	}
	if created.ID == "" || len(created.ID) != 36 {
		t.Errorf("id = %q, want a generated uuid", created.ID)
	}
	if created.Name != "Test-Bohrmaschine-01" || created.ToolTypeID != toolTypeID {
		t.Errorf("created = %+v, want persisted values", created)
	}
	if created.ToolTypeName != "Test-Geraetetyp" {
		t.Errorf("tool_type_name = %q, want the JOINed type name", created.ToolTypeName)
	}
	if created.ScheduleID != "" {
		t.Errorf("schedule_id = %q, want empty (NULL → inherit)", created.ScheduleID)
	}
	if len(created.Attributes) != 0 {
		t.Errorf("attributes = %v, want empty map from '{}' default", created.Attributes)
	}
	if created.ArchivedAt != nil {
		t.Error("new tool must be active (archived_at NULL)")
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Errorf("timestamps missing: %+v", created)
	}

	// The empty override is stored as SQL NULL (never a zero uuid).
	var dbSchedule *string
	if err := pool.QueryRow(ctx,
		"SELECT schedule_id FROM tools WHERE id = $1", created.ID,
	).Scan(&dbSchedule); err != nil {
		t.Fatalf("scan schedule_id err = %v", err)
	}
	if dbSchedule != nil {
		t.Errorf("DB schedule_id = %v, want SQL NULL (inherit)", *dbSchedule)
	}

	// GET_LIST: the created tool is returned with its JOINed type name.
	list, err := repo.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools err = %v", err)
	}
	testRows := activeTestTools(list)
	if len(testRows) != 1 || testRows[0].ID != created.ID {
		t.Fatalf("test rows = %d, want the created tool", len(testRows))
	}
	if testRows[0].ToolTypeName != "Test-Geraetetyp" {
		t.Errorf("listed tool_type_name = %q, want the JOINed type name", testRows[0].ToolTypeName)
	}

	// CREATE_OVERRIDE: a second tool with a valid ACTIVE schedule override is
	// persisted with the FK override.
	withOverride, err := repo.CreateTool(ctx, &core.Tool{
		Name:       "Test-Bohrmaschine-02",
		ToolTypeID: toolTypeID,
		ScheduleID: scheduleID,
	})
	if err != nil {
		t.Fatalf("CreateTool(override) err = %v", err)
	}
	if withOverride.ScheduleID != scheduleID {
		t.Errorf("override = %q, want %q", withOverride.ScheduleID, scheduleID)
	}

	// UPDATE_CLEAR_OVERRIDE: editing the overridden tool and clearing the
	// override stores SQL NULL again (the tool inherits its type's default).
	updated, err := repo.UpdateTool(ctx, &core.Tool{
		ID:         withOverride.ID,
		Name:       "Test-Bohrmaschine-02-neu",
		ToolTypeID: toolTypeID,
		ScheduleID: "",
	})
	if err != nil {
		t.Fatalf("UpdateTool(clear override) err = %v", err)
	}
	if updated.ScheduleID != "" {
		t.Errorf("updated schedule override = %q, want empty (cleared to NULL)", updated.ScheduleID)
	}
	var dbScheduleAfter *string
	if err := pool.QueryRow(ctx,
		"SELECT schedule_id FROM tools WHERE id = $1", withOverride.ID,
	).Scan(&dbScheduleAfter); err != nil {
		t.Fatalf("scan schedule_id(after clear) err = %v", err)
	}
	if dbScheduleAfter != nil {
		t.Errorf("DB schedule_id after clear = %v, want SQL NULL", *dbScheduleAfter)
	}
	if updated.Name != "Test-Bohrmaschine-02-neu" {
		t.Errorf("updated name = %q", updated.Name)
	}
	if !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Errorf("updated_at = %v, want after create's %v", updated.UpdatedAt, created.UpdatedAt)
	}

	// ARCHIVE: archived_at set, the row leaves the active list.
	archived, err := repo.ArchiveTool(ctx, created.ID)
	if err != nil {
		t.Fatalf("ArchiveTool err = %v", err)
	}
	if archived.ArchivedAt == nil {
		t.Fatal("archived_at = nil, want set")
	}
	list, err = repo.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools(after archive) err = %v", err)
	}
	testRows = activeTestTools(list)
	if len(testRows) != 1 || testRows[0].ID != withOverride.ID {
		t.Fatalf("test rows = %d, want only the non-archived tool", len(testRows))
	}

	// ARCHIVE_ARCHIVED: archiving the already-archived row → 404 sentinel.
	if _, err := repo.ArchiveTool(ctx, created.ID); !errors.Is(err, core.ErrToolNotFound) {
		t.Fatalf("ArchiveTool(archived) err = %v, want ErrToolNotFound", err)
	}
	// UPDATE_ARCHIVED: updating the archived row → 404 sentinel.
	if _, err := repo.UpdateTool(ctx, &core.Tool{
		ID: created.ID, Name: "Test-Darf-Nicht", ToolTypeID: toolTypeID,
	}); !errors.Is(err, core.ErrToolNotFound) {
		t.Fatalf("UpdateTool(archived) err = %v, want ErrToolNotFound", err)
	}
	// UPDATE missing id → 404 sentinel.
	if _, err := repo.UpdateTool(ctx, &core.Tool{
		ID: "00000000-0000-0000-0000-000000000000", Name: "x", ToolTypeID: toolTypeID,
	}); !errors.Is(err, core.ErrToolNotFound) {
		t.Fatalf("UpdateTool(missing) err = %v, want ErrToolNotFound", err)
	}
	// ARCHIVE missing id → 404 sentinel.
	if _, err := repo.ArchiveTool(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, core.ErrToolNotFound) {
		t.Fatalf("ArchiveTool(missing) err = %v, want ErrToolNotFound", err)
	}
	// The lean active-exists check: true for an active id, false for a missing
	// id and for the archived row.
	active, err := repo.ToolExistsActive(ctx, withOverride.ID)
	if err != nil {
		t.Fatalf("ToolExistsActive err = %v", err)
	}
	if !active {
		t.Error("ToolExistsActive(active) = false, want true")
	}
	active, err = repo.ToolExistsActive(ctx, created.ID)
	if err != nil {
		t.Fatalf("ToolExistsActive(archived) err = %v", err)
	}
	if active {
		t.Error("ToolExistsActive(archived) = true, want false (the row is archived)")
	}

	// Re-creating the EXACT archived name is rejected with the German
	// duplicate-name 400 (the DB UNIQUE constraint is the backstop the active
	// catalog guard cannot see) — never a 500.
	_, err = repo.CreateTool(ctx, &core.Tool{
		Name: "Test-Bohrmaschine-01", ToolTypeID: toolTypeID,
	})
	var inv *core.InvalidToolError
	if !errors.As(err, &inv) {
		t.Fatalf("recreate archived name err = %v, want *InvalidToolError (German duplicate)", err)
	}
	if inv.Message != core.MsgToolNameTaken {
		t.Errorf("message = %q, want %q", inv.Message, core.MsgToolNameTaken)
	}

	// UPDATE to a name already held by ANOTHER ACTIVE tool trips the DB UNIQUE
	// constraint and is mapped to the German duplicate-name 400 — the DB-level
	// backstop for the update path (the core's active case-insensitive guard
	// would catch a live duplicate first; this pins the repo mapping).
	conflictTool, err := repo.CreateTool(ctx, &core.Tool{
		Name: "Test-Konflikt", ToolTypeID: toolTypeID,
	})
	if err != nil {
		t.Fatalf("CreateTool(conflict target) err = %v", err)
	}
	_, err = repo.UpdateTool(ctx, &core.Tool{
		ID: withOverride.ID, Name: "Test-Konflikt", ToolTypeID: toolTypeID,
	})
	inv = nil
	if !errors.As(err, &inv) {
		t.Fatalf("update to held name err = %v, want *InvalidToolError (German duplicate)", err)
	}
	if inv.Message != core.MsgToolNameTaken {
		t.Errorf("message = %q, want %q", inv.Message, core.MsgToolNameTaken)
	}
	// The rejected update leaves BOTH active tools unchanged (withOverride keeps
	// its name; conflictTool stays).
	list, err = repo.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools(after conflict) err = %v", err)
	}
	testRows = activeTestTools(list)
	if len(testRows) != 2 {
		t.Fatalf("test rows = %d, want 2 active tools (the rejected update persisted nothing)", len(testRows))
	}
	foundConflict := false
	for _, tool := range testRows {
		if tool.ID == conflictTool.ID {
			foundConflict = true
		}
	}
	if !foundConflict {
		t.Error("conflict target missing from the active list")
	}
}

// TestPostgresToolsFKConstraints verifies the DB enforces the FKs (AD-10): a
// create referencing a non-existent tool type (intra-module FK) or schedule
// override (cross-module FK) is MAPPED by the repository to the German 400 —
// the raw pg error must never surface as a 500. The raw DB constraint is
// additionally pinned with a direct INSERT expecting 23503.
func TestPostgresToolsFKConstraints(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	toolTypeID, _ := seedToolRefs(t, ctx, pool)

	missing := "00000000-0000-0000-0000-000000000000"

	// Bad tool_type FK → mapped to the German 400 (intra-module FK, AD-10).
	_, err := repo.CreateTool(ctx, &core.Tool{
		Name: "Test-Fk-Type", ToolTypeID: missing,
	})
	var inv *core.InvalidToolError
	if !errors.As(err, &inv) {
		t.Fatalf("create with missing type err = %v, want *InvalidToolError (German 400)", err)
	}
	if inv.Message != core.MsgToolReferencedGone {
		t.Errorf("message = %q, want %q", inv.Message, core.MsgToolReferencedGone)
	}

	// Bad schedule override FK → mapped to the German 400.
	_, err = repo.CreateTool(ctx, &core.Tool{
		Name: "Test-Fk-Override", ToolTypeID: toolTypeID, ScheduleID: missing,
	})
	inv = nil
	if !errors.As(err, &inv) {
		t.Fatalf("create with missing override err = %v, want *InvalidToolError (German 400)", err)
	}
	if inv.Message != core.MsgToolReferencedGone {
		t.Errorf("message = %q, want %q", inv.Message, core.MsgToolReferencedGone)
	}

	// The raw DB backstop still exists: a direct INSERT with a missing tool
	// type FK trips SQLSTATE 23503.
	_, err = pool.Exec(ctx,
		`INSERT INTO tools (name, tool_type_id) VALUES ('Test-Fk-Raw', $1)`, missing)
	if !isForeignKeyViolation(err) {
		t.Fatalf("raw insert with missing type err = %v, want FK violation 23503", err)
	}
}