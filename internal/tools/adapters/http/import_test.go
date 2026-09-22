package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/saskia-peters/gear/internal/platform/httpapi"
	toolscore "github.com/saskia-peters/gear/internal/tools/core"
	toolports "github.com/saskia-peters/gear/internal/tools/ports"
)

// importUpload builds a multipart POST /import with the given CSV file content
// and runs it through the REAL ToolRoutes write sub-gate (tools.manage-only
// re-check, Story 4-3b + 4.5). It returns the recorder and the captured rows
// the service received.
func importUpload(t *testing.T, perms []string, csvContent string) (*httptest.ResponseRecorder, *fakeToolService) {
	t.Helper()
	svc := &fakeToolService{}
	rec, svc2 := importUploadWithService(t, perms, csvContent, svc)
	return rec, svc2
}

// importUploadWithService is importUpload with an injected fake service (so a
// test can drive the importErr / importNil branches of the handler).
func importUploadWithService(t *testing.T, perms []string, csvContent string, svc *fakeToolService) (*httptest.ResponseRecorder, *fakeToolService) {
	t.Helper()
	surface := toolGateway(perms, activeAdmin(), svc)

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, err := w.CreateFormFile("file", "tools.csv")
	if err != nil {
		t.Fatalf("CreateFormFile err = %v", err)
	}
	if _, err := fw.Write([]byte(csvContent)); err != nil {
		t.Fatalf("writing csv err = %v", err)
	}
	_ = w.Close()

	req := httptest.NewRequest(http.MethodPost, "/import", &body)
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	surface.ServeHTTP(rec, req)
	return rec, svc
}

// decodeImportResult decodes the 200 JSON body into the wire shape.
func decodeImportResult(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding import result err = %v (body %s)", err, rec.Body.String())
	}
	return body
}

func TestToolsImportHappy(t *testing.T) {
	// IMPORT_HAPPY: a valid CSV → 200 { imported: 2, errors: [] }, the header
	// matched case-insensitively, the file line numbers are physical rows.
	csv := "Name,Tool_Type,Schedule,Inventory_Number\nBohrmaschine-01,Bohrmaschine,,GEAR000001\nBohrmaschine-02,bohrmaschine,1 Jahr,\n"
	rec, svc := importUpload(t, []string{toolscore.ToolsManagePermission}, csv)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.importCalls != 1 {
		t.Fatalf("service import calls = %d, want 1", svc.importCalls)
	}
	if len(svc.lastImportRows) != 2 {
		t.Fatalf("parsed rows = %d, want 2", len(svc.lastImportRows))
	}
	first := svc.lastImportRows[0]
	if first.Line != 2 || first.Name != "Bohrmaschine-01" || first.ToolTypeName != "Bohrmaschine" {
		t.Errorf("row 0 = %+v, want line 2 / name / type", first)
	}
	if first.ScheduleName != "" || first.InventoryNumber != "GEAR000001" {
		t.Errorf("row 0 schedule/inventory = %q/%q, want empty / GEAR000001", first.ScheduleName, first.InventoryNumber)
	}
	second := svc.lastImportRows[1]
	if second.Line != 3 || second.ToolTypeName != "bohrmaschine" || second.ScheduleName != "1 Jahr" {
		t.Errorf("row 1 = %+v, want line 3 / type / schedule", second)
	}
	body := decodeImportResult(t, rec)
	if body["imported"] != float64(2) {
		t.Errorf("imported = %v, want 2", body["imported"])
	}
	if errs, ok := body["errors"].([]any); !ok || len(errs) != 0 {
		t.Errorf("errors = %v, want empty array (never null)", body["errors"])
	}
}

func TestToolsImportMixedErrors(t *testing.T) {
	// IMPORT_MIXED: the service's per-row errors travel verbatim in the 200
	// response (row line + German reason).
	svc := &fakeToolService{}
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)
	svc.importResult = &toolscore.ToolImportResult{
		Imported: 1,
		Errors: []toolscore.ToolImportError{
			{Row: 3, Reason: "Tool Type 'X' nicht gefunden"},
			{Row: 4, Reason: "Zeitplan 'Y' nicht gefunden"},
		},
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, _ := w.CreateFormFile("file", "tools.csv")
	_, _ = fw.Write([]byte("name,tool_type,schedule,inventory_number\nA,B,\nC,D,\nE,F,\n"))
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/import", &body)
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	surface.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (partial success by design; body %s)", rec.Code, rec.Body.String())
	}
	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decoding err = %v", err)
	}
	if res["imported"] != float64(1) {
		t.Errorf("imported = %v, want 1", res["imported"])
	}
	errs, ok := res["errors"].([]any)
	if !ok || len(errs) != 2 {
		t.Fatalf("errors = %v, want 2 per-row errors", res["errors"])
	}
	first, ok := errs[0].(map[string]any)
	if !ok || first["row"] != float64(3) || first["reason"] != "Tool Type 'X' nicht gefunden" {
		t.Errorf("errors[0] = %v, want {row: 3, reason: ...}", errs[0])
	}
}

