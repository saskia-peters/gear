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
RETURNING id, email, display_name, first_name, last_name, password_hash, state, is_mfa_enabled, totp_secret_encrypted, pending_totp_secret_encrypted, pending_totp_expires_at, attributes, created_at, updated_at, pending_email, must_change_password, one_time_password_hash, one_time_password_expires_at;

-- name: GetUserByEmail :one
SELECT id, email, display_name, first_name, last_name, password_hash, state, is_mfa_enabled, totp_secret_encrypted, pending_totp_secret_encrypted, pending_totp_expires_at, attributes, created_at, updated_at, pending_email, must_change_password, one_time_password_hash, one_time_password_expires_at
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
-- Resolved permission set (AD-12): additive union of
--   1. individual permission-group (role) memberships,
--   2. roles INHERITED via organisational user-groups (a team holding a role
--      grants its members that role's permissions, Spec 2.9), and
--   3. direct one-off grants.
-- Deduplicated via DISTINCT; no precedence, no subtraction. Resolved live per
-- request (FR-21/FR-22) — removing a role from a group revokes it from all
-- members on the next request.
SELECT DISTINCT p.code
FROM permissions p
WHERE p.id IN (
    SELECT pgp.permission_id
    FROM user_permission_groups upg
    JOIN permission_group_permissions pgp ON pgp.permission_group_id = upg.permission_group_id
    WHERE upg.user_id = $1
    UNION
    SELECT pgp.permission_id
    FROM user_group_members ugm
    JOIN user_group_permission_groups ugpg ON ugpg.user_group_id = ugm.user_group_id
    JOIN permission_group_permissions pgp ON pgp.permission_group_id = ugpg.permission_group_id
    WHERE ugm.user_id = $1
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
RETURNING id, email, display_name, first_name, last_name, password_hash, state, is_mfa_enabled, totp_secret_encrypted, pending_totp_secret_encrypted, pending_totp_expires_at, attributes, created_at, updated_at, pending_email, must_change_password, one_time_password_hash, one_time_password_expires_at;

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
RETURNING id, email, display_name, first_name, last_name, password_hash, state, is_mfa_enabled, totp_secret_encrypted, pending_totp_secret_encrypted, pending_totp_expires_at, attributes, created_at, updated_at, pending_email, must_change_password, one_time_password_hash, one_time_password_expires_at;

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
RETURNING id, email, display_name, first_name, last_name, password_hash, state, is_mfa_enabled, totp_secret_encrypted, pending_totp_secret_encrypted, pending_totp_expires_at, attributes, created_at, updated_at, pending_email, must_change_password, one_time_password_hash, one_time_password_expires_at;

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

-- name: SetUserOneTimePassword :execrows
-- Upsert an admin-issued one-time password for an ACTIVE account (Spec 2.8):
-- stores the Argon2id hash of a freshly generated OTP with its TTL expiry AND
-- flips must_change_password so the next login forces the Story 1.8 change
-- flow. The plaintext OTP is never stored (NFR-S4). Re-issuing REPLACES the
-- hash/expiry, so the old OTP is invalid immediately (RE_ISSUE). A zero-row
-- update (the target vanished between the eligibility read and this write) is
-- reported via :execrows so the caller never hands over a credential that
-- cannot work.
UPDATE users
SET one_time_password_hash          = $2,
    one_time_password_expires_at    = $3,
    must_change_password            = true,
    updated_at                      = now()
WHERE id = $1;

-- name: ClearUserOneTimePassword :execrows
-- Atomic compare-and-swap consumption of a single-use one-time password (Spec
-- 2.8 Design Notes): clears the OTP hash+expiry ONLY while the stored hash
-- still equals the presented one, so two concurrent logins with the same OTP
-- cannot both succeed — the losing statement affects zero rows and the caller
-- treats it as an already-consumed OTP (login fails).
UPDATE users
SET one_time_password_hash       = '',
    one_time_password_expires_at = NULL,
    updated_at                   = now()
WHERE id = $1 AND one_time_password_hash = $2;

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

-- name: ListPendingUsers :many
-- Pending-approval users for the admin approval surface (Story 2.4, FR-20):
-- the submitted profile details plus id and created_at, oldest first. The
-- password hash and other secret material are deliberately NOT selected — the
-- listing must never expose credentials (NFR-O1).
SELECT id, email, first_name, last_name, created_at
FROM users
WHERE state = 'pending_approval'
ORDER BY created_at ASC, id ASC;

-- name: SetUserState :one
-- Conditional account-state transition (Story 2.4, FR-20): flips a user from
-- an EXPECTED current state to a new state — pending_approval -> active on
-- approve, pending_approval -> deactivated on reject — in one statement. The
-- WHERE guard makes the transition atomic against a concurrent change: a
-- zero-row update (unknown id, or the user left the expected state) is a no-op
-- the caller maps to the uniform not-found/conflict (no existence leak beyond
-- what the admin already sees, FR-19). Also used later for deactivate.
UPDATE users
SET state = @state_new, updated_at = now()
WHERE id = @id AND state = @state_current
RETURNING id, email, display_name, first_name, last_name, password_hash, state, is_mfa_enabled, totp_secret_encrypted, pending_totp_secret_encrypted, pending_totp_expires_at, attributes, created_at, updated_at, pending_email, must_change_password, one_time_password_hash, one_time_password_expires_at;

-- name: AddUserToGroup :exec
-- Idempotent role seed (Story 2.4, AD-2/AD-12): grants the named permission
-- group (e.g. the default 'helfende' role) to the user. ON CONFLICT DO NOTHING
-- makes re-application a no-op, so an already-in-helfende user is never
-- duplicated.
INSERT INTO user_permission_groups (user_id, permission_group_id)
SELECT @user_id, g.id
FROM permission_groups g
WHERE g.name = @group_name
ON CONFLICT DO NOTHING;

-- ============================================================================
-- Role & Permission-Group Management (Story 2.5, AD-12/AD-6/FR-19)
-- ============================================================================

-- name: ListPermissionGroups :many
-- Every permission group (the four base roles + any custom named groups,
-- AD-12), base-roles-first then name, for the admin "Rollen" surface. No secret
-- material is selected.
SELECT id, name, description, is_base_role
FROM permission_groups
ORDER BY is_base_role DESC, name;

-- name: ListPermissionsByGroup :many
-- The permission codes (with their labels) granted to ONE permission group,
-- ordered by code. Used to build each role's checked set on the list surface.
SELECT p.code, p.description
FROM permissions p
JOIN permission_group_permissions pgp ON pgp.permission_id = p.id
WHERE pgp.permission_group_id = $1
ORDER BY p.code;

-- name: ListGroupPermissionsByGroupIDs :many
-- The permission codes of EVERY permission group in ONE query (Story 2.5): the
-- grouped counterpart of ListPermissionsByGroup that lets ListGroups assemble
-- all groups' permission sets in a single pass instead of one query per group
-- (N+1). Codes are ordered by code within each group (grouped by
-- permission_group_id in the ORDER BY, matching the per-group ordering).
SELECT pgp.permission_group_id, p.code
FROM permission_group_permissions pgp
JOIN permissions p ON p.id = pgp.permission_id
ORDER BY pgp.permission_group_id, p.code;

-- name: CreatePermissionGroup :one
-- Create a named permission group (is_base_role=false, AD-12). Custom groups
-- are never base roles.
INSERT INTO permission_groups (name, description, is_base_role)
VALUES ($1, $2, false)
RETURNING id, name, description, is_base_role;

-- name: UpdatePermissionGroup :one
-- Replace a permission group's name/description (its permission set is replaced
-- atomically via ReplaceGroupPermissions). Base roles are editable (they remain
-- the named matrix starting point, AD-12). A zero-row update (unknown id) maps
-- to the uniform not-found in the repository.
UPDATE permission_groups
SET name = $2, description = $3, updated_at = now()
WHERE id = $1
RETURNING id, name, description, is_base_role;

-- name: ListGroupPermissionIdsByCodes :many
-- Resolve permission codes → row ids for the given code set. The server accepts
-- only the 22 base codes, so every resolved id exists; a code with no row is
-- never matched and the caller rejects it as unknown (additive-only, FR-6).
SELECT id
FROM permissions
WHERE code = ANY($1::text[])
ORDER BY code;

-- name: InsertGroupPermissions :exec
-- Bulk insert the permission rows of a fresh group (Story 2.5). Additive: it
-- never stores a deny; ON CONFLICT DO NOTHING guards a duplicate row. An empty
-- code set inserts zero rows (a group may have an empty additive set).
INSERT INTO permission_group_permissions (permission_group_id, permission_id)
SELECT $1, p.id
FROM permissions p
WHERE p.id = ANY($2::uuid[])
ON CONFLICT DO NOTHING;

-- name: DeleteGroupPermissions :exec
-- Remove every permission row of a group (Story 2.5). Used by UpdateGroup
-- BEFORE InsertGroupPermissions, both in ONE transaction, so the group's set is
-- replaced atomically (delete-then-insert): a failed half-write rolls the whole
-- transaction back. NOTE: the delete and the re-insert MUST be separate
-- statements — a data-modifying CTE that deletes a row blocks a same-statement
-- re-insert of that row (PostgreSQL unique-index behaviour).
DELETE FROM permission_group_permissions
WHERE permission_group_id = $1;

-- name: ListAllPermissions :many
-- The server-authoritative permission catalog (Story 2.5): the full 22-code
-- base series with their labels, so the SPA editor's checkbox grid never drifts
-- from the seed. The German display label is derived in the core from the code;
-- the description is the raw DB label (English seed text) fallback.
SELECT code, description
FROM permissions
ORDER BY code;

-- name: PermissionGroupNameExists :one
-- Case-insensitive duplicate-name guard (Story 2.5): reports whether ANY
-- permission group already holds the given name, compared case-insensitively.
-- The schema's UNIQUE constraint is exact-match only, so this closes the
-- "Gerätewart" vs "gerätewart" duplicate window inside the create/update
-- transaction. A pre-existing exact match also trips the constraint (23505) as
-- a belt-and-suspenders fallback.
SELECT EXISTS (
    SELECT 1 FROM permission_groups
    WHERE lower(name) = lower($1)
);

-- name: PermissionGroupExists :one
-- Existence check for a permission group by id (Story 2.5). Run FIRST inside
-- UpdateGroup so an unknown id maps to the uniform 404 NOT-FOUND BEFORE the
-- duplicate-name check — an update of a nonexistent group must never answer
-- 409 "name taken" even when the requested name is held by another group.
SELECT EXISTS (
    SELECT 1 FROM permission_groups
    WHERE id = $1
);

-- name: PermissionGroupNameExistsExcept :one
-- Case-insensitive duplicate-name guard for UPDATE (Story 2.5): like
-- PermissionGroupNameExists but EXCLUDING the target group itself, so renaming
-- a group to its OWN name (or a case variant) stays legal while any other
-- holder of the name maps to the uniform 409 conflict.
SELECT EXISTS (
    SELECT 1 FROM permission_groups
    WHERE lower(name) = lower($1) AND id <> $2
);

-- ============================================================================
-- User & Group Administration (Story 2.6, AD-2/AD-6/FR-19/FR-21/FR-22, AD-12)
-- ============================================================================

-- name: ListUsers :many
-- Every user for the admin "Benutzer" list surface (Story 2.6): id, names,
-- email and state, ordered by last name then first name. An optional `status`
-- filter (active/pending_approval/deactivated) narrows the set; a NULL status
-- returns all users (Spec 2.9 status filter). No secret material (password
-- hash, tokens) is selected — the listing never exposes credentials (NFR-O1).
SELECT id, email, first_name, last_name, state
FROM users
WHERE (sqlc.narg('status')::text IS NULL OR state = sqlc.narg('status')::text)
ORDER BY last_name, first_name, email;

-- name: GetUserByID :one
-- A single user's profile for the admin detail surface (Story 2.6): the
-- editable profile fields plus state. The password hash is deliberately NOT
-- selected — no secret material in a detail response (NFR-O1). A zero-row
-- read (unknown id) maps to the uniform not-found in the repository.
SELECT id, email, first_name, last_name, display_name, state
FROM users
WHERE id = $1;

-- name: ListUserRoles :many
-- The permission groups (roles) a user holds, for the user detail surface
-- (Story 2.6, AD-12). No secret material is selected.
SELECT pg.id, pg.name, pg.is_base_role
FROM permission_groups pg
JOIN user_permission_groups upg ON upg.permission_group_id = pg.id
WHERE upg.user_id = $1
ORDER BY pg.name;

-- name: ListUserGroupMemberships :many
-- The organisational user groups (teams) a user belongs to, for the user
-- detail surface (Story 2.6, AD-12). Membership grants NO permission by
-- itself — the resolution query never joins user_groups (already true).
SELECT ug.id, ug.name
FROM user_groups ug
JOIN user_group_members ugm ON ugm.user_group_id = ug.id
WHERE ugm.user_id = $1
ORDER BY ug.name;

-- name: ListUserGroupNamesByUsers :many
-- The organisational user-group names each listed user belongs to (Effort 2):
-- one row per (user_id, group name), ordered by user id then group name, so
-- the admin "Benutzer" list can render inline group tags in a single query
-- instead of one lookup per row (no N+1). Membership grants NO permission
-- (AD-12); the resolution query never joins user_groups.
SELECT ugm.user_id, ug.name
FROM user_group_members ugm
JOIN user_groups ug ON ug.id = ugm.user_group_id
WHERE ugm.user_id = ANY($1::uuid[])
ORDER BY ugm.user_id, ug.name;

-- name: ListUserDirectGrants :many
-- The direct one-off permission grants a user holds (additive, AD-12), for the
-- user detail surface (Story 2.6). Codes ordered by code.
SELECT p.id, p.code, up.granted_at
FROM user_permissions up
JOIN permissions p ON p.id = up.permission_id
WHERE up.user_id = $1
ORDER BY p.code;

-- name: ListUserQualifications :many
-- The qualification assignments of a user (Story 2.6, AD-7/FR-22): the
-- vocabulary row and the PER-ASSIGNMENT expires_at (Spec 2.9, nullable — NULL
-- for an unlimited assignment, never expires). The core derives the
-- per-assignment display status (Gültig / Bald ablaufend / Abgelaufen /
-- Unbegrenzt) from the per-assignment expires_at. Ordered by qualification
-- name.
SELECT q.id, q.name, q.description, q.expiry_kind, uq.expires_at AS assigned_expires_at, uq.assigned_at
FROM user_qualifications uq
JOIN qualifications q ON q.id = uq.qualification_id
WHERE uq.user_id = $1
ORDER BY q.name;

-- name: CreateAdminUser :one
-- Create a user from the admin surface (Story 2.6): the submitted profile
-- fields plus an explicit initial state (active or pending_approval — the
-- form allows either; admin-created users are typically active with credentials
-- provisioned out-of-band, so the password hash is written as '' and set later).
-- The email UNIQUE constraint is the belt-and-suspenders backstop behind the
-- repository's case-insensitive pre-check.
INSERT INTO users (email, display_name, first_name, last_name, password_hash, state)
VALUES ($1, $2, $3, $4, '', $5)
RETURNING id, email, display_name, first_name, last_name, password_hash, state, is_mfa_enabled, totp_secret_encrypted, pending_totp_secret_encrypted, pending_totp_expires_at, attributes, created_at, updated_at, pending_email, must_change_password, one_time_password_hash, one_time_password_expires_at;

-- name: UpdateUserProfileAdmin :one
-- Replace an existing user's profile fields AND state from the admin surface
-- (Story 2.6). Email and state are touched here (unlike the self-service
-- UpdateUserProfile); the email UNIQUE constraint is the backstop behind the
-- repository's case-insensitive pre-check (existence-first, uniform 404 before
-- any 409). A zero-row update (unknown id) maps to the uniform not-found.
UPDATE users
SET email        = $2,
    display_name = $3,
    first_name   = $4,
    last_name    = $5,
    state        = $6,
    updated_at   = now()
