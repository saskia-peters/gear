-- Reverse of 000011_fuehrende_tools.up.sql: remove ONLY the two rows this
-- migration added — `tools.manage` + `tool_types.manage` for `fuehrende`.
-- Nothing else (including the same rows on `schirrmeister`/`admin`, and the
-- permission codes themselves) is touched.
DELETE FROM permission_group_permissions
WHERE permission_group_id IN (SELECT id FROM permission_groups WHERE name = 'fuehrende')
  AND permission_id IN (
      SELECT id FROM permissions WHERE code IN ('tools.manage', 'tool_types.manage')
  );
