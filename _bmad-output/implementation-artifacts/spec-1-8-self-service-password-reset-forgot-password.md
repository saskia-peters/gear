---
title: 'Self-Service Password Reset (Forgot Password)'
type: 'feature'
created: '2026-09-06'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'da9a262d9edd81b57904c56936988509e7de43c6'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** A user who forgets their password cannot regain access without an admin, and there is no recovery mechanism (no forgot-password flow, no reset tokens, no one-time-password fallback when SMTP is unconfigured).

**Approach:** Implement the FR-26 forgot-password flow: a "Passwort vergessen" entry on the login page that accepts an email with uniform anti-enumeration confirmation; if the account is `active` and SMTP is configured, a single transactional email with a single-use, hashed, 30-minute reset link is sent; if SMTP is **not** configured (early deployment), the user is told "the admins have been notified" and the account is flagged so the next login requires a mandatory password change (admin one-time-password fallback, created out-of-band in Epic 2). A reset link lets the user set a new password (≥10 chars, FR-2). All events are audited (NFR-O1).

## Boundaries & Constraints

**Always:**
- "Passwort vergessen" on the login page accepts an email and always returns the **uniform** confirmation "Wenn deine E-Mail registriert ist, erhältst du einen Link" (FR-26/UX-DR7) — no account-enumeration.
- If the email matches an `active` account AND SMTP email delivery is configured (AD-14, `smtp_settings`), a **single transactional email** is sent carrying a **single-use, hashed-at-rest, 30-minute** reset link/token (FR-26/AD-13). No automated notification emails (PRD §5).
- A reset token: valid at most 30 minutes, usable once, invalidated on use/expiry; **multiple reset requests invalidate earlier tokens** (only the latest is valid); stored as a hash at rest (never the raw token).
- If the account is `active` but SMTP is NOT configured (early deployment), no email is sent; the user is shown German microcopy that the admins have been notified and will provide a one-time password, and the account is flagged (e.g. `must_change_password = true`) so the next login **requires a mandatory password change** before accessing the app.
- Reset-complete (via valid link): set a new password (≥10 chars, FR-2) + repeat; the old password is replaced with an Argon2id hash (AD-13); the used token and any earlier tokens are invalidated; the user's sessions are revoked (re-auth required, NFR-S2).
- `deactivated` / `pending_approval` accounts: the uniform confirmation is returned WITHOUT sending an actionable reset or leaking account state (anti-enumeration).
- Every reset request and completion is emitted to structured auth logging and audited (NFR-O1 via the existing `audit_log`).
- All API errors use the uniform envelope `{"error":{"code","message","details?"}}`.
- German UI: "Passwort vergessen" link on login, a reset-request form (email), and a set-new-password form (Neues Passwort + Wiederholung) with FR-2 inline validation.

**Ask First:**
- None. SMTP sending itself is deferred to Epic 3's `smtp_settings` (Story 3.1); this story implements the token issuance + the "SMTP not configured" fallback path (no actual SMTP delivery code is required — the email send is stubbed behind a port so Story 3.1 wires the real sender).

