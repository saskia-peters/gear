package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	admcore "github.com/saskia-peters/gear/internal/admin/core"
)

const actorID = "u-admin"

// fakePerms is a PermissionResolver with a fixed set.
type fakePerms struct {
	perms []string
}

func (f *fakePerms) ListPermissionsByUser(context.Context, string) ([]string, error) {
	return f.perms, nil
}

// fakeAudit records InsertAuditEvent calls.
type fakeAudit struct {
	events []auditEvent
}

type auditEvent struct {
	actorID   string
	operation string
	detail    string
	severity  string
}

func (f *fakeAudit) InsertAuditEvent(_ context.Context, userID, operation, detail, severity string) error {
	f.events = append(f.events, auditEvent{actorID: userID, operation: operation, detail: detail, severity: severity})
	return nil
}

// fakeToolTypeStore is an in-memory ToolTypeStore emulating the repository's
// soft-archive + item-replacement semantics: ListToolTypes returns only ACTIVE
// types, GetToolType returns the stored row (active or archived), and
// Update/Archive refuse a missing OR already-archived row with
// ErrToolTypeNotFound (the archived row is non-existent to the surface).
type fakeToolTypeStore struct {
	types      []*ToolType
	listErr    error
	getErr     error
	createErr  error
	updateErr  error
	archiveErr error
	created    []*ToolType
	updated    []*ToolType
	archivedIDs []string
}

func (f *fakeToolTypeStore) ListToolTypes(context.Context) ([]*ToolType, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []*ToolType
	for _, tt := range f.types {
		if tt.ArchivedAt == nil {
			out = append(out, tt)
		}
	}
	return out, nil
}

func (f *fakeToolTypeStore) ToolTypeExistsActive(_ context.Context, id string) (bool, error) {
	if f.getErr != nil {
		return false, f.getErr
	}
	for _, tt := range f.types {
		if tt.ID == id {
			return tt.ArchivedAt == nil, nil
		}
	}
	return false, nil
}

func (f *fakeToolTypeStore) CreateToolType(_ context.Context, tt *ToolType) (*ToolType, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	persisted := *tt
	persisted.ID = "id-" + tt.Name
	f.types = append(f.types, &persisted)
	f.created = append(f.created, &persisted)
	return &persisted, nil
}

func (f *fakeToolTypeStore) UpdateToolType(_ context.Context, tt *ToolType) (*ToolType, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	for i, existing := range f.types {
		if existing.ID == tt.ID {
			if existing.ArchivedAt != nil {
				return nil, ErrToolTypeNotFound
			}
			persisted := *tt
			f.types[i] = &persisted
			f.updated = append(f.updated, &persisted)
			return &persisted, nil
		}
	}
	return nil, ErrToolTypeNotFound
}

func (f *fakeToolTypeStore) ArchiveToolType(_ context.Context, id string) (*ToolType, error) {
	if f.archiveErr != nil {
		return nil, f.archiveErr
	}
	for i, tt := range f.types {
		if tt.ID == id {
			if tt.ArchivedAt != nil {
				return nil, ErrToolTypeNotFound
			}
			now := time.Now()
			archived := *tt
			archived.ArchivedAt = &now
			f.types[i] = &archived
			f.archivedIDs = append(f.archivedIDs, id)
			return &archived, nil
		}
	}
	return nil, ErrToolTypeNotFound
}

// The combined store dependency (Story 4.3) requires the ToolStore surface
// too. The tool-type fakes never exercise the tool surface — these stubs keep
// the shared constructor compilable without touching tool-type tests.
func (f *fakeToolTypeStore) ListTools(context.Context) ([]*Tool, error) { return []*Tool{}, nil }
func (f *fakeToolTypeStore) ToolExistsActive(context.Context, string) (bool, error) { return false, nil }
func (f *fakeToolTypeStore) CreateTool(_ context.Context, _ *Tool) (*Tool, error) { return nil, ErrToolNotFound }
func (f *fakeToolTypeStore) UpdateTool(_ context.Context, _ *Tool) (*Tool, error) { return nil, ErrToolNotFound }
func (f *fakeToolTypeStore) ArchiveTool(_ context.Context, _ string) (*Tool, error) { return nil, ErrToolNotFound }

