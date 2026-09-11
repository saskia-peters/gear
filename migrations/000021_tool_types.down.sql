-- Reverse of 000021_tool_types.up.sql: drop the Tool-owned ordered child rows
-- first (the FK CASCADE would drop them anyway), then the tool-type catalog
-- table. No other object is touched.
DROP TABLE IF EXISTS tool_type_checklist_items;
DROP TABLE IF EXISTS tool_types;