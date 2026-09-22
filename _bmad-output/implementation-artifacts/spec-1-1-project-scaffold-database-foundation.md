---
title: 'Project Scaffold & Database Foundation'
type: 'feature'
created: '2026-09-04'
status: 'done'
review_loop_iteration: 0
baseline_commit: '464d36d6e8ff9132ef4ce8950e42d3831c3e0f8a'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The G.E.A.R. repository has no application code — only the Docusaurus docs site. Every later story (auth, permissions, inspections) depends on a runnable scaffold with a wired database.

**Approach:** Cold-start the structural seed from the architecture spine: Go module with a chi composition root in `cmd/server/`, module boundaries under `internal/`, a Vite/React/TS SPA under `web/`, one shared golang-migrate migration set, a podman compose PostgreSQL 18 dev database, and a root `justfile` as the single command interface — ending with a live health check proving the DB connection.

## Boundaries & Constraints

**Always:**
- Follow the structural seed layout (`cmd/server/`, `internal/{user,tools,admin,platform}/`, `web/`, `migrations/`, `deploy/`, `infra/`, `docs/`, root `justfile`); every directory ships with a documented purpose.
- `cmd/server` is the composition root wiring module hexagons + adapters; no business logic in handlers/adapters/repositories (AD-1).
- Pin stack versions: Go 1.27, chi v5.3.2+ (GO-2026-4316 open-redirect), sqlc v1.31.1, pgx v5, golang-migrate v4.19.1, PostgreSQL 18.
- One shared golang-migrate migration set (AD-3/AD-11); each table owned by exactly one module; each later story adds its own incremental migration — do NOT pre-create future stories' tables now.
- Use **podman** (not docker) for all container operations.
- Root `justfile` is the only command entry point — no DB/container commands duplicated outside its recipes. Preserve existing docs-* recipes.
- Error responses use the uniform envelope `{"error":{"code","message","details?"}}` with matching HTTP status.
- Cold-start seed creates only the tables the seeder needs and seeds the four base roles (`helfende`, `schirrmeister`, `fuehrende`, `admin`) plus the two pre-seeded admin accounts; seeded admin credentials are delivered out-of-band (env, never in VCS — FR-27/AD-13).
- UUID v7 primary keys, UTC RFC 3339 timestamps.

**Ask First:**
- Go toolchain: stack pins Go 1.27 but the local toolchain is 1.26.1. Default is `go 1.27.0` in go.mod with `GOTOOLCHAIN=auto` (fetches toolchain once; network is available). If the user prefers no toolchain download, fall back to pinning go.mod to the installed 1.26.1 with the same code.
- Module path: assume `github.com/saskia-peters/gear` unless the user specifies another.

**Never:**
- No docker usage or docker-specific config.
- No business logic in adapters/handlers/repositories (AD-1).
- No auth features yet (login, MFA, lockout, registration flows are stories 1.3–1.10); Story 1.1 only scaffolds structure, DB, seed, health check.
- No secrets or admin credentials committed to the repository.
- No composite schedule engine, no email sending, no inspection logic.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| HAPPY_PATH | fresh checkout, empty volumes | `just db-up` starts postgres:18 and applies migrations; `just dev` serves API + SPA; `GET /healthz` returns 200 confirming DB live | n/a |
| IDEMPOTENT | `just db-up` twice | second run exits 0 with container already running, no error | n/a |
| DB_DOWN | API running, DB container stopped | `GET /healthz` returns 503 with the error envelope (does not hang) | structured log; non-2xx response |
| MIGRATION_RERUN | migrate down then up on same volume | schema rebuilds cleanly to the same state | migrate errors surface with non-zero exit |
| NO_RUNTIME | podman unavailable | `just db-up` fails with an actionable message naming the requirement | non-zero exit + clear hint |

</frozen-after-approval>

## Code Map

Only the docs site exists today; no application code to search. Read-only references the implementation agent should consult:

- `_bmad-output/implementation-artifacts/epic-1-context.md` — distilled Epic 1 constraints: stack, AD-1/2/3/6/11/12/13, seed scope, permission/role model. Primary context.
- `_bmad-output/planning-artifacts/architecture/architecture-app-ovsingen-2026-08-30/ARCHITECTURE-SPINE.md` — "Structural Seed", "Stack", "Database Artifacts" (User-owned tables + columns), AD-1/AD-11/AD-12/AD-13, base permission series + role matrix. Source of truth for the cold-start schema and seed.
- `docs/docs/planning/architecture-spine.md` — docs-site copy of the spine (identical content).
- `justfile` (existing, docs-only) — extend with app lifecycle recipes; preserve `docs-*`.
- `docs/` — existing Docusaurus site; leave functional.

