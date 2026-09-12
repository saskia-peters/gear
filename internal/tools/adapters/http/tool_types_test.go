package http

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	toolscore "github.com/saskia-peters/gear/internal/tools/core"
	toolports "github.com/saskia-peters/gear/internal/tools/ports"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeService is an in-memory toolports.Service for the handler tests.
type fakeService struct {
	types      []*toolscore.ToolType
	listErr    error
	writeErr   error
	archiveErr error
	lastInput  toolscore.ToolTypeInput
	lastID     string
}

func (f *fakeService) ListToolTypes(_ context.Context, _ string) ([]*toolscore.ToolType, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.types == nil {
		return []*toolscore.ToolType{}, nil
	}
	return f.types, nil
}

func (f *fakeService) CreateToolType(_ context.Context, _ string, input toolscore.ToolTypeInput) (*toolscore.ToolType, error) {
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	f.lastInput = input
	return &toolscore.ToolType{
		ID: "id-new", Name: input.Name, DefaultScheduleID: input.DefaultScheduleID,
		RequiredQualificationID: input.RequiredQualificationID, InspectionMode: input.InspectionMode,
		Attributes: input.Attributes,
		Items:      []toolscore.ToolTypeChecklistItem{{ID: "item-1", Position: 0, Label: "Kabel"}},
	}, nil
}

func (f *fakeService) UpdateToolType(_ context.Context, _, id string, input toolscore.ToolTypeInput) (*toolscore.ToolType, error) {
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	f.lastID = id
	f.lastInput = input
	return &toolscore.ToolType{
		ID: id, Name: input.Name, DefaultScheduleID: input.DefaultScheduleID,
		RequiredQualificationID: input.RequiredQualificationID, InspectionMode: input.InspectionMode,
		Attributes: input.Attributes,
		Items:      []toolscore.ToolTypeChecklistItem{{ID: "item-1", Position: 0, Label: "Kabel"}},
	}, nil
}

func (f *fakeService) ArchiveToolType(_ context.Context, _, id string) (*toolscore.ToolType, error) {
	if f.archiveErr != nil {
		return nil, f.archiveErr
	}
	f.lastID = id
	return &toolscore.ToolType{ID: id, Name: "archiviert"}, nil
}

// The port contract (Story 4.3) grew the tool methods; the tool-type fakes
// never exercise the tool surface — these stubs keep the shared Service port
// compilable without touching the tool-type tests.
func (f *fakeService) ListTools(_ context.Context, _ string) ([]*toolscore.Tool, error) {
	return []*toolscore.Tool{}, nil
}

func (f *fakeService) ListToolsForDashboard(_ context.Context) ([]*toolscore.Tool, error) {
	return []*toolscore.Tool{}, nil
}

func (f *fakeService) CreateTool(_ context.Context, _ string, _ toolscore.ToolInput) (*toolscore.Tool, error) {
	return nil, toolscore.ErrToolNotFound
}

func (f *fakeService) UpdateTool(_ context.Context, _, _ string, _ toolscore.ToolInput) (*toolscore.Tool, error) {
	return nil, toolscore.ErrToolNotFound
}

func (f *fakeService) ArchiveTool(_ context.Context, _, _ string) (*toolscore.Tool, error) {
	return nil, toolscore.ErrToolNotFound
}

// toolTypeGateway wraps the REAL ToolTypeRoutes() behind the same
// RequireAnyPermission gate the composition root uses (tool_types.manage), with
// a fake session validator + permission resolver.
func toolTypeGateway(perms []string, session *usercore.Session, svc toolports.Service) http.Handler {
	h := NewHandler(svc, &gateValidator{session: session}, &gateResolver{perms: perms}, discardLogger())
	return auth.RequireAnyPermission(
		&gateValidator{session: session},
		&gateResolver{perms: perms},
		[]string{toolscore.ToolTypesManagePermission},
		"tool_types.manage access denied", discardLogger(),
	)(h.ToolTypeRoutes())
}

type gateValidator struct {
	session *usercore.Session
}

func (v *gateValidator) Validate(context.Context, string) (*usercore.Session, error) {
	return v.session, nil
}

type gateResolver struct {
	perms []string
}

func (r *gateResolver) ListPermissionsByUser(context.Context, string) ([]string, error) {
	return r.perms, nil
}

