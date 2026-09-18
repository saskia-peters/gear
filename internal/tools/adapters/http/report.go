package http

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-pdf/fpdf"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	toolscore "github.com/saskia-peters/gear/internal/tools/core"
	usercore "github.com/saskia-peters/gear/internal/user/core"
)

// Status report export (Story 6.2, FR-17/AD-6/AD-5): GET /api/v1/tools/report.pdf
// renders the dashboard's CURRENT view (the same derived statuses, AD-4) as a
// shareable PDF. The SPA sends only the ACTIVE status filter codes
// (?status=green,oos — absent = "Alle"); the SERVER re-derives each tool's
// status itself and includes only the matching tools — the client is never
// trusted with the statuses. The whole surface is gated by `report.export` at
// the composition-root mount (one permission per surface, AD-6); a non-holder
// answers the uniform 403 with NO PDF bytes. German throughout.

// reportStatusLabels maps the derived status codes (AD-4/AD-5) to the German
// user-facing vocabulary the dashboard renders — the PDF's Status column and
// the filter caption reuse the SAME words so the export never disagrees with
// the screen.
var reportStatusLabels = map[string]string{
	"green":  "Einsatzbereit",
	"orange": "Ausstehend",
	"red":    "Überfällig",
	"oos":    "Außer Betrieb",
}

// reportFilterCodes is the server-authoritative set of accepted status filter
// codes. A code NOT in this set answers the German 400 envelope (REPORT_BAD_CODE).
var reportFilterCodes = map[string]struct{}{
	"green": {}, "orange": {}, "red": {}, "oos": {},
}

// ReportRoutes returns the status-report router (Story 6.2, FR-17/AD-6): the
// GET report handler at the router ROOT. The composition root mounts this router
// at the full path prefix (/api/v1/tools/report.pdf via chi Mount, which strips
// the prefix), so the route pattern is defined ONCE here and never duplicated at
// the mount site. The whole router is gated by `report.export` at the
// composition-root mount point — its OWN gate, one permission per surface
// (AD-6) — so this router carries no gateway itself; 404/405 answer with the
// uniform JSON envelope so no sub-path can emit a plain-text body. Read-only —
// no write path lives here.
func (h *Handler) ReportRoutes() http.Handler {
	r := chi.NewRouter()
	r.NotFound(httpapi.NotFoundHandler())
	r.MethodNotAllowed(httpapi.MethodNotAllowedHandler())
	r.Get("/", h.ExportStatusReport)
	return r
}

