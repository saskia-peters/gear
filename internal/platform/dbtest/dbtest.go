// Package dbtest isolates DB-backed test suites per package (Story 7.4,
// NFR-M2). `go test ./...` runs Go packages in parallel by default, but the
// suites all used the ONE shared database schema (public): `test-%`/`Test-%`
// row prefixes and the single global `tools_inventory_number_seq` caused
// cross-package interference (one package's cleanup deleting another's rows
// mid-test; the collision-retry test reading exact shared-sequence values).
//
// Open() gives each test package its OWN PostgreSQL schema (e.g.
// `gear_test_tools`): it creates a clean schema, applies the full migration
// set there, and returns a pool whose connections resolve unqualified table
// names to that schema via search_path. Each schema has its own tables, rows
// AND sequences — parallel interference becomes structurally impossible.
package dbtest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// schemaNameRe validates a schema name before it is interpolated into DDL /
// SET search_path: lowercase ASCII identifier, no reserved-word surprises.
var schemaNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// defaultURL mirrors the justfile default so a local run needs no config.
const defaultURL = "postgres://gear:gear@localhost:5432/gear?sslmode=disable"

// Open returns a pool bound to a dedicated per-package schema, with the full
// migration set applied to a CLEAN schema. It t.Skipf when no DB is reachable
// (the existing postgres-suite convention). `schema` must be a short stable
// lowercase name (e.g. "gear_test_tools") — it is used verbatim as a
// PostgreSQL schema (validated). Open is for the PRIMARY pool of a test; the
// schema is dropped and recreated, so call it once per test (see Connect for
// secondary pools). NOTE: it is NOT safe to call Open from two goroutines
// (t.Parallel) within the same package — the shared per-package schema is
// dropped and recreated, so a parallel test would wipe the other's tables.
func Open(t *testing.T, schema string) *pgxpool.Pool {
	t.Helper()
	validateSchema(t, schema)

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = defaultURL
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	admin, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("skipping db integration test: %v", err)
	}
	defer admin.Close()
	if err := admin.Ping(ctx); err != nil {
		t.Skipf("skipping db integration test (db ping failed): %v", err)
	}

	// DROP/CREATE with QUALIFIED schema names — never depend on a pooled
	// connection's search_path (a prior Open may have left it pointing at its
	// schema on a reused connection).
	if _, err := admin.Exec(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema)); err != nil {
		t.Fatalf("drop schema %s: %v", schema, err)
	}
	if _, err := admin.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schema)); err != nil {
		t.Fatalf("create schema %s: %v", schema, err)
	}

	// A FRESH (non-pooled) connection with search_path set, so the migrations'
	// CREATE TABLE/INDEX/SEQUENCE resolve to the package's schema. Using a
	// standalone pgx.Connect avoids any pooled-connection search_path leakage.
	mig, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect for migrations: %v", err)
	}
	defer func() { _ = mig.Close(ctx) }()
	if _, err := mig.Exec(ctx, fmt.Sprintf("SET search_path TO %s", schema)); err != nil {
		t.Fatalf("set search_path for migrations: %v", err)
	}

	// Apply the migration set in numeric order against the schema. Multi-
	// statement files execute in one Exec (pgx handles them); search_path
	// resolves unqualified CREATE TABLE/INDEX/SEQUENCE to the schema.
	for _, path := range migrationFiles(t) {
		sql, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read migration %s: %v", path, err)
		}
		if _, err := mig.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply migration %s: %v", filepath.Base(path), err)
		}
	}

	// A pool whose every connection runs `SET search_path TO <schema>`, so
	// both the helpers' seeding and the code under test resolve tables there.
	return schemaPool(t, dbURL, schema)
}

// validateSchema fails fast on a schema name that is unsafe to interpolate
// into DDL / SET search_path (identifier injection / quoting surprises).
func validateSchema(t *testing.T, schema string) {
	t.Helper()
	if !schemaNameRe.MatchString(schema) {
		t.Fatalf("dbtest: invalid schema name %q (want ^[a-z][a-z0-9_]*$)", schema)
	}
}

// Connect returns a pool bound to an EXISTING per-package schema WITHOUT
// dropping or migrating it (no clean slate). Use it for SECONDARY pools in a
// test — e.g. a t.Cleanup delete pool that must survive the primary pool's
// closure — where Open would wipe the schema the primary pool just migrated.
func Connect(t *testing.T, schema string) *pgxpool.Pool {
	t.Helper()
	validateSchema(t, schema)
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = defaultURL
	}

	// Fail clearly if the schema was never created (Open not called first), so
	// a misordered Open/Connect surfaces a setup error, not a later "relation
	// does not exist".
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	probe, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("skipping db integration test: %v", err)
	}
	defer probe.Close()
	var exists bool
	if err := probe.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)", schema).Scan(&exists); err != nil {
		t.Fatalf("check schema %s: %v", schema, err)
	}
	if !exists {
		t.Fatalf("dbtest: schema %q does not exist — call dbtest.Open(t, %q) (or an equivalent pool helper) before Connect", schema, schema)
	}

	return schemaPool(t, dbURL, schema)
}

// schemaPool builds a pool whose connections resolve to `schema` via
// search_path, registering pool.Close as a t.Cleanup. The AfterConnect hook
// must be set on the Config BEFORE pgxpool.NewWithConfig (pgx's Config()
// returns a copy, so mutating it after New has no effect).
func schemaPool(t *testing.T, dbURL, schema string) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Skipf("skipping db integration test: %v", err)
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, fmt.Sprintf("SET search_path TO %s", schema))
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Skipf("skipping db integration test: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("skipping db integration test (schema pool ping failed): %v", err)
	}
	return pool
}

// migrationFiles returns the sorted migrations/*.up.sql paths. The migrations
// directory is the single golang-migrate source. Resolved relative to THIS
// source file (dbtest.go → internal/platform/dbtest → up 3 = module root),
// NOT the caller's CWD: Go tests run with CWD = the test package dir, which
// differs per package (internal/admin/adapters/postgres vs internal/platform/
// dbtest), so a fixed-depth relative path would break for some callers.
func migrationFiles(t *testing.T) []string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	dir := filepath.Join(root, "migrations")
	matches, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("dbtest: no migration files found under %s — the schema would be empty (wrong root?)", dir)
	}
	// NUMERIC sort on the NNNNNN_ prefix: lexicographic would mis-order when
	// prefixes differ in digit width (e.g. 00002 vs 000010).
	sort.Slice(matches, func(i, j int) bool {
		bi := migrationNum(t, matches[i])
		bj := migrationNum(t, matches[j])
		return bi < bj
	})
	return matches
}

// migrationNum extracts the numeric prefix of a migration filename.
func migrationNum(t *testing.T, path string) int {
	t.Helper()
	base := strings.SplitN(filepath.Base(path), "_", 2)[0]
	n, err := strconv.Atoi(base)
	if err != nil {
		t.Fatalf("dbtest: migration %s has non-numeric prefix %q", path, base)
	}
	return n
}
