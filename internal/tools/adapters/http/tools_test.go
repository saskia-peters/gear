package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	toolscore "github.com/saskia-peters/gear/internal/tools/core"
	toolports "github.com/saskia-peters/gear/internal/tools/ports"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

// fakeToolService is an in-memory toolports.Service for the tool-surface
// handler tests.
type fakeToolService struct {
	tools           []*toolscore.Tool
	dashboardTools  []*toolscore.DashboardTool
	listErr         error
	writeErr        error
	archiveErr      error
	startErr        error
	submitErr       error
	startItems      []toolscore.ToolTypeChecklistItem
	lastInput       toolscore.ToolInput
	lastID          string
	lastInspection  toolscore.InspectionInput
	submitNextDue   *time.Time
	submitNilResult bool
	// ReinstateTool fixture (Story 5.6): reinstateErr drives the error rows;
	// reinstateStatus is the derived status of a success; reinstateNil simulates
	// a nil-returning service path (a clean 500); lastReason captures the reason.
	reinstateErr     error
	reinstateStatus  toolscore.ToolStatus
	reinstateNil     bool
	lastReinstateID  string
	lastReason       string
	// ListInspectionHistory fixture (Story 6.3): historyErr drives the error
	// rows; history is the returned payload (defaults to an empty history);
	// historyNil simulates a nil-returning service path (a clean 500);
	// lastHistoryID captures the tool id from the URL.
	historyErr    error
	history       *toolscore.ToolHistory
	historyNil    bool
	lastHistoryID string
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

// ListToolsForDashboard serves the dashboard.read surface (Story 4-3b + 6.1)
// from the same in-memory catalog; the handler calls it ungated. When
// dashboardTools is set it is returned verbatim (status fixtures); otherwise
// the plain tools are wrapped with a default GREEN status so the existing
// shape assertions keep passing.
func (f *fakeToolService) ListToolsForDashboard(_ context.Context) ([]*toolscore.DashboardTool, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.dashboardTools != nil {
		return f.dashboardTools, nil
	}
	if f.tools == nil {
		return []*toolscore.DashboardTool{}, nil
	}
	out := make([]*toolscore.DashboardTool, 0, len(f.tools))
	// Patch 10: the default GREEN status carries a REAL next_due (the clock
	// always produces one for a green tool) — so a UI regression that expects
	// green rows to carry a due date can never be masked by the fake.
	nextDue := time.Now().Add(30 * 24 * time.Hour)
	for _, tool := range f.tools {
		out = append(out, &toolscore.DashboardTool{
			Tool:   *tool,
			Status: toolscore.ToolStatus{Status: toolscore.ToolStatusCodeGreen, NextDue: &nextDue},
		})
	}
	return out, nil
}

func (f *fakeToolService) CreateTool(_ context.Context, _ string, input toolscore.ToolInput) (*toolscore.Tool, error) {
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	f.lastInput = input
	return &toolscore.Tool{
		ID: "id-new", Name: input.Name, ToolTypeID: input.ToolTypeID,
		ToolTypeName: "Bohrmaschine", ScheduleID: input.ScheduleID,
		InventoryNumber: "GEAR000001",
		Attributes:      input.Attributes,
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
		InventoryNumber: input.InventoryNumber,
		Attributes:      input.Attributes,
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

func (f *fakeToolService) StartInspection(_ context.Context, _, toolID string) (*toolscore.InspectionStartResult, error) {
	if f.startErr != nil {
		return nil, f.startErr
	}
	f.lastID = toolID
	return &toolscore.InspectionStartResult{
		ToolID:         toolID,
		ToolName:       "Bohrmaschine-01",
		ToolTypeID:     "id-t1",
		ToolTypeName:   "Bohrmaschine",
		InspectionMode: toolscore.InspectionModeChecklist,
		ChecklistItems: f.startItems,
	}, nil
}

func (f *fakeToolService) SubmitInspection(_ context.Context, _, toolID string, input toolscore.InspectionInput) (*toolscore.SubmitInspectionResult, error) {
	if f.submitErr != nil {
		return nil, f.submitErr
	}
	if f.submitNilResult {
		// Patch 2: a nil-returning service path (a wiring defect) — the handler
		// must answer a clean 500, never panic.
		return nil, nil
	}
	f.lastID = toolID
	f.lastInspection = input
	// Snapshot the FULL submitted items slice (patch 14): the checklist
	// round-trip asserts every submitted item, never just the first.
	items := make([]toolscore.InspectionItem, 0, len(input.Items))
	for i, in := range input.Items {
		items = append(items, toolscore.InspectionItem{
			ID: fmt.Sprintf("id-item-%d", i), InspectionID: "id-insp-1", ItemID: in.ItemID,
			Label: fmt.Sprintf("Punkt-%d", i), Position: i, Result: in.Result,
		})
	}
	status := toolscore.ToolStatus{Status: toolscore.ToolStatusCodeGreen}
	if input.Result == toolscore.InspectionResultFail {
		status = toolscore.ToolStatus{Status: toolscore.ToolStatusCodeOOS}
	}
	if f.submitNextDue != nil {
		t := *f.submitNextDue
		status.NextDue = &t
	}
	return &toolscore.SubmitInspectionResult{
		Inspection: &toolscore.Inspection{
			ID:            "id-insp-1",
			ToolID:        toolID,
			InspectorID:   "u-admin",
			Mode:          input.Mode,
			OverallResult: input.Result,
			Notes:         input.Notes,
			SubmittedAt:   time.Now(),
			Items:         items,
		},
		Status: status,
	}, nil
}

func (f *fakeToolService) ReinstateTool(_ context.Context, _, toolID, reason string) (*toolscore.ReinstateResult, error) {
	if f.reinstateErr != nil {
		return nil, f.reinstateErr
	}
	if f.reinstateNil {
		return nil, nil
	}
	f.lastReinstateID = toolID
	f.lastReason = reason
	return &toolscore.ReinstateResult{Status: f.reinstateStatus}, nil
}

func (f *fakeToolService) ListInspectionHistory(_ context.Context, _, toolID string) (*toolscore.ToolHistory, error) {
	if f.historyErr != nil {
		return nil, f.historyErr
	}
	if f.historyNil {
		return nil, nil
	}
	if f.history == nil {
		return &toolscore.ToolHistory{
			Inspections:    []*toolscore.ToolHistoryInspection{},
			Reinstatements: []*toolscore.ToolHistoryReinstatement{},
		}, nil
	}
	f.lastHistoryID = toolID
	return f.history, nil
}

var _ toolports.Service = (*fakeToolService)(nil)

// toolGateway wraps the REAL ToolRoutes() behind the same ANY-of gate the
// composition root uses ([tools.manage, tool.edit], Story 4-3b), with a fake
// session validator + permission resolver. The write-only tools.manage sub-gate
// lives INSIDE ToolRoutes, so the per-action split is exercised here too.
func toolGateway(perms []string, session *usercore.Session, svc toolports.Service) http.Handler {
	h := NewHandler(svc, &gateValidator{session: session}, &gateResolver{perms: perms}, discardLogger())
	return auth.RequireAnyPermission(
		&gateValidator{session: session},
		&gateResolver{perms: perms},
		[]string{toolscore.ToolsManagePermission, toolscore.ToolEditPermission},
		"tools.manage/tool.edit access denied", discardLogger(),
	)(h.ToolRoutes())
}

// dashboardToolGateway wraps the REAL DashboardToolsRoutes() behind the same
// RequirePermission gate the composition root uses (dashboard.view, Story
// 4-3b), with a fake session validator + permission resolver.
func dashboardToolGateway(perms []string, session *usercore.Session, svc toolports.Service) http.Handler {
	h := NewHandler(svc, &gateValidator{session: session}, &gateResolver{perms: perms}, discardLogger())
	return auth.RequirePermission(
		&gateValidator{session: session},
		&gateResolver{perms: perms},
		toolscore.DashboardViewPermission,
	)(h.DashboardToolsRoutes())
}

// toolsInspectionGateway mimics the composition-root combined /api/v1/tools
// router (Story 5.1 + 5.3 + 5.6 + 6.3): the dashboard list surface (GET /,
// dashboard.view), the inspection surface (POST /{id}/inspection/start + POST
// /{id}/inspection, inspection.submit), the reinstatement surface (POST
// /{id}/reinstatement, tool.reinstate) AND the history surface (GET
// /{id}/history, inspection.history.view) combined via exact-match Handle +
// prefix Mount, EACH behind its OWN gate — so the composition mount gate test
// is exercised here (a dashboard.view-but-not-inspection.submit caller reads
// the list but 403s on the start/submit; an inspection.submit-but-not-
// tool.reinstate caller 403s on the reinstatement; a dashboard.view-but-not-
// inspection.history.view caller 403s on the history). Each surface is mounted
// at the full path prefix; InspectionRoutes/ReinstateRoutes/HistoryRoutes own
// the route patterns.
func toolsInspectionGateway(perms []string, session *usercore.Session, svc toolports.Service) http.Handler {
	h := NewHandler(svc, &gateValidator{session: session}, &gateResolver{perms: perms}, discardLogger())
	dashboardSurface := auth.RequirePermission(
		&gateValidator{session: session},
		&gateResolver{perms: perms},
		toolscore.DashboardViewPermission,
	)(h.DashboardToolsRoutes())
	inspectionSurface := auth.RequirePermission(
		&gateValidator{session: session},
		&gateResolver{perms: perms},
		toolscore.InspectionSubmitPermission,
	)(h.InspectionRoutes())
	reinstateSurface := auth.RequirePermission(
		&gateValidator{session: session},
		&gateResolver{perms: perms},
		toolscore.ToolReinstatePermission,
	)(h.ReinstateRoutes())
	historySurface := auth.RequirePermission(
		&gateValidator{session: session},
		&gateResolver{perms: perms},
		toolscore.InspectionHistoryViewPermission,
	)(h.HistoryRoutes())
	combined := chi.NewRouter()
	combined.NotFound(httpapi.NotFoundHandler())
	combined.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	combined.Handle("/", dashboardSurface)
	combined.Mount("/{id}/inspection", inspectionSurface)
	combined.Mount("/{id}/reinstatement", reinstateSurface)
	combined.Mount("/{id}/history", historySurface)
	return combined
}

// nakedInspectionRouter exposes the InspectionRoutes router WITHOUT the auth
// gateway, so the handler's own guards (the nil-user 401) are directly testable
// — unreachable through the gated composition but still defense-in-depth at the
// handler layer.
func nakedInspectionRouter(svc toolports.Service) http.Handler {
	h := NewHandler(svc, &gateValidator{}, &gateResolver{}, discardLogger())
	return h.InspectionRoutes()
}

// nakedHistoryRouter exposes the HistoryRoutes router WITHOUT the auth gateway,
// so the handler's own guards (the nil-user 401) are directly testable.
func nakedHistoryRouter(svc toolports.Service) http.Handler {
	h := NewHandler(svc, &gateValidator{}, &gateResolver{}, discardLogger())
	return h.HistoryRoutes()
}

func toolFixture(id, name string) *toolscore.Tool {
	return &toolscore.Tool{
		ID: id, Name: name,
		ToolTypeID: "id-t1", ToolTypeName: "Bohrmaschine",
		ScheduleID:      "id-s1",
		InventoryNumber: "GEAR00000X",
	}
}

func writeToolBody() string {
	return `{"name":"Bohrmaschine-01","tool_type_id":"id-t1","schedule_id":""}`
}

// writeToolBodyInventory is the PUT body WITH an inventory-number edit (Story
// 4-3b, UPDATE_INVENTORY).
func writeToolBodyInventory() string {
	return `{"name":"Bohrmaschine-01","tool_type_id":"id-t1","schedule_id":"","inventory_number":"GEAR0042"}`
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
	if body[0]["inventory_number"] != "GEAR00000X" {
		t.Errorf("row 0 inventory_number = %+v, want GEAR00000X", body[0]["inventory_number"])
	}
	attrs, ok := body[0]["attributes"].(map[string]any)
	if !ok || attrs["standort"] != "Werkstatt" {
		t.Errorf("row 0 attributes = %+v, want the passthrough", body[0]["attributes"])
	}
	// The nil-attributes row serializes as a JSON OBJECT too — never null (the
	// DTO defaults a nil map to `{}`), mirroring TestToolTypesGetList.
	attrs1, ok := body[1]["attributes"].(map[string]any)
	if !ok || len(attrs1) != 0 {
		t.Errorf("row 1 attributes = %+v, want an empty JSON object (never null)", body[1]["attributes"])
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

func TestDashboardToolsGetListEmpty(t *testing.T) {
	// GET_LIST_EMPTY (Story 4-3b): 200 `[]` (never null) — the SPA keeps the
	// "Keine Werkzeuge vorhanden" EmptyState.
	surface := dashboardToolGateway([]string{toolscore.DashboardViewPermission}, activeAdmin(), &fakeToolService{})
	rec := doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("body = %s, want JSON empty array", rec.Body.String())
	}
}

func TestDashboardToolsGetList(t *testing.T) {
	// GET_LIST (Story 4-3b): 200 with the MINIMAL dashboard DTO — id, name,
	// tool_type_id, tool_type_name + inventory_number (shown as row meta in the
	// Werkzeugliste) + the derived status. No schedule_id, no
	// default_schedule_id, no attributes, no audit timestamps on this surface.
	svc := &fakeToolService{tools: []*toolscore.Tool{
		toolFixture("id-a", "Bohrmaschine-01"),
		toolFixture("id-b", "Bohrmaschine-02"),
	}}
	surface := dashboardToolGateway([]string{toolscore.DashboardViewPermission}, activeAdmin(), svc)
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
	if body[0]["inventory_number"] != "GEAR00000X" {
		t.Errorf("row 0 inventory_number = %+v, want GEAR00000X", body[0]["inventory_number"])
	}
	// Story 6.1: every row carries the derived status (green here — the fake
	// wraps plain tools with a default green + a real next_due).
	status, ok := body[0]["status"].(map[string]any)
	if !ok || status["status"] != "green" {
		t.Errorf("row 0 status = %+v, want {status: green, next_due: <RFC3339>}", body[0]["status"])
	}
	if nextDue, ok := status["next_due"].(string); !ok || nextDue == "" {
		t.Errorf("row 0 next_due = %v, want a non-empty RFC3339 string (the clock always produces one)", status["next_due"])
	}
	for _, row := range body {
		for _, leak := range []string{"schedule_id", "default_schedule_id", "attributes", "archived_at", "created_at", "updated_at"} {
			if _, present := row[leak]; present {
				t.Errorf("dashboard DTO leaks %q: %+v (minimal surface, Story 4-3b)", leak, row)
			}
		}
	}
}

func TestDashboardToolsGetListStatus(t *testing.T) {
	// GET_LIST status (Story 6.1, DASH_DTO): each row carries the derived
	// `status { status, next_due }` — oos/red/orange/green with the next-due
	// anchor (RFC3339 UTC, null for oos / never-inspected red) — and nothing
	// else leaks on the minimal surface.
	due := time.Date(2026, 10, 15, 8, 30, 0, 0, time.UTC)
	svc := &fakeToolService{dashboardTools: []*toolscore.DashboardTool{
		{Tool: *toolFixture("id-a", "Bohrmaschine-01"), Status: toolscore.ToolStatus{Status: toolscore.ToolStatusCodeGreen, NextDue: &due}},
		{Tool: *toolFixture("id-b", "Bohrmaschine-02"), Status: toolscore.ToolStatus{Status: toolscore.ToolStatusCodeOOS}},
		{Tool: *toolFixture("id-c", "Bohrmaschine-03"), Status: toolscore.ToolStatus{Status: toolscore.ToolStatusCodeOrange, NextDue: &due}},
		{Tool: *toolFixture("id-d", "Bohrmaschine-04"), Status: toolscore.ToolStatus{Status: toolscore.ToolStatusCodeRed}},
	}}
	surface := dashboardToolGateway([]string{toolscore.DashboardViewPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if len(body) != 4 {
		t.Fatalf("rows = %d, want 4", len(body))
	}
	want := []struct {
		id     string
		status string
		due    any
	}{
		{"id-a", "green", "2026-10-15T08:30:00Z"},
		{"id-b", "oos", nil},
		{"id-c", "orange", "2026-10-15T08:30:00Z"},
		{"id-d", "red", nil},
	}
	for i, w := range want {
		row := body[i]
		if row["id"] != w.id {
			t.Errorf("row %d id = %v, want %s", i, row["id"], w.id)
		}
		status, ok := row["status"].(map[string]any)
		if !ok || status["status"] != w.status {
			t.Errorf("row %d status = %+v, want %q", i, row["status"], w.status)
		}
		if status["next_due"] != w.due {
			t.Errorf("row %d next_due = %v, want %v", i, status["next_due"], w.due)
		}
		for _, leak := range []string{"schedule_id", "default_schedule_id", "attributes", "archived_at", "created_at", "updated_at"} {
			if _, present := row[leak]; present {
				t.Errorf("row %d leaks %q: %+v (minimal surface, Story 4-3b)", i, leak, row)
			}
		}
	}
}

func TestDashboardToolsGetUnauthenticated(t *testing.T) {
	// GET_UNAUTHENTICATED (Story 4-3b): no session → 401 uniform envelope, no
	// tool data.
	surface := dashboardToolGateway([]string{toolscore.DashboardViewPermission}, nil, &fakeToolService{})
	rec := doRequest(surface, http.MethodGet, "/", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 401 err = %v", err)
	}
	if env.Error.Code != "unauthorized" {
		t.Errorf("code = %q, want unauthorized", env.Error.Code)
	}
}

func TestDashboardToolsGetForbidden(t *testing.T) {
	// GET_FORBIDDEN (Story 4-3b, AD-6): an authenticated caller WITHOUT
	// dashboard.view — even a tools.manage holder — answers the uniform 403
	// with no tool data exposed.
	svc := &fakeToolService{tools: []*toolscore.Tool{toolFixture("id-a", "Bohrmaschine-01")}}
	surface := dashboardToolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "bohrmaschine") || strings.Contains(rec.Body.String(), "id-a") {
		t.Errorf("403 body leaks tool data: %s", rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 403 err = %v", err)
	}
	if env.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", env.Error.Code)
	}
}

func TestDashboardToolsNoWrites(t *testing.T) {
	// Story 4-3b: the dashboard surface is read-only — the write verbs answer
	// the uniform 405 (no route is registered for them), never a write through
	// to the core. A path outside the single GET / route (no /{id} here) answers
	// the uniform 404.
	surface := dashboardToolGateway([]string{toolscore.DashboardViewPermission}, activeAdmin(), &fakeToolService{})
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		if rec := doRequest(surface, m, "/", "tok", writeToolBody()); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s status = %d, want 405", m, rec.Code)
		}
	}
	if rec := doRequest(surface, http.MethodPost, "/id-a/archive", "tok", ""); rec.Code != http.StatusNotFound {
		t.Errorf("archive status = %d, want 404 (no /{id} routes on the dashboard surface)", rec.Code)
	}
}

func TestDashboardToolsNotFoundEnvelope(t *testing.T) {
	surface := dashboardToolGateway([]string{toolscore.DashboardViewPermission}, activeAdmin(), &fakeToolService{})
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
	// CREATE_AUTO: the server auto-assigns the inventory number (a client value
	// is ignored) and the response carries it.
	if body["inventory_number"] != "GEAR000001" {
		t.Errorf("body inventory_number = %v, want the auto-assigned GEAR000001", body["inventory_number"])
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

func TestToolsEditOnlyCanReadAndUpdate(t *testing.T) {
	// GATE_GET / GATE_UPDATE (Story 4-3b): a tool.edit-ONLY holder (no
	// tools.manage) can GET (list) and PUT (edit) through the any-of outer gate —
	// the write-only sub-gate does NOT cover GET/PUT.
	svc := &fakeToolService{tools: []*toolscore.Tool{toolFixture("id-a", "Bohrmaschine-01")}}
	surface := toolGateway([]string{toolscore.ToolEditPermission}, activeAdmin(), svc)

	rec := doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding GET err = %v", err)
	}
	if len(body) != 1 || body[0]["id"] != "id-a" {
		t.Errorf("GET body = %+v, want the tool list", body)
	}

	rec = doRequest(surface, http.MethodPut, "/id-a", "tok", writeToolBodyInventory())
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.lastInput.InventoryNumber != "GEAR0042" {
		t.Errorf("core received inventory_number = %q, want GEAR0042", svc.lastInput.InventoryNumber)
	}
}

func TestToolsEditOnlyCannotCreateOrArchive(t *testing.T) {
	// GATE_CREATE / GATE_ARCHIVE (Story 4-3b): the write-only sub-router
	// re-applies a tools.manage-ONLY gate, so a tool.edit-only holder is denied
	// POST (create) and POST /{id}/archive with the uniform 403 — no tool data.
	svc := &fakeToolService{tools: []*toolscore.Tool{toolFixture("id-a", "Bohrmaschine-01")}}
	surface := toolGateway([]string{toolscore.ToolEditPermission}, activeAdmin(), svc)

	rec := doRequest(surface, http.MethodPost, "/", "tok", writeToolBody())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "bohrmaschine") {
		t.Errorf("POST 403 body leaks tool data: %s", rec.Body.String())
	}

	rec = doRequest(surface, http.MethodPost, "/id-a/archive", "tok", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("archive status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.lastID != "" {
		t.Errorf("core received archive id = %q, want none (denied at the sub-gate)", svc.lastID)
	}
}

func TestToolsUpdateInventory(t *testing.T) {
	// UPDATE_INVENTORY: a PUT carrying an inventory_number edits it — the value
	// travels to the core and the response reflects it.
	svc := &fakeToolService{}
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPut, "/id-a", "tok", writeToolBodyInventory())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if body["inventory_number"] != "GEAR0042" {
		t.Errorf("response inventory_number = %v, want GEAR0042", body["inventory_number"])
	}
	if svc.lastInput.InventoryNumber != "GEAR0042" {
		t.Errorf("core received inventory_number = %q, want GEAR0042", svc.lastInput.InventoryNumber)
	}
}

func TestToolsUpdateInventoryCleared(t *testing.T) {
	// UPDATE_CLEAR: the core rejects an empty inventory_number (a tool always
	// has one) — mapped to the uniform 400 German message.
	svc := &fakeToolService{writeErr: &toolscore.InvalidToolError{Message: toolscore.MsgToolInventoryNumberRequired}}
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPut, "/id-a", "tok", `{"name":"Bohrmaschine-01","tool_type_id":"id-t1","schedule_id":"","inventory_number":""}`)
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
	if !strings.Contains(env.Error.Message, "leer") {
		t.Errorf("message = %q, want the never-empty microcopy", env.Error.Message)
	}
}

func TestToolsCreateIgnoresClientInventory(t *testing.T) {
	// CREATE_IGNORE_CLIENT: a client-sent inventory_number on create is passed
	// through the handler but the SERVER auto-assigns the returned number (the
	// fake service mimics the auto-assignment; the core ignores the client
	// value entirely — pinned at the core level).
	svc := &fakeToolService{}
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/", "tok",
		`{"name":"Bohrmaschine-01","tool_type_id":"id-t1","schedule_id":"","inventory_number":"GEAR999999"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if body["inventory_number"] != "GEAR000001" {
		t.Errorf("response inventory_number = %v, want the server-assigned GEAR000001", body["inventory_number"])
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

// writeToolBodyAttrs is the POST/PUT body WITH the attributes JSONB surface
// (Story 4.4).
func writeToolBodyAttrs() string {
	return `{"name":"Bohrmaschine-01","tool_type_id":"id-t1","schedule_id":"","attributes":{"standort":"Werkstatt","leistung":1200}}`
}

func TestToolsCreateWithAttributes(t *testing.T) {
	// CREATE_TOOL_ATTRS: a POST carrying `attributes` travels to the core and
	// the write response (write+read) reflects the stored set.
	svc := &fakeToolService{}
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/", "tok", writeToolBodyAttrs())
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

func TestToolsUpdateWithAttributes(t *testing.T) {
	// UPDATE_TOOL_ATTRS: a PUT carrying `attributes` travels to the core and the
	// response reflects the set.
	svc := &fakeToolService{}
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPut, "/id-a", "tok", writeToolBodyAttrs())
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

func TestToolsUpdateAbsentAttributes(t *testing.T) {
	// UPDATE_TOOL_ABSENT: a PUT WITHOUT the attributes field leaves the core
	// input nil — the server-side leave-unchanged contract applies.
	svc := &fakeToolService{}
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPut, "/id-a", "tok", writeToolBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.lastInput.Attributes != nil {
		t.Errorf("core input attributes = %+v, want nil (absent = unchanged)", svc.lastInput.Attributes)
	}
}

func TestToolsInvalidAttributes(t *testing.T) {
	// VALID_INVALID_KEY: a bad key surfaces the uniform 400 invalid_request
	// with the German MsgInvalidAttributes message + machine-readable details.
	svc := &fakeToolService{writeErr: &toolscore.AttributeError{Key: "   ", Reason: "empty key"}}
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/", "tok", writeToolBodyAttrs())
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

func TestToolsCreateNonObjectAttributes(t *testing.T) {
	// VALID_NON_OBJECT: `attributes` as an array/string/primitive is rejected at
	// the HTTP decode boundary (the typed map[string]any input rejects a
	// non-object shape) → 400 German, never a write.
	for _, bad := range []string{`[1,2]`, `"string"`, `42`} {
		svc := &fakeToolService{}
		surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)
		body := `{"name":"x","tool_type_id":"id-t1","schedule_id":"","attributes":` + bad + `}`
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

func TestToolsExplicitEmptyAttributesReachesCore(t *testing.T) {
	// UPDATE_TOOL_CLEAR / CREATE clear-signal (Story 4.4): an EXPLICIT
	// `attributes: {}` must reach the core as a NON-NIL empty map — distinct
	// from an ABSENT field (nil = unchanged) — on BOTH POST and PUT.
	body := `{"name":"Bohrmaschine-01","tool_type_id":"id-t1","schedule_id":"","attributes":{}}`
	svc := &fakeToolService{}
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)

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

func TestToolsEditOnlyCanEditAttributes(t *testing.T) {
	// GATE_UPDATE (Story 4-3b + 4.4): a tool.edit-ONLY holder (no tools.manage)
	// edits tool ATTRIBUTES via the existing PUT any-of gate — no separate
	// attribute endpoint exists.
	svc := &fakeToolService{}
	surface := toolGateway([]string{toolscore.ToolEditPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPut, "/id-a", "tok", writeToolBodyAttrs())
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.lastInput.Attributes == nil || svc.lastInput.Attributes["standort"] != "Werkstatt" {
		t.Errorf("core input attributes = %+v, want the submitted set", svc.lastInput.Attributes)
	}
}

// ============================================================================
// Inspection start (Story 5.1, FR-11/AD-7): the qualification-gated
// POST /api/v1/tools/{id}/inspection/start surface behind inspection.submit.
// ============================================================================

func TestInspectionStartEligible(t *testing.T) {
	// START_ELIGIBLE: an inspection.submit holder (who also reads the dashboard)
	// starts an inspection → 200 with the tool + its type's inspection_mode DTO.
	// The mode-aware /start payload (Story 5.2) carries the type's ordered
	// checklist items — the SPA renders one Pass/Fail group per item.
	svc := &fakeToolService{startItems: []toolscore.ToolTypeChecklistItem{
		{ID: "id-i1", Position: 1, Label: "Kabel"},
		{ID: "id-i2", Position: 2, Label: "Bohrfutter"},
	}}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/inspection/start", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if body["tool_id"] != "id-a" || body["tool_name"] != "Bohrmaschine-01" {
		t.Errorf("tool = %+v, want id-a / Bohrmaschine-01", body)
	}
	if body["tool_type_id"] != "id-t1" || body["tool_type_name"] != "Bohrmaschine" {
		t.Errorf("type = %+v, want id-t1 / Bohrmaschine", body)
	}
	if body["inspection_mode"] != toolscore.InspectionModeChecklist {
		t.Errorf("mode = %+v, want %q", body["inspection_mode"], toolscore.InspectionModeChecklist)
	}
	items, ok := body["checklist_items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("checklist_items = %+v, want the two ordered items", body["checklist_items"])
	}
	if item, ok := items[0].(map[string]any); !ok || item["id"] != "id-i1" || item["position"] != float64(1) || item["label"] != "Kabel" {
		t.Errorf("checklist_items[0] = %+v, want id-i1 / 1 / Kabel", items[0])
	}
	if item, ok := items[1].(map[string]any); !ok || item["id"] != "id-i2" || item["label"] != "Bohrfutter" {
		t.Errorf("checklist_items[1] = %+v, want id-i2 / Bohrfutter", items[1])
	}
}

func TestInspectionStartEligibleEmptyItems(t *testing.T) {
	// START_ELIGIBLE_NO_ITEMS: a pass_fail (or item-less) type answers an EMPTY
	// checklist_items array — never null — so the SPA's empty-checklist fallback
	// is a well-formed array.
	svc := &fakeToolService{startItems: nil}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/inspection/start", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"checklist_items":[]`) {
		t.Errorf("body = %s, want an EMPTY checklist_items array", rec.Body.String())
	}
}

func TestInspectionStartMissingQual(t *testing.T) {
	// START_MISSING_QUAL: the tool's type requires a qualification the caller
	// lacks → 403 uniform envelope with the German reason.
	svc := &fakeToolService{startErr: toolscore.ErrToolQualificationMissing}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/inspection/start", "tok", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 403 err = %v", err)
	}
	if env.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", env.Error.Code)
	}
	if env.Error.Message != toolscore.MsgToolQualificationMissing {
		t.Errorf("message = %q, want %q", env.Error.Message, toolscore.MsgToolQualificationMissing)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "bohrmaschine") {
		t.Errorf("403 body leaks tool data: %s", rec.Body.String())
	}
}

