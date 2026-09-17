---
title: 'Tool Details: Merged Track Record (FR-18)'
type: 'feature'
created: '2026-09-17'
status: 'done'
route: 'one-shot'
review_loop_iteration: 0
baseline_commit: '629532a'
---

# Tool Details: Merged Track Record (FR-18)

## Intent

**Problem:** The tool details page rendered inspections and reinstatements in TWO separate sections (Prüfhistorie on top, Wiederherstellungen below), so the tool's audit trail was not one contiguous chronological record.

**Approach:** Merge both lists into ONE newest-first track record in `ToolDetailsPage.tsx`: a `trackRecord` memo interleaves inspections (`submitted_at`) and reinstatements (`created_at`) sorted by timestamp descending (id-desc tiebreak), each card tagged with a kind badge ("Prüfung" / "Wiederherstellung"). One section, one empty state ("Keine Einträge vorhanden."), one loading/error/no-permission state.

## Suggested Review Order

**Merge + ordering**

- The `trackRecord` merge: interleaves inspections + reinstatements newest-first with an id-desc tiebreak, defensive against null lists.
  [`ToolDetailsPage.tsx:267`](../../web/src/pages/ToolDetailsPage.tsx#L267)

- The discriminated `TrackRecordEntry` union + stable kind-prefixed React key.
  [`ToolDetailsPage.tsx:52`](../../web/src/pages/ToolDetailsPage.tsx#L52)

**Rendering**

- The single merged section with kind badges, per-kind fields, and one empty/loading/error/no-permission state.
  [`ToolDetailsPage.tsx:305`](../../web/src/pages/ToolDetailsPage.tsx#L305)

- The badge styles (Prüfung vs Wiederherstellung).
  [`ToolDetailsPage.module.css:199`](../../web/src/pages/ToolDetailsPage.module.css#L199)

**Tests**

- Newest-first interleaving across kinds + kind badges.
  [`ToolDetailsPage.test.tsx:184`](../../web/src/pages/ToolDetailsPage.test.tsx#L184)

- Single empty note, single no-permission/error, and the tool-switch loading text.
  [`ToolDetailsPage.test.tsx:220`](../../web/src/pages/ToolDetailsPage.test.tsx#L220)