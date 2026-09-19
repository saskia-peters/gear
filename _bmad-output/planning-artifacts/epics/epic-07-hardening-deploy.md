# Epic 7: Hardening & Deploy

G.E.A.R. ships its features; this epic makes them **releasable and dependable**. The functional surface (account + auth, permissions, config/compliance, catalogue, inspection, dashboard/reporting) is complete — this epic closes the operational gaps the earlier stories deliberately deferred: a real CI gate, a production serving path for the SPA, automated seed/integration verification, test isolation so the full suite runs in parallel, at-most-once hardening for the two append-only write paths, and the actual deployment (staging on Cloud Run, production self-hosted, with TLS, secrets, and backup/restore). It is the **deploy epic/site** the architecture spine and PRD defer to (NFR-R1–R4, NFR-S1/S4, NFR-M2/M3).

**Stories:**
- Story 7.1: CI Pipeline (NFR-M2/M3)
- Story 7.2: Production Serving (SPA + Go binary, NFR-R1)
- Story 7.3: Integration & Seed Assertions (`just test-integration`)
- Story 7.4: Parallel-DB Test Isolation
- Story 7.5: Idempotency Hardening (inspection submit + reinstatement)
- Story 7.6: Deployment — Staging (Cloud Run) + Production (Self-Hosted Compose)
- Story 7.7: Backup & Restore Procedure (NFR-R3)

### Story 7.1: CI Pipeline

As a maintainer,
I want every push and pull request to run the full quality gate automatically,
So that a regression or lint failure is caught before it lands (NFR-M2/M3).

**Acceptance Criteria:**

**Given** a push or pull request to the repository,
**When** CI runs,
**Then** `.github/workflows/ci.yml` executes `just build`, `just vet`, `just test`, `just lint` and the docs build
**And** the pipeline fails on any test failure or lint issue (NFR-M2/NFR-M3).

**Given** the postgres-backed suites,
**When** CI runs,
**Then** a PostgreSQL service container is provided so the DB-backed tests run for real (the default `go test` skip-without-DB convention must not silently skip in CI).

**Given** a dependency or security issue,
**When** CI runs,
**Then** a dependency audit step runs and fails the pipeline on findings (NFR-M2).

### Story 7.2: Production Serving

As an operator,
I want the deployed Go binary to serve the SPA,
So that production has one serving process instead of relying on Vite dev (NFR-R1).

**Acceptance Criteria:**

**Given** a built `web/dist`,
**When** the Go server starts,
**Then** it serves the SPA at `/` and static assets from the embedded/bundled `web/dist`
**And** unknown non-API routes fall back to the SPA (client-side routing keeps working),
**And** `/api/*` still routes to the Go handlers (no SPA fallback on API paths).

**Given** a container build,
**When** the image is built,
**Then** `web/dist` is produced and embedded into the binary (or served from the image) so no Vite process is needed at runtime.

### Story 7.3: Integration & Seed Assertions

As a maintainer,
I want the cold-start seed (base roles, permissions, dual admin accounts) asserted by an automated DB-backed test,
So that a broken seed is caught in CI, not on a fresh deployment (NFR-R2, AD-12/AD-13).

**Acceptance Criteria:**

**Given** the migration set,
**When** a `//go:build integration` test runs against a real (migration-completed) database,
**Then** it asserts the 4 base roles, the full permission codes, and the 2 seeded admin accounts exist as seeded
**And** the role→permission matrix matches the documented base series (AD-12).

**Given** the `justfile`,
**When** a maintainer runs `just test-integration`,
**Then** it spins up the database (if needed), applies migrations, and runs the integration-tagged tests.

### Story 7.4: Parallel-DB Test Isolation

As a maintainer,
I want `go test ./...` to run in parallel without cross-suite interference,
So that the default Go test mode (not just `-p 1`) is a trustworthy gate.

**Acceptance Criteria:**

**Given** the current cross-package DB contention (admin/tools/cmd suites sharing `Test-%`/`test-%` rows + the shared `tools_inventory_number_seq`),
**When** the suites run in default parallel mode,
**Then** they are isolated (per-suite test schemas, unique-row/sequence namespacing, or equivalent)
**And** the previously flaky tests (`TestComposedAdminRouteGroup`, `TestPostgresToolInventoryArchivedBackstop`, `TestPostgresCreateToolInventoryCollisionRetry`, `TestComposedToolCreateAutoAssignsInventory`, `TestPostgresCreateToolsBatchPreservesExplicitOrder`) pass deterministically in parallel.

### Story 7.5: Idempotency Hardening

As an operator,
I want the two append-only write paths (inspection submit, reinstatement) to be at-most-once,
So that a client retry after a timeout/500 cannot persist duplicate records (deferred-work idempotency items).

**Acceptance Criteria:**

**Given** a client-supplied idempotency key on `POST /api/v1/tools/{id}/inspection`,
**When** the same key is replayed,
**Then** the original record is returned (or the retry rejected) and NO second record is persisted.

**Given** a client-supplied idempotency key on `POST /api/v1/tools/{id}/reinstatement`,
**When** the same key is replayed,
**Then** no duplicate reinstatement row is created.

### Story 7.6: Deployment — Staging (Cloud Run) + Production (Self-Hosted Compose)

As an operator,
I want the application deployed to staging (Cloud Run) and production (self-hosted single host, compose),
So that the Ortsverband's instance runs with TLS, managed secrets, and the documented env split (NFR-S1/S4, NFR-R1).

**Acceptance Criteria:**

**Given** the OpenTofu modules under `infra/`,
**When** staging is provisioned,
**Then** Cloud Run serves the container (scaled to zero when idle), with a managed Postgres tier and Artifact Registry image (spine's firm choices)
**And** TLS 1.2+ is enforced (plain HTTP rejected/redirected, NFR-S1).

**Given** production is the Ortsverband's self-hosted single host,
**When** it is deployed,
**Then** a single `docker-compose` (or `podman-compose`) definition runs the app + database
**And** secrets are injected via env/secret-manager, never in VCS (NFR-S4)
**And** an edge/CDN layer (Cloudflare or bunny.net) terminates TLS in front (spine's candidate choice).

**Given** the deployed app,
**When** it is released,
**Then** the migration + dual-admin bootstrap procedure is documented and executable (NFR-R2, AD-13).

### Story 7.7: Backup & Restore Procedure

As an operator,
I want automated database backups to the configured destination(s) and a tested restore procedure,
So that the Ortsverband can recover from loss (NFR-R3).

**Acceptance Criteria:**

**Given** the configured backup destinations (already admin-configurable via FR-29/AD-15),
**When** the backup job runs,
**Then** it backs up the database automatically to at least one destination on schedule
**And** a backup failure is logged and surfaced (NFR-O1), never silent.

**Given** a documented restore procedure,
**When** it is followed,
**Then** a backup can be restored and verified (tested from initial deployment, NFR-R3).