-- Reverse 000031 (Story 5-2c): restore the D1 day-count setting, the 000026
-- width seed 6 (only when the stored value is still the 000031 result 9 — the
-- reverse of the conditional bump), and remove the '1 Woche' seed row.

INSERT INTO app_settings (key, value_type, int_value)
VALUES ('inspection_orange_window_days', 'integer', 14)
ON CONFLICT (key) DO NOTHING;

DELETE FROM app_settings WHERE key = 'inspection_orange_window_percent';

UPDATE app_settings
SET int_value = 6
WHERE key = 'inventory_width' AND int_value = 9;

DELETE FROM schedules WHERE name = '1 Woche' AND interval_unit = 'week' AND interval_magnitude = 1;