func TestInspectionStartToolNotFound(t *testing.T) {
	// START_TOOL_NOT_FOUND: unknown / archived tool → 404 uniform envelope.
	svc := &fakeToolService{startErr: toolscore.ErrToolNotFound}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-missing/inspection/start", "tok", "")
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
	if env.Error.Message != toolscore.MsgToolNotFound {
		t.Errorf("message = %q, want %q", env.Error.Message, toolscore.MsgToolNotFound)
	}
}

func TestInspectionStartUnauthenticated(t *testing.T) {
	// START_UNAUTHENTICATED: no session → 401 uniform envelope.
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, nil, &fakeToolService{})
	rec := doRequest(surface, http.MethodPost, "/id-a/inspection/start", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 401 err = %v", err)
	}
	if env.Error.Code != "unauthorized" {
		t.Errorf("code = %q, want unauthorized", env.Error.Code)
	}
}

func TestInspectionStartCompositionMountGate(t *testing.T) {
	// Composition mount gate (Story 5.1): the start surface is a SIBLING to the
	// dashboard list — a dashboard.view-but-not-inspection.submit caller (e.g. a
	// report-only role) can still READ the Werkzeugliste but the start answers
	// the generic 403 with NO tool data. The inspection.submit holder passes.
	svc := &fakeToolService{tools: []*toolscore.Tool{toolFixture("id-a", "Bohrmaschine-01")}}

	// dashboard.view WITHOUT inspection.submit: list 200, start 403.
	dashboardOnly := toolsInspectionGateway([]string{toolscore.DashboardViewPermission}, activeAdmin(), svc)
	if rec := doRequest(dashboardOnly, http.MethodGet, "/", "tok", ""); rec.Code != http.StatusOK {
		t.Fatalf("dashboard GET status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	rec := doRequest(dashboardOnly, http.MethodPost, "/id-a/inspection/start", "tok", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("start status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 403 err = %v", err)
	}
	if env.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", env.Error.Code)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "bohrmaschine") || strings.Contains(rec.Body.String(), "id-a") {
		t.Errorf("403 body leaks tool data: %s", rec.Body.String())
	}

	// inspection.submit WITHOUT dashboard.view: the start 200s (the mount gate),
	// the dashboard list 403s (its own gate).
	submitOnly := toolsInspectionGateway([]string{toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	if rec := doRequest(submitOnly, http.MethodPost, "/id-a/inspection/start", "tok", ""); rec.Code != http.StatusOK {
		t.Fatalf("start (submit-only) status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := doRequest(submitOnly, http.MethodGet, "/", "tok", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("dashboard GET (submit-only) status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestInspectionStartMethodNotAllowedEnvelope(t *testing.T) {
	// Only POST is registered on the start surface: GET answers the uniform 405.
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), &fakeToolService{})
	rec := doRequest(surface, http.MethodGet, "/id-a/inspection/start", "tok", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 405 err = %v", err)
	}
	if env.Error.Code != "method_not_allowed" {
		t.Errorf("code = %q, want method_not_allowed", env.Error.Code)
	}
}

func TestInspectionStartInternalErrorEnvelope(t *testing.T) {
	// DEFAULT branch (mapInspectionError): an UNEXPECTED service error → 500
	// internal_error uniform envelope with the German message, and NO tool data
	// leak — the raw error never reaches the client.
	svc := &fakeToolService{startErr: errors.New("boom")}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/inspection/start", "tok", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 500 err = %v", err)
	}
	if env.Error.Code != "internal_error" {
		t.Errorf("code = %q, want internal_error", env.Error.Code)
	}
	if env.Error.Message != "Ein interner Fehler ist aufgetreten." {
		t.Errorf("message = %q, want the German internal-error microcopy", env.Error.Message)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "bohrmaschine") || strings.Contains(rec.Body.String(), "id-a") {
		t.Errorf("500 body leaks tool data: %s", rec.Body.String())
	}
}

func TestInspectionStartForbiddenEnvelope(t *testing.T) {
	// ErrForbidden branch (mapInspectionError): the core re-check can deny a
	// caller whose live set lost inspection.submit between the gateway and the
	// core (defense-in-depth, AD-6). The handler maps it to the generic 403 with
	// no hint of what is missing (FR-19) and no tool data.
	svc := &fakeToolService{startErr: toolscore.ErrForbidden}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/inspection/start", "tok", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 403 err = %v", err)
	}
	if env.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", env.Error.Code)
	}
	if env.Error.Message != "Keine Berechtigung." {
		t.Errorf("message = %q, want the generic no-hint forbidden microcopy", env.Error.Message)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "bohrmaschine") || strings.Contains(rec.Body.String(), "id-a") {
		t.Errorf("403 body leaks tool data: %s", rec.Body.String())
	}
}

