-- Reverse of 000022_tool_type_checklist_items_unique_position.up.sql: drop the
-- ordered-child uniqueness constraint. No other object is touched.
ALTER TABLE tool_type_checklist_items
    DROP CONSTRAINT IF EXISTS tool_type_checklist_items_tool_type_position_key;