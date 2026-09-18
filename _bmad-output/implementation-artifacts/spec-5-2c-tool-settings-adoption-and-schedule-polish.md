---
title: 'Tool Settings Adoption + Schedule Catalog Polish (D1/C2/FR-30)'
type: 'feature'
created: '2026-09-18'
status: 'in-progress'
review_loop_iteration: 0
baseline_commit: '6102028643e2db89d11a9b7161da74f4fc4d6575'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-3-context.md'
  - '{project-root}/_bmad-output/implementation-artifacts/epic-4-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Three operational defaults are wrong or dead: the auto-assigned inventory number uses a hardcoded `'GEAR'` prefix + width **6** (the configurable `inventory_prefix`/`inventory_width` app_settings are never consumed); the inspection orange window is a hardcoded `interval/4` while the `inspection_orange_window_days` setting is dead data; and the schedule catalog lacks a `1 Woche` seed and sorts alphabetically instead of by duration.

**Approach:** Adopt the tool-related configurable settings (deferred-work's consumer-adoption item, scoped to the tool module): thread the existing `adminports.AppSettingsPort` into the tool core, use `inventory_prefix`/`inventory_width` in tool creation (default width → **9**), repurpose the orange-window setting as a **percentage of the inspection interval** (default **25** = the current 1/4, consumed by `deriveToolStatus`). Add a `1 Woche` seed schedule and sort the admin schedule list ascending by real duration (3 Tage first, 1 Jahr last).

## Boundaries & Constraints

**Always:**
- **AppSettingsPort seam** (`internal/admin/ports/ports.go` L123 `AppSettingsPort.CurrentAppSettings`, implemented by the Admin core Service): add it as a new param to `toolscore.NewService` (tool_types.go L231) wired in main.go L148 as the existing `adminSettingsService` (which implements it). The tool module reads settings through this read-only port — never a copy (AD-14/16).
- **Inventory number adoption** (`internal/tools`): `CreateTool` core reads `CurrentAppSettings` once → `InventoryPrefix` (default GEAR) + `InventoryWidth` (**default 9**); pass both into the repo + `CreateTool` SQL, replacing the hardcoded `'GEAR' || lpad(nextval(...), 6, '0')` with `$5 || lpad(nextval(...)::text, $6, '0')`. The `tools_inventory_number_max_length` CHECK (≤16) still holds (`GEAR` + 9 digits = 13). The unique-collision retry loop is unchanged.
- **Orange-window percentage** (`internal/admin/core/app_settings.go`): repurpose the D1 setting — rename `inspection_orange_window_days` → `inspection_orange_window_percent` (key, field `InspectionOrangeWindowPercent`, unit `Prozent`, min 1, default **25**), and thread it into the derivation. `deriveToolStatus` (status.go L84) gains an `orangeWindowPercent int` param and computes `window = interval * percent / 100` (the old `orangeWindowFraction = 4` constant is removed). Callers — `dashboardStatus` (tools.go L496), `SubmitInspection` + `ReinstateTool` (inspections.go L518/520/692) — resolve `CurrentAppSettings` ONCE per call (the dashboard already resolves the schedule catalog once; the settings read joins it, no N+1). The tools core gets an `appSettings adminports.AppSettingsPort` seam.
- **Migration `000031`** (up/down): (a) `INSERT INTO app_settings (key, value_type, int_value) VALUES ('inspection_orange_window_percent','integer',25) ON CONFLICT DO NOTHING` + `DELETE FROM app_settings WHERE key='inspection_orange_window_days'`; (b) `UPDATE app_settings SET int_value=9 WHERE key='inventory_width'` (only when the stored value is still the seeded 6 — do not clobber an admin-typed override); (c) `INSERT INTO schedules (name, interval_unit, interval_magnitude) VALUES ('1 Woche','week',1) ON CONFLICT DO NOTHING` (idempotent, and adds the missing seed the user wants). The Admin core catalog (`appSettingsCatalog`) gets the renamed key + unit + default; `AppSettingCatalogKeys` reflects it. sqlc unaffected (app_settings unchanged shape).
- **Admin schedules sort** (`internal/admin/adapters/postgres/queries.sql` `ListSchedules` L97): order by REAL duration ascending — add `interval_weight` via the unit→days mapping (year=365, quarter=91, month=30, week=7, day=1) so `3 Tage` (3d) < `1 Woche` (7d) < `2 Wochen` (14d) < `1 Monat` (30d) < `1 Quartal` (91d) < `1 Jahr` (365d). Simplest: `ORDER BY (CASE interval_unit WHEN 'day' THEN 1 WHEN 'week' THEN 7 WHEN 'month' THEN 30 WHEN 'quarter' THEN 91 WHEN 'year' THEN 365 ELSE 0 END) * interval_magnitude ASC, id ASC`. The `created_at ASC, name ASC` tiebreak is replaced.
- **SPA** (`AdminEinstellungenPage.tsx`): the System-tab catalog key/label/help updates for the renamed setting (label "Orange-Fenster Prüfung (Prozent des Prüfintervalls)", help explains 25 = ein Viertel des Prüfintervalls); the schedule list already renders server order, so the sort change appears automatically. The inventory width input needs no change (already the `inventory_width` key).
- **Tests:** admin core (renamed key resolves, width 9 default, percent default 25, update validation), tool core (CreateTool passes prefix/width from settings; dashboard/submit/reinstate use the percent window), postgres (CreateTool SQL with params, ListSchedules duration order incl. `1 Woche` seed, migration idempotency), http/composition, SPA (System tab shows the renamed setting + width 9; schedules render duration-ascending).

**Ask First:**
- None.

**Never:**
- No change to the status-color semantics (red/orange/green thresholds stay; only the WINDOW becomes a consumed percentage).
- No change to the dashboard/submit/reinstate flows beyond the window source.
- No new settings keys beyond the rename (the other 19 stay deferred-adoption).
- No removal of the `tools_inventory_number_max_length` CHECK or the UNIQUE retry semantics.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| CREATE_INVENTORY | settings width 9 | new tool number `GEAR` + 9-digit zero-padded nextval | retry loop unchanged |
| CREATE_INVENTORY_ADMIN | admin set width 5, prefix "WKZ" | number `WKZ` + 5-digit pad (consumed, not hardcoded) | n/a |
| ORANGE_PERCENT | settings percent 25 | window = interval × 25% (= interval/4, the old behavior) | n/a |
| ORANGE_PERCENT_CHANGED | admin set percent 50 | window = interval × 50% (derivation consumes it) | n/a |
| SEED_1WOCHE | fresh migrate | schedules catalog contains `1 Woche` (week,1) exactly once | idempotent (ON CONFLICT) |
| SCHEDULES_SORT | catalog seeded | list ordered 3 Tage < 1 Woche < 2 Wochen < 1 Monat < 1 Quartal < 1 Jahr | n/a |
| INVENTORY_WIDTH_SEED | fresh migrate, untouched setting | `inventory_width` = 9 | does NOT clobber an admin-typed override |

</frozen-after-approval>

## Code Map

- `internal/admin/core/app_settings.go` -- D1 def rename → `inspection_orange_window_percent` (L292-296), field rename (L122), unit `Prozent`, default 25; `inventory_width` default 9 in the catalog (L289-291).
- `internal/admin/adapters/postgres/queries.sql` L97-104 -- `ListSchedules` ORDER BY duration-ascending (CASE weight × magnitude, id tiebreak).
- `migrations/000031_*.{up,down}.sql` -- seed rename + width-update-if-6 + `1 Woche` schedule; README row.
- `internal/tools/core/tool_types.go` L231 -- `NewService` += `appSettings adminports.AppSettingsPort`; `Service` struct field.
- `internal/tools/core/tools.go` L297-330 (`ListToolsForDashboard`) + L496 (`dashboardStatus`) -- resolve `CurrentAppSettings` once per call, pass percent; `CreateTool` (L360+) resolves prefix/width → repo.
- `internal/tools/core/inspections.go` L518/520/692 -- submit/reinstate resolve percent once per call.
- `internal/tools/core/status.go` L41 (remove `orangeWindowFraction`) + L84 (param `orangeWindowPercent`) + L107 (`interval * percent / 100`).
- `internal/tools/adapters/postgres/queries.sql` `CreateTool` L146 -- `$5` prefix + `$6` width params; `tools_repo.go` `CreateToolParams` + `CreateTool` signature.
- `internal/tools/ports/ports.go` -- (no change to the Service interface; the settings seam is a constructor dep).
- `web/src/pages/admin/AdminEinstellungenPage.tsx` -- `SYSTEM_SETTING_META` D1 key/label/help rename; `SYSTEM_SETTING_GROUPS` "Prüfung & Qualifikation" key list.
- `cmd/server/main.go` L148 -- pass `adminSettingsService` as the new appSettings arg.
- Tests -- `internal/admin/core/app_settings_test.go`, `internal/tools/core/{tools,status,inspections}_test.go`, `internal/tools/adapters/postgres/{tools,inspections}_test.go`, `internal/admin/adapters/postgres/schedules_test.go`, `cmd/server/main_test.go`, `web/src/pages/admin/AdminEinstellungenPage.test.tsx`.

## Tasks & Acceptance

**Execution:**
- [ ] Migration 000031 (setting rename + width-9-if-6 + `1 Woche` seed) -- backend
- [ ] Admin core + schedules sort (D1 percent default, width 9, duration-ascending query) -- backend
- [ ] Tool adoption (AppSettingsPort into NewService; CreateTool prefix/width; deriveToolStatus percent) -- backend
- [ ] SPA System-tab D1 label/help + schedule order -- SPA
- [ ] Tests -- admin/tool core+postgres, schedules order, migration, SPA -- verification

**Acceptance Criteria:**
- Given a tool creation, when settings carry `inventory_prefix` + `inventory_width`, then the auto-assigned number uses them (default `GEAR` + 9 digits) (C2/adoption).
- Given the inspection clock, when the orange-window setting is 25 (%), then the window equals one quarter of the tool's inspection interval — the same behavior as before, now configurable (D1).
- Given the schedule catalog, when the admin opens Zeitpläne, then `3 Tage` … `1 Jahr` render ascending by real duration and `1 Woche` is seeded (FR-30).
- Given a fresh migrate, when the app_settings rows are read, then `inspection_orange_window_percent` exists with 25 and `inventory_width` reads 9 without clobbering an admin override.

## Spec Change Log

## Design Notes

- **The percentage keeps the current semantics:** the old constant was `interval/4` = 25%; the setting defaults to 25 so nothing changes until an admin edits it. `window = interval * percent / 100` for percent in 1..100.
- **Width override safety:** the migration bumps `inventory_width` to 9 ONLY when the stored value is still the seeded 6, so an admin who already typed a custom width keeps it (idempotent, non-destructive).
- **Settings read once per call:** the dashboard already resolves the schedule catalog once; adding the app-settings read alongside it keeps the no-N+1 invariant. Submit/reinstate resolve once per request.

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -count=1 -p 1 ./cmd/... ./internal/...` -- expected: all pass incl. the adoption + sort + migration cases
- `npx vitest run` in web/ -- expected: all pass incl. the System-tab + schedule-order cases
- `npm --prefix web run lint && npm --prefix web run typecheck && npm --prefix web run build` -- expected: clean

**Manual checks (if no CLI):**
- Create a tool → number `GEAR` + 9 digits; Zeitpläne → 3 Tage … 1 Jahr ascending incl. `1 Woche`; System → "Orange-Fenster Prüfung (Prozent …)" at 25, inventory width 9.