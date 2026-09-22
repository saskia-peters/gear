---
title: 'Dashboard Row: One-Line Layout + Click-to-Details Navigation (FR-16)'
type: 'feature'
created: '2026-09-17'
status: 'done'
review_loop_iteration: 0
baseline_commit: '3dc6ae18f1ef1bed8989b573be9322c4e0f52363'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-6-context.md'
  - '{project-root}/_bmad-output/implementation-artifacts/spec-6-3-inspection-history-per-tool.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The dashboard rows stack name/type/Gerätenummer vertically (two-level layout) and are not clickable — the Story 6.3 details page (`/tools/:toolId`) exists but is reachable only by URL, and the deferred-work ledger records the row rework as the follow-up.

**Approach:** Rework the dashboard row into a ONE-LINE layout (status chip, name, type, Gerätenummer, action buttons) and make the row itself an interactive target: clicking anywhere except a button opens the tool's details page (`/tools/:toolId`) carrying the header data as router state.

## Boundaries & Constraints

**Always:**
- **One-line row** (`web/src/pages/DashboardPage.tsx` row JSX L322-378 + `DashboardPage.module.css` L109-165): flatten `.rowInfo` (the name/type/Gerätenummer column) into a single horizontal flex line with the status chip, name, type, Gerätenummer and the action buttons; `flex-wrap` so it degrades on narrow screens. No vertical stacking of the info fields. Keep the existing button styles, min-heights and the `.statusChip` colors.
- **Clickable row** (UserTable pattern `web/src/components/UserTable.tsx` L112-145): the `<li>` gets `onClick` → navigate, `onKeyDown` (Enter/Space, preventDefault) → navigate, `tabIndex={0}` and `aria-label="Details für {name} öffnen"`. Row click navigates to `/tools/{toolId}` with state matching the EXISTING `ToolDetailsState` interface in `ToolDetailsPage.tsx` L15-20: `{ tool_name: tool.name, tool_type_name: tool.tool_type_name, inventory_number: tool.inventory_number, status: tool.status }`. This is the same state shape 6.3 already consumes — do not change `ToolDetailsPage`.
- **Button isolation:** the "Prüfung starten" and "Wiederherstellen" buttons, and the reinstate dialog-opener click, must `stopPropagation` so their actions run WITHOUT row navigation. The row-error `<p>` must never trigger navigation. The reinstate `PromptDialog` must not be opened by a row click.
- **No behavior changes** to the start/reinstate/filter/count/empty states — only the row's container layout and clickability change.
- **Deep-link:** the row navigation passes state, but the details page already handles a missing state (dashboard-list refetch) — unchanged.

**Ask First:**
- None.

**Never:**
- No changes to the details page, the history endpoint, or the status/filter semantics (6.1/6.3 are done).
- No changes to the start/reinstate flows or their gates.
- No new backend changes.
- No stored status (AD-4).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| ROW_LAYOUT | a tool row renders | status chip + name + type + Gerätenummer + buttons on ONE line (wraps on narrow screens) | n/a |
| ROW_NAV | row (not a button) clicked | navigates to /tools/{toolId} with the header state | n/a |
| ROW_KEYBOARD | row focused, Enter or Space pressed | navigates to /tools/{toolId} (no double nav) | n/a |
| ROW_START | "Prüfung starten" clicked | start flow runs, NO navigation | n/a |
| ROW_REINSTATE | "Wiederherstellen" clicked (OOS + holder) | reinstate dialog opens, NO navigation | n/a |
| ROW_ERROR | a row error is present | clicking the error area does not navigate | n/a |

</frozen-after-approval>

## Code Map