// fakeSchedulesPort is an adminports.SchedulesPort over a fixed ACTIVE catalog.
type fakeSchedulesPort struct {
	schedules []*admcore.Schedule
}

func (f *fakeSchedulesPort) CurrentSchedules(context.Context) ([]*admcore.Schedule, error) {
	return f.schedules, nil
}

// fakeQualificationPort is a userports.QualificationCatalogPort over a fixed
// vocabulary.
type fakeQualificationPort struct {
	qualificationIDs []string
}

func (f *fakeQualificationPort) QualificationExists(_ context.Context, id string) (bool, error) {
	for _, q := range f.qualificationIDs {
		if q == id {
			return true, nil
		}
	}
	return false, nil
}

// newToolTypeService wires the fakes around a Service. perms defaults to the
// tool_types.manage holder; the schedule/qualification ports default to one
// ACTIVE schedule (id-s1) and one qualification (id-q1).
func newToolTypeService(perms ...string) (*Service, *fakeToolTypeStore, *fakeAudit) {
	store := &fakeToolTypeStore{}
	audit := &fakeAudit{}
	if len(perms) == 0 {
		perms = []string{ToolTypesManagePermission}
	}
	svc := NewService(
		store,
		&fakeSchedulesPort{schedules: []*admcore.Schedule{{ID: "id-s1", Name: "1 Jahr"}}},
		&fakeQualificationPort{qualificationIDs: []string{"id-q1"}},
		&fakePerms{perms: perms},
		audit,
		nil,
	)
	return svc, store, audit
}

func toolTypeInput() ToolTypeInput {
	return ToolTypeInput{
		Name:                    "Bohrmaschine",
		DefaultScheduleID:       "id-s1",
		RequiredQualificationID: "id-q1",
		InspectionMode:          InspectionModeChecklist,
		Items: []ToolTypeChecklistItemInput{
			{Label: "Bohrfutter sitzt fest"},
			{Label: "Kabel intakt"},
		},
	}
}

