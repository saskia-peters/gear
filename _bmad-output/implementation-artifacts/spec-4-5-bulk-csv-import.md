---
title: 'Bulk CSV Import of Tools (FR-9/FR-23)'
type: 'feature'
created: '2026-09-19'
status: 'done'
review_loop_iteration: 0
baseline_commit: '02776bd3e33a1e0aa8c8816d1c482672d32c2815'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-4-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Tools can only be onboarded one-by-one via the Werkzeuge editor; a fleet migration (FR-9/FR-23) has no bulk path, so onboarding many tools is manual and duplicate rows could slip in silently.

**Approach:** Add a `tools.manage`-gated CSV import surface to the Tool catalogue — upload a CSV whose valid rows are created/updated and whose invalid rows are reported per-row (file line + German reason), with a result screen that summarizes counts and offers a downloadable error report. Execution is SET-PARTITIONED (new vs update) and batched so large files import in a handful of round-trips, and updating an existing tool NEVER loses data not present in the row.

## Boundaries & Constraints

**Always:**
- **Endpoint:** `POST /api/v1/admin/tools/import` (multipart, field `file`), mounted INSIDE the existing `tools.manage`-only write sub-gate in `ToolRoutes` (next to POST `/` and POST `/{id}/archive`). Core re-checks `tools.manage` only (AD-6) — a `tool.edit`-only holder gets the uniform 403.
- **CSV format:** UTF-8 text, `encoding/csv` (comma, trimmed leading space). First row is a HEADER; columns matched case-insensitively by name: `name` (required, the tool identifier / upsert match key), `tool_type` (required, by NAME), `schedule` (optional, by name), `inventory_number` (optional). Extra columns ignored; missing `name`/`tool_type` → German 400 BEFORE any row is processed. An empty `schedule`/`inventory_number` cell counts as NOT provided.
- **Set-partitioned execution (performance):** Phase A loads `ListToolTypes` + `CurrentSchedules` + `ListTools` into name→id/tool maps ONCE. Phase B validates every row and partitions the valid ones into `newSet` (name not in active tools) and `updateSet` (name already active); invalid rows become per-row errors. Phase C persists each set as ONE batched store call in its own transaction (see Store): a multi-row `INSERT … ON CONFLICT DO NOTHING RETURNING` for new (explicit inventory via `COALESCE`, else in-SQL auto-assign), and a multi-row `UPDATE … FROM (VALUES …)` for updates. Total ~2–4 round-trips + 2 commits regardless of row count — NO row-by-row round-trips.
- **Per-row atomicity (FR-9):** a row is validated fully BEFORE it is persisted; an invalid row produces NO tool record and is reported with its file line + German reason (e.g. "Zeile 4: Tool Type 'X' nicht gefunden"). Valid rows persist even when other rows fail. Within-file duplicate `name` or `inventory_number` → the LATER row is a row error.
- **No data loss on update (absolute):** an `updateSet` row changes ONLY fields the row provides: `tool_type_id` (always, resolved by name), `schedule_id` (only when the cell is non-empty — else PRESERVED, existing override kept), `inventory_number` (only when non-empty — else PRESERVED, never cleared). `attributes`, `name`, `archived_at` are NEVER touched by CSV. The batch `UPDATE` writes `tool_type_id` always and `schedule_id`/`inventory_number` via `CASE WHEN provided THEN value ELSE tools.<col> END` (or per-row fallback with the same semantics).
- **Collision precision:** a store pre-check (`FindToolCollisions`) reports exact-name / case-insensitive-inventory collisions against ALL rows (active + archived — the 4-3b backstop) so archived-name/archived-inventory rows fail with precise German row errors before the batch; `ON CONFLICT DO NOTHING` remains the race backstop (a row skipped by a concurrent collision → generic "bereits vergeben" row error, no abort).
- **Result:** HTTP 200 JSON `{ "imported": N, "errors": [{ "row": LINE, "reason": "DE" }] }` (empty errors → all rows OK). Malformed CSV, empty file, or no data rows → German 400. Body capped via `http.MaxBytesReader` (5 MB).
- **Audit:** ONE `tool.import` event per call (detail `imported=N errors=M`), not per row (NFR-O2 still records actor/time/op).
- **Template CSV:** the SPA offers **"Vorlage herunterladen"** next to "CSV importieren": a client-side generated `Blob` (`a[download]`, precedent: DSGVO JSON / status-PDF downloads) with the header + one example row (`name,tool_type,schedule,inventory_number` / a sample row with an empty schedule). The format guide stays inline in the import UI.
- **SPA:** Werkzeuge tab (visible only to `tools.manage` holders) gains "CSV importieren" + hidden file input, template download, a format guide, busy state, and a result view (imported/error counts, per-row error list, "Fehlerreport herunterladen" via client-side Blob from the JSON, "Erneut importieren"). Reload the tool list after import.

