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
// input read (latest-fail anchor + latest pass anchor + latest reinstatement
// anchor, nil-safe for a never-inspected tool — a PASS inspection does NOT
// clear the latest-fail OOS anchor, AD-4).
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
	if empty.LatestFailAt != nil || empty.LastSuccessAt != nil || empty.LastReinstatedAt != nil {
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

	// GET_STATUS after the two inspections: the latest FAILED is the checklist
	// fail's submitted_at; there is no PASS inspection yet (LastSuccessAt nil);
	// no reinstatement.
	afterFails, err := repo.GetToolInspectionStatus(ctx, tool.ID)
	if err != nil {
		t.Fatalf("GetToolInspectionStatus(after fails) err = %v", err)
	}
	if afterFails.LatestFailAt == nil || !afterFails.LatestFailAt.Equal(check.SubmittedAt) {
		t.Fatalf("latest_fail = %v, want the checklist fail's submitted_at %v", afterFails.LatestFailAt, check.SubmittedAt)
	}
	if afterFails.LastSuccessAt != nil {
		t.Errorf("last_success = %v, want nil (no pass inspection yet)", afterFails.LastSuccessAt)
	}
	if afterFails.LastReinstatedAt != nil {
		t.Errorf("last_reinstated = %v, want nil (no reinstatement)", afterFails.LastReinstatedAt)
	}

	// A later PASS inspection becomes the last-success anchor — but does NOT
	// clear the latest-FAIL OOS anchor (AD-4: a passing inspection is not the
	// exit from OOS; reinstatement is).
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
	if afterPass.LatestFailAt == nil || !afterPass.LatestFailAt.Equal(check.SubmittedAt) {
		t.Errorf("latest_fail = %v, want the earlier fail's submitted_at %v (a pass does NOT clear OOS)", afterPass.LatestFailAt, check.SubmittedAt)
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

// TestPostgresInsertReinstatement exercises the Story 5.6 store contract over
// the dev database: the single-row reinstatement insert (actor + reason
// round-trip, created_at = DB now()) and the derived-status anchors that flip
// the OOS derivation — a fail STRICTLY BEFORE the latest reinstatement is NOT
// OOS, a NEW fail at-or-after the latest reinstatement is OOS (AD-4).
func TestPostgresInsertReinstatement(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	toolTypeID, _ := seedToolRefs(t, ctx, pool)
	tool, err := repo.CreateTool(ctx, &core.Tool{Name: "Test-Reinstate-Werkzeug", ToolTypeID: toolTypeID})
	if err != nil {
		t.Fatalf("CreateTool err = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM inspections WHERE tool_id = $1", tool.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM reinstatements WHERE tool_id = $1", tool.ID)
	})

	// A failing inspection makes the tool OOS (a latest fail, no reinstatement).
	if _, err := repo.InsertInspection(ctx, &core.Inspection{
		ToolID: tool.ID, InspectorID: "00000000-0000-0000-0000-0000000000ff",
		Mode: core.InspectionModePassFail, OverallResult: core.InspectionResultFail,
	}); err != nil {
		t.Fatalf("InsertInspection(fail) err = %v", err)
	}
	oos, err := repo.GetToolInspectionStatus(ctx, tool.ID)
	if err != nil {
		t.Fatalf("GetToolInspectionStatus(oos) err = %v", err)
	}
	if oos.LatestFailAt == nil || oos.LastReinstatedAt != nil {
		t.Fatalf("status = %+v, want a latest fail with NO reinstatement (OOS)", oos)
	}

	// InsertReinstatement round-trip: actor + reason persist, created_at is the
	// DB now(), and the latest reinstatement becomes the clock reset anchor.
	if err := repo.InsertReinstatement(ctx, tool.ID, "00000000-0000-0000-0000-0000000000aa", "  Ersatzteil eingetroffen  "); err != nil {
		t.Fatalf("InsertReinstatement err = %v", err)
	}
	after, err := repo.GetToolInspectionStatus(ctx, tool.ID)
	if err != nil {
		t.Fatalf("GetToolInspectionStatus(after) err = %v", err)
	}
	if after.LastReinstatedAt == nil {
		t.Fatal("last_reinstated = nil, want the inserted reinstatement's created_at")
	}
	// The reinstatement is AFTER the earlier fail → the OOS rule yields
	// NOT-OOS (a fail strictly before the latest reinstatement is not OOS, AD-4
	// — reinstatement resets the clock). The stored reason round-trips verbatim
	// (trimming is the core's job, never the store's).
	var reason string
	if err := pool.QueryRow(ctx, "SELECT reason FROM reinstatements WHERE tool_id = $1", tool.ID).Scan(&reason); err != nil {
		t.Fatalf("scan reason err = %v", err)
	}
	if reason != "  Ersatzteil eingetroffen  " {
		t.Errorf("reason = %q, want the persisted value verbatim (trim is core's job)", reason)
	}
	if !oos.LatestFailAt.Before(*after.LastReinstatedAt) {
		t.Fatalf("latest_fail %v is NOT before last_reinstated %v → the derivation would read OOS", oos.LatestFailAt, after.LastReinstatedAt)
	}

	// A NEW fail after the reinstatement flips it back to OOS: the latest fail
	// is at-or-after the latest reinstatement (the tie boundary favors safety).
	if _, err := repo.InsertInspection(ctx, &core.Inspection{
		ToolID: tool.ID, InspectorID: "00000000-0000-0000-0000-0000000000ff",
		Mode: core.InspectionModePassFail, OverallResult: core.InspectionResultFail,
	}); err != nil {
		t.Fatalf("InsertInspection(new fail) err = %v", err)
	}
	flipped, err := repo.GetToolInspectionStatus(ctx, tool.ID)
	if err != nil {
		t.Fatalf("GetToolInspectionStatus(flipped) err = %v", err)
	}
	if flipped.LatestFailAt == nil || flipped.LastReinstatedAt == nil {
		t.Fatalf("status = %+v, want both anchors for the at-or-after boundary", flipped)
	}
	if flipped.LatestFailAt.Before(*flipped.LastReinstatedAt) {
		t.Errorf("latest_fail %v IS before last_reinstated %v → the derivation would read NOT OOS, want OOS", flipped.LatestFailAt, flipped.LastReinstatedAt)
	}

	// A malformed tool id answers the 404 sentinel (never a raw parse error).
	if err := repo.InsertReinstatement(ctx, "nonsense", "00000000-0000-0000-0000-0000000000aa", "x"); !errors.Is(err, core.ErrToolNotFound) {
		t.Fatalf("malformed id err = %v, want ErrToolNotFound", err)
	}
}

// TestPostgresToolHistory exercises the Story 6.3 history store contract over
// the dev database: ListInspectionsByTool returns the tool's FULL inspection
// history newest-first (submitted_at DESC with the id tiebreak) EACH WITH its
// snapshotted ordered checklist items (the one-query items join), and
// ListReinstatementsByTool returns the reinstatement ledger newest-first
// (created_at DESC). A tool without history answers empty lists; a malformed
// tool id answers the 404 sentinel.
func TestPostgresToolHistory(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	toolTypeID, _ := seedToolRefs(t, ctx, pool)
	tool, err := repo.CreateTool(ctx, &core.Tool{Name: "Test-History-Werkzeug", ToolTypeID: toolTypeID})
	if err != nil {
		t.Fatalf("CreateTool err = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM inspections WHERE tool_id = $1", tool.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM reinstatements WHERE tool_id = $1", tool.ID)
	})

	// Seed THREE inspections with back-dated submitted_at (the DESC order must
	// be deterministic): an OLDER pass_fail, a NEWER pass_fail and a NEWEST
	// checklist WITH its snapshot items.
	older, err := repo.InsertInspection(ctx, &core.Inspection{
		ToolID: tool.ID, InspectorID: "00000000-0000-0000-0000-0000000000ff",
		Mode: core.InspectionModePassFail, OverallResult: core.InspectionResultPass, Notes: "alt",
	})
	if err != nil {
		t.Fatalf("InsertInspection(older) err = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE inspections SET submitted_at = '2026-09-01T09:00:00Z' WHERE id = $1`, older.ID); err != nil {
		t.Fatalf("back-dating older inspection err = %v", err)
	}
	newer, err := repo.InsertInspection(ctx, &core.Inspection{
		ToolID: tool.ID, InspectorID: "00000000-0000-0000-0000-0000000000ff",
		Mode: core.InspectionModePassFail, OverallResult: core.InspectionResultFail,
	})
	if err != nil {
		t.Fatalf("InsertInspection(newer) err = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE inspections SET submitted_at = '2026-09-10T09:00:00Z' WHERE id = $1`, newer.ID); err != nil {
		t.Fatalf("back-dating newer inspection err = %v", err)
	}
	newest, err := repo.InsertInspection(ctx, &core.Inspection{
		ToolID: tool.ID, InspectorID: "00000000-0000-0000-0000-0000000000ff",
		Mode: core.InspectionModeChecklist, OverallResult: core.InspectionResultFail, Notes: "Bohrfutter locker",
		Items: []core.InspectionItem{
			{ItemID: "11111111-1111-1111-1111-111111111111", Label: "Kabel", Position: 0, Result: core.InspectionResultPass},
			{ItemID: "22222222-2222-2222-2222-222222222222", Label: "Bohrfutter", Position: 1, Result: core.InspectionResultFail},
		},
	})
	if err != nil {
		t.Fatalf("InsertInspection(newest checklist) err = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE inspections SET submitted_at = '2026-09-15T09:00:00Z' WHERE id = $1`, newest.ID); err != nil {
		t.Fatalf("back-dating newest inspection err = %v", err)
	}

	// LIST newest first (submitted_at DESC): newest checklist, newer fail, older pass.
	history, err := repo.ListInspectionsByTool(ctx, tool.ID)
	if err != nil {
		t.Fatalf("ListInspectionsByTool err = %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("inspections = %d, want 3", len(history))
	}
	if history[0].ID != newest.ID || history[1].ID != newer.ID || history[2].ID != older.ID {
		t.Errorf("order = [%s, %s, %s], want [newest, newer, older]", history[0].ID, history[1].ID, history[2].ID)
	}
	// The newest checklist carries its snapshotted ORDERED items (the items
	// join grouped them onto the right inspection); the pass_fail rows are empty.
	if len(history[0].Items) != 2 {
		t.Fatalf("newest items = %+v, want the two snapshot items", history[0].Items)
	}
	if history[0].Items[0].Label != "Kabel" || history[0].Items[0].Position != 0 || history[0].Items[0].Result != core.InspectionResultPass {
		t.Errorf("newest items[0] = %+v, want the Kabel pass snapshot", history[0].Items[0])
	}
	if history[0].Items[1].Label != "Bohrfutter" || history[0].Items[1].Position != 1 || history[0].Items[1].Result != core.InspectionResultFail {
		t.Errorf("newest items[1] = %+v, want the Bohrfutter fail snapshot", history[0].Items[1])
	}
	for _, h := range history[1:] {
		if len(h.Items) != 0 {
			t.Errorf("pass_fail inspection %s items = %+v, want none", h.ID, h.Items)
		}
	}

	// EMPTY: a tool with no records answers an empty list, nil-safe.
	other, err := repo.CreateTool(ctx, &core.Tool{Name: "Test-History-Leer", ToolTypeID: toolTypeID})
	if err != nil {
		t.Fatalf("CreateTool(empty target) err = %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM tools WHERE id = $1", other.ID) })
	empty, err := repo.ListInspectionsByTool(ctx, other.ID)
	if err != nil {
		t.Fatalf("ListInspectionsByTool(empty) err = %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("empty inspections = %+v, want an empty list", empty)
	}

	// REINSTATEMENTS newest first (created_at DESC): two rows seeded out of
	// order by created_at.
	if _, err := pool.Exec(ctx,
		`INSERT INTO reinstatements (tool_id, actor_id, reason, created_at) VALUES
		 ($1, '00000000-0000-0000-0000-0000000000aa', 'frucher', '2026-09-14T08:00:00Z'),
		 ($1, '00000000-0000-0000-0000-0000000000aa', 'spaeter', '2026-09-16T08:00:00Z')`, tool.ID); err != nil {
		t.Fatalf("seeding reinstatements err = %v", err)
	}
	rein, err := repo.ListReinstatementsByTool(ctx, tool.ID)
	if err != nil {
		t.Fatalf("ListReinstatementsByTool err = %v", err)
	}
	if len(rein) != 2 {
		t.Fatalf("reinstatements = %d, want 2", len(rein))
	}
	if rein[0].Reason != "spaeter" || rein[1].Reason != "frucher" {
		t.Errorf("reinstatement order = [%s, %s], want [spaeter, frucher]", rein[0].Reason, rein[1].Reason)
	}
	if rein[0].ActorID != "00000000-0000-0000-0000-0000000000aa" {
		t.Errorf("reinstatement actor = %q, want the seeded actor", rein[0].ActorID)
	}

	emptyRein, err := repo.ListReinstatementsByTool(ctx, other.ID)
	if err != nil {
		t.Fatalf("ListReinstatementsByTool(empty) err = %v", err)
	}
	if len(emptyRein) != 0 {
		t.Errorf("empty reinstatements = %+v, want an empty list", emptyRein)
	}

	// A malformed tool id answers the 404 sentinel (never a raw parse error).
	if _, err := repo.ListInspectionsByTool(ctx, "nonsense"); !errors.Is(err, core.ErrToolNotFound) {
		t.Fatalf("ListInspectionsByTool(malformed) err = %v, want ErrToolNotFound", err)
	}
	if _, err := repo.ListReinstatementsByTool(ctx, "nonsense"); !errors.Is(err, core.ErrToolNotFound) {
		t.Fatalf("ListReinstatementsByTool(malformed) err = %v, want ErrToolNotFound", err)
	}
}

// TestPostgresToolHistoryEqualTimestampTiebreak pins the id DESC tiebreak of
// the Story 6.3 history queries (FR-18): two inspections sharing ONE
// submitted_at and two reinstatements sharing ONE created_at come back ordered
// by id DESC — the deterministic tiebreak for equal timestamps (the same
// convention as the tool_types `name ASC` tiebreaker test: rows are inserted
// directly with a PINNED timestamp so the ORDER BY tiebreak is exercised).
func TestPostgresToolHistoryEqualTimestampTiebreak(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	toolTypeID, _ := seedToolRefs(t, ctx, pool)
	tool, err := repo.CreateTool(ctx, &core.Tool{Name: "Test-Tiebreak-Werkzeug", ToolTypeID: toolTypeID})
	if err != nil {
		t.Fatalf("CreateTool err = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM inspections WHERE tool_id = $1", tool.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM reinstatements WHERE tool_id = $1", tool.ID)
	})

	// TWO inspections with the SAME submitted_at → the id DESC tiebreak decides.
	// The DB-generated uuidv7 ids are monotonic, so the second insert carries the
	// lexicographically-greater id — it must come FIRST.
	inspA, err := repo.InsertInspection(ctx, &core.Inspection{
		ToolID: tool.ID, InspectorID: "00000000-0000-0000-0000-0000000000ff",
		Mode: core.InspectionModePassFail, OverallResult: core.InspectionResultPass,
	})
	if err != nil {
		t.Fatalf("InsertInspection(A) err = %v", err)
	}
	inspB, err := repo.InsertInspection(ctx, &core.Inspection{
		ToolID: tool.ID, InspectorID: "00000000-0000-0000-0000-0000000000ff",
		Mode: core.InspectionModePassFail, OverallResult: core.InspectionResultPass,
	})
	if err != nil {
		t.Fatalf("InsertInspection(B) err = %v", err)
	}
	// Pin BOTH to the SAME submitted_at so the ORDER BY tiebreak is exercised.
	for _, id := range []string{inspA.ID, inspB.ID} {
		if _, err := pool.Exec(ctx, `UPDATE inspections SET submitted_at = '2026-01-01T00:00:00Z' WHERE id = $1`, id); err != nil {
			t.Fatalf("pinning equal submitted_at err = %v", err)
		}
	}

	history, err := repo.ListInspectionsByTool(ctx, tool.ID)
	if err != nil {
		t.Fatalf("ListInspectionsByTool(tiebreak) err = %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("inspections = %d, want 2", len(history))
	}
	wantFirst := inspB.ID
	if inspA.ID > inspB.ID {
		wantFirst = inspA.ID
	}
	if history[0].ID != wantFirst {
		t.Fatalf("tiebreak order = [%s, %s], want [%s, ...] (id DESC for equal submitted_at)", history[0].ID, history[1].ID, wantFirst)
	}
	if history[1].ID == history[0].ID {
		t.Fatalf("tiebreak rows share an id: %s", history[0].ID)
	}

	// TWO reinstatements with the SAME created_at → the id DESC tiebreak decides.
	var reinA, reinB string
	if err := pool.QueryRow(ctx,
		`INSERT INTO reinstatements (tool_id, actor_id, reason, created_at)
		 VALUES ($1, '00000000-0000-0000-0000-0000000000aa', 'A', '2026-01-01T00:00:00Z') RETURNING id`, tool.ID,
	).Scan(&reinA); err != nil {
		t.Fatalf("inserting rein-A err = %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO reinstatements (tool_id, actor_id, reason, created_at)
		 VALUES ($1, '00000000-0000-0000-0000-0000000000aa', 'B', '2026-01-01T00:00:00Z') RETURNING id`, tool.ID,
	).Scan(&reinB); err != nil {
		t.Fatalf("inserting rein-B err = %v", err)
	}

	rein, err := repo.ListReinstatementsByTool(ctx, tool.ID)
	if err != nil {
		t.Fatalf("ListReinstatementsByTool(tiebreak) err = %v", err)
	}
	if len(rein) != 2 {
		t.Fatalf("reinstatements = %d, want 2", len(rein))
	}
	wantFirstRein := reinB
	if reinA > reinB {
		wantFirstRein = reinA
	}
	if rein[0].ID != wantFirstRein {
		t.Fatalf("rein tiebreak order = [%s, %s], want [%s, ...] (id DESC for equal created_at)", rein[0].ID, rein[1].ID, wantFirstRein)
	}
}

// TestPostgresGetLatestInspectionByTool exercises the Story 6.2 store read over
// the dev database: the LATEST inspection of a tool (ANY result) with the
// submitted_at DESC, id DESC deterministic tiebreak, a nil (nil, nil) answer
// for a never-inspected tool and the 404 sentinel for a malformed id.
func TestPostgresGetLatestInspectionByTool(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	toolTypeID, _ := seedToolRefs(t, ctx, pool)
	tool, err := repo.CreateTool(ctx, &core.Tool{Name: "Test-Report-Werkzeug", ToolTypeID: toolTypeID})
	if err != nil {
		t.Fatalf("CreateTool err = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM inspections WHERE tool_id = $1", tool.ID)
	})

	// EMPTY: a tool with no inspections answers (nil, nil) — never an error (the
	// report renders "–").
	latest, err := repo.GetLatestInspectionByTool(ctx, tool.ID)
	if err != nil {
		t.Fatalf("GetLatestInspectionByTool(empty) err = %v", err)
	}
	if latest != nil {
		t.Errorf("latest = %+v, want nil for a never-inspected tool", latest)
	}

	// NEWEST: two inspections (any result) — the newer submitted_at must win.
	older, err := repo.InsertInspection(ctx, &core.Inspection{
		ToolID: tool.ID, InspectorID: "00000000-0000-0000-0000-0000000000ff",
		Mode: core.InspectionModePassFail, OverallResult: core.InspectionResultPass,
	})
	if err != nil {
		t.Fatalf("InsertInspection(older) err = %v", err)
	}
	// Backdate the first row so the second insert deterministically wins the
	// submitted_at DESC order.
	if _, err := pool.Exec(ctx, "UPDATE inspections SET submitted_at = $1 WHERE id = $2",
		older.SubmittedAt.Add(-24*time.Hour), older.ID); err != nil {
		t.Fatalf("backdating older inspection err = %v", err)
	}
	newer, err := repo.InsertInspection(ctx, &core.Inspection{
		ToolID: tool.ID, InspectorID: "00000000-0000-0000-0000-0000000000ff",
		Mode: core.InspectionModePassFail, OverallResult: core.InspectionResultFail,
	})
	if err != nil {
		t.Fatalf("InsertInspection(newer) err = %v", err)
	}

	latest, err = repo.GetLatestInspectionByTool(ctx, tool.ID)
	if err != nil {
		t.Fatalf("GetLatestInspectionByTool err = %v", err)
	}
	if latest == nil {
		t.Fatal("latest = nil, want the newest inspection")
	}
	if !latest.SubmittedAt.Equal(newer.SubmittedAt) {
		t.Errorf("latest.SubmittedAt = %v, want the newer inspection's %v", latest.SubmittedAt, newer.SubmittedAt)
	}
	if latest.InspectorID != "00000000-0000-0000-0000-0000000000ff" {
		t.Errorf("latest.InspectorID = %q, want the inspector id", latest.InspectorID)
	}

	// TIEBREAK: two inspections at the EXACT same submitted_at → the id DESC
	// tiebreak decides. The DB-generated uuidv7 ids are monotonic, so the second
	// insert carries the lexicographically-greater id. The winner is made
	// observable via distinct inspector ids on the two equal-timestamp rows.
	inspectorHigh := "00000000-0000-0000-0000-0000000000aa"
	inspectorLow := "00000000-0000-0000-0000-0000000000bb"
	for _, row := range []struct {
		id        string
		inspector string
		submitted string
	}{
		{older.ID, inspectorLow, "2026-01-01T00:00:00Z"},
		{newer.ID, inspectorHigh, "2026-01-01T00:00:00Z"},
	} {
		if _, err := pool.Exec(ctx,
			"UPDATE inspections SET submitted_at = $1, inspector_id = $2 WHERE id = $3",
			row.submitted, row.inspector, row.id); err != nil {
			t.Fatalf("pinning equal submitted_at err = %v", err)
		}
	}
	wantInspector := inspectorHigh
	if older.ID > newer.ID {
		wantInspector = inspectorLow
	}
	latest, err = repo.GetLatestInspectionByTool(ctx, tool.ID)
	if err != nil {
		t.Fatalf("GetLatestInspectionByTool(tiebreak) err = %v", err)
	}
	if latest == nil {
		t.Fatal("latest = nil, want the tiebroken newest inspection")
	}
	if latest.InspectorID != wantInspector {
		t.Errorf("tiebreak latest.InspectorID = %q, want %q (id DESC for equal submitted_at)", latest.InspectorID, wantInspector)
	}

	// A malformed tool id answers the 404 sentinel (never a raw parse error).
	if _, err := repo.GetLatestInspectionByTool(ctx, "nonsense"); !errors.Is(err, core.ErrToolNotFound) {
		t.Fatalf("malformed id err = %v, want ErrToolNotFound", err)
	}
}