- `web/src/pages/DashboardPage.tsx` (401 lines) -- row JSX L322-378: add `onClick`/`onKeyDown`/`tabIndex`/`aria-label` to the `<li>`; add `handleOpenDetails(tool)` navigating `/tools/${tool.id}` with the `ToolDetailsState` shape; `stopPropagation` on the start button (L339-355), the reinstate button (L356-366) and the reinstate dialog-opener; wrap the row-error `<p>` (L370-374) so it can't bubble a navigation.
- `web/src/pages/DashboardPage.module.css` (339 lines) -- L109-165: `.row`/`.rowMain`/`.rowInfo`/`.rowActions` flattened to one horizontal flex line (`.rowInfo` becomes a row of name/type/Gerätenummer with gap + `min-width:0`; `flex-wrap` on the row); add `.row:hover`/`.row:focus-visible` affordance (`cursor:pointer`, subtle surface change) WITHOUT changing the button styles.
- `web/src/components/UserTable.tsx` (152 lines) -- the clickable-row + keyboard + aria-label pattern to mirror (L112-145); NOT modified.
- `web/src/pages/ToolDetailsPage.tsx` (320 lines) -- `ToolDetailsState` (L15-20) + header consumption (L167-170) already exist; read-only, unchanged.
- Tests -- `web/src/pages/DashboardPage.test.tsx` (834 lines): existing row tests updated to stub a `/tools/:toolId` route + assert navigation state; new cases for ROW_LAYOUT (one line), ROW_NAV (state passed), ROW_KEYBOARD, ROW_START/ROW_REINSTATE (no navigation, stopPropagation), ROW_ERROR.

## Tasks & Acceptance

**Execution:**
- [x] `DashboardPage.tsx` -- clickable one-line row (navigate + state + stopPropagation + keyboard/aria) -- SPA
- [x] `DashboardPage.module.css` -- one-line row layout + hover/focus affordance -- SPA
- [x] `DashboardPage.test.tsx` -- row layout/nav/keyboard/button-isolation tests -- verification

**Acceptance Criteria:**
- Given a dashboard row, when it renders, then the status chip, name, tool type, Gerätenummer and the action buttons sit on ONE line (wrapping on narrow screens) (FR-16/UX-DR9).
- Given a row (not a button), when I click it or activate it with Enter/Space, then the tool details page opens with the tool header (FR-16/6.3).
- Given I click "Prüfung starten" or "Wiederherstellen" on a row, when the action runs, then it does NOT navigate to the details page.

## Spec Change Log

## Design Notes

- **State contract is the 6.3 `ToolDetailsState`:** the row passes exactly the fields `ToolDetailsPage` already reads from router state (name/type/Gerätenummer/status) — the fast header path; a deep-link still refetches. No change to the details page.
- **Button isolation via `stopPropagation`:** the row is the interactive container; every interactive child that must NOT navigate stops propagation. The reinstate dialog opener already captures its target in state (not derived from the list), so stopPropagation is the only change needed there.

## Verification

**Commands:**
- `npx vitest run` in web/ -- expected: all pass incl. the dashboard row nav/layout/button-isolation cases
- `npm --prefix web run lint && npm --prefix web run typecheck && npm --prefix web run build` -- expected: clean

**Manual checks (if no CLI):**
- Dashboard: rows show all fields on one line; clicking a row opens the details page with the header; "Prüfung starten" / "Wiederherstellen" work without navigating; keyboard Enter/Space opens details.

## Suggested Review Order

**Row click navigation**

- The clickable row: `onClick`, keyboard Enter/Space (with key-repeat + target guards), in-flight guard, and the `ToolDetailsState`-typed navigate.
  [`DashboardPage.tsx:252`](../../web/src/pages/DashboardPage.tsx#L252)

- The row JSX: interactive `<li>` + `stopPropagation` on start/reinstate/error.
  [`DashboardPage.tsx:355`](../../web/src/pages/DashboardPage.tsx#L355)

- The exported `ToolDetailsState` the row state is typed to (compile-time drift guard).
  [`ToolDetailsPage.tsx:16`](../../web/src/pages/ToolDetailsPage.tsx#L16)

**One-line layout**

- The flattened one-line row: single flex line with wrap, truncating name/type/Gerätenummer, hover/focus affordance.
  [`DashboardPage.module.css:112`](../../web/src/pages/DashboardPage.module.css#L112)

**Tests**

- Structural one-line pin + chip-click navigation.
  [`DashboardPage.test.tsx:956`](../../web/src/pages/DashboardPage.test.tsx#L956)

- Space-key activation, exactly-once navigation, and button-bubble isolation.
  [`DashboardPage.test.tsx:1022`](../../web/src/pages/DashboardPage.test.tsx#L1022)