func TestListToolTypesEmpty(t *testing.T) {
	// GET_LIST_EMPTY: no tool types → empty list, no error.
	svc, _, _ := newToolTypeService()
	got, err := svc.ListToolTypes(context.Background(), actorID)
	if err != nil {
		t.Fatalf("ListToolTypes err = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("tool types = %d, want 0", len(got))
	}
}

func TestListToolTypes(t *testing.T) {
	// GET_LIST: the ACTIVE catalog is returned, oldest first, each with its
	// ordered checklist items; archived rows are filtered out by the store.
	svc, store, _ := newToolTypeService()
	store.types = []*ToolType{
		{ID: "id-a", Name: "Bohrmaschine", Items: []ToolTypeChecklistItem{{Position: 0, Label: "Kabel"}}},
		{ID: "id-b", Name: "Schleifmaschine"},
	}
	archived := &ToolType{ID: "id-arch", Name: "Alt"}
	now := time.Now()
	archived.ArchivedAt = &now
	store.types = append(store.types, archived)

	got, err := svc.ListToolTypes(context.Background(), actorID)
	if err != nil {
		t.Fatalf("ListToolTypes err = %v", err)
	}
	if len(got) != 2 || got[0].ID != "id-a" || got[1].ID != "id-b" {
		t.Fatalf("tool types = %+v, want both active rows in order", got)
	}
	if len(got[0].Items) != 1 || got[0].Items[0].Label != "Kabel" {
		t.Errorf("items = %+v, want ordered checklist items", got[0].Items)
	}
}

func TestCreateToolTypeValid(t *testing.T) {
	// CREATE_VALID: name trimmed, mode + items persisted, audited
	// (tool_type.create). Checklist positions are assigned from array order.
	svc, store, audit := newToolTypeService()
	got, err := svc.CreateToolType(context.Background(), actorID, toolTypeInput())
	if err != nil {
		t.Fatalf("CreateToolType err = %v", err)
	}
	if got.Name != "Bohrmaschine" {
		t.Errorf("name = %q, want trimmed", got.Name)
	}
	if got.DefaultScheduleID != "id-s1" || got.RequiredQualificationID != "id-q1" {
		t.Errorf("FKs = %+v", got)
	}
	if got.InspectionMode != InspectionModeChecklist {
		t.Errorf("mode = %q, want checklist", got.InspectionMode)
	}
	if len(got.Items) != 2 || got.Items[0].Position != 0 || got.Items[1].Position != 1 {
		t.Fatalf("items = %+v, want two items with positions 0,1", got.Items)
	}
	if got.ArchivedAt != nil {
		t.Error("new tool type must be active (ArchivedAt nil)")
	}
	if len(store.created) != 1 {
		t.Fatalf("created = %d, want 1", len(store.created))
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationToolTypeCreate {
		t.Fatalf("audit events = %+v, want one create audit", audit.events)
	}
	if !strings.Contains(audit.events[0].detail, "id="+got.ID) {
		t.Errorf("create audit detail = %q, want it to carry the created id %q", audit.events[0].detail, got.ID)
	}
	if audit.events[0].actorID != actorID || audit.events[0].severity != AuditSeverityNormal {
		t.Errorf("audit actor/severity = %+v", audit.events[0])
	}
}

func TestCreateToolTypeWithoutQualification(t *testing.T) {
	// 000023 made the required qualification OPTIONAL: most tools need no
	// specific qualification (any Helfer*in may inspect them, FR-8/FR-11), so
	// an empty required_qualification_id is valid and persists as a nil FK.
	svc, store, _ := newToolTypeService()
	input := toolTypeInput()
	input.RequiredQualificationID = ""
	got, err := svc.CreateToolType(context.Background(), actorID, input)
	if err != nil {
		t.Fatalf("CreateToolType(no qualification) err = %v", err)
	}
	if got.RequiredQualificationID != "" {
		t.Errorf("required qualification = %q, want empty", got.RequiredQualificationID)
	}
	if len(store.created) != 1 || store.created[0].RequiredQualificationID != "" {
		t.Errorf("persisted = %+v, want no required qualification", store.created)
	}
}

func TestCreateToolTypePassFailIgnoresItems(t *testing.T) {
	// A pass_fail type persists with an EMPTY checklist (the item editor is
	// only shown in checklist mode; any submitted items are ignored).
	svc, store, _ := newToolTypeService()
	input := toolTypeInput()
	input.InspectionMode = InspectionModePassFail
	got, err := svc.CreateToolType(context.Background(), actorID, input)
	if err != nil {
		t.Fatalf("CreateToolType(pass_fail) err = %v", err)
	}
	if got.InspectionMode != InspectionModePassFail {
		t.Errorf("mode = %q", got.InspectionMode)
	}
	if len(got.Items) != 0 {
		t.Errorf("items = %+v, want empty for pass_fail", got.Items)
	}
	if len(store.created) != 1 || len(store.created[0].Items) != 0 {
		t.Errorf("persisted = %+v, want no items for pass_fail", store.created)
	}
}

func TestCreateToolTypeInvalid(t *testing.T) {
	// CREATE_INVALID: empty name / bad mode / missing FK / bad checklist item →
	// 400-class German error, nothing persisted.
	cases := []struct {
		name    string
		mutate  func(*ToolTypeInput)
		wantMsg string
	}{
		{"empty name", func(in *ToolTypeInput) { in.Name = "  " }, "Namen"},
		{"name too long", func(in *ToolTypeInput) { in.Name = strings.Repeat("ä", 256) }, "zu lang"},
		{"bad mode", func(in *ToolTypeInput) { in.InspectionMode = "matrix" }, "Prüfmodus"},
		{"missing schedule", func(in *ToolTypeInput) { in.DefaultScheduleID = "" }, "Zeitplan"},
		{"empty item label", func(in *ToolTypeInput) { in.Items = append(in.Items, ToolTypeChecklistItemInput{Label: "  "}) }, "Eintrag"},
		{"item label too long", func(in *ToolTypeInput) { in.Items[0].Label = strings.Repeat("x", 256) }, "zu lang"},
		// Fix 3: a checklist mode with ZERO items has no inspection template.
		{"checklist without items", func(in *ToolTypeInput) { in.Items = nil }, MsgToolTypeChecklistRequired},
		// Fix 8: duplicate (case-insensitive) checklist labels are ambiguous.
		{"duplicate item label", func(in *ToolTypeInput) { in.Items[1].Label = strings.ToUpper(in.Items[0].Label) }, MsgToolTypeChecklistDuplicate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, store, _ := newToolTypeService()
			input := toolTypeInput()
			tc.mutate(&input)
			_, err := svc.CreateToolType(context.Background(), actorID, input)
			var inv *InvalidToolTypeError
			if !errors.As(err, &inv) {
				t.Fatalf("err = %v, want *InvalidToolTypeError", err)
			}
			if !strings.Contains(inv.Message, tc.wantMsg) {
				t.Errorf("message = %q, want contains %q", inv.Message, tc.wantMsg)
			}
			if !errors.Is(err, ErrToolTypeInvalid) {
				t.Errorf("err = %v, want unwraps to ErrToolTypeInvalid", err)
			}
			if len(store.created) != 0 {
				t.Error("tool type must not be persisted on invalid input")
			}
		})
	}
}

func TestCreateToolTypeBadSchedule(t *testing.T) {
	// CREATE_BAD_SCHEDULE: the default schedule id is not an ACTIVE schedule →
	// 400 German (port lookup), nothing persisted.
	svc, store, _ := newToolTypeService()
	input := toolTypeInput()
	input.DefaultScheduleID = "id-missing-schedule"
	_, err := svc.CreateToolType(context.Background(), actorID, input)
	var inv *InvalidToolTypeError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *InvalidToolTypeError", err)
	}
	if !strings.Contains(inv.Message, MsgToolTypeInvalidSchedule) {
		t.Errorf("message = %q, want %q", inv.Message, MsgToolTypeInvalidSchedule)
	}
	if len(store.created) != 0 {
		t.Error("tool type must not be persisted with a bad schedule FK")
	}
}

