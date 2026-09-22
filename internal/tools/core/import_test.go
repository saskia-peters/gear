package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	admcore "github.com/saskia-peters/gear/internal/admin/core"
)

// importRow is a convenience builder for ToolImportRow test fixtures.
func importRow(line int, name, toolType, schedule, inventory string) ToolImportRow {
	return ToolImportRow{
		Line:            line,
		Name:            name,
		ToolTypeName:    toolType,
		ScheduleName:    schedule,
		InventoryNumber: inventory,
	}
}

// importService wires a Service over the fake store with TWO ACTIVE schedules
// ("1 Jahr" → id-s1, "1 Monat" → id-s2) and one ACTIVE tool type
// ("Bohrmaschine" → id-t1), so both the inherit/default and the override
// resolution paths are exercisable. perms defaults to the tools.manage holder.
func importService(perms ...string) (*Service, *fakeToolStore, *fakeAudit) {
	if len(perms) == 0 {
		perms = []string{ToolsManagePermission}
	}
	store := &fakeToolStore{
		types: []*ToolType{{ID: "id-t1", Name: "Bohrmaschine"}},
	}
	audit := &fakeAudit{}
	svc := NewService(
		store,
		&fakeSchedulesPort{schedules: []*admcore.Schedule{
			{ID: "id-s1", Name: "1 Jahr"},
			{ID: "id-s2", Name: "1 Monat"},
		}},
		&fakeQualificationPort{},
		nil,
		&fakePerms{perms: perms},
		nil,
		audit,
		nil,
	)
	return svc, store, audit
}

func TestImportToolsHappy(t *testing.T) {
	// IMPORT_HAPPY: two valid NEW rows → imported=2, errors=[] (all rows OK),
	// audited ONCE (tool.import), each row resolved its type by name.
	svc, store, audit := importService()
	rows := []ToolImportRow{
		importRow(2, "Bohrmaschine-01", "Bohrmaschine", "", ""),
		importRow(3, "Bohrmaschine-02", "bohrmaschine", "1 Jahr", "GEAR000042"),
	}
	result, err := svc.ImportTools(context.Background(), actorID, rows)
	if err != nil {
		t.Fatalf("ImportTools err = %v", err)
	}
	if result.Imported != 2 {
		t.Errorf("imported = %d, want 2", result.Imported)
	}
	if len(result.Errors) != 0 {
		t.Errorf("errors = %+v, want none", result.Errors)
	}
	if len(store.tools) != 2 {
		t.Fatalf("persisted tools = %d, want 2", len(store.tools))
	}
	if store.tools[0].Name != "Bohrmaschine-01" || store.tools[0].ToolTypeID != "id-t1" {
		t.Errorf("tool[0] = %+v, want name + resolved type", store.tools[0])
	}
	// Row 3: type matched case-insensitively, schedule by name resolved, the
	// explicit inventory honored.
	if store.tools[1].ToolTypeID != "id-t1" || store.tools[1].ScheduleID != "id-s1" || store.tools[1].InventoryNumber != "GEAR000042" {
		t.Errorf("tool[1] = %+v, want resolved type/schedule + explicit inventory", store.tools[1])
	}
	// ONE audit event per call, detail carries the counts (NFR-O2).
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationToolImport {
		t.Fatalf("audit events = %+v, want exactly one tool.import", audit.events)
	}
	if !strings.Contains(audit.events[0].detail, "imported=2") || !strings.Contains(audit.events[0].detail, "errors=0") {
		t.Errorf("audit detail = %q, want imported=2 errors=0", audit.events[0].detail)
	}
	if audit.events[0].actorID != actorID || audit.events[0].severity != AuditSeverityNormal {
		t.Errorf("audit actor/severity = %+v", audit.events[0])
	}
}

