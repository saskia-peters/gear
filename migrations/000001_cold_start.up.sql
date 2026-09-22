-- G.E.A.R. cold-start schema (Story 1.1).
--
-- Created here: the User-owned identity tables the cold-start seeder needs
-- plus the `admin.recovery.approve` permission, the four base roles
-- (AD-12) and the two seeded admin accounts (FR-27/AD-13). Later stories add
-- their own incremental migrations; future stories' tables are NOT created
-- here (spec: do NOT pre-create future stories' tables now).
--
-- Conventions (architecture spine): UUID v7 primary keys, UTC timestamps,
-- one owning module per table (AD-11), flat additive permission model (AD-12).

CREATE TABLE users (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    email           text NOT NULL UNIQUE,
    display_name    text NOT NULL,
    state           text NOT NULL DEFAULT 'pending_approval'
                    CHECK (state IN ('pending_approval', 'active', 'deactivated')),
    is_mfa_enabled  boolean NOT NULL DEFAULT false,
    attributes      jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE permissions (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    code        text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Permission groups are the roles; the four base roles are just pre-seeded
-- permission groups (AD-12). Flat: no groups inside groups in V1.
CREATE TABLE permission_groups (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    name         text NOT NULL UNIQUE,
    description  text NOT NULL DEFAULT '',
    is_base_role boolean NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE permission_group_permissions (
    permission_group_id uuid NOT NULL REFERENCES permission_groups(id) ON DELETE CASCADE,
    permission_id       uuid NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    created_at          timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (permission_group_id, permission_id)
);

-- A user's resolved permission set is the additive union of these
-- memberships plus their direct grants (user_permissions) below (AD-12).
CREATE TABLE user_permission_groups (
    user_id             uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    permission_group_id uuid NOT NULL REFERENCES permission_groups(id) ON DELETE CASCADE,
    created_at          timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, permission_group_id)
);

-- Direct one-off grants; additive, no negative/deny entries (AD-12).
CREATE TABLE user_permissions (
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    permission_id uuid NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    granted_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, permission_id)
);

-- ============================================================================
-- Cold-start seed (AD-12 role matrix, AD-13 seeding note)
-- ============================================================================

-- The dual-admin credential-recovery gate (FR-27): admin-only, exercised by
-- the second admin. Additional permission rows land with their stories.
INSERT INTO permissions (code, description) VALUES
    ('admin.recovery.approve',
     'Authorize credential recovery for a locked-out or forgotten admin (FR-27/AD-13)');

-- The four base roles.
INSERT INTO permission_groups (name, description, is_base_role) VALUES
    ('helfende',      'Volunteer who inspects tools (base role)', true),
    ('schirrmeister', 'Equipment caretaker: volunteer plus tool/tool-type management (base role)', true),
    ('fuehrende',     'Leadership: inspection, history, report export, reinstate (base role)', true),
    ('admin',         'Full access (base role)', true);

-- The admin role carries admin.recovery.approve (AD-13 seeding note).
INSERT INTO permission_group_permissions (permission_group_id, permission_id)
SELECT g.id, p.id
FROM permission_groups g, permissions p
WHERE g.name = 'admin' AND p.code = 'admin.recovery.approve';

-- The two seeded admin accounts (FR-27/AD-13). Identity rows only: login
-- credentials are generated and distributed out-of-band via environment
-- secrets (never in VCS); credential storage lands with Story 1.4.
INSERT INTO users (email, display_name, state) VALUES
    ('admin.1@gear.local', 'Admin 1', 'active'),
    ('admin.2@gear.local', 'Admin 2', 'active');

-- Both admins hold the admin role.
INSERT INTO user_permission_groups (user_id, permission_group_id)
SELECT u.id, g.id
FROM users u, permission_groups g
WHERE u.email IN ('admin.1@gear.local', 'admin.2@gear.local')
  AND g.name = 'admin';
