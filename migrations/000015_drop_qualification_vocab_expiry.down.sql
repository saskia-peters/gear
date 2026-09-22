-- Reverse of 000015_drop_qualification_vocab_expiry.up.sql: restore the
-- vocabulary-level valid-until column on `qualifications`. Existing rows are
-- backfilled as NULL (no date) — the forward migration only dropped the column
-- and never wrote dates, so a clean reverse is a plain re-add.

ALTER TABLE qualifications
    ADD COLUMN expires_at timestamptz NULL;