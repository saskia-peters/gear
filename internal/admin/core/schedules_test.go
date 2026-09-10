package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeScheduleStore is an in-memory SchedulesStore emulating the repository's
// soft-archive semantics: ListSchedules returns only ACTIVE rows, and
// Update/Archive refuse a missing OR already-archived row with
// ErrScheduleNotFound (the archived row is non-existent to the surface).
type fakeScheduleStore struct {
	schedules   []*Schedule
	listErr     error
	createErr   error
	updateErr   error
	archiveErr  error
	created     []*Schedule
	updated     []*Schedule
	archivedIDs []string
}

func (f *fakeScheduleStore) ListSchedules(context.Context) ([]*Schedule, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []*Schedule
	for _, s := range f.schedules {
		if s.ArchivedAt == nil {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeScheduleStore) CreateSchedule(_ context.Context, schedule *Schedule) (*Schedule, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	persisted := *schedule
	persisted.ID = "id-" + schedule.Name
	f.schedules = append(f.schedules, &persisted)
	f.created = append(f.created, &persisted)
	return &persisted, nil
}

func (f *fakeScheduleStore) UpdateSchedule(_ context.Context, schedule *Schedule) (*Schedule, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	for i, s := range f.schedules {
		if s.ID == schedule.ID {
			if s.ArchivedAt != nil {
				return nil, ErrScheduleNotFound
			}
			persisted := *schedule
			f.schedules[i] = &persisted
			f.updated = append(f.updated, &persisted)
			return &persisted, nil
		}
	}
	return nil, ErrScheduleNotFound
}

func (f *fakeScheduleStore) ArchiveSchedule(_ context.Context, id string) (*Schedule, error) {
	if f.archiveErr != nil {
		return nil, f.archiveErr
	}
	for i, s := range f.schedules {
		if s.ID == id {
			if s.ArchivedAt != nil {
				return nil, ErrScheduleNotFound
			}
			now := time.Now()
			archived := *s
			archived.ArchivedAt = &now
			f.schedules[i] = &archived
			f.archivedIDs = append(f.archivedIDs, id)
			return &archived, nil
		}
	}
	return nil, ErrScheduleNotFound
}

// newScheduleService wires the fakes around a Service with the schedule store
// populated. perms defaults to the schedules.manage holder.
func newScheduleService(perms ...string) (*Service, *fakeScheduleStore, *fakeAudit) {
	store := &fakeScheduleStore{}
	audit := &fakeAudit{}
	if len(perms) == 0 {
		perms = []string{SchedulesPermission}
	}
	svc := NewService(nil, nil, store, &fakeCipher{}, &fakePerms{perms: perms}, audit, nil, nil, nil)
	return svc, store, audit
}

func scheduleFixture(id, name string) *Schedule {
	return &Schedule{ID: id, Name: name, IntervalUnit: IntervalUnitYear, IntervalMagnitude: 1}
}

func TestListSchedulesEmpty(t *testing.T) {
	// GET_LIST_EMPTY: no schedules → empty list, no error.
	svc, _, _ := newScheduleService()
	got, err := svc.ListSchedules(context.Background(), actorID)
	if err != nil {
		t.Fatalf("ListSchedules err = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("schedules = %d, want 0", len(got))
	}
}

func TestListSchedules(t *testing.T) {
	// GET_LIST: the ACTIVE catalog is returned, oldest first; archived rows are
	// filtered out by the store and never reach the surface.
	svc, store, _ := newScheduleService()
	store.schedules = []*Schedule{
		scheduleFixture("id-a", "1 year"),
		scheduleFixture("id-b", "1 month"),
	}
	archived := scheduleFixture("id-arch", "2 weeks")
	now := time.Now()
	archived.ArchivedAt = &now
	store.schedules = append(store.schedules, archived)

	got, err := svc.ListSchedules(context.Background(), actorID)
	if err != nil {
		t.Fatalf("ListSchedules err = %v", err)
	}
	if len(got) != 2 || got[0].ID != "id-a" || got[1].ID != "id-b" {
		t.Fatalf("schedules = %+v, want both active rows in order", got)
	}
}

func TestListSchedulesForbidden(t *testing.T) {
	// FORBIDDEN: caller without schedules.manage → ErrForbidden.
	svc, store, _ := newScheduleService("dashboard.view")
	store.schedules = []*Schedule{scheduleFixture("id-a", "1 year")}
	if _, err := svc.ListSchedules(context.Background(), actorID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestCreateScheduleValid(t *testing.T) {
	// CREATE_VALID: name trimmed, interval persisted, audited (schedule.create).
	svc, store, audit := newScheduleService()
	input := ScheduleInput{Name: "  1 year  ", IntervalUnit: IntervalUnitYear, IntervalMagnitude: 1}
	got, err := svc.CreateSchedule(context.Background(), actorID, input)
	if err != nil {
		t.Fatalf("CreateSchedule err = %v", err)
	}
	if got.Name != "1 year" {
		t.Errorf("name = %q, want trimmed", got.Name)
	}
	if got.IntervalUnit != IntervalUnitYear || got.IntervalMagnitude != 1 {
		t.Errorf("interval = %+v", got)
	}
	if got.ArchivedAt != nil {
		t.Error("new schedule must be active (ArchivedAt nil)")
	}
	if len(store.created) != 1 {
		t.Fatalf("created = %d, want 1", len(store.created))
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationScheduleCreate {
		t.Fatalf("audit events = %+v, want one create audit", audit.events)
	}
	// The create audit detail carries the persisted id (mirroring update/archive).
	if !strings.Contains(audit.events[0].detail, "id="+got.ID) {
		t.Errorf("create audit detail = %q, want it to carry the created id %q", audit.events[0].detail, got.ID)
	}
	if audit.events[0].actorID != actorID || audit.events[0].severity != AuditSeverityNormal {
		t.Errorf("audit actor/severity = %+v", audit.events[0])
	}
}

func TestCreateScheduleInvalid(t *testing.T) {
	// CREATE_INVALID: empty name / bad unit / magnitude ≤ 0 → 400-class German
	// error, nothing persisted.
	cases := []struct {
		name    string
		input   ScheduleInput
		wantMsg string
	}{
		{"empty name", ScheduleInput{Name: "  ", IntervalUnit: IntervalUnitYear, IntervalMagnitude: 1}, "Namen"},
		{"name too long bytes", ScheduleInput{Name: strings.Repeat("x", 256), IntervalUnit: IntervalUnitYear, IntervalMagnitude: 1}, "zu lang"},
		{"name too long runes", ScheduleInput{Name: strings.Repeat("ä", 256), IntervalUnit: IntervalUnitYear, IntervalMagnitude: 1}, "zu lang"},
		{"bad unit", ScheduleInput{Name: "x", IntervalUnit: "fortnight", IntervalMagnitude: 1}, "Zeiteinheit"},
		{"magnitude zero", ScheduleInput{Name: "x", IntervalUnit: IntervalUnitDay, IntervalMagnitude: 0}, "Intervallgröße"},
		{"magnitude negative", ScheduleInput{Name: "x", IntervalUnit: IntervalUnitDay, IntervalMagnitude: -1}, "Intervallgröße"},
		{"magnitude oversized", ScheduleInput{Name: "x", IntervalUnit: IntervalUnitDay, IntervalMagnitude: 1000001}, "Intervallgröße"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, store, _ := newScheduleService()
			_, err := svc.CreateSchedule(context.Background(), actorID, tc.input)
			var inv *InvalidSchedulesError
			if !errors.As(err, &inv) {
				t.Fatalf("err = %v, want *InvalidSchedulesError", err)
			}
			if !strings.Contains(inv.Message, tc.wantMsg) {
				t.Errorf("message = %q, want contains %q", inv.Message, tc.wantMsg)
			}
			if !errors.Is(err, ErrSchedulesInvalid) {
				t.Errorf("err = %v, want unwraps to ErrSchedulesInvalid", err)
			}
			if len(store.created) != 0 {
				t.Error("schedule must not be persisted on invalid input")
			}
		})
	}
}

func TestCreateScheduleNameLengthCountsRunes(t *testing.T) {
	// The 255-char name limit is counted in RUNES (utf8.RuneCountInString), so
	// an umlaut-heavy name of 255 runes (510 UTF-8 bytes) is accepted while the
	// same name at 256 runes is rejected — a byte-count check would wrongly
	// reject the 255-rune umlaut name (finding: client maxLength in UTF-16
	// units vs server byte count).
	svc, store, _ := newScheduleService()
	name255 := strings.Repeat("ä", 255) // 510 bytes, 255 runes
	got, err := svc.CreateSchedule(context.Background(), actorID, ScheduleInput{
		Name: name255, IntervalUnit: IntervalUnitYear, IntervalMagnitude: 1,
	})
	if err != nil {
		t.Fatalf("CreateSchedule(255 runes) err = %v, want accepted", err)
	}
	if got.Name != name255 {
		t.Errorf("name = %q", got.Name)
	}
	if len(store.created) != 1 {
		t.Fatalf("created = %d, want 1", len(store.created))
	}
}

func TestCreateScheduleDuplicateName(t *testing.T) {
	// CREATE_DUPLICATE: a create whose name another schedule already holds
	// (case-insensitive) is rejected, not persisted.
	svc, store, _ := newScheduleService()
	store.schedules = []*Schedule{scheduleFixture("id-a", "1 Year")}

	input := ScheduleInput{Name: "1 year", IntervalUnit: IntervalUnitYear, IntervalMagnitude: 1}
	_, err := svc.CreateSchedule(context.Background(), actorID, input)
	var inv *InvalidSchedulesError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *InvalidSchedulesError", err)
	}
	if !strings.Contains(inv.Message, MsgScheduleNameTaken) {
		t.Errorf("message = %q, want %q", inv.Message, MsgScheduleNameTaken)
	}
	if len(store.created) != 0 {
		t.Error("duplicate-name schedule must not be persisted")
	}
}

func TestCreateScheduleForbidden(t *testing.T) {
	svc, _, _ := newScheduleService("dashboard.view")
	input := ScheduleInput{Name: "x", IntervalUnit: IntervalUnitYear, IntervalMagnitude: 1}
	if _, err := svc.CreateSchedule(context.Background(), actorID, input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestUpdateScheduleValid(t *testing.T) {
	// UPDATE_VALID: name/interval replaced, audited (schedule.update). The
	// reserved weekday/time inputs are absent (ignored by design).
	svc, store, audit := newScheduleService()
	store.schedules = []*Schedule{scheduleFixture("id-a", "1 year")}

	input := ScheduleInput{Name: "2 years", IntervalUnit: IntervalUnitYear, IntervalMagnitude: 2}
	got, err := svc.UpdateSchedule(context.Background(), actorID, "id-a", input)
	if err != nil {
		t.Fatalf("UpdateSchedule err = %v", err)
	}
	if got.Name != "2 years" || got.IntervalUnit != IntervalUnitYear || got.IntervalMagnitude != 2 {
		t.Errorf("updated = %+v, want new values", got)
	}
	if len(store.updated) != 1 {
		t.Fatalf("updated = %d, want 1", len(store.updated))
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationScheduleUpdate {
		t.Fatalf("audit events = %+v, want one update audit", audit.events)
	}
}

func TestUpdateScheduleDuplicateName(t *testing.T) {
	// An update that takes another schedule's name is rejected; an update
	// keeping its own name (case variant) is fine.
	svc, store, _ := newScheduleService()
	store.schedules = []*Schedule{
		scheduleFixture("id-a", "1 year"),
		scheduleFixture("id-b", "1 month"),
	}

	input := ScheduleInput{Name: "1 MONTH", IntervalUnit: IntervalUnitMonth, IntervalMagnitude: 1}
	_, err := svc.UpdateSchedule(context.Background(), actorID, "id-a", input)
	var inv *InvalidSchedulesError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *InvalidSchedulesError", err)
	}
	if !strings.Contains(inv.Message, MsgScheduleNameTaken) {
		t.Errorf("message = %q, want %q", inv.Message, MsgScheduleNameTaken)
	}

	input = ScheduleInput{Name: "1 YEAR", IntervalUnit: IntervalUnitYear, IntervalMagnitude: 1}
	if _, err := svc.UpdateSchedule(context.Background(), actorID, "id-a", input); err != nil {
		t.Fatalf("UpdateSchedule(own name) err = %v", err)
	}
}

func TestUpdateScheduleNotFound(t *testing.T) {
	svc, _, _ := newScheduleService()
	input := ScheduleInput{Name: "x", IntervalUnit: IntervalUnitMonth, IntervalMagnitude: 1}
	if _, err := svc.UpdateSchedule(context.Background(), actorID, "id-missing", input); !errors.Is(err, ErrScheduleNotFound) {
		t.Fatalf("err = %v, want ErrScheduleNotFound", err)
	}
}

func TestUpdateScheduleArchived(t *testing.T) {
	// UPDATE_ARCHIVED: updating an already-archived row answers the 404
	// sentinel — the archived row is non-existent to the surface.
	svc, store, audit := newScheduleService()
	archived := scheduleFixture("id-arch", "1 year")
	now := time.Now()
	archived.ArchivedAt = &now
	store.schedules = []*Schedule{archived}

	input := ScheduleInput{Name: "1 year v2", IntervalUnit: IntervalUnitYear, IntervalMagnitude: 1}
	if _, err := svc.UpdateSchedule(context.Background(), actorID, "id-arch", input); !errors.Is(err, ErrScheduleNotFound) {
		t.Fatalf("err = %v, want ErrScheduleNotFound", err)
	}
	if len(store.updated) != 0 {
		t.Error("archived schedule must not be updated")
	}
	if len(audit.events) != 0 {
		t.Errorf("audit events = %+v, want none for a rejected update", audit.events)
	}
}

func TestArchiveSchedule(t *testing.T) {
	// ARCHIVE: archived_at set, the row leaves the active list, audited
	// (schedule.archive).
	svc, store, audit := newScheduleService()
	store.schedules = []*Schedule{scheduleFixture("id-a", "1 year")}

	got, err := svc.ArchiveSchedule(context.Background(), actorID, "id-a")
	if err != nil {
		t.Fatalf("ArchiveSchedule err = %v", err)
	}
	if got.ArchivedAt == nil {
		t.Error("archived_at = nil, want set")
	}
	if len(store.archivedIDs) != 1 || store.archivedIDs[0] != "id-a" {
		t.Errorf("archived = %v, want [id-a]", store.archivedIDs)
	}
	if len(audit.events) != 1 || audit.events[0].operation != AuditOperationScheduleArchive {
		t.Fatalf("audit events = %+v, want one archive audit", audit.events)
	}

	// The archived row leaves the active list.
	active, err := svc.ListSchedules(context.Background(), actorID)
	if err != nil {
		t.Fatalf("ListSchedules err = %v", err)
	}
	if len(active) != 0 {
		t.Errorf("active = %d, want 0 after archive", len(active))
	}
}

func TestArchiveScheduleArchived(t *testing.T) {
	// ARCHIVE_ARCHIVED: archiving an already-archived row answers the 404
	// sentinel (idempotent-or-conflict → the 404 sentinel).
	svc, store, _ := newScheduleService()
	archived := scheduleFixture("id-arch", "1 year")
	now := time.Now()
	archived.ArchivedAt = &now
	store.schedules = []*Schedule{archived}

	if _, err := svc.ArchiveSchedule(context.Background(), actorID, "id-arch"); !errors.Is(err, ErrScheduleNotFound) {
		t.Fatalf("err = %v, want ErrScheduleNotFound", err)
	}
	if len(store.archivedIDs) != 0 {
		t.Error("already-archived schedule must not be re-archived")
	}
}

func TestArchiveScheduleNotFound(t *testing.T) {
	svc, _, _ := newScheduleService()
	if _, err := svc.ArchiveSchedule(context.Background(), actorID, "id-missing"); !errors.Is(err, ErrScheduleNotFound) {
		t.Fatalf("err = %v, want ErrScheduleNotFound", err)
	}
}

func TestSchedulesForbidden(t *testing.T) {
	// FORBIDDEN: every schedule method re-checks the live permission.
	svc, store, _ := newScheduleService("dashboard.view")
	store.schedules = []*Schedule{scheduleFixture("id-a", "1 year")}
	input := ScheduleInput{Name: "x", IntervalUnit: IntervalUnitYear, IntervalMagnitude: 1}
	if _, err := svc.ListSchedules(context.Background(), actorID); !errors.Is(err, ErrForbidden) {
		t.Errorf("list err = %v, want ErrForbidden", err)
	}
	if _, err := svc.CreateSchedule(context.Background(), actorID, input); !errors.Is(err, ErrForbidden) {
		t.Errorf("create err = %v, want ErrForbidden", err)
	}
	if _, err := svc.UpdateSchedule(context.Background(), actorID, "id-a", input); !errors.Is(err, ErrForbidden) {
		t.Errorf("update err = %v, want ErrForbidden", err)
	}
	if _, err := svc.ArchiveSchedule(context.Background(), actorID, "id-a"); !errors.Is(err, ErrForbidden) {
		t.Errorf("archive err = %v, want ErrForbidden", err)
	}
}

func TestCurrentSchedulesReadOnlyPort(t *testing.T) {
	// The read-only consumer port (AD-16): returns the ACTIVE catalog for the
	// future Tool module, no permission check.
	svc, store, _ := newScheduleService()
	store.schedules = []*Schedule{scheduleFixture("id-a", "1 year")}
	got, err := svc.CurrentSchedules(context.Background())
	if err != nil {
		t.Fatalf("CurrentSchedules err = %v", err)
	}
	if len(got) != 1 || got[0].Name != "1 year" {
		t.Errorf("schedules = %+v, want the active catalog", got)
	}

	store.schedules = nil
	got, err = svc.CurrentSchedules(context.Background())
	if err != nil {
		t.Fatalf("CurrentSchedules err = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty list = %d, want 0", len(got))
	}
}
