---
title: 'Parallel-DB Test Isolation (NFR-M2)'
type: 'refactor'
created: '2026-09-19'
status: 'done'
review_loop_iteration: 0
baseline_commit: '1b2fbe44ddd7f236423415ce2077b47f0cef42bc'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-7-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** `go test ./...` runs Go packages in parallel by default (`-p = #CPUs`), but the DB-backed suites all share ONE database schema (`gear` public): they use `test-%`/`Test-%` row prefixes and the single global `tools_inventory_number_seq`. Under parallel mode, one package's `DELETE ... LIKE 'test-%'` cleanup deletes another package's rows mid-test, and the sequence is shared — so `TestComposedAdminRouteGroup`, `TestPostgresToolInventoryArchivedBackstop`, `TestPostgresCreateToolInventoryCollisionRetry`, `TestComposedToolCreateAutoAssignsInventory` (and the already-relaxed `TestPostgresCreateToolsBatchReturnsAllInputs`) flake. CI currently works around this with `-p 1` (Story 7.1); the default parallel mode is not trustworthy (NFR-M2).

**Approach:** Isolate each DB-backed test **package into its own PostgreSQL schema** (e.g. `gear_test_tools`, `gear_test_admin`, `gear_test_user`, `gear_test_admincomp`, `gear_test_authcomp`, `gear_test_server`). A shared `dbtest` helper creates the schema, applies the full migration set there (via search_path + multi-statement `Exec`), and returns a pool bound to that schema. Each package gets its OWN sequence, its OWN `test-%` rows, and its OWN tables — parallel interference is structurally impossible, not mitigated.

## Boundaries & Constraints

**Always:**
- **One schema per test package**, not per test file or per test case. Packages with DB tests: `internal/tools/adapters/postgres`, `internal/admin/adapters/postgres`, `internal/user/adapters/postgres`, `internal/user/adapters/http` (admin composition), `internal/platform/auth` (composition), `cmd/server` (composed tool tests). Each connects to its own schema.
- **Shared helper:** new package `internal/platform/dbtest` with `func Open(t *testing.T, schema string) *pgxpool.Pool`. It:
  - connects to `DATABASE_URL` (default `postgres://gear:gear@localhost:5432/gear`),
  - `CREATE SCHEMA IF NOT EXISTS <schema>` (CASCADE on cleanup),
  - `SET search_path TO <schema>` on the connection,
  - applies every `migrations/*.up.sql` in numeric order via multi-statement `Exec` (verified: pgx executes multi-statement SQL + search_path),
  - skips with the existing `t.Skipf` convention when no DB is reachable,
  - returns a pool whose connections have `search_path` set (via `AfterConnect` hook) so all unqualified table references resolve to the schema.
- **Every existing pool helper replaced:** `toolTestPool` (tools/postgres), `adminTestPool` (admin/postgres), the inline pools in `internal/user/adapters/postgres/*_test.go`, `newCompositionRouter` (auth/composition), the admin-composition pool (user/http), and the `cmd/server` composed-tool pool — all call `dbtest.Open(t, "<pkg-schema>")`.
- **`seedToolRefs` / `seedUserRefs` style seeding works unchanged** — they already operate through the pool and use `test-%` names; with per-schema isolation they no longer collide across packages.
- **CI drops the workaround:** `.github/workflows/ci.yml`'s Go test step changes from `go test -p 1` back to the parallel default `go test ./cmd/... ./internal/...` (the `-p 1` comment is removed). The DB-skip guard stays.
- **justfile `test` unchanged** (it already runs the parallel default); the `-p 1` mention in comments/docs is removed.
- **Test cleanup conventions stay** (`test-%` prefixes, per-suite DELETE-on-setup) — they're now per-schema.

**Ask First:**
- None (per-suite schemas is the deferred-work-recommended fix and the only one that fixes the shared-sequence race deterministically).