// ExportStatusReport handles GET /api/v1/tools/report.pdf
// (REPORT_OK / REPORT_FILTERED / REPORT_NEVER / REPORT_DELETED_USER /
// REPORT_BAD_CODE, Story 6.2, FR-17/AD-6/AD-5): it parses the optional
// `status` filter codes (comma-separated, each MUST be green|orange|red|oos —
// anything else answers the German 400 envelope), asks the core to derive the
// statuses + latest-inspection inputs (the server NEVER trusts the client) and
// renders the A4 PDF table. Gated `report.export` at the mount; the core
// re-checks defense-in-depth (AD-6) — a non-holder answers the uniform 403 with
// no PDF bytes.
//
// Error mapping (uniform envelope, via mapReportError):
//   - 401 unauthorized when the caller is not authenticated
//   - 400 invalid_request (German) for a malformed `status` filter code
//   - 403 forbidden when the caller lacks report.export (no PDF bytes, AD-6)
//   - 500 internal_error on an unexpected failure
func (h *Handler) ExportStatusReport(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	filterCodes, err := parseReportFilterCodes(r)
	if err != nil {
		h.log().Warn("status report export rejected: malformed status filter", "email", user.Email)
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Ungültiger Statusfilter.")
		return
	}

	rows, err := h.service.ExportStatusReport(r.Context(), user.ID, filterCodes)
	if err != nil {
		h.mapReportError(w, r, err, user)
		return
	}
	// Defensive nil-guard: a nil-returning service path (a wiring defect) must
	// not panic — answer the clean 500 via the error mapper's default branch.
	if rows == nil {
		h.mapReportError(w, r, errors.New("tools http: nil report result from service"), user)
		return
	}

	pdfBytes, err := renderReportPDF(rows, filterCodes, time.Now().UTC())
	if err != nil {
		h.log().Error("status report PDF rendering failed", "email", user.Email, "error", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
		return
	}

	h.log().Info("status report exported", "email", user.Email, "rows", len(rows), "filters", strings.Join(filterCodes, ","))
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="statusbericht.pdf"`)
	// The report is a GATED artifact (report.export, AD-6): tool + status data
	// must never be cached by a proxy or the browser.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdfBytes)
}

// parseReportFilterCodes reads the optional `status` query parameter: a
// comma-separated list of derived status codes (green|orange|red|oos). An
// ABSENT parameter answers an EMPTY slice (the report shows "Alle Status"). An
// EMPTY or whitespace-only value, an empty element between commas, or a code
// outside the accepted set answers an error (the German 400, REPORT_BAD_CODE).
// Repeated codes are DEDUPLICATED (order preserved, first occurrence wins) so
// the filter caption never duplicates a label (e.g. ?status=green,green).
func parseReportFilterCodes(r *http.Request) ([]string, error) {
	q := r.URL.Query()
	if !q.Has("status") {
		return nil, nil
	}
	raw := strings.TrimSpace(q.Get("status"))
	if raw == "" {
		return nil, errors.New("tools http: empty status filter")
	}
	parts := strings.Split(raw, ",")
	codes := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		code := strings.TrimSpace(part)
		if code == "" {
			return nil, errors.New("tools http: empty status filter code")
		}
		if _, ok := reportFilterCodes[code]; !ok {
			return nil, errors.New("tools http: unknown status filter code")
		}
		if _, dup := seen[code]; dup {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	return codes, nil
}

// reportStatusLabel resolves a derived status code to its German label (the
// Status column + the filter caption). An unknown/empty code (a server value
// that slipped past typing) renders the code itself — the PDF must never crash
// on a value it did not emit.
func reportStatusLabel(code string) string {
	if label, ok := reportStatusLabels[code]; ok {
		return label
	}
	return code
}

// mapReportError writes the uniform envelope for the status-report service
// errors (Story 6.2, AD-6). A report.export-less caller → the generic no-hint
// 403 (no PDF bytes); the default branch → the clean 500 (a canceled request is
// not answered).
func (h *Handler) mapReportError(w http.ResponseWriter, r *http.Request, err error, user *usercore.User) {
	switch {
	case errors.Is(err, toolscore.ErrForbidden):
		h.log().Warn("status report access forbidden", "email", user.Email)
		httpapi.WriteError(w, http.StatusForbidden, "forbidden", "Keine Berechtigung.")
	default:
		// Client-abort guard: a canceled request has no one to answer.
		if r.Context().Err() != nil {
			return
		}
		h.log().Error("status report request failed unexpectedly", "error", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal_error", "Ein interner Fehler ist aufgetreten.")
	}
}

// PDF layout constants (A4 portrait, mm): the report is a small pure-Go table
// with wrapping columns. Margins + the five column widths sum to the 210mm page
// width.
const (
	reportMarginX      = 15.0
	reportMarginTop    = 15.0
	reportPageBottom   = 20.0
	reportCellPad      = 1.5
	reportHeaderHeight = 7.0
	reportContentWidth = 210.0 - 2*reportMarginX // 180mm — the five columns
	reportPageUsableHt = 297.0 - reportPageBottom
)

// reportColumnWidths are the five report columns in mm (Name | Typ | Status |
// Zuletzt geprüft | Prüfer/in), summing to reportContentWidth (180mm).
var reportColumnWidths = []float64{52, 30, 32, 30, 36}

// renderReportPDF renders the status report as a pure-Go A4 PDF (Story 6.2,
// FR-17): the title "G.E.A.R. – Statusbericht", a generated-at timestamp (UTC),
// a caption naming the active filter ("Alle Status" or the German labels), the
// header row (Name | Typ | Status | Zuletzt geprüft | Prüfer/in — REPEATED on
// every page of a multi-page table) and one wrapping row per tool. The Status
// column uses the German labels (Einsatzbereit/Ausstehend/Überfällig/Außer
// Betrieb); the last-inspected date renders the RFC3339 date part in UTC; a
// never-inspected tool renders "–" in the Zuletzt geprüft AND Prüfer/in
// columns. A report with NO rows renders a German empty message instead of a
// bare header. The output is bytes-only — the handler sets the
// application/pdf content type.
func renderReportPDF(rows []*toolscore.ReportRow, filterCodes []string, generatedAt time.Time) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetTitle("G.E.A.R. – Statusbericht", true)
	pdf.SetMargins(reportMarginX, reportMarginTop, reportMarginX)
	// Page breaks are handled MANUALLY in drawReportRows (the whole row is
	// moved to a fresh page and the column header is re-drawn there). The
	// library's automatic break is DISABLED so a row can never split across
	// pages mid-cell without its repeated header.
	pdf.SetAutoPageBreak(false, reportPageBottom)
	// The built-in core fonts are Latin-1; the cp1252 translation maps the
	// German umlauts + the en-dash to their WinAnsi glyphs (the whole report is
	// German throughout — no TTF embedding needed at V1).
	tr := pdf.UnicodeTranslatorFromDescriptor("cp1252")

	pdf.AddPage()

	// Title + generated-at + filter caption. The timestamp is UTC (consistent
	// with the DB-submitted_at dates below) and labelled as such.
	pdf.SetFont("Helvetica", "B", 18)
	pdf.CellFormat(0, 11, tr("G.E.A.R. – Statusbericht"), "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.CellFormat(0, 6, tr("Erstellt am "+generatedAt.UTC().Format("02.01.2006 um 15:04")+" UTC"), "", 1, "L", false, 0, "")
	pdf.CellFormat(0, 6, tr(reportFilterCaption(filterCodes)), "", 1, "L", false, 0, "")
	pdf.Ln(4)

	// The column header is drawn on the first page and REPEATED after every
	// page break (review finding: a multi-page table stays readable).
	drawReportHeader(pdf, tr)

	if len(rows) == 0 {
		// EMPTY: a zero-tool fleet (or a filter matching nothing) must not
		// render a bare header — a German message fills the table.
		drawReportEmpty(pdf, tr, filterCodes)
	} else {
		drawReportRows(pdf, tr, rows)
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// drawReportHeader renders the Name | Typ | Status | Zuletzt geprüft | Prüfer/in
// column-header row. It is called on page 1 and re-drawn after every page break
// so a multi-page table repeats its column headers.
func drawReportHeader(pdf *fpdf.Fpdf, tr func(string) string) {
	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetFillColor(240, 240, 240)
	for i, header := range []string{"Name", "Typ", "Status", "Zuletzt geprüft", "Prüfer/in"} {
		pdf.CellFormat(reportColumnWidths[i], reportHeaderHeight, tr(header), "1", 0, "L", true, 0, "")
	}
	pdf.Ln(reportHeaderHeight)
}

// drawReportEmpty renders the German empty state across the full table width
// when the report has no rows (a zero-tool fleet → "Keine Werkzeuge
// vorhanden.", a filter matching nothing → "Keine Werkzeuge im ausgewählten
// Status.").
func drawReportEmpty(pdf *fpdf.Fpdf, tr func(string) string, filterCodes []string) {
	msg := "Keine Werkzeuge vorhanden."
	if len(filterCodes) > 0 {
		msg = "Keine Werkzeuge im ausgewählten Status."
	}
	pdf.SetFont("Helvetica", "", 10)
	pdf.CellFormat(reportContentWidth, 8, tr(msg), "1", 1, "C", false, 0, "")
}

// drawReportRows renders the wrapping data rows, breaking to a NEW page (with
// a REPEATED column header) when a row would cross the bottom margin.
func drawReportRows(pdf *fpdf.Fpdf, tr func(string) string, rows []*toolscore.ReportRow) {
	pdf.SetFont("Helvetica", "", 10)
	for _, row := range rows {
		lastInspected := "–"
		inspector := "–"
		if row.LastInspectedAt != nil {
			lastInspected = row.LastInspectedAt.UTC().Format("2006-01-02")
			inspector = row.LastInspectorName
		}
		cells := []string{
			row.Tool.Name,
			row.Tool.ToolTypeName,
			reportStatusLabel(string(row.Tool.Status.Status)),
			lastInspected,
			inspector,
		}

		// The box height is the tallest wrapped column PLUS the top + bottom
		// padding: the text (starting at y + cellPad, spanning lines·lineHeight)
		// ends at y + cellPad + lines·lineHeight, which is ≤ the rect bottom by
		// exactly cellPad — descenders never overflow the border (review
		// finding). An empty cell yields 0 SplitLines → clamp to one line so the
		// row always has the height of a rendered cell.
		rowHeight := reportLineHeight(pdf) + 2*reportCellPad
		for i, cell := range cells {
			lines := len(pdf.SplitLines([]byte(tr(cell)), reportColumnWidths[i]-2*reportCellPad))
			if lines < 1 {
				lines = 1
			}
			if h := float64(lines)*reportLineHeight(pdf) + 2*reportCellPad; h > rowHeight {
				rowHeight = h
			}
		}

		// The whole row fits on the current page (a single row taller than a
		// full page still degrades gracefully — it is clipped at the bottom
		// margin rather than split mid-row without a repeated header).
		if pdf.GetY()+rowHeight > reportPageUsableHt {
			pdf.AddPage()
			drawReportHeader(pdf, tr)
		}
		startY := pdf.GetY()
		for i, cell := range cells {
			x := pdf.GetX()
			y := pdf.GetY()
			pdf.Rect(x, y, reportColumnWidths[i], rowHeight, "D")
			pdf.SetXY(x+reportCellPad, y+reportCellPad)
			pdf.MultiCell(reportColumnWidths[i]-2*reportCellPad, reportLineHeight(pdf), tr(cell), "", "L", false)
			pdf.SetXY(x+reportColumnWidths[i], startY)
		}
		pdf.SetXY(reportMarginX, startY+rowHeight)
	}
}

// reportFilterCaption names the active filter ("Alle Status" when no filter is
// active, otherwise the German labels of the selected codes, comma-joined).
func reportFilterCaption(filterCodes []string) string {
	if len(filterCodes) == 0 {
		return "Alle Status"
	}
	labels := make([]string, 0, len(filterCodes))
	for _, code := range filterCodes {
		labels = append(labels, reportStatusLabel(code))
	}
	return "Status: " + strings.Join(labels, ", ")
}

// reportLineHeight is the wrapped-cell line height in the current unit (mm):
// the font size in points converted to mm with a ~1.25 line factor.
func reportLineHeight(pdf *fpdf.Fpdf) float64 {
	_, unitSize := pdf.GetFontSize()
	return unitSize * 1.25
}
