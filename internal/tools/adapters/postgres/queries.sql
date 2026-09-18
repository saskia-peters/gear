-- Tool module store (AD-1/AD-10/AD-11), generated into package postgres by
-- sqlc. Story 4.2 ships the tool-type catalog queries over the Tool-owned
-- `tool_types` + `tool_type_checklist_items` tables (FR-8/FR-10/FR-23). The
-- schema input is the single migration 000021 (see sqlc.yaml); the Tool module
-- never joins another module's tables — the cross-module FKs are validated
-- through the modules' read-only ports at the core layer.

-- name: ListToolTypes :many
-- The ACTIVE tool-type catalog (FR-8/AD-10). Archived rows (archived_at NOT
-- NULL) are filtered out — the active surface never shows them. The order is
-- deterministic: created_at ASC with a name tiebreaker.
SELECT id, name, default_schedule_id, required_qualification_id, inspection_mode, attributes, archived_at, created_at, updated_at
FROM tool_types
WHERE archived_at IS NULL
ORDER BY created_at ASC, name ASC;

-- name: ToolTypeExistsActive :one
-- A lean existence check used by the update path to resolve the archived
-- sentinel BEFORE the duplicate-name guard (so updating an unknown/archived id
-- never answers a duplicate-name 400). Reports whether the row exists AND is
-- still active (archived_at IS NULL); the store maps a false result to
-- ErrToolTypeNotFound. No checklist items are fetched (they are not used here).
SELECT EXISTS (
    SELECT 1 FROM tool_types
    WHERE id = $1 AND archived_at IS NULL
);

-- name: ListToolTypeChecklistItems :many
-- The ordered checklist items of a SET of tool types (FR-23), grouped by
-- tool_type_id and ordered by position within each group. The repository groups
-- them onto the fetched tool types in one round-trip for the GET_LIST surface.
SELECT id, tool_type_id, position, label, created_at, updated_at
FROM tool_type_checklist_items
WHERE tool_type_id = ANY($1::uuid[])
ORDER BY tool_type_id ASC, position ASC;

-- name: GetToolTypeChecklistItems :many
-- The ordered checklist items of ONE tool type, by position (FR-23).
SELECT id, tool_type_id, position, label, created_at, updated_at
FROM tool_type_checklist_items
WHERE tool_type_id = $1
ORDER BY position ASC;

-- name: CreateToolType :one
-- Insert a tool type and return the resulting row. The attributes jsonb column
-- is written explicitly (Story 4.4, FR-10/AD-3): the core passes '{}' for an
-- absent field and a validated object otherwise. A name already held by ANY row
-- (active or archived) trips the UNIQUE constraint and is mapped by the
-- repository to the German duplicate-name 400.
INSERT INTO tool_types (name, default_schedule_id, required_qualification_id, inspection_mode, attributes)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, name, default_schedule_id, required_qualification_id, inspection_mode, attributes, archived_at, created_at, updated_at;

-- name: InsertToolTypeChecklistItem :exec
-- Insert one ordered checklist item of a tool type (FR-23). Called once per
-- item inside the create/update transaction; position comes from the array
-- order the client submitted.
INSERT INTO tool_type_checklist_items (tool_type_id, position, label)
VALUES ($1, $2, $3);

-- name: UpdateToolType :one
-- Replace one ACTIVE tool type's core fields and refresh updated_at. The
-- `AND archived_at IS NULL` guard makes an update against an already-archived
-- row affect zero rows → ErrToolTypeNotFound (soft archive is irreversible in
-- V1; the archived row is non-existent to the surface). The attributes jsonb
-- column follows the shared contract (Story 4.4): COALESCE($6, attributes)
-- keeps the stored value when the core passes a NIL map ("absent = unchanged"),
-- while an explicit '{}' (a non-NIL value) clears it and a non-empty object
-- replaces it.
UPDATE tool_types
SET name = $2,
    default_schedule_id = $3,
    required_qualification_id = $4,
    inspection_mode = $5,
    attributes = COALESCE($6, attributes),
    updated_at = now()
WHERE id = $1 AND archived_at IS NULL
RETURNING id, name, default_schedule_id, required_qualification_id, inspection_mode, attributes, archived_at, created_at, updated_at;

