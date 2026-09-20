package postgres

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saskia-peters/gear/internal/platform/dbtest"
)

// userTestPool returns a schema-isolated pool for the user/postgres tests
// (Story 7.4): a dedicated `gear_test_user` schema with the full migration set,
// so parallel `go test ./...` packages never share tables/rows/sequences. It
// t.Skipf when no DB is reachable and registers pool.Close as a t.Cleanup.
func userTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return dbtest.Open(t, "gear_test_user")
}

// userCleanupPool returns an independent pool bound to the SAME schema WITHOUT
// dropping/migrating it. Tests that run DELETE cleanups in a t.Cleanup use
// this instead of the primary pool (the primary pool's connection may be
// released; a cleanup delete needs its own pool that never closes early).
func userCleanupPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return dbtest.Connect(t, "gear_test_user")
}