func TestImportToolsMixed(t *testing.T) {
	// IMPORT_MIXED: valid + invalid rows → imported = valid-count, one error per
	// invalid row with the file line + a German reason; the valid rows persist.
	svc, store, _ := importService()
	rows := []ToolImportRow{
		importRow(2, "Bohrmaschine-01", "Bohrmaschine", "", ""),      // valid NEW
		importRow(3, "Kaputt-01", "GibtEsNicht", "", ""),             // IMPORT_TYPE_UNKNOWN
		importRow(4, "Kaputt-02", "Bohrmaschine", "GibtEsNicht", ""), // IMPORT_SCHEDULE_UNKNOWN
		importRow(5, "  ", "Bohrmaschine", "", ""),                   // empty name
		importRow(6, "Bohrmaschine-02", "Bohrmaschine", "", ""),      // valid NEW
	}
	result, err := svc.ImportTools(context.Background(), actorID, rows)
	if err != nil {
		t.Fatalf("ImportTools err = %v", err)
	}
	if result.Imported != 2 {
		t.Errorf("imported = %d, want 2 (valid rows only)", result.Imported)
	}
	if len(result.Errors) != 3 {
		t.Fatalf("errors = %+v, want 3 per-row errors", result.Errors)
	}
	// Errors carry the file line + German reason, in file-line order.
	want := []ToolImportError{
		{Row: 3, Reason: "Tool Type 'GibtEsNicht' nicht gefunden"},
		{Row: 4, Reason: "Zeitplan 'GibtEsNicht' nicht gefunden"},
		{Row: 5, Reason: "Bitte gib einen Namen für das Werkzeug an."},
	}
	for i, e := range want {
		if result.Errors[i].Row != e.Row || result.Errors[i].Reason != e.Reason {
			t.Errorf("errors[%d] = %+v, want %+v", i, result.Errors[i], e)
		}
	}
	// The valid rows persisted (no partial records for the invalid ones).
	if len(store.tools) != 2 {
		t.Errorf("persisted tools = %d, want 2", len(store.tools))
	}
	for _, tool := range store.tools {
		if strings.HasPrefix(tool.Name, "Kaputt-") || strings.TrimSpace(tool.Name) == "" {
			t.Errorf("an invalid row was persisted: %+v", tool)
		}
	}
}

func TestImportToolsUpdateExisting(t *testing.T) {
	// IMPORT_NAME_DUP: a row whose name is already ACTIVE UPDATES that tool —
	// the type is re-applied, a provided schedule/inventory replaces the stored
	// values, an absent cell PRESERVES them (UPDATE_PROVIDED_FIELDS /
	// UPDATE_PRESERVE_SCHEDULE / UPDATE_PRESERVE_INVENTORY).
	svc, store, audit := importService()
	store.tools = []*Tool{toolFixture("id-a", "Bohrmaschine-01")} // schedule id-s1, inv GEAR00000X

	// Update with a provided schedule (id-s2) + inventory; schedule/inventory
	// are applied, attributes/name/archived_at untouched.
	rows := []ToolImportRow{importRow(2, "Bohrmaschine-01", "Bohrmaschine", "1 Monat", "GEAR000042")}
	result, err := svc.ImportTools(context.Background(), actorID, rows)
	if err != nil {
		t.Fatalf("ImportTools err = %v", err)
	}
	if result.Imported != 1 || len(result.Errors) != 0 {
		t.Fatalf("result = %+v, want imported=1 errors=[]", result)
	}
	got := store.tools[0]
	if got.ScheduleID != "id-s2" {
		t.Errorf("schedule_id = %q, want id-s2 (provided override applied)", got.ScheduleID)
	}
	if got.InventoryNumber != "GEAR000042" {
		t.Errorf("inventory_number = %q, want GEAR000042 (provided number applied)", got.InventoryNumber)
	}
	if got.Name != "Bohrmaschine-01" || got.ToolTypeName != "Bohrmaschine" {
		t.Errorf("name/type = %+v, want preserved name", got)
	}

	// Re-import with EMPTY schedule/inventory cells: the stored override +
	// number are PRESERVED (never cleared), attributes untouched.
	rows = []ToolImportRow{importRow(2, "Bohrmaschine-01", "Bohrmaschine", "", "")}
	result, err = svc.ImportTools(context.Background(), actorID, rows)
	if err != nil {
		t.Fatalf("re-import err = %v", err)
	}
	if result.Imported != 1 || len(result.Errors) != 0 {
		t.Fatalf("re-import result = %+v, want imported=1 errors=[]", result)
	}
	got = store.tools[0]
	if got.ScheduleID != "id-s2" {
		t.Errorf("schedule_id after empty-cell re-import = %q, want id-s2 PRESERVED (no data loss)", got.ScheduleID)
	}
	if got.InventoryNumber != "GEAR000042" {
		t.Errorf("inventory_number after empty-cell re-import = %q, want GEAR000042 PRESERVED", got.InventoryNumber)
	}
	if len(audit.events) != 2 {
		t.Errorf("audit events = %d, want 2 (one per import call, never per row)", len(audit.events))
	}
}