-- name: DeleteToolTypeChecklistItems :exec
-- Remove EVERY checklist item of a tool type (FR-23). Used by the update item
-- REPLACEMENT before InsertToolTypeChecklistItem, both in ONE transaction
-- (delete-then-insert, separate statements — Story 2.5 lesson).
DELETE FROM tool_type_checklist_items WHERE tool_type_id = $1;

-- name: ArchiveToolType :one
-- Soft-archive one tool type: archived_at = now() (never a hard delete — FK
-- history intact, AD-10). The `AND archived_at IS NULL` guard makes archiving
-- an already-archived row affect zero rows → ErrToolTypeNotFound (the row is
-- non-existent to the surface).
UPDATE tool_types
SET archived_at = now(),
    updated_at = now()
WHERE id = $1 AND archived_at IS NULL
RETURNING id, name, default_schedule_id, required_qualification_id, inspection_mode, attributes, archived_at, created_at, updated_at;

-- ============================================================================
-- Tool queries (Story 4.3, FR-9/FR-10/AD-5/AD-10): the physical tools that
-- belong to a tool type. Every read/write joins the Tool-OWNED `tool_types`
-- for the type display name (AD-8/AD-11: the Tool module only joins ITS OWN
-- tables — the cross-module schedules override is validated through the Admin
-- SchedulesPort, never by joining the Admin tables).
-- ============================================================================

-- name: ListTools :many
-- The ACTIVE tool catalog (FR-9/AD-10), each with its tool type's display name
-- (JOIN on Tool-owned tool_types). Archived rows (archived_at NOT NULL) are
-- filtered out — the active surface never shows them. The order is
-- deterministic: created_at ASC with a name tiebreaker. An empty schedule_id
-- (SQL NULL) means the tool inherits its type's default schedule (AD-5); the
-- type's default_schedule_id is carried alongside (Story 6.1: the dashboard's
-- interval-resolution input, via the Tool-owned JOIN — never a cross-module
-- join, AD-8/AD-11).
SELECT t.id, t.name, t.tool_type_id, tt.name AS tool_type_name, t.schedule_id, tt.default_schedule_id AS default_schedule_id, t.inventory_number, t.attributes, t.archived_at, t.created_at, t.updated_at
FROM tools t
JOIN tool_types tt ON tt.id = t.tool_type_id
WHERE t.archived_at IS NULL
ORDER BY t.created_at ASC, t.name ASC;

-- name: ToolExistsActive :one
-- A lean existence check used by the update path to resolve the archived
-- sentinel BEFORE the duplicate-name guard (so updating an unknown/archived id
-- never answers a duplicate-name 400). Reports whether the row exists AND is
-- still active (archived_at IS NULL); the store maps a false result to
-- ErrToolNotFound.
SELECT EXISTS (
    SELECT 1 FROM tools
    WHERE id = $1 AND archived_at IS NULL
);

-- name: GetToolWithTypeQualification :one
-- The lean inspection-start read (Story 5.1, FR-11/AD-7): the ACTIVE tool plus
-- its type's required_qualification_id and inspection_mode (intra-module JOIN on
-- Tool-owned tool_types — the Tool module never joins another module's tables,
-- AD-8/AD-11). BOTH guards (`t.archived_at IS NULL` AND `tt.archived_at IS
-- NULL`) make a tool with an ARCHIVED type (or an archived/missing tool) answer
-- ErrToolNotFound — an active tool must never resolve a retired type's gating
-- data for the start (the row is non-existent to the surface). Story 5.3 also
-- carries the tool's schedule OVERRIDE and the type's DEFAULT schedule id (the
-- AD-5 interval-resolution inputs, resolved through the Admin SchedulesPort).
SELECT t.id, t.name, t.tool_type_id, tt.name AS tool_type_name, tt.required_qualification_id, tt.inspection_mode, t.schedule_id, tt.default_schedule_id
FROM tools t
JOIN tool_types tt ON tt.id = t.tool_type_id
WHERE t.id = $1 AND t.archived_at IS NULL AND tt.archived_at IS NULL;