WHERE id = $1
RETURNING id, email, display_name, first_name, last_name, password_hash, state, is_mfa_enabled, totp_secret_encrypted, pending_totp_secret_encrypted, pending_totp_expires_at, attributes, created_at, updated_at, pending_email, must_change_password, one_time_password_hash, one_time_password_expires_at;

-- name: UserEmailExists :one
-- Case-insensitive email-uniqueness guard (Story 2.6): reports whether ANY
-- user already holds the given email. The schema's UNIQUE constraint is
-- exact-match only, so this closes the "A@x.de" vs "a@X.de" duplicate window
-- inside the create transaction. A pre-existing exact match also trips the
-- constraint (23505) as a belt-and-suspenders fallback.
SELECT EXISTS (
    SELECT 1 FROM users
    WHERE lower(email) = lower($1)
);

-- name: UserEmailExistsExcept :one
-- Case-insensitive email-uniqueness guard for UPDATE (Story 2.6): like
-- UserEmailExists but EXCLUDING the target user itself, so keeping the user's
-- OWN email (or a case variant) stays legal while any other holder of the
-- address maps to the uniform 409 conflict.
SELECT EXISTS (
    SELECT 1 FROM users
    WHERE lower(email) = lower($1) AND id <> $2
);

-- name: ListUserGroups :many
-- Every organisational user group (Story 2.6, AD-12), ordered by name, for the
-- admin user-group management surface and the user editor's assignment grid.
SELECT id, name, description, created_at
FROM user_groups
ORDER BY name;