## Tasks & Acceptance

**Execution:**
- [x] `go.mod` — init Go module `github.com/saskia-peters/gear`; pin chi, pgx, sqlc, golang-migrate per stack — foundation for all builds
- [x] `cmd/server/main.go` + config — composition root wiring pgx pool, chi router, module adapters, health handler — AD-1 seed
- [x] `internal/{user,tools,admin,platform}/` — package boundaries with ports/adapters layout and a README documenting purpose — AD-1/AD-2 seed
- [x] `internal/platform/httpapi/` — uniform error envelope type `{"error":{"code","message","details?"}}` and response helpers — JSON error contract AC
- [x] `web/` — Vite + React + TypeScript scaffold rendering a minimal shell — so `just dev` serves the SPA
- [x] `migrations/` — golang-migrate versioned up/down pair for the cold-start schema (identity tables the seeder needs + `admin.recovery.approve` permission) + seed of 4 base roles and 2 admin accounts — AD-11/AD-12/AD-13
- [x] `sqlc.yaml` — wire schema (`migrations/`) to per-module store generation; `just sqlc-generate` recipe — establishes the query toolchain for later stories
- [x] `compose.yaml` — postgres:18 service (named volume, healthcheck, port) runnable via podman compose — `just db-up` deliverable
- [x] `justfile` — add `db-up`, `db-down`, `migrate-up`, `migrate-down`, `dev`, `build`, `test`, `vet`, `lint`, `sqlc-generate`; preserve `docs-*` — single command entry point
- [x] `deploy/` + `infra/` — directories with README documenting intended purpose (contents land in later stories) — seed completeness
- [x] health endpoint — `GET /healthz` pings the pool and returns 200/503 per I/O matrix; unit tests cover up/down — I/O matrix + health AC

**Acceptance Criteria:**
- Given an empty repo, when the scaffold is created per the structural seed, then all seed directories exist with documented purpose and `cmd/server` wires module hexagons + adapters with no business logic in adapters/handlers/repositories (AD-1).
- Given the initialized Go module, when `go build ./...`, `go vet ./...`, and `go test ./...` run, then all pass with no errors and chi is pinned to v5.3.2 or higher.
- Given PostgreSQL 18 via podman compose, when `just db-up` runs on a fresh volume, then the postgres:18 container starts, golang-migrate applies the cold-start migration, a pgx connection pool is established at `cmd/server` and exposed to module repositories through adapter ports, and the seeder creates the four base roles and two admin accounts (credentials out-of-band, never in VCS) (AD-12/AD-13).
- Given any handler error, when a response is returned, then the body uses the uniform envelope `{"error":{"code","message","details?"}}` with the matching HTTP status.
- Given the root justfile, when `just dev` runs the full stack (API + SPA + DB), then the API health check confirms a live database connection and no database command is duplicated outside the justfile recipes.

### Review Findings

- [x] [Review][Patch] justfile `dev` — `vite_pid=$$!` runs in a new shell (just runs each recipe line in a separate shell), so the PID is empty; the `kill -0` guard and the EXIT trap never target Vite (and the recipe errors under `set -u`). Capture the PID on the same line as the backgrounded `npm` (or use a backslash continuation). [justfile:77-78]
- [x] [Review][Patch] justfile `dev` — no `web/` dependency provisioning; a fresh checkout has no `node_modules`, so Vite exits and the full stack never comes up (HAPPY_PATH / AC5). Guard with `npm ci` when `web/node_modules` is missing. [justfile:76]
- [x] [Review][Patch] middleware `committedWriter` — missing `Unwrap()`; in the mounted chain (Recovery outer, RequestLogger inner) `http.ResponseController` stops at `committedWriter`, silently losing Flush/Hijack/deadline — the very property `statusWriter.Unwrap()` was added to guarantee. Add `Unwrap()` plus a chain-order test (Recovery∘RequestLogger over a deadline-recording writer). [internal/platform/middleware/middleware.go:52]
- [x] [Review][Patch] compose.yaml / deploy README — wording references "docker-compose" and "shared between docker and podman-compose runtimes", contradicting the frozen "No docker usage or docker-specific config" clause; also add a healthcheck `start_period` so a cold first start is not misread as unhealthy. [compose.yaml:1] [deploy/README.md:10]
- [x] [Review][Patch] Trailing-newline hygiene — several committed files end without `\n` (`.gitignore`, `.env.example`, `compose.yaml`, `cmd/server/main.go`, `sqlc.yaml`, `web/src/main.tsx`). [cmd/server/main.go:71]
- [x] [Review][Defer] Env var split — `DATABASE_URL` (justfile/migrate CLI) vs `GEAR_DATABASE_URL` (Go app): a developer overriding only one gets a silent mismatch. Deferred — pre-existing/tool-scoped env contract, defaults are identical; revisit when env/secret management is reworked in the auth stories (1.4+). [justfile:37]

