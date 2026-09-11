# Epic 4 Context: Equipment Catalogue & Scheduling

<!-- Compiled from planning artifacts. Edit freely. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Let the Schirrmeister/Admin administer the equipment universe: define tool types (name, default schedule from the catalog, required qualification, inspection mode pass/fail vs checklist, checklist items), individual tools (belonging to exactly one type, optional per-tool schedule override), flexible JSON attributes, and bulk CSV import with per-row error reporting. This gives every physical tool a consistent, centrally managed inspection template.

## Stories

- Story 4.1: Schedule Catalog Management (DONE — named schedules, `schedules.manage`, migration 000019/000020)
- Story 4.2: Tool Type Management
- Story 4.3: Tool Management
- Story 4.4: Flexible Attributes on Tools & Tool Types
- Story 4.5: Bulk CSV Import

## Requirements & Constraints

- Tool Types (FR-8): create/edit with Name, default inspection schedule (FK from catalog), required Qualification (FK), inspection mode (pass/fail vs checklist). Mode persists and controls the inspection UI for tools of this type.
- Checklist items (FR-23): per-tool-type, add/order/remove. Changes reflect on **future** inspections only — historical inspection records keep their own recorded item results unchanged.
- Tools (FR-9): belong to exactly one tool type; Name/identifier; optional per-tool schedule override that is a first-class FK to `schedules` (never JSONB); empty override inherits the type default.
- Flexible attributes (FR-10): arbitrary custom metadata via a JSON field without DB migration.
- Bulk CSV import (FR-9/FR-23): per-row error report, no partial silent failures.
- Gating (AD-6): `tool_types.manage` for types, `tools.manage` for tools, `schedules.manage` for the catalog; 403 with no data exposed otherwise.
- Audit (NFR-O2): config writes are audited with actor, timestamp, operation.

## Technical Decisions

- Tool hexagon (`internal/tools`) owns tool types, tools, checklist items, and the inspection domain (AD-1/AD-10); currently a structural seed (package boundaries only) — Epic 4 materializes it.
- Configuration write path single owner (AD-10): Admin configures tools/types/checklist exclusively through the Tool module's exported **configuration port** — never Admin-owned copies or ad-hoc SQL over the same tables.
- Tool Type schedule is a first-class FK to the Admin-owned `schedules` catalog (AD-16); schedule resolution is shared (AD-5): per-tool override else type default.
- Required qualification is a cross-module FK reference from the Tool module to User-module qualification IDs (AD-7/AD-11); the Tool module obtains qualification data through the auth port, never by joining user tables.
- Core attributes map to typed columns; one `attributes JSONB` column is the no-migration extension surface (AD-3).
- Migration numbering follows the existing golang-migrate set (000019/000020 exist; next is 000021); sqlc schema inputs stay scoped per module (Admin block vs Tool block).
- Tool status is derived on read, never stored (AD-4) — Epic 5 concern, not Epic 4.

## UX & Interaction Patterns

- Tool catalogue with tabs ("Typen" and "Werkzeuge"), German UI throughout.
- Type editor: name, default schedule dropdown (from catalog), required qualification dropdown, inspection-mode switch, and checklist item list (add/order/remove) when mode is checklist.
- Inline form validation + inline feedback, never toast-only (UX-DR6/UX-DR8); ≥48px targets; sticky actions.
- Empty states in German ("Keine Werkzeuge vorhanden", etc.); loading skeletons; 403 → dashboard redirect + "Zugriff verweigert".

## Cross-Story Dependencies

- 4.1 (schedules catalog) is DONE and must be consumed via FK for 4.2/4.3.
- 4.2 (tool types) is a prerequisite for 4.3 (tools) and 4.4 (attributes).
- 4.5 (CSV import) depends on 4.2/4.3 data model and the configuration port.
- Qualification vocabulary lives in the User module (Epic 2) — 4.2 needs a read port for it.