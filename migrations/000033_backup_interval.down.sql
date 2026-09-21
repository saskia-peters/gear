-- Reverse of 000033_backup_interval.up.sql: remove ONLY the row this migration
-- added (the backup job then falls back to the 24h default interval). The
-- app_settings store itself (000026) and all other rows are untouched.

DELETE FROM app_settings WHERE key = 'backup_interval';