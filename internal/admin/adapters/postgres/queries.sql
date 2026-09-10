-- Admin module store (AD-1/AD-11), generated into package postgres by sqlc.
-- Story 3.1 ships the SMTP-settings queries for the Admin-owned
-- `smtp_settings` single-row table (FR-28/AD-14); later Epic 3 stories extend
-- this file (backup_destinations, schedules) and re-run `just sqlc-generate`.

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