func TestUpdateToolTypeBadSchedule(t *testing.T) {
	// The FK port checks run on UPDATE too (not just create): a schedule that
	// left the ACTIVE catalog must reject the update.
	svc, store, audit := newToolTypeService()
	store.types = []*ToolType{toolTypeFixture("id-a")}
	input := toolTypeInput()
	input.DefaultScheduleID = "id-s1"
	// Drop id-s1 from the active catalog → it is no longer an ACTIVE schedule.
	svc.schedules = &fakeSchedulesPort{schedules: []*admcore.Schedule{{ID: "id-s2", Name: "2 Wochen"}}}
	_, err := svc.UpdateToolType(context.Background(), actorID, "id-a", input)
	if err == nil {
		t.Fatal("UpdateToolType with deactivated schedule err = nil, want 400 German")
	}
	var inv *InvalidToolTypeError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *InvalidToolTypeError", err)
	}
	if !strings.Contains(inv.Message, MsgToolTypeInvalidSchedule) {
		t.Errorf("message = %q, want %q", inv.Message, MsgToolTypeInvalidSchedule)
	}
	if len(store.updated) != 0 || len(audit.events) != 0 {
		t.Error("update with bad schedule must not persist nor audit")
	}
}

func TestCreateToolTypeBadQualification(t *testing.T) {
	// CREATE_BAD_QUALIFICATION: the required qualification id is unknown →
	// 400 German (port lookup), nothing persisted.
	svc, store, _ := newToolTypeService()
	input := toolTypeInput()
	input.RequiredQualificationID = "id-missing-qual"
	_, err := svc.CreateToolType(context.Background(), actorID, input)
	var inv *InvalidToolTypeError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *InvalidToolTypeError", err)
	}
	if !strings.Contains(inv.Message, MsgToolTypeInvalidQualification) {
		t.Errorf("message = %q, want %q", inv.Message, MsgToolTypeInvalidQualification)
	}
	if len(store.created) != 0 {
		t.Error("tool type must not be persisted with a bad qualification FK")
	}
}

func TestCreateToolTypeDuplicateName(t *testing.T) {
	// CREATE_DUPLICATE: a create whose name another tool type already holds
	// (case-insensitive) is rejected, not persisted.
	svc, store, _ := newToolTypeService()
	store.types = []*ToolType{{ID: "id-a", Name: "Bohrmaschine"}}

	input := toolTypeInput()
	input.Name = "bohrmaschine"
	_, err := svc.CreateToolType(context.Background(), actorID, input)
	var inv *InvalidToolTypeError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *InvalidToolTypeError", err)
	}
	if !strings.Contains(inv.Message, MsgToolTypeNameTaken) {
		t.Errorf("message = %q, want %q", inv.Message, MsgToolTypeNameTaken)
	}
	if len(store.created) != 0 {
		t.Error("duplicate-name tool type must not be persisted")
	}
}

