package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	admcore "github.com/saskia-peters/gear/internal/admin/core"
)

// fakeToolStore is an in-memory store emulating the repository's soft-archive
// semantics over BOTH the Tool-owned tables (tool types for the intra-module
// type check + tools). The tools list returns only ACTIVE tools; the type list
// only ACTIVE types; Update/Archive refuse a missing OR already-archived row
// with the matching Err*NotFound sentinel.
type fakeToolStore struct {
	tools       []*Tool
	types       []*ToolType
	listErr     error
	createErr   error
	updateErr   error
	archiveErr  error
	created     []*Tool
	updated     []*Tool
	archivedIDs []string
}

func (f *fakeToolStore) ListTools(context.Context) ([]*Tool, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []*Tool
	for _, t := range f.tools {
		if t.ArchivedAt == nil {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeToolStore) ToolExistsActive(_ context.Context, id string) (bool, error) {
	for _, t := range f.tools {
		if t.ID == id {
			return t.ArchivedAt == nil, nil
		}
	}
	return false, nil
}

func (f *fakeToolStore) CreateTool(_ context.Context, tool *Tool) (*Tool, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	persisted := *tool
	persisted.ID = "id-tool-" + tool.Name
	f.tools = append(f.tools, &persisted)
	f.created = append(f.created, &persisted)
	return &persisted, nil
}

func (f *fakeToolStore) UpdateTool(_ context.Context, tool *Tool) (*Tool, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	for i, existing := range f.tools {
		if existing.ID == tool.ID {
			if existing.ArchivedAt != nil {
				return nil, ErrToolNotFound
			}
			persisted := *tool
			f.tools[i] = &persisted
			f.updated = append(f.updated, &persisted)
			return &persisted, nil
		}
	}
	return nil, ErrToolNotFound
}

func (f *fakeToolStore) ArchiveTool(_ context.Context, id string) (*Tool, error) {
	if f.archiveErr != nil {
		return nil, f.archiveErr
	}
	for i, t := range f.tools {
		if t.ID == id {
			if t.ArchivedAt != nil {
				return nil, ErrToolNotFound
			}
			now := time.Now()
			archived := *t
			archived.ArchivedAt = &now
			f.tools[i] = &archived
			f.archivedIDs = append(f.archivedIDs, id)
			return &archived, nil
		}
	}
	return nil, ErrToolNotFound
}

func (f *fakeToolStore) ListToolTypes(context.Context) ([]*ToolType, error) {
	var out []*ToolType
	for _, tt := range f.types {
		if tt.ArchivedAt == nil {
			out = append(out, tt)
		}
	}
	return out, nil
}

func (f *fakeToolStore) ToolTypeExistsActive(_ context.Context, id string) (bool, error) {
	for _, tt := range f.types {
		if tt.ID == id {
			return tt.ArchivedAt == nil, nil
		}
	}
	return false, nil
}

func (f *fakeToolStore) CreateToolType(_ context.Context, tt *ToolType) (*ToolType, error) {
	return nil, ErrToolTypeNotFound
}

func (f *fakeToolStore) UpdateToolType(_ context.Context, _ *ToolType) (*ToolType, error) {
	return nil, ErrToolTypeNotFound
}

func (f *fakeToolStore) ArchiveToolType(_ context.Context, _ string) (*ToolType, error) {
	return nil, ErrToolTypeNotFound
}

// newToolService wires the fakes around a Service. perms defaults to the
// tools.manage holder; the schedule port defaults to one ACTIVE schedule
// (id-s1); the store carries one ACTIVE tool type (id-t1).
func newToolService(perms ...string) (*Service, *fakeToolStore, *fakeAudit) {
	store := &fakeToolStore{
		types: []*ToolType{{ID: "id-t1", Name: "Bohrmaschine"}},
	}
	audit := &fakeAudit{}
	if len(perms) == 0 {
		perms = []string{ToolsManagePermission}
	}
	svc := NewService(
		store,
		&fakeSchedulesPort{schedules: []*admcore.Schedule{{ID: "id-s1", Name: "1 Jahr"}}},
		&fakeQualificationPort{},
		&fakePerms{perms: perms},
		audit,
		nil,
	)
	return svc, store, audit
}

func toolInput() ToolInput {
	return ToolInput{
		Name:            "Bohrmaschine-01",
		ToolTypeID:      "id-t1",
		ScheduleID:      "",
		InventoryNumber: "GEAR000001",
	}
}

func toolFixture(id, name string) *Tool {
	return &Tool{
		ID:              id,
		Name:            name,
		ToolTypeID:      "id-t1",
		ToolTypeName:    "Bohrmaschine",
		ScheduleID:      "id-s1",
		InventoryNumber: "GEAR00000X",
	}
}

func TestListToolsEmpty(t *testing.T) {
	// GET_LIST_EMPTY: no tools → empty list, no error.
	svc, _, _ := newToolService()
	got, err := svc.ListTools(context.Background(), actorID)
	if err != nil {
		t.Fatalf("ListTools err = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("tools = %d, want 0", len(got))
	}
}

func TestListTools(t *testing.T) {
	// GET_LIST: the ACTIVE catalog is returned, each with its tool_type name;
	// archived rows are filtered out by the store.
	svc, store, _ := newToolService()
	store.tools = []*Tool{
		toolFixture("id-a", "Bohrmaschine-01"),
		toolFixture("id-b", "Bohrmaschine-02"),
	}
	archived := toolFixture("id-arch", "Alt")
	now := time.Now()
	archived.ArchivedAt = &now
	store.tools = append(store.tools, archived)

	got, err := svc.ListTools(context.Background(), actorID)
	if err != nil {
		t.Fatalf("ListTools err = %v", err)
	}
	if len(got) != 2 || got[0].ID != "id-a" || got[1].ID != "id-b" {
		t.Fatalf("tools = %+v, want both active rows in order", got)
	}
	if got[0].ToolTypeName != "Bohrmaschine" {
		t.Errorf("tool_type_name = %q, want the JOINed type name", got[0].ToolTypeName)
	}
}

func TestCreateToolValid(t *testing.T) {
	// CREATE_VALID: name trimmed, no override → ScheduleID empty (inherit the
	// type default, AD-5), attributes defaulted, audited (tool.create).
	svc, store, audit := newToolService()
	got, err := svc.CreateTool(context.Background(), actorID, toolInput())
	if err != nil {
		t.Fatalf("CreateTool err = %v", err)
	}
	if got.Name != "Bohrmaschine-01" {
		t.Errorf("name = %q, want trimmed", got.Name)
	}
	if got.ToolTypeID != "id-t1" {
		t.Errorf("tool_type_id = %q, want id-t1", got.ToolTypeID)
	}
	if got.ScheduleID != "" {
		t.Errorf("schedule_id = %q, want empty (inherit the type default)", got.ScheduleID)
	}
	if got.ArchivedAt != nil {
		t.Error("new tool must be active (ArchivedAt nil)")
	}
	if len(store.created) != 1 {
		t.Fatalf("created = %d, want 1", len(store.created))
	}
	if store.created[0].ScheduleID != "" {
		t.Errorf("persisted schedule override = %q, want empty", store.created[0].ScheduleID)
	}
	if len(store.created[0].Attributes) != 0 {
		t.Errorf("attributes = %v, want empty map default", store.created[0].Attributes)
	}
	// CREATE_AUTO: the inventory number is NOT carried by the create path — the
	// store auto-assigns it in-SQL (the core never forwards a client value).
	if store.created[0].InventoryNumber != "" {
		t.Errorf("persisted inventory_number = %q, want empty (server auto-assigns on create)", store.created[0].InventoryNumber)
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationToolCreate {
		t.Fatalf("audit events = %+v, want one create audit", audit.events)
	}
	if !strings.Contains(audit.events[0].detail, "id="+got.ID) {
		t.Errorf("create audit detail = %q, want it to carry the created id %q", audit.events[0].detail, got.ID)
	}
	if audit.events[0].actorID != actorID || audit.events[0].severity != AuditSeverityNormal {
		t.Errorf("audit actor/severity = %+v", audit.events[0])
	}
}

func TestCreateToolWithOverride(t *testing.T) {
	// CREATE_OVERRIDE: name + type + a valid ACTIVE schedule override →
	// persisted with the FK override, audited.
	svc, store, audit := newToolService()
	input := toolInput()
	input.ScheduleID = "id-s1"
	got, err := svc.CreateTool(context.Background(), actorID, input)
	if err != nil {
		t.Fatalf("CreateTool(override) err = %v", err)
	}
	if got.ScheduleID != "id-s1" {
		t.Errorf("schedule_id = %q, want id-s1", got.ScheduleID)
	}
	if len(store.created) != 1 || store.created[0].ScheduleID != "id-s1" {
		t.Errorf("persisted = %+v, want the override stored", store.created)
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationToolCreate {
		t.Fatalf("audit events = %+v, want one create audit", audit.events)
	}
}

func TestCreateToolEmptyOverrideSkipsPort(t *testing.T) {
	// AD-5: an EMPTY override is allowed and skips the SchedulesPort entirely —
	// even a NIL (unwired) port must not fail the create (mirrors the
	// optional-qualification precedent). Nothing to validate, nothing to skip.
	svc, store, _ := newToolService()
	svc.schedules = nil
	got, err := svc.CreateTool(context.Background(), actorID, toolInput())
	if err != nil {
		t.Fatalf("CreateTool(empty override, nil port) err = %v, want success", err)
	}
	if got.ScheduleID != "" {
		t.Errorf("schedule_id = %q, want empty", got.ScheduleID)
	}
	if len(store.created) != 1 {
		t.Fatalf("created = %d, want 1", len(store.created))
	}
}

func TestCreateToolBadType(t *testing.T) {
	// CREATE_BAD_TYPE: the tool's type does not exist OR is archived → 400
	// German (intra-module ToolTypeExistsActive), nothing persisted.
	t.Run("missing type", func(t *testing.T) {
		svc, store, _ := newToolService()
		input := toolInput()
		input.ToolTypeID = "id-missing"
		_, err := svc.CreateTool(context.Background(), actorID, input)
		var inv *InvalidToolError
		if !errors.As(err, &inv) {
			t.Fatalf("err = %v, want *InvalidToolError", err)
		}
		if inv.Message != MsgToolInvalidType {
			t.Errorf("message = %q, want %q", inv.Message, MsgToolInvalidType)
		}
		if len(store.created) != 0 {
			t.Error("tool must not be persisted with a bad type FK")
		}
	})
	t.Run("archived type", func(t *testing.T) {
		svc, store, _ := newToolService()
		archived := &ToolType{ID: "id-arch", Name: "Alt"}
		now := time.Now()
		archived.ArchivedAt = &now
		svc.store = &fakeToolStore{types: []*ToolType{archived}}
		input := toolInput()
		input.ToolTypeID = "id-arch"
		_, err := svc.CreateTool(context.Background(), actorID, input)
		var inv *InvalidToolError
		if !errors.As(err, &inv) {
			t.Fatalf("err = %v, want *InvalidToolError", err)
		}
		if inv.Message != MsgToolInvalidType {
			t.Errorf("message = %q, want %q", inv.Message, MsgToolInvalidType)
		}
		if len(store.created) != 0 {
			t.Error("tool must not be persisted with an archived type FK")
		}
	})
}

func TestCreateToolBadOverride(t *testing.T) {
	// CREATE_BAD_OVERRIDE: the schedule override is unknown/archived → 400
	// German (port lookup), nothing persisted.
	svc, store, _ := newToolService()
	input := toolInput()
	input.ScheduleID = "id-missing-schedule"
	_, err := svc.CreateTool(context.Background(), actorID, input)
	var inv *InvalidToolError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *InvalidToolError", err)
	}
	if inv.Message != MsgToolInvalidSchedule {
		t.Errorf("message = %q, want %q", inv.Message, MsgToolInvalidSchedule)
	}
	if len(store.created) != 0 {
		t.Error("tool must not be persisted with a bad override FK")
	}
}

func TestCreateToolInvalid(t *testing.T) {
	// CREATE_INVALID: empty name / too-long name / missing type → 400-class
	// German error, nothing persisted.
	cases := []struct {
		name    string
		mutate  func(*ToolInput)
		wantMsg string
	}{
		{"empty name", func(in *ToolInput) { in.Name = "  " }, "Namen"},
		{"name too long", func(in *ToolInput) { in.Name = strings.Repeat("ä", 256) }, "zu lang"},
		{"missing type", func(in *ToolInput) { in.ToolTypeID = "" }, "Gerätetyp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, store, _ := newToolService()
			input := toolInput()
			tc.mutate(&input)
			_, err := svc.CreateTool(context.Background(), actorID, input)
			var inv *InvalidToolError
			if !errors.As(err, &inv) {
				t.Fatalf("err = %v, want *InvalidToolError", err)
			}
			if !strings.Contains(inv.Message, tc.wantMsg) {
				t.Errorf("message = %q, want contains %q", inv.Message, tc.wantMsg)
			}
			if !errors.Is(err, ErrToolInvalid) {
				t.Errorf("err = %v, want unwraps to ErrToolInvalid", err)
			}
			if len(store.created) != 0 {
				t.Error("tool must not be persisted on invalid input")
			}
		})
	}
}

func TestCreateToolDuplicateName(t *testing.T) {
	// CREATE_DUPLICATE: a create whose name another tool already holds
	// (case-insensitive) is rejected, not persisted.
	svc, store, _ := newToolService()
	store.tools = []*Tool{toolFixture("id-a", "Bohrmaschine-01")}

	input := toolInput()
	input.Name = "bohrmaschine-01"
	_, err := svc.CreateTool(context.Background(), actorID, input)
	var inv *InvalidToolError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *InvalidToolError", err)
	}
	if inv.Message != MsgToolNameTaken {
		t.Errorf("message = %q, want %q", inv.Message, MsgToolNameTaken)
	}
	if len(store.created) != 0 {
		t.Error("duplicate-name tool must not be persisted")
	}
}

func TestUpdateToolClearsOverride(t *testing.T) {
	// UPDATE_CLEAR_OVERRIDE: editing a tool with an override and removing it
	// persists an EMPTY schedule_id (inherit the type default again, AD-5),
	// audited (tool.update).
	svc, store, audit := newToolService()
	store.tools = []*Tool{toolFixture("id-a", "Bohrmaschine-01")}

	input := toolInput()
	input.Name = "Bohrmaschine-01-neu"
	input.ScheduleID = "" // clearing the override
	got, err := svc.UpdateTool(context.Background(), actorID, "id-a", input)
	if err != nil {
		t.Fatalf("UpdateTool err = %v", err)
	}
	if got.ScheduleID != "" {
		t.Errorf("schedule_id = %q, want empty after clearing the override", got.ScheduleID)
	}
	if got.Name != "Bohrmaschine-01-neu" {
		t.Errorf("name = %q, want updated", got.Name)
	}
	if len(store.updated) != 1 {
		t.Fatalf("updated = %d, want 1", len(store.updated))
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationToolUpdate {
		t.Fatalf("audit events = %+v, want one update audit", audit.events)
	}
}

func TestUpdateToolNotFound(t *testing.T) {
	svc, _, _ := newToolService()
	if _, err := svc.UpdateTool(context.Background(), actorID, "id-missing", toolInput()); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("err = %v, want ErrToolNotFound", err)
	}
}

func TestUpdateToolKeepsItsName(t *testing.T) {
	// The dominant edit flow keeps the tool's name: the exceptID exclusion in
	// the duplicate-name guard must let a tool keep its OWN name (a regression
	// there would break every name-preserving edit).
	svc, store, _ := newToolService()
	store.tools = []*Tool{toolFixture("id-a", "Bohrmaschine-01")}

	input := toolInput() // name "Bohrmaschine-01" == the stored name
	input.ScheduleID = "id-s1" // change only the override
	got, err := svc.UpdateTool(context.Background(), actorID, "id-a", input)
	if err != nil {
		t.Fatalf("UpdateTool(keep name) err = %v, want success", err)
	}
	if got.Name != "Bohrmaschine-01" {
		t.Errorf("name = %q, want unchanged", got.Name)
	}
	if got.ScheduleID != "id-s1" {
		t.Errorf("schedule override = %q, want id-s1 (the only changed field)", got.ScheduleID)
	}
	if len(store.updated) != 1 {
		t.Fatalf("updated = %d, want 1", len(store.updated))
	}
}

func TestUpdateToolDuplicateName(t *testing.T) {
	// Updating a tool to a name another ACTIVE tool already holds
	// (case-insensitive) is rejected with the German duplicate-name 400, and
	// nothing is persisted.
	svc, store, _ := newToolService()
	store.tools = []*Tool{
		toolFixture("id-a", "Bohrmaschine-01"),
		toolFixture("id-b", "Bohrmaschine-02"),
	}

	input := toolInput()
	input.Name = "bohrmaschine-02" // case-variant of id-b's name
	_, err := svc.UpdateTool(context.Background(), actorID, "id-a", input)
	var inv *InvalidToolError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *InvalidToolError", err)
	}
	if inv.Message != MsgToolNameTaken {
		t.Errorf("message = %q, want %q", inv.Message, MsgToolNameTaken)
	}
	if !errors.Is(err, ErrToolInvalid) {
		t.Errorf("err = %v, want unwraps to ErrToolInvalid", err)
	}
	if len(store.updated) != 0 {
		t.Error("duplicate-name update must not persist")
	}
}

func TestUpdateToolArchived(t *testing.T) {
	// UPDATE_ARCHIVED: updating an already-archived tool answers the 404
	// sentinel — the archived row is non-existent to the surface.
	svc, store, audit := newToolService()
	archived := toolFixture("id-arch", "Alt")
	now := time.Now()
	archived.ArchivedAt = &now
	store.tools = []*Tool{archived}

	if _, err := svc.UpdateTool(context.Background(), actorID, "id-arch", toolInput()); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("err = %v, want ErrToolNotFound", err)
	}
	if len(store.updated) != 0 {
		t.Error("archived tool must not be updated")
	}
	if len(audit.events) != 0 {
		t.Errorf("audit events = %+v, want none for a rejected update", audit.events)
	}
}

