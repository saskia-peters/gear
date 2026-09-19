package postgres

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/saskia-peters/gear/internal/tools/core"
)

// TestPostgresCreateToolsBatch pins the set-partitioned batch CREATE (Story
// 4.5): explicit inventory numbers are honored, absent ones auto-assigned
// in-SQL (COALESCE), and all rows commit in ONE transaction.
func TestPostgresCreateToolsBatch(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	toolTypeID, scheduleID := seedToolRefs(t, ctx, pool)

	tools := []*core.Tool{
		{Name: "Test-Batch-Explicit", ToolTypeID: toolTypeID, ScheduleID: scheduleID, InventoryNumber: "GEAR-BATCH-01"},
		{Name: "Test-Batch-Auto-A", ToolTypeID: toolTypeID},
		{Name: "Test-Batch-Auto-B", ToolTypeID: toolTypeID, InventoryNumber: ""},
	}
	created, failed, err := repo.CreateToolsBatch(ctx, tools, "GEAR", 9)
	if err != nil {
		t.Fatalf("CreateToolsBatch err = %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("failed = %+v, want none", failed)
	}
	if len(created) != 3 {
		t.Fatalf("created = %d, want 3", len(created))
	}
	// Explicit inventory honored verbatim; the two absent ones auto-assigned.
	byName := map[string]*core.Tool{}
	for _, c := range created {
		byName[c.Name] = c
	}
	if got := byName["Test-Batch-Explicit"].InventoryNumber; got != "GEAR-BATCH-01" {
		t.Errorf("explicit inventory = %q, want GEAR-BATCH-01", got)
	}
	if got := byName["Test-Batch-Explicit"].ScheduleID; got != scheduleID {
		t.Errorf("explicit schedule = %q, want %q", got, scheduleID)
	}
	autoFmt := regexp.MustCompile(`^GEAR\d{9}$`)
	for _, name := range []string{"Test-Batch-Auto-A", "Test-Batch-Auto-B"} {
		inv := byName[name].InventoryNumber
		if !autoFmt.MatchString(inv) {
			t.Errorf("%s auto inventory = %q, want 'GEAR' + 9 zero-padded digits", name, inv)
		}
	}
	if byName["Test-Batch-Auto-A"].InventoryNumber == byName["Test-Batch-Auto-B"].InventoryNumber {
		t.Errorf("auto inventories collide: %q", byName["Test-Batch-Auto-A"].InventoryNumber)
	}

	// The rows round-trip on the active list.
	list, err := repo.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools err = %v", err)
	}
	count := 0
	for _, l := range list {
		if strings.HasPrefix(l.Name, "Test-Batch-") {
			count++
		}
	}
	if count != 3 {
		t.Errorf("listed batch rows = %d, want 3", count)
	}
}

// TestPostgresCreateToolsBatchRace pins NEW_BATCH_RACE: a row whose name is
// already taken (here by a row the SAME import pre-seeded) is SKIPPED by
// ON CONFLICT DO NOTHING → reported per-index as core.ErrToolImportCollision,
// while the other rows commit.
func TestPostgresCreateToolsBatchRace(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	toolTypeID, _ := seedToolRefs(t, ctx, pool)

	// Pre-seed a row with the name the batch will try to insert again — a race
	// the collision pre-check did not see (concurrent insert).
	if _, err := repo.CreateTool(ctx, &core.Tool{Name: "Test-Batch-Race-A", ToolTypeID: toolTypeID}, "GEAR", 9); err != nil {
		t.Fatalf("pre-seed err = %v", err)
	}

	tools := []*core.Tool{
		{Name: "Test-Batch-Race-A", ToolTypeID: toolTypeID}, // collides → skipped
		{Name: "Test-Batch-Race-B", ToolTypeID: toolTypeID}, // commits
	}
	created, failed, err := repo.CreateToolsBatch(ctx, tools, "GEAR", 9)
	if err != nil {
		t.Fatalf("CreateToolsBatch err = %v", err)
	}
	if len(created) != 1 || created[0].Name != "Test-Batch-Race-B" {
		t.Fatalf("created = %+v, want only the non-colliding row", created)
	}
	if len(failed) != 1 {
		t.Fatalf("failed = %+v, want exactly the colliding index", failed)
	}
	if _, ok := failed[0]; !ok {
		t.Errorf("failed = %+v, want index 0 (the colliding row)", failed)
	}
}