func activeAdmin() *usercore.Session {
	return &usercore.Session{User: &usercore.User{ID: "u-admin", Email: "admin@gear.local", State: usercore.StateActive}}
}

func doRequest(h http.Handler, method, path, token string, body string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func toolTypeFixture(id, name string) *toolscore.ToolType {
	return &toolscore.ToolType{
		ID: id, Name: name,
		DefaultScheduleID: "id-s1", RequiredQualificationID: "id-q1",
		InspectionMode: toolscore.InspectionModeChecklist,
		Items:          []toolscore.ToolTypeChecklistItem{{ID: "item-1", Position: 0, Label: "Kabel"}},
	}
}

func writeToolTypeBody() string {
	return `{"name":"Bohrmaschine","default_schedule_id":"id-s1","required_qualification_id":"id-q1","inspection_mode":"checklist","items":[{"label":"Kabel"},{"label":"Bohrfutter"}]}`
}

func TestToolTypesGetListEmpty(t *testing.T) {
	// GET_LIST_EMPTY: 200 `[]` (never null).
	surface := toolTypeGateway([]string{toolscore.ToolTypesManagePermission}, activeAdmin(), &fakeService{})
	rec := doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("body = %s, want JSON empty array", rec.Body.String())
	}
}

func TestToolTypesGetList(t *testing.T) {
	// GET_LIST: 200 active list with typed FKs, ordered checklist items AND the
	// attributes jsonb extension surface (Story 4.4 — attributes read back as a
	// JSON object); no archived rows.
	svc := &fakeService{types: []*toolscore.ToolType{
		toolTypeFixture("id-a", "Bohrmaschine"),
		toolTypeFixture("id-b", "Schleifmaschine"),
	}}
	svc.types[0].Attributes = map[string]any{"standort": "Werkstatt"}
	surface := toolTypeGateway([]string{toolscore.ToolTypesManagePermission}, activeAdmin(), svc)
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
	if body[0]["id"] != "id-a" || body[0]["name"] != "Bohrmaschine" {
		t.Errorf("row 0 = %+v", body[0])
	}
	if body[0]["default_schedule_id"] != "id-s1" || body[0]["required_qualification_id"] != "id-q1" {
		t.Errorf("row 0 FKs = %+v", body[0])
	}
	if body[0]["inspection_mode"] != "checklist" {
		t.Errorf("row 0 mode = %+v", body[0])
	}
	items, ok := body[0]["checklist_items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("checklist_items = %+v, want one ordered item", body[0]["checklist_items"])
	}
	first := items[0].(map[string]any)
	if first["label"] != "Kabel" || first["position"] != float64(0) {
		t.Errorf("item 0 = %+v, want label Kabel at position 0", first)
	}
	// The attributes surface is present on EVERY row and always a JSON object
	// ({} when empty, the stored set otherwise).
	attrs, ok := body[0]["attributes"].(map[string]any)
	if !ok || attrs["standort"] != "Werkstatt" {
		t.Errorf("row 0 attributes = %+v, want the stored set", body[0]["attributes"])
	}
	if _, present := body[1]["attributes"]; !present {
		t.Error("row 1 attributes missing, want the extension surface on every row")
	}
	attrs1, ok := body[1]["attributes"].(map[string]any)
	if !ok || len(attrs1) != 0 {
		t.Errorf("row 1 attributes = %+v, want an empty object", body[1]["attributes"])
	}
}

func TestToolTypesGetForbidden(t *testing.T) {
	// FORBIDDEN: authenticated caller without tool_types.manage → uniform 403
	// with no tool-type data and no hint of what is missing.
	svc := &fakeService{types: []*toolscore.ToolType{toolTypeFixture("id-a", "Bohrmaschine")}}
	surface := toolTypeGateway([]string{"dashboard.view"}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "bohrmaschine") || strings.Contains(rec.Body.String(), "id-a") {
		t.Errorf("403 body leaks tool-type data: %s", rec.Body.String())
	}
}

