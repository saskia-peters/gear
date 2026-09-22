-- G.E.A.R. server-side authentication sessions (Story 1.4, NFR-S2/AD-2/AD-11).
--
-- The User module owns the `sessions` table (AD-2/AD-11). Only the SHA-256
-- hash of the opaque session token is stored at rest (defense-in-depth): the
-- raw token is returned to the client once and never persisted. Sessions have
-- an idle expiry (`expires_at`) defaulting to 8h and are invalidated
-- server-side on logout (NFR-S2).

CREATE TABLE sessions (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash text NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);
