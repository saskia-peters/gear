-- Reverse of 000007_user_profile.up.sql: remove the staged-email column and
-- its uniqueness constraint.
ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_pending_email_key,
    DROP COLUMN IF EXISTS pending_email;