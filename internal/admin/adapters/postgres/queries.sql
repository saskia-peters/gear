-- Admin module store (AD-1/AD-11), generated into package postgres by sqlc.
-- Story 3.1 ships the SMTP-settings queries for the Admin-owned
-- `smtp_settings` single-row table (FR-28/AD-14); Story 3.2 adds the
-- `backup_destinations` multi-row table (FR-29/AD-15); Story 4.1 adds the
-- `schedules` named schedule catalog (FR-30/AD-16).

-- name: GetSmtpSettings :many
-- The single SMTP-settings row (the partial unique index guarantees at most
-- one). Zero rows = "not configured yet" → the consumer returns zero defaults.
-- The encrypted password column is intentionally selected: decryption happens
-- only in the sender/test-send path, in memory (NFR-S4) — it is never
-- serialized by the HTTP surface.
SELECT id, host, port, security, sender_address, sender_name, username, password_encrypted, created_at, updated_at
FROM smtp_settings
ORDER BY created_at ASC
LIMIT 1;

-- name: InsertSmtpSettings :exec
-- First-time write of the settings row. ON CONFLICT DO NOTHING absorbs the
-- lost race of two concurrent first writes (the single-row partial index lets
-- only one INSERT land; the repository re-applies its values via a follow-up
-- UPDATE so no write is silently dropped). An empty password stores the empty
-- default (no-auth relay); keep-existing is a no-op on the very first write.
INSERT INTO smtp_settings (host, port, security, sender_address, sender_name, username, password_encrypted)
VALUES ($1, $2, $3, $4, $5, $6, COALESCE(NULLIF(sqlc.arg('password_encrypted')::text, ''), ''))
ON CONFLICT DO NOTHING;

-- name: UpdateSmtpSettings :execrows
-- Replace the single settings row's values (zero rows when none exists yet —
-- the repository inserts in that case). The password is COALESCED: an empty
-- value KEEPS the existing ciphertext, a non-empty one replaces it — both in
-- the SAME statement (the RHS reads the pre-update row value atomically), so
-- keep-existing is atomic (no read-then-write race a concurrent update could
-- exploit).
UPDATE smtp_settings
SET host = $1,
    port = $2,
    security = $3,
    sender_address = $4,
    sender_name = $5,
    username = $6,
    password_encrypted = COALESCE(NULLIF(sqlc.arg('password_encrypted')::text, ''), password_encrypted),
    updated_at = now()
WHERE id = (SELECT id FROM smtp_settings ORDER BY created_at ASC LIMIT 1);

-- name: ListBackupDestinations :many
-- The full multi-row backup-destination list (FR-29/AD-15), oldest first. The
-- encrypted credential column is intentionally selected: the consumer seam
-- (and the test-connection path) decrypt it in memory (NFR-S4) — the HTTP
-- surface never serializes it.
SELECT id, name, mechanism, endpoint, bucket_or_path, username, password_encrypted, schedule, created_at, updated_at
FROM backup_destinations
ORDER BY created_at ASC;

-- name: GetBackupDestination :one
-- A single destination by id (for update/test/delete). Zero rows = not found.
SELECT id, name, mechanism, endpoint, bucket_or_path, username, password_encrypted, schedule, created_at, updated_at
FROM backup_destinations
WHERE id = $1;

-- name: CreateBackupDestination :one
-- Insert a destination and return the resulting row. An empty schedule is
-- stored as NULL (the optional scheduling hint); the credential is stored as
-- the already-encrypted ciphertext the core produced.
INSERT INTO backup_destinations (name, mechanism, endpoint, bucket_or_path, username, password_encrypted, schedule)
VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7::text, ''))
RETURNING id, name, mechanism, endpoint, bucket_or_path, username, password_encrypted, schedule, created_at, updated_at;

-- name: UpdateBackupDestination :one
-- Replace one destination's values (zero rows when the id does not exist).
-- The credential is COALESCED: an empty value KEEPS the existing ciphertext, a
-- non-empty one replaces it — both in the SAME statement (the RHS reads the
-- pre-update row value atomically), so keep-existing is atomic (no
-- read-then-write race a concurrent update could exploit). clear_credential:
-- true explicitly WIPES the stored credential (revoke — a blank password alone
-- means keep-existing, so clearing must be explicit, finding). updated_at is
-- refreshed on every edit. An empty schedule clears the stored hint (NULL).
UPDATE backup_destinations
SET name = $2,
    mechanism = $3,
    endpoint = $4,
    bucket_or_path = $5,
    username = $6,
    password_encrypted = CASE
        WHEN sqlc.arg('clear_credential')::boolean THEN ''
        ELSE COALESCE(NULLIF(sqlc.arg('password_encrypted')::text, ''), password_encrypted)
    END,
    schedule = NULLIF($7::text, ''),
    updated_at = now()
WHERE id = $1
RETURNING id, name, mechanism, endpoint, bucket_or_path, username, password_encrypted, schedule, created_at, updated_at;

-- name: DeleteBackupDestination :execrows
-- Remove one destination. Zero rows = the id did not exist.
DELETE FROM backup_destinations WHERE id = $1;

-- name: ListSchedules :many
-- The ACTIVE named-schedule catalog (FR-30/AD-16). Archived rows (archived_at
-- NOT NULL) are filtered out — the active surface never shows them. The order
-- is deterministic: created_at ASC with a name tiebreaker, so the seed rows
-- (which share one now() created_at) always render in a stable order. The
-- reserved weekday_set/time_of_day columns are selected so the returned rows
-- carry the full stored row (they are NULL in V1).
SELECT id, name, interval_unit, interval_magnitude, weekday_set, time_of_day, archived_at, created_at, updated_at
FROM schedules
WHERE archived_at IS NULL
ORDER BY created_at ASC, name ASC;

-- name: CreateSchedule :one
-- Insert a schedule and return the resulting row. The reserved weekday_set /
-- time_of_day columns stay NULL (stored-but-ignored in V1, AD-16).
INSERT INTO schedules (name, interval_unit, interval_magnitude)
VALUES ($1, $2, $3)
RETURNING id, name, interval_unit, interval_magnitude, weekday_set, time_of_day, archived_at, created_at, updated_at;

-- name: UpdateSchedule :one
-- Replace one ACTIVE schedule's name/interval and refresh updated_at. The
-- `AND archived_at IS NULL` guard makes an update against an already-archived
-- row affect zero rows → ErrScheduleNotFound (soft archive is irreversible in
-- V1; the archived row is non-existent to the surface). The reserved
-- weekday/time columns are untouched (NULL in V1).
UPDATE schedules
SET name = $2,
    interval_unit = $3,
    interval_magnitude = $4,
    updated_at = now()
WHERE id = $1 AND archived_at IS NULL
RETURNING id, name, interval_unit, interval_magnitude, weekday_set, time_of_day, archived_at, created_at, updated_at;

-- name: ArchiveSchedule :one
-- Soft-archive one schedule: archived_at = now() (never a hard delete — FK
-- references keep history intact, AD-16). The `AND archived_at IS NULL` guard
-- makes archiving an already-archived row affect zero rows →
-- ErrScheduleNotFound (ARCHIVE_ARCHIVED answers the 404 sentinel).
UPDATE schedules
SET archived_at = now(),
    updated_at = now()
WHERE id = $1 AND archived_at IS NULL
RETURNING id, name, interval_unit, interval_magnitude, weekday_set, time_of_day, archived_at, created_at, updated_at;