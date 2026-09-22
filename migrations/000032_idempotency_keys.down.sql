-- Reverse of 000032_idempotency_keys.up.sql: remove ONLY what this migration
-- added — the per-tool idempotency-key columns (dropping the column also drops
-- its dependent index, but the indexes are removed explicitly for clarity).
-- No pre-existing rows are touched beyond the dropped columns.
DROP INDEX IF EXISTS inspections_tool_id_idempotency_key_idx;
ALTER TABLE inspections DROP COLUMN IF EXISTS idempotency_key;

DROP INDEX IF EXISTS reinstatements_tool_id_idempotency_key_idx;
ALTER TABLE reinstatements DROP COLUMN IF EXISTS idempotency_key;