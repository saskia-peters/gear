-- G.E.A.R. Admin-owned SMTP settings (Story 3.1, FR-28/AD-14).
--
-- The single runtime-configurable email-delivery row edited from the Admin
-- panel by holders of `admin.settings.email`. The User module's reset-email
-- sender consumes the row READ-ONLY through the Admin settings port (AD-14) —
-- this table is authored exclusively by the Admin module (AD-11).
--
-- The SMTP password is stored ENCRYPTED at rest (AES-256-GCM with the
-- app-level key from GEAR_ENCRYPTION_KEY, NFR-S4) and is write-only/masked:
-- it is never returned by GET and never logged. A missing/empty value means
-- "no password configured" (Configured()=false → FR-26 keeps the
-- must-change-password fallback).
--
-- Single-row semantics are enforced by a partial unique index on a constant,
-- so there can never be more than one settings row (uuidv7 id keeps the
-- ID/AD-11 convention).
CREATE TABLE smtp_settings (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    host               text NOT NULL,
    port               integer NOT NULL
                       CHECK (port BETWEEN 1 AND 65535),
    security           text NOT NULL DEFAULT 'none'
                       CHECK (security IN ('none', 'starttls', 'tls')),
    sender_address     text NOT NULL,
    sender_name        text NOT NULL DEFAULT '',
    username           text NOT NULL DEFAULT '',
    password_encrypted text NOT NULL DEFAULT '',
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

-- Single-row guarantee: at most one settings row exists at any time.
CREATE UNIQUE INDEX smtp_settings_single_row_idx ON smtp_settings ((true));