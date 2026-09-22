-- G.E.A.R. Tool settings adoption + schedule catalog polish (Story 5-2c,
-- D1/C2/FR-30, deferred-work.md consumer adoption, scoped to the tool module):
--   (a) the D1 orange-window setting is REPURPOSED from a dead day-count to a
--       CONSUMED percentage of the inspection interval: the key is renamed
--       inspection_orange_window_days → inspection_orange_window_percent, the
--       seed becomes 25 (percent — a quarter of the interval, the current
--       interval/4 behavior), and deriveToolStatus consumes it.
--   (b) the seeded inventory_width default becomes 9 ('GEAR' + 9 zero-padded
--       digits, still inside the ≤16 CHECK), but ONLY when the stored value is
--       still the 000026 seed 6 — an admin-typed override is NEVER clobbered.
--   (c) the schedule catalog gains the missing '1 Woche' seed (week, 1) so the
--       admin Zeitpläne list renders 3 Tage < 1 Woche < 2 Wochen < ... — the
--       duration-ascending sort (ListSchedules) then orders it correctly.

-- (a) D1 rename: the new percent row (idempotent), then the old day-count row
-- is dropped (its 14-day value was dead data — nothing ever consumed it).
INSERT INTO app_settings (key, value_type, int_value)
VALUES ('inspection_orange_window_percent', 'integer', 25)
ON CONFLICT (key) DO NOTHING;

DELETE FROM app_settings WHERE key = 'inspection_orange_window_days';

-- (b) width default 9, only when the stored value is still the 000026 seed 6.
UPDATE app_settings
SET int_value = 9
WHERE key = 'inventory_width' AND int_value = 6;

-- (c) the '1 Woche' seed (name is UNIQUE — idempotent ON CONFLICT).
INSERT INTO schedules (name, interval_unit, interval_magnitude)
VALUES ('1 Woche', 'week', 1)
ON CONFLICT (name) DO NOTHING;