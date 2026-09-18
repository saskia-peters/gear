package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/saskia-peters/gear/internal/admin/core"
)

// snapAppSettingRow is one app_settings row captured verbatim (including
// updated_at) so the shared dev DB can be restored exactly.
type snapAppSettingRow struct {
	key           string
	valueType     string
	durationValue *int64
	intValue      *int64
	textValue     *string
	updatedAt     time.Time
}

// snapshotAppSettings captures every app_settings row exactly as it exists in
// the shared dev DB. The store holds REAL admin-edited data, so the test
// snapshots-and-restores the whole table (the SMTP data-loss lesson) rather
// than assuming a pristine seed or restoring only the rows it touches.
func snapshotAppSettings(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []snapAppSettingRow {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT key, value_type, duration_value, int_value, text_value, updated_at FROM app_settings`)
	if err != nil {
		t.Fatalf("snapshotting app_settings err = %v", err)
	}
	defer rows.Close()
	var snap []snapAppSettingRow
	for rows.Next() {
		var r snapAppSettingRow
		if err := rows.Scan(&r.key, &r.valueType, &r.durationValue, &r.intValue, &r.textValue, &r.updatedAt); err != nil {
			t.Fatalf("scanning app_settings snapshot row err = %v", err)
		}
		snap = append(snap, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating app_settings snapshot err = %v", err)
	}
	return snap
}

// restoreAppSettings writes the snapshot back verbatim (upsert per key — the
// test never deletes rows, so re-inserting the captured rows fully restores).
func restoreAppSettings(t *testing.T, ctx context.Context, pool *pgxpool.Pool, snap []snapAppSettingRow) {
	t.Helper()
	for _, r := range snap {
		if _, err := pool.Exec(ctx, `INSERT INTO app_settings (key, value_type, duration_value, int_value, text_value, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (key) DO UPDATE SET
				value_type = EXCLUDED.value_type,
				duration_value = EXCLUDED.duration_value,
				int_value = EXCLUDED.int_value,
				text_value = EXCLUDED.text_value,
				updated_at = EXCLUDED.updated_at`,
			r.key, r.valueType, r.durationValue, r.intValue, r.textValue, r.updatedAt); err != nil {
			t.Fatalf("restoring app_settings row %q err = %v", r.key, err)
		}
	}
}

// TestPostgresAppSettingsMigration031 pins the Story 5-2c migration contract
// (000031) against the SHARED dev DB: the D1 key is renamed to a CONSUMED
// percent (25), the old day-count row is gone, the inventory_width seed is 9,
// the '1 Woche' schedule is seeded exactly once, and re-running the migration
// statements is idempotent — crucially, the width-only-if-6 guard never
// clobbers an admin-typed override. The app_settings table is
// snapshot-and-restored (the SMTP data-loss lesson); the idempotent schedule
// INSERT needs no restore.
func TestPostgresAppSettingsMigration031(t *testing.T) {
	pool := adminTestPool(t)
	t.Cleanup(pool.Close)
	ctx := context.Background()

	snap := snapshotAppSettings(t, ctx, pool)
	t.Cleanup(func() { restoreAppSettings(t, ctx, pool, snap) })

	// Apply the 000031 up statements exactly, twice — idempotency.
	for i := 0; i < 2; i++ {
		if _, err := pool.Exec(ctx, `INSERT INTO app_settings (key, value_type, int_value)
			VALUES ('inspection_orange_window_percent', 'integer', 25) ON CONFLICT (key) DO NOTHING`); err != nil {
			t.Fatalf("apply 000031 percent insert err = %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM app_settings WHERE key = 'inspection_orange_window_days'`); err != nil {
			t.Fatalf("apply 000031 days delete err = %v", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE app_settings SET int_value = 9 WHERE key = 'inventory_width' AND int_value = 6`); err != nil {
			t.Fatalf("apply 000031 width update err = %v", err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO schedules (name, interval_unit, interval_magnitude)
			VALUES ('1 Woche', 'week', 1) ON CONFLICT (name) DO NOTHING`); err != nil {
			t.Fatalf("apply 000031 schedule seed err = %v", err)
		}
	}

	var percent int64
	if err := pool.QueryRow(ctx, `SELECT int_value FROM app_settings WHERE key = 'inspection_orange_window_percent'`).Scan(&percent); err != nil {
		t.Fatalf("percent row missing: %v", err)
	}
	if percent != 25 {
		t.Errorf("percent = %d, want 25", percent)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM app_settings WHERE key = 'inspection_orange_window_days'`).Scan(&count); err != nil {
		t.Fatalf("counting days row err = %v", err)
	}
	if count != 0 {
		t.Errorf("inspection_orange_window_days rows = %d, want 0 (renamed away)", count)
	}
	var width int64
	if err := pool.QueryRow(ctx, `SELECT int_value FROM app_settings WHERE key = 'inventory_width'`).Scan(&width); err != nil {
		t.Fatalf("inventory_width row missing: %v", err)
	}
	if width != 9 {
		t.Errorf("inventory_width = %d, want 9", width)
	}
	var weeks int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schedules WHERE name = '1 Woche' AND interval_unit = 'week' AND interval_magnitude = 1`).Scan(&weeks); err != nil {
		t.Fatalf("counting 1 Woche rows err = %v", err)
	}
	if weeks != 1 {
		t.Errorf("'1 Woche' rows = %d, want exactly 1", weeks)
	}

	// Non-clobber guard: an admin-typed width override (5) survives a
	// re-applied width update (the WHERE int_value = 6 guard is the boundary).
	if _, err := pool.Exec(ctx, `UPDATE app_settings SET int_value = 5 WHERE key = 'inventory_width'`); err != nil {
		t.Fatalf("setting admin width override err = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE app_settings SET int_value = 9 WHERE key = 'inventory_width' AND int_value = 6`); err != nil {
		t.Fatalf("re-applying width update err = %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT int_value FROM app_settings WHERE key = 'inventory_width'`).Scan(&width); err != nil {
		t.Fatalf("reading width err = %v", err)
	}
	if width != 5 {
		t.Errorf("inventory_width after override = %d, want 5 (the 6-only guard must not clobber an admin override)", width)
	}
}

// TestPostgresAppSettingsStore verifies the Story 5-2b app_settings store
// (migration 000026 applied) against the SHARED dev DB. Because the table holds
// real admin-edited data, the test snapshots-and-restores the whole table in
// t.Cleanup (never assumes a pristine seed — the SMTP data-loss lesson) and
// asserts invariants that hold under legitimate edits: every catalog key is
// present with the catalog's value_type, exactly one value column is set per
// row, and every value is within its catalog bounds. A per-key upsert
// round-trips (update persists + reads back, untouched rows keep their values).
func TestPostgresAppSettingsStore(t *testing.T) {
	pool := adminTestPool(t)
	// t.Cleanup runs in reverse registration order: the pool closes LAST, after
	// the snapshot-restore (registered below) has run.
	t.Cleanup(pool.Close)

	ctx := context.Background()
	queries := New(pool)
	repo := NewRepository(queries)

	snap := snapshotAppSettings(t, ctx, pool)
	t.Cleanup(func() { restoreAppSettings(t, ctx, pool, snap) })

	// 1. Store shape: every catalog key is present, and exactly one value
	// column is set per row (the typed-store invariant).
	rows, err := repo.ListAppSettings(ctx)
	if err != nil {
		t.Fatalf("ListAppSettings err = %v", err)
	}
	if len(rows) < len(core.AppSettingCatalogKeys()) {
		t.Fatalf("store rows = %d, want >= %d (every catalog key)", len(rows), len(core.AppSettingCatalogKeys()))
	}
	byKey := make(map[string]*core.AppSetting, len(rows))
	for _, row := range rows {
		byKey[row.Key] = row
		set := 0
		if row.DurationValue != nil {
			set++
		}
		if row.IntValue != nil {
			set++
		}
		if row.TextValue != nil {
			set++
		}
		if set != 1 {
			t.Errorf("row %s sets %d value columns, want exactly 1", row.Key, set)
		}
	}

	// 2. Every catalog key is present with the catalog's value_type and a value
	// within its bounds (robust to legitimate admin edits — a zeroed/empty seed
	// is still caught here, but an admin-tuned value is not flagged).
	for _, key := range core.AppSettingCatalogKeys() {
		row, ok := byKey[key]
		if !ok {
			t.Errorf("store is missing catalog key %q", key)
			continue
		}
		catalogRow := core.AppSettingFor(&core.AppSettings{}, key)
		if catalogRow == nil {
			t.Errorf("no catalog def pinned for catalog key %q", key)
			continue
		}
		if row.ValueType != catalogRow.ValueType {
			t.Errorf("%s value_type = %q, want %q", key, row.ValueType, catalogRow.ValueType)
			continue
		}
		switch catalogRow.ValueType {
		case core.ValueTypeDuration:
			if row.Duration() <= 0 {
				t.Errorf("%s = %v, want > 0 (a zeroed duration is a seed wipe)", key, row.Duration())
			}
		case core.ValueTypeInteger:
			if row.Int() <= 0 {
				t.Errorf("%s = %d, want > 0 (a zeroed integer is a seed wipe)", key, row.Int())
			}
		case core.ValueTypeText:
			if row.Text() == "" {
				t.Errorf("%s = %q, want non-empty text", key, row.Text())
			}
		}
	}

	// 3. Upsert round-trip: update one duration + one text key, read back, then
	// the t.Cleanup snapshot restores the original values.
	newTTL := 2400 * time.Second
	if err := repo.UpsertAppSetting(ctx, &core.AppSetting{Key: "password_reset_ttl", ValueType: core.ValueTypeDuration, DurationValue: &newTTL}); err != nil {
		t.Fatalf("UpsertAppSetting(duration) err = %v", err)
	}
	newPrefix := "GKW"
	if err := repo.UpsertAppSetting(ctx, &core.AppSetting{Key: "inventory_prefix", ValueType: core.ValueTypeText, TextValue: &newPrefix}); err != nil {
		t.Fatalf("UpsertAppSetting(text) err = %v", err)
	}

	after, err := repo.ListAppSettings(ctx)
	if err != nil {
		t.Fatalf("ListAppSettings(after upsert) err = %v", err)
	}
	afterByKey := make(map[string]*core.AppSetting, len(after))
	for _, r := range after {
		afterByKey[r.Key] = r
	}
	if got := afterByKey["password_reset_ttl"].Duration(); got != newTTL {
		t.Errorf("password_reset_ttl after upsert = %v, want %v", got, newTTL)
	}
	if got := afterByKey["inventory_prefix"].Text(); got != newPrefix {
		t.Errorf("inventory_prefix after upsert = %q, want %q", got, newPrefix)
	}
	// The untouched rows keep their values (a per-key upsert does not clobber).
	if got := afterByKey["otp_length"].Int(); got <= 0 {
		t.Errorf("otp_length after upsert = %d, want the untouched value", got)
	}
}