func TestUpdateToolArchivedSentinelWinsOverInvalidFK(t *testing.T) {
	// The archived-sentinel-wins-over-400 contract: an update against an
	// already-archived (or unknown) id answers the 404 sentinel EVEN when the
	// submitted body carries a currently-invalid type/schedule — the existence
	// check runs before the FK validation.
	svc, store, _ := newToolService()
	archived := toolFixture("id-arch", "Alt")
	now := time.Now()
	archived.ArchivedAt = &now
	store.tools = []*Tool{archived}

	input := toolInput()
	input.ToolTypeID = "id-missing-type"   // invalid: would 400 a live tool
	input.ScheduleID = "id-missing-schedule" // invalid: would 400 a live tool
	if _, err := svc.UpdateTool(context.Background(), actorID, "id-arch", input); !errors.Is(err, ErrToolNotFound) {
		var inv *InvalidToolError
		if errors.As(err, &inv) {
			t.Fatalf("err = %v (InvalidToolError %q), want ErrToolNotFound (404 sentinel wins over 400)", err, inv.Message)
		}
		t.Fatalf("err = %v, want ErrToolNotFound", err)
	}
	if _, err := svc.UpdateTool(context.Background(), actorID, "id-missing", input); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("unknown id with invalid FK: err = %v, want ErrToolNotFound", err)
	}
}

