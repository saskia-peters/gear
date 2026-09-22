-- G.E.A.R. profile base-data editing (Story 2.1): staged email change.
--
-- The User module owns the `users` table (AD-2/AD-11). This migration adds a
-- single nullable column that holds a STAGED email address awaiting admin
-- approval (the Epic 2 admin workflow). Until approval the account stays
-- active on the current `email`, which remains the login identifier.
--
-- `pending_email` is UNIQUE so two users can never stage the same address.
-- The existing `email` UNIQUE constraint still governs the real login address:
-- a staged value equal to an existing `email` is rejected by `users_email_key`,
-- and a value equal to another user's `pending_email` by this constraint.
-- NULL (no staged change) is allowed multiple times.

ALTER TABLE users
    ADD COLUMN pending_email text NULL,
    ADD CONSTRAINT users_pending_email_key UNIQUE (pending_email);