package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/saskia-peters/gear/internal/admin/core"
	"github.com/saskia-peters/gear/internal/admin/ports"
	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

// Schedule fakeService methods (defined here, same package): the Story 4.1
// schedule surface consumed by the handler tests.

func (f *fakeService) ListSchedules(_ context.Context, _ string) ([]*core.Schedule, error) {
	if f.scheduleListErr != nil {
		return nil, f.scheduleListErr
	}
	if f.schedules == nil {
		return []*core.Schedule{}, nil
	}
	return f.schedules, nil
}

func (f *fakeService) CreateSchedule(_ context.Context, _ string, input core.ScheduleInput) (*core.Schedule, error) {
	if f.scheduleWriteErr != nil {
		return nil, f.scheduleWriteErr
	}
	f.lastScheduleInput = input
	return &core.Schedule{
		ID: "id-new", Name: input.Name, IntervalUnit: input.IntervalUnit,
		IntervalMagnitude: input.IntervalMagnitude,
	}, nil
}

func (f *fakeService) UpdateSchedule(_ context.Context, _, id string, input core.ScheduleInput) (*core.Schedule, error) {
	if f.scheduleWriteErr != nil {
		return nil, f.scheduleWriteErr
	}
	f.lastScheduleInput = input
	return &core.Schedule{
		ID: id, Name: input.Name, IntervalUnit: input.IntervalUnit,
		IntervalMagnitude: input.IntervalMagnitude,
	}, nil
}

func (f *fakeService) ArchiveSchedule(_ context.Context, _, id string) (*core.Schedule, error) {
	if f.scheduleArchiveErr != nil {
		return nil, f.scheduleArchiveErr
	}
	return &core.Schedule{ID: id, Name: "archiviert"}, nil
}

// scheduleGateway wraps the REAL ScheduleRoutes() behind the same
// RequireAnyPermission gate the composition root uses (schedules.manage), with
// a fake session validator + permission resolver.
func scheduleGateway(perms []string, session *usercore.Session, svc ports.Service) http.Handler {
	h := NewHandler(svc, discardLogger())
	return auth.RequireAnyPermission(
		&gateValidator{session: session},
		&gateResolver{perms: perms},
		[]string{core.SchedulesPermission},
		"schedules.manage access denied", discardLogger(),
	)(h.ScheduleRoutes())
}

func scheduleFixture(id, name string) *core.Schedule {
	return &core.Schedule{ID: id, Name: name, IntervalUnit: core.IntervalUnitYear, IntervalMagnitude: 1}
}

func TestSchedulesGetListEmpty(t *testing.T) {
	// GET_LIST_EMPTY: 200 `[]` (never null).
	surface := scheduleGateway([]string{core.SchedulesPermission}, activeAdmin(), &fakeService{})
	rec := doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("body = %s, want JSON empty array", rec.Body.String())
	}
}

