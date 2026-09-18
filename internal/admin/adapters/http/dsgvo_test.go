package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dsgvocore "github.com/saskia-peters/gear/internal/dsgvo/core"
	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	toolscore "github.com/saskia-peters/gear/internal/tools/core"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

// fakeDsgvoService is an in-memory DsgvoService for the handler tests.
type fakeDsgvoService struct {
	report *dsgvocore.AccessReport
	err    error
	calls  int
	last   string
}

func (f *fakeDsgvoService) GenerateAccessReport(_ context.Context, _, targetUserID string) (*dsgvocore.AccessReport, error) {
	f.calls++
	f.last = targetUserID
	if f.err != nil {
		return nil, f.err
	}
	return f.report, nil
}

// dsgvoGateway wraps the REAL DsgvoRoutes() behind the same any-of DSGVO gate
// the composition root uses ([dsgvo.access_report, dsgvo.delete], AD-6), with a
// fake session validator + permission resolver.
func dsgvoGateway(perms []string, session *usercore.Session, svc DsgvoService) http.Handler {
	h := NewDsgvoHandler(svc, discardLogger())
	return auth.RequireAnyPermission(
		&gateValidator{session: session},
		&gateResolver{perms: perms},
		[]string{dsgvocore.AccessReportPermission, dsgvocore.DeletePermission},
		"dsgvo access denied", discardLogger(),
	)(h.DsgvoRoutes())
}

func doDsgvoRequest(h http.Handler, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func sampleDsgvoReport() *dsgvocore.AccessReport {
	return &dsgvocore.AccessReport{
		User: &usercore.UserDataExport{
			Profile: usercore.UserExportProfile{ID: "u-target", Email: "target@gear.local"},
		},
		Tools: &toolscore.UserInspectionDataExport{
			Inspections: []toolscore.InspectionExport{{ID: "insp-1"}},
			Summary:     []toolscore.ToolExportSummary{},
		},
		GeneratedAt: time.Now().UTC(),
	}
}

func TestDsgvoReportGetAsHolder(t *testing.T) {
	// REPORT_OK: a `dsgvo.access_report` holder → 200 with the assembled report
	// JSON AND an attachment-style Content-Disposition (the download contract).
	svc := &fakeDsgvoService{report: sampleDsgvoReport()}
	surface := dsgvoGateway([]string{dsgvocore.AccessReportPermission}, activeAdmin(), svc)
	rec := doDsgvoRequest(surface, http.MethodGet, "/reports/u-target", "tok")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, "dsgvo-datenauskunft.json") {
		t.Errorf("Content-Disposition = %q, want an attachment download of dsgvo-datenauskunft.json", cd)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store (gated personal data)", cc)
	}
	if svc.calls != 1 || svc.last != "u-target" {
		t.Errorf("service calls = %d (last %q), want one call with u-target", svc.calls, svc.last)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding report body err = %v", err)
	}
	if body["user"] == nil || body["tools"] == nil || body["generated_at"] == nil {
		t.Errorf("report body = %+v, want the user/tools/generated_at sections", body)
	}
}

func TestDsgvoReportGetForbidden(t *testing.T) {
	// REPORT_GATED: an authenticated caller without a DSGVO code → uniform 403,
	// the service is NEVER reached and no personal data is exposed (AD-6).
	svc := &fakeDsgvoService{report: sampleDsgvoReport()}
	surface := dsgvoGateway([]string{"dashboard.view"}, activeAdmin(), svc)
	rec := doDsgvoRequest(surface, http.MethodGet, "/reports/u-target", "tok")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.calls != 0 {
		t.Errorf("service reached for a non-holder: calls=%d", svc.calls)
	}
	if strings.Contains(rec.Body.String(), "u-target") || strings.Contains(rec.Body.String(), "target@gear.local") {
		t.Errorf("403 body leaks report data: %s", rec.Body.String())
	}
}

func TestDsgvoReportGetUnauthenticated(t *testing.T) {
	// REPORT_401: no bearer token → uniform 401.
	surface := dsgvoGateway([]string{dsgvocore.AccessReportPermission}, nil, &fakeDsgvoService{})
	rec := doDsgvoRequest(surface, http.MethodGet, "/reports/u-target", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestDsgvoReportGetUnknownUser(t *testing.T) {
	// REPORT_UNKNOWN: the orchestrator's ErrAdminUserNotFound maps to the
	// German 404.
	svc := &fakeDsgvoService{err: usercore.ErrAdminUserNotFound}
	surface := dsgvoGateway([]string{dsgvocore.AccessReportPermission}, activeAdmin(), svc)
	rec := doDsgvoRequest(surface, http.MethodGet, "/reports/u-gibtsnicht", "tok")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "nicht gefunden") {
		t.Errorf("404 body lacks the German message: %s", rec.Body.String())
	}
}

func TestDsgvoReportGetEmptyUserID(t *testing.T) {
	// An empty/whitespace target id in the URL → the German 400
	// (invalid_request), and the service is NEVER reached.
	svc := &fakeDsgvoService{report: sampleDsgvoReport()}
	surface := dsgvoGateway([]string{dsgvocore.AccessReportPermission}, activeAdmin(), svc)
	rec := doDsgvoRequest(surface, http.MethodGet, "/reports/%20", "tok")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.calls != 0 {
		t.Errorf("service reached for an empty target id: calls=%d", svc.calls)
	}
	if !strings.Contains(rec.Body.String(), `"code":"invalid_request"`) {
		t.Errorf("400 body lacks the uniform envelope: %s", rec.Body.String())
	}
}

func TestDsgvoReportGetInternalError(t *testing.T) {
	// A generic (non-sentinel) service failure → the uniform 500 with the
	// German message and NO leaked error detail (the log carries the cause).
	svc := &fakeDsgvoService{err: errors.New("postgres exploded: connection-refused-secret")}
	surface := dsgvoGateway([]string{dsgvocore.AccessReportPermission}, activeAdmin(), svc)
	rec := doDsgvoRequest(surface, http.MethodGet, "/reports/u-target", "tok")
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
		t.Errorf("message = %q, want the German 500 message", env.Error.Message)
	}
	if strings.Contains(rec.Body.String(), "postgres exploded") || strings.Contains(rec.Body.String(), "connection-refused-secret") {
		t.Errorf("500 body leaks the underlying error detail: %s", rec.Body.String())
	}
}

