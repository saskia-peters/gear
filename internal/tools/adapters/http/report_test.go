package http

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/saskia-peters/gear/internal/platform/auth"
	toolscore "github.com/saskia-peters/gear/internal/tools/core"
	toolports "github.com/saskia-peters/gear/internal/tools/ports"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

// reportGateway wraps the REAL ReportRoutes() behind the same RequirePermission
// gate the composition root uses (report.export, Story 6.2, AD-6), with a fake
// session validator + permission resolver.
func reportGateway(perms []string, session *usercore.Session, svc toolports.Service) http.Handler {
	h := NewHandler(svc, &gateValidator{session: session}, &gateResolver{perms: perms}, discardLogger())
	return auth.RequirePermission(
		&gateValidator{session: session},
		&gateResolver{perms: perms},
		toolscore.ReportExportPermission,
	)(h.ReportRoutes())
}

// nakedReportRouter exposes the ReportRoutes router WITHOUT the auth gateway, so
// the handler's own guards (the nil-user 401) are directly testable.
func nakedReportRouter(svc toolports.Service) http.Handler {
	h := NewHandler(svc, &gateValidator{}, &gateResolver{}, discardLogger())
	return h.ReportRoutes()
}

// reportRowFixture builds one report row (Story 6.2) with a derived status +
// latest-inspection inputs. neverInspected yields a nil LastInspectedAt (the
// PDF renders "–").
func reportRowFixture(id, name, status string, lastInspectedAt *time.Time, inspector string) *toolscore.ReportRow {
	row := &toolscore.ReportRow{
		Tool: toolscore.DashboardTool{
			Tool: toolscore.Tool{
				ID: id, Name: name, ToolTypeID: "id-t1", ToolTypeName: "Bohrmaschine",
				InventoryNumber: "GEAR00000X",
			},
			Status: toolscore.ToolStatus{Status: toolscore.ToolStatusCode(status)},
		},
		LastInspectorName: inspector,
	}
	if lastInspectedAt != nil {
		t := *lastInspectedAt
		row.LastInspectedAt = &t
	}
	return row
}

func TestReportPdfOk(t *testing.T) {
	// REPORT_OK: a report.export holder gets 200 application/pdf with a
	// NON-EMPTY body carrying the %PDF magic — the server rendered the rows.
	inspected := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	svc := &fakeToolService{reportRows: []*toolscore.ReportRow{
		reportRowFixture("id-a", "Bohrmaschine-01", "green", &inspected, "Anna Muster"),
		reportRowFixture("id-b", "Bohrmaschine-02", "oos", nil, ""),
	}}
	surface := reportGateway([]string{toolscore.ReportExportPermission}, activeAdmin(), svc)

	rec := doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type = %q, want application/pdf", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store (the report is a gated artifact)", cc)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("body empty, want the rendered PDF bytes")
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF")) {
		t.Errorf("body does not start with the %%PDF magic: %q", rec.Body.String()[:min(len(rec.Body.String()), 32)])
	}
	if len(svc.reportRows) != 2 {
		t.Fatalf("report rows served = %d, want 2", len(svc.reportRows))
	}
	// No filter param → the empty filter slice reaches the service ("Alle").
	if len(svc.lastReportFilters) != 0 {
		t.Errorf("service filter codes = %v, want empty (absent status param)", svc.lastReportFilters)
	}
}

