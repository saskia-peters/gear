-- Reverts 000030: restores the three-state CHECK and drops the archive table.
--
-- LIMITATION (mirrors the repo's documented down-migration edge-case
-- convention): the CHECK re-add FAILS while a `deleted` tombstone exists (the
-- constraint admits only pending_approval/active/deactivated). A clean rollback
-- therefore requires that NO account was deleted since 000030 was applied —
-- i.e. `SELECT count(*) FROM users WHERE state = 'deleted'` must be 0. A
-- mid-life rollback with deleted rows must first purge/clean the tombstones
-- (or re-apply 000030) — the CHECK is the guard that makes a half-deleted
-- state impossible.
ALTER TABLE users DROP CONSTRAINT users_state_check;
ALTER TABLE users ADD CONSTRAINT users_state_check
    CHECK (state IN ('pending_approval', 'active', 'deactivated'));

DROP INDEX IF EXISTS dsgvo_deleted_accounts_deleted_at_idx;

DROP TABLE IF EXISTS dsgvo_deleted_accounts;