**Never:**
- No actual SMTP sending in this story (SMTP configuration/sending is Story 3.1); the email delivery is a **port** the User module consumes, with the not-configured path exercised now.
- No admin "create one-time password" UI here (that is Epic 2, FR-21) — only the flag `must_change_password` + the login-time forced-change behavior.
- No dual-admin credential recovery (FR-27) in this story (Story 1.10).
- Never store the raw reset token — only its hash; never log or return tokens.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| FORGOT_UNKNOWN_EMAIL | Any email, no matching account | Uniform confirmation "Wenn deine E-Mail registriert ist, erhältst du einen Link" — no enumeration | n/a |
| FORGOT_ACTIVE_NO_SMTP | Active account, SMTP not configured | Uniform confirmation + German note "the admins have been notified"; `must_change_password=true`; no email sent | n/a |
| FORGOT_ACTIVE_WITH_SMTP | Active account, SMTP configured | Uniform confirmation; single-use hashed 30-min token created (invalidates prior); email send requested via port | send failure logged (NFR-O1), user still sees uniform confirmation |
| FORGOT_DEACTIVATED | Deactivated account | Uniform confirmation, NO actionable reset, no state leak | n/a |
| FORGOT_PENDING | pending_approval account | Uniform confirmation, NO actionable reset | n/a |
| RESET_VALID | Valid unexpired single-use token + new password (≥10, match) | New password set (Argon2id), token invalidated, earlier tokens invalidated, sessions revoked, 200 confirmation | n/a |
| RESET_EXPIRED | Token past 30 min | Rejected with German error, require a new link | 400 invalid_token |
| RESET_USED | Token already used | Rejected with German error | 400 invalid_token |
| RESET_SHORT_PW | New password < 10 chars | Rejected with FR-2 inline validation error, no change | 400 invalid_request |
| RESET_MISMATCH | New password and repeat differ | Rejected, no change | 400 invalid_request |
| LOGIN_MUST_CHANGE | Active user with `must_change_password=true` logs in with password/OTP | Authentication succeeds but NO session is issued for app access; response signals "must change password"; UI forces the change form | n/a |

</frozen-after-approval>

## Code Map

- `migrations/000008_password_reset.{up,down}.sql` -- Add User-owned `password_reset_tokens` table (spine table 19): id, user_id FK→users, token_hash (unique), expires_at, created_at. Add `must_change_password boolean NOT NULL DEFAULT false` to `users`.
- `internal/user/adapters/postgres/queries.sql` -- sqlc queries: `CreatePasswordResetToken` (invalidating prior), `GetPasswordResetTokenByHash`, `DeletePasswordResetToken`, `SetUserMustChangePassword`, `ClearUserMustChangePassword`. Re-run `just sqlc-generate`.
- `internal/user/core/reset.go` -- Forgot-password + reset use-cases: `RequestPasswordReset` (anti-enumeration, token issuance + email port OR must-change flag), `CompletePasswordReset` (validate token, set password, invalidate, revoke sessions, audit).
- `internal/user/ports` -- Add a `ResetEmailSender` port (send a single reset email; Story 3.1 wires a real SMTP sender) so the not-configured path is the active default now.
- `internal/user/core/auth.go` -- Login: if `must_change_password=true`, authenticate credentials but return a "must change password" signal (no app session) so the UI forces the change.
- `internal/user/adapters/http/handler.go` -- `POST /api/v1/auth/password/forgot` (email) and `POST /api/v1/auth/password/reset` (token + new password); map token errors to the uniform envelope.
- `cmd/server/main.go` -- Wire the reset service + email-sender stub.
- `web/src/pages/ForgotPasswordPage.tsx` -- German email form with uniform confirmation.
- `web/src/pages/ResetPasswordPage.tsx` -- German set-new-password form (token from URL) with FR-2 validation.
- `web/src/pages/LoginPage.tsx` -- Add "Passwort vergessen" link; handle `must_change_password` (force change flow).
- `web/src/App.tsx` -- Routes `/forgot-password` and `/reset-password/:token`.
- `internal/user/adapters/postgres/queries.sql` -- sqlc query `IsUserInPermissionGroup` (membership in the `admin` group) -- drives ADMIN module visibility.
- `internal/user/core/auth.go` / `profile.go` -- Add `IsAdmin bool` to `LoginUser` and `Profile` (resolved server-side from admin-group membership) -- server-authoritative ADMIN visibility.
- `internal/user/adapters/http/handler.go` -- Include `is_admin` in login + profile responses.
- `web/src/auth/authState.ts` -- Cache `gear.is_admin`; add `getIsAdmin()`/`setIsAdmin()`.
- `web/src/components/Sidebar.tsx` + `.module.css` -- Left sidebar showing the "GEAR" module (always) and the "ADMIN" module (only when `is_admin`), with DESIGN.md tokens.
- `web/src/App.tsx` / page layout -- Mount the Sidebar in the authenticated app shell (dashboard + profile + mfa + password + new auth surfaces).
- `web/src/App.tsx` -- Harden `RequireAuth`: validate the session server-side (`GET /auth/profile`) on mount (and on `pageshow` to defeat back-forward cache); a revoked/absent token clears auth state and redirects to `/login` -- logout is enforced client-side, not just token presence.