**Ask First:**
- None (set-partitioning, no-data-loss update semantics, optional inventory column, and the 5 MB cap follow from the AC + 4-3b backstop; an empty schedule/inventory cell is "not provided", so an existing override is preserved unless the cell is non-empty).

**Never:**
- No CSV parsing inside core — the HTTP adapter parses bytes into `[]core.ToolImportRow`; core receives structured rows (hexagon).
- No new DB migration, no new permission/role seed.
- No partial tool records (a row that fails validation is never persisted, even partly).
- No update of a DIFFERENT tool via inventory-number match; no clearing a tool's inventory number via CSV; no clearing an existing schedule override via an empty cell.
- No server-side stored error-report file — the downloadable report is built client-side from the JSON result.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| IMPORT_HAPPY | valid rows | imported=N, errors=[] | n/a |
| IMPORT_MIXED | valid + invalid rows | imported=valid-count, errors per invalid row (file line + German reason) | 200 (partial success by design) |
| IMPORT_TYPE_UNKNOWN | row tool_type not found | ROW error "Tool Type 'X' nicht gefunden", no tool | row error |
| IMPORT_SCHEDULE_UNKNOWN | row schedule not found | ROW error "Zeitplan 'X' nicht gefunden", no tool | row error |
| IMPORT_NAME_DUP | row name already active | UPDATES that tool (type/schedule/inventory) | n/a |
| IMPORT_INV_TAKEN | provided inventory held by another tool | ROW error duplicate-inventory, no write | row error |
| IMPORT_INV_LONG | inventory > 16 runes | ROW error | row error |
| IMPORT_INFILE_DUP | same name/inventory twice in file | LATER row is a ROW error | row error |
| UPDATE_PRESERVE_SCHEDULE | update row, empty schedule cell | Existing override kept (not cleared) | n/a |
| UPDATE_PRESERVE_INVENTORY | update row, empty inventory cell | Existing number kept (never cleared) | n/a |
| UPDATE_PRESERVE_ATTRIBUTES | update row | attributes JSONB untouched byte-for-byte | n/a |
| UPDATE_PROVIDED_FIELDS | update row, non-empty schedule/inventory | Those fields applied, others preserved | n/a |
| NEW_BATCH_RACE | concurrent insert takes a name/number mid-import | That row skipped by ON CONFLICT → generic "bereits vergeben" row error; rest commit | row error |
| IMPORT_MISSING_HEADER | no `name`/`tool_type` column | German 400 before processing | 400 |
| IMPORT_MALFORMED | unparseable CSV (bad quoting) | German 400 | 400 |
| IMPORT_EMPTY | empty file / header only | German 400 ("keine Datenzeilen") | 400 |
| IMPORT_TOO_LARGE | body > 5 MB | German 400 | 400 |
| IMPORT_FORBIDDEN | tool.edit-only / no tools.manage | uniform 403, no tool data (AD-6) | 403 |
| IMPORT_ARCHIVED_NAME | name held by an ARCHIVED tool | FindToolCollisions → ROW error duplicate-name | row error |
| TEMPLATE_DOWNLOAD | click "Vorlage herunterladen" | CSV blob with header + one example row | n/a |

</frozen-after-approval>

## Code Map

- `internal/tools/ports/ports.go` -- `Service` interface gains `ImportTools(ctx, actorID string, rows []core.ToolImportRow) (*core.ToolImportResult, error)`.
- `internal/tools/core/tools.go` -- new `ToolImportRow{Line, Name, ToolTypeName, ScheduleName, InventoryNumber}` / `ToolImportError{Row, Reason}` / `ToolImportResult{Imported, Errors}` / `ToolImportUpdate` types; `ImportTools` impl: one `requireToolsPermission(tools.manage)`, load maps once, validate + partition (new/update), store collision pre-check, batch calls, one summary audit. Reuse `validateToolInput`/inventory checks/messages (sentinel consts `tools.go:50-115`), `inventoryNumberFormat` (`tools.go:708-733`).
- `internal/tools/adapters/postgres/queries.sql` + `tools_repo.go` -- three additions (existing tx pattern `beginTx` `tool_types_repo.go:353` + `queries.WithTx` `db.go:28`):
  - `CreateToolsBatch(ctx, tools []*core.Tool, prefix, width) (created []*core.Tool, failed map[int]error, err error)` -- one tx, multi-row `INSERT … ON CONFLICT DO NOTHING RETURNING`; explicit inventory honored (`COALESCE`), else auto-assign; per-index failures = race collisions.
  - `UpdateToolsBatch(ctx, updates []core.ToolImportUpdate) ([]*core.Tool, []error, error)` -- one tx, multi-row `UPDATE … FROM (VALUES …)` preserving absent schedule/inventory + always preserving attributes; on a constraint violation → rollback + per-row fallback in the same tx to isolate/report the offender.
  - `FindToolCollisions(ctx, names, inventoryNumbers []string) (nameHits, inventoryHits map[string]struct{}, err error)` -- exact name + case-insensitive inventory over ALL rows (active + archived, the 4-3b backstop).
  - `isInventoryNumberCollision` (`tools_repo.go:447`) maps the explicit-inventory UNIQUE race to German duplicate-inventory in the per-row fallback.
