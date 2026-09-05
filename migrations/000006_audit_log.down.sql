-- Roll back Story 1.7: drop the User-owned append-only audit trail.
DROP TABLE IF EXISTS audit_log;