func TestUpdateToolArchivedSentinelWinsOverInvalidInventory(t *testing.T) {
	// The archived-sentinel-wins-over-400 contract (finding 12) extended to the
	// inventory number: an update against an already-archived (or unknown) id
	// answers the 404 sentinel EVEN when the submitted body carries an EMPTY or
	// over-long inventory number (which would 400 a live tool) — the existence
	// check runs BEFORE the inventory validation.
	svc, store, _ := newToolService()
	archived := toolFixture("id-arch", "Alt")
	now := time.Now()
	archived.ArchivedAt = &now
	store.tools = []*Tool{archived}

	for _, mutate := range []func(*ToolInput){
		func(in *ToolInput) { in.InventoryNumber = "" },        // UPDATE_CLEAR: would 400 a live tool
		func(in *ToolInput) { in.InventoryNumber = strings.Repeat("x", 17) }, // over-long: would 400 a live tool
	} {
		input := toolInput()
		mutate(&input)
		if _, err := svc.UpdateTool(context.Background(), actorID, "id-arch", input); !errors.Is(err, ErrToolNotFound) {
			var inv *InvalidToolError
			if errors.As(err, &inv) {
				t.Fatalf("err = %v (InvalidToolError %q), want ErrToolNotFound (404 sentinel wins over 400)", err, inv.Message)
			}
			t.Fatalf("archived id with invalid inventory: err = %v, want ErrToolNotFound", err)
		}
		if _, err := svc.UpdateTool(context.Background(), actorID, "id-missing", input); !errors.Is(err, ErrToolNotFound) {
			t.Fatalf("unknown id with invalid inventory: err = %v, want ErrToolNotFound", err)
		}
	}
}

