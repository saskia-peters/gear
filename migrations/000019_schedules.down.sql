-- Reverse of 000019_schedules.up.sql: drop the Admin-owned named schedule
-- catalog table (the seeded rows drop with it). No other object is touched.
DROP TABLE IF EXISTS schedules;