- `internal/tools/adapters/http/tools.go` -- `ImportTools` handler (multipart parse, MaxBytesReader, `encoding/csv` decode, header mapping → `[]core.ToolImportRow` with file line numbers, call service, JSON result); register `POST /import` inside the write sub-gate (`tools.go:202-207`); reuse `mapToolError`.
- `web/src/auth/tools.ts` -- `importToolsCsv(file: File): Promise<ToolImportResult>` via `FormData` + `authTokenHeaders()` (new multipart call; keep JSON `request()` untouched); `toolImportTemplate(): Blob` for the downloadable template.
- `web/src/pages/admin/AdminWerkzeugePage.tsx` (+css) -- ToolsTab import control/result block + template download, gated on `canManageTools` (`AdminWerkzeugePage.tsx:45`).
- Tests: core (`tools_test.go` upsert/type/schedule/inventory/in-file-dup/preserve/permission, set partition), postgres (`tools_test.go` batch create+update, explicit-inventory, collision pre-check, preserved fields, race fallback), http (`tools_test.go` upload happy/400/403, header mapping), web (upload→result→template download→error-report download→reload).

## Tasks & Acceptance

**Execution:**
- [x] `internal/tools/core/tools.go` -- ImportTools + row/error/result/update types + validate→partition→batch flow -- core
- [x] `internal/tools/ports/ports.go` -- service method on the port -- API
- [x] `internal/tools/adapters/postgres/queries.sql` + `tools_repo.go` -- CreateToolsBatch / UpdateToolsBatch / FindToolCollisions -- persistence
- [x] `internal/tools/adapters/http/tools.go` -- multipart handler + route in the tools.manage sub-gate -- API
- [x] `web/src/auth/tools.ts` -- importToolsCsv (FormData multipart) + toolImportTemplate -- SPA
- [x] `web/src/pages/admin/AdminWerkzeugePage.tsx` (+css) -- import UI: format guide, template download, result, error-report download -- SPA
- [x] Tests -- core/postgres/http/web incl. I/O-matrix rows -- verification

**Acceptance Criteria:**
- Given a `tools.manage` holder on the Werkzeuge tab, when they upload a CSV with a valid header, then a format guide + downloadable template are available, rows are created/updated in a set-partitioned batch, and the result screen shows imported + error counts (FR-9/FR-23).
- Given a CSV with invalid rows, when the import runs, then each invalid row is reported with its file line number and a German reason, and NO tool record was created/partially written for it (FR-9).
- Given an update row whose schedule/inventory cell is empty, when the import runs, then the existing override/number (and the attributes) are preserved unchanged — no data is lost (FR-9, absolute).
- Given an import with errors, when the result screen is shown, then the errors are listed per-row and a downloadable error report is offered (FR-23).
- Given a caller lacking `tools.manage`, then the import answers uniform 403 with no tool data (AD-6).

## Spec Change Log

- **2026-09-19, pre-approval revision (user direction):** changed the execution model from per-row store calls to SET-PARTITIONED batching (load maps once → validate + split into new/update sets → one batched create tx + one batched update tx), added the ABSOLUTE no-data-loss rule for update rows (only provided fields applied; schedule/inventory preserved when empty; attributes/name/archived_at never touched), and added the downloadable template CSV to the SPA. Added `CreateToolsBatch`/`UpdateToolsBatch`/`FindToolCollisions` store methods and the `UPDATE_PRESERVE_*`/`NEW_BATCH_RACE`/`TEMPLATE_DOWNLOAD` I/O rows.

## Design Notes

