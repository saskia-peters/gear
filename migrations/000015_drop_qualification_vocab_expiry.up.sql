-- G.E.A.R. Qualification vocabulary rework (2026-09-08): a qualification may be
-- valid forever — that is now the ONLY expiry control on the qualification
-- itself (the `expiry_kind` column; 'unlimited' vs 'fixed'). There is NO
-- valid-until date on a qualification anymore; a valid-until exists only on a
-- QUALIFICATION WHEN ASSIGNED TO A USER (`user_qualifications.expires_at`,
-- Spec 2.9). This migration drops the now-unused vocabulary-level column.
--
-- Existing per-user assignments are unaffected: `user_qualifications.expires_at`
-- already carries the per-assignment valid-until (NULL = unlimited assignment,
-- never expires).

ALTER TABLE qualifications
    DROP COLUMN expires_at;