func TestUpdateToolTypeReplaceItems(t *testing.T) {
	// UPDATE_REPLACE_ITEMS: name/mode + a new ordered item list persisted
	// (full replacement), audited (tool_type.update).
	svc, store, audit := newToolTypeService()
	store.types = []*ToolType{toolTypeFixture("id-a")}

	input := toolTypeInput()
	input.Name = "Schlagbohrmaschine"
	input.Items = []ToolTypeChecklistItemInput{{Label: "Neu"}, {Label: "Neuer"}}
	got, err := svc.UpdateToolType(context.Background(), actorID, "id-a", input)
	if err != nil {
		t.Fatalf("UpdateToolType err = %v", err)
	}
	if got.Name != "Schlagbohrmaschine" {
		t.Errorf("name = %q, want updated", got.Name)
	}
	if len(got.Items) != 2 || got.Items[0].Label != "Neu" || got.Items[1].Label != "Neuer" {
		t.Fatalf("items = %+v, want the replaced ordered list", got.Items)
	}
	if len(store.updated) != 1 {
		t.Fatalf("updated = %d, want 1", len(store.updated))
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationToolTypeUpdate {
		t.Fatalf("audit events = %+v, want one update audit", audit.events)
	}
}

func TestUpdateToolTypeNotFound(t *testing.T) {
	svc, _, _ := newToolTypeService()
	if _, err := svc.UpdateToolType(context.Background(), actorID, "id-missing", toolTypeInput()); !errors.Is(err, ErrToolTypeNotFound) {
		t.Fatalf("err = %v, want ErrToolTypeNotFound", err)
	}
}

func TestUpdateToolTypeArchived(t *testing.T) {
	// UPDATE_ARCHIVED: updating an already-archived type answers the 404
	// sentinel — the archived row is non-existent to the surface.
	svc, store, audit := newToolTypeService()
	archived := toolTypeFixture("id-arch")
	now := time.Now()
	archived.ArchivedAt = &now
	store.types = []*ToolType{archived}

	if _, err := svc.UpdateToolType(context.Background(), actorID, "id-arch", toolTypeInput()); !errors.Is(err, ErrToolTypeNotFound) {
		t.Fatalf("err = %v, want ErrToolTypeNotFound", err)
	}
	if len(store.updated) != 0 {
		t.Error("archived tool type must not be updated")
	}
	if len(audit.events) != 0 {
		t.Errorf("audit events = %+v, want none for a rejected update", audit.events)
	}
}

func TestArchiveToolType(t *testing.T) {
	// ARCHIVE: archived_at set, the row leaves the active list, audited
	// (tool_type.archive).
	svc, store, audit := newToolTypeService()
	store.types = []*ToolType{toolTypeFixture("id-a")}

	got, err := svc.ArchiveToolType(context.Background(), actorID, "id-a")
	if err != nil {
		t.Fatalf("ArchiveToolType err = %v", err)
	}
	if got.ArchivedAt == nil {
		t.Error("archived_at = nil, want set")
	}
	if len(store.archivedIDs) != 1 || store.archivedIDs[0] != "id-a" {
		t.Errorf("archived = %v, want [id-a]", store.archivedIDs)
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationToolTypeArchive {
		t.Fatalf("audit events = %+v, want one archive audit", audit.events)
	}

	active, err := svc.ListToolTypes(context.Background(), actorID)
	if err != nil {
		t.Fatalf("ListToolTypes err = %v", err)
	}
	if len(active) != 0 {
		t.Errorf("active = %d, want 0 after archive", len(active))
	}
}

func TestArchiveToolTypeArchived(t *testing.T) {
	// ARCHIVE_ARCHIVED: archiving an already-archived row answers the 404
	// sentinel.
	svc, store, _ := newToolTypeService()
	archived := toolTypeFixture("id-arch")
	now := time.Now()
	archived.ArchivedAt = &now
	store.types = []*ToolType{archived}

	if _, err := svc.ArchiveToolType(context.Background(), actorID, "id-arch"); !errors.Is(err, ErrToolTypeNotFound) {
		t.Fatalf("err = %v, want ErrToolTypeNotFound", err)
	}
	if len(store.archivedIDs) != 0 {
		t.Error("already-archived tool type must not be re-archived")
	}
}

func TestArchiveToolTypeNotFound(t *testing.T) {
	svc, _, _ := newToolTypeService()
	if _, err := svc.ArchiveToolType(context.Background(), actorID, "id-missing"); !errors.Is(err, ErrToolTypeNotFound) {
		t.Fatalf("err = %v, want ErrToolTypeNotFound", err)
	}
}

func TestToolTypesForbidden(t *testing.T) {
	// FORBIDDEN: every tool-type method re-checks the live permission.
	svc, store, _ := newToolTypeService("dashboard.view")
	store.types = []*ToolType{toolTypeFixture("id-a")}
	if _, err := svc.ListToolTypes(context.Background(), actorID); !errors.Is(err, ErrForbidden) {
		t.Errorf("list err = %v, want ErrForbidden", err)
	}
	if _, err := svc.CreateToolType(context.Background(), actorID, toolTypeInput()); !errors.Is(err, ErrForbidden) {
		t.Errorf("create err = %v, want ErrForbidden", err)
	}
	if _, err := svc.UpdateToolType(context.Background(), actorID, "id-a", toolTypeInput()); !errors.Is(err, ErrForbidden) {
		t.Errorf("update err = %v, want ErrForbidden", err)
	}
	if _, err := svc.ArchiveToolType(context.Background(), actorID, "id-a"); !errors.Is(err, ErrForbidden) {
		t.Errorf("archive err = %v, want ErrForbidden", err)
	}
}

func TestToolTypesEmptyActorNeverPasses(t *testing.T) {
	// Defense-in-depth: an empty actor ID never passes even when the resolver
	// would grant the permission.
	svc, _, _ := newToolTypeService()
	svc.perms = &fakePerms{perms: []string{ToolTypesManagePermission}}
	if _, err := svc.ListToolTypes(context.Background(), ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func toolTypeFixture(id string) *ToolType {
	return &ToolType{
		ID:                      id,
		Name:                    "Bohrmaschine",
		DefaultScheduleID:       "id-s1",
		RequiredQualificationID: "id-q1",
		InspectionMode:          InspectionModeChecklist,
		Items:                   []ToolTypeChecklistItem{{ID: "item-1", Position: 0, Label: "Kabel"}},
	}
}

func TestToolTypesNilPortsFailLoudly(t *testing.T) {
	// Fix 4: a nil SchedulesPort / QualificationCatalogPort is a composition-root
	// wiring defect. The write path must FAIL LOUDLY (a 500-style internal
	// error, never ErrForbidden / ErrToolTypeInvalid) — never silently skip the
	// AD-7/AD-16 FK validation.
	store := &fakeToolTypeStore{}
	svc := NewService(
		store,
		nil,
		nil,
		&fakePerms{perms: []string{ToolTypesManagePermission}},
		&fakeAudit{},
		nil,
	)

	if _, err := svc.CreateToolType(context.Background(), actorID, toolTypeInput()); err == nil {
		t.Fatal("CreateToolType with nil ports err = nil, want internal error")
	} else {
		var inv *InvalidToolTypeError
		if errors.As(err, &inv) {
			t.Fatalf("CreateToolType err = %v, want a 500-style internal error, not a validation sentinel", err)
		}
		if errors.Is(err, ErrForbidden) {
			t.Fatalf("CreateToolType err = %v, want internal error, not ErrForbidden", err)
		}
	}

	store.types = []*ToolType{toolTypeFixture("id-a")}
	if _, err := svc.UpdateToolType(context.Background(), actorID, "id-a", toolTypeInput()); err == nil {
		t.Fatal("UpdateToolType with nil ports err = nil, want internal error")
	} else {
		var inv *InvalidToolTypeError
		if errors.As(err, &inv) {
			t.Fatalf("UpdateToolType err = %v, want a 500-style internal error, not a validation sentinel", err)
		}
	}
	if len(store.created) != 0 || len(store.updated) != 0 {
		t.Error("nil-port write must not persist")
	}
}