func TestInspectionStartNilUserUnauthorized(t *testing.T) {
	// The handler's nil-user 401 guard is unreachable through the gated
	// composition (the gateway always sets the user), but it is still
	// defense-in-depth at the handler layer (a direct caller must never see the
	// start without a session). Drive it through the NAKED router (no gateway,
	// route owned at "/start") — no user in the context → uniform 401.
	surface := nakedInspectionRouter(&fakeToolService{})
	rec := doRequest(surface, http.MethodPost, "/start", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 401 err = %v", err)
	}
	if env.Error.Code != "unauthorized" {
		t.Errorf("code = %q, want unauthorized", env.Error.Code)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "bohrmaschine") {
		t.Errorf("401 body leaks tool data: %s", rec.Body.String())
	}
}

// ============================================================================
// Inspection submit (Story 5.3, FR-12/FR-13/FR-14/AD-4/AD-5): the persistence
// seam at POST /api/v1/tools/{id}/inspection behind inspection.submit.
// ============================================================================

// submitPassBody is a pass_fail pass body (SUBMIT_PASSFAIL).
func submitPassBody() string {
	return `{"mode":"pass_fail","result":"pass","notes":"Alles ok","items":[]}`
}

func TestInspectionSubmitPass(t *testing.T) {
	// SUBMIT_PASSFAIL: a valid pass body → 200 with the persisted record +
	// derived status (green for a fresh pass), the input travels to the core.
	svc := &fakeToolService{}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/inspection", "tok", submitPassBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	insp, ok := body["inspection"].(map[string]any)
	if !ok {
		t.Fatalf("inspection = %+v, want the persisted record", body["inspection"])
	}
	if insp["id"] != "id-insp-1" || insp["tool_id"] != "id-a" {
		t.Errorf("inspection = %+v, want the persisted record", insp)
	}
	if insp["overall_result"] != "pass" || insp["mode"] != "pass_fail" || insp["notes"] != "Alles ok" {
		t.Errorf("inspection = %+v, want the submitted values", insp)
	}
	status, ok := body["status"].(map[string]any)
	if !ok || status["status"] != "green" {
		t.Errorf("status = %+v, want green", body["status"])
	}
	if svc.lastID != "id-a" || svc.lastInspection.Result != "pass" {
		t.Errorf("core received id %q / input %+v", svc.lastID, svc.lastInspection)
	}
}

