-- G.E.A.R. Admin-owned schedule catalog (Story 4.1, FR-30/AD-16).
--
-- Renames the 000019 canonical seed rows to German names so the catalog is
-- consistent with the German surface naming (Zeitpläne). The name is a
-- display label; the interval (unit + magnitude) is unchanged, so FK
-- references and the interval semantics are untouched. The migration is a
-- plain UPDATE of the seed labels — it must not run twice with different
-- results, which is why the WHERE clauses guard on the exact English names
-- from 000019.
UPDATE schedules SET name = '1 Jahr'    WHERE name = '1 year'    AND interval_unit = 'year';
UPDATE schedules SET name = '1 Quartal' WHERE name = '1 quarter' AND interval_unit = 'quarter';
UPDATE schedules SET name = '1 Monat'   WHERE name = '1 month'   AND interval_unit = 'month';
UPDATE schedules SET name = '2 Wochen'  WHERE name = '2 weeks'   AND interval_unit = 'week';
UPDATE schedules SET name = '3 Tage'    WHERE name = '3 days'    AND interval_unit = 'day';