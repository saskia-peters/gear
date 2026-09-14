-- Reverse of 000027_inspections.up.sql: drop the Tool-owned inspection +
-- reinstatement tables (FK history to tools is irrelevant — the tables
-- themselves are removed). inspection_items drops FIRST (its FK references
-- inspections); inspections + reinstatements reference only tools, which stays.
DROP TABLE IF EXISTS inspection_items;
DROP TABLE IF EXISTS inspections;
DROP TABLE IF EXISTS reinstatements;