func TestReportPdfFiltered(t *testing.T) {
	// REPORT_FILTERED: the active status filters travel as ?status=… and reach
	// the service verbatim (the server re-derives and filters — never trusts
	// the client).
	inspected := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	svc := &fakeToolService{reportRows: []*toolscore.ReportRow{
		reportRowFixture("id-a", "Bohrmaschine-01", "green", &inspected, "Anna Muster"),
	}}
	surface := reportGateway([]string{toolscore.ReportExportPermission}, activeAdmin(), svc)

	rec := doRequest(surface, http.MethodGet, "/?status=green,oos", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(svc.lastReportFilters) != 2 || svc.lastReportFilters[0] != "green" || svc.lastReportFilters[1] != "oos" {
		t.Errorf("service filter codes = %v, want [green oos]", svc.lastReportFilters)
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF")) {
		t.Error("body does not start with the %PDF magic")
	}
}

func TestReportPdfNeverInspectedRendersDash(t *testing.T) {
	// REPORT_NEVER: a never-inspected row (nil LastInspectedAt) still renders —
	// the PDF must not crash and must contain the "–" cell. Asserted structurally
	// via the 200 + PDF magic (the exact glyph is not golden-tested).
	svc := &fakeToolService{reportRows: []*toolscore.ReportRow{
		reportRowFixture("id-never", "Nie-geprüft", "red", nil, ""),
	}}
	surface := reportGateway([]string{toolscore.ReportExportPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF")) {
		t.Error("body does not start with the %PDF magic")
	}
}

func TestReportPdfGate(t *testing.T) {
	// REPORT_GATED (AD-6): a caller without report.export answers the uniform
	// 403 with NO PDF bytes and no tool data.
	surface := reportGateway([]string{toolscore.DashboardViewPermission}, activeAdmin(), &fakeToolService{})

	rec := doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF")) {
		t.Error("403 body leaks PDF bytes")
	}
	if strings.Contains(rec.Body.String(), "Bohrmaschine") || strings.Contains(rec.Body.String(), "Statusbericht") {
		t.Errorf("403 body leaks report data: %s", rec.Body.String())
	}
}

func TestReportPdfBadStatus(t *testing.T) {
	// REPORT_BAD_CODE: a status filter value that is NOT a clean comma-separated
	// list of green|orange|red|oos answers the German 400 envelope, never a PDF.
	// Covered: an unknown code, a code mixed with an unknown one, an empty
	// element between commas, an EMPTY value (present-but-empty ≠ absent) and a
	// whitespace-only value.
	svc := &fakeToolService{reportRows: []*toolscore.ReportRow{}}
	surface := reportGateway([]string{toolscore.ReportExportPermission}, activeAdmin(), svc)

	for _, query := range []string{
		"?status=grün",
		"?status=green,neon",
		"?status=green,,oos",
		"?status=",
		"?status=%20",
		"?status=green,",
	} {
		rec := doRequest(surface, http.MethodGet, "/"+query, "tok", "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body %s)", query, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"code":"invalid_request"`) {
			t.Errorf("%s: body lacks the uniform 400 envelope: %s", query, rec.Body.String())
		}
		if bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF")) {
			t.Errorf("%s: 400 body leaks PDF bytes", query)
		}
	}
	// The service must never have been called for a malformed filter (a real
	// call counter — not a len() check on a pre-set fixture).
	if svc.reportCalls != 0 {
		t.Errorf("service was called %d times despite the malformed status filter", svc.reportCalls)
	}
}

func TestReportPdfFilterDedupesCodes(t *testing.T) {
	// ?status=green,green is VALID (both codes are in the accepted set) but the
	// repeated code is deduplicated so the filter caption never duplicates a
	// label — the service receives ["green"].
	svc := &fakeToolService{reportRows: []*toolscore.ReportRow{}}
	surface := reportGateway([]string{toolscore.ReportExportPermission}, activeAdmin(), svc)

	rec := doRequest(surface, http.MethodGet, "/?status=green,green,oos,green", "tok", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(svc.lastReportFilters) != 2 || svc.lastReportFilters[0] != "green" || svc.lastReportFilters[1] != "oos" {
		t.Errorf("deduped filter codes = %v, want [green oos]", svc.lastReportFilters)
	}
	if bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF")) != true {
		t.Error("body does not start with the %PDF magic")
	}
}

func TestReportPdfUnauthenticated(t *testing.T) {
	// REPORT_401: no session → the handler's own 401 guard (also unreachable
	// through the gated mount, which answers 401 first).
	rec := doRequest(nakedReportRouter(&fakeToolService{}), http.MethodGet, "/", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestReportPdfServiceForbidden(t *testing.T) {
	// The core re-check defense-in-depth (AD-6): a service ErrForbidden maps to
	// the uniform 403 with no PDF bytes.
	svc := &fakeToolService{reportErr: toolscore.ErrForbidden}
	surface := reportGateway([]string{toolscore.ReportExportPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF")) {
		t.Error("403 body leaks PDF bytes")
	}
}

func TestReportPdfNilResult(t *testing.T) {
	// A nil-returning service path (a wiring defect) must answer the clean 500,
	// never panic.
	svc := &fakeToolService{reportNil: true}
	surface := reportGateway([]string{toolscore.ReportExportPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
	if bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF")) {
		t.Error("500 body leaks PDF bytes")
	}
}

func TestReportPdfServiceError(t *testing.T) {
	// An unexpected service failure → the clean 500.
	svc := &fakeToolService{reportErr: errors.New("boom")}
	surface := reportGateway([]string{toolscore.ReportExportPermission}, activeAdmin(), svc)
	rec := doRequest(surface, http.MethodGet, "/", "tok", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestReportPdfMethodNotAllowed(t *testing.T) {
	// The report surface is READ-ONLY: a POST answers the uniform 405 envelope.
	surface := reportGateway([]string{toolscore.ReportExportPermission}, activeAdmin(), &fakeToolService{})
	rec := doRequest(surface, http.MethodPost, "/", "tok", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405 (body %s)", rec.Code, rec.Body.String())
	}
}
