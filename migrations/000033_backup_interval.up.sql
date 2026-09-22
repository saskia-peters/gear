-- G.E.A.R. `backup_interval` system setting (Story 7.7, NFR-R3): the
-- configurable ticker interval of the in-process backup job — how often the
-- job runs pg_dump and ships the artifact to every configured destination.
-- Seeded to 86400 seconds (daily), admin-configurable via the Einstellungen →
-- System surface (min 1 second, enforced by the catalog's `min: 1`).
--
-- This is a DATA-ONLY migration (no schema change): it adds one row to the
-- existing Admin-owned app_settings store (000026). The row is idempotent
-- (ON CONFLICT DO NOTHING) so re-applying never clobbers an admin override.

INSERT INTO app_settings (key, value_type, duration_value, int_value, text_value)
VALUES ('backup_interval', 'duration', 86400, NULL, NULL)
ON CONFLICT (key) DO NOTHING;