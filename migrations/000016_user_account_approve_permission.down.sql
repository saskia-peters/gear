-- Reverse of 000016_user_account_approve_permission.up.sql: remove ONLY the
-- rows this migration added — the `user.account.approve` grant and the code
-- itself. No pre-existing rows are touched.

-- Revoke the grant added here (admin only).
DELETE FROM permission_group_permissions
WHERE permission_group_id IN (SELECT id FROM permission_groups WHERE name = 'admin')
  AND permission_id IN (
      SELECT id FROM permissions WHERE code = 'user.account.approve'
  );

-- Remove the seeded permission code itself.
DELETE FROM permissions
WHERE code = 'user.account.approve';