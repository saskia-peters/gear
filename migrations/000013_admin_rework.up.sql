-- G.E.A.R. Admin Rework Effort 1 (Spec 2.9, AD-12/AD-2/AD-6/FR-21/FR-22):
-- four changes that glue the isolated admin surfaces (Stories 2.4–2.7)
-- together:
--
--   1. `user_group_permission_groups` — a many-to-many linking an
--      ORGANISATIONAL user group (team) to the permission groups (roles) it
--      grants its members. This makes "role inherited via a team" possible:
--      a member of a team that holds a role inherits that role's permissions.
--   2. `user_qualifications.expires_at` — a per-user, per-assignment
--      valid-until override. A fixed (non-"unbegrenzt") qualification REQUIRES
--      an `expires_at` at assignment; the per-assignment value is read first
--      when deriving the display status, before the qualification's own
--      vocabulary `expires_at`.
--   3. Seed the new permission code `users.qualifications.manage` (assign/
--      revoke a qualification on a user + edit a user's per-qualification
--      `expires_at`).
--   4. Grants: `users.view` + `users.qualifications.manage` to `fuehrende` and
--      `schirrmeister`; `users.qualifications.manage` to `admin`. (Admin
--      already holds `users.view`.)
--
-- All INSERTs are idempotent (ON CONFLICT DO NOTHING) so the data-seeding
-- statements can be re-applied against any state without duplicates. The DDL
-- (CREATE TABLE / ALTER TABLE / CREATE INDEX) runs once per clean schema and is
-- the forward-migration contract — a re-run on an already-migrated schema is
-- the migrate tool's job, not a re-application of this file.

-- 1. user_group_permission_groups: the join table that lets a team grant roles.
CREATE TABLE user_group_permission_groups (
    user_group_id       uuid NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
    permission_group_id uuid NOT NULL REFERENCES permission_groups(id) ON DELETE CASCADE,
    created_at          timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_group_id, permission_group_id)
);

-- Role-side lookups (ListGroupRolesByUser / ListResolvedPermissionSources,
-- deleting a permission group) join on permission_group_id — index it
-- (review finding 2.9).
CREATE INDEX user_group_permission_groups_permission_group_id_idx
    ON user_group_permission_groups (permission_group_id);

-- 2. Per-user qualification valid-until override (nullable: no override).
ALTER TABLE user_qualifications
    ADD COLUMN expires_at timestamptz NULL;

-- 3. Seed the new `users.qualifications.manage` permission code.
INSERT INTO permissions (code, description)
VALUES ('users.qualifications.manage', 'Assign/revoke qualifications on users and edit per-user valid-until')
ON CONFLICT (code) DO NOTHING;

-- 4. Grants:
--    - fuehrende + schirrmeister get `users.view` (open the directory read-only)
--      and `users.qualifications.manage` (assign qualifications + valid-until).
--    - admin gets `users.qualifications.manage` (already holds `users.view`).
INSERT INTO permission_group_permissions (permission_group_id, permission_id)
SELECT g.id, p.id
FROM (VALUES
    ('fuehrende', 'users.view'),
    ('fuehrende', 'users.qualifications.manage'),
    ('schirrmeister', 'users.view'),
    ('schirrmeister', 'users.qualifications.manage'),
    ('admin', 'users.qualifications.manage')
) AS m(role, code)
JOIN permission_groups g ON g.name = m.role
JOIN permissions p ON p.code = m.code
ON CONFLICT DO NOTHING;
