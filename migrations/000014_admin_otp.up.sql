-- G.E.A.R. Admin One-Time-Password (OTP) Issuance (Spec 2.8, FR-26 Epic 2):
-- an admin can hand an ACTIVE, password-lost user a single-use one-time
-- password so they can re-enter and run the forced password-change flow (Story
-- 1.8). The OTP is stored as an Argon2id hash (like passwords, NFR-S4) with an
-- expiry — the plaintext is shown once in the issuance response and never
-- stored, emailed or read back. OTPs are ACTIVE-only: a deactivated or
-- pending_approval account cannot hold one, because deactivation means "sofort
-- kein Login" and the OTP is a recovery credential for a lost password, never a
-- reactivation path.

-- `one_time_password_hash` is the Argon2id hash of the currently valid OTP
-- (empty string = no OTP issued). `one_time_password_expires_at` is its TTL
-- (NULL = no OTP issued). Re-issuing simply replaces both (the old OTP becomes
-- invalid immediately).
ALTER TABLE users
    ADD COLUMN one_time_password_hash text NOT NULL DEFAULT '',
    ADD COLUMN one_time_password_expires_at timestamptz NULL;