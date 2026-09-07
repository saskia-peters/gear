-- G.E.A.R. User & Group Administration (Story 2.6, AD-2/AD-3/AD-11, FR-21/
-- FR-22): the four missing spine tables — `user_groups` (organisational teams,
-- e.g. "Gruppe Ost"), `user_group_members` (who is in which team), the
-- `qualifications` vocabulary, and `user_qualifications` (which certificates a
-- person holds).
--
-- The User module owns all four (AD-11); every FK points into User-owned
-- tables. User groups are ORGANISATIONAL ONLY (AD-12): the permission-resolution
-- query never joins `user_groups`, so team membership grants no access by
-- construction — the permission-group/direct-grant path is the only thing that
-- affects the resolved permission set.
--
-- `qualifications.expiry_kind` distinguishes never-expiring qualifications
-- ('unlimited', the default) from those with a fixed validity period ('fixed',
-- where `expires_at` holds the expiry date). The user detail (Story 2.6)
-- DISPLAYS assignments with a computed status (Gültig / Bald ablaufend /
-- Abgelaufen / Unbegrenzt); creating/editing qualifications and assignments is
-- Story 2.7.

CREATE TABLE user_groups (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    name        text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_group_members (
    user_group_id uuid NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
    user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_group_id, user_id)
);

CREATE TABLE qualifications (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    name        text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    expiry_kind text NOT NULL DEFAULT 'unlimited'
                CHECK (expiry_kind IN ('unlimited', 'fixed')),
    expires_at  timestamptz NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_qualifications (
    user_id          uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    qualification_id uuid NOT NULL REFERENCES qualifications(id) ON DELETE CASCADE,
    assigned_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, qualification_id)
);