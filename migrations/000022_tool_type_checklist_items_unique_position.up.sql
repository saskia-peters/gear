-- G.E.A.R. Tool-owned ordered-child uniqueness (Story 4.2 review, FR-23).
--
-- tool_type_checklist_items is the codebase's first position-ordered child
-- table: `position` is the explicit order the SPA submits and the store
-- persists (full-replacement on update). Without a UNIQUE(tool_type_id,
-- position) constraint a direct DB write could store duplicate positions for
-- one type, making the read order nondeterministic. The constraint is the DB
-- backstop behind the app-level guarantee; the SQL read paths already ORDER BY
-- position.
ALTER TABLE tool_type_checklist_items
    ADD CONSTRAINT tool_type_checklist_items_tool_type_position_key
    UNIQUE (tool_type_id, position);