-- name: CreateTool :one
-- Insert a tool and return the resulting row JOINed with its type name. The
-- inventory number is AUTO-ASSIGNED in-SQL (Story 4-3b): 'GEAR' || zero-padded
-- nextval from the dedicated sequence — atomic, monotonic, one round-trip, no
-- client input (CREATE_IGNORE_CLIENT). NOTE: the zero-pad width is 6 for the
-- backfill/early numbering; once the sequence exceeds 999999 the number
-- NATURALLY widens to 7+ digits (e.g. 'GEAR1000000') — still well inside the
-- CHECK (char_length <= 16), monotonic, and fine for the surface. A manual edit
-- can consume a future sequence value; a UNIQUE collision on
-- tools_inventory_number_key (a case-insensitive functional index) is handled
-- by the repository's bounded retry loop (re-running this INSERT computes a
-- FRESH nextval). The core validated the type EXISTS + ACTIVE and the
-- (optional) schedule override against the SchedulesPort first; an EMPTY
-- schedule_id is passed as NULL (inherit the type default, AD-5). A name
-- already held by ANY row (active or archived) trips the UNIQUE constraint and
-- is mapped by the repository to the German duplicate-name 400.
WITH new_tool AS (
    INSERT INTO tools (name, tool_type_id, schedule_id, inventory_number, attributes)
    VALUES ($1, $2, $3, 'GEAR' || lpad(nextval('tools_inventory_number_seq')::text, 6, '0'), $4)
    RETURNING id, name, tool_type_id, schedule_id, inventory_number, attributes, archived_at, created_at, updated_at
)
SELECT nt.id, nt.name, nt.tool_type_id, tt.name AS tool_type_name, nt.schedule_id, nt.inventory_number, nt.attributes, nt.archived_at, nt.created_at, nt.updated_at
FROM new_tool nt
JOIN tool_types tt ON tt.id = nt.tool_type_id;

-- name: UpdateTool :one
-- Replace one ACTIVE tool's core fields and refresh updated_at, returning the
-- row JOINed with its type name. The `AND archived_at IS NULL` guard makes an
-- update against an already-archived row affect zero rows → ErrToolNotFound
-- (soft archive is irreversible in V1; the archived row is non-existent to the
-- surface). The schedule override REPLACES the stored value, so clearing it
-- (schedule_id NULL) makes the tool inherit its type's default again
-- (UPDATE_CLEAR_OVERRIDE, AD-5). The inventory number REPLACES the stored value
-- too (Story 4-3b, UPDATE_INVENTORY): the core already validated it non-empty
-- + bounded + unique case-insensitively among active tools; a reuse of a number
-- held by ANOTHER row (active or archived — incl. case-variants, via the
-- lower() functional UNIQUE index) is mapped by the repository to the German
-- duplicate-inventory 400. The attributes jsonb column follows the shared
-- contract (Story 4.4): COALESCE($6, attributes) keeps the stored value when
-- the core passes a NIL map ("absent = unchanged"), while an explicit '{}' (a
-- non-NIL value) clears it and a non-empty object replaces it.
WITH updated AS (
    UPDATE tools
    SET name = $2,
        tool_type_id = $3,
        schedule_id = $4,
        inventory_number = $5,
        attributes = COALESCE($6, attributes),
        updated_at = now()
    WHERE tools.id = $1 AND tools.archived_at IS NULL
    RETURNING tools.id, tools.name, tools.tool_type_id, tools.schedule_id, tools.inventory_number, tools.attributes, tools.archived_at, tools.created_at, tools.updated_at
)
SELECT u.id, u.name, u.tool_type_id, tt.name AS tool_type_name, u.schedule_id, u.inventory_number, u.attributes, u.archived_at, u.created_at, u.updated_at
FROM updated u
JOIN tool_types tt ON tt.id = u.tool_type_id;

-- name: ArchiveTool :one
-- Soft-archive one tool: archived_at = now() (never a hard delete — FK
-- history intact, AD-10), returning the row JOINed with its type name. The
-- `AND archived_at IS NULL` guard makes archiving an already-archived row
-- affect zero rows → ErrToolNotFound (the row is non-existent to the surface).
WITH archived AS (
    UPDATE tools
    SET archived_at = now(),
        updated_at = now()
    WHERE tools.id = $1 AND tools.archived_at IS NULL
    RETURNING tools.id, tools.name, tools.tool_type_id, tools.schedule_id, tools.inventory_number, tools.attributes, tools.archived_at, tools.created_at, tools.updated_at
)
SELECT a.id, a.name, a.tool_type_id, tt.name AS tool_type_name, a.schedule_id, a.inventory_number, a.attributes, a.archived_at, a.created_at, a.updated_at
FROM archived a
JOIN tool_types tt ON tt.id = a.tool_type_id;

