---
title: 'Progressive Login Lockout'
type: 'feature'
created: '2026-09-05'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'b52f660d9d1c424d0ab79c7d0be948e44851dac4'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Repeated failed login attempts are currently unthrottled, so an attacker can brute-force an account's password without any slowdown (Story 1.4 explicitly deferred rate limiting).

**Approach:** Implement progressive, time-based login lockout (FR-3): after 3 consecutive failed logins the account is blocked for 30 seconds, and after 4 or more for 60 seconds, returning HTTP 429 with a `Retry-After` header. Failures are tracked per account (no cross-account cascade), the counter resets after the lockout expires and on a successful login (no permanent lockout), lockout events are emitted to structured logging (NFR-O1), and the login UI shows a German lockout screen "Zu viele Fehlversuche — 30/60 Sekunden warten" (UX-DR6/UX-DR8).

## Boundaries & Constraints

**Always:**
- Per-account tracking only — lockout for one account never affects another (FR-3).
- After 3 consecutive failed logins for an account, the next login attempt is blocked for 30 seconds (HTTP 429).
- After 4 or more consecutive failed logins for an account, the next login attempt is blocked for 60 seconds (HTTP 429).
- A locked account still returns HTTP 429 for ANY login attempt (correct or not) during the lockout window, so an attacker cannot distinguish "blocked" from "wrong password".
- The lockout is **not permanent**: once the lockout period expires, login attempts are accepted again and the failure counter resets for a fresh cycle (no indefinite lockout from repeated failures alone).
- A successful login resets the failure counter for that account.
- Any 429 lockout is emitted to structured logging for auth events (NFR-O1).
- The login handler returns `Retry-After` (seconds) on 429 so the client can count down.
- Anti-enumeration (UX-DR7) still applies outside lockout: wrong password / unknown email / non-active all remain HTTP 401 with identical microcopy. The lockout check runs BEFORE the password verify and returns 429 regardless of whether the presented password would be correct.
- Login UI shows the German lockout message "Zu viele Fehlversuche — 30/60 Sekunden warten" with no retry button until the countdown expires; accessible (UX-DR9).
- All API errors use the uniform envelope `{"error":{"code","message","details?"}}`; 429 uses code `too_many_attempts`.

**Ask First:**
- None.

**Never:**
- No IP-based rate limiting — lockout is per account only (FR-3 explicitly scopes to account).
- No permanent/indefinite account lockout from repeated failures.
- No CAPTCHA or challenge-gating (out of scope).
- Never reveal the exact failure reason for a locked account (attacker could confirm account existence) — the 429 message is generic.
- No MFA interplay yet (TOTP ships in Story 1.6).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| LOCKOUT_3_FAILS | 3 consecutive failed logins for an account, then a 4th attempt | HTTP 429 with `Retry-After: 30`, code `too_many_attempts`, generic German message; account blocked 30s | 429 too_many_attempts |
| LOCKOUT_4_PLUS | 4+ consecutive failed logins | HTTP 429 with `Retry-After: 60`, account blocked 60s | 429 too_many_attempts |
| LOCKOUT_ACTIVE_CORRECT_PW | Account in lockout, user submits correct password | Still HTTP 429 (blocked) — no session issued, no counter reset | 429 too_many_attempts |
| LOCKOUT_EXPIRED | Account in lockout, lockout period elapses | Login accepted again; failure counter reset for a fresh cycle | n/a |
| LOCKOUT_SUCCESS_RESET | Account had failures, then a successful login | Login 200, session issued, failure counter reset | n/a |
| NO_LOCKOUT_PER_ACCOUNT | Two different accounts, each with failures | Each tracked independently; no cascade between accounts (FR-3) | n/a |
| NORMAL_401 | Fewer than 3 failures, wrong password | HTTP 401 anti-enumeration (unchanged behavior) | 401 invalid_credentials |
| FRESH_ACCOUNT | First failed login | HTTP 401, failure counter becomes 1 (not yet locked) | 401 invalid_credentials |

</frozen-after-approval>

## Code Map

