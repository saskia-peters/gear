-- User Directory & Auth module store (AD-2), generated into package postgres
-- by sqlc. These are the building blocks the auth port resolves identity and
-- permission sets from (AD-12: additive union of permission-group memberships
-- + direct grants). Story 1.1 ships the toolchain plus the first queries;
-- later stories extend this file and re-run `just sqlc-generate`.

-- name: GetPermissionByCode :one
SELECT id, code, description
FROM permissions
WHERE code = $1;

-- name: ListPermissionGroupsByUser :many
SELECT pg.id, pg.name, pg.description, pg.is_base_role
FROM permission_groups pg
JOIN user_permission_groups upg ON upg.permission_group_id = pg.id
WHERE upg.user_id = $1
ORDER BY pg.name;

-- name: CreateRegisteredUser :one
INSERT INTO users (
    email,
    display_name,
    first_name,
    last_name,
    password_hash,
    state
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    'pending_approval'
)
RETURNING id, email, display_name, first_name, last_name, password_hash, state, is_mfa_enabled, totp_secret_encrypted, pending_totp_secret_encrypted, pending_totp_expires_at, attributes, created_at, updated_at, pending_email;

-- name: GetUserByEmail :one
SELECT id, email, display_name, first_name, last_name, password_hash, state, is_mfa_enabled, totp_secret_encrypted, pending_totp_secret_encrypted, pending_totp_expires_at, attributes, created_at, updated_at, pending_email
FROM users
WHERE email = $1;

