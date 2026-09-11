package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	toolscore "github.com/saskia-peters/gear/internal/tools/core"
	toolports "github.com/saskia-peters/gear/internal/tools/ports"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

// fakeToolService is an in-memory toolports.Service for the tool-surface
// handler tests.
type fakeToolService struct {
	tools      []*toolscore.Tool
	listErr    error
	writeErr   error
	archiveErr error
	lastInput  toolscore.ToolInput
	lastID     string
}

func (f *fakeToolService) ListToolTypes(context.Context, string) ([]*toolscore.ToolType, error) {
	return []*toolscore.ToolType{}, nil
}
func (f *fakeToolService) CreateToolType(context.Context, string, toolscore.ToolTypeInput) (*toolscore.ToolType, error) {
	return nil, toolscore.ErrToolTypeNotFound
}
func (f *fakeToolService) UpdateToolType(context.Context, string, string, toolscore.ToolTypeInput) (*toolscore.ToolType, error) {
	return nil, toolscore.ErrToolTypeNotFound
}
func (f *fakeToolService) ArchiveToolType(context.Context, string, string) (*toolscore.ToolType, error) {
	return nil, toolscore.ErrToolTypeNotFound
}

func (f *fakeToolService) ListTools(_ context.Context, _ string) ([]*toolscore.Tool, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.tools == nil {
		return []*toolscore.Tool{}, nil
	}
	return f.tools, nil
}

func (f *fakeToolService) CreateTool(_ context.Context, _ string, input toolscore.ToolInput) (*toolscore.Tool, error) {
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	f.lastInput = input
	return &toolscore.Tool{
		ID: "id-new", Name: input.Name, ToolTypeID: input.ToolTypeID,
		ToolTypeName: "Bohrmaschine", ScheduleID: input.ScheduleID,
	}, nil
}

func (f *fakeToolService) UpdateTool(_ context.Context, _, id string, input toolscore.ToolInput) (*toolscore.Tool, error) {
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	f.lastID = id
	f.lastInput = input
	return &toolscore.Tool{
		ID: id, Name: input.Name, ToolTypeID: input.ToolTypeID,
		ToolTypeName: "Bohrmaschine", ScheduleID: input.ScheduleID,
	}, nil
}

func (f *fakeToolService) ArchiveTool(_ context.Context, _, id string) (*toolscore.Tool, error) {
	if f.archiveErr != nil {
		return nil, f.archiveErr
	}
	f.lastID = id
	now := time.Now()
	return &toolscore.Tool{ID: id, Name: "archiviert", ToolTypeName: "Bohrmaschine", ArchivedAt: &now}, nil
}

var _ toolports.Service = (*fakeToolService)(nil)

// toolGateway wraps the REAL ToolRoutes() behind the same RequireAnyPermission
// gate the composition root uses (tools.manage), with a fake session validator
// + permission resolver.
func toolGateway(perms []string, session *usercore.Session, svc toolports.Service) http.Handler {
	h := NewHandler(svc, discardLogger())
	return auth.RequireAnyPermission(
		&gateValidator{session: session},
		&gateResolver{perms: perms},
		[]string{toolscore.ToolsManagePermission},
		"tools.manage access denied", discardLogger(),
	)(h.ToolRoutes())
}

func toolFixture(id, name string) *toolscore.Tool {
	return &toolscore.Tool{
		ID: id, Name: name,
		ToolTypeID: "id-t1", ToolTypeName: "Bohrmaschine",
		ScheduleID: "id-s1",
	}
}

func writeToolBody() string {
	return `{"name":"Bohrmaschine-01","tool_type_id":"id-t1","schedule_id":""}`
}

func TestToolsGetListEmpty(t *testing.T) {
	// GET_LIST_EMPTY: 200 `[]` (never null).
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), &fakeToolService{})
	rec := doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("body = %s, want JSON empty array", rec.Body.String())
	}
}

