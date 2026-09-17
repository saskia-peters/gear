---
title: 'Tool Details: Condensed Track Record + Back Button (FR-18)'
type: 'feature'
created: '2026-09-17'
status: 'done'
route: 'one-shot'
review_loop_iteration: 0
baseline_commit: '3bc1fb0'
---

# Tool Details: Condensed Track Record + Back Button (FR-18)

## Intent

**Problem:** The merged track-record cards on the tool details page were too tall — each inspection/reinstatement rendered as a stacked `<dl>` with several labeled rows, making a long history unwieldy — and there was no way to navigate back to the dashboard from the page.

**Approach:** Condense each history card: the kind badge and the date sit on one header line, and the record fields (inspector/actor + outcome + mode, or actor + reason) collapse into one compact meta line; notes and per-checklist-item results stay as small indented blocks. Add a "← Zurück zur Übersicht" back button at the top that navigates to `/` (mirroring the admin back-row pattern). A11y preserved: the collapsed spans carry `aria-label`s so screen readers still get "Prüfer/in:", "Ergebnis:", "Modus:", "Durchgeführt von:" and "Grund:".

## Suggested Review Order

**Back button**

- The back button navigating to the dashboard, placed above the title.
  [`ToolDetailsPage.tsx:232`](../../web/src/pages/ToolDetailsPage.tsx#L232)

- Its styles (mirroring the admin back-row pattern).
  [`ToolDetailsPage.module.css:41`](../../web/src/pages/ToolDetailsPage.module.css#L41)

**Condensed card layout**

- The card header line (badge + date) and the single meta line with aria-labelled spans.
  [`ToolDetailsPage.tsx:312`](../../web/src/pages/ToolDetailsPage.tsx#L312)

- The compact card/meta/badge/notes CSS.
  [`ToolDetailsPage.module.css:210`](../../web/src/pages/ToolDetailsPage.module.css#L210)

**Tests**

- Back-button navigation + details unmount.
  [`ToolDetailsPage.test.tsx:131`](../../web/src/pages/ToolDetailsPage.test.tsx#L131)

- Condensed track-record rendering (newest-first, badges, meta line).
  [`ToolDetailsPage.test.tsx:196`](../../web/src/pages/ToolDetailsPage.test.tsx#L196)