func TestArchiveTool(t *testing.T) {
	// ARCHIVE: archived_at set, the row leaves the active list, audited
	// (tool.archive).
	svc, store, audit := newToolService()
	store.tools = []*Tool{toolFixture("id-a", "Bohrmaschine-01")}

	got, err := svc.ArchiveTool(context.Background(), actorID, "id-a")
	if err != nil {
		t.Fatalf("ArchiveTool err = %v", err)
	}
	if got.ArchivedAt == nil {
		t.Error("archived_at = nil, want set")
	}
	if len(store.archivedIDs) != 1 || store.archivedIDs[0] != "id-a" {
		t.Errorf("archived = %v, want [id-a]", store.archivedIDs)
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationToolArchive {
		t.Fatalf("audit events = %+v, want one archive audit", audit.events)
	}

	active, err := svc.ListTools(context.Background(), actorID)
	if err != nil {
		t.Fatalf("ListTools err = %v", err)
	}
	if len(active) != 0 {
		t.Errorf("active = %d, want 0 after archive", len(active))
	}
}

func TestArchiveToolArchived(t *testing.T) {
	// ARCHIVE_ARCHIVED: archiving an already-archived row answers the 404
	// sentinel.
	svc, store, _ := newToolService()
	archived := toolFixture("id-arch", "Alt")
	now := time.Now()
	archived.ArchivedAt = &now
	store.tools = []*Tool{archived}

	if _, err := svc.ArchiveTool(context.Background(), actorID, "id-arch"); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("err = %v, want ErrToolNotFound", err)
	}
	if len(store.archivedIDs) != 0 {
		t.Error("already-archived tool must not be re-archived")
	}
}

