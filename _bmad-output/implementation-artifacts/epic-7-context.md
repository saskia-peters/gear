# Epic 7 Context: Hardening & Deploy

<!-- Compiled from planning artifacts. Edit freely. Regenerate with compile-epic-context if planning docs change. -->

## Goal

G.E.A.R.'s functional surface is complete (account + auth, permissions, config/compliance, catalogue, inspection, dashboard/reporting). This epic makes the application **releasable and dependable**: it closes the operational gaps earlier stories deliberately deferred — a real CI gate, a production serving path for the SPA, automated seed/integration verification, parallel-safe test isolation, at-most-once hardening for the two append-only write paths, and the actual deployment with TLS, managed secrets, and a tested backup/restore procedure. This is the **deploy epic/site** the architecture spine and PRD defer to (NFR-R1–R4, NFR-S1/S4, NFR-M2/M3).

## Stories

- Story 7.1: CI Pipeline
- Story 7.2: Production Serving
- Story 7.3: Integration & Seed Assertions
- Story 7.4: Parallel-DB Test Isolation
- Story 7.5: Idempotency Hardening
- Story 7.6: Deployment — Staging + Production
- Story 7.7: Backup & Restore Procedure

## Requirements & Constraints

- **NFR-M2 (Test Coverage):** all functional requirements have automated tests; the CI pipeline FAILS on test failure. The DB-backed suites must run against a real PostgreSQL in CI (the default skip-without-DB convention must not silently skip).
- **NFR-M3 (Linting):** all code passes lint as part of CI; PRs failing lint are not merged. A dependency audit step gates the pipeline.
- **NFR-R1 (Containerization):** backend + frontend + database deployable as Docker containers via a single compose configuration. The Go binary serves the SPA (`web/dist`), so no Vite process is needed at runtime.
- **NFR-R2 (Migrations):** versioned, incremental migration files; rollback procedures documented. The cold-start seed (4 base roles, 2 admin accounts, permission matrix) must be asserted by an automated DB-backed integration test.
- **NFR-R3 (Backup & Restore):** automated backups to ≥1 configurable destination (already admin-configurable via FR-29/AD-15); a documented and TESTED restore procedure exists from initial deployment; failures logged, never silent.
- **NFR-R4 (Availability):** 99% monthly target for the deployed instance.
- **NFR-S1 (TLS):** TLS 1.2+ enforced; plain HTTP rejected or redirected.
- **NFR-S4 (Secrets):** no secrets in VCS; injected via env/secret-manager at runtime.
- **AD-13 (Dual-admin bootstrap):** exactly two `admin` accounts seeded at deployment; credentials out-of-band; the migration + bootstrap procedure is documented and executable.
- **Test isolation:** cross-package DB contention (admin/tools/cmd suites sharing `Test-%`/`test-%` rows and the shared `tools_inventory_number_seq`) must be resolved so default parallel `go test ./...` is trustworthy — per-suite test schemas, unique-row/sequence namespacing, or equivalent.
- **Idempotency:** `POST /api/v1/tools/{id}/inspection` and `POST /api/v1/tools/{id}/reinstatement` must be at-most-once via a client-supplied idempotency key (append-only rows have no unique guard today).

## Technical Decisions

- **Firm spine choices (already made, not re-litigated here):** all dev/deploy commands go through the single `justfile` (just); cloud resources provisioned by **OpenTofu** IaC; **testing/staging platform is Google Cloud Run** with `min_instance_count = 0` (scale to zero when idle), plus Artifact Registry and a managed Postgres tier; **production is the Ortsverband's self-hosted single host** (compose) per earlier agreement. **Candidate hosting providers** (deferred to this epic): Hetzner, IONOS, or T-Cloud Public. **Edge/CDN:** Cloudflare or bunny.net in front for TLS termination, proxying, caching, DDoS mitigation.
- **Containerization:** Docker / docker-compose for deployed targets; local dev runs the multi-service stack with Podman / podman-compose (same Compose definition); the Compose file is the single source for both runtimes.
- **Serving:** the production binary serves the SPA from `web/dist` (embedded or bundled in the image); API paths (`/api/*`) stay Go-handler routed; unknown non-API routes fall back to the SPA for client-side routing.
- **Backups:** destinations are the admin-configurable `backup_destinations` (FR-29/AD-15); this epic adds the automated job + documented/tested restore procedure, not new destination config.
- **Idempotency:** a client-supplied key column (unique) on the inspection and reinstatement tables (or an equivalent idempotency-key store), with the SPA generating + sending the key per attempt.

## UX & Interaction Patterns

- Minimal new UX: the idempotency keys are transport-level (SPA generates + retries transparently). The deploy work is operator-facing (runbooks, env templates, compose files). No new end-user screens unless a small "system status" affordance is added later.

## Cross-Story Dependencies

- 7.2 (production serving) depends on the existing `web/dist` build pipeline and the `cmd/server` HTTP routing; it is independent of 7.6 but is a prerequisite for a useful deploy.
- 7.1 (CI) must run 7.3 (integration/seed) with a PostgreSQL service container; 7.4 (parallel isolation) makes the CI gate deterministic.
- 7.6 (deployment) requires 7.2 (serving) and the documented migration + dual-admin bootstrap (AD-13); 7.7 (backup/restore) is deploy-critical (NFR-R3) and tested from initial deployment.
- 7.5 (idempotency) touches the inspection/reinstatement write paths from Epics 5/6 and is independent of the deploy stories.
- The permission-gating centralization (surface→code registry) is a separate documented future enhancement, deliberately NOT part of this epic.