- **Why set-partitioned:** row-by-row execution is O(N) round-trips and O(N) commits. Loading the three maps once removes N+1 lookups; a multi-row INSERT for the new set and a multi-row UPDATE (VALUES join) for the update set collapse the writes to ~2–4 round-trips and 2 commits. `ON CONFLICT DO NOTHING` keeps per-row atomicity within a batch: a colliding row is skipped + reported, the rest commit — the batch never aborts wholesale.
- **No-data-loss update semantics:** the CSV row is the diff, not the target state. Only `tool_type_id` is always applied; schedule/inventory apply only when their cell is non-empty; attributes have no CSV column and are preserved by not appearing in the UPDATE. This is what makes re-importing an existing fleet safe.
- **Collision precision + race backstop:** `FindToolCollisions` runs before any write so archived-name/archived-inventory rows get precise German errors (the 4-3b backstop is deliberately strict over all rows). A concurrent collision during the batch is absorbed by `ON CONFLICT DO NOTHING` → generic row error, never an abort.
- **Template CSV:** client-side `Blob` (no server endpoint) matches the existing DSGVO JSON and status-PDF download precedents; the header it generates IS the exact set of accepted column names, so the template and the parser can never drift.

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go/web tests pass, 0 lint issues
- `curl` as admin: `curl -s -H "Authorization: Bearer $TOKEN" -F "file=@tools.csv" http://localhost:8080/api/v1/admin/tools/import` with a valid CSV (200 imported=N), a mixed CSV (200 + per-row errors), a no-header CSV (400 German) -- expected per matrix
- Re-import the same CSV twice: second run updates the same tools (imported=N, no data loss on schedule/inventory/attributes) -- expected per UPDATE_PRESERVE_*
- `curl` as non-holder: import -- expected: uniform 403 with no data
- `npx vitest run` in web/ -- expected: all pass incl. import-UI + template tests

**Manual checks (if no CLI):**
- Werkzeuge tab → "CSV importieren": download the template, edit it (valid/invalid rows), upload; verify counts + per-row German errors, download the error report, confirm the list reloads; re-import to confirm existing schedule overrides/inventory numbers/attributes are preserved; a `tool.edit`-only holder sees no import control.

## Suggested Review Order

**Entry point — core ImportTools**

- Read this first: validates + partitions rows into new/update sets, then batch-persists each; the whole story's contract lives here
  [`tools.go:833`](../../internal/tools/core/tools.go#L833)

**Set-partitioned persistence**

- One-tx multi-row INSERT with ON CONFLICT DO NOTHING + savepoint fallback; explicit inventory via COALESCE
  [`tools_repo.go:572`](../../internal/tools/adapters/postgres/tools_repo.go#L572)
- One-tx multi-row UPDATE with CASE-when-provided preserve semantics + per-row fallback
  [`tools_repo.go:723`](../../internal/tools/adapters/postgres/tools_repo.go#L723)
- Pre-check that reports archived-name/archived-inventory collisions before any batch (the 4-3b backstop)
  [`tools_repo.go:890`](../../internal/tools/adapters/postgres/tools_repo.go#L890)

**HTTP surface**

- Multipart handler: UTF-8 check, header mapping, duplicate-column rejection, physical-line numbers, result JSON
  [`tools.go:663`](../../internal/tools/adapters/http/tools.go#L663)

**SPA import UI**

- Import flow: template download, format guide, upload, per-row result + error-report download, isolated reload
  [`AdminWerkzeugePage.tsx:470`](../../web/src/pages/admin/AdminWerkzeugePage.tsx#L470)
- Multipart client call (FormData, auth-only headers) + downloadable template Blob
  [`tools.ts:272`](../../web/src/auth/tools.ts#L272)

**Spec + tracking**

- The plan this implements: set-partitioning, no-data-loss updates, template CSV
  [`spec-4-5-bulk-csv-import.md:20`](../../_bmad-output/implementation-artifacts/spec-4-5-bulk-csv-import.md#L20)

**Tests**

- Core: upsert/preserve/in-file-dups/archived-backstop/race/permission matrix
  [`import_test.go:52`](../../internal/tools/core/import_test.go#L52)
- HTTP: upload happy/mixed/400s/403/too-large/empty/duplicate-cols/UTF-8 + parser unit tests
  [`import_test.go:55`](../../internal/tools/adapters/http/import_test.go#L55)
- Postgres: batch create/update, preserve, fallback, collisions
  [`import_test.go:1`](../../internal/tools/adapters/postgres/import_test.go#L1)
- Web: upload→result→template/error-report downloads→reload, tools.manage gate
  [`AdminWerkzeugePage.test.tsx:484`](../../web/src/pages/admin/AdminWerkzeugePage.test.tsx#L484)