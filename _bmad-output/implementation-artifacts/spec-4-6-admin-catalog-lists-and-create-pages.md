---
title: 'Admin Catalog: Compact Sortable Lists + Create/Edit Pages (FR-8/FR-9/FR-30)'
type: 'feature'
created: '2026-09-17'
status: 'done'
review_loop_iteration: 0
baseline_commit: '53dd5c2f4b74541cae1e70fe0c21d0b5e6397d3d'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-4-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The admin catalogue surfaces mix the create/edit FORM with the LIST on the same screen — "Neuer Gerätetyp"/"Neues Werkzeug"/"Neuer Zeitplan" render at the top of the list, so the list is pushed down, rows are tall cards, and nothing is sortable. On tool types + tools (AdminWerkzeugePage tabs) and schedules (Einstellungen → Zeitpläne tab).

**Approach:** Move create AND edit to dedicated pages; the list screen keeps ONLY the list (compact, sortable, one line per row) plus a "Create new …" button. Each entity gets a create page and a reuse-able edit page sharing the existing editor component; "Bearbeiten" on a row navigates to the edit page. German UI throughout, ≥48px targets, the same 401→login / 403→leave-module handling.

## Boundaries & Constraints

**Always:**
- **Tool types list** (`AdminWerkzeugePage` `ToolTypesTab` L145-539): remove the inline `<form>` (L350-489); the list becomes ONE line per row — name · mode (Pass/Fail|Checkliste · N Punkte) · Edit button — with NO checklist-label dump. A "Create new" button (`Neuer Gerätetyp`, gated `tool_types.manage`) above the list navigates to `/admin/werkzeuge/typen/neu`; the per-row "Bearbeiten" navigates to `/admin/werkzeuge/typen/:id`. Rows render as compact one-line flex (no stacked meta), sortable by Name and Modus.
- **Tools list** (`ToolsTab` L551-950): remove the inline `<form>` (L762-902); one line per row — name · type · inventory number · Edit button (Archive for `tools.manage` holders). "Create new" (`Neues Werkzeug`, gated `tools.manage`) → `/admin/werkzeuge/tools/neu`; per-row edit → `/admin/werkzeuge/tools/:id`. Sortable by Name, Typ, Gerätenummer.
- **Schedules list** (`AdminEinstellungenPage` `ScheduleSettingsTab` L914-1151): remove the inline `<form>` (L1038-1115); one line per row — name · interval display (`scheduleDisplay` L75) · Edit button. "Create new" (`Neuer Zeitplan`, gated `schedules.manage`) → `/admin/einstellungen/zeitplaene/neu`; per-row edit → `/admin/einstellungen/zeitplaene/:id`. Sortable by Name and Intervall.
- **Create pages** (NEW `AdminToolTypeEditorPage`, `AdminToolEditorPage`, `AdminScheduleEditorPage`): each is a full page (Header + AdminNav + title + back button "← Zurück zur Liste") wrapping the EXISTING editor form in "create" mode (empty form, auto-selected first schedule/type). Create submits `createToolType`/`createTool`/`createSchedule`, then navigates back to the list. Guarded by the entity's manage permission (`RequireAdminEntry` codes).
- **Edit pages** (same three page components, "edit" mode from `:id`): load the entity by id from the list fetch (`listToolTypes`/`listTools`/`listSchedules` find-by-id), pre-fill the EXISTING `startEdit` fields, submit `updateToolType`/`updateTool`/`updateSchedule`, then back to the list. Unknown id → German "nicht gefunden".
- **Sortable lists** (SPA-only): each list header is a clickable column that toggles asc/desc (German labels, `localeCompare('de', {sensitivity:'base'})`, mirroring `UserTable.tsx` L60). Default sort: Name asc. Sort buttons carry `aria-sort`-equivalent accessible names ("Sortieren nach Name").
- **Shared editor extraction:** the existing form JSX (tool-type editor fields, tool editor fields, schedule editor fields) moves into the editor PAGE components verbatim (fields, validation, `canSave`, `AttributesEditor`, `ChecklistEditor`), so create and edit share one form. The list components no longer hold form state (`name`/`setName`/`editingId`/`busy`/`feedback` for forms).
- **Routing** (`App.tsx` L380-403): add six routes under the `RequireAdminModule`+`RequireAdminEntry` wrappers matching the adminNav codes (`werkzeuge`/`einstellungen`): `/admin/werkzeuge/typen/neu`, `/admin/werkzeuge/typen/:id`, `/admin/werkzeuge/tools/neu`, `/admin/werkzeuge/tools/:id`, `/admin/einstellungen/zeitplaene/neu`, `/admin/einstellungen/zeitplaene/:id`. Order matters: `:id` routes before any broader catch.
- **Tests:** `AdminWerkzeugePage.test.tsx` + `AdminEinstellungenPage.test.tsx` rewritten for list-only + navigation; NEW editor-page tests (create submit + redirect, edit pre-fill + update, unknown id, 401/403); sorting tests per list.
- **Feedback:** success/error feedback moves to the editor pages (inline after submit, then navigate back); the list shows a load error only.

