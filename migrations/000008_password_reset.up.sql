-- G.E.A.R. self-service password reset (Story 1.8, FR-26/AD-13).
--
-- The User module owns the `users` table and the new `password_reset_tokens`
-- table (AD-2/AD-11, spine table 19). Reset tokens are single-use, valid at
-- most 30 minutes, and stored ONLY as a SHA-256 hash at rest — the raw token
-- is returned exactly once (in the emailed link) and never persisted/logged.
--
-- `users.must_change_password` implements the SMTP-not-configured fallback
-- (FR-26): when no email delivery is available, a reset request flags the
-- account so the next login forces a mandatory password change before any app
-- access (admin one-time-password fallback, Epic 2). It is cleared once the
-- user completes a change via the reset link.

CREATE TABLE password_reset_tokens (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash text NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX password_reset_tokens_user_id_idx ON password_reset_tokens (user_id);

ALTER TABLE users
    ADD COLUMN must_change_password boolean NOT NULL DEFAULT false;