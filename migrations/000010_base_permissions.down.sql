-- Reverse of 000010_base_permissions.up.sql: remove ONLY the rows this
-- migration added — the 20 base permission codes it seeded and every
-- permission_group_permissions row referencing them. `admin.recovery.approve`
-- (and its admin-group row from Story 1.1) predates this migration and is
-- preserved.
DELETE FROM permission_group_permissions
WHERE permission_id IN (
    SELECT id FROM permissions WHERE code IN (
        'dashboard.view',
        'inspection.submit',
        'inspection.history.view',
        'report.export',
        'tool.reinstate',
        'tools.manage',
        'tool_types.manage',
        'users.view',
        'users.approve',
        'users.manage',
        'user_groups.manage',
        'roles.create',
        'roles.edit',
        'roles.assign',
        'qualifications.manage',
        'dsgvo.access_report',
        'dsgvo.delete',
        'admin.settings.email',
        'admin.settings.backup',
        'schedules.manage'
    )
);

DELETE FROM permissions
WHERE code IN (
    'dashboard.view',
    'inspection.submit',
    'inspection.history.view',
    'report.export',
    'tool.reinstate',
    'tools.manage',
    'tool_types.manage',
    'users.view',
    'users.approve',
    'users.manage',
    'user_groups.manage',
    'roles.create',
    'roles.edit',
    'roles.assign',
    'qualifications.manage',
    'dsgvo.access_report',
    'dsgvo.delete',
    'admin.settings.email',
    'admin.settings.backup',
    'schedules.manage'
);