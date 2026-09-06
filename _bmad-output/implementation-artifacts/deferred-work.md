# Deferred Work Ledger

Triage output of review loops — real, non-story-blocking findings that are not caused by (or are deliberately out of scope for) the current story. Each entry records why it is real and where it should be picked up. This file is append-only; do not modify or delete existing entries.

- source_spec: `_bmad-output/implementation-artifacts/spec-1-1-project-scaffold-database-foundation.md`
  summary: SPA (`web/`) cannot call the Go API in `just dev` — no Vite dev proxy and no CORS middleware, so browser fetches between :5173 and :8080 are cross-origin blocked.
  evidence: Story 1.1 only requires API+SPA+DB all run and `/healthz` proves DB liveness; no in-SPA consumer action exists yet (Spend: defer directly from the Story 1.1 review; the first real cross-origin call arrives with Story 1.2 dashboard mount / 1.3 registration).
  recommended: add `server.proxy` (e.g. `/api` → `http://localhost:8080`) in `web/vite.config.ts` when the first SPA→API call ships.
- source_spec: `_bmad-output/implementation-artifacts/spec-1-1-project-scaffold-database-foundation.md`
  summary: `web/` has no lint/test gate — `just lint`/`just test` only cover Go; no ESLint configuration, no npm lint/test scripts, and the TypeScript layer is not part of the single-command quality gates.
  evidence: Story 1.1 ships `web/` as a minimal Vite+React+TS shell with no business logic; the frontend lint/test pipeline belongs with the first real frontend story (1.2 dashboard foundation).
  recommended: introduce ESLint + `npm run lint`/`test` and wire into `justfile` gates alongside the Go gates when frontend work begins.
- source_spec: `_bmad-output/implementation-artifacts/spec-1-1-project-scaffold-database-foundation.md`
  summary: `updated_at` columns are never maintained — tables default `now()` but there is no `set_updated_at()` trigger and no app-layer obligation, so `updated_at` stays frozen at insert time.
  evidence: Story 1.1 performs no UPDATEs (seed is insert-only); the first row-updating story (1.3 self-registration creates accounts; admin approval later flips state) introduces the maintenance mechanism.
  recommended: add a `set_updated_at` trigger (or document app-layer obligation) in the first story that writes to `users`.
- source_spec: `_bmad-output/implementation-artifacts/spec-1-1-project-scaffold-database-foundation.md`
  summary: No production serving path for the SPA — the Go binary never serves `web/dist`; only Vite dev serves the frontend.
  evidence: Story 1.1 AC requires `just dev` (dev-time Vite) serve the SPA; production serving is deploy-epic scope.
  recommended: build+Serve `web/dist` from the Go binary (or a static host) in the deploy/infra story; validate before release.
- source_spec: `_bmad-output/implementation-artifacts/spec-1-1-project-scaffold-database-foundation.md`
  summary: The cold-start migration seed rows (4 base roles, 2 admin accounts, `admin.recovery.approve`) are not asserted by any automated test — only by the spec's manual-check bullet and live `just migrate-up/down` verification.
  evidence: A DB-backed test would pin the seed contents against AD-12/AD-13 but requires a running podman PostgreSQL at test time (would couple `go test ./...` to a live DB).
  recommended: add a `//go:build integration` seeded-rows test (run the migration, assert role/permission/admin rows) gated behind a `just test-integration` recipe when the CI/deploy story lands.
- source_spec: `_bmad-output/implementation-artifacts/spec-1-1-project-scaffold-database-foundation.md`
  summary: The composition root (`cmd/server/main.go`) — /healthz mount, pool-error exit, graceful-shutdown — has zero automated coverage.
  evidence: wired routing is now pinned by `internal/platform/router` tests; remaining main.go paths (config→pool→shutdown) are verified only by manual `just dev` + curl in the spec's Verification.
  recommended: extract remaining composition-root seams into testable handlers/constructors, or cover with an integration test, in the CI/integration story.
- source_spec: `_bmad-output/implementation-artifacts/spec-1-1-project-scaffold-database-foundation.md`
  summary: No Go CI pipeline — `.github/workflows/` ships only `docs.yml`; `go build/vet/test/lint` run only when a human invokes the justfile.
  evidence: Story 1.1 AC does not require CI; pipelines belong to the deploy/ops epic (OpenTofu/Cloud Run are already planned).
  recommended: add a `ci.yml` running `just build`, `just vet`, `just test`, `just lint` (+ docs build) in the deploy/CI story.