**Never:**
- No test namespacing of rows as the PRIMARY mechanism (it cannot fix the shared `tools_inventory_number_seq` race — the retry test reads exact sequence values).
- No changes to application code, migrations, or the migration set (the schema gets the same files).
- No `go test` code change beyond test helpers + the CI `-p 1` removal.
- No new third-party dependency (pgx is already used).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| PARALLEL_GREEN | `go test ./...` default (parallel) with DB up | all packages pass, no flakes (5 known tests deterministic) | n/a |
| DB_DOWN | no DB reachable | each package's suite `t.Skipf`s (existing convention) | skip |
| SCHEMA_REUSE | schema already exists (prior run) | `CREATE SCHEMA IF NOT EXISTS` is a no-op; migrations re-applied idempotently? NO — migrations are NOT idempotent; the helper must DROP+CREATE the schema per package run to guarantee a clean slate | clean slate |
| SEQUENCE_ISOLATED | two packages create tools in parallel | each uses its OWN `tools_inventory_number_seq` (per-schema) → no cross-contention | n/a |
| CI_GATE | CI runs `go test` (no -p 1) | DB-backed suites run in parallel, deterministic | any failure → red |

</frozen-after-approval>

## Code Map

- `internal/platform/dbtest/dbtest.go` (new) -- `Open(t, schema)` helper: pool + `AfterConnect` `SET search_path`, `CREATE SCHEMA IF NOT EXISTS` then `DROP SCHEMA <schema> CASCADE; CREATE SCHEMA <schema>` (clean slate), read `migrations/*.up.sql` in order + multi-statement `Exec`, `t.Skipf` on no DB.
- `internal/tools/adapters/postgres/tool_types_test.go` -- `toolTestPool` → `dbtest.Open(t, "gear_test_tools")`; the 9 files using it stay.
- `internal/admin/adapters/postgres/settings_test.go` -- `adminTestPool` → `dbtest.Open(t, "gear_test_admin")`.
- `internal/user/adapters/postgres/*_test.go` -- the inline `pgxpool.New` helpers → `dbtest.Open(t, "gear_test_user")`.
- `internal/user/adapters/http/admin_composition_test.go` -- pool → `dbtest.Open(t, "gear_test_admincomp")`.
- `internal/platform/auth/composition_test.go` -- `newCompositionRouter` pool → `dbtest.Open(t, "gear_test_authcomp")`.
- `cmd/server/main_test.go` -- the composed-tool pool (`TestComposedToolCreateAutoAssignsInventory`) → `dbtest.Open(t, "gear_test_server")`.
- `.github/workflows/ci.yml` -- Go test step: drop `-p 1` (back to parallel default); keep the DB-skip guard.
- `deferred-work.md` -- mark the two parallel-contention entries resolved.
- Tests: the 5 flaky tests passing under `go test ./...` parallel is the verification.

## Tasks & Acceptance

**Execution:**
- [x] `internal/platform/dbtest/dbtest.go` (new) -- `Open(t, schema)` with clean-schema + migrations + search_path pool -- helper
- [x] `internal/tools/adapters/postgres/tool_types_test.go` -- `toolTestPool` → dbtest.Open -- tools
- [x] `internal/admin/adapters/postgres/settings_test.go` -- `adminTestPool` → dbtest.Open -- admin
- [x] `internal/user/adapters/postgres/*_test.go` -- inline pools → dbtest.Open -- user
- [x] `internal/user/adapters/http/admin_composition_test.go` -- → dbtest.Open -- user/http
- [x] `internal/platform/auth/composition_test.go` -- → dbtest.Open -- auth
- [x] `cmd/server/main_test.go` -- composed-tool pool → dbtest.Open -- server
- [x] `.github/workflows/ci.yml` -- drop `-p 1` (parallel default) -- CI
- [x] deferred-work.md -- mark the two contention entries resolved -- tracking

**Acceptance Criteria:**
- Given the DB-backed suites, when `go test ./...` runs in the DEFAULT parallel mode with the DB up, then all packages pass deterministically — including the previously flaky `TestComposedAdminRouteGroup`, `TestPostgresToolInventoryArchivedBackstop`, `TestPostgresCreateToolInventoryCollisionRetry`, `TestComposedToolCreateAutoAssignsInventory`, `TestPostgresCreateToolsBatchReturnsAllInputs` (NFR-M2).
- Given the CI workflow, when it runs, then the Go test step uses the parallel default (no `-p 1` workaround) and the DB-backed suites run for real (skip guard intact).
- Given no DB, when the suites run, then each package `t.Skipf`s (existing convention preserved).
- Given a prior run, when the schema already exists, then the helper drops+recreates it so migrations apply to a clean slate (no stale `test-%` rows).