-- name: CreateUserGroup :one
-- Create an organisational user group (Story 2.6, AD-12). The name is unique
-- case-insensitively (a duplicate maps to ErrUserGroupNameTaken → 409).
INSERT INTO user_groups (name, description)
VALUES ($1, $2)
RETURNING id, name, description, created_at;

-- name: UserGroupNameExists :one
-- Case-insensitive duplicate-name guard for a user group (Story 2.6): the
-- schema's UNIQUE constraint is exact-match only, so this closes the
-- "gruppe ost" vs "Gruppe Ost" window inside the create transaction.
SELECT EXISTS (
    SELECT 1 FROM user_groups
    WHERE lower(name) = lower($1)
);

-- name: UserGroupExists :one
-- Existence check for an organisational user group by id (Story 2.6). Run
-- FIRST inside group-member assignment so an unknown group maps to the uniform
-- 404 before any member work.
SELECT EXISTS (
    SELECT 1 FROM user_groups
    WHERE id = $1
);

-- name: DeleteUserGroup :exec
-- Remove an organisational user group (Story 2.6). Member rows cascade
-- (ON DELETE CASCADE). Membership grants no permission (AD-12), so deleting a
-- team never changes anyone's access.
DELETE FROM user_groups
WHERE id = $1;

-- name: UserExists :one
-- Existence check for a user by id (Story 2.6). Run FIRST inside the admin
-- create/edit so an unknown id maps to the uniform 404 before the
-- duplicate-email check (an update of a nonexistent user must never answer 409
-- "email taken").
SELECT EXISTS (
    SELECT 1 FROM users
    WHERE id = $1
);

