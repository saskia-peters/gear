package postgres

import (
	"context"
	"errors"
	"fmt"
	"regexp"
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
	// CREATE_AUTO: the created tool carries an auto-assigned 'GEAR' + 6
	// zero-padded digits inventory number.
	if !strings.HasPrefix(created.InventoryNumber, "GEAR") || len(created.InventoryNumber) != 10 {
		t.Errorf("created inventory_number = %q, want 'GEAR' + 6 zero-padded digits", created.InventoryNumber)
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
	// type FK trips SQLSTATE 23503. (The inventory_number column is NOT NULL —
	// the insert takes a FRESH sequence value so the FK is the only violation.)
	var rawInv string
	if err := pool.QueryRow(ctx,
		`SELECT 'GEAR' || lpad(nextval('tools_inventory_number_seq')::text, 6, '0')`).Scan(&rawInv); err != nil {
		t.Fatalf("reserving a unique raw inventory number err = %v", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO tools (name, tool_type_id, inventory_number) VALUES ('Test-Fk-Raw', $1, $2)`, missing, rawInv)
	if !isForeignKeyViolation(err) {
		t.Fatalf("raw insert with missing type err = %v, want FK violation 23503", err)
	}
}
// toolInventoryNumberFormat is the auto-assigned shape: 'GEAR' + 6 zero-padded
// digits (Story 4-3b, CREATE_AUTO).
var toolInventoryNumberFormat = regexp.MustCompile(`^GEAR\d{6}$`)

// TestPostgresToolInventoryNumbers covers the Story 4-3b inventory-number
// store contract: CREATE_AUTO (monotonic 'GEAR%06d' sequence values), the
// list round-trip, and the DB-level uniqueness backstop (EXACT, over ALL rows
// incl. archived — the Story 4.5 import backstop) mapped to the German
// duplicate-inventory 400.
func TestPostgresToolInventoryNumbers(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	toolTypeID, _ := seedToolRefs(t, ctx, pool)

	// CREATE_AUTO: two creates get distinct, monotonic 'GEAR' + 6 zero-padded
	// numbers (same width → string order == numeric order).
	first, err := repo.CreateTool(ctx, &core.Tool{Name: "Test-Inv-A", ToolTypeID: toolTypeID})
	if err != nil {
		t.Fatalf("CreateTool(A) err = %v", err)
	}
	second, err := repo.CreateTool(ctx, &core.Tool{Name: "Test-Inv-B", ToolTypeID: toolTypeID})
	if err != nil {
		t.Fatalf("CreateTool(B) err = %v", err)
	}
	if !toolInventoryNumberFormat.MatchString(first.InventoryNumber) || !toolInventoryNumberFormat.MatchString(second.InventoryNumber) {
		t.Fatalf("inventory numbers = %q / %q, want 'GEAR' + 6 zero-padded digits", first.InventoryNumber, second.InventoryNumber)
	}
	if first.InventoryNumber == second.InventoryNumber {
		t.Fatalf("inventory numbers collide: %q", first.InventoryNumber)
	}
	if first.InventoryNumber >= second.InventoryNumber {
		t.Errorf("monotonicity broken: %q >= %q", first.InventoryNumber, second.InventoryNumber)
	}

	// ROUND-TRIP: the list returns both numbers on the active catalog.
	list, err := repo.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools err = %v", err)
	}
	seen := map[string]bool{}
	for _, tool := range list {
		if strings.HasPrefix(strings.ToLower(tool.Name), "test-inv-") {
			seen[tool.InventoryNumber] = true
		}
	}
	if !seen[first.InventoryNumber] || !seen[second.InventoryNumber] {
		t.Errorf("round-trip missing numbers: have %v, want %q and %q", seen, first.InventoryNumber, second.InventoryNumber)
	}

	// UNIQUENESS backstop (case-insensitive, all rows): editing a tool onto a
	// number another row already holds — INCLUDING a case-variant — trips the
	// tools_inventory_number_key functional UNIQUE index (lower(...)) → German
	// duplicate-inventory 400 (never a raw 500). The core's active-only
	// case-insensitive guard would catch the ACTIVE variant first; this pins the
	// DB-level backstop for the EXACT same-number case AND the case-variant
	// (which only the functional index can catch).
	_, err = repo.UpdateTool(ctx, &core.Tool{
		ID:              second.ID,
		Name:            "Test-Inv-B",
		ToolTypeID:      toolTypeID,
		InventoryNumber: first.InventoryNumber,
	})
	var inv *core.InvalidToolError
	if !errors.As(err, &inv) {
		t.Fatalf("duplicate-inventory update err = %v, want *InvalidToolError", err)
	}
	if inv.Message != core.MsgToolInventoryNumberTaken {
		t.Errorf("message = %q, want %q", inv.Message, core.MsgToolInventoryNumberTaken)
	}

	// CASE-INSENSITIVITY (finding 3): a case-variant of an ACTIVE number is
	// rejected by the functional index — 'gear000001' collides with
	// 'GEAR000001' (the core guard alone cannot be trusted with a case-blind
	// DB).
	_, err = repo.UpdateTool(ctx, &core.Tool{
		ID:              second.ID,
		Name:            "Test-Inv-B",
		ToolTypeID:      toolTypeID,
		InventoryNumber: strings.ToLower(first.InventoryNumber),
	})
	inv = nil
	if !errors.As(err, &inv) {
		t.Fatalf("case-variant update err = %v, want *InvalidToolError", err)
	}
	if inv.Message != core.MsgToolInventoryNumberTaken {
		t.Errorf("message = %q, want %q", inv.Message, core.MsgToolInventoryNumberTaken)
	}
}

// TestPostgresToolInventoryArchivedBackstop pins the Story 4.5 import backstop
// (finding 2): an ARCHIVED tool's inventory number stays "taken" — updating an
// active tool onto it is rejected (case-insensitively), and a create whose
// auto-assigned nextval lands on an archived number is retried to a fresh
// value instead of 500ing.
func TestPostgresToolInventoryArchivedBackstop(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	toolTypeID, _ := seedToolRefs(t, ctx, pool)

	archived, err := repo.CreateTool(ctx, &core.Tool{Name: "Test-Inv-Arch-A", ToolTypeID: toolTypeID})
	if err != nil {
		t.Fatalf("CreateTool(archived candidate) err = %v", err)
	}
	active, err := repo.CreateTool(ctx, &core.Tool{Name: "Test-Inv-Arch-B", ToolTypeID: toolTypeID})
	if err != nil {
		t.Fatalf("CreateTool(active) err = %v", err)
	}

	// Archive the first tool: its number must stay taken.
	if _, err := repo.ArchiveTool(ctx, archived.ID); err != nil {
		t.Fatalf("ArchiveTool err = %v", err)
	}

	// UPDATE backstop: editing the ACTIVE tool onto the ARCHIVED tool's number
	// (case-insensitively — the lower() functional index) → German duplicate 400.
	_, err = repo.UpdateTool(ctx, &core.Tool{
		ID:              active.ID,
		Name:            "Test-Inv-Arch-B",
		ToolTypeID:      toolTypeID,
		InventoryNumber: strings.ToLower(archived.InventoryNumber),
	})
	var inv *core.InvalidToolError
	if !errors.As(err, &inv) {
		t.Fatalf("update onto archived number err = %v, want *InvalidToolError", err)
	}
	if inv.Message != core.MsgToolInventoryNumberTaken {
		t.Errorf("message = %q, want %q", inv.Message, core.MsgToolInventoryNumberTaken)
	}

	// CREATE retry onto an ARCHIVED number: make the next generated nextval
	// collide with the archived row's (manually re-set) number, then a create
	// must retry to a fresh value.
	var last int64
	var called bool
	if err := pool.QueryRow(ctx, "SELECT last_value, is_called FROM tools_inventory_number_seq").Scan(&last, &called); err != nil {
		t.Fatalf("reading sequence state err = %v", err)
	}
	next := last
	if called {
		next++
	}
	if _, err := pool.Exec(ctx,
		`UPDATE tools SET inventory_number = $1 WHERE id = $2`,
		fmt.Sprintf("GEAR%06d", next), archived.ID,
	); err != nil {
		t.Fatalf("re-setting the archived number err = %v", err)
	}
	created, err := repo.CreateTool(ctx, &core.Tool{Name: "Test-Inv-Arch-C", ToolTypeID: toolTypeID})
	if err != nil {
		t.Fatalf("CreateTool (retry past an archived number) err = %v", err)
	}
	if want := fmt.Sprintf("GEAR%06d", next+1); created.InventoryNumber != want {
		t.Errorf("inventory_number = %q, want %q (the retry advanced past the archived collision)", created.InventoryNumber, want)
	}
}

// TestPostgresCreateToolInventoryCollisionRetry pins the bounded retry loop
// (Story 4-3b, CREATE_COLLISION): when a MANUAL edit consumed the generated
// nextval, the first INSERT trips the UNIQUE index and the repository re-runs
// the INSERT (which computes a FRESH nextval in-SQL) until the budget is
// exhausted → German collision 400.
func TestPostgresCreateToolInventoryCollisionRetry(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	toolTypeID, _ := seedToolRefs(t, ctx, pool)

	// Read the sequence state WITHOUT advancing it: since the sequence has been
	// called (migration backfill + prior creates), the NEXT nextval returns
	// last_value + 1.
	var last int64
	var called bool
	if err := pool.QueryRow(ctx, "SELECT last_value, is_called FROM tools_inventory_number_seq").Scan(&last, &called); err != nil {
		t.Fatalf("reading sequence state err = %v", err)
	}
	next := last
	if called {
		next++
	}

	// Reserve ONLY the first generated value: the auto-assign collides on
	// attempt 1, the retry loop advances to the next fresh number → succeeds.
	reserved := fmt.Sprintf("GEAR%06d", next)
	if _, err := pool.Exec(ctx,
		`INSERT INTO tools (name, tool_type_id, inventory_number) VALUES ('Test-Collision-Reserve', $1, $2)`, toolTypeID, reserved,
	); err != nil {
		t.Fatalf("reserving the next generated number err = %v", err)
	}

	created, err := repo.CreateTool(ctx, &core.Tool{Name: "Test-Collision-Auto", ToolTypeID: toolTypeID})
	if err != nil {
		t.Fatalf("CreateTool (collision retry) err = %v", err)
	}
	if want := fmt.Sprintf("GEAR%06d", next+1); created.InventoryNumber != want {
		t.Errorf("inventory_number = %q, want %q (the retry advanced past the collision)", created.InventoryNumber, want)
	}

	// Reserve the next THREE generated values → every retry attempt collides →
	// the bounded loop exhausts and maps to the German collision 400.
	if err := pool.QueryRow(ctx, "SELECT last_value, is_called FROM tools_inventory_number_seq").Scan(&last, &called); err != nil {
		t.Fatalf("re-reading sequence state err = %v", err)
	}
	cur := last
	if called {
		cur++
	}
	for i := 0; i < 3; i++ {
		if _, err := pool.Exec(ctx,
			`INSERT INTO tools (name, tool_type_id, inventory_number) VALUES ($1, $2, $3)`,
			fmt.Sprintf("Test-Collision-Reserve-%d", i), toolTypeID, fmt.Sprintf("GEAR%06d", cur+int64(i)),
		); err != nil {
			t.Fatalf("reserving colliding number %d err = %v", i, err)
		}
	}
	_, err = repo.CreateTool(ctx, &core.Tool{Name: "Test-Collision-Fail", ToolTypeID: toolTypeID})
	var inv *core.InvalidToolError
	if !errors.As(err, &inv) {
		t.Fatalf("exhausted retry err = %v, want *InvalidToolError (German collision)", err)
	}
	if inv.Message != core.MsgToolInventoryNumberCollision {
		t.Errorf("message = %q, want %q", inv.Message, core.MsgToolInventoryNumberCollision)
	}
}

// TestPostgresToolInventoryBackfill pins the 000025 backfill contract
// (CREATE_BACKFILL): a pre-existing row whose inventory_number was NULL (the
// pre-migration state) is assigned 'GEAR' + zero-padded nextval. The scenario
// is simulated in a rolled-back transaction — the live dev DB is never
// disturbed.
func TestPostgresToolInventoryBackfill(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })

	toolTypeID, _ := seedToolRefs(t, ctx, pool)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx err = %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Recreate the pre-migration state: the column is nullable and the row has
	// no inventory number yet.
	if _, err := tx.Exec(ctx, `ALTER TABLE tools ALTER COLUMN inventory_number DROP NOT NULL`); err != nil {
		t.Fatalf("drop not null err = %v", err)
	}
	var preID string
	if err := tx.QueryRow(ctx,
		`INSERT INTO tools (name, tool_type_id) VALUES ('Test-Backfill-Pre', $1) RETURNING id`, toolTypeID,
	).Scan(&preID); err != nil {
		t.Fatalf("inserting the pre-migration row err = %v", err)
	}
	// The 000025 backfill UPDATE assigns the sequence number to the NULL rows.
	if _, err := tx.Exec(ctx,
		`UPDATE tools SET inventory_number = 'GEAR' || lpad(nextval('tools_inventory_number_seq')::text, 6, '0')
		 WHERE id = $1 AND inventory_number IS NULL`, preID,
	); err != nil {
		t.Fatalf("backfill update err = %v", err)
	}
	var inv string
	if err := tx.QueryRow(ctx, `SELECT inventory_number FROM tools WHERE id = $1`, preID).Scan(&inv); err != nil {
		t.Fatalf("scanning the backfilled number err = %v", err)
	}
	if !toolInventoryNumberFormat.MatchString(inv) {
		t.Errorf("backfilled inventory_number = %q, want 'GEAR' + 6 zero-padded digits", inv)
	}
}
