package http

import (
	"bytes"
	"compress/zlib"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	toolscore "github.com/saskia-peters/gear/internal/tools/core"
)

// The PDF content-stream tests (Story 6.2 review): the renderer is
// DETERMINISTIC for a given row set, so the German text is asserted against
// the decompressed Flate content stream — never golden bytes. The cp1252
// translation maps: ü→0xFC, ä→0xE4, ß→0xDF, the en-dash “–”→0x96.

// reportContentFixture builds the rows the content tests render: a green tool
// inspected on a known date by "Anna Muster", an OOS tool never inspected and
// a red tool inspected by a DELETED account (→ "Deleted User").
func reportContentFixture() []*toolscore.ReportRow {
	inspected := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	return []*toolscore.ReportRow{
		reportRowFixture("id-green", "Bohrmaschine-01", "green", &inspected, "Anna Muster"),
		reportRowFixture("id-oos", "Drehmaschine-02", "oos", nil, ""),
		reportRowFixture("id-deleted", "Schleifmaschine-03", "red", &inspected, toolscore.DeletedUserDisplayName),
	}
}

func TestReportPDFContentFullReport(t *testing.T) {
	// REPORT_OK: the full report carries the tool names, the German status
	// labels, the last-inspected RFC3339 date part (UTC), the "–" cell for the
	// never-inspected tool, the "Deleted User" fallback, the "Alle Status"
	// caption and the UTC generated-at label.
	b, err := renderReportPDF(reportContentFixture(), nil, time.Date(2026, 9, 18, 14, 5, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("render err = %v", err)
	}
	content := decompressPDFStreams(t, b)

	for _, want := range []string{
		// Title + generated-at (UTC) + the absent-filter caption.
		"G.E.A.R. \x96 Statusbericht",
		"Erstellt am 18.09.2026 um 14:05 UTC",
		"Alle Status",
		// Column headers.
		"Name", "Typ", "Status", "Zuletzt gepr\xFCft", "Pr\xFCfer/in",
		// Tool names.
		"Bohrmaschine-01", "Drehmaschine-02", "Schleifmaschine-03",
		// German status labels (cp1252: Ü→0xDC, ä→0xE4, ß→0xDF).
		"Einsatzbereit", "Au\xDFer Betrieb", "\xDCberf\xE4llig",
		// Last-inspected date part (UTC) + the inspector display name.
		"2026-09-10", "Anna Muster",
		// The never-inspected "–" cell (Zuletzt geprüft AND Prüfer/in).
		"\x96",
		// The deleted-account fallback.
		"Deleted User",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("report content missing %q", want)
		}
	}
}