## Deferred from: code review (2026-09-05) of spec-1-1-project-scaffold-database-foundation.md

- Env var split: `DATABASE_URL` (justfile / golang-migrate CLI) vs `GEAR_DATABASE_URL` (Go server). Defaults are identical (`postgres://gear:gear@localhost:5432/gear?sslmode=disable`) and each tool reads only its own contract, so a developer overriding just one variable gets a silent mismatch between what the migrate CLI and the app connect to. Real DX footgun for later stories that touch env/secret management (e.g. 1.4 auth, integration DB spins). Revisit with a documented alias or single-source env resolution when that work lands.

## Deferred from: code review (2026-09-05) of spec-1-4-email-password-authentication.md

- Sliding idle session (NFR-S2): `expires_at` is set to `issue-time + idle` and never refreshed on activity, so a continuously-active user is still logged out after the idle window. True sliding-idle would need a `last_seen`/activity column and an update per request. Acceptable for V1 (absolute expiry satisfies the security intent); revisit if "idle" sliding semantics are required later.
- Expired-session cleanup: expired `sessions` rows are rejected at read time but never purged (no GC/sweeper job), so the table grows with stale rows. Add a background sweeper (or delete-on-read) in a later ops/story; harmless functionally.
- Audit log on login: no event records who logged in, when, or from where (no IP/user-agent captured or stored with the session). Required for NFR-O1/NFR-O2 audit trail and useful to Story 1.5 lockout / 1.6 MFA; add an audit-log entry and session metadata when the audit/DSGVO story lands.
- Token transport (`localStorage`): `gear.session_token` is stored in `localStorage`, readable by any JS and XSS-exfiltratable; there is no cookie/secure-storage alternative decision documented. Acceptable for V1 (Authorization header mitigates CSRF); revisit token transport when the SPA security posture matures.
- Admin bootstrap credentials: the two seeded admin accounts have no `password_hash` (predates Story 1.3's migration), so they cannot log in until a future story seeds/bootstrap their passwords. The admin gateway path is covered by integration tests that seed a hash manually; real bootstrap is a later story.
- Redirect away from `/login` when authenticated: a user with a valid token visiting `/login` still sees the login form. Minor UX gap; add an "already authenticated → redirect to dashboard" guard when a shared auth context/state is introduced (Story 1.11 auth-ux foundation).
- `sessions.user_id ON DELETE CASCADE`: deleting a user silently destroys all their sessions, which may be undesirable for audit if session history should be retained. Acceptable while sessions are transient; revisit if session/audit retention becomes a requirement.

## Deferred from: user decision (2026-09-05)

- Human verification (CAPTCHA) on register + login — **future enhancement (V2+).** The register and login flows should add a "confirm I am a human" check (e.g. Cloudflare Turnstile, Google reCAPTCHA, or hCaptcha) to slow automated abuse. Recorded in the PRD §6.2 as out of scope for V1. Provider and server-side secret-key handling to be decided when it is scheduled; it was explicitly deferred rather than implemented in Story 1.5 (progressive login lockout).

## Resolved during Epic 1 (Epic 1 retrospective, 2026-09-06)

- **RESOLVED — Vite dev proxy / SPA↔API cross-origin.** The earlier deferral ("SPA cannot call the Go API in `just dev` — no Vite dev proxy") shipped in Story 1.3: `web/vite.config.ts` defines `server.proxy` for `/api` → `http://localhost:8080`. Proven live by the Epic 1 retrospective behavior check (register/login/profile/logout exercised through the proxy). The original entry above remains for provenance; this supersedes it.
- **RESOLVED — web lint/test gate.** The earlier deferral ("`web/` has no lint/test gate — `just lint`/`just test` only cover Go") shipped in Story 1.2/1.3: `web/` gained ESLint + vitest, and the root `justfile` `test`/`lint` recipes now run `npm --prefix web run test` and `npm --prefix web run lint` alongside the Go gates. The original entry above remains for provenance; this supersedes it.

## Process convention (2026-09-06)

- **Avoid god classes.** Standing convention: keep files small and focused; split large handler/service/repository/test files proactively instead of growing them across stories. Motivating evidence: Epic 1 god-class finding (`handler_test.go` 2915, `repository_test.go` 1337, `service_test.go` 847, `handler.go` 892 lines). Applies to all new code from Epic 2 onward; later epic retros re-check file sizes against this.