func TestToolsImportMissingHeader(t *testing.T) {
	// IMPORT_MISSING_HEADER: no `name`/`tool_type` column → German 400 BEFORE
	// any row is processed; the service is never called.
	rec, svc := importUpload(t, []string{toolscore.ToolsManagePermission}, "foo,bar\nx,y\n")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.importCalls != 0 {
		t.Errorf("service import calls = %d, want 0 (rejected before processing)", svc.importCalls)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 400 err = %v", err)
	}
	if !strings.Contains(env.Error.Message, "name") || !strings.Contains(env.Error.Message, "tool_type") {
		t.Errorf("message = %q, want it to name the missing columns", env.Error.Message)
	}
}

func TestToolsImportHeaderOnly(t *testing.T) {
	// IMPORT_EMPTY: a header-only CSV → German 400 "keine Datenzeilen".
	rec, svc := importUpload(t, []string{toolscore.ToolsManagePermission}, "name,tool_type,schedule,inventory_number\n")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.importCalls != 0 {
		t.Errorf("service import calls = %d, want 0", svc.importCalls)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 400 err = %v", err)
	}
	if !strings.Contains(env.Error.Message, "Datenzeilen") {
		t.Errorf("message = %q, want the keine-Datenzeilen microcopy", env.Error.Message)
	}
}

func TestToolsImportMalformedCSV(t *testing.T) {
	// IMPORT_MALFORMED: unparseable CSV (broken quoting) → German 400.
	rec, svc := importUpload(t, []string{toolscore.ToolsManagePermission}, "name,tool_type\n\"unterminated,B\n")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.importCalls != 0 {
		t.Errorf("service import calls = %d, want 0", svc.importCalls)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 400 err = %v", err)
	}
	if !strings.Contains(env.Error.Message, "CSV") {
		t.Errorf("message = %q, want the CSV-read error microcopy", env.Error.Message)
	}
}

func TestToolsImportNoFile(t *testing.T) {
	// A multipart request WITHOUT the `file` field → German 400.
	svc := &fakeToolService{}
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("other", "value")
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/import", &body)
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	surface.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 400 err = %v", err)
	}
	if !strings.Contains(env.Error.Message, "CSV") {
		t.Errorf("message = %q, want the pick-a-CSV microcopy", env.Error.Message)
	}
}

func TestToolsImportForbidden(t *testing.T) {
	// IMPORT_FORBIDDEN (AD-6): a tool.edit-ONLY holder is denied the write-only
	// sub-gate with the uniform 403 and NO tool data — the service is never
	// called.
	rec, svc := importUpload(t, []string{toolscore.ToolEditPermission}, "name,tool_type\nA,B\n")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.importCalls != 0 {
		t.Errorf("service import calls = %d, want 0 (denied at the sub-gate)", svc.importCalls)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "bohrmaschine") {
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

func TestToolsImportTooLarge(t *testing.T) {
	// IMPORT_TOO_LARGE: a body over the 5 MB cap → German 400.
	svc := &fakeToolService{}
	surface := toolGateway([]string{toolscore.ToolsManagePermission}, activeAdmin(), svc)
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, _ := w.CreateFormFile("file", "tools.csv")
	// > 5 MB of payload (the MaxBytesReader caps the WHOLE multipart body).
	_, _ = fw.Write([]byte("name,tool_type\n"))
	_, _ = fw.Write([]byte(strings.Repeat("a", (5<<20)+1024)))
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/import", &body)
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	surface.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.importCalls != 0 {
		t.Errorf("service import calls = %d, want 0", svc.importCalls)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 400 err = %v", err)
	}
	if !strings.Contains(env.Error.Message, "zu groß") {
		t.Errorf("message = %q, want the too-large microcopy", env.Error.Message)
	}
}

func TestToolsImportExtraColumnsIgnored(t *testing.T) {
	// Extra columns are ignored; a short data row (missing trailing cells)
	// reads the absent cells as "" (= NOT provided).
	csv := "name,tool_type,schedule,inventory_number,notiz\nBohrmaschine-01,Bohrmaschine\n"
	rec, svc := importUpload(t, []string{toolscore.ToolsManagePermission}, csv)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(svc.lastImportRows) != 1 {
		t.Fatalf("parsed rows = %d, want 1", len(svc.lastImportRows))
	}
	row := svc.lastImportRows[0]
	if row.Name != "Bohrmaschine-01" || row.ScheduleName != "" || row.InventoryNumber != "" {
		t.Errorf("row = %+v, want absent cells read as not-provided", row)
	}
}

func TestToolsImportServiceForbidden(t *testing.T) {
	// The core re-check (AD-6) answers the uniform 403 with NO tool data — the
	// handler routes the service's ErrForbidden through mapToolError.
	rec, svc := importUploadWithService(t, []string{toolscore.ToolsManagePermission},
		"name,tool_type\nA,B\n", &fakeToolService{importErr: toolscore.ErrForbidden})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "bohrmaschine") {
		t.Errorf("403 body leaks tool data: %s", rec.Body.String())
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 403 err = %v", err)
	}
	if env.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", env.Error.Code)
	}
	if svc.importCalls != 1 {
		t.Errorf("service import calls = %d, want 1 (the error came from the service)", svc.importCalls)
	}
}