func TestArchiveToolNotFound(t *testing.T) {
	svc, _, _ := newToolService()
	if _, err := svc.ArchiveTool(context.Background(), actorID, "id-missing"); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("err = %v, want ErrToolNotFound", err)
	}
}

func TestToolsForbidden(t *testing.T) {
	// FORBIDDEN: every tool method re-checks the live permission.
	svc, store, _ := newToolService("dashboard.view")
	store.tools = []*Tool{toolFixture("id-a", "Bohrmaschine-01")}
	if _, err := svc.ListTools(context.Background(), actorID); !errors.Is(err, ErrForbidden) {
		t.Errorf("list err = %v, want ErrForbidden", err)
	}
	if _, err := svc.CreateTool(context.Background(), actorID, toolInput()); !errors.Is(err, ErrForbidden) {
		t.Errorf("create err = %v, want ErrForbidden", err)
	}
	if _, err := svc.UpdateTool(context.Background(), actorID, "id-a", toolInput()); !errors.Is(err, ErrForbidden) {
		t.Errorf("update err = %v, want ErrForbidden", err)
	}
	if _, err := svc.ArchiveTool(context.Background(), actorID, "id-a"); !errors.Is(err, ErrForbidden) {
		t.Errorf("archive err = %v, want ErrForbidden", err)
	}
}