-- ============================================================================
-- Inspection queries (Story 5.3, FR-12/FR-13/FR-14/AD-4/AD-5): the
-- Tool-owned inspections + inspection_items + reinstatements tables. The write
-- (InsertInspection + InsertInspectionItem) runs in ONE transaction (inspection
-- + its snapshot items); the status read (GetToolInspectionStatus's building
-- blocks) feeds the shared derived-status/clock function — OOS is NEVER stored.
-- ============================================================================

-- name: InsertInspection :one
-- Insert one inspection record and return the persisted row. The
-- overall_result and mode were validated by the core; inspector_id and notes
-- are snapshotted plain values (no FK, AD-8/3.4).
INSERT INTO inspections (tool_id, inspector_id, mode, overall_result, notes)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, tool_id, inspector_id, mode, overall_result, notes, submitted_at;

-- name: InsertInspectionItem :exec
-- Insert one snapshotted checklist item of an inspection (FR-12/FR-23): the
-- label + position come from the tool type's checklist at submit time (history
-- stays self-contained even if the template later changes); item_id is a plain
-- uuid (no FK). Called once per item inside the submit transaction.
INSERT INTO inspection_items (inspection_id, item_id, label, position, result)
VALUES ($1, $2, $3, $4, $5);

-- name: GetLatestFailedInspection :one
-- The submitted_at of the LATEST FAILED inspection of a tool (the OOS anchor,
-- AD-4: OOS is derived from the latest FAILED inspection not since reinstated —
-- a PASS inspection does NOT clear it). No row → pgx.ErrNoRows (the repository
-- maps it to a nil anchor).
SELECT submitted_at
FROM inspections
WHERE tool_id = $1 AND overall_result = 'fail'
ORDER BY submitted_at DESC, id DESC
LIMIT 1;

-- name: GetInspectionItems :many
-- The snapshotted ordered checklist items of ONE inspection (the item snapshot
-- at submit, FR-12), by position.
SELECT id, inspection_id, item_id, label, position, result
FROM inspection_items
WHERE inspection_id = $1
ORDER BY position ASC;

-- name: GetLatestPassInspection :one
-- The submitted_at of the LATEST PASSING inspection of a tool (the clock's
-- "last successful inspection" anchor, AD-5). No row → pgx.ErrNoRows (the
-- repository maps it to a nil anchor).
SELECT submitted_at
FROM inspections
WHERE tool_id = $1 AND overall_result = 'pass'
ORDER BY submitted_at DESC, id DESC
LIMIT 1;

-- name: GetLatestReinstatement :one
-- The created_at of the LATEST reinstatement of a tool (the clock's
-- "last reinstated" anchor, AD-5/AD-9; the WRITE path is Story 5.6). No row →
-- pgx.ErrNoRows (the repository maps it to a nil anchor).
SELECT created_at
FROM reinstatements
WHERE tool_id = $1
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: GetLatestInspectionByTool :one
-- The LATEST inspection of a tool (ANY result — Story 6.2, FR-17): the status
-- report's "Zuletzt geprüft" + "Prüfer/in" inputs. The submitted_at DESC,
-- id DESC tiebreak is deterministic (the existing 000027 index
-- inspections_tool_id_submitted_at_idx already supports it). No row →
-- pgx.ErrNoRows (the repository maps it to a nil latest, and the report renders
-- "–").
SELECT submitted_at, inspector_id
FROM inspections
WHERE tool_id = $1
ORDER BY submitted_at DESC, id DESC
LIMIT 1;

-- name: InsertReinstatement :one
-- Persist one reinstatement (Story 5.6, FR-15/AD-9): tool, actor and the
-- MANDATORY reason, created_at = DB now(). The reason was validated by the
-- core (non-empty, ≤ 2000 runes); actor_id is a plain uuid (no FK, AD-8/3.4).
-- The row immediately flips the derived status — a fail before the latest
-- reinstatement is not OOS (AD-4).
INSERT INTO reinstatements (tool_id, actor_id, reason)
VALUES ($1, $2, $3)
RETURNING id, tool_id, actor_id, reason, created_at;

-- ============================================================================
-- History queries (Story 6.3, FR-18/AD-6/AD-8): the per-tool audit trail of
-- inspections + reinstatements, newest first. The inspector/actor NAMES never
-- resolve here — the Tool module never joins user tables (AD-8/AD-11); the
-- plain FK-less ids are read out and the core resolves the display names
-- through the User module's DisplayNameResolver seam in ONE bulk call.
-- ============================================================================

