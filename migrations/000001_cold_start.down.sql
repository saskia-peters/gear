-- Reverse of 000001_cold_start.up.sql: drop the User-owned identity tables
-- created for the cold-start schema. Join tables are dropped first (FKs).

DROP TABLE IF EXISTS user_permissions;
DROP TABLE IF EXISTS user_permission_groups;
DROP TABLE IF EXISTS permission_group_permissions;
DROP TABLE IF EXISTS permission_groups;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS users;