## Tasks & Acceptance

**Execution:**
- [x] `migrations/000008_password_reset.{up,down}.sql` -- `password_reset_tokens` table + `users.must_change_password` -- reset token + forced-change storage (FR-26/AD-13)
- [x] `internal/user/adapters/postgres/queries.sql` -- sqlc queries (create-invalidating, get-by-hash, delete, set/clear must-change) + re-run `just sqlc-generate` -- type-safe persistence
- [x] `internal/user/core/reset.go` -- `RequestPasswordReset` + `CompletePasswordReset` use-cases (anti-enumeration, token lifecycle, audit) -- FR-26 core
- [x] `internal/user/ports` -- `ResetEmailSender` port (stubbed; real SMTP in Story 3.1) -- decoupled email delivery
- [x] `internal/user/core/auth.go` -- Login handles `must_change_password` (authenticate but signal forced change, no app session) -- FR-26 fallback behavior
- [x] `internal/user/adapters/http/handler.go` -- `POST /auth/password/forgot` + `POST /auth/password/reset` -- API contract
- [x] `cmd/server/main.go` -- Wire reset service + email-sender stub -- composition root
- [x] `internal/user/` -- Unit/integration tests covering the full I/O matrix -- automated verification
- [x] `web/src/pages/ForgotPasswordPage.tsx` -- German email form + uniform confirmation -- forgot-password UX
- [x] `web/src/pages/ResetPasswordPage.tsx` -- German set-new-password form (token in URL) -- reset UX
- [x] `web/src/pages/LoginPage.tsx` -- "Passwort vergessen" link + forced-change handling -- login UX
- [x] `web/src/App.tsx` -- `/forgot-password` + `/reset-password/:token` routes -- navigable surfaces
- [x] `internal/user/adapters/postgres/queries.sql` -- Add `IsUserInPermissionGroup` sqlc query + re-run `just sqlc-generate` -- admin-group membership check
- [x] `internal/user/core/auth.go` / `profile.go` -- Add `IsAdmin` to `LoginUser` + `Profile` (resolved from admin-group membership) -- server-authoritative ADMIN visibility
- [x] `internal/user/adapters/http/handler.go` -- Include `is_admin` in login + profile responses -- API contract
- [x] `web/src/auth/authState.ts` -- Cache `gear.is_admin`; `getIsAdmin()`/`setIsAdmin()` -- client module-visibility state
- [x] `web/src/components/Sidebar.tsx` + `.module.css` -- Left sidebar: "GEAR" module always, "ADMIN" module only for admins -- module navigation shell
- [x] `web/src/App.tsx` -- Mount Sidebar in the authenticated shell (all authenticated pages) -- persistent module nav
- [x] `web/src/App.tsx` -- Harden `RequireAuth`: validate the session server-side (`GET /auth/profile`) on mount + `pageshow` (defeat back-forward cache); revoked/absent token → clear state + redirect `/login` -- enforced logout

**Acceptance Criteria:**
- Given I am on the login page and click "Passwort vergessen" and enter my email, then I am shown the uniform confirmation "Wenn deine E-Mail registriert ist, erhältst du einen Link" regardless of whether the account exists (FR-26/UX-DR7).
- Given an `active` account with SMTP configured, when I submit the request, then a single-use, hashed, 30-minute reset token is created and a single transactional email is requested via the sender port (FR-26/AD-13).
- Given SMTP is NOT configured, when I submit the request, then the user is told the admins have been notified and will provide a one-time password, and the account is flagged `must_change_password` so the next login forces a password change (no email sent).
- Given a valid reset link, when I set a new password (≥10 chars), then the old password is replaced with an Argon2id hash, the token and earlier tokens are invalidated, sessions are revoked, and I can log in with the new password (FR-2/AD-13/FR-26).
- Given an authenticated user, then a left sidebar shows the "GEAR" module for everyone and the "ADMIN" module only when the account belongs to the admin group.
- Given a logged-out or session-revoked user, when they navigate to an authenticated route (or use the browser back button after logout), then they are redirected to `/login` — the session is validated server-side, not just by local token presence.