-- name: ListInspectionsByTool :many
-- The FULL inspection history of a tool (FR-18), reverse-chronological with
-- the id tiebreak for equal timestamps (submitted_at DESC, id DESC — the same
-- deterministic tiebreak as the status-read queries; the existing 000027 index
-- inspections_tool_id_submitted_at_idx already supports it). A tool with no
-- inspections answers an empty set (the history surface renders the German
-- empty state, never a 404).
SELECT id, tool_id, inspector_id, mode, overall_result, notes, submitted_at
FROM inspections
WHERE tool_id = $1
ORDER BY submitted_at DESC, id DESC;

-- name: ListReinstatementsByTool :many
-- The FULL reinstatement ledger of a tool (FR-18), newest first with the id
-- tiebreak (created_at DESC, id DESC — the existing 000027 index
-- reinstatements_tool_id_created_at_idx already supports it). A tool with no
-- reinstatements answers an empty set.
SELECT id, tool_id, actor_id, reason, created_at
FROM reinstatements
WHERE tool_id = $1
ORDER BY created_at DESC, id DESC;

-- name: ListInspectionItemsByTool :many
-- The snapshotted ordered checklist items of EVERY inspection of a tool (FR-18
-- per-checklist-item results), grouped by inspection and ordered by position
-- within each group. The repository groups them onto the fetched inspections
-- in ONE round-trip (no N+1 per-inspection item reads).
SELECT ii.id, ii.inspection_id, ii.item_id, ii.label, ii.position, ii.result
FROM inspection_items ii
JOIN inspections i ON i.id = ii.inspection_id
WHERE i.tool_id = $1
ORDER BY ii.inspection_id, ii.position;

-- ============================================================================
-- DSGVO data-access export queries (Story 3.3, FR-24/AD-8): the per-USER reads
-- behind the Tool module's DSGVOInspectionExportPort. They filter on the plain
-- FK-less inspector_id / actor_id columns (the 000029 indexes
-- inspections_inspector_id_idx / reinstatements_actor_id_idx serve them).
-- The export stays inside Tool-owned tables (inspections JOIN tools for the
-- display name is intra-module, AD-8/AD-11); no actor names resolve here — the
-- report subject is the exporting user.
-- ============================================================================

-- name: ListInspectionsByInspector :many
-- Every inspection the user performed as inspector (FR-24), newest first with
-- the id tiebreak. The repository attaches the snapshotted per-checklist-item
-- results in one grouped round-trip. A user with no inspections answers an
-- empty set (the report renders the German empty note, never a 404).
SELECT id, tool_id, inspector_id, mode, overall_result, notes, submitted_at
FROM inspections
WHERE inspector_id = $1
ORDER BY submitted_at DESC, id DESC;

-- name: ListInspectionItemsByInspector :many
-- The snapshotted ordered checklist items of EVERY inspection the user
-- performed (Story 3.3), grouped by inspection and ordered by position within
-- each group — the per-inspection item results of the DSGVO export in ONE
-- round-trip (no N+1 per-inspection item reads, mirroring the history surface).
SELECT ii.id, ii.inspection_id, ii.item_id, ii.label, ii.position, ii.result
FROM inspection_items ii
JOIN inspections i ON i.id = ii.inspection_id
WHERE i.inspector_id = $1
ORDER BY ii.inspection_id, ii.position;

-- name: ListReinstatementsByActor :many
-- Every reinstatement the user performed as actor (FR-24), newest first with
-- the id tiebreak. A user with no reinstatements answers an empty set.
SELECT id, tool_id, actor_id, reason, created_at
FROM reinstatements
WHERE actor_id = $1
ORDER BY created_at DESC, id DESC;

-- name: ListToolNamesByIDs :many
-- The id → name map of the EXISTING tools among the given set (Story 3.3): the
-- DSGVO export resolves the display names of the tools the subject inspected /
-- reinstated, INCLUDING archived ones (the report covers the full fleet
-- history, so the active-only ListTools would drop archived rows). The read is
-- intra-module (Tool-owned tools, AD-8/AD-11). A tool id ABSENT from the
-- result (concurrent deletion) is simply a MISSING key — the export falls back
-- to the id itself, never a 404.
SELECT id, name
FROM tools
WHERE id = ANY($1::uuid[])
ORDER BY id;
