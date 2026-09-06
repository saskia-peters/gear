-- Reverse of 000008_password_reset.up.sql: drop the reset-token store and the
-- mandatory-change flag.
ALTER TABLE users
    DROP COLUMN IF EXISTS must_change_password;

DROP TABLE IF EXISTS password_reset_tokens;