- `migrations/000004_login_attempts.{up,down}.sql` -- Add `login_attempts` table: user_id PK FK→users, failed_count, lockout_until (nullable timestamptz), updated_at. User-owned (AD-2/AD-11).
- `internal/user/adapters/postgres/queries.sql` -- sqlc queries: `GetLoginAttempts`, `UpsertLoginAttempts`, `ClearLoginAttempts`. Re-run `just sqlc-generate`.
- `internal/user/core/lockout.go` -- Lockout domain logic: thresholds (3→30s, 4+→60s), `Check`, `RecordFailure`, `RecordSuccess`, reset-after-expiry, per-account.
- `internal/user/core/auth.go` -- Integrate lockout into `Login`: check lockout before verify (→ `ErrLockedOut`), record failure on wrong password, reset on success.
- `internal/user/adapters/http/handler.go` -- Map `ErrLockedOut` to HTTP 429 with `Retry-After` header and `too_many_attempts` envelope.
- `internal/platform/logger` (NFR-O1) -- Emit a structured WARN/INFO log line on each lockout trigger.
- `internal/user/core/lockout_test.go` -- Unit tests covering the I/O matrix (3 fails, 4+ fails, expired reset, success reset, per-account).
- `internal/user/adapters/http/handler_test.go` -- Handler tests asserting 429 + `Retry-After`.
- `web/src/pages/LoginPage.tsx` -- Handle 429: show lockout screen with countdown and no retry button.
- `web/src/pages/LoginPage.module.css` -- DESIGN.md token styling for the lockout state.
- `web/src/pages/LoginPage.test.tsx` -- Component tests for the 429 lockout screen.

## Tasks & Acceptance

**Execution:**
- [x] `migrations/000004_login_attempts.{up,down}.sql` -- Create `login_attempts` table (user_id PK FK, failed_count, lockout_until) -- per-account lockout persistence (FR-3)
- [x] `internal/user/adapters/postgres/queries.sql` -- Add sqlc queries `GetLoginAttempts`, `UpsertLoginAttempts`, `ClearLoginAttempts` and re-run `just sqlc-generate` -- type-safe attempt tracking
- [x] `internal/user/core/lockout.go` -- Implement lockout thresholds and per-account check/record/reset logic -- FR-3 rules
- [x] `internal/user/core/auth.go` -- Integrate lockout into `Login`: pre-verify lockout check, failure recording, success reset -- FR-3 behavior
- [x] `internal/user/adapters/http/handler.go` -- Map `ErrLockedOut` to HTTP 429 + `Retry-After` + `too_many_attempts` envelope -- API contract
- [x] `internal/platform/logger` -- Emit structured log on lockout (NFR-O1) -- auth event observability
- [x] `internal/user/` -- Write unit/integration tests covering lockout matrix -- I/O matrix verification
- [x] `web/src/pages/LoginPage.tsx` -- Handle 429 with German lockout screen + countdown, no retry button -- UX-DR6/UX-DR8
- [x] `web/src/pages/LoginPage.test.tsx` -- Component tests for the lockout screen -- frontend verification

**Acceptance Criteria:**
- Given 3 consecutive failed logins for an account, when a further login attempt is made, then the account is blocked for 30 seconds and the response is HTTP 429 (FR-3).
- Given 4 or more consecutive failed logins, when a further attempt is made, then the account is blocked for 60 seconds and the response is HTTP 429 (FR-3).
- Given an account in lockout, when the period expires, then login is accepted again and the counter resets (no permanent lockout).
- Given any 429 lockout, when it triggers, then it is emitted to structured logging (NFR-O1), and the login UI shows "Zu viele Fehlversuche — 30/60 Sekunden warten" with no retry button until the timer expires (UX-DR6/UX-DR8).

## Spec Change Log

- 2026-09-05 (Story 1.5 implementation): implemented progressive per-account login lockout (FR-3) with a `login_attempts` table (migration 000004), sqlc queries, lockout domain logic, integration into `Service.Login`, HTTP 429 + `Retry-After` mapping, structured logging (NFR-O1), and a German lockout UI with countdown. **Design-note correction (non-frozen):** the "Reset semantics" note originally said the counter clears on expiry; the implementation clears the counter **only on successful login**. Rationale: clearing on expiry would reset the count to 0 after every 30s block, making the mandatory "4+ fails → 60s" tier unreachable (a 4th failed verify can only occur after the 30s window elapses). Keeping the counter until a success satisfies all acceptance criteria (3 fails → 30s, 4+ fails → 60s, bounded lockout that always expires = no permanent lockout, success resets for a fresh cycle) and preserves per-account isolation.

