-- Reverse of 000026_app_settings.up.sql: drop the Admin-owned app_settings
-- store (the seeded rows drop with it) and remove ONLY what this migration
-- added — the admin.settings.system grant (EVERY grant of it, incl. any custom
-- role granted it post-migration) and the permission code itself. No
-- pre-existing rows are touched beyond the revoked grants.
--
-- The grant rows are deleted BEFORE the permission row so the permissions
-- DELETE can never trip the FK or orphan a grant (000025 down pattern).

-- 1. Revoke EVERY admin.settings.system grant.
DELETE FROM permission_group_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code = 'admin.settings.system');

-- 2. Remove the seeded permission code itself.
DELETE FROM permissions
WHERE code = 'admin.settings.system';

-- 3. Drop the store.
DROP TABLE IF EXISTS app_settings;