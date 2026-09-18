-- G.E.A.R. DSGVO account deletion (Story 3.4, FR-24/AD-8): the User-owned
-- ARCHIVE table for soft-deleted accounts plus the `deleted` account state.
--
-- DELETION NEVER HARD-DELETES (user decision 2026-09-18): the account becomes a
-- scrubbed `deleted` tombstone (login permanently blocked — only `active`
-- authenticates) and its personal data moves into `dsgvo_deleted_accounts`.
-- The archive + tombstone are hard-deleted ONLY when an admin invokes the
-- on-demand purge (the sole hard delete).
--
-- The archive row mirrors the tool-snapshot pattern (AD-8/3.4): `original_user_id`
-- is a PLAIN uuid with NO FK — the archive must survive the users-row hard
-- delete on purge, so no reference may tie it to the (eventually deleted) user.
-- `deleted_by` is likewise a plain uuid (the deleting admin, audited). The
-- snapshot EXCLUDES secrets (password hash, TOTP, OTP — the scrubbed tombstone
-- carries none and the archive never did).
CREATE TABLE dsgvo_deleted_accounts (
    id                uuid PRIMARY KEY DEFAULT uuidv7(),
    original_user_id  uuid NOT NULL,
    email             text NOT NULL,
    display_name      text NOT NULL,
    first_name        text NOT NULL DEFAULT '',
    last_name         text NOT NULL DEFAULT '',
    attributes        jsonb NOT NULL DEFAULT '{}'::jsonb,
    reason            text NOT NULL,
    deleted_by        uuid,
    created_at        timestamptz NOT NULL DEFAULT now(),
    deleted_at        timestamptz NOT NULL DEFAULT now()
);

-- The admin "Gelöschte Konten" list reads newest-first (deleted_at DESC).
CREATE INDEX dsgvo_deleted_accounts_deleted_at_idx
    ON dsgvo_deleted_accounts (deleted_at DESC);

-- Admit the `deleted` account state (the scrubbed tombstone). The existing
-- CHECK (000001) only allows pending_approval/active/deactivated; the state is
-- re-declared with the new value admitted. DROP the auto-named constraint and
-- re-add it with the extended set.
ALTER TABLE users DROP CONSTRAINT users_state_check;
ALTER TABLE users ADD CONSTRAINT users_state_check
    CHECK (state IN ('pending_approval', 'active', 'deactivated', 'deleted'));
