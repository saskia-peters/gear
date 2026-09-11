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
-- is intentionally omitted — the DB default '{}' applies (FR-10/AD-3). A name
-- already held by ANY row (active or archived) trips the UNIQUE constraint and
-- is mapped by the repository to the German duplicate-name 400.
INSERT INTO tool_types (name, default_schedule_id, required_qualification_id, inspection_mode)
VALUES ($1, $2, $3, $4)
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
-- column is left untouched (Story 4.4 owns its surface).
UPDATE tool_types
SET name = $2,
    default_schedule_id = $3,
    required_qualification_id = $4,
    inspection_mode = $5,
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