func TestImportToolsUpdatePreservesAttributes(t *testing.T) {
	// UPDATE_PRESERVE_ATTRIBUTES: an update row never touches the stored
	// attributes — the fake store keeps them byte-for-byte via the absent
	// semantics.
	svc, store, _ := importService()
	store.tools = []*Tool{toolFixture("id-a", "Bohrmaschine-01")}
	store.tools[0].Attributes = map[string]any{"standort": "Werkstatt"}

	result, err := svc.ImportTools(context.Background(), actorID, []ToolImportRow{
		importRow(2, "Bohrmaschine-01", "Bohrmaschine", "1 Jahr", "GEAR00000X"),
	})
	if err != nil {
		t.Fatalf("ImportTools err = %v", err)
	}
	if result.Imported != 1 || len(result.Errors) != 0 {
		t.Fatalf("result = %+v, want imported=1 errors=[]", result)
	}
	if store.tools[0].Attributes["standort"] != "Werkstatt" {
		t.Errorf("attributes = %+v, want the stored set PRESERVED byte-for-byte", store.tools[0].Attributes)
	}
}

func TestImportToolsInventoryCollisions(t *testing.T) {
	// IMPORT_INV_TAKEN / IMPORT_INV_LONG / IMPORT_INFILE_DUP (inventory):
	// a provided inventory held by ANOTHER active tool, an over-long number,
	// and a within-file duplicate number are all per-row errors.
	svc, store, _ := importService()
	store.tools = []*Tool{
		toolFixture("id-a", "Bohrmaschine-01"),
		{ID: "id-b", Name: "Bohrmaschine-02", ToolTypeID: "id-t1", ToolTypeName: "Bohrmaschine", InventoryNumber: "GEAR000002"},
	}

	rows := []ToolImportRow{
		importRow(2, "Neu-01", "Bohrmaschine", "", "GEAR00000X"),            // held by id-a → IMPORT_INV_TAKEN
		importRow(3, "Neu-02", "Bohrmaschine", "", strings.Repeat("x", 17)), // IMPORT_INV_LONG
		importRow(4, "Neu-03", "Bohrmaschine", "", "GEAR111"),               // first file occurrence → ok
		importRow(5, "Neu-04", "Bohrmaschine", "", "GEAR111"),               // within-file duplicate → error
	}
	result, err := svc.ImportTools(context.Background(), actorID, rows)
	if err != nil {
		t.Fatalf("ImportTools err = %v", err)
	}
	if result.Imported != 1 {
		t.Errorf("imported = %d, want 1 (only row 4)", result.Imported)
	}
	if len(result.Errors) != 3 {
		t.Fatalf("errors = %+v, want 3", result.Errors)
	}
	want := []ToolImportError{
		{Row: 2, Reason: MsgToolInventoryNumberTaken},
		{Row: 3, Reason: MsgToolInventoryNumberTooLong},
		{Row: 5, Reason: MsgToolImportInventoryDuplicateInFile},
	}
	for i, e := range want {
		if result.Errors[i].Row != e.Row || result.Errors[i].Reason != e.Reason {
			t.Errorf("errors[%d] = %+v, want %+v", i, result.Errors[i], e)
		}
	}
}

func TestImportToolsInventoryCollisionOnUpdate(t *testing.T) {
	// An UPDATE row providing an inventory held by ANOTHER active tool is a row
	// error; providing its OWN current number is a no-op (not a collision).
	svc, store, _ := importService()
	store.tools = []*Tool{
		toolFixture("id-a", "Bohrmaschine-01"),
		{ID: "id-b", Name: "Bohrmaschine-02", ToolTypeID: "id-t1", ToolTypeName: "Bohrmaschine", InventoryNumber: "GEAR000002"},
	}

	result, err := svc.ImportTools(context.Background(), actorID, []ToolImportRow{
		importRow(2, "Bohrmaschine-01", "Bohrmaschine", "", "GEAR000002"), // collides with id-b → error
		importRow(3, "Bohrmaschine-02", "Bohrmaschine", "", "GEAR000002"), // own number → no-op
	})
	if err != nil {
		t.Fatalf("ImportTools err = %v", err)
	}
	if result.Imported != 1 {
		t.Errorf("imported = %d, want 1", result.Imported)
	}
	if len(result.Errors) != 1 || result.Errors[0].Row != 2 || result.Errors[0].Reason != MsgToolInventoryNumberTaken {
		t.Errorf("errors = %+v, want row 2 duplicate-inventory", result.Errors)
	}
}

func TestImportToolsInFileDuplicateName(t *testing.T) {
	// IMPORT_INFILE_DUP (name): a within-file duplicate name → the LATER row is
	// a row error; the first row processes normally.
	svc, _, _ := importService()
	rows := []ToolImportRow{
		importRow(2, "Bohrmaschine-01", "Bohrmaschine", "", ""),
		importRow(3, "bohrmaschine-01", "Bohrmaschine", "", ""), // case-insensitive dup
	}
	result, err := svc.ImportTools(context.Background(), actorID, rows)
	if err != nil {
		t.Fatalf("ImportTools err = %v", err)
	}
	if result.Imported != 1 {
		t.Errorf("imported = %d, want 1", result.Imported)
	}
	if len(result.Errors) != 1 || result.Errors[0].Row != 3 || result.Errors[0].Reason != MsgToolImportNameDuplicateInFile {
		t.Errorf("errors = %+v, want row 3 in-file duplicate", result.Errors)
	}
}