## Spec Change Log

- 2026-09-06 (Story 1.8 implementation): implemented FR-26 forgot-password reset with a User-owned `password_reset_tokens` table (migration 000008, single-use hashed 30-min tokens), a `ResetEmailSender` port (stubbed; real SMTP in Story 3.1) with the SMTP-not-configured fallback (`must_change_password` flag + session revocation), forced-change login handling, and the new left sidebar (GEAR always / ADMIN for admin-group users) plus server-side session validation in `RequireAuth` (hardened logout).

### Review-loop fixes (applied 2026-09-06; frozen intent/contract untouched)

- **Rate-limited forgot endpoint (1.8-2):** `POST /auth/password/forgot` is now
  gated by a per-email in-memory throttle (`ForgotPasswordMinInterval`, 60s;
  `ErrForgotThrottled` → uniform 429). The gate is keyed by the normalized email
  string regardless of account existence, so a 429 is not discriminating
  (anti-enumeration, mirroring the login lockout).
- **Timing normalization for unknown emails (1.8-3/1.8-10):** unknown-email
  forgot requests now write an anonymous audit row
  (`password.reset.request.unknown`, no actor) so enumeration attempts leave a
  trail (NFR-O1) and the unknown path performs comparable-cost work; the throttle
  runs before the lookup on every path.
- **"Admins have been notified" surfaced (1.8-4):** the must-change login
  response now carries a German `message` (constant `MsgMustChangePassword`); the
  SPA carries it to `/reset-password/:token` via navigation state and shows it on
  the forced-change form. The forgot response stays strictly uniform
  (anti-enumeration).
- **Atomic single-use token invalidation (1.8-5):** `CompletePasswordReset` now
  consumes the token via a data-modifying CTE
  (`DELETE ... RETURNING`, `ConsumePasswordResetToken`) in the same statement that
  reads it; a losing concurrent completion sees no row → `ErrResetTokenInvalid`.
  A rejected policy violation no longer leaves a reusable token.
- **Reset link built by the service (1.8-6):** the `ResetEmailSender` port now
  receives the full clickable `/reset-password/<rawToken>` link; the service
  builds it from the new `GEAR_APP_ORIGIN` config (default
  `http://localhost:5173`, added to config + `.env.example`), so Story 3.1's real
  sender never needs to know the link format.
- **Lazy expired-token purge (1.8-7):** `RequestPasswordReset` deletes the user's
  expired tokens (`DeleteExpiredPasswordResetTokens`) on every request; a
  background sweeper is deferred.
- **RequireAuth clears stale cache on no-token (1.8-8):** the `RequireAuth`
  no-token branch calls `clearAuthState()` before redirecting, matching the 401
  path.
- **Anti-enumeration string consolidation (1.8-9):** the two uniform
  anti-enumeration strings are intentionally distinct, FROZEN contracts for
  distinct flows — `MsgPasswordResetRequested`
  ("…erhältst du einen Link.", spec-1-8) for forgot, `UniformSuccessMessage`
  ("…erhältst du eine Bestätigung.", spec-1-3) for registration. Each lives
  behind its own constant (cross-referenced) so no flow can pick up the other's
  text; merging would violate a frozen contract.
- **Audit unknown-email forgot requests (1.8-10):** see 1.8-3.
- **Sessions revoked on must-change flag (1.8-11):** the SMTP-not-configured
  fallback now revokes the user's live sessions when it sets
  `must_change_password`, so the forced change is enforced immediately (re-login
  required), not just at the next opportunistic login.
