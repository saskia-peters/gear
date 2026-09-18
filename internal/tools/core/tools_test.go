package core

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
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
	inspections []*Inspection
	status      *ToolInspectionStatus
	statusErr   error
	// statusErrFromRead makes GetToolInspectionStatus fail ONLY from the Nth
	// read onward (1-based; 0 = never). Story 5.6: the submit OOS gate reads the
	// status BEFORE the write, the post-commit status read AFTER — a test can
	// let the gate read succeed and the post-commit read fail (the best-effort
	// path).
	statusErrFromRead int
	statusReads       int
	// statusNilResult makes GetToolInspectionStatus return (nil, nil) — the
	// dashboard read must treat it as never-inspected (red), never panic.
	statusNilResult bool
	// statusByTool / statusErrByTool drive PER-TOOL status fixtures for the
	// dashboard list (Story 6.1): keyed by tool id, falling back to the shared
	// fields above.
	statusByTool    map[string]*ToolInspectionStatus
	statusErrByTool map[string]error
	// latestByTool drives the PER-TOOL latest-inspection fixtures for the status
	// report (Story 6.2): keyed by tool id; a tool ABSENT from the map reads as
	// never-inspected (nil latest — the report renders "–"). latestByToolErr
	// lets tests simulate a read failure.
	latestByTool    map[string]*LatestInspection
	latestByToolErr error
	// reinstatements / reinstateErr back the Story 5.6 InsertReinstatement
	// write: tests assert the persisted actor + reason; an error simulates a
	// storage failure.
	reinstatements []reinstatementRecord
	reinstateErr   error
}

// reinstatementRecord is the in-memory reinstatement row (Story 5.6). CreatedAt
// mirrors the DB created_at = now() so tests can pin the derived clock anchor.
type reinstatementRecord struct {
	ToolID    string
	ActorID   string
	Reason    string
	CreatedAt time.Time
}

// InsertInspection emulates the repository's transactional insert (Story 5.3):
// the record is assigned an id + submitted_at (the DB uuidv7 + now()) and kept
// so tests can assert the persisted shape. It ALSO mirrors the real DB read
// semantics: a committed inspection anchors the status fixture — a fail becomes
// the latest fail (the OOS anchor), a pass the latest success (the clock
// anchor) — so a post-submit GetToolInspectionStatus reflects the new record,
// exactly like the repository's read-after-write.
func (f *fakeToolStore) InsertInspection(_ context.Context, inspection *Inspection) (*Inspection, error) {
	persisted := *inspection
	if persisted.ID == "" {
		persisted.ID = "id-insp-" + inspection.ToolID
	}
	if persisted.SubmittedAt.IsZero() {
		persisted.SubmittedAt = time.Now()
	}
	f.inspections = append(f.inspections, &persisted)
	if f.status == nil {
		f.status = &ToolInspectionStatus{}
	}
	t := persisted.SubmittedAt
	if persisted.OverallResult == InspectionResultFail {
		f.status.LatestFailAt = &t
	} else if f.status.LastSuccessAt == nil || t.After(*f.status.LastSuccessAt) {
		f.status.LastSuccessAt = &t
	}
	return &persisted, nil
}

// GetToolInspectionStatus returns the status-read fixture a test set (nil-safe:
// an unset fixture reads as a never-inspected tool). statusErr lets tests
// simulate a post-commit status-read failure (the best-effort path); the
// per-tool statusByTool / statusErrByTool maps let the dashboard tests drive
// DIFFERENT statuses per tool in one call (Story 6.1), with the shared fields
// as the fallback.
func (f *fakeToolStore) GetToolInspectionStatus(_ context.Context, toolID string) (*ToolInspectionStatus, error) {
	f.statusReads++
	if f.statusNilResult {
		return nil, nil
	}
	if f.statusErrByTool != nil {
		if err, ok := f.statusErrByTool[toolID]; ok {
			return nil, err
		}
	}
	if f.statusByTool != nil {
		if status, ok := f.statusByTool[toolID]; ok {
			return status, nil
		}
	}
	if f.statusErr != nil && (f.statusErrFromRead == 0 || f.statusReads >= f.statusErrFromRead) {
		return nil, f.statusErr
	}
	if f.status != nil {
		return f.status, nil
	}
	return &ToolInspectionStatus{}, nil
}

// InsertReinstatement emulates the repository's single-row insert (Story 5.6):
// the row is kept (actor + reason + created_at = now) so tests can assert the
// persisted shape. It ALSO mirrors the real DB read semantics: the row becomes
// the clock's "latest reinstatement" anchor (LastReinstatedAt), so a post-write
// GetToolInspectionStatus reflects the new row — exactly like InsertInspection's
// read-after-write upgrade — and the tool leaves OOS.
func (f *fakeToolStore) InsertReinstatement(_ context.Context, toolID, actorID, reason string) error {
	if f.reinstateErr != nil {
		return f.reinstateErr
	}
	createdAt := time.Now()
	f.reinstatements = append(f.reinstatements, reinstatementRecord{ToolID: toolID, ActorID: actorID, Reason: reason, CreatedAt: createdAt})
	if f.status == nil {
		f.status = &ToolInspectionStatus{}
	}
	t := createdAt
	f.status.LastReinstatedAt = &t
	return nil
}