func TestSchedulesGetList(t *testing.T) {
	// GET_LIST: 200 active list with name + interval; no archived rows, no
	// reserved weekday/time fields in the payload.
	svc := &fakeService{schedules: []*core.Schedule{
		scheduleFixture("id-a", "1 year"),
		scheduleFixture("id-b", "1 month"),
	}}
	surface := scheduleGateway([]string{core.SchedulesPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if len(body) != 2 {
		t.Fatalf("rows = %d, want 2", len(body))
	}
	if body[0]["id"] != "id-a" || body[0]["name"] != "1 year" {
		t.Errorf("row 0 = %+v", body[0])
	}
	if body[0]["interval_unit"] != "year" || body[0]["interval_magnitude"] != float64(1) {
		t.Errorf("row 0 interval = %+v", body[0])
	}
	for _, row := range body {
		if _, present := row["weekday_set"]; present {
			t.Error("response leaks the reserved weekday_set field")
		}
		if _, present := row["time_of_day"]; present {
			t.Error("response leaks the reserved time_of_day field")
		}
	}
}

func TestSchedulesGetForbidden(t *testing.T) {
	// FORBIDDEN: authenticated caller without schedules.manage → uniform 403
	// with no schedule data and no hint of what is missing.
	svc := &fakeService{schedules: []*core.Schedule{scheduleFixture("id-a", "1 year")}}
	surface := scheduleGateway([]string{core.SmtpSettingsPermission, "dashboard.view"}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "zeitplan") || strings.Contains(rec.Body.String(), "id-a") || strings.Contains(rec.Body.String(), "1 year") {
		t.Errorf("403 body leaks schedule data: %s", rec.Body.String())
	}
}

func TestSchedulesGetUnauthenticated(t *testing.T) {
	surface := scheduleGateway([]string{core.SchedulesPermission}, nil, &fakeService{})
	rec := doRequest(surface, http.MethodGet, "/", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 401 err = %v", err)
	}
	if env.Error.Code != "unauthorized" {
		t.Errorf("code = %q, want unauthorized", env.Error.Code)
	}
}

func TestSchedulesCreateValid(t *testing.T) {
	// CREATE_VALID: 201 with the saved schedule + German confirmation.
	surface := scheduleGateway([]string{core.SchedulesPermission}, activeAdmin(), &fakeService{})
	rec := doRequest(surface, http.MethodPost, "/", "tok",
		`{"name":"1 year","interval_unit":"year","interval_magnitude":1}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if body["message"] != core.MsgScheduleSaved {
		t.Errorf("message = %v, want %q", body["message"], core.MsgScheduleSaved)
	}
	if body["name"] != "1 year" || body["interval_unit"] != "year" || body["interval_magnitude"] != float64(1) {
		t.Errorf("body = %+v, want persisted schedule", body)
	}
}

func TestSchedulesCreateInvalid(t *testing.T) {
	// CREATE_INVALID: bad unit → 400 invalid_request with the German message.
	svc := &fakeService{scheduleWriteErr: &core.InvalidSchedulesError{Message: "Bitte wähle eine gültige Zeiteinheit (Jahr, Quartal, Monat, Woche oder Tag)."}}
	surface := scheduleGateway([]string{core.SchedulesPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/", "tok",
		`{"name":"x","interval_unit":"fortnight","interval_magnitude":1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if env.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", env.Error.Code)
	}
	if !strings.Contains(env.Error.Message, "Zeiteinheit") {
		t.Errorf("message = %q, want unit microcopy", env.Error.Message)
	}
}

func TestSchedulesCreateDuplicate(t *testing.T) {
	// CREATE_DUPLICATE: duplicate name → 400 with the German duplicate message.
	svc := &fakeService{scheduleWriteErr: &core.InvalidSchedulesError{Message: core.MsgScheduleNameTaken}}
	surface := scheduleGateway([]string{core.SchedulesPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/", "tok",
		`{"name":"1 YEAR","interval_unit":"year","interval_magnitude":1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if !strings.Contains(env.Error.Message, "bereits einen Zeitplan") {
		t.Errorf("message = %q, want duplicate-name microcopy", env.Error.Message)
	}
}

func TestSchedulesUpdate(t *testing.T) {
	// UPDATE_VALID: PUT returns 200 with the saved schedule + German confirmation.
	surface := scheduleGateway([]string{core.SchedulesPermission}, activeAdmin(), &fakeService{})
	rec := doRequest(surface, http.MethodPut, "/id-a", "tok",
		`{"name":"2 years","interval_unit":"year","interval_magnitude":2}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if body["id"] != "id-a" || body["name"] != "2 years" {
		t.Errorf("body = %+v, want updated schedule", body)
	}
	if body["message"] != core.MsgScheduleSaved {
		t.Errorf("message = %v, want %q", body["message"], core.MsgScheduleSaved)
	}
}

func TestSchedulesUpdateArchived(t *testing.T) {
	// UPDATE_ARCHIVED: updating an archived row → uniform 404 sentinel.
	svc := &fakeService{scheduleWriteErr: core.ErrScheduleNotFound}
	surface := scheduleGateway([]string{core.SchedulesPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPut, "/id-archived", "tok",
		`{"name":"x","interval_unit":"month","interval_magnitude":1}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 404 err = %v", err)
	}
	if env.Error.Code != "not_found" {
		t.Errorf("code = %q, want not_found", env.Error.Code)
	}
}

func TestSchedulesUpdateDuplicateArchivedName(t *testing.T) {
	// UPDATE to a name an ARCHIVED row holds: the active-only uniqueness guard
	// cannot see it, the DB UNIQUE constraint trips, and the repository maps it
	// to the German duplicate-name error — the handler must answer the 400
	// invalid_request envelope (NOT a 500).
	svc := &fakeService{scheduleWriteErr: &core.InvalidSchedulesError{Message: core.MsgScheduleNameTaken}}
	surface := scheduleGateway([]string{core.SchedulesPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPut, "/id-a", "tok",
		`{"name":"2 years","interval_unit":"year","interval_magnitude":2}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 400 err = %v", err)
	}
	if env.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", env.Error.Code)
	}
	if !strings.Contains(env.Error.Message, "bereits einen Zeitplan") {
		t.Errorf("message = %q, want duplicate-name microcopy", env.Error.Message)
	}
}

func TestSchedulesArchive(t *testing.T) {
	// ARCHIVE: POST /{id}/archive returns 200 with the German confirmation.
	surface := scheduleGateway([]string{core.SchedulesPermission}, activeAdmin(), &fakeService{})
	rec := doRequest(surface, http.MethodPost, "/id-a/archive", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if body["message"] != core.MsgScheduleArchived {
		t.Errorf("message = %v, want %q", body["message"], core.MsgScheduleArchived)
	}
}

func TestSchedulesArchiveArchived(t *testing.T) {
	// ARCHIVE_ARCHIVED: archiving an already-archived row → uniform 404
	// sentinel.
	svc := &fakeService{scheduleArchiveErr: core.ErrScheduleNotFound}
	surface := scheduleGateway([]string{core.SchedulesPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-archived/archive", "tok", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 404 err = %v", err)
	}
	if env.Error.Code != "not_found" {
		t.Errorf("code = %q, want not_found", env.Error.Code)
	}
}

func TestSchedulesNotFoundEnvelope(t *testing.T) {
	surface := scheduleGateway([]string{core.SchedulesPermission}, activeAdmin(), &fakeService{})
	rec := doRequest(surface, http.MethodGet, "/a/b/c", "tok", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 404 err = %v", err)
	}
	if env.Error.Code != "not_found" {
		t.Errorf("code = %q, want not_found", env.Error.Code)
	}
}

func TestSchedulesMethodNotAllowedEnvelope(t *testing.T) {
	// No DELETE endpoint (soft archive only): DELETE / answers 405.
	surface := scheduleGateway([]string{core.SchedulesPermission}, activeAdmin(), &fakeService{})
	rec := doRequest(surface, http.MethodDelete, "/", "tok", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 405 err = %v", err)
	}
	if env.Error.Code != "method_not_allowed" {
		t.Errorf("code = %q, want method_not_allowed", env.Error.Code)
	}
}