- 2026-09-05 (Story 1.5 review loop — two human-approved spec changes + code patches): (1) **Email-keyed lockout (anti-enumeration, human-approved):** `login_attempts` is keyed on the normalized email (no users FK) so unknown emails also accumulate failures and also hit 429 — previously only real accounts could reach 429, letting an attacker enumerate valid emails. Migration 000004 rewritten in place (dev-only DB). (2) **Reset semantics confirmed (human-approved):** counter persists across lockout windows and clears only on successful login (see implementation entry). (3) **Atomic RMW:** replaced get-then-upsert with a single atomic `INSERT ... ON CONFLICT ... SET failed_count = LEAST(failed_count+1, 10)` so concurrent attempts can't lose updates. (4) Dead `RecordSuccess`/`RecordFailure` removed (recording now lives in the atomic SQL). (5) 429 message uses the real `Retry-After` value dynamically. (6) Robust fallback for a bare `ErrLockedOut` (sane 30s default + generic WARN). (7) Web: Enter-submit guard while locked, lockout persisted to `localStorage` (survives refresh), impure state-updater side effect fixed. (8) `failed_count` capped at 10. (9) Consistent German wait wording across server/UI/tests. (10) Mock/repository aligned (clear keeps a zeroed row).

## Design Notes

- **Progressive Thresholds (FR-3):** `failed_count < 3` → no block (401 on failure); `failed_count == 3` → block 30s; `failed_count >= 4` → block 60s. `lockout_until = now() + duration` is set when the threshold is crossed.
- **Reset semantics:** The counter is cleared on a **successful login** (fresh cycle). Lockout windows are bounded (30s / 60s) and always expire, so there is no permanent lockout from repeated failures alone. The counter is NOT cleared on expiry — keeping it lets the failure count escalate from 3 (→30s) to 4+ (→60s) across successive lockout windows, which is what makes the "4+ fails → 60s" acceptance criterion reachable.
- **429 timing:** The lockout check runs before the Argon2id verify and returns 429 regardless of password correctness, so a blocked account cannot be probed. Because lockout is keyed on the normalized email (including unknown emails), a 429 is non-discriminating — it cannot confirm whether an account exists. The counter only increments on a failed verify; attempts are recorded atomically (`INSERT ... ON CONFLICT ... SET failed_count = LEAST(failed_count+1, 10)`), so concurrent requests cannot lose updates.

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go and web tests pass, 0 lint issues
- `just db-up` -- expected: migration 000004 applies cleanly
- `just migrate-down && just migrate-up` -- expected: schema rebuilds cleanly
- `curl` login loop asserting 401 → 401 → 401 → 429 (Retry-After) -- expected: lockout triggers after 3 failures

## Suggested Review Order

**Lockout Domain Logic**

- per-email lockout thresholds (3→30s, 4+→60s) and error type with Retry-After
  [`lockout.go:23`](../../internal/user/core/lockout.go#L23)

**Auth Integration**

- email-keyed lockout gate before the verify, failure recording, success reset
  [`auth.go:103`](../../internal/user/core/auth.go#L103)

**HTTP & Persistence**

- 429 mapping with dynamic Retry-After and NFR-O1 log
  [`handler.go:85`](../../internal/user/adapters/http/handler.go#L85)

- atomic email-keyed attempt increment (no lost updates under concurrency)
  [`queries.sql:65`](../../internal/user/adapters/postgres/queries.sql#L65)

- email-keyed login_attempts table (migration 000004)
  [`000004_login_attempts.up.sql:1`](../../migrations/000004_login_attempts.up.sql#L1)

**Frontend Lockout UI**

- 429 handling, countdown, Enter-guard, and localStorage persistence
  [`LoginPage.tsx:42`](../../web/src/pages/LoginPage.tsx#L42)

**Automated Verification**

- full I/O matrix: 3/4+ fails, expiry, success reset, per-email isolation
  [`lockout_test.go:1`](../../internal/user/core/lockout_test.go#L1)
