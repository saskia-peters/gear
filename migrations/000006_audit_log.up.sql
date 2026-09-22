-- G.E.A.R. User-owned append-only audit trail (Story 1.7, NFR-O1/NFR-O2,
-- architecture spine table 11).
--
-- The User module owns the `audit_log` table (AD-2/AD-11). It records a
-- minimal compliance trail of security-relevant user actions: who acted
-- (actor_user_id), which operation ("password.change", and later "password
-- reset", admin operations), and when. It is append-only by construction: the
-- data model exposes no update/delete path for rows and consumers treat it as
-- immutable. No password values or other sensitive payloads are ever recorded
-- here — only actor, operation type and timestamp (NFR-O1).
--
-- The actor reference is intentionally `ON DELETE SET NULL` (not CASCADE):
-- deleting a user must NOT delete the compliance trail (that would silently
-- destroy audit evidence). Instead the actor column is anonymized to NULL on
-- user deletion while the row — and thus the historical record of the action —
-- survives (NFR-O1/NFR-O2).
--
-- InsertAuditEvent is best-effort (NFR-O1): a failed audit write must not roll
-- back the security action that triggered it (availability), but the failure
-- is logged server-side.

CREATE TABLE audit_log (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    actor_user_id  uuid NULL REFERENCES users(id) ON DELETE SET NULL,
    operation      text NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_actor_user_id_idx ON audit_log (actor_user_id);
