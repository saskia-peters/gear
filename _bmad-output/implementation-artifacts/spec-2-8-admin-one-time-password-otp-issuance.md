---
title: 'Admin One-Time-Password (OTP) Issuance'
type: 'feature'
created: '2026-09-08'
status: 'done'
review_loop_iteration: 1
baseline_commit: 'c2b0912a3c3a953ec53f5943fa42cd7da3c3fa03'
context: []
---

<!-- Target: 900–1300 tokens. Cohesive cross-layer story (DB+BE+UI) stays in ONE file. -->

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** A user locked out of G.E.A.R. while SMTP is unconfigured cannot regain access: the forced-password-change fallback (Story 1.8) exists but there is no way for an admin to hand that user a credential, so a password-lost account is stranded.

**Approach:** Let an admin generate a single-use one-time password (OTP) for an **active** account from the user detail. The OTP is hashed at rest (Argon2id), shown once in the response, never recoverable, never emailed, and must be transmitted out-of-band. Login with the OTP as the password succeeds and forces the existing mandatory password-change flow (must_change_password), after which the OTP is consumed and invalid. **OTPs are only issued to `active` accounts** — a `deactivated` or `pending_approval` account cannot hold an OTP (deactivation means "sofort kein Login"; OTP is a recovery credential for users whose password is lost, not a reactivation path).

## Boundaries & Constraints

**Always:**
- **Migration 000014** (one migration): add `one_time_password_hash text NOT NULL DEFAULT ''` and `one_time_password_expires_at timestamptz NULL` to `users`. Down reverses both.
- **Issuance endpoint** `POST /api/v1/admin/users/{userID}/otp` with body `{"confirmed": true}`. Gated by `users.manage` (defense-in-depth re-check in core). Target must exist and be `active` (a `pending_approval` or `deactivated` target → 409 conflict, uniform message; unknown/malformed id → 404). `confirmed` missing/false → 400. Sets `must_change_password=true`, stores the Argon2id hash of a freshly generated random OTP with an expiry (TTL constant, e.g. 15 minutes). Returns `{message, user_id, email, one_time_password, expires_at}` — the plaintext OTP appears ONLY in this response, never again (no GET/readback). Audited (NFR-O1).
- **OTP generation**: crypto-random, human-typeable (e.g. 10 chars from an unambiguous alphabet — no 0/O/1/I), via `crypto/rand`. Never logged.
- **Login consumption**: in `Service.Login`, when the account has a valid (unexpired) `one_time_password_hash`, the presented `input.Password` is verified against the OTP hash INSTEAD of the normal password hash, and authentication is allowed only for `active` accounts (the OTP never re-admits a deactivated account). On success: consume the OTP (clear hash+expiry), set `must_change_password=true`, and fall into the existing forced-change branch (mint a single-use reset token, no app session — Story 1.8 flow). The OTP is single-use: after one successful login it is gone. Wrong OTP → the same uniform 401 + progressive lockout as a wrong password (anti-enumeration, UX-DR7).
- **Timing discipline**: exactly one Argon2id verify per login attempt (UX-DR7). When an OTP is present, the dummy-hash fallback still applies for unknown emails/accounts without a usable OTP so timing stays uniform.
- **SPA (user detail)**: a "Einmal-Passwort" action on the user detail, visible for `active` users, gated client-side on `users.manage`. Confirmation step → POST → display the OTP once in a dismissible panel with the German warning "Nur einmal anzeigen — sicher außerhalb des Systems übermitteln (nicht per E-Mail)." Closing the panel discards the value (never stored in SPA state long-term, no copy/re-display). A 401 → login redirect; 403 → leave admin module.
- **Login SPA**: no change required — the existing `must_change_password` + `reset_token` branch already redirects to `/reset-password/<token>`.
- **Doc sync**: ARCHITECTURE-SPINE note for the OTP columns/flag.

**Ask First:**
- None (story ACs + Story 1.8 mechanism resolve the design).