-- name: UsersExistByIDs :many
-- The user ids that exist among the given set (Story 2.6). Used to validate a
-- group-member assignment: every requested member must exist (the count
-- comparison catches an unknown id → uniform 400).
SELECT id
FROM users
WHERE id = ANY($1::uuid[])
ORDER BY id;

-- name: PermissionGroupsExistByIDs :many
-- The permission-group ids that exist among the given set (Story 2.6). Used to
-- validate a user edit's role set: every requested role must exist (count
-- comparison → uniform 400).
SELECT id
FROM permission_groups
WHERE id = ANY($1::uuid[])
ORDER BY id;

-- name: UserGroupsExistByIDs :many
-- The user-group ids that exist among the given set (Story 2.6). Used to
-- validate a user edit's user-group set: every requested group must exist
-- (count comparison → uniform 400).
SELECT id
FROM user_groups
WHERE id = ANY($1::uuid[])
ORDER BY id;

-- name: DeleteUserRoles :exec
-- Remove EVERY permission-group membership row of a user (Story 2.6). Used by
-- the admin edit BEFORE InsertUserRoles, both in ONE transaction, so the user's
-- role set is replaced atomically (delete-then-insert). NOTE: the delete and the
-- re-insert MUST be separate statements — a data-modifying CTE that deletes a
-- row blocks a same-statement re-insert of that row (PostgreSQL unique-index
-- behaviour, Story 2.5 lesson).
DELETE FROM user_permission_groups
WHERE user_id = $1;

