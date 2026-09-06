-- G.E.A.R. base permission series + role matrix (Story 2.2, AD-12).
--
-- Story 1.1 seeded only `admin.recovery.approve` (and wired it to the admin
-- group). This migration seeds the remaining 20 base permission codes and
-- wires the FULL base role → permission matrix from the architecture spine
-- role-matrix table (AD-12):
--
--   admin         = all 21 codes
--   helfende      = dashboard.view + inspection.submit
--   schirrmeister = dashboard.view, inspection.submit, tools.manage, tool_types.manage
--   fuehrende     = dashboard.view, inspection.submit, inspection.history.view,
--                   report.export, tool.reinstate
--
-- Every INSERT is idempotent (ON CONFLICT DO NOTHING) so the migration can be
-- re-applied against any state without duplicates. `admin.recovery.approve`
-- and its admin-group row predate this migration (Story 1.1); the down
-- migration never removes them.

INSERT INTO permissions (code, description)
VALUES
    ('dashboard.view',         'View the dashboard'),
    ('inspection.submit',      'Submit an inspection'),
    ('inspection.history.view','View inspection history'),
    ('report.export',          'Export reports'),
    ('tool.reinstate',         'Reinstate a tool'),
    ('tools.manage',           'Manage tools'),
    ('tool_types.manage',      'Manage tool types'),
    ('users.view',             'View users'),
    ('users.approve',          'Approve user registrations'),
    ('users.manage',           'Manage users'),
    ('user_groups.manage',     'Manage user groups'),
    ('roles.create',           'Create roles'),
    ('roles.edit',             'Edit roles'),
    ('roles.assign',           'Assign roles'),
    ('qualifications.manage',  'Manage qualifications'),
    ('dsgvo.access_report',    'Access GDPR (DSGVO) reports'),
    ('dsgvo.delete',           'Delete data (GDPR)'),
    ('admin.settings.email',   'Manage email settings'),
    ('admin.settings.backup',  'Manage backups'),
    ('schedules.manage',       'Manage schedules')
ON CONFLICT (code) DO NOTHING;

-- The base role → permission matrix (AD-12). `admin` carries all 21 codes;
-- the pre-existing admin.recovery.approve row from Story 1.1 is covered by the
-- ON CONFLICT DO NOTHING (same primary key).
INSERT INTO permission_group_permissions (permission_group_id, permission_id)
SELECT g.id, p.id
FROM (VALUES
    ('admin', 'dashboard.view'),
    ('admin', 'inspection.submit'),
    ('admin', 'inspection.history.view'),
    ('admin', 'report.export'),
    ('admin', 'tool.reinstate'),
    ('admin', 'tools.manage'),
    ('admin', 'tool_types.manage'),
    ('admin', 'users.view'),
    ('admin', 'users.approve'),
    ('admin', 'users.manage'),
    ('admin', 'user_groups.manage'),
    ('admin', 'roles.create'),
    ('admin', 'roles.edit'),
    ('admin', 'roles.assign'),
    ('admin', 'qualifications.manage'),
    ('admin', 'dsgvo.access_report'),
    ('admin', 'dsgvo.delete'),
    ('admin', 'admin.recovery.approve'),
    ('admin', 'admin.settings.email'),
    ('admin', 'admin.settings.backup'),
    ('admin', 'schedules.manage'),
    ('helfende', 'dashboard.view'),
    ('helfende', 'inspection.submit'),
    ('schirrmeister', 'dashboard.view'),
    ('schirrmeister', 'inspection.submit'),
    ('schirrmeister', 'tools.manage'),
    ('schirrmeister', 'tool_types.manage'),
    ('fuehrende', 'dashboard.view'),
    ('fuehrende', 'inspection.submit'),
    ('fuehrende', 'inspection.history.view'),
    ('fuehrende', 'report.export'),
    ('fuehrende', 'tool.reinstate')
) AS m(role, code)
JOIN permission_groups g ON g.name = m.role
JOIN permissions p ON p.code = m.code
ON CONFLICT DO NOTHING;