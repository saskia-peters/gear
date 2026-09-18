package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// dsgvoToolStore seeds the Story 3.3 DSGVO inspection export: a two-tool fleet
// where the subject (u-inspektor) inspected one tool twice (a pass with two
// checklist items, then a fail) and reinstated the other. Another inspector's
// records must never leak into the export.
func dsgvoToolStore() *fakeToolStore {
	store := &fakeToolStore{
		tools: []*Tool{
			{ID: "id-tool-a", Name: "Bohrmaschine-01"},
			{ID: "id-tool-b", Name: "Kettensäge-02"},
		},
		inspections: []*Inspection{
			{
				ID:            "insp-1",
				ToolID:        "id-tool-a",
				InspectorID:   "u-inspektor",
				Mode:          InspectionModeChecklist,
				OverallResult: InspectionResultPass,
				Notes:         "sauber",
				SubmittedAt:   time.Now().UTC().Add(-48 * time.Hour),
				Items: []InspectionItem{
					{ID: "item-1", InspectionID: "insp-1", ItemID: "c-1", Label: "Kabel", Position: 0, Result: InspectionResultPass},
					{ID: "item-2", InspectionID: "insp-1", ItemID: "c-2", Label: "Bohrfutter", Position: 1, Result: InspectionResultPass},
				},
			},
			{
				ID:            "insp-2",
				ToolID:        "id-tool-a",
				InspectorID:   "u-inspektor",
				Mode:          InspectionModeChecklist,
				OverallResult: InspectionResultFail,
				Notes:         "",
				SubmittedAt:   time.Now().UTC().Add(-time.Hour),
				Items: []InspectionItem{
					{ID: "item-3", InspectionID: "insp-2", ItemID: "c-1", Label: "Kabel", Position: 0, Result: InspectionResultFail},
				},
			},
			{
				ID:            "insp-other",
				ToolID:        "id-tool-b",
				InspectorID:   "u-andere",
				Mode:          InspectionModePassFail,
				OverallResult: InspectionResultPass,
				SubmittedAt:   time.Now().UTC(),
			},
		},
		reinstatements: []reinstatementRecord{
			{ToolID: "id-tool-b", ActorID: "u-inspektor", Reason: "Ersatzteil eingetroffen", CreatedAt: time.Now().UTC().Add(-30 * time.Minute)},
			{ToolID: "id-tool-a", ActorID: "u-andere", Reason: "fremde Aktion", CreatedAt: time.Now().UTC()},
		},
	}
	return store
}

func TestExportUserInspectionDataFull(t *testing.T) {
	// REPORT_OK: the subject's inspections (newest first, EACH with its
	// per-checklist-item results) + reinstatements (newest first) + the
	// per-tool summary (counts + tool names). Another inspector's records never
	// appear.
	store := dsgvoToolStore()
	svc := NewService(store, nil, nil, &fakePerms{perms: []string{}}, nil, &fakeAudit{}, nil)

	export, err := svc.ExportUserInspectionData(context.Background(), "u-inspektor")
	if err != nil {
		t.Fatalf("ExportUserInspectionData err = %v, want success", err)
	}

	if len(export.Inspections) != 2 {
		t.Fatalf("inspections = %d, want the subject's 2 (the foreign one must not leak)", len(export.Inspections))
	}
	// Newest first: insp-2 (fail, 1h ago) before insp-1 (pass, 48h ago).
	if export.Inspections[0].ID != "insp-2" || export.Inspections[1].ID != "insp-1" {
		t.Errorf("inspection order = %+v, want [insp-2, insp-1] newest first", export.Inspections)
	}
	if export.Inspections[0].ToolID != "id-tool-a" || export.Inspections[0].ToolName != "Bohrmaschine-01" {
		t.Errorf("inspection tool = %+v, want id-tool-a / Bohrmaschine-01", export.Inspections[0])
	}
	if export.Inspections[0].OverallResult != InspectionResultFail || export.Inspections[0].Mode != InspectionModeChecklist {
		t.Errorf("inspection record = %+v", export.Inspections[0])
	}
	if export.Inspections[1].Notes != "sauber" {
		t.Errorf("inspection notes = %q, want sauber", export.Inspections[1].Notes)
	}
	// Per-item results attached (insp-1's two checklist items).
	if len(export.Inspections[1].Items) != 2 ||
		export.Inspections[1].Items[0].Label != "Kabel" || export.Inspections[1].Items[1].Label != "Bohrfutter" {
		t.Errorf("inspection items = %+v, want the two ordered checklist items", export.Inspections[1].Items)
	}

	if len(export.Reinstatements) != 1 {
		t.Fatalf("reinstatements = %d, want the subject's 1 (the foreign one must not leak)", len(export.Reinstatements))
	}
	if export.Reinstatements[0].ToolID != "id-tool-b" || export.Reinstatements[0].ToolName != "Kettensäge-02" {
		t.Errorf("reinstatement = %+v, want id-tool-b / Kettensäge-02", export.Reinstatements[0])
	}
	if export.Reinstatements[0].Reason != "Ersatzteil eingetroffen" {
		t.Errorf("reinstatement reason = %q", export.Reinstatements[0].Reason)
	}

	// Summary: the subject's INSPECTIONS grouped per tool (the design note) —
	// tool-a with 2 inspections (1 fail). Tool-b appears in the reinstatement
	// list (with its name) but has no inspection counts, so it is not a summary
	// group.
	if len(export.Summary) != 1 {
		t.Fatalf("summary = %+v, want only the tool with inspections", export.Summary)
	}
	a := export.Summary[0]
	if a.ToolID != "id-tool-a" || a.ToolName != "Bohrmaschine-01" || a.InspectionCount != 2 || a.FailCount != 1 {
		t.Errorf("summary = %+v, want id-tool-a / Bohrmaschine-01 / 2 inspections / 1 fail", a)
	}
}