-- name: InsertUserRoles :exec
-- Bulk insert a user's permission-group membership rows (Story 2.6). The
-- input is resolved permission-group ids (already validated to exist). An empty
-- set inserts zero rows (removing all roles is valid).
INSERT INTO user_permission_groups (user_id, permission_group_id)
SELECT $1, p.id
FROM permission_groups p
WHERE p.id = ANY($2::uuid[])
ON CONFLICT DO NOTHING;

-- name: DeleteUserGroupMemberships :exec
-- Remove EVERY organisational user-group membership row of a user (Story 2.6).
-- Used by the admin edit BEFORE InsertUserGroupMemberships, both in ONE
-- transaction (delete-then-insert, separate statements — Story 2.5 lesson).
DELETE FROM user_group_members
WHERE user_id = $1;

-- name: InsertUserGroupMemberships :exec
-- Bulk insert a user's organisational user-group memberships (Story 2.6). The
-- input is resolved user-group ids (already validated to exist). An empty set
-- inserts zero rows.
INSERT INTO user_group_members (user_id, user_group_id)
SELECT $1, ug.id
FROM user_groups ug
WHERE ug.id = ANY($2::uuid[])
ON CONFLICT DO NOTHING;

-- name: DeleteUserDirectGrants :exec
-- Remove EVERY direct permission-grant row of a user (Story 2.6). Used by the
-- admin edit BEFORE InsertUserDirectGrants, both in ONE transaction
-- (delete-then-insert, separate statements — Story 2.5 lesson).
DELETE FROM user_permissions
WHERE user_id = $1;

