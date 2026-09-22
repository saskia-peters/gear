---
title: 'Email & Password Authentication'
type: 'feature'
created: '2026-09-05'
status: 'done'
review_loop_iteration: 0
baseline_commit: '1f1b04b007bff8694b0a4178fd5d14cc4b00fc90'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Registered volunteers with `active` status cannot log in, and the server has no authenticated session mechanism, so no protected route can validate a caller's identity or resolved permission set (AD-2/AD-6).

**Approach:** Implement email+password login with server-side sessions: a `sessions` table (opaque, hashed-at-rest token, idle expiry default 8h, invalidation on logout — NFR-S2/FR-1), a `POST /api/v1/auth/login` endpoint verifying password against the Argon2id hash and account state, a `POST /api/v1/auth/logout` endpoint, a chi auth-gateway middleware that validates the session token and resolves the caller's live permission set (AD-2/AD-6/AD-12), and a German login page at `/login` with anti-enumeration errors (UX-DR7).

## Boundaries & Constraints

**Always:**
- Issue an opaque session token on successful login (FR-1/NFR-S2). The token is cryptographically random; only its SHA-256 hash is stored server-side (defense-in-depth, never leak the raw token).
- Session tokens expire after a configurable idle period (default 8h) and are invalidated server-side on logout (NFR-S2).
- Only `active` accounts can authenticate. `pending_approval` and `deactivated` accounts are rejected without issuing a session (FR-5/FR-21).
- Anti-enumeration on login (UX-DR7): incorrect password, non-existent email, and non-active account all return HTTP 401 with the same German microcopy ("E-Mail oder Passwort ist falsch.") and no account-existence leakage.
- Auth gateway per AD-2/AD-6: protected routes validate the session token and resolve the caller's **live** permission set (additive union of permission-group memberships + direct grants, AD-12), re-derived per request — never client-trusted, never a session-cached snapshot.
- Permission checks return HTTP 403 when the resolved set lacks the required permission code (AD-6/FR-19 existence-hiding).
- Store the session token hash in the User-owned `sessions` table (AD-2/AD-11); sessions are owned by the User module.
- All API errors use the uniform envelope `{"error":{"code","message","details?"}}`.
- Login UI uses DESIGN.md tokens, is accessible (≥48px touch targets), and shows German microcopy.

**Ask First:**
- Session idle duration default: 8h per NFR-S2. Configurable via `GEAR_SESSION_IDLE` env with default `8h`. Default is 8h unless overridden.

**Never:**
- No progressive lockout yet (Story 1.5) — repeated failed logins are not yet rate-limited in this story.
- No MFA/TOTP yet (Story 1.6).
- No password reset / forgot-password yet (Story 1.8).
- No JWT — use opaque server-side sessions stored (hashed) in the `sessions` table (NFR-S2 server-side invalidation).
- Never log, store, or return raw passwords or raw session tokens.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| LOGIN_SUCCESS | active user, correct email+password | HTTP 200 with `{"token":"<opaque>","user":{...}}`; new session row stored with hashed token + expiry; gateway accepts token | n/a |
| LOGIN_WRONG_PASSWORD | active user, wrong password | HTTP 401 anti-enumeration "E-Mail oder Passwort ist falsch." — no session created | 401 invalid_credentials |
| LOGIN_UNKNOWN_EMAIL | non-existent email | HTTP 401 identical to wrong-password (no enumeration) | 401 invalid_credentials |
| LOGIN_PENDING | pending_approval user, correct credentials | HTTP 401 rejected, no session issued (FR-5) | 401 invalid_credentials |
| LOGIN_DEACTIVATED | deactivated user, correct credentials | HTTP 401 rejected, no session issued (FR-21) | 401 invalid_credentials |
| LOGOUT | valid session token | Session row invalidated/deleted server-side; subsequent requests with token rejected | n/a |
| GATEWAY_NO_TOKEN | protected route, no Authorization header | HTTP 401 unauthorized | 401 unauthorized |
| GATEWAY_INVALID_TOKEN | protected route, malformed/unknown token | HTTP 401 unauthorized | 401 unauthorized |
| GATEWAY_EXPIRED | protected route, token past idle expiry | HTTP 401 unauthorized (expiry re-checked against stored expires_at) | 401 unauthorized |
| GATEWAY_FORBIDDEN | authenticated user without required permission | HTTP 403 forbidden (AD-6) | 403 forbidden |

