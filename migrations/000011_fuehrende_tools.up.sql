-- G.E.A.R. user decision (Story 2.3): `fuehrende` may add/maintain tools like
-- `schirrmeister`. This grants the two tool-administration codes
-- (`tools.manage`, `tool_types.manage`) to the `fuehrende` role, matching the
-- `schirrmeister` permission set.
--
-- The codes themselves already exist (seeded by 000010); this migration only
-- wires the two new `permission_group_permissions` rows for `fuehrende`. Every
-- INSERT is idempotent (ON CONFLICT DO NOTHING) so the migration can be
-- re-applied against any state without duplicates. The resolved permission set
-- query (Story 2.2) picks the new codes up automatically — no query change.
INSERT INTO permission_group_permissions (permission_group_id, permission_id)
SELECT g.id, p.id
FROM (VALUES
    ('fuehrende', 'tools.manage'),
    ('fuehrende', 'tool_types.manage')
) AS m(role, code)
JOIN permission_groups g ON g.name = m.role
JOIN permissions p ON p.code = m.code
ON CONFLICT DO NOTHING;
