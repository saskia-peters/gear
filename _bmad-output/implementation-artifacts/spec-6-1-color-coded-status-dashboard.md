---
title: 'Color-Coded Status Dashboard (FR-16/AD-4/AD-5)'
type: 'feature'
created: '2026-09-14'
status: 'done'
review_loop_iteration: 0
baseline_commit: '242cfc26ef499ba9d8b018a102d11a0623a5b2fd'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-6-context.md'
  - '{project-root}/_bmad-output/implementation-artifacts/epic-5-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The dashboard is the operational landing surface but shows no real status: every row renders a static "verfügbar" label, the status chips do not filter, and the summary grid shows zeros. The derived-status function (Epic 5) exists server-side but nothing surfaces it.

**Approach:** Surface the derived status on the dashboard (Story 6.1, pulled forward by user decision): the tool-list endpoint returns each tool's derived status (via the shared clock function — never stored), the SPA renders color-coded rows + real counts, the summary grid counts become tappable, and the filter chips actually filter with multi-status support.

## Boundaries & Constraints

**Always:**
- **Backend `ListToolsForDashboard`** (`internal/tools/core/tools.go:274`): return per-tool derived status. Add `tt.default_schedule_id` to the `ListTools` query (`queries.sql:105`) + `core.Tool.DefaultScheduleID`. Resolve the schedule catalog ONCE per call (a pure `scheduleIntervalFor(overrideID, defaultID, schedules)` helper, extracted from `resolveToolScheduleInterval` at `inspections.go:375`), then per tool: `GetToolInspectionStatus` (`inspections.go:253`) + `deriveToolStatus` (`status.go:80`, `OrangeWindowDays`). New `core.DashboardTool{ Tool; Status ToolStatus }`; port `ListToolsForDashboard` returns `[]*core.DashboardTool`.
- **Resilience on the read path:** a tool whose effective schedule is missing/invalid, or whose status read errors, is logged and rendered `red` (NextDue nil) — the dashboard must never fail the whole list on one config defect, and never falsely claim serviceable (conservative red).
- **HTTP DTO** (`internal/tools/adapters/http/tools.go` `ListDashboardTools` L314 + `dashboardToolDTO` L49): add `status` (reuse the existing `statusDTO` `{ status, next_due }` shape). No other fields leak.
- **SPA types** (`web/src/auth/tools.ts`): `DashboardTool` gains `status: ToolStatusInfo` (`{ status: 'oos'|'red'|'orange'|'green'; next_due: string|null }`; share/alias the submit surface's status shape).
- **SPA status vocabulary** (`web/src/types/filters.ts`): add the code→label map `green→Einsatzbereit, orange→Ausstehend, red→Überfällig, oos→OOS_STATUS` (+ a stable class key per code). German labels stay the user-facing vocabulary.
- **DashboardPage** (`web/src/pages/DashboardPage.tsx`): replace the static `verfügbar` span (L172) with the derived label + color chip; filter `tools` by the active statuses (`statusLabel(tool.status.status)`); pass real `counts` to `SummaryGrid` and wire tappable cards; keep the start-button/row-error/empty-state behavior.
- **FilterChips** (`web/src/components/FilterChips.tsx`): multi-select — props become `selectedFilters: ReadonlySet<FilterStatus>` + `onToggleFilter`; "Alle" is active when the set is empty and clears it. `aria-pressed` per chip.
- **SummaryGrid** (`web/src/components/SummaryGrid.tsx`): cards become tappable buttons (`onToggleFilter(label)`), show real per-status counts; card colors already map green/orange/red/OOS.
- **Refresh:** unchanged — `navigate('/')` remounts the dashboard and refetches (statuses/colors/counts update after an inspection).
- **Tests:** backend core (never-inspected red, OOS, green+next_due, orange/red window, resilient-schedule-error), http (DTO status + no-leak regression), postgres (`ListTools` returns `default_schedule_id`), SPA (row label/color, single + multi filtering, counts, tappable cards, empty/all).

**Ask First:**
- None.

**Never:**
- No status column stored anywhere (AD-4 — derived on read only).
- No PDF export (6.2), no inspection history (6.3), no reinstatement surface (5.6).
- No change to the status derivation semantics or the schedule catalog.
- No changes to the inspection start/submit flow.
- No per-tool N+1 catalog reads (the schedule catalog is resolved once per dashboard call).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| DASH_NEVER | tool has no inspections | status `red`, next_due null | n/a |
| DASH_OOS | latest fail not since reinstated | status `oos` | n/a |
| DASH_GREEN | recent pass | status `green` + next_due = pass + interval | n/a |
| DASH_WINDOW | next_due ≤ 14d / past due | `orange` / `red` (+ next_due) | n/a |
| DASH_DTO | dashboard GET | per-tool `status { status, next_due }`; no other leaks | n/a |
| DASH_RESILIENT | effective schedule missing/invalid or status-read error | that tool `red`, list still 200 | log |
| SPA_ROW | tool rendered | derived German label + color chip replaces "verfügbar" | n/a |
| SPA_FILTER | a status selected | only matching tools visible | n/a |
| SPA_MULTI | several statuses active | union of the selected statuses | n/a |
| SPA_ALL | no status selected / "Alle" | full list | n/a |
| SPA_COUNTS | tools loaded | grid shows per-status totals | n/a |
| SPA_TAP | a grid card tapped | its status filter toggles | n/a |

</frozen-after-approval>

## Code Map

- `internal/tools/core/tools.go` -- `core.Tool` + `DefaultScheduleID` (L123-134); `DashboardTool` type; `ListToolsForDashboard` (L274-280) computes status per tool (schedule catalog read once).
- `internal/tools/core/inspections.go` -- extract `scheduleIntervalFor(overrideID, defaultID, schedules)` from `resolveToolScheduleInterval` (L375); keep the submit path using it.
- `internal/tools/adapters/postgres/queries.sql` (L105 `ListTools`) + `tools_repo.go` (`toolFromListRow` L281) -- add `tt.default_schedule_id AS default_schedule_id`; `just sqlc-generate`.
- `internal/tools/ports/ports.go` (L49-54) -- `ListToolsForDashboard` returns `[]*core.DashboardTool`.
- `internal/tools/adapters/http/tools.go` -- `dashboardToolDTO` (L49) + `ListDashboardTools` (L314): add `status` via the existing `statusDTO` (L98-101).
- `web/src/auth/tools.ts` -- `DashboardTool.status: ToolStatusInfo` (L254-260); share the status shape with the submit surface.
- `web/src/types/filters.ts` -- code→German-label map + class key (reuse `OOS_STATUS`).
- `web/src/pages/DashboardPage.tsx` -- row status chip (replaces L172 `verfügbar`), multi-status filter over `tools`, counts + tappable grid wiring.
- `web/src/components/FilterChips.tsx` (+css) -- multi-select props/behavior.
- `web/src/components/SummaryGrid.tsx` (+css) -- tappable cards + real counts.
- Tests: `tools_test.go` (ListToolsForDashboard status/resilience), `tools_test.go` http (DTO + leak regression), `tools_test.go` postgres (ListTools default_schedule_id), `DashboardPage.test.tsx`, `SummaryGrid.test.tsx`, `FilterChips.test.tsx`, `App.test.tsx`.

## Tasks & Acceptance

**Execution:**
- [x] `internal/tools/` (core + postgres + ports + http) -- per-tool derived status on `ListToolsForDashboard` + DTO -- backend
- [x] `web/src/types/filters.ts` + `web/src/auth/tools.ts` -- status vocabulary + `DashboardTool.status` -- SPA types
- [x] `web/src/pages/DashboardPage.tsx` -- row chip, real filtering, counts + grid wiring -- SPA
- [x] `web/src/components/FilterChips.tsx` + `SummaryGrid.tsx` -- multi-select + tappable counts -- SPA
- [x] Tests -- backend (status matrix/resilience/DTO/postgres), SPA (row/filter/multi/counts/tap) -- verification

**Acceptance Criteria:**
- Given I have `dashboard.view`, when the dashboard loads, then every tool is listed color-coded with the shared derived status — Red (past due or OOS), Orange (due within one quarter of the tool's own inspection cycle), Green (current), never-inspected Red (FR-16/AD-5).
- Given the derived statuses, when the dashboard renders, then status is computed on read via the single shared clock function, never stored (AD-4).
- Given the summary counts, when I tap a count block, then the matching status filter activates (FR-16/UX-DR5).
- Given the filter chips, when I tap a status chip, then the visible list filters to it, with multiple statuses active at once (FR-16/UX-DR5).
- Given a tool is inspected, when I return to the dashboard, then the status, counts and colors are refreshed (AD-4/UX-DR6).

## Spec Change Log

- **Review patches (review 1, 2026-09-14):** status-label/class lookups are defensive (unknown/empty code can't crash the row render; counts switch has a default); a distinct filtered-empty state with an "Alle anzeigen" reset replaces the misleading fleet-empty `EmptyState` when filters match nothing; zero-count summary cards are disabled (not tappable); the filter selection is keyed on the stable status CODE (`ReadonlySet<ToolStatusCode>`) with German labels only for display — label renames can no longer break filtering; the schedule catalog is not read for an empty fleet; `default_schedule_id` added to the HTTP no-leak regression; `.statusOos` dark-mode contrast fixed; refetch-on-remount, chip-deselect, fake-green-carries-next_due, chip-color-class, and nil-status-read tests added; trailing newlines + a required `SummaryGrid.onToggleFilter`; stale `main.go` comment corrected.
- **2026-09-19, post-done doc reconcile (retro item 31, user decision 2026-09-17):** the orange window is PROPORTIONAL to each tool's own inspection cycle (`window = interval * orangeWindowPercent / 100`, default 25 = one quarter), superseding the earlier fixed 14-day rule (commit 53dd5c2 + Story 5-2c configurable percentage). The Acceptance Criteria above now state the proportional rule; the frozen I/O matrix row DASH_WINDOW ("next_due ≤ 14d") is retained as the historical approved intent — the code and the AC reflect the proportional rule. The deviation is also recorded in `epic-6-context.md:18` and `epic-5-retro-2026-09-17.md:14,26,43`.

## Design Notes

- **Resilient dashboard read:** `ListToolsForDashboard` is a read path — a config defect (missing/invalid effective schedule) or a status-read error must not take the whole list down. Such a tool is logged and rendered `red` (never falsely green). The SUBMIT path keeps failing loudly (there the schedule is needed to record correctly).
- **Catalog read once:** the schedule catalog is resolved once per dashboard call and the pure `scheduleIntervalFor` helper resolves each tool against that snapshot — no N+1 catalog reads. The per-tool `GetToolInspectionStatus` reads (3 small queries each) are accepted at V1 fleet scale; a batched anchors query is a follow-up if fleets grow (noted, not built).
- **No effective schedule** (both ids empty) ⇒ `red` (no clock to compute next_due — conservative, never serviceable-by-default).
- **Vocabulary:** German labels (`Einsatzbereit`/`Ausstehend`/`Überfällig`/`Außer Betrieb`) remain the single user-facing filter/card vocabulary; the mapping to derived codes (`green`/`orange`/`red`/`oos`) lives in `types/filters.ts`.

## Verification

**Commands:**
- `just build` && `just vet` && `just test -p 1` && `just lint` -- expected: all Go/web tests pass, 0 lint issues
- `curl` as any holder: GET /api/v1/tools -- expected: 200 with per-tool `status { status, next_due }`
- `npx vitest run` in web/ -- expected: all pass incl. the dashboard filter/count/tap cases

**Manual checks (if no CLI):**
- Open the dashboard: rows show derived labels/colors (Red/Orange/Green/Außer Betrieb); tapping a summary count or a chip filters the list; multiple chips combine; after a failing inspection the tool returns as "Außer Betrieb".

## Suggested Review Order

**Backend status read**

- Entry point: `ListToolsForDashboard` — one schedule-catalog read, per-tool derived status, resilient (a bad tool → red, never a failed list).
  [`tools.go:297`](../../internal/tools/core/tools.go#L297)

- The resilient per-tool derivation + the never-inspected nil-guard.
  [`tools.go:338`](../../internal/tools/core/tools.go#L338)

- The pure effective-interval resolver (override → type default) shared with the submit path.
  [`inspections.go:398`](../../internal/tools/core/inspections.go#L398)

- `ListTools` now carries `default_schedule_id` (the dashboard needs the type default).
  [`queries.sql:105`](../../internal/tools/adapters/postgres/queries.sql#L105)

**HTTP DTO**

- Dashboard list handler + the shared `toStatusDTO` (status + next_due, reused by the submit surface).
  [`tools.go:315`](../../internal/tools/adapters/http/tools.go#L315)

**SPA vocabulary + filtering**

- The code→German-label/class-key vocabulary (single source; display labels can't break filter identity).
  [`filters.ts:30`](../../web/src/types/filters.ts#L30)

- Code-keyed filtering (multi-status union) + the distinct filtered-empty state with "Alle anzeigen".
  [`DashboardPage.tsx:120`](../../web/src/pages/DashboardPage.tsx#L120)

- The row status chip (derived label + color class) replacing the static "verfügbar".
  [`DashboardPage.tsx:249`](../../web/src/pages/DashboardPage.tsx#L249)

**Filter / grid components**

- Multi-select chips on status codes; "Alle" = empty set.
  [`FilterChips.tsx:16`](../../web/src/components/FilterChips.tsx#L16)

- Tappable count cards (zero-count disabled) that toggle the matching status.
  [`SummaryGrid.tsx:22`](../../web/src/components/SummaryGrid.tsx#L22)

**Tests**

- Status matrix, override-beats-default, resilience, empty-fleet-with-nil-port.
  [`tools_test.go:828`](../../internal/tools/core/tools_test.go#L828)

- SPA row color-class, filter/multi/all, counts, tap, refetch-on-remount.
  [`DashboardPage.test.tsx:364`](../../web/src/pages/DashboardPage.test.tsx#L364)