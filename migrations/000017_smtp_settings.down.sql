-- Reverse of 000017_smtp_settings.up.sql: drop the Admin-owned single-row
-- SMTP settings table and its single-row guard. No other object is touched.
DROP INDEX IF EXISTS smtp_settings_single_row_idx;

DROP TABLE IF EXISTS smtp_settings;