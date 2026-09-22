-- G.E.A.R. progressive login lockout (Story 1.5, FR-3/AD-2/AD-11).
--
-- The User module owns the `login_attempts` table (AD-2/AD-11). It tracks
-- failed login attempts keyed by the normalized email so that UNKNOWN emails
-- also accumulate failures and reach HTTP 429 identically (anti-enumeration):
-- a 429 is not discriminating because every probed email can hit it. There is
-- deliberately NO FK to users — unknown emails have no user row. `failed_count`
-- holds the number of consecutive failed logins (capped); `lockout_until` is
-- set (nullable) when a threshold is crossed and gates further attempts until
-- it passes.

CREATE TABLE login_attempts (
    email         text PRIMARY KEY,
    failed_count  int NOT NULL DEFAULT 0,
    lockout_until timestamptz NULL,
    updated_at    timestamptz NOT NULL DEFAULT now()
);