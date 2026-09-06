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
RETURNING id, email, display_name, first_name, last_name, password_hash, state, is_mfa_enabled, totp_secret_encrypted, pending_totp_secret_encrypted, pending_totp_expires_at, attributes, created_at, updated_at, pending_email, must_change_password;

-- name: GetUserByEmail :one
SELECT id, email, display_name, first_name, last_name, password_hash, state, is_mfa_enabled, totp_secret_encrypted, pending_totp_secret_encrypted, pending_totp_expires_at, attributes, created_at, updated_at, pending_email, must_change_password
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
       u.email, u.display_name, u.first_name, u.last_name, u.state, u.is_mfa_enabled, u.password_hash, u.totp_secret_encrypted, u.pending_totp_secret_encrypted, u.pending_totp_expires_at, u.attributes, u.pending_email, u.must_change_password
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
RETURNING id, email, display_name, first_name, last_name, password_hash, state, is_mfa_enabled, totp_secret_encrypted, pending_totp_secret_encrypted, pending_totp_expires_at, attributes, created_at, updated_at, pending_email, must_change_password;

-- name: UpdateUserProfile :one
-- Persist the user's editable base data (first/last/display name, Story 2.1) and
-- the full custom-attribute set (Story 1.9): the supplied attributes REPLACE the
-- stored JSONB map wholesale (additive-union-free contract). The repository
-- always sends a concrete value (marshalled map or '{}'); the COALESCE($5,
-- '{}'::jsonb) fallback is a defensive safety net guaranteeing the column can
-- never be set to NULL, even if a future caller passes a nil param. Absent-vs-
-- clear semantics live in the core (nil = leave unchanged, '{}' = clear), not
-- here. Changes take effect immediately for the authenticated user. Only the
-- caller-supplied values are written; email and state are never touched here.
UPDATE users
SET first_name   = $2,
    last_name    = $3,
    display_name = $4,
    attributes   = COALESCE($5, '{}'::jsonb),
    updated_at   = now()
WHERE id = $1
RETURNING id, email, display_name, first_name, last_name, password_hash, state, is_mfa_enabled, totp_secret_encrypted, pending_totp_secret_encrypted, pending_totp_expires_at, attributes, created_at, updated_at, pending_email, must_change_password;

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
RETURNING id, email, display_name, first_name, last_name, password_hash, state, is_mfa_enabled, totp_secret_encrypted, pending_totp_secret_encrypted, pending_totp_expires_at, attributes, created_at, updated_at, pending_email, must_change_password;

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
-- actor_user_id + operation + created_at, plus an optional operation_detail
-- (e.g. the admin-recovery Begründung and target email) and a severity
-- ('normal' by default, 'high' for recovery events). The repository passes a
-- concrete severity (default 'normal') so the column is never NULL. Never
-- records password values or other sensitive payloads. Written best-effort: a
-- failure is logged, not rolled back into the triggering operation
-- (availability).
INSERT INTO audit_log (actor_user_id, operation, operation_detail, severity)
VALUES ($1, $2, $3, $4);

-- name: InsertAuditEventAnonymous :exec
-- Append an audit row WITHOUT an actor (actor_user_id stays NULL). Used for
-- anti-enumeration paths that have no authenticated user, e.g. a forgot-password
-- request for an unknown email (review findings 1.8-3 / 1.8-10): enumeration
-- attempts leave a trail (NFR-O1) and the path performs comparable-cost work.
INSERT INTO audit_log (operation)
VALUES ($1);

-- name: CreatePasswordResetToken :exec
-- Issue a fresh single-use reset token (FR-26/AD-13): the data-modifying CTE
-- first invalidates EVERY earlier token of the user (only the latest request
-- stays valid), then stores the new one. Only the SHA-256 hash is persisted —
-- never the raw token.
WITH invalidated AS (
    DELETE FROM password_reset_tokens
    WHERE user_id = $1
)
INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
VALUES ($1, $2, $3);

-- name: GetPasswordResetTokenByHash :one
-- Resolve a reset token by its stored hash, joining the owning user so the
-- completion step can verify the account is still active (FR-26). The token is
-- looked up ONLY by hash; the raw token is never stored or queried. Only
-- NON-recovery (FR-26) tokens are matched: an admin-recovery token
-- (recovery_target_admin=true) can never be resolved via the forgot-password
-- path (FR-27 overrides FR-26 for admins).
SELECT t.id, t.user_id, t.token_hash, t.expires_at, t.created_at,
       u.email, u.display_name, u.first_name, u.last_name, u.state,
       u.is_mfa_enabled, u.password_hash, u.must_change_password
