-- G.E.A.R. dual-admin credential recovery (Story 1.10, FR-27/AD-13).
--
-- Extends the existing `password_reset_tokens` store (spine table 19) with
-- admin-recovery semantics. Ordinary FR-26 forgot tokens and admin-recovery
-- tokens share the same table (single-use, hashed at rest, 30-min expiry,
-- invalidated on use), but an admin-recovery token is:
--
--   * marked `recovery_target_admin = true` so it is DISTINCT from a FR-26
--     forgot token and only consumable through the recovery completion path;
--   * stamped with `approved_by_user_id` — the OTHER admin (B) who approved
--     the request — before it becomes usable. An admin-recovery token that is
--     not yet approved can never be completed;
--   * stamped with `requested_by_user_id` — the requesting admin (A) — so the
--     approving admin (B) can never approve the request they themselves
--     created (dual-control bypass fix, review finding): the requester may not
--     approve their own request.
--
-- The approver and requester references are `ON DELETE SET NULL` (not
-- CASCADE): deleting an admin must not destroy the recovery record/token (it
-- simply loses the link), mirroring the audit trail's immutable-evidence
-- stance.
ALTER TABLE password_reset_tokens
    ADD COLUMN recovery_target_admin boolean NOT NULL DEFAULT false,
    ADD COLUMN approved_by_user_id   uuid NULL REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN requested_by_user_id  uuid NULL REFERENCES users(id) ON DELETE SET NULL;

-- Index the recovery marker so listing/approving pending admin-recovery
-- requests stays cheap (the default `false` covers the large FR-26 token
-- population without an index).
CREATE INDEX password_reset_tokens_recovery_idx
    ON password_reset_tokens (recovery_target_admin, approved_by_user_id);

-- Extend the append-only audit trail (spine table 11) so security-relevant
-- events can carry an operation detail (e.g. the recovery Begründung/reason and
-- target email) and a severity (NFR-O1/NFR-O2). Recovery events are written
-- with severity='high'. The columns are NOT NULL with safe defaults so existing
-- audit rows remain valid and ordinary events default to 'normal'.
ALTER TABLE audit_log
    ADD COLUMN operation_detail text NULL,
    ADD COLUMN severity        text NOT NULL DEFAULT 'normal';
