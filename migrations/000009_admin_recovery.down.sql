-- Reverse of 000009_admin_recovery.up.sql: drop the admin-recovery and audit
-- columns added for Story 1.10.
DROP INDEX IF EXISTS password_reset_tokens_recovery_idx;

ALTER TABLE audit_log
    DROP COLUMN IF EXISTS severity,
    DROP COLUMN IF EXISTS operation_detail;

ALTER TABLE password_reset_tokens
    DROP COLUMN IF EXISTS requested_by_user_id,
    DROP COLUMN IF EXISTS approved_by_user_id,
    DROP COLUMN IF EXISTS recovery_target_admin;