-- name: CreateSession :one
INSERT INTO sessions (user_id, token_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING id, user_id, token_hash, expires_at, created_at;

-- name: GetSessionByTokenHash :one
-- The session user snapshot carries the password hash so the authenticated
-- user can be verified server-side on the change-password flow (FR-25), just
-- as it carries the encrypted TOTP secret for MFA disable (FR-4). The hash is
-- never serialized to clients (core.User.PasswordHash is json:"-").
SELECT s.id, s.user_id, s.token_hash, s.expires_at, s.created_at,
       u.email, u.display_name, u.first_name, u.last_name, u.state, u.is_mfa_enabled, u.password_hash, u.totp_secret_encrypted, u.pending_totp_secret_encrypted, u.pending_totp_expires_at, u.attributes, u.pending_email
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = $1;

-- name: DeleteSessionByTokenHash :exec
-- Atomic logout (NFR-S2): delete by the hashed token directly so there is no
-- Get-then-Delete TOCTOU window.
DELETE FROM sessions
WHERE token_hash = $1;

-- name: DeleteSessionsByUser :exec
-- Revoke every session of a user (NFR-S2). Used when MFA is disabled so all
-- pre-existing sessions must re-authenticate (review finding 1.6-2).
DELETE FROM sessions
WHERE user_id = $1;

-- name: DeleteSessionsByUserExcept :exec
-- Revoke all of a user's sessions except the one identified by the given token
-- hash. Used when MFA is enabled so sessions issued before enrollment cannot
-- bypass the new second factor (review finding 1.6-2).
DELETE FROM sessions
WHERE user_id = $1 AND token_hash <> $2;

-- name: GetLoginAttempts :one
SELECT email, failed_count, lockout_until, updated_at
FROM login_attempts
WHERE email = $1;

-- name: IncrementLoginAttempts :exec
-- Atomic record of a failed login (FR-3): increments the per-email failure
-- counter and sets the progressive lockout window when a threshold is crossed,
-- in a single statement so concurrent attempts for the same email cannot lose
-- updates. The counter is capped (LockoutMaxFailedCount = 10). Thresholds and
-- durations mirror core.LockoutThreshold*/LockoutDuration*.
INSERT INTO login_attempts (email, failed_count, lockout_until)
VALUES ($1, 1, NULL)
ON CONFLICT (email) DO UPDATE SET
    failed_count  = LEAST(login_attempts.failed_count + 1, 10),
    lockout_until = CASE
        WHEN login_attempts.failed_count + 1 >= 4 THEN now() + interval '60 seconds'
        WHEN login_attempts.failed_count + 1 = 3 THEN now() + interval '30 seconds'
        ELSE NULL
    END,
    updated_at    = now();

-- name: ClearLoginAttempts :exec
UPDATE login_attempts
SET failed_count = 0, lockout_until = NULL, updated_at = now()
WHERE email = $1;

-- name: ListPermissionsByUser :many
-- Resolved permission set (AD-12): additive union of permission-group
-- memberships plus direct grants. Deduplicated via DISTINCT.
SELECT DISTINCT p.code
FROM permissions p
WHERE p.id IN (
    SELECT pgp.permission_id
    FROM user_permission_groups upg
    JOIN permission_group_permissions pgp ON pgp.permission_group_id = upg.permission_group_id
    WHERE upg.user_id = $1
    UNION
    SELECT up.permission_id
    FROM user_permissions up
    WHERE up.user_id = $1
)
ORDER BY p.code;

-- name: SetUserTotpSecret :exec
-- Enable TOTP MFA (FR-4): persist the AES-256-GCM encrypted shared secret and
-- flip the is_mfa_enabled flag in one statement, clearing any pending
-- enrollment. The plaintext secret is never stored (NFR-S4).
UPDATE users
SET totp_secret_encrypted         = $2,
    is_mfa_enabled                = true,
    pending_totp_secret_encrypted = NULL,
    pending_totp_expires_at       = NULL,
    updated_at                    = now()
WHERE id = $1;

-- name: ClearUserTotpSecret :exec
-- Disable TOTP MFA (FR-4): clear the stored encrypted secret, the flag and any
-- pending enrollment.
UPDATE users
SET totp_secret_encrypted         = NULL,
    is_mfa_enabled                = false,
    pending_totp_secret_encrypted = NULL,
    pending_totp_expires_at       = NULL,
    updated_at                    = now()
WHERE id = $1;

-- name: SetUserPendingTotpSecret :exec
-- Persist a short-lived pending TOTP enrollment (FR-4): the freshly generated
-- secret is stored ENCRYPTED at rest (NFR-S4) with an expiry. The confirm step
-- validates a code against THIS server-issued secret.
UPDATE users
SET pending_totp_secret_encrypted = $2,
    pending_totp_expires_at       = $3,
    updated_at                    = now()
WHERE id = $1;

-- name: ClearUserPendingTotpSecret :exec
-- Clear a pending TOTP enrollment after the confirm step (success or failure).
UPDATE users
SET pending_totp_secret_encrypted = NULL,
    pending_totp_expires_at       = NULL,
    updated_at                    = now()
WHERE id = $1;

-- name: UpdateUserPassword :one
-- Persist a new password hash for a user (FR-25). The plaintext password is
-- never stored, logged or returned (NFR-O1/AD-13); only the Argon2id hash
-- provided by the caller is written, with a fresh updated_at.
UPDATE users
SET password_hash = $2,
    updated_at    = now()
WHERE id = $1
RETURNING id, email, display_name, first_name, last_name, password_hash, state, is_mfa_enabled, totp_secret_encrypted, pending_totp_secret_encrypted, pending_totp_expires_at, attributes, created_at, updated_at, pending_email;

-- name: UpdateUserProfile :one
-- Persist the user's editable base data (first/last/display name, Story 2.1):
-- changes take effect immediately for the authenticated user. Only the caller-
-- supplied values are written; email and state are never touched here.
UPDATE users
SET first_name   = $2,
    last_name    = $3,
    display_name = $4,
    updated_at   = now()
WHERE id = $1
RETURNING id, email, display_name, first_name, last_name, password_hash, state, is_mfa_enabled, totp_secret_encrypted, pending_totp_secret_encrypted, pending_totp_expires_at, attributes, created_at, updated_at, pending_email;

-- name: StagePendingEmail :one
-- Persist a STAGED email change (Story 2.1): the new address is stored in
-- pending_email and the user stays ACTIVE on the current email until an admin
-- approves the change (Epic 2 admin workflow).
--
-- The UPDATE is conditional (review finding: TOCTOU email-collision guard): it
-- only proceeds while NO OTHER account holds the address as its current email
-- OR as an already-staged pending_email, compared case-insensitively with
-- lower(). This closes the window where a registration racing between the
-- core's GetUserByEmail pre-check and this UPDATE could leave pending_email
-- equal to another account's email. A row that matches no other account is
-- updated; otherwise zero rows are affected and the caller maps it to
-- ErrEmailInUse.
UPDATE users
SET pending_email = $2,
    updated_at    = now()
WHERE users.id = $1
  AND NOT EXISTS (
      SELECT 1 FROM users AS other
      WHERE other.id <> $1
        AND (lower(other.email) = lower($2) OR lower(other.pending_email) = lower($2))
  )
RETURNING id, email, display_name, first_name, last_name, password_hash, state, is_mfa_enabled, totp_secret_encrypted, pending_totp_secret_encrypted, pending_totp_expires_at, attributes, created_at, updated_at, pending_email;

-- name: ClearPendingEmail :exec
-- Clear a staged email change (pending_email -> NULL). Used by the Epic 2
-- admin workflow when the staged email becomes the real email or the change is
-- cancelled/rejected. Never touches the current email.
UPDATE users
SET pending_email = NULL,
    updated_at    = now()
WHERE id = $1;

-- name: InsertAuditEvent :exec
-- Append a row to the User-owned audit trail (NFR-O1/NFR-O2, spine table 11):
-- actor_user_id + operation + created_at. Never records password values or
-- other sensitive payloads. Written best-effort: a failure is logged, not
-- rolled back into the triggering operation (availability).
INSERT INTO audit_log (actor_user_id, operation)
VALUES ($1, $2);