func TestListToolsForDashboardUngated(t *testing.T) {
	// Story 4-3b: the dashboard read must be reachable by ANY dashboard.view
	// holder — even one WITHOUT tools.manage (e.g. Helfer*in). The core method
	// deliberately does NOT re-check tools.manage (the HTTP surface carries the
	// dashboard.view gate instead), while the admin ListTools still 403s for
	// the same caller — defense-in-depth for the admin surface is unchanged.
	svc, store, _ := newToolService("dashboard.view")
	store.tools = []*Tool{
		toolFixture("id-a", "Bohrmaschine-01"),
		toolFixture("id-b", "Bohrmaschine-02"),
	}
	archived := toolFixture("id-arch", "Alt")
	now := time.Now()
	archived.ArchivedAt = &now
	store.tools = append(store.tools, archived)

	got, err := svc.ListToolsForDashboard(context.Background())
	if err != nil {
		t.Fatalf("ListToolsForDashboard err = %v (dashboard.view holder without tools.manage), want success", err)
	}
	if len(got) != 2 || got[0].ID != "id-a" || got[1].ID != "id-b" {
		t.Fatalf("tools = %+v, want both ACTIVE rows (archived filtered by the store)", got)
	}
	if got[0].ToolTypeName != "Bohrmaschine" {
		t.Errorf("tool_type_name = %q, want the JOINed type name", got[0].ToolTypeName)
	}

	// The admin surface stays gated for the same caller: tools.manage-less
	// dashboard.view holders cannot use ListTools.
	if _, err := svc.ListTools(context.Background(), actorID); !errors.Is(err, ErrForbidden) {
		t.Errorf("ListTools err = %v, want ErrForbidden for the tools.manage-less caller", err)
	}
}

