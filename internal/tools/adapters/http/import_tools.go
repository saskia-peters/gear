package http

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/saskia-peters/gear/internal/platform/auth"
	"github.com/saskia-peters/gear/internal/platform/httpapi"
	toolscore "github.com/saskia-peters/gear/internal/tools/core"
)// toolImportErrorDTO is one per-row import error (Story 4.5, FR-9): the 1-based
// file line + the German reason.
type toolImportErrorDTO struct {
	Row    int    `json:"row"`
	Reason string `json:"reason"`
}

// toolImportResultDTO is the POST /import payload (Story 4.5): the number of
// created/updated tools + the per-row errors. An EMPTY errors array means every
// row succeeded (never null).
type toolImportResultDTO struct {
	Imported int                  `json:"imported"`
	Errors   []toolImportErrorDTO `json:"errors"`
}

func (h *Handler) ImportTools(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFrom(r.Context())
	if user == nil {
		httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentifizierung erforderlich.")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, importBodyLimit)
	if err := r.ParseMultipartForm(importBodyLimit); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Die Datei ist zu groß (maximal 5 MB).")
			return
		}
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Die CSV-Datei konnte nicht gelesen werden.")
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, _, err := r.FormFile("file")
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", "Bitte wähle eine CSV-Datei aus.")
		return
	}
	defer func() { _ = file.Close() }()

	rows, parseErr := parseToolImportCSV(file)
	if parseErr != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid_request", importParseErrorMessage(parseErr))
		return
	}

	result, err := h.service.ImportTools(r.Context(), user.ID, rows)
	if err != nil {
		h.mapToolError(w, r, err, user)
		return
	}
	if result == nil {
		h.mapToolError(w, r, errors.New("tools http: nil import result from service"), user)
		return
	}
	h.log().Info("tools imported", "email", user.Email, "imported", result.Imported, "errors", len(result.Errors))

	httpapi.WriteJSON(w, http.StatusOK, toImportResultDTO(result))
}

// parseToolImportCSV decodes the uploaded CSV into structured core rows (the
// hexagon: core receives rows, never bytes). It reads the whole file, REJECTS
// non-UTF-8 bytes, then uses encoding/csv (comma, trimmed leading space,
// variable field count so short rows read as empty cells); the first row is
// the HEADER, columns matched case-insensitively by name (a NORMALIZED header
// name appearing twice rejects the file). Missing `name`/`tool_type` columns,
// an empty file or a header-only file answer a German 400 BEFORE any row is
// processed. The row Line numbers are the PHYSICAL file positions (header = 1,
// first data row = 2) computed from the raw bytes — so a quoted cell with an
// embedded newline still makes the following record point at its true line.
func parseToolImportCSV(r io.Reader) ([]toolscore.ToolImportRow, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, errImportCSVUnreadable
	}
	// UTF-8 is a hard contract: reject invalid bytes before any CSV decoding.
	if !utf8.Valid(raw) {
		return nil, errImportCSVNotUTF8
	}
	reader := csv.NewReader(bytes.NewReader(raw))
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, errImportCSVUnreadable
	}
	if len(records) == 0 {
		return nil, errImportCSVEmpty
	}
	header := records[0]
	cols := map[string]int{}
	for i, name := range header {
		// Normalize the header cell: lowercase + strip the UTF-8 BOM
		// (Excel-exported CSVs) + trim. A duplicate normalized name is a 400.
		norm := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "\ufeff"))
		if _, dup := cols[norm]; dup {
			return nil, errImportCSVDupCols
		}
		cols[norm] = i
	}
	nameIdx, nameOK := cols["name"]
	typeIdx, typeOK := cols["tool_type"]
	if !nameOK || !typeOK {
		return nil, errImportCSVMissingCols
	}
	// An absent schedule/inventory column (index -1) reads as "" via
	// toolCSVCell — treated exactly like an empty cell (NOT provided).
	scheduleIdx := -1
	if idx, ok := cols["schedule"]; ok {
		scheduleIdx = idx
	}
	invIdx := -1
	if idx, ok := cols["inventory_number"]; ok {
		invIdx = idx
	}

	// Physical file-line tracking: csv.Reader buffers ahead, so the record
	// index cannot express the true file line when a quoted cell contains an
	// embedded newline — compute each record's START line from the raw bytes.
	startLines := csvRecordStartLines(raw)

	rows := make([]toolscore.ToolImportRow, 0, len(records)-1)
	for i := 1; i < len(records); i++ {
		line := i + 1
		if i < len(startLines) {
			line = startLines[i]
		}
		record := records[i]
		rows = append(rows, toolscore.ToolImportRow{
			Line:            line,
			Name:            toolCSVCell(record, nameIdx),
			ToolTypeName:    toolCSVCell(record, typeIdx),
			ScheduleName:    toolCSVCell(record, scheduleIdx),
			InventoryNumber: toolCSVCell(record, invIdx),
		})
	}
	if len(rows) == 0 {
		return nil, errImportCSVNoRows
	}
	return rows, nil
}

