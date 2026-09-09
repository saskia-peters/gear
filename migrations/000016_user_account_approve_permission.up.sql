-- G.E.A.R. account-approval permission (2026-09-09): adds a dedicated
-- `user.account.approve` permission so the admin-recovery DENY surface (and any
-- future account-approval gate) is not reachable by every admin-module-code
-- holder (retro finding F11). It is granted to the admin base role.
--
-- All INSERTs are idempotent (ON CONFLICT DO NOTHING) so the data-seeding
-- statements can be re-applied against any state without duplicates.

-- 1. Seed the `user.account.approve` permission code.
INSERT INTO permissions (code, description)
VALUES ('user.account.approve', 'Approve or deny user account / recovery actions')
ON CONFLICT (code) DO NOTHING;

-- 2. Grant to the admin base role.
INSERT INTO permission_group_permissions (permission_group_id, permission_id)
SELECT g.id, p.id
FROM (VALUES ('admin', 'user.account.approve')) AS m(role, code)
JOIN permission_groups g ON g.name = m.role
JOIN permissions p ON p.code = m.code
ON CONFLICT DO NOTHING;