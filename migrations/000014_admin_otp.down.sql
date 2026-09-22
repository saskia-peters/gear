-- Reverse of 000014_admin_otp.up.sql: drop the two one-time-password columns
-- from users. Existing OTP state is discarded (an OTP is a short-lived
-- recovery credential, so losing it on rollback is acceptable).
ALTER TABLE users
    DROP COLUMN IF EXISTS one_time_password_hash,
    DROP COLUMN IF EXISTS one_time_password_expires_at;