// csvRecordStartLines returns the 1-based PHYSICAL line on which each logical
// CSV record STARTS, computed from the raw bytes with a quote-aware scan. A
// newline INSIDE a quoted cell does not end the record but still advances the
// physical line counter; only an unquoted `\n` is a record boundary. This is
// what makes error rows point at the true file line even when a cell contains
// an embedded newline (csv.Reader buffers ahead, so its record index cannot
// express this). The scan mirrors encoding/csv's quoting rules (`""` escape),
// so the boundaries it reports match the records csv.Reader returns.
func csvRecordStartLines(raw []byte) []int {
	lines := []int{1}
	physicalLine := 1
	inQuote := false
	for i := 0; i < len(raw); i++ {
		b := raw[i]
		if b == '\n' {
			// Every newline advances the physical line — whether or not it is a
			// record boundary (a quoted cell may contain a newline).
			physicalLine++
			if !inQuote {
				// An unquoted newline ends the logical record → the NEXT record
				// starts on the following physical line.
				lines = append(lines, physicalLine)
			}
			continue
		}
		if inQuote {
			if b == '"' {
				if i+1 < len(raw) && raw[i+1] == '"' {
					i++ // escaped quote inside a quoted cell
					continue
				}
				inQuote = false
			}
			continue
		}
		if b == '"' {
			inQuote = true
		}
	}
	return lines
}

// importParseErrorMessage maps the CSV parse sentinels to the German 400
// microcopy (the uniform-envelope message, surfaced verbatim in the SPA).
func importParseErrorMessage(err error) string {
	switch {
	case errors.Is(err, errImportCSVUnreadable):
		return "Die CSV-Datei konnte nicht gelesen werden."
	case errors.Is(err, errImportCSVEmpty):
		return "Die CSV-Datei ist leer."
	case errors.Is(err, errImportCSVNotUTF8):
		return "Die CSV-Datei ist nicht UTF-8-kodiert."
	case errors.Is(err, errImportCSVMissingCols):
		return "Die CSV-Datei muss die Spalten 'name' und 'tool_type' enthalten."
	case errors.Is(err, errImportCSVDupCols):
		return "Die CSV enthält doppelte Spalten."
	case errors.Is(err, errImportCSVNoRows):
		return "Die CSV-Datei enthält keine Datenzeilen."
	default:
		return "Die CSV-Datei konnte nicht gelesen werden."
	}
}

// toolCSVCell reads one column of a record, trimming whitespace and treating a
// missing column (a short row) or an empty cell as "" (= NOT provided).
func toolCSVCell(record []string, idx int) string {
	if idx < 0 || idx >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[idx])
}

// toImportResultDTO maps the domain import result to the wire payload: the
// imported count + the per-row errors (an empty slice serializes as `[]`,
// never null).
func toImportResultDTO(result *toolscore.ToolImportResult) toolImportResultDTO {
	out := toolImportResultDTO{Errors: []toolImportErrorDTO{}}
	if result == nil {
		return out
	}
	out.Imported = result.Imported
	for _, e := range result.Errors {
		out.Errors = append(out.Errors, toolImportErrorDTO{Row: e.Row, Reason: e.Reason})
	}
	return out
}