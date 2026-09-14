package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/saskia-peters/gear/internal/tools/core"
)

// TestPostgresInspectionStore exercises the Story 5.3 store contract over the
// dev database (migration 000027 applied): the transactional inspection + items
// insert (SUBMIT_PASSFAIL no items / SUBMIT_CHECKLIST with the snapshot items,
// notes NULL semantics, item label/position round-trip) and the derived-status
// input read (latest inspection + latest pass anchor + latest reinstatement
// anchor, nil-safe for a never-inspected tool).
func TestPostgresInspectionStore(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	toolTypeID, _ := seedToolRefs(t, ctx, pool)

	tool, err := repo.CreateTool(ctx, &core.Tool{Name: "Test-Pruef-Werkzeug", ToolTypeID: toolTypeID})
	if err != nil {
		t.Fatalf("CreateTool err = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM inspections WHERE tool_id = $1", tool.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM reinstatements WHERE tool_id = $1", tool.ID)
	})

	// GET_STATUS never-inspected: no inspections, no reinstatement → every
	// anchor nil (the derivation answers `red`, AD-5).
	empty, err := repo.GetToolInspectionStatus(ctx, tool.ID)
	if err != nil {
		t.Fatalf("GetToolInspectionStatus(empty) err = %v", err)
	}
	if empty.Latest != nil || empty.LastSuccessAt != nil || empty.LastReinstatedAt != nil {
		t.Errorf("empty status = %+v, want all nil anchors", empty)
	}

	// SUBMIT_PASSFAIL (fail): a pass_fail inspection with NO items persists
	// with the actor + a NULL notes column (reads back as "").
	fail, err := repo.InsertInspection(ctx, &core.Inspection{
		ToolID: tool.ID, InspectorID: "00000000-0000-0000-0000-0000000000ff",
		Mode: core.InspectionModePassFail, OverallResult: core.InspectionResultFail, Notes: "",
	})
	if err != nil {
		t.Fatalf("InsertInspection(fail) err = %v", err)
	}
	if fail.ID == "" || len(fail.ID) != 36 {
		t.Errorf("id = %q, want a generated uuid", fail.ID)
	}
	if fail.Mode != core.InspectionModePassFail || fail.OverallResult != core.InspectionResultFail {
		t.Errorf("record = %+v, want the persisted values", fail)
	}
	if fail.Notes != "" {
		t.Errorf("notes = %q, want empty (SQL NULL)", fail.Notes)
	}
	if fail.SubmittedAt.IsZero() {
		t.Error("submitted_at = zero, want the DB now()")
	}
	if len(fail.Items) != 0 {
		t.Errorf("items = %+v, want none for a pass_fail inspection", fail.Items)
	}
	var dbNotes *string
	if err := pool.QueryRow(ctx, "SELECT notes FROM inspections WHERE id = $1", fail.ID).Scan(&dbNotes); err != nil {
		t.Fatalf("scan notes err = %v", err)
	}
	if dbNotes != nil {
		t.Errorf("DB notes = %v, want SQL NULL for an empty note", *dbNotes)
	}

	// SUBMIT_CHECKLIST: an inspection WITH the snapshotted items persists the
	// label/position snapshot (the item_id is the plain FK-less reference).
	check, err := repo.InsertInspection(ctx, &core.Inspection{
		ToolID: tool.ID, InspectorID: "00000000-0000-0000-0000-0000000000ff",
		Mode: core.InspectionModeChecklist, OverallResult: core.InspectionResultFail, Notes: "Bohrfutter locker",
		Items: []core.InspectionItem{
			{ItemID: "11111111-1111-1111-1111-111111111111", Label: "Kabel", Position: 0, Result: core.InspectionResultPass},
			{ItemID: "22222222-2222-2222-2222-222222222222", Label: "Bohrfutter", Position: 1, Result: core.InspectionResultFail},
		},
	})
	if err != nil {
		t.Fatalf("InsertInspection(checklist) err = %v", err)
	}
	if len(check.Items) != 2 {
		t.Fatalf("items = %+v, want the two snapshotted items", check.Items)
	}
	if check.Items[0].Label != "Kabel" || check.Items[0].Position != 0 || check.Items[0].Result != core.InspectionResultPass {
		t.Errorf("items[0] = %+v, want the Kabel snapshot", check.Items[0])
	}
	if check.Items[1].Label != "Bohrfutter" || check.Items[1].Position != 1 || check.Items[1].Result != core.InspectionResultFail {
		t.Errorf("items[1] = %+v, want the Bohrfutter snapshot", check.Items[1])
	}
	if check.Items[0].InspectionID != check.ID || check.Items[1].InspectionID != check.ID {
		t.Errorf("items carry inspection_id = %q, want %q", check.Items[0].InspectionID, check.ID)
	}

	// GET_STATUS after the two inspections: the LATEST is the checklist fail;
	// there is no PASS inspection yet (LastSuccessAt nil); no reinstatement.
	afterFails, err := repo.GetToolInspectionStatus(ctx, tool.ID)
	if err != nil {
		t.Fatalf("GetToolInspectionStatus(after fails) err = %v", err)
	}
	if afterFails.Latest == nil || afterFails.Latest.ID != check.ID || afterFails.Latest.OverallResult != core.InspectionResultFail {
		t.Fatalf("latest = %+v, want the checklist fail inspection", afterFails.Latest)
	}
	if afterFails.LastSuccessAt != nil {
		t.Errorf("last_success = %v, want nil (no pass inspection yet)", afterFails.LastSuccessAt)
	}
	if afterFails.LastReinstatedAt != nil {
		t.Errorf("last_reinstated = %v, want nil (no reinstatement)", afterFails.LastReinstatedAt)
	}

	// A later PASS inspection becomes the LATEST inspection AND the last-success
	// anchor.
	pass, err := repo.InsertInspection(ctx, &core.Inspection{
		ToolID: tool.ID, InspectorID: "00000000-0000-0000-0000-0000000000ff",
		Mode: core.InspectionModePassFail, OverallResult: core.InspectionResultPass, Notes: "Ok",
	})
	if err != nil {
		t.Fatalf("InsertInspection(pass) err = %v", err)
	}
	afterPass, err := repo.GetToolInspectionStatus(ctx, tool.ID)
	if err != nil {
		t.Fatalf("GetToolInspectionStatus(after pass) err = %v", err)
	}
	if afterPass.Latest == nil || afterPass.Latest.ID != pass.ID {
		t.Fatalf("latest = %+v, want the pass inspection", afterPass.Latest)
	}
	if afterPass.LastSuccessAt == nil || !afterPass.LastSuccessAt.Equal(pass.SubmittedAt) {
		t.Errorf("last_success = %v, want the pass's submitted_at %v", afterPass.LastSuccessAt, pass.SubmittedAt)
	}

	// A reinstatement (Story 5.6 owns the WRITE path — seeded here directly for
	// the DERIVE_REINSTATED consult) becomes the clock anchor.
	var reinstatedAt time.Time
	if err := pool.QueryRow(ctx,
		`INSERT INTO reinstatements (tool_id, actor_id, reason) VALUES ($1, '00000000-0000-0000-0000-0000000000aa', 'wieder freigegeben')
		 RETURNING created_at`, tool.ID,
	).Scan(&reinstatedAt); err != nil {
		t.Fatalf("seeding reinstatement err = %v", err)
	}
	withReinstatement, err := repo.GetToolInspectionStatus(ctx, tool.ID)
	if err != nil {
		t.Fatalf("GetToolInspectionStatus(with reinstatement) err = %v", err)
	}
	if withReinstatement.LastReinstatedAt == nil || !withReinstatement.LastReinstatedAt.Equal(reinstatedAt) {
		t.Errorf("last_reinstated = %v, want the seeded %v", withReinstatement.LastReinstatedAt, reinstatedAt)
	}

	// A malformed tool id answers the 404 sentinel (never a raw parse error).
	if _, err := repo.GetToolInspectionStatus(ctx, "nonsense"); !errors.Is(err, core.ErrToolNotFound) {
		t.Fatalf("malformed id err = %v, want ErrToolNotFound", err)
	}
}