**Never:**
- No OTP readback/recovery endpoint — the plaintext exists only in the issuance response (NFR-S4).
- No emailing the OTP (NFR-S4, story AC).
- No plaintext storage — Argon2id hash only (like passwords).
- No bypass of the forced password change: the OTP never issues a normal app session.
- No changes to already-committed migrations.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| ISSUE_OK_ACTIVE | admin issues for active user | OTP returned once; must_change_password set; audited | n/a |
| ISSUE_DEACTIVATED | target is deactivated | 409 conflict, uniform message (no OTP for deactivated) | 409 |
| ISSUE_PENDING | target is pending_approval | 409 conflict, uniform message | 409 |
| ISSUE_UNKNOWN | unknown/malformed id | Uniform 404 | 404 |
| ISSUE_NOT_CONFIRMED | confirmed=false or body missing | 400 | 400 |
| ISSUE_FORBIDDEN | caller lacks users.manage | Uniform 403 | 403 |
| LOGIN_OTP_OK_ACTIVE | active user, valid OTP as password | Login succeeds → must_change_password + reset token, OTP consumed, no session | n/a |
| LOGIN_OTP_EXPIRED | expired OTP | OTP not accepted; normal auth applies | 401 if password also wrong |
| LOGIN_OTP_WRONG | wrong OTP value | Uniform 401 + progressive lockout counted | 401 |
| LOGIN_OTP_CONSUMED | OTP used once, reused | Not accepted (hash cleared) → 401 | 401 |
| RE_ISSUE | OTP issued twice | New OTP replaces old (old invalid immediately) | n/a |

</frozen-after-approval>

## Code Map

- `migrations/000014_admin_otp.{up,down}.sql` -- `one_time_password_hash` + `one_time_password_expires_at` on `users`. (Highest existing: `000013_admin_rework`.)
- `internal/user/adapters/postgres/queries.sql` -- `SetUserOneTimePassword :exec` (upsert hash+expiry+must_change_password), `ClearUserOneTimePassword :exec`; extend `GetUserByEmail` SELECT to include the two new columns. Re-run `just sqlc-generate`.
- `internal/user/adapters/postgres/repository.go` -- `userFromRow` (`:861`) gains the two new params → `core.User`; `GetUserByEmail` (`:52`) passes them; new repo methods `SetUserOneTimePassword` / `ClearUserOneTimePassword`.
- `internal/user/core/user.go` -- `User` struct (`:57`) gains `OneTimePasswordHash string json:"-"` + `OneTimePasswordExpiresAt time.Time json:"-"`.
- `internal/user/core/auth.go` -- `Login` (`:124`): when a valid OTP exists, verify `input.Password` against the OTP hash (one verify, dummy fallback preserved; active-only, no deactivated re-admission); on success consume OTP + ensure `must_change_password`; reuse the forced-change branch (`:208`) which mints a reset token. `LoginInput` unchanged.
- `internal/user/core/otp.go` (new) -- `IssueOneTimePassword(ctx, actor, userID, confirmed) (*OneTimePasswordResult, error)`: gate `users.manage`, target active-only check, generate OTP, hash, persist+flag, audit. Constants: OTP alphabet, `OneTimePasswordTTL`, message/error sentinels (`ErrOneTimePasswordNotConfirmed`, `ErrOneTimePasswordTargetNotEligible`), `AuditOperationUserOtpIssue = "user.otp.issue"`.
- `internal/user/adapters/http/admin_users.go` (or new `admin_otp.go`) -- `IssueOneTimePasswordHandler` for `POST /users/{userID}/otp`, modeled on `DeactivateAdminUser` (`admin_users.go:378-419`): confirm body, 400/403/404/409 mapping, `httpapi.WriteJSON` with the plaintext OTP.
- `internal/user/adapters/http/admin.go` -- mount `users.Post("/{userID}/otp", h.IssueOneTimePasswordHandler)` in the `/users` sub-mount (`:55-71`).
- `web/src/auth/users.ts` -- `issueOneTimePassword(userId): Promise<OtpIssueResult>` POST; `OtpIssueResult` type (message, user_id, email, one_time_password, expires_at).
- `web/src/components/UserDetail.tsx` (+ `.module.css`) -- "Einmal-Passwort" action for active users gated on `canManage`; confirm step; show OTP once in a dismissible panel (warning, no persist).
- Tests: core unit (I/O matrix rows; auth OTP login branches incl. expired/consumed/timing), postgres integration (migration columns, set/clear, GetUserByEmail shape), http handler contracts (400/403/404/409, plaintext-once), web UserDetail (issue confirm, OTP shown once, 401/403).

