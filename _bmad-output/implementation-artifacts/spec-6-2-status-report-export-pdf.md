---
title: 'Status Report Export (PDF) (FR-17/AD-6)'
type: 'feature'
created: '2026-09-17'
status: 'in-progress'
review_loop_iteration: 0
baseline_commit: 'af02dadff8c4484459ba65e1c3636f9e36b07a96'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-6-context.md'
  - '{project-root}/_bmad-output/implementation-artifacts/spec-6-1-color-coded-status-dashboard.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Fuehrung/Admin cannot export the dashboard's current status view — audits and readiness reviews have no shareable artifact (FR-17).

**Approach:** A server-side Go PDF report (`GET /api/v1/tools/report.pdf`), gated by `report.export` (AD-6, no data exposed on 403), rendered from the SAME derived statuses as the dashboard. The SPA passes the ACTIVE status filters as query params (`?status=green,oos`; absent = "Alle"); the server re-derives each tool's status itself (never trusts the client) and includes only the matching tools. Columns: Name · Typ · Status · Zuletzt geprüft · Prüfer/in (last inspection date + inspector, resolved via the existing DisplayNameResolver seam). The export control is hidden from non-holders.

## Boundaries & Constraints

**Always:**
- **Backend endpoint** `GET /api/v1/tools/report.pdf` (NEW `ReportRoutes()` in `internal/tools/adapters/http/tools.go`, mirroring `HistoryRoutes` L254): gated by `auth.RequirePermission(sessionManager, userRepo, toolscore.ReportExportPermission)` mounted in main.go as `toolsSurface.Mount("/report.pdf", reportSurface)`. New const `ReportExportPermission = "report.export"` in `core/inspections.go` (next to `InspectionHistoryViewPermission` L46). Returns `application/pdf`; a malformed `status` param (not in `green|orange|red|oos`) answers the German 400 envelope; 403 → uniform envelope, no PDF.
- **Core** `Service.ExportStatusReport(ctx, actorID, filterCodes)` (in `core/tools.go` next to `ListToolsForDashboard`): permission re-check; resolves the schedule catalog once; per tool derives status via the existing `dashboardStatus` (L338) AND reads the tool's LATEST inspection (any result) → `last_inspected_at` + `last_inspector_id`; filters to `filterCodes` (empty = all); resolves ALL inspector ids in ONE `DisplayNameResolver.ResolveDisplayNames` call (missing user → "Deleted User", the 6.3 seam L198). Returns `[]*core.ReportRow{ Tool DashboardTool; LastInspectedAt *time.Time; LastInspectorName string }`.
- **New store read** `GetLatestInspectionByTool :one` in `queries.sql` (inspection section, after L271): `SELECT submitted_at, inspector_id FROM inspections WHERE tool_id=$1 ORDER BY submitted_at DESC, id DESC LIMIT 1` (the 000027 `(tool_id, submitted_at DESC)` index serves it). Repo method on `InspectionStore` (`core/inspections.go` L305-313).
- **PDF rendering** (`internal/tools/adapters/http/report.go` NEW): small pure-Go table via `github.com/go-pdf/fpdf` (add to `go.mod`): report title "G.E.A.R. – Statusbericht", generated-at timestamp, a caption naming the active filter ("Alle Status" or the German labels, reusing `green→Einsatzbereit` vocabulary), and a header row Name | Typ | Status | Zuletzt geprüft | Prüfer/in. Status column uses the German labels (`Einsatzbereit/Ausstehend/Überfällig/Außer Betrieb`); last-inspected renders the date (server-local, RFC3339 date part); never-inspected renders "–". Columns wrap; ~A4 portrait. German throughout.
- **SPA** (`web/src/pages/DashboardPage.tsx`): an "Als PDF exportieren" button in the dashboard header row, rendered ONLY for `hasPermission('report.export')` (hide = AD-6 UX). On click it builds `?status=…` from `selectedFilters` (empty set → no param), fetches `GET /api/v1/tools/report.pdf` as a blob and triggers a download (`a[download]`/`URL.createObjectURL`), or `window.open` the URL. 401 → login; 403 → inline German error (stale permission cache); other → inline German. Button ≥48px, disabled while the fetch is in flight.
- **Client const** `REPORT_EXPORT_PERMISSION = 'report.export'` in `web/src/auth/tools.ts` (mirror `REINSTATE_PERMISSION` L29).
- **Tests:** backend core (permission 403, filter pass/empty/unknown-code, last-inspection resolution incl. never-inspected "–" and Deleted-User fallback, order), postgres (`GetLatestInspectionByTool` newest + tiebreak + empty), http (200 content-type + non-empty body, 403 gate, 400 bad status, 401), main composition mount gate; SPA (button hidden for non-holder, params from active filters, blob download fires, 401/403 inline). PDF bytes are asserted as non-empty + `%PDF` magic, not golden-tested.

**Ask First:**
- None.

