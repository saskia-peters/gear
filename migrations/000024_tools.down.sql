-- Reverse of 000024_tools.up.sql: drop the Tool-owned tools catalog table. No
-- other object is touched (the tool_types/schedules tables it referenced stay).
DROP TABLE IF EXISTS tools;