// TestPostgresUpdateToolsBatch pins the set-partitioned batch UPDATE (Story
// 4.5): tool_type_id always applied; schedule/inventory applied ONLY when
// provided (an absent cell PRESERVES the stored value); attributes are NEVER
// touched — the absolute no-data-loss rule.
func TestPostgresUpdateToolsBatch(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	toolTypeID, scheduleID := seedToolRefs(t, ctx, pool)

	// A second schedule so a "provided" update changes the override away from
	// the type default.
	var overrideID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO schedules (name, interval_unit, interval_magnitude) VALUES ('Test-Override', 'month', 1) RETURNING id`,
	).Scan(&overrideID); err != nil {
		t.Fatalf("seeding override schedule err = %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM schedules WHERE id = $1", overrideID) })

	tool, err := repo.CreateTool(ctx, &core.Tool{
		Name: "Test-Batch-Upd", ToolTypeID: toolTypeID, ScheduleID: scheduleID,
		Attributes: map[string]any{"standort": "Werkstatt"},
	}, "GEAR", 9)
	if err != nil {
		t.Fatalf("CreateTool err = %v", err)
	}

	// Update 1: tool_type_id always applied (same type id here), schedule +
	// inventory PROVIDED → applied; attributes preserved.
	updates := []core.ToolImportUpdate{{
		ToolID: tool.ID, ToolTypeID: toolTypeID,
		ScheduleID: overrideID, ScheduleProvided: true,
		InventoryNumber: "GEAR-UPD-01", InventoryProvided: true,
	}}
	updated, errs, err := repo.UpdateToolsBatch(ctx, updates)
	if err != nil {
		t.Fatalf("UpdateToolsBatch err = %v", err)
	}
	if len(updated) != 1 || errs[0] != nil {
		t.Fatalf("updated = %+v, errs = %+v, want one success", updated, errs)
	}
	if updated[0].ScheduleID != overrideID {
		t.Errorf("schedule_id = %q, want the provided override %q", updated[0].ScheduleID, overrideID)
	}
	if updated[0].InventoryNumber != "GEAR-UPD-01" {
		t.Errorf("inventory_number = %q, want the provided GEAR-UPD-01", updated[0].InventoryNumber)
	}
	if updated[0].Attributes["standort"] != "Werkstatt" {
		t.Errorf("attributes = %+v, want the stored set PRESERVED byte-for-byte", updated[0].Attributes)
	}

	// Update 2: schedule + inventory ABSENT (empty cells) → PRESERVED (the
	// override + number stay), attributes still untouched.
	updates = []core.ToolImportUpdate{{
		ToolID: tool.ID, ToolTypeID: toolTypeID,
		ScheduleID: "", ScheduleProvided: false,
		InventoryNumber: "", InventoryProvided: false,
	}}
	updated, errs, err = repo.UpdateToolsBatch(ctx, updates)
	if err != nil {
		t.Fatalf("UpdateToolsBatch(preserve) err = %v", err)
	}
	if len(updated) != 1 || errs[0] != nil {
		t.Fatalf("updated = %+v, errs = %+v, want one success", updated, errs)
	}
	if updated[0].ScheduleID != overrideID {
		t.Errorf("schedule_id = %q, want %q PRESERVED (no data loss)", updated[0].ScheduleID, overrideID)
	}
	if updated[0].InventoryNumber != "GEAR-UPD-01" {
		t.Errorf("inventory_number = %q, want GEAR-UPD-01 PRESERVED", updated[0].InventoryNumber)
	}
	if updated[0].Attributes["standort"] != "Werkstatt" {
		t.Errorf("attributes = %+v, want still preserved", updated[0].Attributes)
	}

	// The DB row reflects the applied + preserved values.
	var dbSchedule, dbInv *string
	if err := pool.QueryRow(ctx,
		"SELECT schedule_id, inventory_number FROM tools WHERE id = $1", tool.ID,
	).Scan(&dbSchedule, &dbInv); err != nil {
		t.Fatalf("scan err = %v", err)
	}
	if dbSchedule == nil || *dbSchedule != overrideID {
		t.Errorf("DB schedule_id = %v, want the preserved override", dbSchedule)
	}
	if dbInv == nil || *dbInv != "GEAR-UPD-01" {
		t.Errorf("DB inventory_number = %v, want the preserved number", dbInv)
	}
}

// TestPostgresUpdateToolsBatchFallback pins the constraint-violation fallback
// (Story 4.5): a provided inventory colliding with an ARCHIVED row trips the
// unique index → the batch rolls back and applies each row individually in the
// SAME transaction, isolating the offender with the German duplicate-inventory
// row error while the non-offending rows commit.
func TestPostgresUpdateToolsBatchFallback(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	toolTypeID, _ := seedToolRefs(t, ctx, pool)

	archived, err := repo.CreateTool(ctx, &core.Tool{Name: "Test-Batch-Fb-Arch", ToolTypeID: toolTypeID}, "GEAR", 9)
	if err != nil {
		t.Fatalf("CreateTool(archived candidate) err = %v", err)
	}
	target, err := repo.CreateTool(ctx, &core.Tool{Name: "Test-Batch-Fb-A", ToolTypeID: toolTypeID}, "GEAR", 9)
	if err != nil {
		t.Fatalf("CreateTool(target) err = %v", err)
	}
	other, err := repo.CreateTool(ctx, &core.Tool{Name: "Test-Batch-Fb-B", ToolTypeID: toolTypeID}, "GEAR", 9)
	if err != nil {
		t.Fatalf("CreateTool(other) err = %v", err)
	}
	if _, err := repo.ArchiveTool(ctx, archived.ID); err != nil {
		t.Fatalf("ArchiveTool err = %v", err)
	}

	// The archived tool keeps its number "taken" (the 4-3b backstop). The
	// batch UPDATE tries to move `target` onto it → unique violation → the
	// batch falls back per-row: `target` surfaces the German duplicate-inventory
	// row error, `other` commits.
	updates := []core.ToolImportUpdate{
		{ToolID: target.ID, ToolTypeID: toolTypeID, InventoryNumber: archived.InventoryNumber, InventoryProvided: true},
		{ToolID: other.ID, ToolTypeID: toolTypeID},
	}
	updated, errs, err := repo.UpdateToolsBatch(ctx, updates)
	if err != nil {
		t.Fatalf("UpdateToolsBatch err = %v", err)
	}
	if len(updated) != 1 || updated[0].ID != other.ID {
		t.Fatalf("updated = %+v, want only the non-offending row committed", updated)
	}
	if errs[0] == nil {
		t.Fatalf("errs[0] = nil, want the duplicate-inventory error")
	}
	var inv *core.InvalidToolError
	if !errors.As(errs[0], &inv) || inv.Message != core.MsgToolInventoryNumberTaken {
		t.Errorf("errs[0] = %v, want the German duplicate-inventory 400", errs[0])
	}
	if errs[1] != nil {
		t.Errorf("errs[1] = %v, want nil (the other row committed)", errs[1])
	}
	// `target` was NOT moved onto the archived number.
	var targetInv string
	if err := pool.QueryRow(ctx, "SELECT inventory_number FROM tools WHERE id = $1", target.ID).Scan(&targetInv); err != nil {
		t.Fatalf("scan target inventory err = %v", err)
	}
	if targetInv == archived.InventoryNumber {
		t.Errorf("target inventory = %q, must NOT be moved onto the archived number", targetInv)
	}
}

// TestPostgresFindToolCollisions pins the Story 4.5 collision pre-check (the
// 4-3b backstop): exact-name + case-insensitive-inventory hits over ACTIVE AND
// ARCHIVED rows.
func TestPostgresFindToolCollisions(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	toolTypeID, _ := seedToolRefs(t, ctx, pool)

	active, err := repo.CreateTool(ctx, &core.Tool{Name: "Test-Coll-A", ToolTypeID: toolTypeID}, "GEAR", 9)
	if err != nil {
		t.Fatalf("CreateTool(active) err = %v", err)
	}
	archived, err := repo.CreateTool(ctx, &core.Tool{Name: "Test-Coll-Archiv", ToolTypeID: toolTypeID}, "GEAR", 9)
	if err != nil {
		t.Fatalf("CreateTool(archived candidate) err = %v", err)
	}
	// Give the archived row a distinctive number to probe.
	if _, err := pool.Exec(ctx, `UPDATE tools SET inventory_number = 'ALT-INV-1' WHERE id = $1`, archived.ID); err != nil {
		t.Fatalf("re-setting archived inventory err = %v", err)
	}
	if _, err := repo.ArchiveTool(ctx, archived.ID); err != nil {
		t.Fatalf("ArchiveTool err = %v", err)
	}

	// Exact-name hit over an ACTIVE row.
	nameHits, invHits, err := repo.FindToolCollisions(ctx, []string{"Test-Coll-A", "Never-Seen"}, nil)
	if err != nil {
		t.Fatalf("FindToolCollisions err = %v", err)
	}
	if _, ok := nameHits["Test-Coll-A"]; !ok {
		t.Errorf("nameHits = %v, want Test-Coll-A hit", nameHits)
	}
	if _, ok := nameHits["Never-Seen"]; ok {
		t.Errorf("nameHits = %v, want no Never-Seen hit", nameHits)
	}
	if len(invHits) != 0 {
		t.Errorf("invHits = %v, want none for a name-only query", invHits)
	}

	// Case-insensitive inventory hit over an ARCHIVED row (the 4-3b backstop).
	nameHits, invHits, err = repo.FindToolCollisions(ctx, nil, []string{"alt-inv-1", "GEAR-NOPE"})
	if err != nil {
		t.Fatalf("FindToolCollisions(inventory) err = %v", err)
	}
	if _, ok := invHits["alt-inv-1"]; !ok {
		t.Errorf("invHits = %v, want alt-inv-1 hit (archived + case-insensitive)", invHits)
	}
	if _, ok := invHits["gear-nope"]; ok {
		t.Errorf("invHits = %v, want no GEAR-NOPE hit", invHits)
	}
	if len(nameHits) != 0 {
		t.Errorf("nameHits = %v, want none for an inventory-only query", nameHits)
	}

	// The active row's auto-assigned number is hit case-insensitively too.
	_, invHits, err = repo.FindToolCollisions(ctx, nil, []string{strings.ToLower(active.InventoryNumber)})
	if err != nil {
		t.Fatalf("FindToolCollisions(active inv) err = %v", err)
	}
	if _, ok := invHits[strings.ToLower(active.InventoryNumber)]; !ok {
		t.Errorf("invHits = %v, want the active number hit", invHits)
	}

	// Empty inputs → empty maps (no query).
	nameHits, invHits, err = repo.FindToolCollisions(ctx, nil, nil)
	if err != nil {
		t.Fatalf("FindToolCollisions(empty) err = %v", err)
	}
	if len(nameHits) != 0 || len(invHits) != 0 {
		t.Errorf("empty inputs hits = %v/%v, want empty maps", nameHits, invHits)
	}
}

// TestPostgresCreateToolsBatchPreservesExplicitOrder pins that the batch
// INSERT returns created rows aligned to the input order.
func TestPostgresCreateToolsBatchPreservesExplicitOrder(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	toolTypeID, _ := seedToolRefs(t, ctx, pool)

	tools := []*core.Tool{
		{Name: "Test-Batch-Ord-2", ToolTypeID: toolTypeID, InventoryNumber: "ORD-2"},
		{Name: "Test-Batch-Ord-1", ToolTypeID: toolTypeID, InventoryNumber: "ORD-1"},
	}
	created, failed, err := repo.CreateToolsBatch(ctx, tools, "GEAR", 9)
	if err != nil {
		t.Fatalf("CreateToolsBatch err = %v", err)
	}
	if len(failed) != 0 || len(created) != 2 {
		t.Fatalf("created/failed = %d/%d, want 2/0", len(created), len(failed))
	}
	if created[0].Name != "Test-Batch-Ord-2" || created[0].InventoryNumber != "ORD-2" {
		t.Errorf("created[0] = %+v, want the first input preserved", created[0])
	}
	if created[1].Name != "Test-Batch-Ord-1" || created[1].InventoryNumber != "ORD-1" {
		t.Errorf("created[1] = %+v, want the second input preserved", created[1])
	}
}
