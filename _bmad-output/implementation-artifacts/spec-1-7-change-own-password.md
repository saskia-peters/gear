---
title: 'Change Own Password'
type: 'feature'
created: '2026-09-05'
status: 'done'
review_loop_iteration: 0
baseline_commit: '30d5a3fe6c148ac5b25ec7c3ccdda43cd0a1bfba'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Logged-in users cannot change their own password, so credentials cannot be rotated or renewed when they may be compromised, and there is no way to force other devices to re-authenticate.

**Approach:** Implement a self-service "Passwort ändern" flow (FR-25): an authenticated user confirms their current password, sets a new one (≥10 chars, FR-2), the new password is stored as an Argon2id hash (AD-13), all other active sessions are revoked instantly (FR-25), the change is audited (NFR-O1/NFR-O2), and the current session stays logged in. Introduces the User-owned `audit_log` table (architecture spine table 11) for the required audit trail.

## Boundaries & Constraints

**Always:**
- The change requires the user's **current password** to be confirmed before any change is accepted (FR-25).
- The new password must satisfy the password policy (≥10 characters, FR-2); violations are rejected with a clear German inline validation error (UX-DR8) and no change occurs.
- On a successful change the new password is stored as an Argon2id hash; plaintext is never stored, logged, or returned (FR-25/NFR-O1/AD-13).
- All other active sessions for the account are revoked instantly server-side (FR-25), forcing re-authentication on those devices; the current session stays logged in.
- The change is audited with actor, timestamp, and operation type (NFR-O1/NFR-O2) in the new User-owned `audit_log` table.
- Only an authenticated user can change their OWN password — no elevated permission needed (ownership of self is the capability, AD-12). No permission code required beyond authentication.
- The authenticated user is resolved from the session via the auth gateway (AD-6); the endpoint is protected (RequireAuth, not a specific permission).
- German UI: fields "Aktuelles Passwort", "Neues Passwort", "Wiederholung"; success confirmation "→ Andere Sitzungen beendet" (UX-DR8).
- All API errors use the uniform envelope `{"error":{"code","message","details?"}}`.

**Ask First:**
- None.