</frozen-after-approval>

## Code Map

- `migrations/000003_auth_sessions.{up,down}.sql` -- Add `sessions` table: id, user_id FK→users, token_hash (unique), expires_at, created_at. User-owned (AD-2/AD-11).
- `internal/user/adapters/postgres/queries.sql` -- sqlc queries: `CreateSession`, `GetSessionByTokenHash`, `DeleteSessionById`, `ListPermissionsByUser`, `CreateUserPermission`. Re-run `just sqlc-generate`.
- `internal/user/core/session.go` -- Session domain model, `SessionManager` with `Issue`, `Validate`, `Invalidate`; idle-expiry logic (NFR-S2).
- `internal/user/core/auth.go` -- Login use-case: verify credentials + account state, resolve permission set (AD-2/AD-6/AD-12).
- `internal/user/adapters/http/handler.go` -- Add `POST /api/v1/auth/login` and `POST /api/v1/auth/logout` handlers.
- `internal/platform/auth/gateway.go` -- chi middleware validating bearer token, resolving live permission set per request, requiring a permission code; returns 401/403 with uniform envelope (AD-6).
- `internal/platform/router/router.go` -- Mount login/logout under `/api/v1/auth`; add a protected example route or hook for gateway demonstration.
- `cmd/server/main.go` -- Wire SessionManager, auth repository, and gateway into composition root.
- `internal/platform/config/config.go` -- Add `SessionIdle` (default 8h) config.
- `web/src/pages/LoginPage.tsx` -- Implement German login form with email/password, inline validation, anti-enumeration error display, token storage.
- `web/src/pages/LoginPage.module.css` -- DESIGN.md token styling for login form.
- `web/src/pages/LoginPage.test.tsx` -- Component tests for login form, error handling, and successful token storage.
- `web/src/App.tsx` -- Wire login navigation and protected dashboard route.

## Tasks & Acceptance

**Execution:**
- [x] `migrations/000003_auth_sessions.{up,down}.sql` -- Create `sessions` table (id, user_id FK, token_hash unique, expires_at, created_at) -- server-side session persistence (AD-2/NFR-S2)
- [x] `internal/user/adapters/postgres/queries.sql` -- Add sqlc queries for session create/validate/delete + permission resolution and re-run `just sqlc-generate` -- type-safe session & permission access
- [x] `internal/user/core/session.go` -- Implement SessionManager: opaque token generation, SHA-256 hashing, issue/validate/invalidate with idle expiry -- NFR-S2 session lifecycle
- [x] `internal/user/core/auth.go` -- Implement Login use-case: verify Argon2id password, enforce active state, resolve permission set -- AD-2/AD-6 auth rules
- [x] `internal/user/adapters/http/handler.go` -- Add `POST /api/v1/auth/login` and `POST /api/v1/auth/logout` handlers with uniform envelope and anti-enumeration -- API contract
- [x] `internal/platform/auth/gateway.go` -- Implement chi auth-gateway middleware: validate token, resolve live permission set, enforce permission code (401/403) -- AD-6 server-side authorization
- [x] `internal/platform/router/router.go` & `cmd/server/main.go` -- Wire login/logout routes and auth gateway into composition root -- composition root wiring
- [x] `internal/platform/config/config.go` -- Add `SessionIdle` config with 8h default -- configurable idle period (NFR-S2)
- [x] `internal/user/` -- Write unit/integration tests covering login matrix (success, wrong password, unknown email, pending, deactivated) and gateway (no/invalid/expired/forbidden) -- I/O matrix verification
- [x] `web/src/pages/LoginPage.tsx` -- Implement German login UI with inline validation, anti-enumeration error, token storage -- volunteer login UX
- [x] `web/src/App.tsx` & `web/src/pages/LoginPage.test.tsx` -- Wire `/login` route, protected dashboard, and component tests -- navigable auth flow

**Acceptance Criteria:**
- Given an `active` user with correct credentials, when logging in, then a secure opaque session token is returned (NFR-S2/FR-1) and the gateway accepts it on protected routes.
- Given an incorrect password, non-existent email, or non-active account, when logging in, then HTTP 401 is returned with identical anti-enumeration German microcopy (UX-DR7) and no session is issued (FR-5/FR-21).
- Given a logged-in user, when they log out, then the session is invalidated server-side and subsequent requests with that token are rejected (NFR-S2).