func TestExportUserInspectionDataEmpty(t *testing.T) {
	// REPORT_NO_INSPECTIONS: a user who never inspected / reinstated gets an
	// EMPTY export with zero counts — never an error.
	store := &fakeToolStore{
		tools: []*Tool{{ID: "id-tool-a", Name: "Bohrmaschine-01"}},
	}
	svc := NewService(store, nil, nil, &fakePerms{perms: []string{}}, nil, &fakeAudit{}, nil)

	export, err := svc.ExportUserInspectionData(context.Background(), "u-neu")
	if err != nil {
		t.Fatalf("ExportUserInspectionData err = %v, want success", err)
	}
	if export.Inspections == nil || len(export.Inspections) != 0 {
		t.Errorf("inspections = %+v, want an empty non-nil list", export.Inspections)
	}
	if export.Reinstatements == nil || len(export.Reinstatements) != 0 {
		t.Errorf("reinstatements = %+v, want an empty non-nil list", export.Reinstatements)
	}
	if export.Summary == nil || len(export.Summary) != 0 {
		t.Errorf("summary = %+v, want zero counts", export.Summary)
	}
}

func TestExportUserInspectionDataToolNameFallback(t *testing.T) {
	// A tool id ABSENT from the store (a concurrent deletion) falls back to the
	// id itself — the report never 404s on a vanished tool row.
	store := &fakeToolStore{
		inspections: []*Inspection{{
			ID: "insp-x", ToolID: "id-tool-weg", InspectorID: "u-inspektor",
			Mode: InspectionModePassFail, OverallResult: InspectionResultPass,
			SubmittedAt: time.Now().UTC().Add(-time.Hour),
		}},
	}
	svc := NewService(store, nil, nil, &fakePerms{perms: []string{}}, nil, &fakeAudit{}, nil)

	export, err := svc.ExportUserInspectionData(context.Background(), "u-inspektor")
	if err != nil {
		t.Fatalf("ExportUserInspectionData err = %v, want success", err)
	}
	if len(export.Inspections) != 1 || export.Inspections[0].ToolName != "id-tool-weg" {
		t.Errorf("inspection tool_name = %+v, want the id fallback", export.Inspections)
	}
	if len(export.Summary) != 1 || export.Summary[0].ToolName != "id-tool-weg" {
		t.Errorf("summary = %+v, want the id fallback name", export.Summary)
	}
}

func TestExportUserInspectionDataNeverExposesForeignInspectorID(t *testing.T) {
	// REPORT_SECRETS-adjacent: the export is self-contained for the subject —
	// the JSON payload never names another inspector (the subject's own records
	// are what is exported).
	store := dsgvoToolStore()
	svc := NewService(store, nil, nil, &fakePerms{perms: []string{}}, nil, &fakeAudit{}, nil)

	export, err := svc.ExportUserInspectionData(context.Background(), "u-inspektor")
	if err != nil {
		t.Fatalf("ExportUserInspectionData err = %v, want success", err)
	}
	raw, marshalErr := marshalForInspectionTest(export)
	if marshalErr != nil {
		t.Fatalf("marshal export err = %v", marshalErr)
	}
	if strings.Contains(raw, "u-andere") {
		t.Errorf("export JSON leaks a foreign inspector: %s", raw)
	}
	if strings.Contains(raw, "insp-other") {
		t.Errorf("export JSON leaks a foreign inspection: %s", raw)
	}
	if strings.Contains(raw, "fremde Aktion") {
		t.Errorf("export JSON leaks a foreign reinstatement: %s", raw)
	}
}

// marshalForInspectionTest serializes the export via the standard library so
// the leak assertions run against the wire shape.
func marshalForInspectionTest(export *UserInspectionDataExport) (string, error) {
	b, err := json.Marshal(export)
	if err != nil {
		return "", err
	}
	return string(b), nil
}