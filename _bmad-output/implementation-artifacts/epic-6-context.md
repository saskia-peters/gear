# Epic 6 Context: Dashboard, Reporting & History

<!-- Compiled from planning artifacts. Edit freely. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Users see the current operational truth, and leadership reviews and exports it. Every authenticated user gets a color-coded status dashboard (Red/Orange/Green, derived on read) filterable by status, so anyone can tell at a glance what is safe, due, and overdue. Fuehrung/Admin can additionally export the currently filtered view as a PDF status report and inspect a tool's full inspection history for audits and readiness reviews.

## Stories

- Story 6.1: Color-Coded Status Dashboard
- Story 6.2: Status Report Export (PDF)
- Story 6.3: Inspection History per Tool

## Requirements & Constraints

- Tool status is always computed on read from inspection records and due-date math via a single shared clock/status function — never stored — and the same derived result feeds the dashboard, the PDF export, and tool detail. A never-inspected tool is Red (AD-4/AD-5).
- Color thresholds: Red = past due or Out of Service (Out of Service tools also render Red); Orange = due within ONE QUARTER of the tool's OWN inspection cycle; Green = current (due beyond the quarter-cycle window). The orange window is proportional to each tool's schedule (user decision 2026-09-17 — a fresh inspection reads green/Einsatzbereit on every cycle, superseding the earlier fixed 14-day window).
- The dashboard is visible to all authenticated users (gated by `dashboard.view`, granted to every base role). It shows a 2×2 summary count grid (totals per status) and filter chips that filter the visible list; counts are tappable and activate the matching filter, and multiple filters can be active at once.
- Dashboard state (statuses, counts, colors) must reflect the current derived state whenever data changes (e.g. after an inspection or reinstatement).
- PDF export is available to Fuehrung/Admin only (gated by `report.export`) and must reflect exactly the currently visible/filtered tool list with its derived statuses — no separate export configuration. Each entry includes tool name, tool type, current status, last inspection date, and last inspector.
- Full per-tool inspection history is available to Schirrmeister/Fuehrung/Admin (gated by `inspection.history.view`), newest first. Each listed inspection shows the inspector, timestamp, overall result, inspection mode (pass/fail or checklist), and per-checklist-item results where applicable; inspectors of deleted accounts render as "Deleted User".
- Missing the permission for an action means the server returns HTTP 403 with no data exposed, and the corresponding UI entry point is hidden — frontend visibility is supplementary only, never the gate.
- Dashboard load must meet the sub-500ms p95 response target and be usable on mobile/4G.

## Technical Decisions

- Lives in the Tool module (`internal/tools`): dashboard/status read path, report export, and history are Tool-module concerns; status derivation (AD-4) and the single shared inspection clock (AD-5) are the same functions the inspection flow already uses.
- The derived status function resolves each tool's `next_due` as last successful inspection (or reinstatement) date plus the resolved schedule interval (per-tool schedule if set, else tool-type default); Out of Service is derived from the latest failed inspection not since reinstated.
- All three features are gated server-side against the auth port's resolved permission set (`dashboard.view`, `report.export`, `inspection.history.view`), re-checked per request.
- History reads Tool-owned `inspections` + `inspection_items` rows, reverse-chronological by inspection date; the inspector reference is a FK to users that DSGVO deletion rewrites to the literal "Deleted User" through the Tool module's own lifecycle port (AD-8).
- Timestamps UTC/RFC 3339 from the server clock; inspection dates stored as dates with due-date math in UTC day boundaries; uniform JSON error envelope for 403s.

## UX & Interaction Patterns

- The dashboard is the primary authenticated landing surface; German microcopy throughout. A 2×2 summary count grid shows one glanceable display-size number per status color; tappable counts and filter chips activate the matching filter. Status chips are color-specific pills (green/orange/red/OOS).
- Export uses the "export current view" primitive: the PDF reflects the active filters with no separate configuration screen; a preview shows the columns (Name · Typ · Status · Prüfer) before download.
- Empty state is "Keine Werkzeuge vorhanden". Cold open shows cached counts + tool list with background server refresh; loading uses skeleton placeholders; post-inspection returns to the refreshed dashboard automatically.
- Accessibility floor applies (WCAG AA, ≥48px touch targets, keyboard-operable, screen-reader announcements for status changes); light and dark mode token pairs both ship. On mobile the summary counts form the 2×2 grid and the tool list is single-column.

## Cross-Story Dependencies

- Story 6.1 builds on the Story 1.2 dashboard foundation (route + shell + empty state, already gated by `dashboard.view`).
- Status derivation depends on inspection/reinstatement data and the shared clock produced by Epic 5, and on tools/tool types/schedules from Epic 4.
- Story 6.3 depends on inspection records persisted by Epic 5 (incl. per-checklist-item results) and on the Story 3.4 DSGVO anonymization that rewrites inspector references to "Deleted User".