func TestInspectionSubmitFailOOS(t *testing.T) {
	// SUBMIT_FAIL: a failing inspection answers 200 with the record + the
	// derived `oos` status (AD-4 — the fake mirrors the core derivation).
	svc := &fakeToolService{}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/inspection", "tok",
		`{"mode":"pass_fail","result":"fail","notes":"","items":[]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	insp := body["inspection"].(map[string]any)
	if insp["overall_result"] != "fail" {
		t.Errorf("inspection = %+v, want a fail record", insp)
	}
	status := body["status"].(map[string]any)
	if status["status"] != "oos" {
		t.Errorf("status = %+v, want oos on a fail", status)
	}
	if nextDue, present := status["next_due"]; !present || nextDue != nil {
		t.Errorf("next_due = %v, want present + null for oos", nextDue)
	}
}

func TestInspectionSubmitNextDueWire(t *testing.T) {
	// Patch 11: a NON-nil NextDue serializes as the expected RFC3339 UTC string
	// on the wire (every other fake returns nil, so the serialization branch is
	// otherwise untested).
	due := time.Date(2026, 10, 15, 8, 30, 0, 0, time.UTC)
	svc := &fakeToolService{submitNextDue: &due}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/inspection", "tok", submitPassBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	status := body["status"].(map[string]any)
	if status["status"] != "green" {
		t.Errorf("status = %+v, want green", status)
	}
	if status["next_due"] != "2026-10-15T08:30:00Z" {
		t.Errorf("next_due = %+v, want the RFC3339 UTC 2026-10-15T08:30:00Z", status["next_due"])
	}
}

func TestInspectionSubmitChecklist(t *testing.T) {
	// SUBMIT_CHECKLIST: the snapshot items round-trip through the response DTO
	// (patch 14 — EVERY submitted item is snapshotted, never just the first).
	svc := &fakeToolService{}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/inspection", "tok",
		`{"mode":"checklist","result":"pass","notes":"","items":[{"item_id":"id-i1","result":"pass"},{"item_id":"id-i2","result":"fail"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	insp := body["inspection"].(map[string]any)
	items, ok := insp["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("items = %+v, want the two snapshotted items", insp["items"])
	}
	first := items[0].(map[string]any)
	if first["item_id"] != "id-i1" || first["result"] != "pass" || first["position"] != float64(0) {
		t.Errorf("item[0] = %+v, want id-i1 / pass / position 0", first)
	}
	second := items[1].(map[string]any)
	if second["item_id"] != "id-i2" || second["result"] != "fail" || second["position"] != float64(1) {
		t.Errorf("item[1] = %+v, want id-i2 / fail / position 1", second)
	}
}

func TestInspectionSubmitInvalid(t *testing.T) {
	// SUBMIT_INVALID: a validation failure (mode mismatch / bad result / notes
	// too long / checklist mismatch) → 400 invalid_request with the German
	// message, never a record.
	svc := &fakeToolService{submitErr: &toolscore.InvalidInspectionError{Message: toolscore.MsgInspectionModeMismatch}}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/inspection", "tok", submitPassBody())
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
	if env.Error.Message != toolscore.MsgInspectionModeMismatch {
		t.Errorf("message = %q, want %q", env.Error.Message, toolscore.MsgInspectionModeMismatch)
	}
}

func TestInspectionSubmitNotesTooLong(t *testing.T) {
	// SUBMIT_NOTES: the notes > 4000 runes German message round-trips.
	svc := &fakeToolService{submitErr: &toolscore.InvalidInspectionError{Message: toolscore.MsgInspectionNotesTooLong}}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/inspection", "tok", submitPassBody())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "4000") {
		t.Errorf("body = %s, want the notes bound microcopy", rec.Body.String())
	}
}

func TestInspectionSubmitGated(t *testing.T) {
	// SUBMIT_GATED: a caller lacking inspection.submit (or the tool's required
	// qualification) → 403 uniform envelope, no tool data leaked.
	svc := &fakeToolService{submitErr: toolscore.ErrForbidden}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/inspection", "tok", submitPassBody())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 403 err = %v", err)
	}
	if env.Error.Code != "forbidden" || env.Error.Message != "Keine Berechtigung." {
		t.Errorf("403 envelope = %+v, want the generic no-hint forbidden", env)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "bohrmaschine") || strings.Contains(rec.Body.String(), "id-a") {
		t.Errorf("403 body leaks tool data: %s", rec.Body.String())
	}

	// Missing qualification → the German 403.
	svc = &fakeToolService{submitErr: toolscore.ErrToolQualificationMissing}
	surface = toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	rec = doRequest(surface, http.MethodPost, "/id-a/inspection", "tok", submitPassBody())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing-qual status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), toolscore.MsgToolQualificationMissing) {
		t.Errorf("body = %s, want the German qualification microcopy", rec.Body.String())
	}
}