## Tasks & Acceptance

**Execution:**
- [x] `migrations/000014_admin_otp.{up,down}.sql` -- two columns on users -- schema
- [x] `queries.sql` + `just sqlc-generate` -- OTP set/clear + GetUserByEmail columns -- persistence
- [x] repo methods + `userFromRow`/`core.User` fields -- OTP state plumbing
- [x] `core/otp.go` -- `IssueOneTimePassword` (gate, eligible-target, generate+hash, flag, audit) -- domain
- [x] `core/auth.go` Login -- OTP verify/consume + forced-change reuse (active-only) -- login
- [x] http handler + route -- `POST /users/{userID}/otp` contract -- API
- [x] `web/src/auth/users.ts` -- `issueOneTimePassword` + result type -- SPA client
- [x] `web/src/components/UserDetail.tsx` (+css) -- issue action + show-once panel -- SPA surface
- [x] Tests -- core/postgres/http/web for I/O rows + ACs, incl. concurrent-consume CAS and the documented malformed-JSON 400 -- verification
- [x] `ARCHITECTURE-SPINE.md` -- OTP columns/flag note -- docs

**Acceptance Criteria:**
- Given an admin opens a user detail for an `active` user, when they generate a one-time password and confirm, then the OTP is shown once, `must_change_password` is set, and the action is audited.
- Given the OTP is generated, when the user logs in with it as the password, then login succeeds only into the forced password-change flow (no app session) and the OTP is consumed (single-use, invalid afterwards).
- Given the target account is `deactivated` or `pending_approval`, when the admin attempts to generate a one-time password, then it is rejected with a uniform 409 (OTPs are only for active accounts).
- Given the OTP is presented at login, then it is never accepted a second time, and a wrong/expired OTP answers with the uniform 401 (progressive lockout applies).
- Given the OTP is generated, then it is only ever returned once and must be transmitted out-of-band (never emailed, never stored in plaintext).

## Spec Change Log

- **intent_gap loopback (review, 2026-09-08):** the deactivated-account OTP path was a dead end — `CompletePasswordReset` rejects non-active users (`reset.go:323`), so a deactivated user who logged in with an OTP could never complete the forced change. Human decision: **OTPs are active-only** — deactivation means "sofort kein Login" and the OTP is a password-recovery credential, not a reactivation path. Frozen intent, boundaries (issuance target `active` only; `deactivated`/`pending_approval` → 409), login consumption (active-only, no re-admission), I/O matrix (ISSUE_DEACTIVATED row, dropped LOGIN_OTP_OK_DEACTIVATED), SPA visibility (active only), ACs, Code Map, tasks, Design Notes, and Verification were amended. KEEP: single-verify timing discipline; OTP consume-on-login + `must_change_password` reuse of the Story 1.8 branch; show-once / never-email / Argon2id-at-rest invariants; crypto-random 10-char unambiguous alphabet.
- **Review patches applied (review 2, 2026-09-08):** orphaned reset-token on a lost CAS claim is now revoked before returning 401 (`DeletePasswordResetToken`); `SetUserOneTimePassword` changed to `:execrows` so a vanished target maps to 404 (never hand over a credential that cannot work); OTP button mutually exclusive with the deactivate confirm; `expires_at` shown in the show-once panel; dead rejection-sampling guard removed (alphabet length 32 divides 256); OTP+MFA test, malformed-JSON + empty-body HTTP tests, and a realistic post-expiry test added; `queries.sql` "21 base codes" source comments fixed to 22. Deferred: session/in-flight-token revocation on issuance; DB-only migration contract (skip-without-DB convention); down-migration does not clear `must_change_password`.

## Design Notes