-- name: InsertUserDirectGrants :exec
-- Bulk insert a user's direct permission grants (Story 2.6, additive AD-12).
-- The input is resolved permission ids (the codes are validated against the 22
-- base codes by the core/repository). An empty set inserts zero rows.
INSERT INTO user_permissions (user_id, permission_id)
SELECT $1, p.id
FROM permissions p
WHERE p.id = ANY($2::uuid[])
ON CONFLICT DO NOTHING;

-- name: DeleteGroupMembers :exec
-- Remove EVERY member row of an organisational user group (Story 2.6). Used by
-- the group-member assignment BEFORE InsertGroupMembers, both in ONE transaction
-- (delete-then-insert, separate statements — Story 2.5 lesson).
DELETE FROM user_group_members
WHERE user_group_id = $1;

-- name: ListUserGroupMemberIDs :many
-- The user ids that currently belong to an organisational user group (Story
-- 2.6), ordered by id. Drives the group-member editor's pre-checked set; the
-- assignment endpoint replaces this set atomically.
SELECT user_id
FROM user_group_members
WHERE user_group_id = $1
ORDER BY user_id;

-- name: InsertGroupMembers :exec
-- Bulk insert the member rows of an organisational user group (Story 2.6). The
-- input is resolved user ids (already validated to exist via UsersExistByIDs).
-- An empty set empties the group (removing every member is valid).
INSERT INTO user_group_members (user_group_id, user_id)
SELECT $1, u.id
FROM users u
WHERE u.id = ANY($2::uuid[])
ON CONFLICT DO NOTHING;

-- name: ListUserGroupRoles :many
-- The permission groups (roles) an organisational user group grants its members
-- (Spec 2.9, AD-12): id + name + is_base_role, ordered by name. A member of the
-- team inherits every role here via the resolution query.
SELECT pg.id, pg.name, pg.is_base_role
FROM permission_groups pg
JOIN user_group_permission_groups ugpg ON ugpg.permission_group_id = pg.id
WHERE ugpg.user_group_id = $1
ORDER BY pg.name;

-- name: DeleteUserGroupRoles :exec
-- Remove EVERY role row of an organisational user group (Spec 2.9). Used by the
-- group-role assignment BEFORE InsertUserGroupRoles, both in ONE transaction
-- (delete-then-insert, separate statements — Story 2.5 lesson, never a
-- data-modifying CTE).
DELETE FROM user_group_permission_groups
WHERE user_group_id = $1;

-- name: InsertUserGroupRoles :exec
-- Bulk insert the role rows of an organisational user group (Spec 2.9). The
-- input is resolved permission-group ids (already validated to exist). An empty
-- set removes every role (revoking inherited access from all members on the
-- next request).
INSERT INTO user_group_permission_groups (user_group_id, permission_group_id)
SELECT $1, p.id
FROM permission_groups p
WHERE p.id = ANY($2::uuid[])
ON CONFLICT DO NOTHING;