func TestToolsGetList(t *testing.T) {
	// GET_LIST: 200 active list with typed FKs + the JOINed type name + the
	// attributes passthrough; no archived rows.
	svc := &fakeToolService{tools: []*toolscore.Tool{
		toolFixture("id-a", "Bohrmaschine-01"),
		toolFixture("id-b", "Bohrmaschine-02"),
	}}
	svc.tools[0].Attributes = map[string]any{"standort": "Werkstatt"}
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)
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
	if body[0]["id"] != "id-a" || body[0]["name"] != "Bohrmaschine-01" {
		t.Errorf("row 0 = %+v", body[0])
	}
	if body[0]["tool_type_id"] != "id-t1" || body[0]["tool_type_name"] != "Bohrmaschine" {
		t.Errorf("row 0 type = %+v", body[0])
	}
	if body[0]["schedule_id"] != "id-s1" {
		t.Errorf("row 0 schedule override = %+v", body[0]["schedule_id"])
	}
	attrs, ok := body[0]["attributes"].(map[string]any)
	if !ok || attrs["standort"] != "Werkstatt" {
		t.Errorf("row 0 attributes = %+v, want the passthrough", body[0]["attributes"])
	}
	// Active rows carry a NULL archived_at (never archived — the active surface
	// filters archived rows server-side).
	if archivedAt, present := body[0]["archived_at"]; !present || archivedAt != nil {
		t.Errorf("row 0 archived_at = %+v, want present + null on the active surface", body[0]["archived_at"])
	}
}

func TestToolsGetForbidden(t *testing.T) {
	// FORBIDDEN: authenticated caller without tools.manage → uniform 403 with
	// no tool data and no hint of what is missing.
	svc := &fakeToolService{tools: []*toolscore.Tool{toolFixture("id-a", "Bohrmaschine-01")}}
	surface := toolGateway([]string{"dashboard.view"}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "bohrmaschine") || strings.Contains(rec.Body.String(), "id-a") {
		t.Errorf("403 body leaks tool data: %s", rec.Body.String())
	}
}

func TestToolsGetUnauthenticated(t *testing.T) {
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, nil, &fakeToolService{})
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

func TestToolsCreateValid(t *testing.T) {
	// CREATE_VALID: 201 with the saved tool + German confirmation; an empty
	// schedule override travels to the core (inherit the type default, AD-5).
	svc := &fakeToolService{}
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/", "tok", writeToolBody())
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if body["message"] != toolscore.MsgToolSaved {
		t.Errorf("message = %v, want %q", body["message"], toolscore.MsgToolSaved)
	}
	if body["name"] != "Bohrmaschine-01" {
		t.Errorf("body = %+v, want persisted name", body)
	}
	if svc.lastInput.ScheduleID != "" {
		t.Errorf("core input schedule override = %q, want empty", svc.lastInput.ScheduleID)
	}
	if svc.lastInput.ToolTypeID != "id-t1" {
		t.Errorf("core input tool type = %q, want id-t1", svc.lastInput.ToolTypeID)
	}
}

