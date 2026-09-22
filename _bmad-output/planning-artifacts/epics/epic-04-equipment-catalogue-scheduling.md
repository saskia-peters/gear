## Epic 4: Equipment Catalogue & Scheduling

The Schirrmeister administers the equipment universe. Admins/Schirrmeister define tool types (name, default schedule, required qualification, checklist vs pass/fail mode), individual tools (optional per-tool schedule override), flexible attributes, the named schedule catalog, and bulk CSV import with per-row error reporting.

### Story 4.1: Schedule Catalog Management

As an admin,
I want to create, edit, and archive named schedules in a generic catalog,
So that recurring intervals (currently inspection intervals in V1) can be referenced across modules without renaming.

**Acceptance Criteria:**

**Given** I have the `schedules.manage` permission,
**When** I open "Einstellungen → Zeitpläne" in the admin module,
**Then** I see the schedule-catalog surface listing named schedules (name + repeating interval unit/magnitude, e.g. "Jährlich − 1 year", "Monatlich − 1 month") (FR-30/AD-16)
**And** catalog naming stays generic (Zeitpläne, not Prüfintervalle) so future modules can reuse the schedules (FR-30/AD-16).

**Given** I create or edit a schedule,
**When** I save it,
**Then** it persists as a named schedule (name + interval unit/magnitude) in the Admin-owned `schedules` table (AD-11/AD-16)
**And** the reserved weekday-set and time-of-day fields are present but nullable/unused in V1 (cron-like), so a future composite engine needs no migration (FR-30/AD-16).

**Given** I archive a schedule,
**When** I confirm,
**Then** the schedule is archived (not deleted) — tool types and tools referencing it by FK keep their history intact and references resolve (FR-30/AD-16).

**Given** a schedule is referenced,
**When** a Tool Type or Tool selects its inspection interval,
**Then** it references the catalog entry by FK (never JSONB) (FR-30/FR-9/AD-10/AD-16).

**Given** I lack the `schedules.manage` permission,
**When** I attempt access,
**Then** I receive HTTP 403 and no schedule data is exposed (AD-6).

### Story 4.2: Tool Type Management

As a Schirrmeister/Admin,
I want to define tool types with a default schedule, required qualification, inspection mode, and checklist items,
So that individual tools inherit their inspection behaviour from a consistent type template.

**Acceptance Criteria:**

**Given** I have the `tool_types.manage` permission,
**When** I open the Tool catalogue → "Typen" tab,
**Then** I can create or edit a tool type with: Name, default inspection schedule (FK from catalog, AD-16), required Qualification (FK, FR-8), and inspection mode (pass/fail vs checklist) (FR-8/UX-DR5/UX-DR8)
**And** the mode persists and controls the inspection UI for tools of this type (FR-8).

**Given** I create/edit a checklist-mode tool type,
**When** I manage its checklist items,
**Then** I can add, order, and remove checklist items per tool type (FR-23)
**And** added/removed items reflect on **future** inspections only — historical inspection records keep their own recorded item results unchanged (FR-23).

**Given** I save a tool type,
**When** it is persisted,
**Then** core fields map to typed columns and the `attributes JSONB` column is available for custom metadata (FR-10/AD-3)
**And** the write path goes through the Tool module's configuration port (AD-10).

**Given** I lack the `tool_types.manage` permission,
**When** I attempt access,
**Then** I receive HTTP 403 and no tool-type data is exposed (AD-6).

### Story 4.3: Tool Management

As a Schirrmeister/Admin,
I want to create and edit individual tools belonging to a tool type,
So that I can track each physical tool with an optional schedule override.

**Acceptance Criteria:**

**Given** I have the `tools.manage` permission,
**When** I open the Tool catalogue → "Werkzeuge" tab,
**Then** I can create or edit a tool that belongs to exactly one tool type (Name/identifier; optional per-tool schedule override) (FR-9/UX-DR5/UX-DR8).

**Given** I set a per-tool schedule override,
**When** I select a schedule from the catalog,
**Then** it is a first-class FK reference to `schedules` (never JSONB) and overrides the tool type's default schedule for this tool's inspection clock (FR-9/AD-10).

**Given** I leave the override empty,
**When** I save the tool,
**Then** the tool inherits its inspection schedule from its tool type's default (FR-9/AD-5).

**Given** I save a tool,
**When** it is persisted,
**Then** core fields map to typed columns and the `attributes JSONB` column is available for custom metadata (FR-10/AD-3).

**Given** I lack the `tools.manage` permission,
**When** I attempt access,
**Then** I receive HTTP 403 and no tool data is exposed (AD-6).

### Story 4.4: Flexible Attributes on Tools & Tool Types

As the system,
I want to store arbitrary custom metadata on tools and tool types without a database migration,
So that the organization can track organization-specific fields as needs evolve.

**Acceptance Criteria:**

**Given** a tool or tool type,
**When** custom attributes are set,
**Then** they are stored in the owning row's `attributes JSONB` column (never a new column per attribute) (FR-10/AD-3).

**Given** custom attributes are saved,
**When** they are read back,
**Then** they are retrievable unchanged through the Tool module's port with valid JSON serialization and validation (FR-10/AD-1/AD-3).

**Given** a custom attribute later becomes core/queryable,
**When** it is promoted,
**Then** it is migrated to a real typed column via golang-migrate and backfilled, retaining the JSONB column for continued flexibility (AD-3/NFR-R2).

**Given** I lack the `tools.manage`/`tool_types.manage` permission for the owning entity,
**When** I attempt to write attributes,
**Then** I receive HTTP 403 and no data is exposed (AD-6).

### Story 4.5: Bulk CSV Import

As a Schirrmeister/Admin,
I want to bulk-import tools from a CSV file,
So that I can onboard many tools at once without batch-scoped silent failures.

**Acceptance Criteria:**

**Given** I have the `tools.manage` permission,
**When** I open the Tool catalogue → "Werkzeuge" → CSV import,
**Then** I can upload a CSV with the expected columns (tool identifier, tool type, optional schedule override) and a mapping/format guide is shown (FR-9/UX-DR8).

**Given** I upload a CSV with both valid and invalid rows,
**When** the import runs,
**Then** valid rows are created/updated and every invalid row is reported per-row with the row number and German reason (e.g. "Zeile 4: Tool Type 'X' nicht gefunden") (FR-9/FR-23)
**And** the import is atomic per row, never silently partially successful — invalid rows never produce partial tool records (FR-9).

**Given** the import completes,
**When** the result screen is shown,
**Then** it summarizes imports (imported count, error count) with a downloadable error report (FR-23/UX-DR6/UX-DR8).

**Given** I lack the `tools.manage` permission,
**When** I attempt to import,
**Then** I receive HTTP 403 and no data is exposed (AD-6).
