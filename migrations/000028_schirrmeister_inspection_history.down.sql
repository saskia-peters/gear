-- Reverse of 000028_schirrmeister_inspection_history.up.sql: remove ONLY the
-- schirrmeister grant row this migration added. The `inspection.history.view`
-- permission code itself is NOT deleted — it predates this migration (000010)
-- and is still granted to admin + fuehrende. No pre-existing rows are touched
-- (the scoped reverse of the 000016 pattern, minus the code DELETE).

DELETE FROM permission_group_permissions
WHERE permission_group_id IN (SELECT id FROM permission_groups WHERE name = 'schirrmeister')
  AND permission_id IN (
      SELECT id FROM permissions WHERE code = 'inspection.history.view'
  );