func TestToolsCreateWithOverride(t *testing.T) {
	// CREATE_OVERRIDE: a non-empty valid override travels to the core.
	svc := &fakeToolService{}
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/", "tok",
		`{"name":"Bohrmaschine-01","tool_type_id":"id-t1","schedule_id":"id-s1"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.lastInput.ScheduleID != "id-s1" {
		t.Errorf("core input schedule override = %q, want id-s1", svc.lastInput.ScheduleID)
	}
}

func TestToolsCreateBadType(t *testing.T) {
	// CREATE_BAD_TYPE: tool_type_id missing/archived → 400 German.
	svc := &fakeToolService{writeErr: &toolscore.InvalidToolError{Message: toolscore.MsgToolInvalidType}}
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/", "tok", writeToolBody())
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
	if !strings.Contains(env.Error.Message, "Gerätetyp") {
		t.Errorf("message = %q, want type microcopy", env.Error.Message)
	}
}

func TestToolsCreateBadOverride(t *testing.T) {
	// CREATE_BAD_OVERRIDE: schedule_id unknown/archived → 400 German.
	svc := &fakeToolService{writeErr: &toolscore.InvalidToolError{Message: toolscore.MsgToolInvalidSchedule}}
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/", "tok", writeToolBody())
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

func TestToolsCreateInvalid(t *testing.T) {
	// CREATE_INVALID: empty name → 400 invalid_request with the German message.
	svc := &fakeToolService{writeErr: &toolscore.InvalidToolError{Message: "Bitte gib einen Namen für das Werkzeug an."}}
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/", "tok",
		`{"name":"","tool_type_id":"id-t1","schedule_id":""}`)
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
	if !strings.Contains(env.Error.Message, "Namen") {
		t.Errorf("message = %q, want name microcopy", env.Error.Message)
	}
}

func TestToolsCreateDuplicate(t *testing.T) {
	// CREATE_DUPLICATE: duplicate name → 400 with the German duplicate message.
	svc := &fakeToolService{writeErr: &toolscore.InvalidToolError{Message: toolscore.MsgToolNameTaken}}
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/", "tok", writeToolBody())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if !strings.Contains(env.Error.Message, "bereits ein Werkzeug") {
		t.Errorf("message = %q, want duplicate-name microcopy", env.Error.Message)
	}
}

func TestToolsUpdateClearsOverride(t *testing.T) {
	// UPDATE_CLEAR_OVERRIDE: PUT with an empty schedule_id returns 200 + German
	// confirmation; the id from the URL travels to the core; the empty override
	// travels so the store CLEARS it (AD-5).
	svc := &fakeToolService{}
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPut, "/id-a", "tok", writeToolBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if body["id"] != "id-a" || body["name"] != "Bohrmaschine-01" {
		t.Errorf("body = %+v, want updated tool", body)
	}
	if body["message"] != toolscore.MsgToolSaved {
		t.Errorf("message = %v, want %q", body["message"], toolscore.MsgToolSaved)
	}
	if svc.lastID != "id-a" {
		t.Errorf("core received id = %q, want id-a", svc.lastID)
	}
	if svc.lastInput.ScheduleID != "" {
		t.Errorf("core input schedule override = %q, want empty (cleared)", svc.lastInput.ScheduleID)
	}
}

func TestToolsUpdateArchived(t *testing.T) {
	// UPDATE_ARCHIVED: updating an archived row → uniform 404 sentinel.
	svc := &fakeToolService{writeErr: toolscore.ErrToolNotFound}
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPut, "/id-archived", "tok", writeToolBody())
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

func TestToolsArchive(t *testing.T) {
	// ARCHIVE: POST /{id}/archive returns 200 with the German confirmation AND
	// the archived state is observable via the (now-set) archived_at field —
	// the archive response serializes as a state-changed row, not an active one.
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), &fakeToolService{})
	rec := doRequest(surface, http.MethodPost, "/id-a/archive", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if body["message"] != toolscore.MsgToolArchived {
		t.Errorf("message = %v, want %q", body["message"], toolscore.MsgToolArchived)
	}
	archivedAt, present := body["archived_at"]
	if !present {
		t.Error("archived_at missing from the archive response, want the archived timestamp")
	}
	if s, ok := archivedAt.(string); !ok || s == "" {
		t.Errorf("archived_at = %v, want a non-empty RFC3339 string after archive", archivedAt)
	}
}

func TestToolsArchiveArchived(t *testing.T) {
	// ARCHIVE_ARCHIVED: archiving an already-archived row → uniform 404
	// sentinel.
	svc := &fakeToolService{archiveErr: toolscore.ErrToolNotFound}
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)
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

func TestToolsWriteForbidden(t *testing.T) {
	// FORBIDDEN: every write verb answers the uniform 403 for a non-holder.
	surface := toolGateway([]string{"dashboard.view"}, activeAdmin(), &fakeToolService{})
	if rec := doRequest(surface, http.MethodPost, "/", "tok", writeToolBody()); rec.Code != http.StatusForbidden {
		t.Errorf("POST status = %d, want 403", rec.Code)
	}
	if rec := doRequest(surface, http.MethodPut, "/id-a", "tok", writeToolBody()); rec.Code != http.StatusForbidden {
		t.Errorf("PUT status = %d, want 403", rec.Code)
	}
	if rec := doRequest(surface, http.MethodPost, "/id-a/archive", "tok", ""); rec.Code != http.StatusForbidden {
		t.Errorf("archive status = %d, want 403", rec.Code)
	}
}

func TestToolsNotFoundEnvelope(t *testing.T) {
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), &fakeToolService{})
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

func TestToolsMethodNotAllowedEnvelope(t *testing.T) {
	// No DELETE endpoint (soft archive only): DELETE / answers 405.
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), &fakeToolService{})
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