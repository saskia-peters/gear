ALTER TABLE users
    ADD COLUMN first_name text NOT NULL DEFAULT '',
    ADD COLUMN last_name text NOT NULL DEFAULT '',
    ADD COLUMN password_hash text NOT NULL DEFAULT '';
