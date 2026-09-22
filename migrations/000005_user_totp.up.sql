-- G.E.A.R. optional TOTP multi-factor authentication (Story 1.6, FR-4/NFR-S4).
--
-- The User module owns the `users` table (AD-2/AD-11). This migration adds a
-- single nullable column holding the TOTP shared secret ENCRYPTED AT REST
-- (NFR-S4): only the AES-256-GCM ciphertext produced with the app-level key
-- from GEAR_ENCRYPTION_KEY is persisted, never the plaintext secret. The
-- `is_mfa_enabled` boolean already exists (cold-start) and gates the two-step
-- login challenge; this column carries the encrypted secret a confirmed code
-- is validated against. NULL means the user has no MFA secret stored.
--
-- Two further nullable columns carry the SHORT-LIVED PENDING ENROLLMENT
-- (FR-4): when an authenticated user requests to enable MFA, the freshly
-- generated secret is persisted encrypted here with an expiry, and
-- `ConfirmMFAEnable` validates the submitted code against THIS server-issued
-- secret (never a client-supplied one). The pending state is cleared after
-- confirm, on success or failure. Both columns are NULL when no enrollment is
-- pending. (Review finding 1.6-1.)

ALTER TABLE users
    ADD COLUMN totp_secret_encrypted      text NULL,
    ADD COLUMN pending_totp_secret_encrypted text NULL,
    ADD COLUMN pending_totp_expires_at    timestamptz NULL;