// ListInspectionsByTool returns the tool's inspection history (Story 6.3),
// newest first (submitted_at DESC, id DESC) — mirroring the repository's SQL
// ORDER BY so the core's pass-through ordering is faithfully exercised. The
// items the inspection was inserted with are returned alongside.
func (f *fakeToolStore) ListInspectionsByTool(_ context.Context, toolID string) ([]*Inspection, error) {
	var out []*Inspection
	for _, insp := range f.inspections {
		if insp.ToolID == toolID {
			out = append(out, insp)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SubmittedAt.Equal(out[j].SubmittedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].SubmittedAt.After(out[j].SubmittedAt)
	})
	return out, nil
}

// ListReinstatementsByTool returns the tool's reinstatement ledger (Story 6.3),
// newest first (created_at DESC, id DESC) — mirroring the repository's SQL
// ORDER BY.
func (f *fakeToolStore) ListReinstatementsByTool(_ context.Context, toolID string) ([]*Reinstatement, error) {
	var out []*Reinstatement
	for i, r := range f.reinstatements {
		if r.ToolID == toolID {
			out = append(out, &Reinstatement{
				ID:        fmt.Sprintf("id-rein-%d", i),
				ToolID:    r.ToolID,
				ActorID:   r.ActorID,
				Reason:    r.Reason,
				CreatedAt: r.CreatedAt,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

// GetLatestInspectionByTool returns the tool's latest-inspection fixture (Story
// 6.2): the per-tool latestByTool map, with a tool ABSENT from the map reading
// as never-inspected (nil — the report renders "–"). latestByToolErr lets tests
// simulate a read failure.
func (f *fakeToolStore) GetLatestInspectionByTool(_ context.Context, toolID string) (*LatestInspection, error) {
	if f.latestByToolErr != nil {
		return nil, f.latestByToolErr
	}
	return f.latestByTool[toolID], nil
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
			// Emulate the repository's COALESCE keep (Story 4.4): a NIL
			// attributes map (absent field) leaves the stored JSONB unchanged.
			if tool.Attributes == nil {
				persisted.Attributes = existing.Attributes
			}
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

// GetToolWithTypeQualification is the lean inspection-start read (Story 5.1):
// the ACTIVE tool plus its type's required_qualification_id and inspection_mode
// (intra-module JOIN on the Tool-owned types). A missing or archived tool (or a
// tool whose type is absent from the fake — a wiring defect) answers
// ErrToolNotFound. Story 5.3 also carries the schedule-resolution inputs
// (ScheduleID + DefaultScheduleID) and the type's ordered checklist items.
func (f *fakeToolStore) GetToolWithTypeQualification(_ context.Context, id string) (*ToolWithTypeQualification, error) {
	for _, t := range f.tools {
		if t.ID != id {
			continue
		}
		if t.ArchivedAt != nil {
			return nil, ErrToolNotFound
		}
		for _, tt := range f.types {
			if tt.ID == t.ToolTypeID {
				return &ToolWithTypeQualification{
					ID:                      t.ID,
					Name:                    t.Name,
					ToolTypeID:              t.ToolTypeID,
					ToolTypeName:            t.ToolTypeName,
					RequiredQualificationID: tt.RequiredQualificationID,
					InspectionMode:          tt.InspectionMode,
					ScheduleID:              t.ScheduleID,
					DefaultScheduleID:       tt.DefaultScheduleID,
					ChecklistItems:          tt.Items,
				}, nil
			}
		}
		return nil, ErrToolNotFound
	}
	return nil, ErrToolNotFound
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
		nil,
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

	input := toolInput()       // name "Bohrmaschine-01" == the stored name
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
	input.ToolTypeID = "id-missing-type"     // invalid: would 400 a live tool
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
		func(in *ToolInput) { in.InventoryNumber = "" },                      // UPDATE_CLEAR: would 400 a live tool
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

// dashboardService wires a Service whose ONE ACTIVE schedule (id-s1, 30 days)
// resolves a real interval and a tool type whose default schedule is id-s1 —
// so dashboard tools derive real statuses (Story 6.1). Tools are set per test.
func dashboardService() (*Service, *fakeToolStore) {
	store := &fakeToolStore{
		types: []*ToolType{{
			ID: "id-t1", Name: "Bohrmaschine", DefaultScheduleID: "id-s1",
			InspectionMode: InspectionModePassFail,
		}},
	}
	svc := NewService(
		store,
		&fakeSchedulesPort{schedules: []*admcore.Schedule{{
			ID: "id-s1", Name: "30 Tage", IntervalUnit: admcore.IntervalUnitDay, IntervalMagnitude: 30,
		}}},
		&fakeQualificationPort{},
		&fakePerms{perms: []string{DashboardViewPermission}},
		nil,
		&fakeAudit{},
		nil,
	)
	return svc, store
}

// dashboardToolFixture builds an ACTIVE tool inheriting the type's default
// schedule (id-s1), so the dashboard read resolves a real interval.
func dashboardToolFixture(id, name string) *Tool {
	return &Tool{
		ID:                id,
		Name:              name,
		ToolTypeID:        "id-t1",
		ToolTypeName:      "Bohrmaschine",
		DefaultScheduleID: "id-s1",
		InventoryNumber:   "GEAR00000X",
	}
}

func TestListToolsForDashboardStatusMatrix(t *testing.T) {
	// Story 6.1 status matrix over the derived-status anchors (AD-4/AD-5):
	// never-inspected → red (nil next_due), OOS (latest fail not since
	// reinstated) → oos, recent pass → green + next_due, and the orange/red
	// windows relative to the 30-day schedule.
	now := time.Now()
	svc, store := dashboardService()
	store.tools = []*Tool{
		dashboardToolFixture("id-never", "Nie-geprüft"),
		dashboardToolFixture("id-oos", "Defekt"),
		dashboardToolFixture("id-green", "Frisch"),
		dashboardToolFixture("id-orange", "Bald-fällig"),
		dashboardToolFixture("id-red", "Überfällig"),
	}
	greenAnchor := now.Add(-10 * 24 * time.Hour)  // next_due 20d out → green
	orangeAnchor := now.Add(-26 * 24 * time.Hour) // next_due 4d out → orange (within interval/4)
	redAnchor := now.Add(-40 * 24 * time.Hour)    // next_due 10d past → red
	oosAnchor := now.Add(-1 * 24 * time.Hour)
	store.statusByTool = map[string]*ToolInspectionStatus{
		"id-oos":    {LatestFailAt: &oosAnchor},
		"id-green":  {LastSuccessAt: &greenAnchor},
		"id-orange": {LastSuccessAt: &orangeAnchor},
		"id-red":    {LastSuccessAt: &redAnchor},
	}

	got, err := svc.ListToolsForDashboard(context.Background())
	if err != nil {
		t.Fatalf("ListToolsForDashboard err = %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("tools = %d, want 5", len(got))
	}
	byID := map[string]*DashboardTool{}
	for _, d := range got {
		byID[d.ID] = d
	}

	if s := byID["id-never"].Status; s.Status != ToolStatusCodeRed || s.NextDue != nil {
		t.Errorf("never-inspected = %+v, want red + nil next_due", s)
	}
	if s := byID["id-oos"].Status; s.Status != ToolStatusCodeOOS || s.NextDue != nil {
		t.Errorf("OOS = %+v, want oos + nil next_due", s)
	}
	green := byID["id-green"].Status
	if green.Status != ToolStatusCodeGreen || green.NextDue == nil {
		t.Errorf("fresh = %+v, want green + a next_due", green)
	} else if want := greenAnchor.Add(30 * 24 * time.Hour); !green.NextDue.Equal(want) {
		t.Errorf("green next_due = %v, want %v (pass + 30-day default interval)", green.NextDue, want)
	}
	if s := byID["id-orange"].Status; s.Status != ToolStatusCodeOrange || s.NextDue == nil {
		t.Errorf("within quarter window = %+v, want orange + a next_due", s)
	}
	if s := byID["id-red"].Status; s.Status != ToolStatusCodeRed || s.NextDue == nil {
		t.Errorf("past due = %+v, want red + a next_due", s)
	}
}

func TestListToolsForDashboardOverrideBeatsDefault(t *testing.T) {
	// AD-5: a per-tool OVERRIDE (id-s2, 1 month) beats the type default
	// (id-s1, 30 days) on the dashboard read — the derived next_due uses the
	// override interval, never the default.
	svc, store := dashboardService()
	svc.schedules = &fakeSchedulesPort{schedules: []*admcore.Schedule{
		{ID: "id-s1", Name: "30 Tage", IntervalUnit: admcore.IntervalUnitDay, IntervalMagnitude: 30},
		{ID: "id-s2", Name: "1 Monat", IntervalUnit: admcore.IntervalUnitMonth, IntervalMagnitude: 1},
	}}
	store.tools = []*Tool{dashboardToolFixture("id-a", "Override")}
	store.tools[0].ScheduleID = "id-s2"
	now := time.Now()
	store.statusByTool = map[string]*ToolInspectionStatus{
		"id-a": {LastSuccessAt: &now},
	}

	got, err := svc.ListToolsForDashboard(context.Background())
	if err != nil {
		t.Fatalf("ListToolsForDashboard err = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("tools = %d, want 1", len(got))
	}
	if got[0].Status.Status != ToolStatusCodeGreen {
		t.Errorf("status = %q, want green (a fresh pass under the override interval)", got[0].Status.Status)
	}
	want := now.Add(30 * 24 * time.Hour) // the 1-month OVERRIDE interval
	if got[0].Status.NextDue == nil || !got[0].Status.NextDue.Equal(want) {
		t.Errorf("next_due = %v, want %v (the override interval, not the type default)", got[0].Status.NextDue, want)
	}
}

func TestListToolsForDashboardResilient(t *testing.T) {
	// DASH_RESILIENT (Story 6.1): a tool whose effective schedule is
	// missing/invalid, or whose status read errors, is rendered `red`
	// (NextDue nil) while the list still succeeds — one config defect never
	// takes the whole list down, and no tool is falsely claimed serviceable.
	svc, store := dashboardService()
	svc.schedules = &fakeSchedulesPort{schedules: []*admcore.Schedule{
		{ID: "id-s1", Name: "30 Tage", IntervalUnit: admcore.IntervalUnitDay, IntervalMagnitude: 30},
		{ID: "id-bad", Name: "Kaputt", IntervalUnit: "fortnight", IntervalMagnitude: 1},
	}}
	now := time.Now()
	store.tools = []*Tool{
		dashboardToolFixture("id-good", "Ok"),
		{ID: "id-no-schedule", Name: "Kein-Zeitplan", ToolTypeID: "id-t1", ToolTypeName: "Bohrmaschine", InventoryNumber: "GEAR000001"},
		{ID: "id-bad-schedule", Name: "Kaputter-Zeitplan", ToolTypeID: "id-t1", ToolTypeName: "Bohrmaschine", ScheduleID: "id-bad", InventoryNumber: "GEAR000002"},
		dashboardToolFixture("id-read-error", "Lesefehler"),
	}
	store.statusByTool = map[string]*ToolInspectionStatus{
		"id-good": {LastSuccessAt: &now},
	}
	store.statusErrByTool = map[string]error{
		"id-read-error": errors.New("boom"),
	}

	got, err := svc.ListToolsForDashboard(context.Background())
	if err != nil {
		t.Fatalf("ListToolsForDashboard err = %v, want the list to succeed despite per-tool defects", err)
	}
	if len(got) != 4 {
		t.Fatalf("tools = %d, want all 4 (one defect must not drop a row)", len(got))
	}
	byID := map[string]*DashboardTool{}
	for _, d := range got {
		byID[d.ID] = d
	}
	if s := byID["id-good"].Status; s.Status != ToolStatusCodeGreen || s.NextDue == nil {
		t.Errorf("good tool = %+v, want green + a next_due", s)
	}
	for _, id := range []string{"id-no-schedule", "id-bad-schedule", "id-read-error"} {
		s := byID[id].Status
		if s.Status != ToolStatusCodeRed || s.NextDue != nil {
			t.Errorf("%s = %+v, want red + nil next_due (never falsely serviceable)", id, s)
		}
	}
}

func TestListToolsForDashboardNilSchedulePortFailsLoudly(t *testing.T) {
	// A nil SchedulesPort is a composition-root wiring defect: with a NON-EMPTY
	// fleet the dashboard read FAILS LOUDLY (the status cannot be derived
	// without the clock) — never a silent all-red list. (An EMPTY fleet skips
	// the catalog entirely — pinned by TestListToolsForDashboardEmptyFleetSkipsCatalog.)
	svc, store := dashboardService()
	store.tools = []*Tool{dashboardToolFixture("id-a", "Bohrmaschine-01")}
	svc.schedules = nil
	if _, err := svc.ListToolsForDashboard(context.Background()); err == nil {
		t.Fatal("ListToolsForDashboard(nil port) err = nil, want internal error")
	}
}

func TestListToolsForDashboardCatalogErrorFailsLoudly(t *testing.T) {
	// A catalog resolution failure surfaces as an internal error (500-style),
	// never a silent all-red list.
	svc, store := dashboardService()
	store.tools = []*Tool{dashboardToolFixture("id-a", "Bohrmaschine-01")}
	svc.schedules = &fakeSchedulesPort{err: errors.New("boom")}
	if _, err := svc.ListToolsForDashboard(context.Background()); err == nil {
		t.Fatal("ListToolsForDashboard(catalog error) err = nil, want internal error")
	}
}

func TestListToolsForDashboardEmptyFleetSkipsCatalog(t *testing.T) {
	// Patch 5: an EMPTY fleet must return the empty list WITHOUT touching the
	// schedule catalog — even an unreachable/erroring catalog (or a nil port)
	// must not fail the dashboard when there is nothing to derive.
	svc, store := dashboardService()
	svc.schedules = &fakeSchedulesPort{err: errors.New("boom")}
	got, err := svc.ListToolsForDashboard(context.Background())
	if err != nil {
		t.Fatalf("ListToolsForDashboard(empty fleet, erroring catalog) err = %v, want the empty list", err)
	}
	if len(got) != 0 {
		t.Errorf("tools = %d, want 0", len(got))
	}

	// The nil-port case too: nothing to derive → empty list, no loud failure.
	svc.schedules = nil
	store.tools = nil
	got, err = svc.ListToolsForDashboard(context.Background())
	if err != nil {
		t.Fatalf("ListToolsForDashboard(empty fleet, nil port) err = %v, want the empty list", err)
	}
	if len(got) != 0 {
		t.Errorf("tools = %d, want 0 (nil port, empty fleet)", len(got))
	}
}

func TestListToolsForDashboardNilStatusRead(t *testing.T) {
	// Patch 13: a (nil, nil) status read is treated as NEVER-INSPECTED → red
	// (the nil anchors are never dereferenced — no panic, no false green).
	svc, store := dashboardService()
	store.tools = []*Tool{dashboardToolFixture("id-a", "Nie-gelesen")}
	store.statusNilResult = true

	got, err := svc.ListToolsForDashboard(context.Background())
	if err != nil {
		t.Fatalf("ListToolsForDashboard(nil status read) err = %v, want success", err)
	}
	if len(got) != 1 {
		t.Fatalf("tools = %d, want 1", len(got))
	}
	if s := got[0].Status; s.Status != ToolStatusCodeRed || s.NextDue != nil {
		t.Errorf("status = %+v, want red + nil next_due (nil read = never-inspected)", s)
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
		nil,
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

func TestCreateToolWithAttributes(t *testing.T) {
	// CREATE_TOOL_ATTRS (Story 4.4): a create with an attributes object stores
	// it in the owning row's `attributes` JSONB column (never a new column).
	svc, store, audit := newToolService()
	input := toolInput()
	input.Attributes = map[string]any{"standort": "Werkstatt", "leistung": float64(1200)}
	got, err := svc.CreateTool(context.Background(), actorID, input)
	if err != nil {
		t.Fatalf("CreateTool(attributes) err = %v", err)
	}
	if got.Attributes == nil || got.Attributes["standort"] != "Werkstatt" || got.Attributes["leistung"] != float64(1200) {
		t.Errorf("attributes = %+v, want the stored set", got.Attributes)
	}
	if len(store.created) != 1 || store.created[0].Attributes["standort"] != "Werkstatt" {
		t.Errorf("persisted = %+v, want the attributes stored", store.created)
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationToolCreate {
		t.Fatalf("audit events = %+v, want one create audit", audit.events)
	}
}

func TestCreateToolAbsentAttributesDefaultsEmpty(t *testing.T) {
	// CREATE with an ABSENT attributes field stores the DB default '{}' — the
	// create-path absent semantics (nothing exists yet to keep).
	svc, store, _ := newToolService()
	got, err := svc.CreateTool(context.Background(), actorID, toolInput())
	if err != nil {
		t.Fatalf("CreateTool err = %v", err)
	}
	if got.Attributes == nil || len(got.Attributes) != 0 {
		t.Errorf("attributes = %v, want empty map default", got.Attributes)
	}
	if len(store.created) != 1 || len(store.created[0].Attributes) != 0 {
		t.Errorf("persisted = %+v, want empty attributes", store.created)
	}
}

func TestCreateToolNormalizesAttributeKeys(t *testing.T) {
	// Keys are trimmed and validated on create too (a padded key becomes its
	// trimmed form in the persisted set).
	svc, store, _ := newToolService()
	input := toolInput()
	input.Attributes = map[string]any{" standort ": "Werkstatt"}
	got, err := svc.CreateTool(context.Background(), actorID, input)
	if err != nil {
		t.Fatalf("CreateTool(normalize) err = %v", err)
	}
	if got.Attributes["standort"] != "Werkstatt" {
		t.Errorf("attributes = %+v, want trimmed key standort=Werkstatt", got.Attributes)
	}
	if _, padded := store.created[0].Attributes[" standort "]; padded {
		t.Errorf("persisted must not keep the padded key, got %+v", store.created[0].Attributes)
	}
}

func TestUpdateToolReplacesAttributes(t *testing.T) {
	// UPDATE_TOOL_ATTRS (Story 4.4): a PUT with an attributes object REPLACES
	// the stored set wholesale.
	svc, store, audit := newToolService()
	store.tools = []*Tool{toolFixture("id-a", "Bohrmaschine-01")}
	store.tools[0].Attributes = map[string]any{"standort": "Alt"}

	input := toolInput()
	input.Attributes = map[string]any{"standort": "Neu", "leistung": float64(1200)}
	got, err := svc.UpdateTool(context.Background(), actorID, "id-a", input)
	if err != nil {
		t.Fatalf("UpdateTool(attributes) err = %v", err)
	}
	if got.Attributes == nil || got.Attributes["standort"] != "Neu" || got.Attributes["leistung"] != float64(1200) {
		t.Errorf("attributes = %+v, want the replaced set", got.Attributes)
	}
	if _, stale := got.Attributes["alt"]; stale {
		t.Errorf("attributes = %+v, want the old key gone (wholesale replacement)", got.Attributes)
	}
	if len(store.updated) != 1 || store.updated[0].Attributes["standort"] != "Neu" {
		t.Errorf("persisted = %+v, want the replaced set stored", store.updated)
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationToolUpdate {
		t.Fatalf("audit events = %+v, want one update audit", audit.events)
	}
}

func TestUpdateToolAbsentAttributesLeavesUnchanged(t *testing.T) {
	// UPDATE_TOOL_ABSENT (Story 4.4): a PUT WITHOUT the attributes field leaves
	// the stored JSONB unchanged — a name-only edit never wipes attributes.
	svc, store, audit := newToolService()
	store.tools = []*Tool{toolFixture("id-a", "Bohrmaschine-01")}
	store.tools[0].Attributes = map[string]any{"standort": "Werkstatt"}

	input := toolInput()
	input.Name = "Bohrmaschine-01-neu"
	input.Attributes = nil // absent
	got, err := svc.UpdateTool(context.Background(), actorID, "id-a", input)
	if err != nil {
		t.Fatalf("UpdateTool(absent attributes) err = %v", err)
	}
	if got.Attributes == nil || got.Attributes["standort"] != "Werkstatt" {
		t.Errorf("attributes = %+v, want the stored set preserved", got.Attributes)
	}
	if len(store.updated) != 1 || store.updated[0].Attributes["standort"] != "Werkstatt" {
		t.Errorf("persisted = %+v, want stored attributes kept (COALESCE)", store.updated)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %+v, want one update audit", audit.events)
	}
}

func TestUpdateToolClearsAttributes(t *testing.T) {
	// UPDATE_TOOL_CLEAR (Story 4.4): a PUT with an EXPLICIT `attributes: {}`
	// clears the stored set.
	svc, store, _ := newToolService()
	store.tools = []*Tool{toolFixture("id-a", "Bohrmaschine-01")}
	store.tools[0].Attributes = map[string]any{"standort": "Werkstatt"}

	input := toolInput()
	input.Attributes = map[string]any{}
	got, err := svc.UpdateTool(context.Background(), actorID, "id-a", input)
	if err != nil {
		t.Fatalf("UpdateTool(clear attributes) err = %v", err)
	}
	if got.Attributes == nil || len(got.Attributes) != 0 {
		t.Errorf("attributes = %v, want cleared empty map", got.Attributes)
	}
	if len(store.updated) != 1 || len(store.updated[0].Attributes) != 0 {
		t.Errorf("persisted = %+v, want attributes cleared", store.updated)
	}
}

func TestUpdateToolRoundTripsAttributes(t *testing.T) {
	// ROUND_TRIP (Story 4.4): set → read back unchanged (an object value set
	// via a create persists and comes back identical through the port).
	svc, store, _ := newToolService()
	input := toolInput()
	input.Attributes = map[string]any{"standort": "Werkstatt", "konfig": map[string]any{"modus": "auto"}}
	created, err := svc.CreateTool(context.Background(), actorID, input)
	if err != nil {
		t.Fatalf("CreateTool err = %v", err)
	}

	// A name-only update (absent attributes) must not touch the stored set.
	updateInput := toolInput()
	updateInput.Name = "Bohrmaschine-01-neu"
	updateInput.Attributes = nil
	updated, err := svc.UpdateTool(context.Background(), actorID, created.ID, updateInput)
	if err != nil {
		t.Fatalf("UpdateTool err = %v", err)
	}
	if updated.Attributes["standort"] != "Werkstatt" {
		t.Errorf("attributes after absent update = %+v, want standort preserved", updated.Attributes)
	}
	if len(store.updated[0].Attributes) != 2 {
		t.Errorf("persisted attributes = %+v, want both entries preserved", store.updated[0].Attributes)
	}
}

func TestCreateToolInvalidAttributes(t *testing.T) {
	// VALID_INVALID_KEY / VALID_TOO_LARGE / VALID_BAD_VALUE (Story 4.4): bad
	// attribute payloads are rejected with ErrInvalidAttributes (mapped to a
	// German 400 by the handler) and nothing is persisted.
	cases := []struct {
		name       string
		attrs      map[string]any
		wantReason string
	}{
		{"empty key", map[string]any{"": "wert"}, "empty key"},
		{"whitespace-only key", map[string]any{"   ": "wert"}, "empty key"},
		{"over-long key", map[string]any{strings.Repeat("k", MaxAttributeKeyRunes+1): "wert"}, "key too long"},
		{"trimmed duplicate key", map[string]any{"a": 1, " a ": 2}, "duplicate key"},
		{"too large", map[string]any{"note": strings.Repeat("x", MaxAttributesSize)}, "attributes too large"},
		{"bad value", map[string]any{"wert": math.NaN()}, "value not JSON-serializable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, store, _ := newToolService()
			input := toolInput()
			input.Attributes = tc.attrs
			_, err := svc.CreateTool(context.Background(), actorID, input)
			if !errors.Is(err, ErrInvalidAttributes) {
				t.Fatalf("err = %v, want ErrInvalidAttributes", err)
			}
			var attrErr *AttributeError
			if !errors.As(err, &attrErr) || attrErr.Reason != tc.wantReason {
				t.Fatalf("err = %v, want *AttributeError reason %q", err, tc.wantReason)
			}
			if len(store.created) != 0 {
				t.Error("tool must not be persisted with invalid attributes")
			}
		})
	}
}

func TestUpdateToolInvalidAttributes(t *testing.T) {
	// The attributes validation runs on UPDATE too — an invalid payload is
	// rejected, nothing persisted, not audited.
	svc, store, audit := newToolService()
	store.tools = []*Tool{toolFixture("id-a", "Bohrmaschine-01")}

	input := toolInput()
	input.Attributes = map[string]any{"": "wert"}
	_, err := svc.UpdateTool(context.Background(), actorID, "id-a", input)
	if !errors.Is(err, ErrInvalidAttributes) {
		t.Fatalf("err = %v, want ErrInvalidAttributes", err)
	}
	if len(store.updated) != 0 || len(audit.events) != 0 {
		t.Error("invalid-attributes update must not persist nor audit")
	}
}

func TestUpdateToolArchivedSentinelWinsOverInvalidAttributes(t *testing.T) {
	// The archived-sentinel-wins-over-400 contract extends to attributes: an
	// update against an already-archived (or unknown) id answers the 404
	// sentinel EVEN when the body carries invalid attributes (which would 400 a
	// live tool) — the existence check runs before the attribute validation.
	svc, store, _ := newToolService()
	archived := toolFixture("id-arch", "Alt")
	now := time.Now()
	archived.ArchivedAt = &now
	store.tools = []*Tool{archived}

	input := toolInput()
	input.Attributes = map[string]any{"": "wert"}
	if _, err := svc.UpdateTool(context.Background(), actorID, "id-arch", input); !errors.Is(err, ErrToolNotFound) {
		var attrErr *AttributeError
		if errors.As(err, &attrErr) {
			t.Fatalf("err = %v (AttributeError %q), want ErrToolNotFound (404 sentinel wins over 400)", err, attrErr.Reason)
		}
		t.Fatalf("err = %v, want ErrToolNotFound", err)
	}
	if _, err := svc.UpdateTool(context.Background(), actorID, "id-missing", input); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("unknown id with invalid attributes: err = %v, want ErrToolNotFound", err)
	}
}

// ============================================================================
// Story 6.2 — status report export (FR-17/AD-6/AD-5): the report re-derives
// every status server-side (never trusts the client), filters to the active
// codes and resolves ALL inspector names in ONE bulk call.
// ============================================================================

// reportService wires a Service over ONE ACTIVE schedule (id-s1, 30 days) and a
// tool type whose default schedule is id-s1 — so report tools derive real
// statuses. perms defaults to the report.export holder; display names resolve
// through a fake resolver (absent ids → "Deleted User").
func reportService(perms ...string) (*Service, *fakeToolStore, *fakeDisplayNames) {
	store := &fakeToolStore{
		types: []*ToolType{{
			ID: "id-t1", Name: "Bohrmaschine", DefaultScheduleID: "id-s1",
			InspectionMode: InspectionModePassFail,
		}},
	}
	if len(perms) == 0 {
		perms = []string{ReportExportPermission}
	}
	names := &fakeDisplayNames{names: map[string]string{"u-anna": "Anna Muster"}}
	svc := NewService(
		store,
		&fakeSchedulesPort{schedules: []*admcore.Schedule{{
			ID: "id-s1", Name: "30 Tage", IntervalUnit: admcore.IntervalUnitDay, IntervalMagnitude: 30,
		}}},
		&fakeQualificationPort{},
		&fakePerms{perms: perms},
		names,
		&fakeAudit{},
		nil,
	)
	return svc, store, names
}

// reportToolFixture builds an ACTIVE tool inheriting the type's default
// schedule (id-s1), so the report derives a real status.
func reportToolFixture(id, name string) *Tool {
	return &Tool{
		ID:                id,
		Name:              name,
		ToolTypeID:        "id-t1",
		ToolTypeName:      "Bohrmaschine",
		DefaultScheduleID: "id-s1",
		InventoryNumber:   "GEAR00000X",
	}
}

func TestExportStatusReportGated(t *testing.T) {
	// REPORT_GATED: a caller lacking report.export answers ErrForbidden with no
	// report data (AD-6 — the server is the gate, never the SPA).
	svc, store, _ := reportService("dashboard.view")
	store.tools = []*Tool{reportToolFixture("id-a", "Bohrmaschine-01")}
	if _, err := svc.ExportStatusReport(context.Background(), actorID, nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestExportStatusReportEmptyFleet(t *testing.T) {
	// REPORT_OK (empty fleet): no tools → an empty list WITHOUT touching the
	// schedule catalog or the name resolver (mirroring the dashboard).
	svc, _, names := reportService()
	rows, err := svc.ExportStatusReport(context.Background(), actorID, nil)
	if err != nil {
		t.Fatalf("ExportStatusReport(empty) err = %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("rows = %d, want 0", len(rows))
	}
	if names.calls != 0 {
		t.Errorf("ResolveDisplayNames calls = %d, want 0 for an empty fleet", names.calls)
	}
}

func TestExportStatusReportAllAndFilter(t *testing.T) {
	now := time.Now()
	greenAnchor := now.Add(-10 * 24 * time.Hour)
	oosAnchor := now.Add(-1 * 24 * time.Hour)
	svc, store, _ := reportService()
	store.tools = []*Tool{
		reportToolFixture("id-green", "Grün"),
		reportToolFixture("id-oos", "Oos"),
	}
	store.statusByTool = map[string]*ToolInspectionStatus{
		"id-green": {LastSuccessAt: &greenAnchor},
		"id-oos":   {LatestFailAt: &oosAnchor},
	}
	inspected := now.Add(-5 * 24 * time.Hour)
	store.latestByTool = map[string]*LatestInspection{
		"id-green": {SubmittedAt: inspected, InspectorID: "u-anna"},
		"id-oos":   {SubmittedAt: inspected, InspectorID: "u-anna"},
	}

	// REPORT_OK: no filter → ALL tools, list order preserved, each with the
	// server-DERIVED status + the latest-inspection inputs.
	rows, err := svc.ExportStatusReport(context.Background(), actorID, nil)
	if err != nil {
		t.Fatalf("ExportStatusReport(no filter) err = %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].Tool.ID != "id-green" || rows[1].Tool.ID != "id-oos" {
		t.Errorf("row order = %+v, want the list order preserved", rows)
	}
	if rows[0].Tool.Status.Status != ToolStatusCodeGreen || rows[1].Tool.Status.Status != ToolStatusCodeOOS {
		t.Errorf("derived statuses = %q/%q, want green/oos", rows[0].Tool.Status.Status, rows[1].Tool.Status.Status)
	}
	if rows[0].LastInspectedAt == nil || !rows[0].LastInspectedAt.Equal(inspected) {
		t.Errorf("rows[0].LastInspectedAt = %v, want %v", rows[0].LastInspectedAt, inspected)
	}
	if rows[0].LastInspectorName != "Anna Muster" {
		t.Errorf("rows[0].LastInspectorName = %q, want the resolved display name", rows[0].LastInspectorName)
	}

	// REPORT_FILTERED: ?status=green → only the matching tool (server-derived).
	rows, err = svc.ExportStatusReport(context.Background(), actorID, []string{"green"})
	if err != nil {
		t.Fatalf("ExportStatusReport(green) err = %v", err)
	}
	if len(rows) != 1 || rows[0].Tool.ID != "id-green" {
		t.Fatalf("green filter rows = %+v, want only id-green", rows)
	}

	// ?status=green,oos → both tools (the union of the codes).
	rows, err = svc.ExportStatusReport(context.Background(), actorID, []string{"green", "oos"})
	if err != nil {
		t.Fatalf("ExportStatusReport(green,oos) err = %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("green,oos filter rows = %d, want 2", len(rows))
	}

	// REPORT_UNKNOWN_CODE: an unknown filter code matches nothing server-side
	// (the HTTP layer rejects it with a 400 BEFORE the core; here the core just
	// returns no matching rows — never an error, never a leak of other tools).
	rows, err = svc.ExportStatusReport(context.Background(), actorID, []string{"neon"})
	if err != nil {
		t.Fatalf("ExportStatusReport(unknown) err = %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("unknown filter rows = %d, want 0", len(rows))
	}
}

func TestExportStatusReportNeverInspectedAndDeletedUser(t *testing.T) {
	now := time.Now()
	svc, store, names := reportService()
	store.tools = []*Tool{
		reportToolFixture("id-never", "Nie-geprüft"),
		reportToolFixture("id-deleted", "Gelöscht"),
	}
	greenAnchor := now.Add(-10 * 24 * time.Hour)
	store.statusByTool = map[string]*ToolInspectionStatus{
		"id-never":   {},
		"id-deleted": {LastSuccessAt: &greenAnchor},
	}
	inspected := now.Add(-5 * 24 * time.Hour)
	store.latestByTool = map[string]*LatestInspection{
		"id-deleted": {SubmittedAt: inspected, InspectorID: "u-gone"},
	}

	rows, err := svc.ExportStatusReport(context.Background(), actorID, nil)
	if err != nil {
		t.Fatalf("ExportStatusReport err = %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}

	// REPORT_NEVER: a never-inspected tool → LastInspectedAt nil (the PDF
	// renders "–" in the Zuletzt geprüft AND Prüfer/in columns) and its derived
	// status reads red (AD-5).
	if rows[0].LastInspectedAt != nil {
		t.Errorf("rows[0].LastInspectedAt = %v, want nil (never inspected)", rows[0].LastInspectedAt)
	}
	if rows[0].Tool.Status.Status != ToolStatusCodeRed {
		t.Errorf("rows[0] status = %q, want red (never-inspected derives red)", rows[0].Tool.Status.Status)
	}

	// REPORT_DELETED_USER: an inspector id with no user row → the literal
	// "Deleted User".
	if rows[1].LastInspectorName != DeletedUserDisplayName {
		t.Errorf("rows[1].LastInspectorName = %q, want %q", rows[1].LastInspectorName, DeletedUserDisplayName)
	}
	if rows[1].LastInspectedAt == nil || !rows[1].LastInspectedAt.Equal(inspected) {
		t.Errorf("rows[1].LastInspectedAt = %v, want %v", rows[1].LastInspectedAt, inspected)
	}

	// The spec's one-call invariant: BOTH inspector ids resolve in ONE
	// ResolveDisplayNames call (no N+1 user reads).
	if names.calls != 1 {
		t.Errorf("ResolveDisplayNames calls = %d, want 1", names.calls)
	}
}
