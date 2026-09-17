-- G.E.A.R. Schirrmeister inspection-history grant (Story 6.3, FR-18): the
-- frozen spec names Schirrmeister as a history viewer, but migration 000010
-- seeded `inspection.history.view` only for admin + fuehrende. This migration
-- grants the EXISTING permission code to the schirrmeister base role.
--
-- The code itself is NOT created here — it already exists (000010). Only the
-- grant row is added; no pre-existing rows are touched. Idempotent
-- (ON CONFLICT DO NOTHING) so the data-seeding statement can be re-applied
-- against any state without duplicates (the 000016 pattern: JOIN by
-- permission_groups.name + permissions.code).

INSERT INTO permission_group_permissions (permission_group_id, permission_id)
SELECT g.id, p.id
FROM (VALUES ('schirrmeister', 'inspection.history.view')) AS m(role, code)
JOIN permission_groups g ON g.name = m.role
JOIN permissions p ON p.code = m.code
ON CONFLICT DO NOTHING;