func TestReportPDFContentFiltered(t *testing.T) {
	// REPORT_FILTERED: only the MATCHING tool renders (the core filters; the
	// renderer draws exactly the rows it receives) — a non-matching tool never
	// appears, and the caption names the active filter.
	rows := reportContentFixture()
	matching := []*toolscore.ReportRow{rows[0]} // Bohrmaschine-01 (green)
	b, err := renderReportPDF(matching, []string{"green"}, time.Date(2026, 9, 18, 14, 5, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("render err = %v", err)
	}
	content := decompressPDFStreams(t, b)

	if !strings.Contains(content, "Bohrmaschine-01") {
		t.Error("filtered report missing the matching tool")
	}
	for _, absent := range []string{"Drehmaschine-02", "Schleifmaschine-03"} {
		if strings.Contains(content, absent) {
			t.Errorf("filtered report leaks the non-matching tool %q", absent)
		}
	}
	if !strings.Contains(content, "Status: Einsatzbereit") {
		t.Errorf("filtered report caption missing: %q", "Status: Einsatzbereit")
	}
}

func TestReportPDFContentNeverInspected(t *testing.T) {
	// REPORT_NEVER: a never-inspected row renders "–" in BOTH the Zuletzt
	// geprüft AND Prüfer/in columns (no date, no name).
	rows := reportContentFixture()
	never := []*toolscore.ReportRow{rows[1]} // Drehmaschine-02, nil LastInspectedAt
	b, err := renderReportPDF(never, nil, time.Date(2026, 9, 18, 14, 5, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("render err = %v", err)
	}
	content := decompressPDFStreams(t, b)

	if !strings.Contains(content, "Drehmaschine-02") {
		t.Error("never-inspected report missing the tool")
	}
	// No date, no name — just the two "–" cells (one per column).
	if strings.Contains(content, "2026-09-10") {
		t.Error("never-inspected report must not carry a date")
	}
	if strings.Contains(content, "Anna Muster") {
		t.Error("never-inspected report must not carry an inspector name")
	}
	if got := strings.Count(content, "\x96"); got < 2 {
		t.Errorf("never-inspected report has %d en-dash cells, want >= 2 (Zuletzt geprüft + Prüfer/in)", got)
	}
}

func TestReportPDFContentDeletedUser(t *testing.T) {
	// REPORT_DELETED_USER: an inspected tool whose inspector account is gone
	// renders the literal "Deleted User" (Story 3.4 forward-compat).
	rows := reportContentFixture()
	b, err := renderReportPDF(rows, nil, time.Date(2026, 9, 18, 14, 5, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("render err = %v", err)
	}
	content := decompressPDFStreams(t, b)
	if !strings.Contains(content, "Deleted User") {
		t.Error("report content missing the Deleted User fallback")
	}
}

func TestReportPDFContentEmpty(t *testing.T) {
	// EMPTY: a zero-row report (no filter) renders the German empty message, not
	// a bare header; a filter matching nothing renders the filtered variant.
	noFilter, err := renderReportPDF(nil, nil, time.Date(2026, 9, 18, 14, 5, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("render(empty) err = %v", err)
	}
	if content := decompressPDFStreams(t, noFilter); !strings.Contains(content, "Keine Werkzeuge vorhanden.") {
		t.Error("empty report missing the German empty message")
	}

	filtered, err := renderReportPDF(nil, []string{"oos"}, time.Date(2026, 9, 18, 14, 5, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("render(empty filtered) err = %v", err)
	}
	if content := decompressPDFStreams(t, filtered); !strings.Contains(content, "Keine Werkzeuge im ausgew\xE4hlten Status.") {
		t.Error("empty filtered report missing the German filtered-empty message")
	}
}

func TestReportPDFContentHeaderRepeatedOnPageBreaks(t *testing.T) {
	// A multi-page table repeats the column header on EVERY page: the header
	// draw count equals the page count (the review's item 2). 90 rows force at
	// least two pages.
	inspected := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	rows := make([]*toolscore.ReportRow, 0, 90)
	for i := 0; i < 90; i++ {
		rows = append(rows, reportRowFixture(
			"id", "Bohrmaschine-01", "green", &inspected, "Anna Muster"))
	}
	b, err := renderReportPDF(rows, nil, time.Date(2026, 9, 18, 14, 5, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("render err = %v", err)
	}
	content := decompressPDFStreams(t, b)
	pages := strings.Count(string(b), "/Type /Page") - strings.Count(string(b), "/Type /Pages")
	headers := strings.Count(content, "Zuletzt gepr\xFCft")
	if pages < 2 {
		t.Fatalf("pages = %d, want >= 2 (the multi-page precondition)", pages)
	}
	if headers != pages {
		t.Errorf("header draws = %d, want %d (one repeated header per page)", headers, pages)
	}
}

func TestReportFilterCaptionAndLabels(t *testing.T) {
	// Item 12: the caption/label vocabulary is folded into the content tests —
	// a wrong caption or a missing label-map entry fails loudly here.
	if got := reportFilterCaption(nil); got != "Alle Status" {
		t.Errorf("reportFilterCaption(nil) = %q, want %q", got, "Alle Status")
	}
	if got := reportFilterCaption([]string{"green", "oos"}); got != "Status: Einsatzbereit, Außer Betrieb" {
		t.Errorf("reportFilterCaption([green oos]) = %q, want %q", got, "Status: Einsatzbereit, Außer Betrieb")
	}
	labels := map[string]string{
		"green":  "Einsatzbereit",
		"orange": "Ausstehend",
		"red":    "Überfällig",
		"oos":    "Außer Betrieb",
	}
	for code, want := range labels {
		if got := reportStatusLabel(code); got != want {
			t.Errorf("reportStatusLabel(%q) = %q, want %q", code, got, want)
		}
	}
	// An unknown/empty code renders the code itself (never crashes, never a
	// wrong label).
	if got := reportStatusLabel("neon"); got != "neon" {
		t.Errorf("reportStatusLabel(unknown) = %q, want the code itself", got)
	}
}

// decompressPDFStreams extracts + inflates every FlateDecode content stream,
// returning the concatenated raw bytes. fpdf compresses the page content
// streams by default, so the German text lives inside them. The scan anchors on
// the /FlateDecode FILTER DECLARATION and inflates the NEXT "stream" keyword
// after it — a bare "stream" can also occur randomly INSIDE compressed bytes,
// so a naked "stream" scan would corrupt the offsets.
func decompressPDFStreams(t *testing.T, pdf []byte) string {
	t.Helper()
	var out bytes.Buffer
	streamRe := regexp.MustCompile(`stream\r?\n`)
	for _, fd := range regexp.MustCompile(`/FlateDecode`).FindAllIndex(pdf, -1) {
		rel := streamRe.FindIndex(pdf[fd[0]:])
		if rel == nil {
			continue
		}
		start := fd[0] + rel[1]
		endMarker := bytes.Index(pdf[start:], []byte("endstream"))
		if endMarker < 0 {
			continue
		}
		raw := pdf[start : start+endMarker]
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			continue
		}
		inflated, _ := io.ReadAll(zr)
		_ = zr.Close()
		out.Write(inflated)
	}
	return out.String()
}