**Never:**
- No change to status derivation, the dashboard list contract, or the inspection flows.
- No new data exposed on the dashboard DTO (the report is a separate surface; `report.export` is the only gate).
- No client-side PDF generation (the server renders it; the client only downloads).
- No N+1 catalog reads (schedule catalog resolved once).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| REPORT_OK | holder, no status param | 200 `application/pdf` with ALL active tools (Name/Typ/Status/Zuletzt/Prüfer) | n/a |
| REPORT_FILTERED | `?status=green,oos` | 200 PDF with only the matching tools (server-derived) | n/a |
| REPORT_NEVER | a tool never inspected | "–" in Zuletzt geprüft + Prüfer/in columns | n/a |
| REPORT_DELETED_USER | inspector account gone | "Deleted User" in Prüfer/in | n/a |
| REPORT_GATED | caller lacks `report.export` | 403 uniform envelope, NO PDF bytes | 403 |
| REPORT_BAD_CODE | `?status=grün` (unknown) | 400 German envelope | 400 |
| REPORT_401 | expired/revoked session | 401 envelope | 401 |
| SPA_BUTTON | holder renders dashboard | "Als PDF exportieren" visible; non-holder: hidden | n/a |
| SPA_DOWNLOAD | export clicked | blob downloaded; active filters in `?status=` | n/a |

</frozen-after-approval>

## Code Map

- `internal/tools/core/inspections.go` -- `ReportExportPermission` const (after L46); `InspectionStore` (L305-313) += `GetLatestInspectionByTool`.
- `internal/tools/core/tools.go` -- `ReportRow` type + `Service.ExportStatusReport` (next to `ListToolsForDashboard` L297): schedule catalog once, per-tool `dashboardStatus` (L338) + latest-inspection read, filter, one `ResolveDisplayNames` call.
- `internal/tools/adapters/postgres/queries.sql` -- `GetLatestInspectionByTool :one` (after L271) + generated; `inspections_repo.go` repo method.
- `internal/tools/ports/ports.go` -- `Service` += `ExportStatusReport(ctx, actorID string, filterCodes []string) ([]*core.ReportRow, error)`.
- `internal/tools/adapters/http/report.go` NEW -- `ReportRoutes()`, `ExportStatusReport` handler (401 guard, `user := auth.UserFrom`, parse `status` query split-by-comma validating each against the 4 codes → 400), fpdf table renderer, `mapReportError` (403/400/404/500).
- `internal/tools/adapters/http/tools.go` -- reuse `dashboardToolDTO` (L48) + `toStatusDTO` for the row header; the report handler builds its own PDF rows.
- `go.mod` -- add `github.com/go-pdf/fpdf`.
- `cmd/server/main.go` -- mount `reportSurface := auth.RequirePermission(sessionManager, userRepo, toolscore.ReportExportPermission)(toolHandler.ReportRoutes())` and `toolsSurface.Mount("/report.pdf", reportSurface)` (after the history mount L218).
- `web/src/auth/tools.ts` -- `REPORT_EXPORT_PERMISSION` const (after L29) + `exportStatusReportPdf(params)` client (blob fetch).
- `web/src/pages/DashboardPage.tsx` -- export button (header row) gated by `hasPermission`; builds `?status=` from `selectedFilters`; blob download; inline error handling.
- Tests -- `internal/tools/core/tools_test.go`, `internal/tools/adapters/postgres/inspections_test.go`, `internal/tools/adapters/http/tools_test.go` + NEW `report_test.go`, `cmd/server/main_test.go`, `web/src/pages/DashboardPage.test.tsx`.

## Tasks & Acceptance

**Execution:**
- [ ] Backend: report core + store read + HTTP/PDF + mount/gate -- backend
- [ ] SPA: export button (gated) + blob download + filter params -- SPA
- [ ] Tests -- backend (core/order/filter/403/400/Deleted-User/never) + http (pdf magic, 403/400/401) + composition + SPA (button/blob/401/403) -- verification

**Acceptance Criteria:**
- Given a `report.export` holder with active filters, when they export, then a PDF of the currently filtered tool list downloads, each row naming tool, type, status, last inspection date and last inspector (FR-17/AD-5).
- Given a non-holder, when they attempt the export, then the server answers 403 with no data and the button is hidden (AD-6).
- Given the report renders, when a tool was never inspected or its inspector was deleted, then the columns read "–" / "Deleted User" without error.

## Spec Change Log

## Design Notes

- **Server re-derives, never trusts:** the SPA sends only the filter codes; the PDF's statuses come from the same `deriveToolStatus` the dashboard uses, so the export can never disagree with the on-screen truth (FR-17/AD-5).
- **One name resolution:** all last-inspector ids across rows are resolved in a single `ResolveDisplayNames` call; missing users → "Deleted User" (the 6.3 seam), no per-row user reads.
- **N+1 accepted at V1 fleet scale** (matches the 6.1 deferred note): per tool the report does the existing 3-query status read + one latest-inspection read; a batched anchors query is the same follow-up as the dashboard's.

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -count=1 -p 1 ./cmd/... ./internal/...` -- expected: all pass incl. the report core/http/postgres/mount cases
- `npx vitest run` in web/ -- expected: all pass incl. the export-button cases
- `npm --prefix web run lint && npm --prefix web run typecheck && npm --prefix web run build` -- expected: clean

**Manual checks (if no CLI):**
- As Fuehrung/Admin on the dashboard: the export button downloads a PDF; filtering by a status then exporting yields only those tools; a Helfer*in sees no button and a direct URL answers 403.