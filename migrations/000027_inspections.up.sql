-- G.E.A.R. Tool-owned inspection records + out-of-service reinstatements (Story
-- 5.3, FR-12/FR-13/FR-14/FR-15/AD-4/AD-5/AD-9/AD-11).
--
-- The Tool Maintenance hexagon (internal/tools) owns these tables — the
-- inspection write path runs exclusively through the Tool module's exported
-- configuration port (AD-10), never ad-hoc SQL over the same rows.
--
-- Status is DERIVED on read (AD-4): no OOS flag, no status column is ever
-- stored. The derivation reads the LATEST inspection's overall_result +
-- submitted_at, the latest PASS inspection's submitted_at and the latest
-- reinstatement's created_at — the shared clock function (AD-5) consumes them
-- and the follow-up stories (5.4/5.5 write, 5.6 reinstatement write, 6.1
-- dashboard render) feed the same seam.
--
-- SNAPSHOT DESIGN (AD-8/3.4): `inspector_id` and `inspection_items.item_id`
-- are PLAIN uuids with NO FK — the inspection history stays self-contained even
-- when the User module rewrites inspector references to "Deleted User" (Story
-- 3.4 DSGVO rewrite, deferred) and when a tool type's checklist later changes
-- (the item LABEL is snapshotted at submit so old history never loses its
-- meaning). `position` is the explicit order persisted from the type's
-- checklist at submit time (FR-23 ordering spine).
CREATE TABLE inspections (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    tool_id        uuid NOT NULL REFERENCES tools(id),
    inspector_id   uuid NOT NULL,
    mode           text NOT NULL
                   CHECK (mode IN ('pass_fail', 'checklist')),
    overall_result text NOT NULL
                   CHECK (overall_result IN ('pass', 'fail')),
    notes          text,
    submitted_at   timestamptz NOT NULL DEFAULT now()
);

-- Per-tool inspection history read (reverse-chronological, FR-18): the status
-- derivation reads the LATEST row; the history surface (follow-up) reads all.
CREATE INDEX inspections_tool_id_submitted_at_idx
    ON inspections (tool_id, submitted_at DESC);

CREATE TABLE inspection_items (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    inspection_id uuid NOT NULL REFERENCES inspections(id) ON DELETE CASCADE,
    item_id       uuid NOT NULL,
    label         text NOT NULL,
    position      integer NOT NULL,
    result        text NOT NULL
                  CHECK (result IN ('pass', 'fail'))
);

-- Per-inspection ordered item read (the snapshot list, by position).
CREATE INDEX inspection_items_inspection_id_idx
    ON inspection_items (inspection_id);

-- Reinstatement ledger (FR-15/AD-9, Story 5.6 owns the WRITE path — this story
-- only ships the table + the derivation's consult): a Fuehrung/Admin-approved
-- exit from OOS with a mandatory non-empty reason; the derivation resets the
-- inspection clock to the LATEST reinstatement's created_at.
CREATE TABLE reinstatements (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    tool_id    uuid NOT NULL REFERENCES tools(id),
    actor_id   uuid NOT NULL,
    reason     text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Per-tool reinstatement read (latest first, the clock anchor).
CREATE INDEX reinstatements_tool_id_created_at_idx
    ON reinstatements (tool_id, created_at DESC);