func TestInspectionSubmitToolNotFound(t *testing.T) {
	// SUBMIT_ARCHIVED / SUBMIT_UNKNOWN: unknown/archived tool → 404 uniform
	// envelope.
	svc := &fakeToolService{submitErr: toolscore.ErrToolNotFound}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-missing/inspection", "tok", submitPassBody())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 404 err = %v", err)
	}
	if env.Error.Code != "not_found" || env.Error.Message != toolscore.MsgToolNotFound {
		t.Errorf("404 envelope = %+v, want the uniform not_found", env)
	}
}

func TestInspectionSubmitUnauthenticated(t *testing.T) {
	// SUBMIT_UNAUTHENTICATED: no session → 401 uniform envelope.
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, nil, &fakeToolService{})
	rec := doRequest(surface, http.MethodPost, "/id-a/inspection", "", submitPassBody())
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 401 err = %v", err)
	}
	if env.Error.Code != "unauthorized" {
		t.Errorf("code = %q, want unauthorized", env.Error.Code)
	}
}

func TestInspectionSubmitRejectsUnknownFields(t *testing.T) {
	// Patch 1: DisallowUnknownFields — an unknown body field → 400 invalid
	// JSON, the core never sees the write.
	svc := &fakeToolService{}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/inspection", "tok",
		`{"mode":"pass_fail","result":"pass","notes":"","items":[],"bogus":1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Ungültiges JSON-Format.") {
		t.Errorf("body = %s, want the German invalid-JSON message", rec.Body.String())
	}
	if svc.lastID != "" {
		t.Errorf("core received id = %q, want none (unknown field rejected at decode)", svc.lastID)
	}
}

func TestInspectionSubmitRejectsTrailingContent(t *testing.T) {
	// Patch 1: trailing content after the JSON object → 400 invalid JSON, the
	// core never sees the write.
	svc := &fakeToolService{}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/inspection", "tok",
		`{"mode":"pass_fail","result":"pass","notes":"","items":[]} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Ungültiges JSON-Format.") {
		t.Errorf("body = %s, want the German invalid-JSON message", rec.Body.String())
	}
	if svc.lastID != "" {
		t.Errorf("core received id = %q, want none (trailing content rejected at decode)", svc.lastID)
	}
}

func TestInspectionSubmitNilServiceResult(t *testing.T) {
	// Patch 2: a nil-returning service path must answer a clean 500 (the error
	// mapper's default branch), never panic on the nil dereference.
	svc := &fakeToolService{submitNilResult: true}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/inspection", "tok", submitPassBody())
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 500 err = %v", err)
	}
	if env.Error.Code != "internal_error" {
		t.Errorf("code = %q, want internal_error", env.Error.Code)
	}
}

func TestInspectionSubmitMalformedJSON(t *testing.T) {
	// SUBMIT_BAD_JSON: a non-decodable body → 400 invalid JSON, the core never
	// sees the write.
	svc := &fakeToolService{}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/inspection", "tok", `{not-json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Ungültiges JSON-Format.") {
		t.Errorf("body = %s, want the German invalid-JSON message", rec.Body.String())
	}
	if svc.lastID != "" {
		t.Errorf("core received id = %q, want none (bad JSON rejected at decode)", svc.lastID)
	}
}