**Ask First:**
- None.

**Never:**
- No backend changes (all endpoints exist).
- No change to the create/edit FIELD sets or validation (the editor forms move, not change).
- No change to archive/delete flows.
- No change to the E-Mail / Backup / System tabs (schedules only).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| LIST_TYPES | typen tab, ≥1 type | one-line rows (name · mode · edit), create button, sortable | load error inline |
| LIST_TOOLS | werkzeuge tab, ≥1 tool | one-line rows (name · type · inventory · edit), create button (manage holders), sortable | load error inline |
| LIST_SCHEDULES | zeitpläne tab, ≥1 schedule | one-line rows (name · interval · edit), create button, sortable | load error inline |
| CREATE_NAV | "Create new" clicked | navigates to the dedicated create page | n/a |
| CREATE_SUBMIT | valid form submitted | create POST succeeds → navigate back to the list | 400 inline German, 401→login, 403→leave module |
| EDIT_NAV | "Bearbeiten" clicked | navigates to `:id` edit page with the entity pre-filled | n/a |
| EDIT_UNKNOWN | :id not in list | German "nicht gefunden", no form | n/a |
| SORT | a column header clicked | rows re-sorted asc/desc by that key | n/a |
| EMPTY | list empty | "Keine … vorhanden" note + create button still visible | n/a |

</frozen-after-approval>

## Code Map

- `web/src/pages/admin/AdminWerkzeugePage.tsx` (1058 lines) -- `ToolTypesTab` (L145-539): drop the `<form>` L350-489 + form state (name/schedule/qual/mode/checklist/attributes/formVersion L154-175); keep `listToolTypes` load (L177-224) + archive (L295-316); add sort + one-line rows + create/edit navigation. `ToolsTab` (L551-950): drop the `<form>` L762-902 + form state (L560-574); keep load (L576-622) + archive (L702-723); add sort + one-line rows + navigation.
- `web/src/pages/admin/AdminToolTypeEditorPage.tsx` (+css) NEW -- the tool-type editor form (moved verbatim from `ToolTypesTab` L350-489: name/schedule/qualification/mode/checklist/attributes + `canSave` L325 + `save` L257-293 create branch) in create/edit modes; `:id` edit loads via `listToolTypes().find`; back button; 401/403 handling.
- `web/src/pages/admin/AdminToolEditorPage.tsx` (+css) NEW -- the tool editor form (from `ToolsTab` L762-902 incl. the readonly auto-inventory + `AttributesEditor` + `canSave` L731) in create/edit modes; create mode ONLY for `tools.manage` (tool.edit-only holders reach edit mode via the list); `:id` edit loads via `listTools().find`.
- `web/src/pages/admin/AdminEinstellungenPage.tsx` (1500+ lines) -- `ScheduleSettingsTab` (L914-1151): drop the `<form>` L1038-1115 + form state (L921-924); keep `listSchedules` load (L926-946) + archive (L994-1013); add sort + one-line rows + navigation. E-Mail/Backup/System tabs untouched.
- `web/src/pages/admin/AdminScheduleEditorPage.tsx` (+css) NEW -- the schedule editor form (from `ScheduleSettingsTab` L1038-1115 incl. `INTERVAL_OPTIONS` L60-66 + `scheduleDisplay` L75-80) in create/edit modes; `:id` edit loads via `listSchedules().find`.
- `web/src/App.tsx` (L380-403) -- add the six editor routes under `RequireAdminModule`+`RequireAdminEntry` (codes from `adminNavCodes('werkzeuge')`/`('einstellungen')`), `:id` routes before their siblings.
- `web/src/components/UserTable.tsx` (L60, L112-145) -- the sortable-column + clickable-row pattern to mirror (NOT modified).
- `web/src/pages/admin/AdminWerkzeugePage.module.css` / `AdminEinstellungenPage.module.css` -- compact one-line row styles + sortable header styles (reuse the existing `row`/`rowInfo`/`rowActions`/`list` tokens where possible, tighten vertical padding).
- Tests -- `AdminWerkzeugePage.test.tsx` (rewrite list/nav cases), NEW `AdminToolTypeEditorPage.test.tsx`, `AdminToolEditorPage.test.tsx`, `AdminScheduleEditorPage.test.tsx`, `AdminEinstellungenPage.test.tsx` (rewrite schedules cases).