**Never:**
- No admin-initiated password resets in this story (that's Story 1.8 / admin flows).
- No forgot-password / recovery in this story (Story 1.8).
- Never store, log, or return passwords in plaintext.
- No audit of the password value — only actor, timestamp, operation type.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| CHANGE_SUCCESS | Authenticated user, correct current password, valid new password (≥10, match) | Password updated (Argon2id), other sessions revoked, current session stays valid, audit row written, 200 with confirmation | n/a |
| WRONG_CURRENT | Authenticated user, incorrect current password | Rejected, password unchanged, German error, no session revocation | 400 invalid_current_password |
| SHORT_NEW_PASSWORD | New password < 10 characters | Rejected, no change, inline German error "Das Passwort muss mindestens 10 Zeichen lang sein." (FR-2) | 400 invalid_request |
| MISMATCH | New password and confirmation differ | Rejected, no change, inline German error "Die Passwörter stimmen nicht überein." | 400 invalid_request |
| UNAUTHENTICATED | No/invalid session token | 401 unauthorized — endpoint is auth-gated | 401 unauthorized |
| LOGOUT | Authenticated user clicks "Abmelden" | `POST /api/v1/auth/logout` invalidates the session server-side (NFR-S2), client clears auth state, redirects to `/login` | n/a |
| AUDIT_WRITE_FAIL | Audit log write fails after password update | Password change still succeeds; audit failure logged (NFR-O1) — do not roll back the password on audit failure (availability) | log error |

</frozen-after-approval>

## Code Map

- `migrations/000006_audit_log.{up,down}.sql` -- Add User-owned `audit_log` table: id (uuidv7 PK), actor_user_id (FK→users), operation (text), created_at (timestamptz). Immutable append-only compliance trail (NFR-O1/NFR-O2). Architecture spine table 11.
- `internal/user/adapters/postgres/queries.sql` -- sqlc queries: `UpdateUserPassword` (set password_hash by user id), `InsertAuditEvent`. Re-run `just sqlc-generate`.
- `internal/user/core/password.go` -- `ChangePassword` use-case: verify current password via hasher, validate new (≥10, match), hash new, persist, revoke other sessions, write audit event.
- `internal/user/core/service.go` / `ports.go` -- Extend `Service` and ports with `ChangePassword`; extend `Repository` with `UpdateUserPassword` + `InsertAuditEvent`.
- `internal/user/adapters/http/handler.go` -- `POST /api/v1/auth/password/change` (auth-gated via `RequireAuth`), uniform envelope mapping.
- `internal/platform/auth/gateway.go` -- `RequireAuth` already exists (from Story 1.6) — mount the change-password route behind it.
- `cmd/server/main.go` -- Wire the change-password handler.
- `web/src/pages/ChangePasswordPage.tsx` -- German form: Aktuelles Passwort, Neues Passwort, Wiederholung; inline validation; success "→ Andere Sitzungen beendet".
- `web/src/App.tsx` -- Route `/password` (protected).
- `web/src/pages/ChangePasswordPage.test.tsx` -- Component tests.
- `web/src/components/Header.tsx` -- Add a logout button (calls `POST /api/v1/auth/logout`, clears auth state, redirects to `/login`) next to the user name.

## Tasks & Acceptance

**Execution:**
- [x] `migrations/000006_audit_log.{up,down}.sql` -- Create User-owned `audit_log` table (actor, operation, timestamp) -- audit trail (NFR-O1/NFR-O2, spine table 11)
- [x] `internal/user/adapters/postgres/queries.sql` -- Add `UpdateUserPassword`, `InsertAuditEvent` sqlc queries + re-run `just sqlc-generate` -- type-safe persistence
- [x] `internal/user/core/password.go` -- Implement `ChangePassword` use-case (verify current, validate new, hash, persist, revoke other sessions, audit) -- FR-25 core
- [x] `internal/user/adapters/http/handler.go` -- Add `POST /api/v1/auth/password/change` behind `RequireAuth` -- API contract
- [x] `internal/platform/auth/gateway.go` & `cmd/server/main.go` -- Mount change-password route behind the auth gateway -- composition root wiring
- [x] `internal/user/` -- Unit/integration tests covering the I/O matrix -- automated verification
- [x] `web/src/pages/ChangePasswordPage.tsx` -- German change-password form with inline validation and success message -- FR-25 UX
- [x] `web/src/components/Header.tsx` -- Add logout button calling `POST /api/v1/auth/logout` + `clearAuthState()` + redirect to `/login` -- logout UX (NFR-S2)
- [x] `web/src/App.tsx` & tests -- Protected `/password` route + logout navigation -- navigable surface

**Acceptance Criteria:**
- Given I am logged in, when I open the "Passwort ändern" surface, then I can enter Aktuelles Passwort, Neues Passwort, and Wiederholung (German UI, FR-25).
- Given an incorrect current password, when I submit, then the change is rejected with a clear German error and my password remains unchanged.
- Given a new password shorter than 10 characters, when I submit, then it is rejected with a clear inline validation error (FR-2/UX-DR8) and no change occurs.
- Given a successful change, when committed, then the password is stored as an Argon2id hash, all other sessions are revoked instantly (FR-25), the change is audited (NFR-O1/NFR-O2), and the UI shows "→ Andere Sitzungen beendet" while I stay logged in on the current session.
- Given a logged-in user, when they click "Abmelden", then `POST /api/v1/auth/logout` is called, the session token is invalidated server-side (NFR-S2), client auth state is cleared, and the user is redirected to `/login`.

## Spec Change Log

- 2026-09-05 (Story 1.7 implementation): implemented self-service "Passwort ändern" (FR-25) with current-password confirmation, new-password policy (≥10 chars), Argon2id hashing, instant revocation of other sessions (reusing Story 1.6 `RevokeOtherSessions`), an audit event, a User-owned `audit_log` table (migration 000006, spine table 11), a frontend logout button (calls `POST /api/v1/auth/logout`, clears auth state, redirects to `/login` — NFR-S2), and a German change-password page.

- 2026-09-05 (Story 1.7 review loop — code patches, frozen contract untouched): (1) `audit_log.actor_user_id` FK changed `ON DELETE CASCADE` → `ON DELETE SET NULL` (trail survives user deletion, actor anonymized — NFR-O1/O2). (2) Empty-token guard in `ChangePassword` (never revoke all sessions incl. the current one). (3) `ErrPasswordTooLong` sentinel → 400 (not generic 500). (4) FR-25 ordering: current password verified before new-password policy validation. (5) Client length check counts code points (matches `utf8.RuneCountInString`). (6) Error mapping by envelope `code`, not German substring. (7) 401 clears auth state before navigating. (8) `ChangePasswordResult.sessions_revoked` added (additive field); UI shows "→ Andere Sitzungen beendet" only when revocation actually succeeded. (9) Trailing newlines normalized. (10) Mock `ErrUserNotFound` sentinel (was `ErrUserAlreadyExists`). (11) Header name gated on a session token. (12) Non-DB test pinning the `password_hash` session-resolution chain through `RequireAuth`. (13) Session/User DTOs never serialize `password_hash` (pinned by tests). (14) Shared `SESSION_TOKEN_KEY` helper. (15) Spec Code Map deduped. Deferred: DB-enforced audit immutability (trigger/RLS — interacts with DSGVO deletion, future hardening) and require-new-password-differs-from-current (not required by FR-25).

## Design Notes

- **Audit table introduction:** Story 1.7 introduces the User-owned `audit_log` table (architecture spine table 11). It is a minimal append-only compliance trail (actor_user_id, operation, created_at). Later stories (1.8 forgot-password, 1.10 dual-admin recovery, admin operations) reuse it. `InsertAuditEvent` is best-effort: a failed audit write does not roll back a successful password change (availability), but the failure is logged (NFR-O1).
- **Session revocation reuse:** the existing `RevokeOtherSessions(ctx, userID, currentRawToken)` from Story 1.6 is reused so the current session survives.
- **Endpoint contract:** `POST /api/v1/auth/password/change` with `{"current_password","new_password","new_password_confirm"}`; 200 on success.

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go and web tests pass, 0 lint issues
- `just db-up` -- expected: migration 000006 applies cleanly
- `just migrate-down && just migrate-up` -- expected: schema rebuilds cleanly
- `curl` change-password flow (auth → change → other session revoked → audit row) -- expected: 200/400/401 per I/O matrix

## Suggested Review Order

**Audit Trail**

- User-owned append-only audit_log table with SET-NULL actor FK
  [`000006_audit_log.up.sql:24`](../../migrations/000006_audit_log.up.sql#L24)

**Change-Password Core**

- FR-25 use-case: current-password verify, policy, Argon2id, session revocation, audit
  [`password.go:87`](../../internal/user/core/password.go#L87)

**HTTP Contract**

- auth-gated change-password endpoint with error mapping
  [`handler.go:340`](../../internal/user/adapters/http/handler.go#L340)

**Frontend**

- logout button: POST /auth/logout, clear auth state, redirect
  [`Header.tsx:28`](../../web/src/components/Header.tsx#L28)

- German change-password page with revocation-status-driven success note
  [`ChangePasswordPage.tsx:22`](../../web/src/pages/ChangePasswordPage.tsx#L22)

- shared session-token key helper
  [`authState.ts:10`](../../web/src/auth/authState.ts#L10)

**Automated Verification**

- I/O matrix incl. session-chain and DTO-serialization tests
  [`password_test.go:1`](../../internal/user/core/password_test.go#L1)
