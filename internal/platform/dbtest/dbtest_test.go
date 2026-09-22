package dbtest

import (
	"testing"
)

// TestOpenBindsSearchPath deterministically pins the isolation contract (Story
// 7.4): a pool returned by Open resolves unqualified tables to the requested
// schema, NOT to public. This makes a regression (e.g. a dropped AfterConnect
// hook) fail deterministically instead of as an intermittent parallel flake.
func TestOpenBindsSearchPath(t *testing.T) {
	pool := Open(t, "gear_test_contract")
	ctx := t.Context()

	var current string
	if err := pool.QueryRow(ctx, "SELECT current_schema()").Scan(&current); err != nil {
		t.Fatalf("current_schema: %v", err)
	}
	if current != "gear_test_contract" {
		t.Fatalf("current_schema = %q, want gear_test_contract (search_path not bound)", current)
	}

	// Unqualified tables resolve to the schema (the full migration set ran).
	var tools int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM tools").Scan(&tools); err != nil {
		t.Fatalf("tools via unqualified name: %v", err)
	}
	// The schema-qualified table must exist — the migration ran in the schema.
	var nsp string
	if err := pool.QueryRow(ctx,
		"SELECT n.nspname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE c.relname = 'tools' AND n.nspname = current_schema()").Scan(&nsp); err != nil {
		t.Fatalf("tools in current_schema: %v", err)
	}
	if nsp != "gear_test_contract" {
		t.Fatalf("tools lives in %q, want gear_test_contract (isolation broken)", nsp)
	}
}

// TestConnectAfterOpen ensures Connect works for a secondary pool on an
// Open-created schema (the t.Cleanup-delete-pool pattern).
func TestConnectAfterOpen(t *testing.T) {
	pool := Open(t, "gear_test_contract")
	secondary := Connect(t, "gear_test_contract")
	var n int
	if err := secondary.QueryRow(t.Context(), "SELECT count(*) FROM tools").Scan(&n); err != nil {
		t.Fatalf("secondary pool query: %v", err)
	}
	if pool == nil || secondary == nil {
		t.Fatal("expected non-nil pools")
	}
}
