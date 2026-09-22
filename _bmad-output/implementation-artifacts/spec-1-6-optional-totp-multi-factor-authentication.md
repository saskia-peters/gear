---
title: 'Optional TOTP Multi-Factor Authentication'
type: 'feature'
created: '2026-09-05'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'dd676f5e6059de21eed8ff186e08fd3803f86bc7'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Accounts are protected only by a password, so a leaked or brute-forced password grants full access. Volunteers cannot enable a second factor.

**Approach:** Implement optional Time-based One-Time Password (TOTP) MFA (FR-4): an enrollment flow that shows a shared secret + QR code and confirms a 6-digit code before enabling, a two-step login (password → TOTP challenge → session) when MFA is enabled, a disable flow that requires a valid current TOTP code, at-rest encryption of the TOTP secret (NFR-S4), structured auth logging (NFR-O1), and accessible German MFA settings + login-step UI (UX-DR6/UX-DR9).

## Boundaries & Constraints

**Always:**
- Use TOTP per RFC 6238 (HMAC-SHA1, 6-digit codes, 30-second period) via the `github.com/pquerna/otp` library.
- Enrollment (FR-4): an authenticated user requests to enable MFA → the server generates a new random secret and returns it plus a QR provisioning URI (`otpauth://totp/...`) for the authenticator app; MFA is NOT enabled until the user confirms a valid current 6-digit code.
- The TOTP secret is stored **encrypted at rest** (NFR-S4) using an app-level key from the environment (`GEAR_ENCRYPTION_KEY`, 32 bytes, hex/base64); the plaintext secret is never transmitted after provisioning and never stored in plaintext or logged.
- When MFA is enabled, login is **two-step**: after password validation the response signals an MFA challenge (no session yet); the client then submits the 6-digit TOTP code, which is validated, and only then is a session issued (FR-4).
- Invalid or expired TOTP codes are rejected with German microcopy; the rejection does not reveal why (UX-DR7) and no session is issued.
- Disabling MFA requires re-authenticating with a valid current TOTP code before removal.
- Every TOTP operation (enroll requested, enroll confirmed, challenge issued, challenge success/failure, disable) emits a structured auth log line (NFR-O1).
- The login UI shows an "MFA aktiv" indicator (UX-DR6) and the MFA challenge step is accessible (UX-DR9).
- All API errors use the uniform envelope `{"error":{"code","message","details?"}}`.

**Ask First:**
- None (library is pinned per architecture spine deferral: TOTP library choice deferred to the User epic; `pquerna/otp` is the standard choice).

**Never:**
- No MFA for pending/deactivated accounts (login already blocks non-active accounts).
- No SMS/email OTP — TOTP via authenticator app only (FR-4).
- Never store the TOTP secret in plaintext, transmit it after provisioning, or log it.
- No recovery codes in this story (not in FR-4; could be a future enhancement).
- No admin-initiated MFA reset (not in scope).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| ENROLL_REQUEST | Authenticated user without MFA requests to enable | Server returns new secret + otpauth:// URI (encrypted at rest only after confirmation); no MFA flag change yet | n/a |
| ENROLL_CONFIRM_VALID | User submits the correct current 6-digit code | MFA enabled (is_mfa_enabled=true, secret persisted encrypted); structured log | n/a |
| ENROLL_CONFIRM_INVALID | User submits wrong/expired 6-digit code | MFA NOT enabled; German error; no flag change | 400 invalid_totp |
| LOGIN_NO_MFA | MFA disabled, valid password | Session issued immediately (unchanged single-step login) | n/a |
| LOGIN_MFA_CHALLENGE | MFA enabled, valid password | HTTP 200 with `mfa_required:true`, no session issued; client must submit TOTP | n/a |
| LOGIN_MFA_VALID | MFA enabled, correct password + valid TOTP | Session issued, 200 with token | n/a |
| LOGIN_MFA_INVALID | MFA enabled, correct password + wrong TOTP | Rejected, no session; German microcopy, no reason revealed | 401 invalid_credentials |
| MFA_DISABLE_VALID | MFA enabled, valid current TOTP | MFA disabled, secret cleared, structured log | n/a |
| MFA_DISABLE_INVALID | MFA enabled, wrong TOTP to disable | MFA stays enabled; German error | 400 invalid_totp |
| MFA_FLAG_DISPLAY | MFA enabled user anywhere in SPA | "MFA aktiv" indicator visible (UX-DR6) | n/a |

</frozen-after-approval>

## Code Map