func TestInspectionSubmitCompositionMountGate(t *testing.T) {
	// Composition mount gate (Story 5.3): the submit surface is a SIBLING to
	// the dashboard list — a dashboard.view-but-not-inspection.submit caller can
	// READ the Werkzeugliste but the submit answers 403 with NO tool data. The
	// inspection.submit holder passes.
	svc := &fakeToolService{tools: []*toolscore.Tool{toolFixture("id-a", "Bohrmaschine-01")}}

	dashboardOnly := toolsInspectionGateway([]string{toolscore.DashboardViewPermission}, activeAdmin(), svc)
	if rec := doRequest(dashboardOnly, http.MethodGet, "/", "tok", ""); rec.Code != http.StatusOK {
		t.Fatalf("dashboard GET status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	rec := doRequest(dashboardOnly, http.MethodPost, "/id-a/inspection", "tok", submitPassBody())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("submit status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 403 err = %v", err)
	}
	if env.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", env.Error.Code)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "bohrmaschine") || strings.Contains(rec.Body.String(), "id-a") {
		t.Errorf("403 body leaks tool data: %s", rec.Body.String())
	}

	// inspection.submit WITHOUT dashboard.view: the submit 200s (the mount
	// gate), the dashboard list 403s (its own gate).
	submitOnly := toolsInspectionGateway([]string{toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	if rec := doRequest(submitOnly, http.MethodPost, "/id-a/inspection", "tok", submitPassBody()); rec.Code != http.StatusOK {
		t.Fatalf("submit (submit-only) status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := doRequest(submitOnly, http.MethodGet, "/", "tok", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("dashboard GET (submit-only) status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestInspectionSubmitMethodNotAllowed(t *testing.T) {
	// Only POST is registered on the submit surface root: GET answers the
	// uniform 405.
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), &fakeToolService{})
	rec := doRequest(surface, http.MethodGet, "/id-a/inspection", "tok", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 405 err = %v", err)
	}
	if env.Error.Code != "method_not_allowed" {
		t.Errorf("code = %q, want method_not_allowed", env.Error.Code)
	}
}

func TestInspectionSubmitInternalErrorEnvelope(t *testing.T) {
	// DEFAULT branch: an UNEXPECTED service error → 500 internal_error with the
	// German message and NO tool data leak.
	svc := &fakeToolService{submitErr: errors.New("boom")}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/inspection", "tok", submitPassBody())
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 500 err = %v", err)
	}
	if env.Error.Code != "internal_error" || env.Error.Message != "Ein interner Fehler ist aufgetreten." {
		t.Errorf("500 envelope = %+v, want the uniform internal_error", env)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "bohrmaschine") || strings.Contains(rec.Body.String(), "id-a") {
		t.Errorf("500 body leaks tool data: %s", rec.Body.String())
	}
}

func TestInspectionSubmitNilUserUnauthorized(t *testing.T) {
	// The submit handler's nil-user 401 guard (unreachable through the gated
	// composition) — direct callers must never submit without a session.
	surface := nakedInspectionRouter(&fakeToolService{})
	rec := doRequest(surface, http.MethodPost, "/", "tok", submitPassBody())
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 401 err = %v", err)
	}
	if env.Error.Code != "unauthorized" {
		t.Errorf("code = %q, want unauthorized", env.Error.Code)
	}
}

// ============================================================================
// Story 5.6 — OOS not-inspectable block + reinstatement HTTP surface
// (FR-14/FR-15/AD-4/AD-9)
// ============================================================================

func TestInspectionStartOOSBlocked(t *testing.T) {
	// START_OOS: a start on an OOS tool → 403 uniform envelope with the German
	// message (FR-14/AD-4 — the OOS block precedes the qualification gate).
	svc := &fakeToolService{startErr: toolscore.ErrToolOutOfService}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/inspection/start", "tok", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 403 err = %v", err)
	}
	if env.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", env.Error.Code)
	}
	if env.Error.Message != toolscore.MsgToolOutOfService {
		t.Errorf("message = %q, want %q", env.Error.Message, toolscore.MsgToolOutOfService)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "bohrmaschine") {
		t.Errorf("403 body leaks tool data: %s", rec.Body.String())
	}
}

func TestInspectionSubmitOOSBlocked(t *testing.T) {
	// SUBMIT_OOS: a submit on an OOS tool → 403 German, nothing persisted
	// (FR-14/AD-4).
	svc := &fakeToolService{submitErr: toolscore.ErrToolOutOfService}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/inspection", "tok", submitPassBody())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 403 err = %v", err)
	}
	if env.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", env.Error.Code)
	}
	if env.Error.Message != toolscore.MsgToolOutOfService {
		t.Errorf("message = %q, want %q", env.Error.Message, toolscore.MsgToolOutOfService)
	}
}

// nakedReinstatementRouter exposes the ReinstateRoutes router WITHOUT the auth
// gateway, so the handler's nil-user 401 guard is directly testable.
func nakedReinstatementRouter(svc toolports.Service) http.Handler {
	h := NewHandler(svc, &gateValidator{}, &gateResolver{}, discardLogger())
	return h.ReinstateRoutes()
}

func TestReinstateToolOK(t *testing.T) {
	// REINSTATE_OK: a tool.reinstate holder reinstates with a valid reason → 200
	// with the derived not-OOS status (status + next_due) and the German
	// confirmation; the reason reaches the service.
	svc := &fakeToolService{reinstateStatus: toolscore.ToolStatus{Status: toolscore.ToolStatusCodeGreen}}
	svc.reinstateStatus.NextDue = func() *time.Time { t := time.Now().Add(365 * 24 * time.Hour); return &t }()
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.ToolReinstatePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/reinstatement", "tok", `{"reason":"Ersatzteil eingetroffen"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding 200 err = %v", err)
	}
	if body["message"] != toolscore.MsgToolReinstated {
		t.Errorf("message = %+v, want %q", body["message"], toolscore.MsgToolReinstated)
	}
	status, ok := body["status"].(map[string]any)
	if !ok || status["status"] != "green" {
		t.Errorf("status = %+v, want the derived green status", body["status"])
	}
	if nextDue, ok := status["next_due"].(string); !ok || nextDue == "" {
		t.Errorf("next_due = %+v, want a set RFC3339 timestamp", status["next_due"])
	}
	if svc.lastReinstateID != "id-a" || svc.lastReason != "Ersatzteil eingetroffen" {
		t.Errorf("service args = id %q reason %q, want id-a / Ersatzteil eingetroffen", svc.lastReinstateID, svc.lastReason)
	}
}

func TestReinstateToolEmptyReason(t *testing.T) {
	// REINSTATE_EMPTY: an empty/whitespace reason → 400 German (the reason is
	// MANDATORY, FR-15/AD-9). The service is never reached with an empty value
	// (the handler passes the raw body; the CORE validates — here we verify the
	// 400 mapping of the sentinel the core returns).
	svc := &fakeToolService{reinstateErr: &toolscore.InvalidInspectionError{Message: toolscore.MsgReinstatementReasonRequired}}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.ToolReinstatePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/reinstatement", "tok", `{"reason":"   "}`)
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
	if env.Error.Message != toolscore.MsgReinstatementReasonRequired {
		t.Errorf("message = %q, want %q", env.Error.Message, toolscore.MsgReinstatementReasonRequired)
	}
}

func TestReinstateToolTooLongReason(t *testing.T) {
	// REINSTATE_LONG: a reason over 2000 runes → 400 German (the core's
	// sentinel; the handler maps the same InvalidInspectionError).
	svc := &fakeToolService{reinstateErr: &toolscore.InvalidInspectionError{Message: toolscore.MsgReinstatementReasonTooLong}}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.ToolReinstatePermission}, activeAdmin(), svc)
	long := strings.Repeat("x", toolscore.MaxReinstatementReasonRunes+1)
	rec := doRequest(surface, http.MethodPost, "/id-a/reinstatement", "tok", `{"reason":"`+long+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 400 err = %v", err)
	}
	if env.Error.Message != toolscore.MsgReinstatementReasonTooLong {
		t.Errorf("message = %q, want %q", env.Error.Message, toolscore.MsgReinstatementReasonTooLong)
	}
}

func TestReinstateToolGated(t *testing.T) {
	// REINSTATE_GATED: a caller without tool.reinstate (e.g. an
	// inspection.submit-only holder) → 403 uniform, no write. The mount gate is
	// the tool.reinstate RequirePermission on the reinstatement surface.
	svc := &fakeToolService{}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/reinstatement", "tok", `{"reason":"Ersatzteil"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 403 err = %v", err)
	}
	if env.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", env.Error.Code)
	}
}

func TestReinstateToolToolNotFound(t *testing.T) {
	// REINSTATE_ARCHIVED / REINSTATE_UNKNOWN: an unknown/archived tool → 404
	// uniform envelope.
	svc := &fakeToolService{reinstateErr: toolscore.ErrToolNotFound}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.ToolReinstatePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-missing/reinstatement", "tok", `{"reason":"Ersatzteil"}`)
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
	if env.Error.Message != toolscore.MsgToolNotFound {
		t.Errorf("message = %q, want %q", env.Error.Message, toolscore.MsgToolNotFound)
	}
}