FROM password_reset_tokens t
JOIN users u ON u.id = t.user_id
WHERE t.token_hash = $1 AND t.recovery_target_admin = false;

-- name: ConsumePasswordResetToken :one
-- Atomic single-use consumption of a NON-recovery reset token (review finding
-- 1.8-5): the data-modifying CTE deletes the token in the SAME statement that
-- reads it, so two concurrent completions with the same token cannot both
-- succeed — the losing statement sees no row in the CTE and the caller maps the
-- resulting no-rows to ErrResetTokenInvalid. Only FR-26 tokens
-- (recovery_target_admin=false) are consumed here; an admin-recovery token is
-- consumed exclusively via ConsumeAdminRecoveryToken (FR-27). The owning user
-- is joined so the completion step can verify the account is still active.
WITH consumed AS (
    DELETE FROM password_reset_tokens
    WHERE token_hash = $1 AND recovery_target_admin = false
    RETURNING id, user_id, token_hash, expires_at, created_at
)
SELECT c.id, c.user_id, c.token_hash, c.expires_at, c.created_at,
       u.email, u.display_name, u.first_name, u.last_name, u.state,
       u.is_mfa_enabled, u.password_hash, u.must_change_password
FROM consumed c
JOIN users u ON u.id = c.user_id;

-- name: DeletePasswordResetToken :exec
-- Invalidate a single reset token after use (single-use, FR-26).
DELETE FROM password_reset_tokens
WHERE token_hash = $1;

-- name: DeleteExpiredPasswordResetTokens :exec
-- Lazy purge of a user's expired reset tokens (review finding 1.8-7): run on
-- each reset request so expired rows do not accumulate indefinitely. A
-- background sweeper is deferred.
DELETE FROM password_reset_tokens
WHERE user_id = $1 AND expires_at < now();

-- name: SetUserMustChangePassword :exec
-- Flag an active account so the next successful login forces a mandatory
-- password change (FR-26, SMTP-not-configured fallback / Epic 2 one-time
-- password).
UPDATE users
SET must_change_password = true,
    updated_at           = now()
WHERE id = $1;

-- name: ClearUserMustChangePassword :exec
-- Clear the mandatory-change flag once the user completes a password change
-- (via the forced flow or a reset link, FR-26).
UPDATE users
SET must_change_password = false,
    updated_at           = now()
WHERE id = $1;

-- name: IsUserInPermissionGroup :one
-- Reports whether the user is a member of the named permission group (AD-12),
-- e.g. the 'admin' group. Drives server-authoritative ADMIN module visibility
-- in the SPA (Story 1.8).
SELECT EXISTS (
    SELECT 1
    FROM user_permission_groups upg
    JOIN permission_groups pg ON pg.id = upg.permission_group_id
    WHERE upg.user_id = $1 AND pg.name = $2
);

-- name: CountActiveAdmins :one
-- Counts the users who are BOTH active AND members of the admin permission
-- group (FR-27 last-admin guard): recovery of the last remaining active admin
-- is deliberately disabled via self-service. Two statements are avoided; the
-- count is derived from admin-group membership joined to live state.
SELECT COUNT(*)::bigint
FROM users u
JOIN user_permission_groups upg ON upg.user_id = u.id
JOIN permission_groups pg        ON pg.id = upg.permission_group_id
WHERE pg.name = 'admin' AND u.state = 'active';

-- name: CreateAdminRecoveryRequest :exec
-- Issue a fresh admin-recovery request (FR-27): a single-use, hashed, 30-min
-- token row marked recovery_target_admin=true for the target admin, stamped
-- with the requesting admin (requested_by_user_id). The data-modifying CTE
-- first invalidates EVERY earlier recovery token of the user (only the latest
-- request stays valid), then stores the new one. A recovery token is NOT
-- usable until another admin approves it (approved_by_user_id is set via
-- ApproveAdminRecovery).
WITH invalidated AS (
    DELETE FROM password_reset_tokens
    WHERE user_id = $1 AND recovery_target_admin = true
)
INSERT INTO password_reset_tokens (user_id, token_hash, expires_at, recovery_target_admin, requested_by_user_id)
VALUES ($1, $2, $3, true, $4);