## Spec Change Log

- 2026-09-05 (Story 1.4 implementation & review loop): Implemented email+password authentication with server-side sessions:
  - `migrations/000003_auth_sessions.{up,down}.sql`: `sessions` table (uuidv7 PK, user_id FK→users ON DELETE CASCADE, token_hash unique, expires_at, created_at).
  - `internal/user/core/session.go`: SessionManager issuing opaque crypto/rand tokens, storing only the SHA-256 hash, with idle expiry (default 8h via `GEAR_SESSION_IDLE`, NFR-S2).
  - `internal/user/core/auth.go`: Login/Logout use-cases — Argon2id password verify, `active`-state gating, uniform anti-enumeration 401, live permission resolution before session issue, input length bounds (email ≤254, password ≤1024), no permissions shipped to the client.
  - `internal/user/adapters/http/handler.go`: `POST /api/v1/auth/login` (200 token) and `POST /api/v1/auth/logout` (204, atomic delete-by-token-hash).
  - `internal/platform/auth/gateway.go`: chi `RequirePermission` middleware validating the bearer token, resolving the live permission set per request, returning 401/403 with the uniform envelope (AD-6); `auth.Route` mounts protected demo `/me`.
  - `internal/platform/auth/BearerToken` shared helper used by both gateway and user handler (deduplicated).
  - `internal/platform/config/config.go`: `SessionIdle` via `GEAR_SESSION_IDLE` default `8h`, warning on invalid value.
  - `web/src/pages/LoginPage.tsx`: German login form, inline validation, anti-enumeration 401 error vs distinct server-error message, token stored in localStorage, redirect to dashboard.
  - Verification: `just build/vet/test/lint` clean; 31 web tests + full Go suite incl. composed-flow integration test against live DB; migrations 000001–000003 rebuild cleanly.

## Design Notes

- **Token Storage:** Only the SHA-256 hash of the opaque session token is stored in `sessions.token_hash`. The raw token is returned to the client once and never persisted. The gateway hashes the presented token and looks it up by hash.
- **Idle Expiry:** `expires_at = now() + idle`. On every gateway check the token is validated against the stored `expires_at` (re-derived per request, never a cached snapshot — AD-2).
- **Live Permission Resolution:** The gateway resolves the permission set per request via sqlc (additive union of permission-group memberships + direct grants, AD-12). This keeps revocation immediate (FR-21/FR-22).

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go and web tests pass, 0 lint issues
- `just db-up` -- expected: migrations 000001–000003 apply cleanly
- `just migrate-down && just migrate-up` -- expected: schema rebuilds cleanly
- `curl` login/logout/gateway flows against local server -- expected: 200/401/403 per I/O matrix

## Suggested Review Order

**Server-Side Session Management**

- sessions table storing only the hashed opaque token with idle expiry
  [`000003_auth_sessions.up.sql:9`](../../migrations/000003_auth_sessions.up.sql#L9)

- SessionManager issuing/validating/invalidating opaque tokens (SHA-256 at rest)
  [`session.go:70`](../../internal/user/core/session.go#L70)

**Authentication Use-Case**

- Login enforcing active state, Argon2id verify, uniform anti-enumeration timing
  [`auth.go:82`](../../internal/user/core/auth.go#L82)

- login/logout HTTP handlers with atomic delete-by-token-hash and envelopes
  [`handler.go:72`](../../internal/user/adapters/http/handler.go#L72)

**Authorization Gateway (AD-6)**

- RequirePermission middleware resolving the live permission set per request
  [`gateway.go:47`](../../internal/platform/auth/gateway.go#L47)

- protected demo route and shared bearer-token parser
  [`gateway.go:102`](../../internal/platform/auth/gateway.go#L102)

**Composition Root & Config**

- wiring SessionManager, gateway, and protected mount
  [`main.go:48`](../../cmd/server/main.go#L48)

- configurable session idle period with invalid-value warning
  [`config.go:43`](../../internal/platform/config/config.go#L43)

**Frontend Login UI**

- German login form with anti-enumeration vs server-error handling
  [`LoginPage.tsx:14`](../../web/src/pages/LoginPage.tsx#L14)

**Automated Verification**

- composed real-wiring integration test (401/403/200)
  [`composition_test.go:1`](../../internal/platform/auth/composition_test.go#L1)
