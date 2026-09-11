-- Reverse of 000025_tool_inventory_number.up.sql: remove ONLY what this
-- migration added — the tool.edit permission + EVERY grant of it (incl. grants
-- to custom roles made after this migration), then the inventory-number
-- column/sequence/constraint. No pre-existing rows are touched beyond the
-- dropped column.

-- 1. Revoke EVERY tool.edit grant (the three seeded groups AND any custom role
--    granted it post-migration) — deleting all rows referencing the permission
--    id BEFORE removing the permission row, so the permissions DELETE can never
--    trip the FK or orphan a grant.
DELETE FROM permission_group_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code = 'tool.edit');

-- 2. Remove the seeded permission code itself.
DELETE FROM permissions
WHERE code = 'tool.edit';

-- 3. Drop the tools.inventory_number column (dropping the column also drops its
--    dependent index + constraint, but they are removed explicitly for clarity),
--    then the dedicated sequence.
ALTER TABLE tools DROP CONSTRAINT IF EXISTS tools_inventory_number_max_length;
DROP INDEX IF EXISTS tools_inventory_number_key;
ALTER TABLE tools DROP COLUMN IF EXISTS inventory_number;
DROP SEQUENCE IF EXISTS tools_inventory_number_seq;