func TestToolsImportServiceInternalError(t *testing.T) {
	// An unexpected service failure surfaces as the uniform 500 internal_error.
	rec, _ := importUploadWithService(t, []string{toolscore.ToolsManagePermission},
		"name,tool_type\nA,B\n", &fakeToolService{importErr: errors.New("boom")})
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

func TestToolsImportNilResultGuard(t *testing.T) {
	// A nil-returning service path (a wiring defect) must answer a clean 500,
	// never panic.
	rec, _ := importUploadWithService(t, []string{toolscore.ToolsManagePermission},
		"name,tool_type\nA,B\n", &fakeToolService{importNil: true})
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

func TestToolsImportNoOptionalColumns(t *testing.T) {
	// A CSV with ONLY the required columns reads the absent schedule /
	// inventory cells as "" (NOT provided) — never as a wrong column (a
	// missing map key must not collapse onto column 0).
	csv := "name,tool_type\nBohrmaschine-01,Bohrmaschine\n"
	rec, svc := importUpload(t, []string{toolscore.ToolsManagePermission}, csv)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(svc.lastImportRows) != 1 {
		t.Fatalf("parsed rows = %d, want 1", len(svc.lastImportRows))
	}
	row := svc.lastImportRows[0]
	if row.Name != "Bohrmaschine-01" || row.ToolTypeName != "Bohrmaschine" {
		t.Errorf("row = %+v, want name + type read correctly", row)
	}
	if row.ScheduleName != "" || row.InventoryNumber != "" {
		t.Errorf("row schedule/inventory = %q/%q, want both empty (columns absent)", row.ScheduleName, row.InventoryNumber)
	}
}

var _ toolports.Service = (*fakeToolService)(nil)

// ============================================================================
// parseToolImportCSV unit tests (Patch 4-7): the parser is a pure function
// over an io.Reader, so the sentinels, the BOM/UTF-8 handling, the ragged-row
// empty-cell semantics and the PHYSICAL line tracking are pinned directly.
// ============================================================================

func TestParseToolImportCSVHappy(t *testing.T) {
	rows, err := parseToolImportCSV(strings.NewReader(
		"Name,Tool_Type,Schedule,Inventory_Number\nBohrmaschine-01,Bohrmaschine,,GEAR000001\n",
	))
	if err != nil {
		t.Fatalf("parseToolImportCSV err = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Line != 2 || rows[0].Name != "Bohrmaschine-01" || rows[0].ToolTypeName != "Bohrmaschine" {
		t.Errorf("row = %+v, want line 2 / name / type", rows[0])
	}
	if rows[0].ScheduleName != "" || rows[0].InventoryNumber != "GEAR000001" {
		t.Errorf("row schedule/inventory = %q/%q, want empty / GEAR000001", rows[0].ScheduleName, rows[0].InventoryNumber)
	}
}

func TestParseToolImportCSVBOM(t *testing.T) {
	// A UTF-8 BOM in the first header cell is stripped (Excel-exported CSVs).
	rows, err := parseToolImportCSV(strings.NewReader("\ufeffname,tool_type\nA,B\n"))
	if err != nil {
		t.Fatalf("parseToolImportCSV(BOM) err = %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "A" || rows[0].ToolTypeName != "B" {
		t.Fatalf("rows = %+v, want the BOM-stripped header mapped", rows)
	}
}

func TestParseToolImportCSVEmpty(t *testing.T) {
	// 0-byte input → errImportCSVEmpty (German "leer" at the handler).
	if _, err := parseToolImportCSV(strings.NewReader("")); !errors.Is(err, errImportCSVEmpty) {
		t.Fatalf("err = %v, want errImportCSVEmpty", err)
	}
}

func TestParseToolImportCSVHeaderOnly(t *testing.T) {
	// A header with no data rows → errImportCSVNoRows.
	if _, err := parseToolImportCSV(strings.NewReader("name,tool_type,schedule,inventory_number\n")); !errors.Is(err, errImportCSVNoRows) {
		t.Fatalf("err = %v, want errImportCSVNoRows", err)
	}
}

func TestParseToolImportCSVMissingCols(t *testing.T) {
	// No `name`/`tool_type` column → the German 400 sentinel.
	if _, err := parseToolImportCSV(strings.NewReader("foo,bar\nx,y\n")); !errors.Is(err, errImportCSVMissingCols) {
		t.Fatalf("err = %v, want errImportCSVMissingCols", err)
	}
}

func TestParseToolImportCSVRaggedRows(t *testing.T) {
	// A short/ragged row (missing trailing cells) reads the absent cells as ""
	// (= NOT provided), never a panic.
	rows, err := parseToolImportCSV(strings.NewReader("name,tool_type,schedule,inventory_number\nBohrmaschine-01,Bohrmaschine\n"))
	if err != nil {
		t.Fatalf("parseToolImportCSV(err) err = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].ScheduleName != "" || rows[0].InventoryNumber != "" {
		t.Errorf("row schedule/inventory = %q/%q, want both empty", rows[0].ScheduleName, rows[0].InventoryNumber)
	}
}

func TestParseToolImportCSVPhysicalLines(t *testing.T) {
	// A quoted cell with an embedded newline spans physical lines: the record
	// that FOLLOWS it must report its TRUE physical line, not the record index.
	csv := "name,tool_type,schedule,inventory_number\n\"A\nB\",Bohrmaschine,,\nNext,Bohrmaschine,,\n"
	rows, err := parseToolImportCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("parseToolImportCSV(err) err = %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	// Physical lines: header=1, record 1 starts at 2 (spans 2-3), record 2
	// starts at 4. An error on record 2 therefore points at line 4.
	if rows[0].Line != 2 || rows[0].Name != "A\nB" {
		t.Errorf("rows[0] = %+v, want line 2 with the embedded-newline value", rows[0])
	}
	if rows[1].Line != 4 || rows[1].Name != "Next" {
		t.Errorf("rows[1] = %+v, want line 4 (the TRUE physical line after the multi-line cell)", rows[1])
	}
}

func TestParseToolImportCSVDupCols(t *testing.T) {
	// A duplicate NORMALIZED header name (two `name` columns) rejects the file.
	if _, err := parseToolImportCSV(strings.NewReader("name,tool_type,Name\nA,B,C\n")); !errors.Is(err, errImportCSVDupCols) {
		t.Fatalf("err = %v, want errImportCSVDupCols", err)
	}
}

func TestParseToolImportCSVNotUTF8(t *testing.T) {
	// An invalid UTF-8 byte rejects the file before any CSV decoding.
	raw := append([]byte("name,tool_type\n"), 0xff)
	if _, err := parseToolImportCSV(bytes.NewReader(raw)); !errors.Is(err, errImportCSVNotUTF8) {
		t.Fatalf("err = %v, want errImportCSVNotUTF8", err)
	}
}

func TestToolsImportEmptyFile(t *testing.T) {
	// IMPORT_EMPTY (empty-file half): a 0-byte `file` → 400 German "leer" and
	// ZERO service calls (rejected before any row is processed).
	rec, svc := importUpload(t, []string{toolscore.ToolsManagePermission}, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.importCalls != 0 {
		t.Errorf("service import calls = %d, want 0", svc.importCalls)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 400 err = %v", err)
	}
	if !strings.Contains(env.Error.Message, "leer") {
		t.Errorf("message = %q, want the leer microcopy", env.Error.Message)
	}
}

func TestToolsImportDuplicateColumns(t *testing.T) {
	// Duplicate header columns → 400 German and ZERO service calls.
	rec, svc := importUpload(t, []string{toolscore.ToolsManagePermission}, "name,tool_type,Name\nA,B,C\n")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.importCalls != 0 {
		t.Errorf("service import calls = %d, want 0", svc.importCalls)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 400 err = %v", err)
	}
	if !strings.Contains(env.Error.Message, "doppelte Spalten") {
		t.Errorf("message = %q, want the duplicate-columns microcopy", env.Error.Message)
	}
}

func TestToolsImportNotUTF8(t *testing.T) {
	// Non-UTF-8 bytes → 400 German and ZERO service calls.
	raw := append([]byte("name,tool_type\nA,B\n"), 0xff)
	rec, svc := importUpload(t, []string{toolscore.ToolsManagePermission}, string(raw))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.importCalls != 0 {
		t.Errorf("service import calls = %d, want 0", svc.importCalls)
	}
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding 400 err = %v", err)
	}
	if !strings.Contains(env.Error.Message, "UTF-8") {
		t.Errorf("message = %q, want the not-UTF-8 microcopy", env.Error.Message)
	}
}
