-- Reverts the 000020 German rename back to the original English seed labels.
UPDATE schedules SET name = '1 year'    WHERE name = '1 Jahr'    AND interval_unit = 'year';
UPDATE schedules SET name = '1 quarter' WHERE name = '1 Quartal' AND interval_unit = 'quarter';
UPDATE schedules SET name = '1 month'   WHERE name = '1 Monat'   AND interval_unit = 'month';
UPDATE schedules SET name = '2 weeks'   WHERE name = '2 Wochen'  AND interval_unit = 'week';
UPDATE schedules SET name = '3 days'    WHERE name = '3 Tage'    AND interval_unit = 'day';