- **Reuse Story 1.8's forced-change branch**: `Login` already returns `MustChangePassword: true` + a single-use `ResetToken` and the SPA already redirects to `/reset-password/<token>`. The OTP only needs to authenticate the password slot, consume itself, and set the flag — the rest of the recovery flow is untouched.
- **Active-only is intentional**: the story and the review resolution restrict OTPs to `active` accounts. Deactivation means "sofort kein Login" — the OTP is a recovery credential for users who lost their password, not a reactivation path. The login's active-only gate is never bypassed by an OTP.
- **Single-use under concurrency**: the OTP consume must be atomic — `ClearUserOneTimePassword` is a compare-and-swap (`WHERE id=$1 AND one_time_password_hash=$2`) and the consume is a claim: if zero rows are affected (already consumed by a racing login), the login fails. Order: mint the reset token FIRST (so a mint failure never strands the user without a credential), then CAS-consume; on a lost claim the just-minted token is revoked and the flag is set only on the winning claim.
- **Server-authoritative text**: the SPA should display the server's `message` (the German confirmation) rather than hardcoding a parallel warning, matching the codebase's "server text" convention (finding 8).
- **Timing**: keep exactly one Argon2id verify. When an OTP hash is present and unexpired, it is the verify target; otherwise the dummy-hash/normal-password logic is unchanged. This preserves the anti-enumeration timing posture (UX-DR7).

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go and web tests pass, 0 lint issues
- `just migrate-up` -- expected: migration 000014 applies; `\d users` shows the two OTP columns
- `just sqlc-generate` -- expected: regenerated queries.sql.go includes OTP queries/columns
- `curl` as admin: `POST /api/v1/admin/users/{id}/otp` with `{"confirmed":true}` -- expected: 200 with `one_time_password` once; second read is impossible (no GET)
- `curl` login with the OTP as password (active target) -- expected: `must_change_password:true` + `reset_token`, no session token; reusing the OTP -- expected: 401

## Suggested Review Order

**Design intent**

- The OTP issuance domain method — active-only gate, generate+hash, flag, audit, zero-row → 404.
  [`otp.go:104`](../../../internal/user/core/otp.go#L104)

- The login consumption — one verify, mint-then-CAS-consume, lost-claim revokes the token, never a session.
  [`auth.go:226`](../../../internal/user/core/auth.go#L226)

**Schema & persistence**

- Migration adds the two OTP columns (hash NOT NULL DEFAULT '', nullable expiry).
  [`000014_admin_otp.up.sql:16`](../../../migrations/000014_admin_otp.up.sql#L16)

- Set (execrows → 404 on vanished target) + CAS clear (single-use under concurrency).
  [`queries.sql:331`](../../../internal/user/adapters/postgres/queries.sql#L331)

- userFromRow plumbing so GetUserByEmail carries the OTP state into Login.
  [`repository.go:530`](../../../internal/user/adapters/postgres/repository.go#L530)

**HTTP contract**

- The issuance handler — confirm body, uniform 400/403/404/409 mapping, plaintext OTP once.
  [`admin_otp.go:42`](../../../internal/user/adapters/http/admin_otp.go#L42)

- Route mounted in the /users sub-mount.
  [`admin.go:63`](../../../internal/user/adapters/http/admin.go#L63)

**SPA binding**

- The issue action, mutually exclusive with deactivate, confirm step, show-once panel with expiry + server message.
  [`UserDetail.tsx:633`](../../../web/src/components/UserDetail.tsx#L633)

- The API client call + result type.
  [`users.ts:255`](../../../web/src/auth/users.ts#L255)

**Tests**

- Full I/O matrix incl. MFA combination, expired/wrong/consumed, never-re-admits-deactivated.
  [`otp_test.go:381`](../../../internal/user/core/otp_test.go#L381)

- Real SQL CAS + migration columns against the live DB.
  [`otp_test.go:20`](../../../internal/user/adapters/postgres/otp_test.go#L20)

- HTTP 400/403/404/409 + malformed-JSON/empty-body boundaries.
  [`admin_otp_test.go:120`](../../../internal/user/adapters/http/admin_otp_test.go#L120)