func TestDsgvoReportNilResultGuard(t *testing.T) {
	// A (nil, nil) service return (a wiring/port defect) must answer the
	// uniform 500 via the nil-report guard — never a panic, never a null body.
	svc := &fakeDsgvoService{report: nil}
	surface := dsgvoGateway([]string{dsgvocore.AccessReportPermission}, activeAdmin(), svc)
	rec := doDsgvoRequest(surface, http.MethodGet, "/reports/u-target", "tok")
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

func TestDsgvoReportNotFoundEnvelope(t *testing.T) {
	// An unknown sub-path answers the uniform 404 `not_found` envelope (never a
	// plain-text chi body).
	surface := dsgvoGateway([]string{dsgvocore.AccessReportPermission}, activeAdmin(), &fakeDsgvoService{})
	rec := doDsgvoRequest(surface, http.MethodGet, "/gibtsnicht", "tok")
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

func TestDsgvoReportMethodNotAllowedEnvelope(t *testing.T) {
	// A wrong method on an existing route answers the uniform 405
	// `method_not_allowed` envelope (never a plain-text chi body).
	surface := dsgvoGateway([]string{dsgvocore.AccessReportPermission}, activeAdmin(), &fakeDsgvoService{})
	rec := doDsgvoRequest(surface, http.MethodDelete, "/reports/u-target", "tok")
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

// --- real-orchestrator audit round-trip (NFR-O2) ---------------------------

// dsgvoAuditWriter records InsertAuditEvent calls.
type dsgvoAuditWriter struct {
	events []dsgvoAuditRecord
}

type dsgvoAuditRecord struct {
	actorID   string
	operation string
	detail    string
	severity  string
}

func (a *dsgvoAuditWriter) InsertAuditEvent(_ context.Context, actorID, operation, detail, severity string) error {
	a.events = append(a.events, dsgvoAuditRecord{actorID: actorID, operation: operation, detail: detail, severity: severity})
	return nil
}

// dsgvoRealUserPort is a userports.DSGVOExportPort over a canned export.
type dsgvoRealUserPort struct {
	export *usercore.UserDataExport
}

func (p *dsgvoRealUserPort) ExportUserData(_ context.Context, _ string) (*usercore.UserDataExport, error) {
	return p.export, nil
}

// dsgvoRealToolPort is a toolports.DSGVOInspectionExportPort over a canned export.
type dsgvoRealToolPort struct {
	export *toolscore.UserInspectionDataExport
}

func (p *dsgvoRealToolPort) ExportUserInspectionData(_ context.Context, _ string) (*toolscore.UserInspectionDataExport, error) {
	return p.export, nil
}

// TestDsgvoReportAuditsGeneration wires the REAL orchestrator (fake export
// ports + fake audit) through the REAL gateway, so the audit row (NFR-O2)
// round-trips the HTTP path: a 200 report generation writes exactly one
// dsgvo.access_report audit with actor + target user id.
func TestDsgvoReportAuditsGeneration(t *testing.T) {
	audit := &dsgvoAuditWriter{}
	orchestrator := dsgvocore.NewService(
		&dsgvoRealUserPort{export: &usercore.UserDataExport{
			Profile: usercore.UserExportProfile{ID: "u-target", Email: "target@gear.local"},
		}},
		&dsgvoRealToolPort{export: &toolscore.UserInspectionDataExport{}},
		&gateResolver{perms: []string{dsgvocore.AccessReportPermission}},
		audit,
		nil,
	)

	surface := dsgvoGateway([]string{dsgvocore.AccessReportPermission}, activeAdmin(), orchestrator)
	rec := doDsgvoRequest(surface, http.MethodGet, "/reports/u-target", "tok")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want exactly one", len(audit.events))
	}
	ev := audit.events[0]
	if ev.actorID != "u-admin" || ev.operation != dsgvocore.AuditOperationAccessReport || ev.severity != "normal" {
		t.Errorf("audit event = %+v, want actor u-admin / operation dsgvo.access_report / severity normal", ev)
	}
	if !strings.Contains(ev.detail, "u-target") {
		t.Errorf("audit detail = %q, want the target user id", ev.detail)
	}
}