func TestReinstateToolUnauthenticated(t *testing.T) {
	// REINSTATE_UNAUTHENTICATED: no session → 401 uniform envelope.
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.ToolReinstatePermission}, nil, &fakeToolService{})
	rec := doRequest(surface, http.MethodPost, "/id-a/reinstatement", "", `{"reason":"Ersatzteil"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestReinstateToolCompositionMountGate(t *testing.T) {
	// Composition mount gate (Story 5.6): the reinstatement surface is a SIBLING
	// to the inspection surface — a dashboard.view + inspection.submit holder
	// WITHOUT tool.reinstate can start/submit but the reinstatement answers the
	// generic 403 with NO tool data (AD-6, one permission per surface). The
	// tool.reinstate holder passes.
	svc := &fakeToolService{reinstateStatus: toolscore.ToolStatus{Status: toolscore.ToolStatusCodeGreen}}

	// inspection.submit holder WITHOUT tool.reinstate: start 200, reinstate 403.
	noReinstate := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionSubmitPermission}, activeAdmin(), svc)
	if rec := doRequest(noReinstate, http.MethodPost, "/id-a/inspection/start", "tok", ""); rec.Code != http.StatusOK {
		t.Fatalf("start status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	rec := doRequest(noReinstate, http.MethodPost, "/id-a/reinstatement", "tok", `{"reason":"Ersatzteil"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("reinstate (no tool.reinstate) status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// tool.reinstate holder WITHOUT inspection.submit: reinstate 200, start 403.
	reinstateOnly := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.ToolReinstatePermission}, activeAdmin(), svc)
	if rec := doRequest(reinstateOnly, http.MethodPost, "/id-a/reinstatement", "tok", `{"reason":"Ersatzteil"}`); rec.Code != http.StatusOK {
		t.Fatalf("reinstate (holder) status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := doRequest(reinstateOnly, http.MethodPost, "/id-a/inspection/start", "tok", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("start (no inspection.submit) status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestReinstateToolRejectsUnknownFields(t *testing.T) {
	// Decoder hardening (the submit pattern): an unknown field answers the
	// uniform 400, never a partial parse.
	svc := &fakeToolService{}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.ToolReinstatePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/reinstatement", "tok", `{"reason":"Ersatzteil","unknown":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestReinstateToolRejectsTrailingContent(t *testing.T) {
	// Decoder hardening: trailing JSON after the object answers the uniform 400.
	svc := &fakeToolService{}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.ToolReinstatePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/reinstatement", "tok", `{"reason":"Ersatzteil"}{"x":1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestReinstateToolNilServiceResult(t *testing.T) {
	// A nil-returning service path (a wiring defect) → clean 500, never panic.
	svc := &fakeToolService{reinstateNil: true}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.ToolReinstatePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/reinstatement", "tok", `{"reason":"Ersatzteil"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestReinstateToolMethodNotAllowed(t *testing.T) {
	// Only POST is registered on the reinstatement surface root: GET answers
	// the uniform 405.
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.ToolReinstatePermission}, activeAdmin(), &fakeToolService{})
	rec := doRequest(surface, http.MethodGet, "/id-a/reinstatement", "tok", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestReinstateToolNilUserUnauthorized(t *testing.T) {
	// The handler's nil-user 401 guard (unreachable through the gated
	// composition) — direct callers must never reinstate without a session.
	surface := nakedReinstatementRouter(&fakeToolService{})
	rec := doRequest(surface, http.MethodPost, "/", "tok", `{"reason":"Ersatzteil"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 401 err = %v", err)
	}
	if env.Error.Code != "unauthorized" {
		t.Errorf("code = %q, want unauthorized", env.Error.Code)
	}
}

func TestReinstateToolNotOOS(t *testing.T) {
	// REINSTATE_NOT_OOS (Story 5.6 patch): a reinstatement on a tool that is NOT
	// out of service → 400 invalid_request with the German message (FR-15: a
	// reinstatement is only meaningful as the SOLE exit from OOS).
	svc := &fakeToolService{reinstateErr: &toolscore.InvalidInspectionError{Message: toolscore.MsgToolNotOutOfService}}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.ToolReinstatePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/reinstatement", "tok", `{"reason":"Ersatzteil"}`)
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
	if env.Error.Message != toolscore.MsgToolNotOutOfService {
		t.Errorf("message = %q, want %q", env.Error.Message, toolscore.MsgToolNotOutOfService)
	}
}

func TestReinstateToolInternalErrorEnvelope(t *testing.T) {
	// DEFAULT branch of mapReinstatementError: an UNEXPECTED (non-sentinel)
	// service error → 500 internal_error with the German message and NO tool
	// data leak.
	svc := &fakeToolService{reinstateErr: errors.New("boom")}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.ToolReinstatePermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodPost, "/id-a/reinstatement", "tok", `{"reason":"Ersatzteil"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 500 err = %v", err)
	}
	if env.Error.Code != "internal_error" || env.Error.Message != "Ein interner Fehler ist aufgetreten." {
		t.Errorf("500 envelope = %+v, want the uniform internal_error", env)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "bohrmaschine") || strings.Contains(rec.Body.String(), "id-a") {
		t.Errorf("500 body leaks tool data: %s", rec.Body.String())
	}
}

func TestReinstateToolClientAbort(t *testing.T) {
	// The client-abort guard in mapReinstatementError: a canceled request has no
	// one to answer — the handler returns WITHOUT writing (a default-branch
	// error must not attempt a response to a dead client). A user is injected so
	// the handler passes its nil-user guard and reaches the error mapper.
	svc := &fakeToolService{reinstateErr: errors.New("boom")}
	h := NewHandler(svc, &gateValidator{}, &gateResolver{}, discardLogger())
	rec := httptest.NewRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/id-a/reinstatement", strings.NewReader(`{"reason":"Ersatzteil"}`))
	req = req.WithContext(auth.WithUser(ctx, activeAdmin().User))
	h.ReinstateTool(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want no response written on a client abort (the recorder defaults to 200)", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty (the abort guard must not write)", rec.Body.String())
	}
}

// ============================================================================
// Tool history (Story 6.3, FR-18/AD-6): the GET /api/v1/tools/{id}/history
// surface behind inspection.history.view.
// ============================================================================

// historyFixture is a full tool history (Story 6.3): a newer checklist
// inspection WITH its per-item snapshot results + an older pass_fail + one
// reinstatement, by the known inspector.
func historyFixture() *toolscore.ToolHistory {
	newer := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	older := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	created := time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC)
	return &toolscore.ToolHistory{
		Inspections: []*toolscore.ToolHistoryInspection{
			{
				ID: "insp-new", InspectorID: "u-admin", InspectorName: "Anna Muster",
				Mode: toolscore.InspectionModeChecklist, OverallResult: toolscore.InspectionResultFail,
				Notes: "Bohrfutter locker", SubmittedAt: newer,
				Items: []toolscore.InspectionItem{
					{ID: "it-1", InspectionID: "insp-new", ItemID: "item-1", Label: "Kabel", Position: 0, Result: toolscore.InspectionResultPass},
					{ID: "it-2", InspectionID: "insp-new", ItemID: "item-2", Label: "Bohrfutter", Position: 1, Result: toolscore.InspectionResultFail},
				},
			},
			{
				ID: "insp-old", InspectorID: "u-admin", InspectorName: "Anna Muster",
				Mode: toolscore.InspectionModePassFail, OverallResult: toolscore.InspectionResultPass,
				Notes: "Alles ok", SubmittedAt: older,
				Items: []toolscore.InspectionItem{},
			},
		},
		Reinstatements: []*toolscore.ToolHistoryReinstatement{
			{ID: "rein-1", ActorID: "u-admin", ActorName: "Anna Muster", Reason: "Ersatzteil eingetroffen", CreatedAt: created},
		},
	}
}

func TestToolHistoryOK(t *testing.T) {
	// HIST_OK: an inspection.history.view holder GETs the tool's history — the
	// DTO carries BOTH newest-first lists with the inspector name, timestamp,
	// outcome, notes, mode and the per-checklist-item results; reinstatements
	// carry the actor + reason. The {id} path param reaches the service.
	svc := &fakeToolService{history: historyFixture()}
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionHistoryViewPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodGet, "/id-a/history", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Inspections    []map[string]any `json:"inspections"`
		Reinstatements []map[string]any `json:"reinstatements"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if len(body.Inspections) != 2 || len(body.Reinstatements) != 1 {
		t.Fatalf("history = %d inspections / %d reinstatements, want 2 / 1", len(body.Inspections), len(body.Reinstatements))
	}
	insp := body.Inspections[0]
	if insp["id"] != "insp-new" || insp["inspector_id"] != "u-admin" || insp["inspector_name"] != "Anna Muster" {
		t.Errorf("inspection[0] identity = %+v", insp)
	}
	if insp["mode"] != "checklist" || insp["overall_result"] != "fail" || insp["notes"] != "Bohrfutter locker" {
		t.Errorf("inspection[0] record = %+v", insp)
	}
	if insp["submitted_at"] != "2026-09-15T10:00:00Z" {
		t.Errorf("submitted_at = %v, want the RFC3339 UTC timestamp", insp["submitted_at"])
	}
	items, ok := insp["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("items = %+v, want the two snapshot items", insp["items"])
	}
	if item, ok := items[0].(map[string]any); !ok || item["item_id"] != "item-1" || item["label"] != "Kabel" || item["position"] != float64(0) || item["result"] != "pass" {
		t.Errorf("items[0] = %+v, want the Kabel pass snapshot", items[0])
	}
	if item, ok := items[1].(map[string]any); !ok || item["label"] != "Bohrfutter" || item["result"] != "fail" {
		t.Errorf("items[1] = %+v, want the Bohrfutter fail snapshot", items[1])
	}
	// Newest first: the second inspection is the older pass_fail with an EMPTY
	// items array (never null).
	if insp2 := body.Inspections[1]; insp2["id"] != "insp-old" || insp2["mode"] != "pass_fail" {
		t.Errorf("inspection[1] = %+v, want the older pass_fail", insp2)
	}
	if items2, ok := body.Inspections[1]["items"].([]any); !ok || len(items2) != 0 {
		t.Errorf("inspection[1] items = %+v, want an empty array", body.Inspections[1]["items"])
	}
	rein := body.Reinstatements[0]
	if rein["actor_id"] != "u-admin" || rein["actor_name"] != "Anna Muster" || rein["reason"] != "Ersatzteil eingetroffen" || rein["created_at"] != "2026-09-16T08:00:00Z" {
		t.Errorf("reinstatement = %+v", rein)
	}
	if svc.lastHistoryID != "id-a" {
		t.Errorf("service received tool id = %q, want id-a", svc.lastHistoryID)
	}
}

func TestToolHistoryEmpty(t *testing.T) {
	// HIST_EMPTY: a tool with no records answers 200 with EMPTY arrays — never
	// null, never a 404.
	surface := toolsInspectionGateway([]string{toolscore.DashboardViewPermission, toolscore.InspectionHistoryViewPermission}, activeAdmin(), &fakeToolService{})
	rec := doRequest(surface, http.MethodGet, "/id-a/history", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if insp, ok := body["inspections"].([]any); !ok || len(insp) != 0 {
		t.Errorf("inspections = %+v, want an empty array", body["inspections"])
	}
	if rein, ok := body["reinstatements"].([]any); !ok || len(rein) != 0 {
		t.Errorf("reinstatements = %+v, want an empty array", body["reinstatements"])
	}
}

func TestToolHistoryCompositionMountGate(t *testing.T) {
	// HIST_GATED (Story 6.3, AD-6): the history is its OWN surface — a
	// dashboard.view-but-not-inspection.history.view caller still READS the
	// Werkzeugliste but 403s on the history with NO data exposed; the history
	// holder reaches it. The reverse is also pinned: a history-only holder
	// reaches the history but is denied the dashboard list.
	svc := &fakeToolService{tools: []*toolscore.Tool{toolFixture("id-a", "Bohrmaschine-01")}, history: historyFixture()}

	dashboardOnly := toolsInspectionGateway([]string{toolscore.DashboardViewPermission}, activeAdmin(), svc)
	if rec := doRequest(dashboardOnly, http.MethodGet, "/", "tok", ""); rec.Code != http.StatusOK {
		t.Fatalf("dashboard GET status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	rec := doRequest(dashboardOnly, http.MethodGet, "/id-a/history", "tok", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("history status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 403 err = %v", err)
	}
	if env.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", env.Error.Code)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "bohrmaschine") || strings.Contains(rec.Body.String(), "Anna Muster") || strings.Contains(rec.Body.String(), "id-a") {
		t.Errorf("403 body leaks history/tool data: %s", rec.Body.String())
	}

	historyOnly := toolsInspectionGateway([]string{toolscore.InspectionHistoryViewPermission}, activeAdmin(), svc)
	if rec := doRequest(historyOnly, http.MethodGet, "/id-a/history", "tok", ""); rec.Code != http.StatusOK {
		t.Fatalf("history (holder) status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := doRequest(historyOnly, http.MethodGet, "/", "tok", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("dashboard (history-only) status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestToolHistoryToolNotFound(t *testing.T) {
	// HIST_UNKNOWN / HIST_ARCHIVED: the service's ErrToolNotFound → the uniform
	// 404 with the German message.
	svc := &fakeToolService{historyErr: toolscore.ErrToolNotFound}
	surface := toolsInspectionGateway([]string{toolscore.InspectionHistoryViewPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodGet, "/id-missing/history", "tok", "")
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
	if env.Error.Message != toolscore.MsgToolNotFound {
		t.Errorf("message = %q, want %q", env.Error.Message, toolscore.MsgToolNotFound)
	}
}

func TestToolHistoryForbiddenEnvelope(t *testing.T) {
	// The core re-check (defense-in-depth, AD-6): a caller whose live set lost
	// inspection.history.view between the gateway and the core answers the
	// generic no-hint 403, with no history data.
	svc := &fakeToolService{historyErr: toolscore.ErrForbidden}
	surface := toolsInspectionGateway([]string{toolscore.InspectionHistoryViewPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodGet, "/id-a/history", "tok", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 403 err = %v", err)
	}
	if env.Error.Message != "Keine Berechtigung." {
		t.Errorf("message = %q, want the generic no-hint forbidden microcopy", env.Error.Message)
	}
	if strings.Contains(rec.Body.String(), "Anna Muster") {
		t.Errorf("403 body leaks history data: %s", rec.Body.String())
	}
}

func TestToolHistoryUnauthenticated(t *testing.T) {
	// No session → 401 uniform envelope.
	surface := toolsInspectionGateway([]string{toolscore.InspectionHistoryViewPermission}, nil, &fakeToolService{})
	rec := doRequest(surface, http.MethodGet, "/id-a/history", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 401 err = %v", err)
	}
	if env.Error.Code != "unauthorized" {
		t.Errorf("code = %q, want unauthorized", env.Error.Code)
	}
}

func TestToolHistoryMethodNotAllowedEnvelope(t *testing.T) {
	// Only GET is registered on the history surface: POST answers the uniform
	// 405 (read-only surface, no write path).
	surface := toolsInspectionGateway([]string{toolscore.InspectionHistoryViewPermission}, activeAdmin(), &fakeToolService{})
	rec := doRequest(surface, http.MethodPost, "/id-a/history", "tok", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 405 err = %v", err)
	}
	if env.Error.Code != "method_not_allowed" {
		t.Errorf("code = %q, want method_not_allowed", env.Error.Code)
	}
}

func TestToolHistoryInternalErrorEnvelope(t *testing.T) {
	// DEFAULT branch (mapInspectionError): an UNEXPECTED service error → 500
	// internal_error uniform envelope with the German message, and NO history
	// data leak — the raw error never reaches the client.
	svc := &fakeToolService{historyErr: errors.New("boom")}
	surface := toolsInspectionGateway([]string{toolscore.InspectionHistoryViewPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodGet, "/id-a/history", "tok", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 500 err = %v", err)
	}
	if env.Error.Code != "internal_error" {
		t.Errorf("code = %q, want internal_error", env.Error.Code)
	}
	if env.Error.Message != "Ein interner Fehler ist aufgetreten." {
		t.Errorf("message = %q, want the German internal-error microcopy", env.Error.Message)
	}
	if strings.Contains(rec.Body.String(), "Anna Muster") {
		t.Errorf("500 body leaks history data: %s", rec.Body.String())
	}
}

func TestToolHistoryNilUserUnauthorized(t *testing.T) {
	// Handler-level defense-in-depth: no authenticated user in the context → 401
	// (unreachable through the gated composition but still a handler guard).
	// The NAKED router owns the route at "/" (the mount strips the /{id}/history
	// prefix in production).
	surface := nakedHistoryRouter(&fakeToolService{})
	rec := doRequest(surface, http.MethodGet, "/", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestToolHistoryNilServiceResult(t *testing.T) {
	// A nil-returning service path (a wiring defect) → the clean 500, never a
	// panic.
	svc := &fakeToolService{historyNil: true}
	surface := toolsInspectionGateway([]string{toolscore.InspectionHistoryViewPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodGet, "/id-a/history", "tok", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 500 err = %v", err)
	}
	if env.Error.Code != "internal_error" {
		t.Errorf("code = %q, want internal_error", env.Error.Code)
	}
}

func TestToolHistoryClientAbort(t *testing.T) {
	// The client-abort guard in mapInspectionError: a canceled request has no
	// one to answer — the handler returns WITHOUT writing. A user is injected so
	// the handler passes its nil-user guard and reaches the error mapper.
	svc := &fakeToolService{historyErr: errors.New("boom")}
	h := NewHandler(svc, &gateValidator{}, &gateResolver{}, discardLogger())
	rec := httptest.NewRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/id-a/history", nil)
	req = req.WithContext(auth.WithUser(ctx, activeAdmin().User))
	h.ListInspectionHistory(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want no response written on a client abort (the recorder defaults to 200)", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty (the abort guard must not write)", rec.Body.String())
	}
}