-- name: ListGroupRolesByUser :many
-- The roles a user inherits VIA organisational user-groups (Spec 2.9,
-- provenance): the user-group id + name and the role id + name for every
-- role a team the user belongs to grants. Used to annotate the resolved
-- permission set on the user detail with its source(s).
SELECT ug.id AS user_group_id, ug.name AS user_group_name, pg.id AS role_id, pg.name AS role_name
FROM user_group_members ugm
JOIN user_groups ug ON ug.id = ugm.user_group_id
JOIN user_group_permission_groups ugpg ON ugpg.user_group_id = ug.id
JOIN permission_groups pg ON pg.id = ugpg.permission_group_id
WHERE ugm.user_id = $1
ORDER BY ug.name, pg.name;

-- name: ListResolvedPermissionSources :many
-- Every resolved permission code of a user ANNOTATED with its source (Spec 2.9
-- provenance): source_kind is 'role' (individual permission-group membership),
-- 'group' (inherited via an organisational user-group), or 'direct' (direct
-- one-off grant); source_name is the role name, the user-group name, or
-- 'direct' respectively. A code may appear multiple times (multi-source); the
-- core groups by code to list all sources. Powers Effort 2's "Alle
-- Berechtigungen" view; computed server-side here (Effort 1).
SELECT p.code, 'role' AS source_kind, pg.name AS source_name
FROM user_permission_groups upg
JOIN permission_groups pg ON pg.id = upg.permission_group_id
JOIN permission_group_permissions pgp ON pgp.permission_group_id = pg.id
JOIN permissions p ON p.id = pgp.permission_id
WHERE upg.user_id = $1
UNION ALL
SELECT p.code, 'group' AS source_kind, ug.name AS source_name
FROM user_group_members ugm
JOIN user_groups ug ON ug.id = ugm.user_group_id
JOIN user_group_permission_groups ugpg ON ugpg.user_group_id = ug.id
JOIN permission_group_permissions pgp ON pgp.permission_group_id = ugpg.permission_group_id
JOIN permissions p ON p.id = pgp.permission_id
WHERE ugm.user_id = $1
UNION ALL
SELECT p.code, 'direct', 'direct'
FROM user_permissions up
JOIN permissions p ON p.id = up.permission_id
WHERE up.user_id = $1
ORDER BY 1;

-- name: ListQualifications :many
-- The full qualification vocabulary (Story 2.6, AD-7/FR-22): every
-- qualification with its expiry model, ordered by name. A qualification itself
-- has NO valid-until date (2026-09-08 rework) — only its expiry_kind
-- (unlimited/fixed); a per-user valid-until exists solely on assignments
-- (user_qualifications.expires_at, Spec 2.9). The qualification management
-- surface (Story 2.7) edits these; Story 2.6 only needs the list for the
-- user-detail qualification view (via ListUserQualifications).
SELECT id, name, description, expiry_kind
FROM qualifications
ORDER BY name;

-- name: AddQualificationToUser :exec
-- Assign a qualification to a user (Story 2.6 persistence seam; the assignment
-- EDITING surface ships with Story 2.7). A `fixed` qualification REQUIRES a
-- per-assignment expires_at (Spec 2.9 human decision A); an `unlimited` one
-- stores NULL. Re-assigning an already-assigned qualification UPDATES the
-- per-assignment expires_at (review finding 2.9): a duplicate assignment with a
-- new valid-until renews/overrides the stored one instead of silently no-oping.
INSERT INTO user_qualifications (user_id, qualification_id, expires_at)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, qualification_id)
DO UPDATE SET expires_at = EXCLUDED.expires_at;

-- name: UpdateUserQualificationExpiry :execrows
-- Update a user's per-assignment valid-until (Spec 2.9): a NULL clears the
-- override (reverting the status to the vocabulary expiry); a value overrides
-- it. A zero-row update (unknown user/qualification pair) reports 0 rows the
-- caller maps to the uniform not-found.
UPDATE user_qualifications
SET expires_at = $3
WHERE user_id = $1 AND qualification_id = $2;

-- name: RemoveQualificationFromUser :execrows
-- Revoke a qualification from a user (Story 2.6 persistence seam; the
-- assignment EDITING surface ships with Story 2.7). Revocation is immediate
-- (FR-22/AD-2) because qualification resolution is live per request. A
-- zero-row delete (unassigned pair) reports 0 rows the caller maps to the
-- uniform not-found — consistent with UpdateUserQualificationExpiry.
DELETE FROM user_qualifications
WHERE user_id = $1 AND qualification_id = $2;

