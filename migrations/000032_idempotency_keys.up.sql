-- G.E.A.R. Idempotency keys for the two append-only write paths (Story 7.5,
-- NFR-R1 / FR-12 / FR-15): the inspection submit and the reinstatement are
-- at-most-once via a CLIENT-SUPPLIED `idempotency_key` — a UUID the SPA
-- generates per submit intent and reuses across retries. A retried request
-- (timeout/500 where the server committed, or a double-click) therefore
-- replays the already-persisted record instead of inserting a duplicate row.
--
-- The key is REQUIRED (NOT NULL) and UNIQUE per tool: `(tool_id,
-- idempotency_key)`. Scoped per tool, not globally, so the same key value is
-- legal across two different tools (a key only needs to be unique within a
-- tool's append stream). It is NEVER generated server-side — a missing key
-- must fail the request, otherwise the guard is silently bypassable.
--
-- Backfill-then-NOT-NULL follows the 000025 precedent: add the nullable
-- column, backfill every existing row with a one-off `uuidv7()` (PG18
-- built-in), THEN set NOT NULL and create the unique index. The pre-existing
-- dev rows' keys are one-off values — they never need to be replayed.

-- 1. The nullable column first (NOT NULL only after the backfill fills it).
ALTER TABLE inspections ADD COLUMN idempotency_key uuid;

-- 2. Backfill existing rows with a one-off uuidv7() so NOT NULL is satisfiable
--    before the constraint lands.
UPDATE inspections SET idempotency_key = uuidv7();

-- 3. NOT NULL + the per-tool UNIQUE index (the at-most-once backstop the
--    repository's ON CONFLICT ... DO NOTHING races against).
ALTER TABLE inspections ALTER COLUMN idempotency_key SET NOT NULL;
CREATE UNIQUE INDEX inspections_tool_id_idempotency_key_idx
    ON inspections (tool_id, idempotency_key);

-- The reinstatement ledger gets the same treatment.
ALTER TABLE reinstatements ADD COLUMN idempotency_key uuid;

UPDATE reinstatements SET idempotency_key = uuidv7();

ALTER TABLE reinstatements ALTER COLUMN idempotency_key SET NOT NULL;
CREATE UNIQUE INDEX reinstatements_tool_id_idempotency_key_idx
    ON reinstatements (tool_id, idempotency_key);