- `migrations/000005_user_totp.{up,down}.sql` -- Add `totp_secret_encrypted text NULL` column to `users` (encrypted at rest, NFR-S4); `is_mfa_enabled` already exists.
- `go.mod` -- Add `github.com/pquerna/otp`.
- `internal/platform/config/config.go` -- Add `GEAR_ENCRYPTION_KEY` (32-byte hex/base64) config for at-rest secret encryption; error if unset when MFA is used.
- `internal/platform/crypto/secret.go` -- AES-256-GCM encrypt/decrypt helper for the TOTP secret at rest (NFR-S4).
- `internal/user/core/totp.go` -- TOTP domain logic: generate secret + otpauth URI, validate 6-digit code (RFC 6238, pquerna), encrypt/decrypt secret, enable/disable.
- `internal/user/core/auth.go` -- Extend `Login`: after password validation, if `is_mfa_enabled` → return `mfa_required` challenge (no session); add TOTP verification step that issues the session on a valid code.
- `internal/user/adapters/http/handler.go` -- Endpoints: `POST /api/v1/auth/mfa/enroll` (request + confirm), `POST /api/v1/auth/mfa/disable`, extend login to accept a `totp_code` and return `mfa_required`.
- `internal/user/adapters/postgres/queries.sql` -- sqlc queries: `SetUserTotpSecret`, `ClearUserTotpSecret`. Re-run `just sqlc-generate`.
- `internal/user/core/totp_test.go` -- Unit tests: generate/validate code, wrong/expired code, encrypt/decrypt round-trip, enable/disable.
- `internal/user/adapters/http/handler_test.go` -- Handler tests for enroll, confirm, login MFA challenge/success/failure, disable.
- `web/src/pages/LoginPage.tsx` -- Two-step login: if `mfa_required`, show the TOTP input step; show "MFA aktiv" indicator.
- `web/src/pages/MfaPage.tsx` -- MFA settings surface (German): enable (secret + QR), confirm code, disable with current code.
- `web/src/App.tsx` -- Route for MFA settings.

## Tasks & Acceptance

**Execution:**
- [x] `go.mod` -- Add `github.com/pquerna/otp` -- RFC 6238 TOTP implementation
- [x] `migrations/000005_user_totp.{up,down}.sql` -- Add `totp_secret_encrypted` column to `users` -- encrypted-at-rest secret storage (NFR-S4)
- [x] `internal/platform/config/config.go` -- Add `GEAR_ENCRYPTION_KEY` config -- app-level key for at-rest encryption
- [x] `internal/platform/crypto/secret.go` -- AES-256-GCM encrypt/decrypt helper -- secret protection at rest (NFR-S4)
- [x] `internal/user/core/totp.go` -- TOTP generate/validate/enable/disable logic (RFC 6238) -- FR-4 core
- [x] `internal/user/core/auth.go` -- Two-step login (password → MFA challenge → session) -- FR-4 login flow
- [x] `internal/user/adapters/http/handler.go` -- MFA enroll/confirm/disable endpoints + login TOTP step -- API contract
- [x] `internal/user/adapters/postgres/queries.sql` -- sqlc queries `SetUserTotpSecret`, `ClearUserTotpSecret` + re-run generate -- type-safe persistence
- [x] `internal/user/` -- Unit/integration tests covering the full I/O matrix -- automated verification
- [x] `web/src/pages/LoginPage.tsx` -- Two-step login UI + "MFA aktiv" indicator (UX-DR6) -- volunteer login UX
- [x] `web/src/pages/MfaPage.tsx` -- German MFA settings surface (enroll/QR/confirm/disable) -- MFA management UX
- [x] `web/src/App.tsx` -- MFA settings route + tests -- navigable MFA surface

**Acceptance Criteria:**
- Given an authenticated user, when they enable TOTP, then they see a shared secret and QR code, and MFA is enabled only after confirming a valid 6-digit code (FR-4), with the secret encrypted at rest (NFR-S4).
- Given MFA is enabled, when logging in with valid credentials, then a current 6-digit TOTP code is required before a session is issued, and the UI shows an "MFA aktiv" indicator (FR-4/UX-DR6).
- Given an invalid or expired TOTP code, when validated, then login is rejected with German microcopy and no session is issued (UX-DR7).
- Given MFA is enabled, when disabling it, then a valid current TOTP code is required before removal.

## Spec Change Log