## Spec Change Log

- 2026-09-04 (Story 1.1 implementation): cold-start scaffold delivered and verified — Go module `github.com/saskia-peters/gear` (Go 1.27.0, chi v5.3.2, pgx v5.10, golang-migrate v4.19.1, sqlc v1.31.1), composition root `cmd/server`, module hexagons under `internal/{user,tools,admin,platform}`, Vite/React/TS SPA in `web/`, migrations `000001_cold_start`, root `compose.yaml`, root `justfile` app recipes (docs-* preserved). Live `just dev` check: `/healthz` 200 → DB stopped → 503 envelope → DB restarted → 200. All Tasks & Acceptance checkboxes marked.

- 2026-09-04 (Story 1.1 review loop, no spec change — `patch` findings): the frozen `<frozen-after-approval>` contract was NOT amended; all fixes were code-level and sat inside the "uniform envelope on any handler error" and "DB_DOWN must not hang" acceptance intent. What changed: (1) added envelope-aware router NotFound (404 `not_found`), MethodNotAllowed (405 `method_not_allowed`), and panic-Recovery (500 `internal_error`) via new `internal/platform/router` package — so the uniform `{"error":{...}}` contract holds for router-level errors too (known-bad state avoided: plain-text 404/405/panic bodies violating the envelope AC); (2) `statusWriter.Unwrap()` so `http.ResponseController` keeps working through the log wrapper (known-bad: silent loss of Flush/Hijack/deadline); (3) health probe no longer leaks raw DB driver error text into `details` and logs probe failures at WARN (known-bad: DSN fragments/cause leaking to unauthenticated callers, error-level spam); (4) sqlc `schema` switched to `./migrations/*.up.sql` glob (known-bad: every future migration requiring a hand edit → generated stores desyncing); (5) **deleted dead `internal/platform/migrate` package** and dropped golang-migrate from go.mod (known-bad: dead code with tests that passed before reaching code under test, plus a `pgx5://`-only scheme conflicting with the default `postgres://` DSN — DB commands stay justfile-only per the single-command-entry intent); (6) router-level tests asserting /healthz mount + JSON 404/405; (7) `logger.New` refactored to accept an io.Writer and real-boundary test asserts structured JSON (known-bad: NFR-O1 JSON-logging contract unobservable); (8) no-hang health test with a blocking pinger pinning the 2s probe timeout (known-bad: DB_DOWN "must not hang" regressing to a hanging probe); (9) WriteJSON encode-error best-effort logging instead of corrupting committed responses; (10) `just dev` aborts if Vite fails to start (known-bad: silent half-up stack); (11) db-wait budget raised to 60s aligned with compose healthcheck, `podman-check` verifies `podman compose version`; (12) added root `.env.example` + `!.env.example` gitignore (documented GEAR_* config surface). KEEP: uniform-envelope-everywhere, eval that health probes must not leak internals, migrations driven solely by the justfile — these must survive any future re-derivation.