func TestImportToolsArchivedNameBackstop(t *testing.T) {
	// IMPORT_ARCHIVED_NAME (the 4-3b backstop): a name held by an ARCHIVED tool
	// is reported with the precise German duplicate-name row error BEFORE the
	// batch (the core's active-only guard cannot see the archived row).
	svc, store, _ := importService()
	archived := toolFixture("id-arch", "Alt")
	now := timeNow()
	archived.ArchivedAt = &now
	store.tools = []*Tool{archived}

	result, err := svc.ImportTools(context.Background(), actorID, []ToolImportRow{
		importRow(2, "Alt", "Bohrmaschine", "", ""),
	})
	if err != nil {
		t.Fatalf("ImportTools err = %v", err)
	}
	if result.Imported != 0 {
		t.Errorf("imported = %d, want 0", result.Imported)
	}
	if len(result.Errors) != 1 || result.Errors[0].Row != 2 || result.Errors[0].Reason != MsgToolNameTaken {
		t.Errorf("errors = %+v, want row 2 duplicate-name (archived backstop)", result.Errors)
	}
	if len(store.tools) != 1 {
		t.Errorf("tools = %d, want the archived row untouched (no new record)", len(store.tools))
	}
}

func TestImportToolsArchivedInventoryBackstop(t *testing.T) {
	// An explicit inventory held by an ARCHIVED tool is a precise German row
	// error via the FindToolCollisions pre-check (the 4-3b backstop).
	svc, store, _ := importService()
	archived := toolFixture("id-arch", "Alt")
	now := timeNow()
	archived.ArchivedAt = &now
	store.tools = []*Tool{archived} // archived holds "GEAR00000X"

	result, err := svc.ImportTools(context.Background(), actorID, []ToolImportRow{
		importRow(2, "Neu", "Bohrmaschine", "", "gear00000x"), // case-variant of the archived number
	})
	if err != nil {
		t.Fatalf("ImportTools err = %v", err)
	}
	if result.Imported != 0 {
		t.Errorf("imported = %d, want 0", result.Imported)
	}
	if len(result.Errors) != 1 || result.Errors[0].Row != 2 || result.Errors[0].Reason != MsgToolInventoryNumberTaken {
		t.Errorf("errors = %+v, want row 2 duplicate-inventory (archived backstop)", result.Errors)
	}
}

func TestImportToolsBatchRaceReportsRowError(t *testing.T) {
	// NEW_BATCH_RACE: a row skipped by a concurrent collision (reported by the
	// store's per-index failure map) becomes a generic "bereits vergeben" row
	// error — the import does NOT abort, the other rows commit.
	svc, store, _ := importService()
	store.batchCreateFail = map[int]error{0: ErrToolImportCollision}

	result, err := svc.ImportTools(context.Background(), actorID, []ToolImportRow{
		importRow(2, "Bohrmaschine-01", "Bohrmaschine", "", ""),
		importRow(3, "Bohrmaschine-02", "Bohrmaschine", "", ""),
	})
	if err != nil {
		t.Fatalf("ImportTools err = %v", err)
	}
	if result.Imported != 1 {
		t.Errorf("imported = %d, want 1 (the other row committed)", result.Imported)
	}
	if len(result.Errors) != 1 || result.Errors[0].Row != 2 || result.Errors[0].Reason != MsgToolImportCollision {
		t.Errorf("errors = %+v, want row 2 generic 'bereits vergeben'", result.Errors)
	}
	if len(store.tools) != 1 || store.tools[0].Name != "Bohrmaschine-02" {
		t.Errorf("persisted = %+v, want only the non-colliding row", store.tools)
	}
}