- **Sidebar admin visibility pinned end-to-end (1.8-12):** App-level test asserts
  RequireAuth's `GET /auth/profile` (with `is_admin:true`) drives the ADMIN link
  in the authenticated shell.
- **Raw-token-in-URL hardening (1.8-13):** the SPA HTML sets
  `Referrer-Policy: no-referrer` (`<meta name="referrer" content="no-referrer">`)
  so the reset token in the URL is not leaked via the Referer header to offsite
  links. Trade-off documented in `core/reset.go`: the token is single-use, hashed
  at rest, expires after 30 minutes, and use/completion (or re-issuing a reset)
  invalidates it, so an exposed link is only usable once within a bounded window.
- **`/admin` route + placeholder (1.8-fix):** admins no longer land on a 404; a
  stub `AdminPage` ("Admin-Modul — folgt in Epic 2") is mounted behind
  RequireAuth/AppShell. Admin-only authorization for the real module is enforced
  server-side in Epic 2.
- **Fresh query surface:** `ConsumePasswordResetToken`,
  `DeleteExpiredPasswordResetTokens`, `InsertAuditEventAnonymous` added;
  `GetPasswordResetTokenByHash`/`DeletePasswordResetToken` retained in the adapter
  (single-use still enforced via atomic consume).

## Design Notes

- **Email sender as a port:** `ResetEmailSender` is an outbound port the User module consumes (AD-14: the User module never owns SMTP). In this story the sender is a stub; the SMTP-not-configured branch is the active default (no `smtp_settings` exist until Story 3.1). When Story 3.1 adds real SMTP, it supplies the concrete sender and the email branch activates automatically.
- **Token storage:** only the SHA-256 hash of the opaque reset token is stored; the raw token is returned once (in the email link) and never persisted/logged. The link format: `/reset-password/<raw-token>`.
- **Mandatory change flag:** `must_change_password` is cleared once the user completes a password change (via the forced flow or the reset link).

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go and web tests pass, 0 lint issues
- `just db-up` -- expected: migration 000008 applies cleanly
- `just migrate-down && just migrate-up` -- expected: schema rebuilds cleanly
- `curl` forgot/reset flow (forgot unknown → uniform; forgot active no-SMTP → must-change flag; reset valid → new password + token invalidated) -- expected: 200/400 per I/O matrix

## Suggested Review Order

**Forgot-Password & Reset Core**

- anti-enumeration request, throttle, token minting, must-change flag + session revocation
  [`reset.go:201`](../../internal/user/core/reset.go#L201)

- atomic single-use token consumption + new-password set (FR-2/AD-13)
  [`reset.go:283`](../../internal/user/core/reset.go#L283)

- reset tokens table + must_change_password column
  [`000008_password_reset.up.sql:14`](../../migrations/000008_password_reset.up.sql#L14)

**Auth Shell: Sidebar & Logout**

- GEAR always / ADMIN only for admin-group users
  [`Sidebar.tsx:10`](../../web/src/components/Sidebar.tsx#L10)

- authenticated app shell mounting the sidebar
  [`AppShell.tsx:9`](../../web/src/components/AppShell.tsx#L9)

- server-side session validation on mount + pageshow (enforced logout)
  [`App.tsx:35`](../../web/src/App.tsx#L35)

**Admin Module Stub & Reset UX**

- admin module placeholder (Epic 2)
  [`AdminPage.tsx:10`](../../web/src/pages/AdminPage.tsx#L10)

- German forgot-password form with uniform confirmation
  [`ForgotPasswordPage.tsx:11`](../../web/src/pages/ForgotPasswordPage.tsx#L11)

- German set-new-password form (token in URL)
  [`ResetPasswordPage.tsx:12`](../../web/src/pages/ResetPasswordPage.tsx#L12)

**Automated Verification**

- reset I/O matrix incl. throttle, timing, concurrency, and sidebar e2e
  [`reset_test.go:1`](../../internal/user/core/reset_test.go#L1)