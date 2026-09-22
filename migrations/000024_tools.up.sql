-- G.E.A.R. Tool-owned tools catalog (Story 4.3, FR-9/FR-10/AD-3/AD-5/AD-10).
--
-- The Tool Maintenance hexagon (internal/tools) owns this table — the config
-- write path runs exclusively through the Tool module's exported configuration
-- port (AD-10), never ad-hoc SQL over the same rows.
--
-- Each physical tool belongs to EXACTLY one tool type (intra-module FK to the
-- Tool-owned `tool_types`). The optional per-tool schedule OVERRIDE is a
-- first-class FK to the Admin-owned `schedules` catalog (AD-16) — an empty
-- override (schedule_id NULL) means the tool inherits its schedule from the
-- tool type's default at resolution time (AD-5); it is NEVER stored as JSONB
-- (AD-10). `attributes` jsonb is the no-migration extension surface for custom
-- metadata (FR-10, following the users/tool_types precedent AD-3) and defaults
-- to '{}'. Soft archive mirrors the schedules/tool_types convention: archiving
-- sets archived_at = now() so a row keeps its FK history intact; the active
-- surface filters archived_at IS NULL and archive is irreversible via the
-- surface in V1.
CREATE TABLE tools (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    name         text NOT NULL UNIQUE,
    tool_type_id uuid NOT NULL REFERENCES tool_types(id),
    schedule_id  uuid REFERENCES schedules(id),
    attributes   jsonb NOT NULL DEFAULT '{}',
    archived_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- Read index for the tools-of-a-type lookup (the Werkzeuge list joins the
-- Tool-owned tool_types for the type display name).
CREATE INDEX tools_tool_type_id_idx ON tools (tool_type_id);

-- Deterministic-read index: ListTools orders the active catalog by
-- created_at ASC (with a name tiebreaker) — an index on the leading sort key
-- keeps the growing catalog from a full scan.
CREATE INDEX tools_created_at_idx ON tools (created_at);