func TestImportToolsForbidden(t *testing.T) {
	// IMPORT_FORBIDDEN (AD-6): a tool.edit-ONLY holder (no tools.manage) gets
	// the uniform ErrForbidden with no tool data — the core re-checks
	// tools.manage only.
	svc, store, audit := importService(ToolEditPermission)
	_, err := svc.ImportTools(context.Background(), actorID, []ToolImportRow{
		importRow(2, "Bohrmaschine-01", "Bohrmaschine", "", ""),
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if len(store.tools) != 0 {
		t.Error("forbidden import must not persist any tool")
	}
	if len(audit.events) != 0 {
		t.Error("forbidden import must not audit")
	}
}

func TestImportToolsEmptyNameFails(t *testing.T) {
	// An empty/whitespace name is a per-row error (the header-level 400 for a
	// MISSING name COLUMN lives in the HTTP adapter).
	svc, _, _ := importService()
	result, err := svc.ImportTools(context.Background(), actorID, []ToolImportRow{
		importRow(2, "   ", "Bohrmaschine", "", ""),
	})
	if err != nil {
		t.Fatalf("ImportTools err = %v", err)
	}
	if result.Imported != 0 || len(result.Errors) != 1 {
		t.Fatalf("result = %+v, want 0 imported + 1 error", result)
	}
	if result.Errors[0].Reason != "Bitte gib einen Namen für das Werkzeug an." {
		t.Errorf("reason = %q, want the German name-required message", result.Errors[0].Reason)
	}
}

func TestImportToolsTypeCaseInsensitive(t *testing.T) {
	// tool_type names resolve case-insensitively (consistent with the
	// case-insensitive unique-name semantics of the type catalog).
	svc, store, _ := importService()
	result, err := svc.ImportTools(context.Background(), actorID, []ToolImportRow{
		importRow(2, "Bohrmaschine-01", "BOHRMASCHINE", "", ""),
	})
	if err != nil {
		t.Fatalf("ImportTools err = %v", err)
	}
	if result.Imported != 1 || len(result.Errors) != 0 {
		t.Fatalf("result = %+v, want imported=1 errors=[]", result)
	}
	if store.tools[0].ToolTypeID != "id-t1" {
		t.Errorf("resolved type = %q, want id-t1", store.tools[0].ToolTypeID)
	}
}

// timeNow is a tiny alias so the archived fixtures read naturally.
func timeNow() time.Time { return time.Now() }

func TestImportToolsUpdateArchivedInventoryBackstop(t *testing.T) {
	// An UPDATE row providing an inventory held by an ARCHIVED tool is a precise
	// German row error via the FindToolCollisions pre-check — it never degrades
	// the update batch into the per-row fallback path, and the tool is untouched.
	svc, store, _ := importService()
	archived := toolFixture("id-arch", "Alt")
	now := timeNow()
	archived.ArchivedAt = &now
	archived.InventoryNumber = "GEAR-ARCHIVED"
	store.tools = []*Tool{
		toolFixture("id-a", "Bohrmaschine-01"), // inv GEAR00000X
		archived,
	}

	result, err := svc.ImportTools(context.Background(), actorID, []ToolImportRow{
		importRow(2, "Bohrmaschine-01", "Bohrmaschine", "", "gear-archived"), // differs from own GEAR00000X → real provided value
	})
	if err != nil {
		t.Fatalf("ImportTools err = %v", err)
	}
	if result.Imported != 0 {
		t.Errorf("imported = %d, want 0 (the update row must not write)", result.Imported)
	}
	if len(result.Errors) != 1 || result.Errors[0].Row != 2 || result.Errors[0].Reason != MsgToolInventoryNumberTaken {
		t.Errorf("errors = %+v, want row 2 precise duplicate-inventory (archived backstop, before the batch)", result.Errors)
	}
	// The active tool was NOT touched (no update ran for it).
	if store.tools[0].ID != "id-a" || store.tools[0].InventoryNumber != "GEAR00000X" {
		t.Errorf("active tool = %+v, want untouched (GEAR00000X preserved)", store.tools[0])
	}
}

func TestImportToolsUpdateOwnInventoryNoOp(t *testing.T) {
	// An UPDATE row providing its OWN current inventory number is a NO-OP — it
	// neither claims the in-file dedup slot nor trips the collision pre-check,
	// and the update still succeeds (imported).
	svc, store, _ := importService()
	store.tools = []*Tool{toolFixture("id-a", "Bohrmaschine-01")} // inv GEAR00000X

	result, err := svc.ImportTools(context.Background(), actorID, []ToolImportRow{
		importRow(2, "Bohrmaschine-01", "Bohrmaschine", "", "GEAR00000X"),
	})
	if err != nil {
		t.Fatalf("ImportTools err = %v", err)
	}
	if result.Imported != 1 || len(result.Errors) != 0 {
		t.Fatalf("result = %+v, want imported=1 errors=[] (own-number no-op)", result)
	}
	if store.tools[0].InventoryNumber != "GEAR00000X" {
		t.Errorf("inventory_number = %q, want GEAR00000X unchanged", store.tools[0].InventoryNumber)
	}
}