-- name: GetAdminRecoveryTokenByHash :one
-- Resolve an admin-recovery token by its stored hash, joining the owning user
-- so the completion step can verify the account is still active (FR-27). Only
-- recovery-marked tokens are matched (a FR-26 forgot token with the same hash
-- can never satisfy a recovery completion); the raw token is never stored or
-- queried. The result carries the approver and the requester so the core can
-- enforce that an approved token was actually approved by a DIFFERENT admin.
SELECT t.id, t.user_id, t.token_hash, t.expires_at, t.created_at,
       t.recovery_target_admin, t.approved_by_user_id, t.requested_by_user_id,
       u.email, u.display_name, u.first_name, u.last_name, u.state,
       u.is_mfa_enabled, u.password_hash, u.must_change_password
FROM password_reset_tokens t
JOIN users u ON u.id = t.user_id
WHERE t.token_hash = $1 AND t.recovery_target_admin = true;

-- name: ConsumeAdminRecoveryToken :one
-- Atomic single-use consumption of an APPROVED admin-recovery token (FR-27):
-- the data-modifying CTE deletes the token in the SAME statement that reads
-- it, so two concurrent completions with the same token cannot both succeed —
-- the losing statement sees no row and the caller rejects it. Only tokens that
-- are recovery-marked AND approved are consumed (a not-yet-approved or FR-26
-- token is never matched). The owning user is joined for the active-state
-- check.
WITH consumed AS (
    DELETE FROM password_reset_tokens
    WHERE token_hash = $1 AND recovery_target_admin = true AND approved_by_user_id IS NOT NULL
    RETURNING id, user_id, token_hash, expires_at, created_at, approved_by_user_id, requested_by_user_id
)
SELECT c.id, c.user_id, c.token_hash, c.expires_at, c.created_at, c.approved_by_user_id, c.requested_by_user_id,
       u.email, u.display_name, u.first_name, u.last_name, u.state,
       u.is_mfa_enabled, u.password_hash, u.must_change_password
FROM consumed c
JOIN users u ON u.id = c.user_id;

-- name: ApproveAdminRecovery :one
-- Approve a pending admin-recovery request (FR-27): stamp the approving admin
-- (approved_by_user_id) on the recovery-marked token row for the target user,
-- mint a FRESH single-use token hash for it — the raw token is generated at
-- approve time (never stored) and returned ONLY to the approving admin (B),
-- who hands it to the recovered admin (A) out-of-band — and reset the expiry
-- to a FRESH 30 minutes from approve time (review finding: refresh token
-- expiry on approval). Only the LATEST pending (not-yet-approved) recovery
-- token is approved, so a stale earlier request cannot be approved. A zero-row
-- update (no pending request, already approved, or expired) maps to
-- ErrAdminRecoveryInvalid. Self-approval is rejected in the core, never here.
UPDATE password_reset_tokens
SET approved_by_user_id = $2,
    token_hash          = $3,
    expires_at          = now() + interval '30 minutes',
    created_at          = created_at
WHERE user_id = $1
  AND recovery_target_admin = true
  AND approved_by_user_id IS NULL
  AND expires_at > now()
RETURNING id;

-- name: ListAdminRecoveryRequest :many
-- List pending (not-yet-approved) admin-recovery requests (FR-27) joined to
-- their target user, newest first, for the admin-B review surface. The
-- password hash is deliberately NOT selected (review finding: never expose a
-- secret through the listing surface).
SELECT t.id, t.user_id, t.token_hash, t.expires_at, t.created_at,
       t.recovery_target_admin, t.approved_by_user_id, t.requested_by_user_id,
       u.email, u.display_name, u.first_name, u.last_name, u.state,
       u.is_mfa_enabled, u.must_change_password
FROM password_reset_tokens t
JOIN users u ON u.id = t.user_id
WHERE t.recovery_target_admin = true AND t.approved_by_user_id IS NULL
ORDER BY t.created_at DESC;

-- name: DenyAdminRecovery :exec
-- Deny a pending admin-recovery request (FR-27): invalidate the target's
-- recovery-marked, not-yet-approved token so it can no longer be approved. A
-- zero-row delete (no pending request, already approved, or expired) is a
-- no-op — the deny audit is still written by the core (NFR-O1).
DELETE FROM password_reset_tokens
WHERE user_id = $1
  AND recovery_target_admin = true
  AND approved_by_user_id IS NULL
  AND expires_at > now();
