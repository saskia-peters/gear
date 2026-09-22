-- Roll back Story 1.6: drop the encrypted TOTP secret column.
ALTER TABLE users
    DROP COLUMN IF EXISTS totp_secret_encrypted;
