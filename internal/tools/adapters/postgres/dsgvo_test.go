package postgres

import (
	"context"
	"testing"

	"github.com/saskia-peters/gear/internal/tools/core"
)

// TestPostgresDsgvoInspectionExport exercises the Story 3.3 DSGVO inspection
// export reads over the dev database (migration 000029 applied): the
// per-inspector inspection list (with the snapshotted per-checklist-item
// results attached in one grouped round-trip), the per-actor reinstatement list
// and the id → tool-name resolution (INCLUDING archived tools, which the
// active-only ListTools would drop).
func TestPostgresDsgvoInspectionExport(t *testing.T) {
	pool := toolTestPool(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Close() })

	repo := NewRepository(New(pool))
	toolTypeID, _ := seedToolRefs(t, ctx, pool)

	tool, err := repo.CreateTool(ctx, &core.Tool{Name: "Test-Dsgvo-Werkzeug", ToolTypeID: toolTypeID})
	if err != nil {
		t.Fatalf("CreateTool err = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM inspections WHERE tool_id = $1", tool.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM reinstatements WHERE tool_id = $1", tool.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM tools WHERE id = $1", tool.ID)
	})

	inspectorID := "00000000-0000-0000-0000-0000000000ff"

	// Two inspections by the subject: a checklist pass with two snapshot items,
	// then a fail (newer). A foreign inspection by another inspector must never
	// leak into the export.
	pass, err := repo.InsertInspection(ctx, &core.Inspection{
		ToolID: tool.ID, InspectorID: inspectorID,
		Mode: core.InspectionModeChecklist, OverallResult: core.InspectionResultPass, Notes: "ok",
		Items: []core.InspectionItem{
			{ItemID: "11111111-1111-1111-1111-111111111111", Label: "Kabel", Position: 0, Result: core.InspectionResultPass},
			{ItemID: "22222222-2222-2222-2222-222222222222", Label: "Bohrfutter", Position: 1, Result: core.InspectionResultPass},
		},
	})
	if err != nil {
		t.Fatalf("InsertInspection(pass) err = %v", err)
	}
	fail, err := repo.InsertInspection(ctx, &core.Inspection{
		ToolID: tool.ID, InspectorID: inspectorID,
		Mode: core.InspectionModeChecklist, OverallResult: core.InspectionResultFail, Notes: "",
	})
	if err != nil {
		t.Fatalf("InsertInspection(fail) err = %v", err)
	}
	foreign, err := repo.InsertInspection(ctx, &core.Inspection{
		ToolID: tool.ID, InspectorID: "00000000-0000-0000-0000-0000000000fe",
		Mode: core.InspectionModePassFail, OverallResult: core.InspectionResultPass,
	})
	if err != nil {
		t.Fatalf("InsertInspection(foreign) err = %v", err)
	}

	// The subject's reinstatement + a foreign one.
	if err := repo.InsertReinstatement(ctx, tool.ID, inspectorID, "Ersatzteil eingetroffen"); err != nil {
		t.Fatalf("InsertReinstatement err = %v", err)
	}
	if err := repo.InsertReinstatement(ctx, tool.ID, "00000000-0000-0000-0000-0000000000fe", "fremde Aktion"); err != nil {
		t.Fatalf("InsertReinstatement(foreign) err = %v", err)
	}

	// ListInspectionsByInspector: only the subject's two, newest first, EACH
	// with its items (the pass has the two snapshot items).
	inspections, err := repo.ListInspectionsByInspector(ctx, inspectorID)
	if err != nil {
		t.Fatalf("ListInspectionsByInspector err = %v", err)
	}
	if len(inspections) != 2 {
		t.Fatalf("inspections = %d, want the subject's 2 (foreign=%s must not leak)", len(inspections), foreign.ID)
	}
	if inspections[0].ID != fail.ID || inspections[1].ID != pass.ID {
		t.Errorf("inspection order = [%s, %s], want [%s, %s] newest first",
			inspections[0].ID, inspections[1].ID, fail.ID, pass.ID)
	}
	if inspections[0].InspectorID != inspectorID {
		t.Errorf("inspection inspector = %q, want %q", inspections[0].InspectorID, inspectorID)
	}
	passItems := inspections[1].Items
	if len(passItems) != 2 || passItems[0].Label != "Kabel" || passItems[1].Label != "Bohrfutter" {
		t.Errorf("pass items = %+v, want the two ordered snapshot items", passItems)
	}
	if inspections[0].Items == nil || len(inspections[0].Items) != 0 {
		t.Errorf("fail items = %+v, want an empty non-nil list", inspections[0].Items)
	}

	// ListReinstatementsByActor: only the subject's.
	reinstatements, err := repo.ListReinstatementsByActor(ctx, inspectorID)
	if err != nil {
		t.Fatalf("ListReinstatementsByActor err = %v", err)
	}
	if len(reinstatements) != 1 {
		t.Fatalf("reinstatements = %d, want the subject's 1", len(reinstatements))
	}
	if reinstatements[0].ActorID != inspectorID || reinstatements[0].Reason != "Ersatzteil eingetroffen" {
		t.Errorf("reinstatement = %+v, want the subject's row", reinstatements[0])
	}
	if reinstatements[0].ToolID != tool.ID || reinstatements[0].CreatedAt.IsZero() {
		t.Errorf("reinstatement = %+v, want the tool + timestamp", reinstatements[0])
	}

	// ListToolNamesByIDs resolves the id → name map (including the ARCHIVED
	// tool: archive the first tool, create a second, archive it, then resolve
	// both — the export covers the full fleet history).
	second, err := repo.CreateTool(ctx, &core.Tool{Name: "Test-Dsgvo-Werkzeug-2", ToolTypeID: toolTypeID})
	if err != nil {
		t.Fatalf("CreateTool(second) err = %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM tools WHERE id = $1", second.ID)
	})
	if _, err := repo.ArchiveTool(ctx, second.ID); err != nil {
		t.Fatalf("ArchiveTool(second) err = %v", err)
	}
	names, err := repo.ListToolNamesByIDs(ctx, []string{tool.ID, second.ID, "00000000-0000-0000-0000-00000000dead"})
	if err != nil {
		t.Fatalf("ListToolNamesByIDs err = %v", err)
	}
	if names[tool.ID] != "Test-Dsgvo-Werkzeug" || names[second.ID] != "Test-Dsgvo-Werkzeug-2" {
		t.Errorf("names = %+v, want both tool names (incl. the archived second)", names)
	}
	if _, ok := names["00000000-0000-0000-0000-00000000dead"]; ok {
		t.Errorf("names contains an unknown id — it must be a MISSING key")
	}
}