func TestListToolsForDashboardEmpty(t *testing.T) {
	// GET_LIST_EMPTY: no tools → empty list, no error — the SPA keeps the
	// "Keine Werkzeuge vorhanden" EmptyState.
	svc, _, _ := newToolService()
	got, err := svc.ListToolsForDashboard(context.Background())
	if err != nil {
		t.Fatalf("ListToolsForDashboard err = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("tools = %d, want 0", len(got))
	}
}

func TestListToolsForDashboardStoreError(t *testing.T) {
	// The store failure surfaces as an internal error (500-style), never a
	// permission sentinel.
	svc, store, _ := newToolService()
	store.listErr = errors.New("boom")
	if _, err := svc.ListToolsForDashboard(context.Background()); err == nil {
		t.Fatal("ListToolsForDashboard err = nil, want store error propagated")
	}
}

func TestToolsEmptyActorNeverPasses(t *testing.T) {
	// Defense-in-depth: an empty actor ID never passes even when the resolver
	// would grant the permission.
	svc, _, _ := newToolService()
	svc.perms = &fakePerms{perms: []string{ToolsManagePermission}}
	if _, err := svc.ListTools(context.Background(), ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestCreateToolIgnoresClientInventory(t *testing.T) {
	// CREATE_IGNORE_CLIENT: a client-sent inventory_number on create is IGNORED —
	// the persisted tool carries an EMPTY number (the store auto-assigns
	// 'GEAR%06d' in-SQL). Nothing is validated against the client value.
	svc, store, _ := newToolService()
	input := toolInput()
	input.InventoryNumber = "GEAR999999"
	got, err := svc.CreateTool(context.Background(), actorID, input)
	if err != nil {
		t.Fatalf("CreateTool err = %v", err)
	}
	if got.InventoryNumber != "" {
		t.Errorf("returned inventory_number = %q, want empty (server-assigned, not yet set at the core)", got.InventoryNumber)
	}
	if len(store.created) != 1 || store.created[0].InventoryNumber != "" {
		t.Fatalf("persisted = %+v, want the client inventory ignored (empty)", store.created)
	}
}

func TestUpdateToolInventory(t *testing.T) {
	// UPDATE_INVENTORY: editing a tool with a new inventory number persists it
	// (bounded, non-empty), audited (tool.update).
	svc, store, audit := newToolService()
	store.tools = []*Tool{toolFixture("id-a", "Bohrmaschine-01")}

	input := toolInput()
	input.InventoryNumber = "GEAR000042"
	got, err := svc.UpdateTool(context.Background(), actorID, "id-a", input)
	if err != nil {
		t.Fatalf("UpdateTool err = %v", err)
	}
	if got.InventoryNumber != "GEAR000042" {
		t.Errorf("inventory_number = %q, want GEAR000042", got.InventoryNumber)
	}
	if len(store.updated) != 1 || store.updated[0].InventoryNumber != "GEAR000042" {
		t.Errorf("persisted = %+v, want the edited inventory number", store.updated)
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationToolUpdate {
		t.Fatalf("audit events = %+v, want one update audit", audit.events)
	}
}

func TestUpdateToolInventoryCleared(t *testing.T) {
	// UPDATE_CLEAR: an EMPTY inventory_number is rejected 400 — a tool always
	// has an inventory number, it can never be cleared.
	svc, store, _ := newToolService()
	store.tools = []*Tool{toolFixture("id-a", "Bohrmaschine-01")}

	input := toolInput()
	input.InventoryNumber = "  "
	_, err := svc.UpdateTool(context.Background(), actorID, "id-a", input)
	var inv *InvalidToolError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *InvalidToolError", err)
	}
	if inv.Message != MsgToolInventoryNumberRequired {
		t.Errorf("message = %q, want %q", inv.Message, MsgToolInventoryNumberRequired)
	}
	if !errors.Is(err, ErrToolInvalid) {
		t.Errorf("err = %v, want unwraps to ErrToolInvalid", err)
	}
	if len(store.updated) != 0 {
		t.Error("cleared-inventory update must not persist")
	}
}

func TestUpdateToolInventoryTooLong(t *testing.T) {
	// UPDATE_INVENTORY too long: a number over the 16-rune bound is rejected 400.
	svc, store, _ := newToolService()
	store.tools = []*Tool{toolFixture("id-a", "Bohrmaschine-01")}

	input := toolInput()
	input.InventoryNumber = "GEAR" + strings.Repeat("0", 13) // 17 runes
	_, err := svc.UpdateTool(context.Background(), actorID, "id-a", input)
	var inv *InvalidToolError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *InvalidToolError", err)
	}
	if inv.Message != MsgToolInventoryNumberTooLong {
		t.Errorf("message = %q, want %q", inv.Message, MsgToolInventoryNumberTooLong)
	}
	if len(store.updated) != 0 {
		t.Error("over-long inventory update must not persist")
	}
}

func TestUpdateToolInventoryDuplicate(t *testing.T) {
	// UPDATE_INVENTORY uniqueness: an inventory number already held by ANOTHER
	// ACTIVE tool (case-insensitive) is rejected with the German duplicate 400.
	svc, store, _ := newToolService()
	store.tools = []*Tool{
		toolFixture("id-a", "Bohrmaschine-01"),
		{ID: "id-b", Name: "Bohrmaschine-02", ToolTypeID: "id-t1", ToolTypeName: "Bohrmaschine", ScheduleID: "id-s1", InventoryNumber: "GEAR000002"},
	}

	input := toolInput()
	input.InventoryNumber = "gear000002" // case-variant of id-b's number
	_, err := svc.UpdateTool(context.Background(), actorID, "id-a", input)
	var inv *InvalidToolError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *InvalidToolError", err)
	}
	if inv.Message != MsgToolInventoryNumberTaken {
		t.Errorf("message = %q, want %q", inv.Message, MsgToolInventoryNumberTaken)
	}
	if !errors.Is(err, ErrToolInvalid) {
		t.Errorf("err = %v, want unwraps to ErrToolInvalid", err)
	}
	if len(store.updated) != 0 {
		t.Error("duplicate-inventory update must not persist")
	}
}

func TestUpdateToolKeepsItsInventoryNumber(t *testing.T) {
	// The dominant edit flow keeps the tool's inventory number: the exceptID
	// exclusion in the duplicate guard must let a tool keep its OWN number (a
	// regression there would break every inventory-preserving edit).
	svc, store, _ := newToolService()
	store.tools = []*Tool{toolFixture("id-a", "Bohrmaschine-01")}

	input := toolInput() // inventory "GEAR000001" == the stored "GEAR00000X"? No:
	// the stored fixture uses GEAR00000X; keep it unchanged by passing it.
	input.InventoryNumber = "GEAR00000X"
	got, err := svc.UpdateTool(context.Background(), actorID, "id-a", input)
	if err != nil {
		t.Fatalf("UpdateTool(keep inventory) err = %v, want success", err)
	}
	if got.InventoryNumber != "GEAR00000X" {
		t.Errorf("inventory_number = %q, want unchanged GEAR00000X", got.InventoryNumber)
	}
	if len(store.updated) != 1 {
		t.Fatalf("updated = %d, want 1", len(store.updated))
	}
}

func TestToolEditOnlyGate(t *testing.T) {
	// GATE (Story 4-3b): a tool.edit-ONLY holder (no tools.manage) can
	// ListTools + UpdateTool (any-of) but CreateTool + ArchiveTool answer
	// ErrForbidden (tools.manage-only).
	svc, store, _ := newToolService(ToolEditPermission)
	store.tools = []*Tool{toolFixture("id-a", "Bohrmaschine-01")}

	got, err := svc.ListTools(context.Background(), actorID)
	if err != nil {
		t.Fatalf("ListTools err = %v, want success for a tool.edit holder", err)
	}
	if len(got) != 1 {
		t.Fatalf("tools = %d, want 1", len(got))
	}

	if _, err := svc.UpdateTool(context.Background(), actorID, "id-a", toolInput()); err != nil {
		t.Fatalf("UpdateTool err = %v, want success for a tool.edit holder", err)
	}

	if _, err := svc.CreateTool(context.Background(), actorID, toolInput()); !errors.Is(err, ErrForbidden) {
		t.Errorf("create err = %v, want ErrForbidden for a tool.edit-only holder", err)
	}
	if _, err := svc.ArchiveTool(context.Background(), actorID, "id-a"); !errors.Is(err, ErrForbidden) {
		t.Errorf("archive err = %v, want ErrForbidden for a tool.edit-only holder", err)
	}
	if len(store.created) != 0 {
		t.Error("tool.edit-only holder must not reach the store create")
	}
	if len(store.archivedIDs) != 0 {
		t.Error("tool.edit-only holder must not reach the store archive")
	}
}

func TestCreateToolNilOverridePortFailsLoudly(t *testing.T) {
	// A NIL SchedulesPort is a composition-root wiring defect. With a
	// NON-EMPTY override the write path must FAIL LOUDLY (a 500-style internal
	// error, never a validation sentinel / ErrForbidden) — never silently skip
	// the AD-16 validation. (An EMPTY override skips the port and succeeds —
	// pinned by TestCreateToolEmptyOverrideSkipsPort.)
	store := &fakeToolStore{types: []*ToolType{{ID: "id-t1", Name: "Bohrmaschine"}}}
	svc := NewService(
		store,
		nil,
		nil,
		&fakePerms{perms: []string{ToolsManagePermission}},
		&fakeAudit{},
		nil,
	)
	input := toolInput()
	input.ScheduleID = "id-s1"
	if _, err := svc.CreateTool(context.Background(), actorID, input); err == nil {
		t.Fatal("CreateTool with nil port + non-empty override err = nil, want internal error")
	} else {
		var inv *InvalidToolError
		if errors.As(err, &inv) {
			t.Fatalf("CreateTool err = %v, want a 500-style internal error, not a validation sentinel", err)
		}
		if errors.Is(err, ErrForbidden) {
			t.Fatalf("CreateTool err = %v, want internal error, not ErrForbidden", err)
		}
	}
	if len(store.created) != 0 {
		t.Error("nil-port write must not persist")
	}
}