func TestToolTypesGetUnauthenticated(t *testing.T) {
	surface := toolTypeGateway([]string{toolscore.ToolTypesManagePermission}, nil, &fakeService{})
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

func TestToolTypesCreateValid(t *testing.T) {
	// CREATE_VALID: 201 with the saved type + German confirmation.
	svc := &fakeService{}
	surface := toolTypeGateway([]string{toolscore.ToolTypesManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/", "tok", writeToolTypeBody())
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if body["message"] != toolscore.MsgToolTypeSaved {
		t.Errorf("message = %v, want %q", body["message"], toolscore.MsgToolTypeSaved)
	}
	if body["name"] != "Bohrmaschine" {
		t.Errorf("body = %+v, want persisted name", body)
	}
	// The ordered items travel to the core.
	if len(svc.lastInput.Items) != 2 || svc.lastInput.Items[0].Label != "Kabel" {
		t.Errorf("core input items = %+v, want the submitted ordered list", svc.lastInput.Items)
	}
}

func TestToolTypesCreateInvalid(t *testing.T) {
	// CREATE_INVALID: bad mode → 400 invalid_request with the German message.
	svc := &fakeService{writeErr: &toolscore.InvalidToolTypeError{Message: "Bitte wähle einen gültigen Prüfmodus (Pass/Fail oder Checkliste)."}}
	surface := toolTypeGateway([]string{toolscore.ToolTypesManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/", "tok",
		`{"name":"x","default_schedule_id":"id-s1","required_qualification_id":"id-q1","inspection_mode":"matrix"}`)
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
	if !strings.Contains(env.Error.Message, "Prüfmodus") {
		t.Errorf("message = %q, want mode microcopy", env.Error.Message)
	}
}

func TestToolTypesCreateChecklistWithoutItems(t *testing.T) {
	// Fix 3: a checklist-mode create with ZERO items has no inspection template
	// → 400 invalid_request with the German message.
	svc := &fakeService{writeErr: &toolscore.InvalidToolTypeError{Message: toolscore.MsgToolTypeChecklistRequired}}
	surface := toolTypeGateway([]string{toolscore.ToolTypesManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/", "tok",
		`{"name":"x","default_schedule_id":"id-s1","required_qualification_id":"id-q1","inspection_mode":"checklist","items":[]}`)
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
	if !strings.Contains(env.Error.Message, "mindestens einen Eintrag") {
		t.Errorf("message = %q, want checklist-required microcopy", env.Error.Message)
	}
}

func TestToolTypesCreateBadSchedule(t *testing.T) {
	// CREATE_BAD_SCHEDULE: default_schedule_id not an ACTIVE schedule → 400
	// German (port lookup).
	svc := &fakeService{writeErr: &toolscore.InvalidToolTypeError{Message: toolscore.MsgToolTypeInvalidSchedule}}
	surface := toolTypeGateway([]string{toolscore.ToolTypesManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/", "tok", writeToolTypeBody())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if !strings.Contains(env.Error.Message, "Zeitplan") {
		t.Errorf("message = %q, want schedule microcopy", env.Error.Message)
	}
}

func TestToolTypesCreateBadQualification(t *testing.T) {
	// CREATE_BAD_QUALIFICATION: required_qualification_id unknown → 400 German
	// (port lookup).
	svc := &fakeService{writeErr: &toolscore.InvalidToolTypeError{Message: toolscore.MsgToolTypeInvalidQualification}}
	surface := toolTypeGateway([]string{toolscore.ToolTypesManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/", "tok", writeToolTypeBody())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if !strings.Contains(env.Error.Message, "Qualifikation") {
		t.Errorf("message = %q, want qualification microcopy", env.Error.Message)
	}
}

func TestToolTypesCreateDuplicate(t *testing.T) {
	// CREATE_DUPLICATE: duplicate name → 400 with the German duplicate message.
	svc := &fakeService{writeErr: &toolscore.InvalidToolTypeError{Message: toolscore.MsgToolTypeNameTaken}}
	surface := toolTypeGateway([]string{toolscore.ToolTypesManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/", "tok", writeToolTypeBody())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if !strings.Contains(env.Error.Message, "bereits einen Gerätetyp") {
		t.Errorf("message = %q, want duplicate-name microcopy", env.Error.Message)
	}
}

func TestToolTypesUpdate(t *testing.T) {
	// UPDATE_REPLACE_ITEMS: PUT returns 200 with the saved type + German
	// confirmation; the id from the URL travels to the core.
	svc := &fakeService{}
	surface := toolTypeGateway([]string{toolscore.ToolTypesManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPut, "/id-a", "tok", writeToolTypeBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if body["id"] != "id-a" || body["name"] != "Bohrmaschine" {
		t.Errorf("body = %+v, want updated type", body)
	}
	if body["message"] != toolscore.MsgToolTypeSaved {
		t.Errorf("message = %v, want %q", body["message"], toolscore.MsgToolTypeSaved)
	}
	if svc.lastID != "id-a" {
		t.Errorf("core received id = %q, want id-a", svc.lastID)
	}
}

func TestToolTypesUpdateArchived(t *testing.T) {
	// UPDATE_ARCHIVED: updating an archived row → uniform 404 sentinel.
	svc := &fakeService{writeErr: toolscore.ErrToolTypeNotFound}
	surface := toolTypeGateway([]string{toolscore.ToolTypesManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPut, "/id-archived", "tok", writeToolTypeBody())
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

func TestToolTypesArchive(t *testing.T) {
	// ARCHIVE: POST /{id}/archive returns 200 with the German confirmation.
	surface := toolTypeGateway([]string{toolscore.ToolTypesManagePermission}, activeAdmin(), &fakeService{})
	rec := doRequest(surface, http.MethodPost, "/id-a/archive", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if body["message"] != toolscore.MsgToolTypeArchived {
		t.Errorf("message = %v, want %q", body["message"], toolscore.MsgToolTypeArchived)
	}
}

func TestToolTypesArchiveArchived(t *testing.T) {
	// ARCHIVE_ARCHIVED: archiving an already-archived row → uniform 404
	// sentinel.
	svc := &fakeService{archiveErr: toolscore.ErrToolTypeNotFound}
	surface := toolTypeGateway([]string{toolscore.ToolTypesManagePermission}, activeAdmin(), svc)
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

func TestToolTypesWriteForbidden(t *testing.T) {
	// FORBIDDEN: every write verb answers the uniform 403 for a non-holder.
	surface := toolTypeGateway([]string{"dashboard.view"}, activeAdmin(), &fakeService{})
	if rec := doRequest(surface, http.MethodPost, "/", "tok", writeToolTypeBody()); rec.Code != http.StatusForbidden {
		t.Errorf("POST status = %d, want 403", rec.Code)
	}
	if rec := doRequest(surface, http.MethodPut, "/id-a", "tok", writeToolTypeBody()); rec.Code != http.StatusForbidden {
		t.Errorf("PUT status = %d, want 403", rec.Code)
	}
	if rec := doRequest(surface, http.MethodPost, "/id-a/archive", "tok", ""); rec.Code != http.StatusForbidden {
		t.Errorf("archive status = %d, want 403", rec.Code)
	}
}

func TestToolTypesExplicitEmptyAttributesReachesCore(t *testing.T) {
	// UPDATE_TYPE clear-signal (Story 4.4): an EXPLICIT `attributes: {}` must
	// reach the core as a NON-NIL empty map — distinct from an ABSENT field
	// (nil = unchanged) — on BOTH POST and PUT.
	body := `{"name":"Bohrmaschine","default_schedule_id":"id-s1","required_qualification_id":"id-q1","inspection_mode":"pass_fail","attributes":{}}`
	svc := &fakeService{}
	surface := toolTypeGateway([]string{toolscore.ToolTypesManagePermission}, activeAdmin(), svc)

	rec := doRequest(surface, http.MethodPost, "/", "tok", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.lastInput.Attributes == nil || len(svc.lastInput.Attributes) != 0 {
		t.Errorf("POST core input attributes = %+v, want a NON-NIL empty map (the clear signal)", svc.lastInput.Attributes)
	}

	rec = doRequest(surface, http.MethodPut, "/id-a", "tok", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.lastInput.Attributes == nil || len(svc.lastInput.Attributes) != 0 {
		t.Errorf("PUT core input attributes = %+v, want a NON-NIL empty map (the clear signal)", svc.lastInput.Attributes)
	}
}

func TestToolTypesNotFoundEnvelope(t *testing.T) {
	surface := toolTypeGateway([]string{toolscore.ToolTypesManagePermission}, activeAdmin(), &fakeService{})
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

func TestToolTypesMethodNotAllowedEnvelope(t *testing.T) {
	// No DELETE endpoint (soft archive only): DELETE / answers 405.
	surface := toolTypeGateway([]string{toolscore.ToolTypesManagePermission}, activeAdmin(), &fakeService{})
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

// writeToolTypeBodyAttrs is the POST/PUT body WITH the attributes JSONB
// surface (Story 4.4).
func writeToolTypeBodyAttrs() string {
	return `{"name":"Bohrmaschine","default_schedule_id":"id-s1","required_qualification_id":"id-q1","inspection_mode":"checklist","items":[{"label":"Kabel"}],"attributes":{"standort":"Werkstatt","leistung":1200}}`
}

func TestToolTypesCreateWithAttributes(t *testing.T) {
	// CREATE_TYPE_ATTRS: a POST carrying `attributes` travels to the core and
	// the write response (write+read) reflects the stored set.
	svc := &fakeService{}
	surface := toolTypeGateway([]string{toolscore.ToolTypesManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/", "tok", writeToolTypeBodyAttrs())
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.lastInput.Attributes == nil || svc.lastInput.Attributes["standort"] != "Werkstatt" {
		t.Errorf("core input attributes = %+v, want the submitted set", svc.lastInput.Attributes)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	attrs, ok := body["attributes"].(map[string]any)
	if !ok || attrs["standort"] != "Werkstatt" {
		t.Errorf("response attributes = %+v, want the stored set", body["attributes"])
	}
}

func TestToolTypesUpdateWithAttributes(t *testing.T) {
	// UPDATE_TYPE_ATTRS: a PUT carrying `attributes` travels to the core and the
	// response reflects the set.
	svc := &fakeService{}
	surface := toolTypeGateway([]string{toolscore.ToolTypesManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPut, "/id-a", "tok", writeToolTypeBodyAttrs())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.lastInput.Attributes == nil || svc.lastInput.Attributes["standort"] != "Werkstatt" {
		t.Errorf("core input attributes = %+v, want the submitted set", svc.lastInput.Attributes)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	attrs, ok := body["attributes"].(map[string]any)
	if !ok || attrs["standort"] != "Werkstatt" {
		t.Errorf("response attributes = %+v, want the stored set", body["attributes"])
	}
}

func TestToolTypesUpdateAbsentAttributes(t *testing.T) {
	// UPDATE_TYPE_ABSENT: a PUT WITHOUT the attributes field leaves the core
	// input nil — the server-side leave-unchanged contract applies.
	svc := &fakeService{}
	surface := toolTypeGateway([]string{toolscore.ToolTypesManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPut, "/id-a", "tok", writeToolTypeBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.lastInput.Attributes != nil {
		t.Errorf("core input attributes = %+v, want nil (absent = unchanged)", svc.lastInput.Attributes)
	}
}

func TestToolTypesInvalidAttributes(t *testing.T) {
	// VALID_INVALID_KEY: a bad key surfaces the uniform 400 invalid_request
	// with the German MsgInvalidAttributes message + machine-readable details
	// (mirroring the user-module profile precedent).
	svc := &fakeService{writeErr: &toolscore.AttributeError{Key: "   ", Reason: "empty key"}}
	surface := toolTypeGateway([]string{toolscore.ToolTypesManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/", "tok", writeToolTypeBodyAttrs())
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
	if env.Error.Message != toolscore.MsgInvalidAttributes {
		t.Errorf("message = %q, want %q", env.Error.Message, toolscore.MsgInvalidAttributes)
	}
	details, ok := env.Error.Details.(map[string]any)
	if !ok || details["reason"] != "empty key" {
		t.Errorf("details = %+v, want reason=empty key", env.Error.Details)
	}
}

func TestToolTypesCreateNonObjectAttributes(t *testing.T) {
	// VALID_NON_OBJECT: `attributes` as an array/string/primitive is rejected at
	// the HTTP decode boundary (the typed map[string]any input rejects a
	// non-object shape) → 400 German, never a write.
	for _, bad := range []string{`[1,2]`, `"string"`, `42`} {
		svc := &fakeService{}
		surface := toolTypeGateway([]string{toolscore.ToolTypesManagePermission}, activeAdmin(), svc)
		body := `{"name":"x","default_schedule_id":"id-s1","required_qualification_id":"id-q1","inspection_mode":"pass_fail","attributes":` + bad + `}`
		rec := doRequest(surface, http.MethodPost, "/", "tok", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("attributes=%s status = %d, want 400 (body %s)", bad, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "Ungültiges JSON-Format.") {
			t.Errorf("attributes=%s body = %s, want the German invalid-JSON message", bad, rec.Body.String())
		}
		if svc.lastInput.Name != "" {
			t.Errorf("attributes=%s: core must not receive the write (decoded as %+v)", bad, svc.lastInput)
		}
	}
}