- 2026-09-05 (Story 1.1 code review fixes): addressed all 5 patch review findings: (1) fixed `just dev` Vite PID capture and trap by running in a single shell via `\` continuation and single `$!` syntax (known-bad: broken PID guard and `set -u` shell error); (2) added `web-deps` recipe (`npm --prefix web ci` if `web/node_modules` is missing) and wired into `just dev` (satisfies fresh-checkout AC5/HAPPY_PATH); (3) implemented `Unwrap()` on `committedWriter` in `internal/platform/middleware` and added `TestResponseControllerUnwrapsFullMiddlewareChain` testing `Recovery∘RequestLogger` order; (4) cleaned docker references in `compose.yaml` header and `deploy/README.md`, added `start_period: 20s` to healthcheck; (5) fixed trailing newlines across all text/source files. Full verification (`just build`, `just vet`, `just test`, `just lint`, and live `just dev` healthz 200 + Vite 200) verified clean.

## Design Notes

- **Container runtime:** podman-compose 1.0.6 is installed (`podman compose`); docker is absent. `compose.yaml` at repo root; justfile recipes invoke `podman compose`.
- **CLI tools not on PATH:** sqlc and golang-migrate are not installed globally. Pin them via `go run <module>@<version>` inside justfile recipes so no global install is required and versions stay locked.
- **Health check:** `/healthz` pings the pgx pool. Even at this stage it must return the uniform envelope on failure (503), so the JSON contract is proven end-to-end before any auth handler exists.

## Verification

**Commands:**
- `just db-up` -- expected: container running, migrations applied, exit 0; re-run exits 0 (idempotent)
- `just migrate-down && just migrate-up` -- expected: schema rebuilds cleanly, exit 0
- `just build` and `just vet` (or `go build ./...`, `go vet ./...`) -- expected: no errors
- `just test` (or `go test ./...`) -- expected: all pass, including health up/down cases
- `just dev`, then `curl -i localhost:<api-port>/healthz` -- expected: 200; with DB stopped: 503 with error envelope
- `just` -- expected: recipe list includes app + preserved `docs-*` recipes

**Manual checks (if no CLI):**
- Inspect `migrations/` up/down pair matches the architecture spine's User-owned seed tables and AD-12 role matrix.

## Suggested Review Order

**Composition root & routing**

- adapter wiring and the health route all hang off one chi router
  [`main.go:45`](../../cmd/server/main.go#L45)

- envelope-aware 404/405/panic handling replaces chi's plain text
  [`router.go:23`](../../internal/platform/router/router.go#L23)

**Uniform error envelope**

- single WriteJSON path every handler must use
  [`httpapi.go:33`](../../internal/platform/httpapi/httpapi.go#L33)

- router-level 404 now returns the JSON envelope
  [`httpapi.go:56`](../../internal/platform/httpapi/httpapi.go#L56)

- middleware recovery returns the 500 envelope, not plain text
  [`middleware.go:71`](../../internal/platform/middleware/middleware.go#L71)

- statusWriter unwraps so `http.ResponseController` keeps working
  [`middleware.go:28`](../../internal/platform/middleware/middleware.go#L28)

**Health probe (no hang, no leak)**

- a 2s probe timeout guarantees DB_DOWN never hangs the request
  [`health.go:24`](../../internal/platform/health/health.go#L24)

- probe failures log at WARN and never leak driver error text
  [`health.go:33`](../../internal/platform/health/health.go#L33)

**Schema and cold-start seed**

- User-owned identity + permission tables, UUID v7, UTC timestamps, state CHECK
  [`000001_cold_start.up.sql:12`](../../migrations/000001_cold_start.up.sql#L12)

- additive seed: `admin.recovery.approve` plus the four base roles (AD-12)
  [`000001_cold_start.up.sql:74`](../../migrations/000001_cold_start.up.sql#L74)

- two dual-admin bootstrap accounts, credentials out-of-band (FR-27/AD-13)
  [`000001_cold_start.up.sql:94`](../../migrations/000001_cold_start.up.sql#L94)

**Query tooling**

- sqlc schema globs all forward migrations, not a hardcoded file
  [`sqlc.yaml:10`](../../sqlc.yaml#L10)

**Single command interface (justfile)**

- guard verifies podman and the compose plugin exist
  [`justfile:47`](../../justfile#L47)

- db-wait budget aligned with the compose healthcheck
  [`justfile:55`](../../justfile#L55)

- `dev` fails fast if Vite dies instead of running a half-up stack
  [`justfile:76`](../../justfile#L76)

**Web shell**

- minimal Vite + React + TS dev shell served on :5173
  [`vite.config.ts:4`](../../web/vite.config.ts#L4)

**Tests (supporting)**

- real router wiring: healthz plus JSON 404/405 cases
  [`router_test.go:41`](../../internal/platform/router/router_test.go#L41)

- blocking-pinger pins the DB_DOWN no-hang contract
  [`health_test.go:79`](../../internal/platform/health/health_test.go#L79)

- structured JSON asserted at the real `logger.New` boundary
  [`logger_test.go:30`](../../internal/platform/logger/logger_test.go#L30)

- ResponseController unwrap passthrough pinned
  [`middleware_test.go:79`](../../internal/platform/middleware/middleware_test.go#L79)