## Spec Change Log

- **2026-09-19, review patches (review 1):** (a) migration files are sorted NUMERICALLY on the `NNNNNN_` prefix (lexicographic would mis-order non-zero-padded prefixes) and a zero-match glob now fails loudly (no vacuous empty-schema greens); (b) schema names are validated (`^[a-z][a-z0-9_]*$`) before DDL/search_path interpolation (identifier-injection guard); (c) `Connect` fails with a clear "call Open first" error when the schema was not created (misordered Open/Connect no longer surfaces as a confusing "relation does not exist"); (d) a deterministic isolation-contract test pins `current_schema()` + schema-qualified table namespace (a dropped `AfterConnect` hook now fails red, not as an intermittent flake); (e) setup budget raised to 2m (the per-test drop+create+migrate grew); (f) the `Open` docstring warns against `t.Parallel()` within a package (shared schema is dropped/recreated); (g) spec schema list corrected to the real names (`gear_test_admincomp`/`gear_test_authcomp`). KEEP: per-package clean-slate schemas, the `-p 1` CI removal, and the skip-only-when-no-DB convention.

## Design Notes

- **Per-schema is the only fix for the sequence race:** `TestPostgresCreateToolInventoryCollisionRetry` reads `tools_inventory_number_seq.last_value` and asserts exact next values. Row namespacing can't help — the sequence is a single global object. A per-package schema gives each package its OWN sequence (the migration `CREATE SEQUENCE tools_inventory_number_seq` runs in that schema), so the race disappears structurally.
- **Clean slate, not idempotent replay:** migrations use plain `CREATE TABLE` (not `IF NOT EXISTS`), so the helper does `DROP SCHEMA <schema> CASCADE; CREATE SCHEMA <schema>` at the start of each package run, then applies the files in order. Deterministic, no stale rows.
- **search_path via AfterConnect:** every connection the pool opens runs `SET search_path TO <schema>`, so unqualified table names in both migrations and queries resolve to the package's schema. One code path, no per-query qualification.
- **CI back to parallel:** removing `-p 1` restores the trustworthy-default goal; the DB-skip guard from 7.1 stays so a silent skip still fails.

## Verification

**Commands:**
- `go test ./cmd/... ./internal/...` (default parallel, DB up) -- expected: ALL packages green, deterministically (run twice to confirm no flake)
- `go test -race -count=2 ./internal/tools/adapters/postgres/ ./cmd/server/` -- expected: the 5 flaky tests pass repeatedly
- `just lint` -- expected: 0 issues
- `git diff` -- expected: only test helpers + CI `-p 1` removal + deferred-work; no app/migration changes

**Manual checks (if no CLI):**
- `psql` the dev DB after a test run: schemas `gear_test_tools`, `gear_test_admin`, `gear_test_user`, etc. exist, each with the full migration set; the `public` schema is untouched by tests.

## Suggested Review Order

**Entry point — the isolation helper**

- Read first: the per-package schema lifecycle — drop+create, apply the full migration set, search_path-bound pool
  [`dbtest.go:48`](../../internal/platform/dbtest/dbtest.go#L48)
- Secondary pool (no drop) + the missing-schema guard
  [`dbtest.go:121`](../../internal/platform/dbtest/dbtest.go#L121)
- Numeric migration ordering + zero-file guard (a wrong order would build a broken schema)
  [`dbtest.go:185`](../../internal/platform/dbtest/dbtest.go#L185)

**Deterministic isolation contract**

- Pins that a returned pool resolves tables to its own schema, not public — a dropped AfterConnect hook fails red, not as a flake
  [`dbtest_test.go:10`](../../internal/platform/dbtest/dbtest_test.go#L10)

**The rewired pool helpers**

- user/postgres shared helper (primary + cleanup pools)
  [`testpool_test.go:15`](../../internal/user/adapters/postgres/testpool_test.go#L15)

**CI reverts to parallel**

- Go tests back to default parallel mode (per-schema isolation now makes it deterministic)
  [`ci.yml:100`](../../.github/workflows/ci.yml#L100)