## Tasks & Acceptance

**Execution:**
- [x] Extract the three editor forms into dedicated create/edit page components -- SPA
- [x] Reduce the three list components to compact sortable one-line lists + create/edit navigation -- SPA
- [x] Add the six admin routes in App.tsx -- SPA
- [x] Rewrite/add tests (list+sort+create+edit+401/403) -- verification

**Acceptance Criteria:**
- Given a catalogue tab (Typen/Werkzeuge/Zeitpläne), when it renders, then ONLY a compact sortable list + a "Create new" button show — no inline create/edit form (FR-8/FR-9/FR-30).
- Given I click "Create new", when the page loads, then a dedicated create page with the full editor renders and saving navigates back to the list.
- Given I click "Bearbeiten" on a row, when the page loads, then the entity is pre-filled and saving updates it and navigates back.
- Given a column header, when I click it, then the list sorts by that column asc/desc (German locale).

## Spec Change Log

- **Review patches (review 1, 2026-09-17):** editors now carry a German success message back to the list via navigate state (cleared on read); the schedule "Intervall" sort compares real durations (unit weight × magnitude), not rendered text; the schedule editor gained a `canSave` gate (name + magnitude ≥ 1, hint text); the Werkzeuge page preserves the active tab across create/edit navigation; the "Zeitplan überschrieben" override badge was re-added to tool rows; a 401 on a secondary catalog fetch in the editors now logs in (create mode too) while 403/other still degrade; tool-type edit clears stale schedule/qualification ids absent from the loaded catalogs; schedule edit-load failure shows an error with NO form; unknown schedule interval unit falls back to 'year'; the tool "Typ" sort null-guards `tool_type_name`; the readonly inventory input got `aria-describedby`; App-level route-gating tests added for all six editor routes; editor degradation/empty-catalog/load-500/save-401/403 tests added; trailing newlines restored. KEEP: the one-line sortable lists, the six-route split, the moved-verbatim editor forms, and the degradation-to-empty-dropdown behavior all stand as implemented.

## Design Notes

- **Edit mode reuses the list fetch:** there is no `getToolType(id)`/`getTool(id)`/`getSchedule(id)` client — the editor pages load the full list and find by id (same data the list already fetches; the pattern `listTools().find` is cheap at catalogue scale). Unknown id → German not-found, no form.
- **Sorting is presentation-only:** the SPA sorts the already-loaded arrays (server returns a stable order); no query params, no backend sort — the sort state resets on remount.

## Verification

**Commands:**
- `npx vitest run` in web/ -- expected: all pass incl. the rewritten list/nav cases + new editor-page tests
- `npm --prefix web run lint && npm --prefix web run typecheck && npm --prefix web run build` -- expected: clean

**Manual checks (if no CLI):**
- Open Werkzeuge → Typen: one-line sortable list + "Neuer Gerätetyp"; create page saves and returns; edit pre-fills. Same for Werkzeuge and Einstellungen → Zeitpläne.

## Suggested Review Order

**List rework (entry point)**

- Tool types list: one-line sortable rows + create/edit navigation.
  [`AdminWerkzeugePage.tsx:145`](../../web/src/pages/admin/AdminWerkzeugePage.tsx#L145)

- Tools list: one-line rows (name · type · inventory · override badge) + navigation.
  [`AdminWerkzeugePage.tsx:388`](../../web/src/pages/admin/AdminWerkzeugePage.tsx#L388)

- Schedules list: one-line rows + duration-based sort.
  [`AdminEinstellungenPage.tsx:741`](../../web/src/pages/admin/AdminEinstellungenPage.tsx#L741)

**Editor pages**

- Tool-type create/edit page (fields, stale-field guards, best-effort catalogs).
  [`AdminToolTypeEditorPage.tsx:60`](../../web/src/pages/admin/AdminToolTypeEditorPage.tsx#L60)

- Tool create/edit page (create gated on tools.manage, readonly auto-inventory).
  [`AdminToolEditorPage.tsx:55`](../../web/src/pages/admin/AdminToolEditorPage.tsx#L55)

- Schedule create/edit page (canSave gate, load-failure no-form).
  [`AdminScheduleEditorPage.tsx:40`](../../web/src/pages/admin/AdminScheduleEditorPage.tsx#L40)

**Routing**

- The six editor routes under the admin guards.
  [`App.tsx:394`](../../web/src/App.tsx#L394)

**Tests**

- App-level route gating for the editor routes.
  [`App.test.tsx:830`](../../web/src/App.test.tsx#L830)

- Editor degradation / empty-catalog / load-500 / save-401/403.
  [`AdminToolEditorPage.test.tsx:300`](../../web/src/pages/admin/AdminToolEditorPage.test.tsx#L300)