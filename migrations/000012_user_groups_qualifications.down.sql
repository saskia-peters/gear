-- Roll back Story 2.6: drop the four User-owned spine tables in reverse
-- dependency order (memberships and assignments reference the vocabulary/
-- team tables).
DROP TABLE IF EXISTS user_qualifications;
DROP TABLE IF EXISTS qualifications;
DROP TABLE IF EXISTS user_group_members;
DROP TABLE IF EXISTS user_groups;