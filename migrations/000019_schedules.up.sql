-- G.E.A.R. Admin-owned schedule catalog (Story 4.1, FR-30/AD-16).
--
-- The generic NAMED schedule catalog (e.g. inspection intervals) edited from
-- the Admin panel by holders of `schedules.manage`. It is a cross-cutting
-- operational setting reusable by future modules (tool types/tools reference a
-- row by FK from Story 4.2) — this table is authored exclusively by the Admin
-- module (AD-11).
--
-- V1 stores a repeating INTERVAL as unit + magnitude (`interval_unit`,
-- `interval_magnitude`, e.g. "1 year" = unit 'year', magnitude 1). The table is
-- PREPARED for future composite timing (cron-like, AD-16): `weekday_set`
-- (one or several days of week) and `time_of_day` are reserved nullable fields,
-- stored but NOT parsed/validated in V1 — no migration will be needed when the
-- composite engine lands.
--
-- SOFT ARCHIVE, never hard delete: archiving sets `archived_at = now()` so a
-- row keeps its history intact for FK references; the active surface filters
-- `archived_at IS NULL` and archive is irreversible via the surface in V1
-- (no unarchive). The canonical seed catalog is inserted here so the surface is
-- non-empty on first open (addendum.md seed vocabulary).
CREATE TABLE schedules (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    name               text NOT NULL UNIQUE,
    interval_unit      text NOT NULL
                       CHECK (interval_unit IN ('year', 'quarter', 'month', 'week', 'day')),
    interval_magnitude integer NOT NULL
                       CHECK (interval_magnitude > 0),
    weekday_set        text[],
    time_of_day        time,
    archived_at        timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

-- Canonical seed catalog (addendum.md): the five intervals the shared
-- inspection clock (AD-5) and the Tool module resolve in V1.
INSERT INTO schedules (name, interval_unit, interval_magnitude) VALUES
    ('1 year',    'year',    1),
    ('1 quarter', 'quarter', 1),
    ('1 month',   'month',   1),
    ('2 weeks',   'week',    2),
    ('3 days',    'day',     3);