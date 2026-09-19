---
title: 'CI Pipeline (NFR-M2/M3)'
type: 'chore'
created: '2026-09-19'
status: 'done'
review_loop_iteration: 0
baseline_commit: '302ff95a922f8cd708ab33342ecf090b44e2829d'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-7-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The repo has no CI gate for application code — `.github/workflows/` ships only `docs.yml` (a docs-pages deploy), so `go build/vet/test/lint`, the web build/test/lint, and the DB-backed suites run only when a human invokes the justfile. A regression can land silently (NFR-M2/M3).

**Approach:** Add a `ci.yml` GitHub Actions workflow that runs the full quality gate on every push and PR — with a real PostgreSQL service so the DB-backed tests run (instead of skipping), then the Go build/vet/test/lint, the web typecheck/test/lint, a docs build, and a dependency audit — and fails the pipeline on any failure.

## Boundaries & Constraints

**Always:**
- **Trigger:** `ci.yml` runs on `push` to `main`/`feat/*` and on `pull_request` (all branches), plus `workflow_dispatch`. Concurrency-cancelled on superseded runs of the same ref.
- **Postgres service:** the DB-backed suites read `DATABASE_URL` (default `postgres://gear:gear@localhost:5432/gear`) and `t.Skip` when unreachable. CI MUST provide a `postgres:18` service container with `POSTGRES_USER/PASSWORD/DB=gear` and port `5432`, then apply migrations (`just migrate-up`) BEFORE `go test` so the suites run for real — a silent skip in CI counts as a failure of this story.
- **Go gate:** `go build ./cmd/... ./internal/...`, `go vet ./cmd/... ./internal/...`, `go test ./cmd/... ./internal/...` (after migrate), and the pinned golangci-lint (`just lint`). Uses `setup-go` with the repo's `go.mod` version (1.27). Fail on any error.
- **Web gate:** `npm ci` in `web/`, then `npm run typecheck`, `npm run lint`, `npm run test` (vitest). Node 24 (match the existing docs.yml toolchain). Fail on any error.
- **Docs gate:** `npm ci` + `npm run build` in `docs/` (matches docs.yml's build job; do NOT deploy Pages in ci.yml — the existing docs.yml owns deployment).
- **Dependency audit:** a step that fails on known vulnerabilities — e.g. `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` for Go and `npm audit --audit-level=high` for web (best-effort network; a failure to run the audit tool itself should not be silently ignored).
- **Existing docs.yml is preserved** and keeps owning the GitHub-Pages deploy; ci.yml builds docs but never deploys.
- **justfile recipes unchanged** — the workflow invokes the same `just` recipes (`build`, `vet`, `test`, `lint`, `migrate-up`) so CI and local behavior never drift. `just` is installed on the runner (or invoked via `just`/`cargo` — prefer a pinned `casey/just` action or `just install`).

**Ask First:**
- GitHub Actions as the CI provider (already implied by the existing `docs.yml`); switching providers later is out of scope.
- Whether the dependency-audit step is a hard gate or informational — default: hard gate for the Go vulncheck, `npm audit --audit-level=high` fails the pipeline (matching NFR-M2).

**Never:**
- No deploy/publish of the app in this story (CI only; deployment is Story 7.6).
- No changes to the app code, justfile recipes, or test skip conventions (the DB-service provision makes them run in CI).
- No secrets in the workflow file (no registry creds, no deploy keys); `permissions: contents: read` minimum.
- No new dependency that a human must install globally (pinned `go run ...@version` or pinned actions only).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| CI_PUSH | push to a tracked branch | full gate runs | any failure → red |
| CI_PR | pull request opened/updated | full gate runs; no deploy | any failure → required check red |
| CI_DB | postgres service up, migrated | DB-backed suites RUN (not skipped) | skip in CI → treated as failure |
| CI_DB_DOWN | service container fails to start | pipeline fails loudly (service error), never silently skips | job error |
| CI_LINT | Go/web lint | 0 issues | lint failure → red |
| CI_VULN | govulncheck / npm audit finds findings | pipeline fails (hard gate) | findings → red |
| CI_DISPATCH | workflow_dispatch | full gate runs manually | n/a |
| CI_DOCS | docs build | builds (no deploy) | build failure → red |

</frozen-after-approval>

## Code Map

- `.github/workflows/ci.yml` (new) -- the full quality gate: postgres service + migrate + go build/vet/test/lint + web typecheck/lint/test + docs build + dependency audit; `permissions: contents: read`; concurrency cancel-in-progress.
- `.github/workflows/docs.yml` (existing) -- unchanged; keeps the GitHub-Pages deploy. Reference: the setup-node 24 + `npm ci` pattern to mirror in ci.yml.
- `justfile` -- the workflow calls the existing recipes (`just migrate-up`, `just build`, `just vet`, `just test`, `just lint`); do NOT edit them in this story. `MIGRATE_VERSION`/`GOLANGCI_VERSION` pins live here (`justfile:44-46`).
- `go.mod` -- `go 1.27.0`; setup-go reads it. `DATABASE_URL` for the migrate/service step mirrors the justfile default (`postgres://gear:gear@localhost:5432/gear?sslmode=disable`).
- `web/package.json` -- `typecheck`/`lint`/`test` scripts the workflow runs after `npm ci`.
- `docs/package.json` + `docs/package-lock.json` -- docs build inputs (same as docs.yml).

## Tasks & Acceptance

**Execution:**
- [ ] `.github/workflows/ci.yml` (new) -- full pipeline: triggers, concurrency, permissions, postgres service, migrate, go build/vet/test/lint, web typecheck/lint/test, docs build, dependency audit -- CI
- [ ] Verify the workflow locally-equivalent commands pass (just build/vet/test/lint, web typecheck/lint/test, docs build) -- verification

**Acceptance Criteria:**
- Given a push or PR to the repository, when CI runs, then the full quality gate executes (Go + web + docs + audit) and the pipeline fails on any test failure or lint issue (NFR-M2/M3).
- Given the postgres service container, when the DB-backed suites run, then they RUN for real (migrations applied first) — a suite that would skip locally due to no DB does NOT skip in CI.
- Given a dependency or security finding, when the audit step runs, then it fails the pipeline (hard gate, NFR-M2).
- Given a docs change, when CI runs, then the docs build succeeds WITHOUT deploying to Pages (the existing docs.yml remains the only deployer).

## Spec Change Log

- **2026-09-19, execution adaptations (review-adjacent, implementation):** (a) `just migrate-up` depends on `db-wait` → `podman-check`/`podman compose up`, which cannot run in GitHub Actions (no podman; the Postgres is the `services:` container) — the workflow runs the pinned migrate CLI directly against the service DB, with the version read from the justfile (`just --evaluate MIGRATE_VERSION`) so the pin stays single-source. (b) The Go test step runs `go test -p 1` (serial) rather than `just test` (which is parallel): the DB-backed suites share the single service schema and parallel packages contend on shared test rows/sequences (the documented cross-package flake) — `-p 1` keeps CI deterministic until Story 7.4 makes the parallel default safe. (c) The govulncheck gate exposed 8 real vulnerabilities in `golang.org/x/crypto@v0.45.0` / `golang.org/x/text@v0.31.0`; both were upgraded (x/crypto → v0.57.0, x/text → v0.42.0) so the audit gate is green (0 in our code, 1 in a required-but-uncalled module). (d) Review patches: added a CI guard that FAILS on any silent `t.Skip` of the DB-backed suites (the story's core value), pinned govulncheck to `@v1.8.0`, `timeout-minutes: 30`, job-level `DATABASE_URL` (single source), a web **production build** gate, a `go mod tidy` drift check (gofmt intentionally NOT gated repo-wide — ~80 legacy files lack a trailing newline; normalization is a separate housekeeping change), and aligned the stale "in input order" comment on `CreateToolsBatch`. (e) The **docs dependency audit is informational** (not a hard gate): the docs site is a build-time toolchain whose 8 remaining high findings (faker/js-yaml/image-size in dev deps) only clear with `--force` major upgrades that could break the docs build — tracked in deferred-work. KEEP: CI shells out to the justfile recipes for the gates that match the runner, and the web + Go dependency audits stay hard gates.

## Design Notes

- **CI == justfile:** the workflow shells out to the same `just` recipes humans run, so a green CI is exactly what a local `just build && just vet && just test && just lint` yields. The pinned versions (`MIGRATE_VERSION`, `GOLANGCI_VERSION`) already live in the justfile — one place to bump.
- **DB service = makes skips impossible:** the suites `t.Skip` when `pgxpool.New` fails. Providing `postgres:18` + migrate in CI flips them from skipped to run. This is the core value over "just run what we run locally."
- **docs.yml keeps deploying:** splitting "build" (ci.yml) from "deploy" (docs.yml) keeps the existing Pages deploy authoritative and avoids a second deployer racing it.
- **Hard audit gate:** govulncheck + `npm audit --audit-level=high` fail the pipeline. Rationale: a shipped web app with a known-high vuln contradicts NFR-S4/M2; an informational-only audit would be silently ignored.

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` (with a local `postgres:18` up + migrated) -- expected: all pass, DB-backed suites run (no skips)
- `npm --prefix web run typecheck && npm --prefix web run lint && npm --prefix web run test` -- expected: all pass
- `npm --prefix docs run build` -- expected: builds
- `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` -- expected: no findings
- Manual: push the branch / open a PR → the `ci` check appears in GitHub and goes green; the DB-backed suites visibly run (not skipped).

**Manual checks (if no CLI):**
- Inspect `.github/workflows/ci.yml`: no secrets, `permissions: contents: read`, postgres service + migrate step precede `go test`, docs built but not deployed, dependency audit is a hard gate.

## Suggested Review Order

**Entry point — the CI workflow**

- Read first: the full gate — triggers, service container, migrate, Go/web/docs/audit steps, and the silent-skip guard
  [`ci.yml:1`](../../.github/workflows/ci.yml#L1)

**Determinism safeguards**

- The DB-backed suites must RUN, not skip: the postgres service + migrate step + the "Fail on silent DB-suite skips" guard
  [`ci.yml:45`](../../.github/workflows/ci.yml#L45)
- `-p 1` serialization rationale (cross-package DB contention → Story 7.4)
  [`ci.yml:95`](../../.github/workflows/ci.yml#L95)

**Security-critical audit gates**

- Hard Go audit (govulncheck, pinned) + web audit (npm audit)
  [`ci.yml:128`](../../.github/workflows/ci.yml#L128)

**The pre-existing test fix that CI surfaced**

- `CreateToolsBatch` order assertion relaxed to a set assertion (PostgreSQL doesn't guarantee RETURNING order; production attributes by name)
  [`import_test.go:347`](../../internal/tools/adapters/postgres/import_test.go#L347)
- The aligned comment on the store method
  [`tools_repo.go:571`](../../internal/tools/adapters/postgres/tools_repo.go#L571)