- 2026-09-05 (Story 1.6 implementation): implemented optional TOTP MFA (FR-4) with `pquerna/otp` (RFC 6238): enrollment (secret + QR + confirm code), two-step login (password → `mfa_required` challenge → session), disable-requires-current-code, AES-256-GCM at-rest encryption of the secret via `GEAR_ENCRYPTION_KEY` (NFR-S4), structured auth logs (NFR-O1), migration 000005 (`totp_secret_encrypted`), and German MFA settings + login-step UI.

- **2026-09-05 (code review round 1)** — implementation refinements that clarify
  (but do not change) the frozen contract:
  - Enrollment is now **server-bound**: `EnrollMFARequest` persists a short-lived
    (10 min) encrypted pending secret + expiry; `ConfirmMFAEnable` validates the
    code against that server-issued secret and rejects a client-supplied secret
    that does not match it, and rejects expired enrollments. Contract unchanged:
    the server generates a fresh secret and MFA is enabled only after a valid
    code is confirmed (FR-4). Adds `pending_totp_secret_encrypted` /
    `pending_totp_expires_at` columns to migration 000005 (edited in place; DB is
    dev-only).
  - Enabling MFA revokes the user's other sessions; disabling MFA revokes all of
    the user's sessions, so pre-existing sessions cannot bypass the second factor
    (NFR-S2). Contract unchanged: two-step login is still required.
  - A missing/invalid/rotated `GEAR_ENCRYPTION_KEY` makes MFA operations answer
    `503 mfa_unavailable "MFA ist derzeit nicht verfügbar."` (real cause logged);
    the server logs a startup warning. This is the "clear server error (never a
    panic)" the design note already promised.
  - Failed-login logging is **uniform** (`login failed` for wrong password, unknown
    email, non-active account AND failed TOTP challenge) so logs do not reveal the
    failing stage (anti-enumeration, UX-DR7). NFR-O1 still emits a structured log
    per failure.
  - `totp_code` is validated as exactly 6 digits server-side (`^\d{6}$`),
    consistent with the client.

## Design Notes

- **Two-step login contract:** `POST /login` with MFA enabled and valid password returns `{"mfa_required":true}` (200) with NO token. The client then calls `POST /login` again with the same credentials + `totp_code`; only a valid code returns a session token. This keeps a single endpoint and avoids a separate challenge-state table.
- **At-rest encryption:** The secret is AES-256-GCM encrypted with a 32-byte key from `GEAR_ENCRYPTION_KEY` (hex or base64). If the key is missing, MFA operations return a clear server error (never a panic).
- **QR code:** The provisioning URI (`otpauth://totp/G.E.A.R.:{email}?secret=...&issuer=G.E.A.R.`) is generated server-side and returned once at enroll-request; the client renders it as a QR (e.g. `qrcode.react` or a small SVG) — the secret itself is shown once for manual entry.

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go and web tests pass, 0 lint issues
- `just db-up` -- expected: migration 000005 applies cleanly
- `just migrate-down && just migrate-up` -- expected: schema rebuilds cleanly
- `curl` MFA flow (enroll → confirm → login challenge → login with code → disable) -- expected: 200/400/401 per I/O matrix

## Suggested Review Order

**TOTP Domain Logic & Enrollment**

- server-bound enrollment (pending secret + expiry), confirm validates against issued secret
  [`totp.go:111`](../../internal/user/core/totp.go#L111)

- 6-digit code validation and TOTP enable/disable with re-auth
  [`totp.go:142`](../../internal/user/core/totp.go#L142)

**Two-Step Login & Session Revocation**

- MFA challenge gate and session issuance only on valid code
  [`auth.go:164`](../../internal/user/core/auth.go#L164)

**HTTP & Persistence**

- MFA endpoints (status/enroll/disable) and 503 mfa_unavailable mapping
  [`handler.go:46`](../../internal/user/adapters/http/handler.go#L46)

- at-rest AES-256-GCM secret encryption helper
  [`secret.go:52`](../../internal/platform/crypto/secret.go#L52)

- users table columns for encrypted + pending TOTP secret
  [`000005_user_totp.up.sql:20`](../../migrations/000005_user_totp.up.sql#L20)

**Frontend MFA UI**

- MFA settings surface (enroll/QR/confirm/disable) with 401 handling
  [`MfaPage.tsx:24`](../../web/src/pages/MfaPage.tsx#L24)

- two-step login UI with "MFA aktiv" indicator
  [`LoginPage.tsx:155`](../../web/src/pages/LoginPage.tsx#L155)

**Automated Verification**

- TOTP I/O matrix incl. lockout↔MFA interaction and crypto boundaries
  [`totp_test.go:1`](../../internal/user/core/totp_test.go#L1)