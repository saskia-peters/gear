-- Reverse of 000018_backup_destinations.up.sql: drop the Admin-owned multi-row
-- backup destinations table. No other object is touched.
DROP TABLE IF EXISTS backup_destinations;