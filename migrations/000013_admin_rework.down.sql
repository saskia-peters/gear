-- Reverse of 000013_admin_rework.up.sql: remove ONLY the rows this migration
-- added. The `user_group_permission_groups` table is dropped, the per-user
-- `expires_at` column on `user_qualifications` is dropped, the seeded
-- `users.qualifications.manage` permission code and its grant rows are removed.
--
-- The pre-existing `users.view` grant on `admin` (seeded by 000010) is NOT
-- touched. Removing the fuehrende/schirrmeister grants added here and the new
-- code must not delete rows that predate this migration.

-- Revoke the grants added here (fuehrende/schirrmeister users.view +
-- users.qualifications.manage; admin users.qualifications.manage).
DELETE FROM permission_group_permissions
WHERE permission_group_id IN (SELECT id FROM permission_groups WHERE name IN ('fuehrende', 'schirrmeister', 'admin'))
  AND permission_id IN (
      SELECT id FROM permissions WHERE code = 'users.qualifications.manage'
  );

DELETE FROM permission_group_permissions
WHERE permission_group_id IN (SELECT id FROM permission_groups WHERE name IN ('fuehrende', 'schirrmeister'))
  AND permission_id IN (
      SELECT id FROM permissions WHERE code = 'users.view'
  );

-- Remove the seeded permission code itself.
DELETE FROM permissions
WHERE code = 'users.qualifications.manage';

-- Drop the per-user valid-until column.
ALTER TABLE user_qualifications
    DROP COLUMN IF EXISTS expires_at;

-- Drop the join table.
DROP TABLE IF EXISTS user_group_permission_groups;