// TestPostgresInspectionItemsRollback verifies the transactional contract
// (Story 2.5 lesson): when an item write fails, the whole inspection rolls
// back — a half-persisted record never exists. The failure is forced by an
// invalid item result (the DB CHECK constraint).
func TestPostgresInspectionItemsRollback(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	toolTypeID, _ := seedToolRefs(t, ctx, pool)
	tool, err := repo.CreateTool(ctx, &core.Tool{Name: "Test-Rollback-Werkzeug", ToolTypeID: toolTypeID})
	if err != nil {
		t.Fatalf("CreateTool err = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM inspections WHERE tool_id = $1", tool.ID)
	})

	_, err = repo.InsertInspection(ctx, &core.Inspection{
		ToolID: tool.ID, InspectorID: "00000000-0000-0000-0000-0000000000ff",
		Mode: core.InspectionModeChecklist, OverallResult: core.InspectionResultPass,
		Items: []core.InspectionItem{
			{ItemID: "11111111-1111-1111-1111-111111111111", Label: "Kabel", Position: 0, Result: core.InspectionResultPass},
			{ItemID: "22222222-2222-2222-2222-222222222222", Label: "Bohrfutter", Position: 1, Result: "maybe"},
		},
	})
	if err == nil {
		t.Fatal("InsertInspection(bad item result) err = nil, want the CHECK constraint to reject it")
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM inspections WHERE tool_id = $1", tool.ID).Scan(&count); err != nil {
		t.Fatalf("counting inspections err = %v", err)
	}
	if count != 0 {
		t.Errorf("inspections = %d, want 0 (the failed item rolled the whole record back)", count)
	}
}
