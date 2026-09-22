---
title: 'Integration & Seed Assertions (NFR-R2, AD-12/AD-13)'
type: 'chore'
created: '2026-09-19'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'cacc451205a09d590b5ada9d93de9904ad7fff0e'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-7-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The cold-start seed — 4 base roles, the full permission catalog, the role→permission base matrix, and the two seeded admin accounts — is verified only by manual checks and live `just migrate-up/down`. A broken seed (a dropped role, a missing permission grant, a deleted admin account) would surface only on a fresh deployment (NFR-R2, AD-12/AD-13).

**Approach:** Add a DB-backed integration test (`//go:build integration`) that applies the migrations to a clean schema and asserts the seed contents — the 4 base roles, the complete permission catalog, the AD-12 role→permission matrix, and the 2 admin accounts — by querying the REAL user-module repository. Gate it behind a `just test-integration` recipe (and run it in CI). Reuses the `dbtest` schema-isolation helper (Story 7.4) so it needs no live shared DB and stays deterministic under parallel `go test`.

## Boundaries & Constraints

**Always:**
- **Integration tag:** a new test (or small suite) with `//go:build integration` so it is NOT part of the default `go test ./...` (no DB coupling in the ordinary gate), matching the repo's existing `//go:build dev` precedent.
- **Clean schema:** the test uses `dbtest.Open(t, "gear_test_seed")` — drop+create, apply the FULL migration set, search_path-bound pool — so it runs against a fresh, migration-completed schema every time (no dependence on a pre-seeded dev DB).
- **Assert through the real repository, not raw SQL:** use `userpostgres.NewRepository(userpostgres.New(pool))` — `ListGroups` (base roles + their permissions), `ListAllPermissions` (catalog), `GetUserByEmail` (the two admins + their `admin` role via `ListUserGroupNamesByUsers` or the group read) — so the test pins the same code path a fresh deployment exercises.
- **Seed assertions (AD-12/AD-13):**
  - Exactly 4 base roles: `helfende`, `schirrmeister`, `fuehrende`, `admin` (each `is_base_role = true`).
  - The permission catalog contains the documented codes (the spine base-role matrix's 22 codes + the later additions `tool.edit`, `user.account.approve`, `admin.settings.system` — **25 distinct** codes in `permissions`; the spine table predates the latter three), each with a non-empty label.
  - The role→permission matrix matches the documented base series: `helfende` = dashboard.view + inspection.submit; `schirrmeister` = + tools.manage + tool_types.manage + inspection.history.view + users.view + users.qualifications.manage + tool.edit; `fuehrende` = schirrmeister + report.export + tool.reinstate; `admin` = all 25 codes. (Bold "uniquely-owned" codes per the spine are covered implicitly by exact-set assertions.)
  - Exactly 2 admin accounts: `admin.1@gear.local` and `admin.2@gear.local`, both `active`, both holding the `admin` base role (FR-27/AD-13).
- **justfile recipe:** `just test-integration` — brings the DB up (reuses `db-wait`/`migrate-up` if the dev DB is down), then runs `go test -tags integration ./internal/user/adapters/postgres/` (the seed test's package) with `DATABASE_URL` set. Idempotent, mirrors the existing recipe style.
- **CI:** `.github/workflows/ci.yml` adds a step that runs the integration-tagged tests directly (`go test -tags integration -v ./internal/user/adapters/postgres/`) after the parallel Go gate — `just test-integration` is for local use only (its `db-wait`→`podman-check` dependency can't run on CI runners, which have no podman; the Postgres is the services container, exactly like the migrate step in 7.1). The seed assertions use the same postgres service; they must RUN, not skip — a skip in CI is a failure, matching the 7.1 DB-skip guard.

**Ask First:**
- None (the deferred-work recommendation is explicit: `//go:build integration` + `just test-integration`, run in CI).

**Never:**
- No `//go:build integration` test that skips silently in CI (the CI step must fail if it skips).
- No assertion against a PRE-SEEDED dev DB's `public` schema (always the fresh `dbtest` schema).
- No changes to the migrations, seed data, or repository code (the test PINS the seed as-is).
- No coupling of `go test ./...` to a live DB (the integration tag keeps it out of the default gate).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| SEED_OK | fresh schema, migrations applied | 4 base roles, 25 permissions, matrix + 2 admins all match | PASS |
| SEED_BROKEN_ROLE | a base role missing/renamed | test fails, names the missing role | FAIL (clear message) |
| SEED_BROKEN_PERM | a permission grant missing | test fails, names the missing code | FAIL (clear message) |
| SEED_BROKEN_ADMIN | admin account missing/not active | test fails, names the account | FAIL |
| NO_DB | DB unreachable | test skips (dbtest convention) | SKIP |
| CI_RUN | CI executes test-integration | seed test RUNS (not skipped) | FAIL if skipped |

</frozen-after-approval>

## Code Map

- `internal/user/adapters/postgres/seed_integration_test.go` (new) -- `//go:build integration`; uses `dbtest.Open(t, "gear_test_seed")`; asserts base roles, permission catalog, matrix, admin accounts through `userpostgres` repo.
- `internal/platform/dbtest/dbtest.go` (existing, Story 7.4) -- `Open` provides the clean migrated schema + search_path pool; reused as-is.
- `internal/user/adapters/postgres/groups.go` -- `ListGroups` (`:26`, returns each group with its permission codes + `IsBaseRole`), `ListAllPermissions` (`:224`, the catalog).
- `internal/user/adapters/postgres/users_admin_repo.go` -- `ListUsers` (`:33`), `ListUserGroupNamesByUsers` (`:61`), `GetUserDetail` (`:86`) for the admin accounts' role membership.
- `internal/user/core/roles.go` -- the base-role constants (`RoleCreatePermission` etc.) + the role-group domain types the assertions compare against.
- `justfile` -- add `test-integration` recipe (db-wait → `go test -tags integration ./internal/user/adapters/postgres/`).
- `.github/workflows/ci.yml` -- add a step running the integration-tagged tests directly after the Go gate (`just test-integration` needs podman, absent on CI); treat a skip or a vanished suite as a failure (grep the output).
- Tests: the seed assertions are themselves the verification (plus a negative check that a deliberately-broken expectation fails).

## Tasks & Acceptance

**Execution:**
- [x] `internal/user/adapters/postgres/seed_integration_test.go` (new) -- `//go:build integration` seed assertions via the real repository -- integration test
- [x] `justfile` -- `test-integration` recipe (db-wait + tagged test) -- recipe
- [x] `.github/workflows/ci.yml` -- `test-integration` step (skip = failure) -- CI
- [x] Run `just test-integration` + full `go test ./...` + lint -- verification

**Acceptance Criteria:**
- Given the migration set, when the integration-tagged test runs against a fresh `dbtest` schema, then it asserts the 4 base roles, the complete permission catalog, the AD-12 role→permission matrix, and EXACTLY the 2 seeded admin accounts (FR-27/AD-12/AD-13), and passes.
- Given the `justfile`, when a maintainer runs `just test-integration`, then it brings up the DB (if needed) and runs the integration-tagged tests against a migrated schema (NFR-R2).
- Given CI, when it runs, then the `test-integration` step runs the seed assertions (not skipped) and fails on any seed regression or silent skip.

## Spec Change Log

- **2026-09-19, review patches (review 1):** (a) the seed test now asserts EXACTLY 2 admin-role holders (an extra seeded admin no longer ships undetected); (b) the permission catalog is pinned against an INDEPENDENT 25-code list, not derived from the same matrix the role test uses (a code dropped from both can no longer pass silently); (c) the catalog label check uses TrimSpace and the correct field (the repo's `ListAllPermissions` returns `Label`, not `Description`); (d) the CI step runs the integration-tagged tests directly (`just test-integration` needs podman, absent on CI — same constraint as the 7.1 migrate step) and fails if the seed suite did NOT run (grep for `TestIntegrationSeed`) or skipped; (e) the step uses `set -o pipefail` so a failing `go test` is not masked by `tee`; (f) stdlib `slices.Sorted`/`slices.Equal`/`maps.Keys` replace bespoke helpers (no tag-scope collision); `core.StateActive` replaces a hardcoded `"active"`; duplicate base-role detection added; trailing newlines added; (g) the recipe/CI run only the user package (the only integration suite) with `-v`; (h) the spec's catalog count corrected to 25 (the spine matrix's 22 + three later additions) — the pre-existing `23-code`/`24-code` comments in `groups.go`/`roles.go` are stale and deferred, not this story's code. KEEP: exact-permission-set matrix assertions and the fresh-schema `dbtest` approach.

## Design Notes

- **Assert via the repository, not raw SQL:** a fresh deployment exercises the migration → repo → service path; asserting through `ListGroups`/`ListAllPermissions`/`GetUserByEmail` pins that exact path. Raw `SELECT` counts would only prove rows exist, not that the read path sees them.
- **`dbtest` reuse keeps it deterministic:** the seed test applies the full migration set to its own schema every run, so it never depends on a pre-seeded dev DB and is safe under parallel `go test` (Story 7.4).
- **Exact matrix, not a sample:** each role is asserted to its EXACT permission set (a missing OR an unexpected grant both fail). This is the difference between "seed looks okay" and "seed matches AD-12".
- **Integration tag ≠ CI skip:** the tag keeps the test out of `go test ./...`, but CI explicitly runs it and treats a skip as a failure — the 7.1 guard principle.

## Verification

**Commands:**
- `just test-integration` -- expected: DB up (if needed), seed assertions pass (base roles, catalog, matrix, admins)
- `go test ./...` (no tags) -- expected: seed test NOT run (integration tag), everything else green
- `just lint` -- expected: 0 issues
- Negative check: temporarily remove one grant expectation → the test fails (proves it asserts, not vacuously passes)

**Manual checks (if no CLI):**
- Inspect the CI workflow: the `test-integration` step runs after the Go gate and greps for "skipping" to fail on a silent skip.

## Suggested Review Order

**Entry point — the seed assertions**

- Read first: pins the AD-12 base-role matrix — exact permission sets per role, no duplicate base roles
  [`seed_integration_test.go:115`](../../internal/user/adapters/postgres/seed_integration_test.go#L115)

**Catalog + admin invariants**

- The permission catalog is pinned against an INDEPENDENT 25-code list (a code dropped from both the seed and the matrix can't pass)
  [`seed_integration_test.go:159`](../../internal/user/adapters/postgres/seed_integration_test.go#L159)
- Exactly 2 admin accounts, both active and holding the admin base role (FR-27/AD-13)
  [`seed_integration_test.go:195`](../../internal/user/adapters/postgres/seed_integration_test.go#L195)

**The gate**

- `just test-integration` — brings the DB up, runs the tagged suite with -v (single source of truth for CI)
  [`justfile:168`](../../justfile#L168)
- CI step runs the tagged tests directly (no podman on runners) and fails if the seed suite did not RUN or skipped (pipefail + positive grep)
  [`ci.yml:118`](../../.github/workflows/ci.yml#L118)