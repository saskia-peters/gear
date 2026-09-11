-- G.E.A.R. Tool-owned tool-type catalog (Story 4.2, FR-8/FR-10/FR-23/AD-10).
--
-- The Tool Maintenance hexagon (internal/tools) owns this table — the config
-- write path runs exclusively through the Tool module's exported configuration
-- port (AD-10), never ad-hoc SQL over the same rows.
--
-- V1 stores the typed core fields as typed columns (AD-3/AD-10): name (UNIQUE),
-- the default inspection schedule as a first-class FK to the Admin-owned
-- `schedules` catalog (AD-16), the single required qualification as a FK to the
-- User-owned `qualifications` vocabulary (AD-7/AD-11 — one FK column, NOT a
-- join table, per FR-8/FR-11), and the inspection mode as a CHECK-constrained
-- text column (pass_fail | checklist). `attributes` jsonb is the no-migration
-- extension surface for custom metadata (FR-10, following the users.attributes
-- precedent AD-3) and defaults to '{}'. Soft archive mirrors the schedules
-- convention: archiving sets archived_at = now() so a row keeps its FK history
-- intact; the active surface filters archived_at IS NULL and archive is
-- irreversible via the surface in V1.
CREATE TABLE tool_types (
    id                        uuid PRIMARY KEY DEFAULT uuidv7(),
    name                      text NOT NULL UNIQUE,
    default_schedule_id       uuid NOT NULL REFERENCES schedules(id),
    required_qualification_id uuid NOT NULL REFERENCES qualifications(id),
    inspection_mode           text NOT NULL
                              CHECK (inspection_mode IN ('pass_fail', 'checklist')),
    attributes                jsonb NOT NULL DEFAULT '{}',
    archived_at               timestamptz,
    created_at                timestamptz NOT NULL DEFAULT now(),
    updated_at                timestamptz NOT NULL DEFAULT now()
);

-- Ordered child rows of a tool type (FR-23): the codebase's first
-- position-ordered child table (spine: "ordering is code-level"). The label is
-- one checklist entry of the type's inspection template; `position` is the
-- explicit order the SPA submits and the store persists (full-replacement on
-- update — the surface always submits the whole ordered list). Child rows
-- cascade away with their parent type; archived or historical items are never
-- touched (FR-23: changes reflect on future inspections only).
CREATE TABLE tool_type_checklist_items (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    tool_type_id uuid NOT NULL REFERENCES tool_types(id) ON DELETE CASCADE,
    position    integer NOT NULL,
    label       text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Deterministic read order for the checklist-item queries: the ordered child
-- rows of a type are returned by position; the DB backstop for the app-level
-- ordering guarantee.
CREATE INDEX tool_type_checklist_items_tool_type_position_idx
    ON tool_type_checklist_items (tool_type_id, position);