-- name: GetQualificationExpiryKindByID :one
-- The expiry model of ONE qualification by id (review finding 2.9): a targeted
-- lookup so the assignment path does not scan the whole vocabulary. Zero rows
-- (unknown id) map to the uniform not-found.
SELECT id, expiry_kind
FROM qualifications
WHERE id = $1;

-- ============================================================================
-- Qualification Management (Story 2.7, AD-6/FR-19/FR-22/AD-7)
-- ============================================================================

-- name: CreateQualification :one
-- Create a qualification vocabulary row (Story 2.7, AD-7/FR-22). The name is
-- unique case-insensitively (the repository pre-checks QualificationNameExists
-- and the schema UNIQUE constraint is the belt-and-suspenders backstop).
-- `unlimited` qualifications never expire; `fixed` ones carry no vocabulary
-- date — a per-user valid-until is required at assignment (Spec 2.9).
INSERT INTO qualifications (name, description, expiry_kind)
VALUES ($1, $2, $3)
RETURNING id, name, description, expiry_kind;

-- name: UpdateQualification :one
-- Replace a qualification's name/description/expiry_kind atomically (Story
-- 2.7). Editing the expiry model never rewrites existing assignments — each
-- assignment keeps its per-user expires_at (Spec 2.9). A zero-row update
-- (unknown id) maps to the uniform not-found in the repository.
UPDATE qualifications
SET name = $2, description = $3, expiry_kind = $4, updated_at = now()
WHERE id = $1
RETURNING id, name, description, expiry_kind;

-- name: QualificationNameExists :one
-- Case-insensitive duplicate-name guard for a qualification (Story 2.7): the
-- schema's UNIQUE constraint is exact-match only, so this closes the
-- "erste hilfe" vs "Erste Hilfe" duplicate window inside the create path.
SELECT EXISTS (
    SELECT 1 FROM qualifications
    WHERE lower(name) = lower($1)
);

-- name: QualificationNameExistsExcept :one
-- Case-insensitive duplicate-name guard for UPDATE (Story 2.7): like
-- QualificationNameExists but EXCLUDING the target qualification itself, so
-- renaming a qualification to its OWN name (or a case variant) stays legal
-- while any other holder of the name maps to the uniform 409 conflict.
SELECT EXISTS (
    SELECT 1 FROM qualifications
    WHERE lower(name) = lower($1) AND id <> $2
);

-- name: QualificationExists :one
-- Existence check for a qualification by id (Story 2.7). Run FIRST inside
-- update/assign so an unknown id maps to the uniform 404 before the
-- duplicate-name / assignee work — an update of a nonexistent qualification
-- must never answer 409 "name taken".
SELECT EXISTS (
    SELECT 1 FROM qualifications
    WHERE id = $1
);

-- name: ListQualificationAssignees :many
-- The users currently assigned a qualification (Story 2.7, AD-7/FR-22): the
-- assignee id plus the display name for the assignment editor's checkbox
-- list, ordered by name. No secret material is selected.
SELECT u.id, u.display_name
FROM user_qualifications uq
JOIN users u ON u.id = uq.user_id
WHERE uq.qualification_id = $1
ORDER BY u.last_name, u.first_name, u.display_name;

-- name: DeleteQualificationAssignees :exec
-- Remove EVERY assignment row of a qualification (Story 2.7). Used by the
-- assignee replacement BEFORE InsertQualificationAssignees, both in ONE
-- transaction (delete-then-insert, separate statements — Story 2.5 lesson).
DELETE FROM user_qualifications
WHERE qualification_id = $1;

-- name: InsertQualificationAssignees :exec
-- Bulk insert the assignment rows of a qualification (Story 2.7). The input is
-- resolved user ids (already validated to exist). An empty set removes every
-- assignee (revoking eligibility immediately, AD-7/FR-22).
INSERT INTO user_qualifications (qualification_id, user_id)
SELECT $1, u.id
FROM users u
WHERE u.id = ANY($2::uuid[])
ON CONFLICT DO NOTHING;
