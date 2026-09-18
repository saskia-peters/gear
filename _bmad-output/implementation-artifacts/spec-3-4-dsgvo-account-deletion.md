---
title: 'DSGVO Account Deletion (FR-24/AD-8/NFR-O2)'
type: 'feature'
created: '2026-09-18'
status: 'done'
review_loop_iteration: 0
baseline_commit: '9c763e7412ae70f43cb4a3428d3267bb64c46568'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-3-context.md'
  - '{project-root}/_bmad-output/implementation-artifacts/spec-3-3-dsgvo-data-access-report.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The DSGVO surface can produce data-access reports (3.3) but cannot erase a user's account per the right of erasure — personal data cannot be made inaccessible while inspection history must stay intact (FR-24).

**Approach:** Extend the DSGVO orchestrator (AD-8) with `DeleteAccount`: the Tool module's lifecycle port rewrites the target's inspection/reinstatement references to the canonical "Deleted User" sentinel (explicit, idempotent, auditable); the User module soft-deletes the account (state `deleted`, login permanently blocked) and moves its personal data into a `dsgvo_deleted_accounts` ARCHIVE table — **no hard delete at deletion time**. The archived accounts are listed on a new admin surface and are HARD-PURGED only ON DEMAND by an admin. The audit trail auto-anonymizes (`ON DELETE SET NULL` on purge) and `dsgvo.delete` is audited. The SPA adds the heavy two-step delete flow plus the purge-on-demand list.

## Boundaries & Constraints

**Always:**
- **Orchestrator** (`internal/dsgvo/core`): `Service.DeleteAccount(ctx, actorID, targetUserID, reason)` — re-checks `dsgvo.delete` (AD-6), rejects SELF-deletion (`actorID == targetUserID` → German 400), rejects an unknown target (`ErrAdminUserNotFound` → 404), requires a NON-EMPTY trimmed `reason` (≤ 2000 runes → 400), then in order: (1) Tool `AnonymizeUserReferences`, (2) User `SoftDeleteAndArchive`, (3) audit `dsgvo.delete` (actor, target id, reason) best-effort + structured log (NFR-O1/O2). Returns a German confirmation (`Konto … wurde gelöscht.`).
- **Tool lifecycle port** (`internal/tools`): NEW exported `Service.AnonymizeUserReferences(ctx, actorID, userID)` + repo — in one transaction: `UPDATE inspections SET inspector_id = $1 WHERE inspector_id = $2` and `UPDATE reinstatements SET actor_id = $1 WHERE actor_id = $2`, rewriting the target's references to the canonical sentinel `core.DeletedUserID` (fixed well-known uuid, const next to `DeletedUserDisplayName`). IDEMPOTENT (no matching rows → no-op). The `DisplayNameResolver` seam already maps the sentinel (absent from users) to "Deleted User". No schema change.
- **User lifecycle port** (`internal/user`): NEW `ports.Service.SoftDeleteAndArchive(ctx, actor *core.User, targetUserID, reason string) error` + repo — in ONE transaction: (a) copy the user's personal data into `dsgvo_deleted_accounts` (original_user_id, email, display_name, first_name, last_name, attributes, reason, deleted_by, created_at — a full snapshot incl. secrets? NO: snapshot excludes secrets); (b) set the user row state → `deleted` and scrub live personal fields (email → `deleted.<id>@deleted.local` placeholder to free the UNIQUE email for re-registration, password_hash → '', display_name/first/last → '', attributes → '{}', totp/otp/pending_email/must_change_password cleared); (c) `DELETE FROM login_attempts WHERE email = $original_email`. Re-login is permanently rejected: `user.State == StateActive` is the only authenticating state (auth.go:160) and sessions reject non-active (session.go:112).
- **Archive table** (migration `000030_dsgvo_account_deletion.{up,down}.sql`): NEW `dsgvo_deleted_accounts` (id uuidv7 PK, original_user_id uuid NOT NULL PLAIN no FK — mirrors the tool snapshot pattern, created_at timestamptz, deleted_by uuid, reason text NOT NULL, email/display_name/first_name/last_name/attributes, deleted_at timestamptz) + `ALTER TABLE users DROP CONSTRAINT`/re-add `state CHECK` to admit `'deleted'`. The archive is the "saved somewhere" — the account is never hard-deleted at deletion time.
- **On-demand purge** (`internal/dsgvo/core`): `Service.ListDeletedAccounts(ctx, actorID)` (permission `dsgvo.delete`) → archive rows newest-first, and `Service.PurgeDeletedAccount(ctx, actorID, archiveID)` — re-checks `dsgvo.delete`, loads the archive row, in ONE transaction deletes the archived row AND the (now-scrubbed) `users` tombstone (`DELETE FROM users WHERE id = $original_user_id` — this is the ONLY hard delete, admin-initiated on demand); CASCADEs any remaining sessions/reset tokens and auto-SET-NULLs `audit_log`/`admin_recovery` refs (000006/000009). Audits `dsgvo.purge` (actor, archive id) best-effort. A purge of an already-purged archive id → 404.
- **Admin HTTP** `internal/admin/adapters/http/dsgvo.go`: `POST /users/{id}/delete` (body `{ reason }`), `GET /users/deleted`, `DELETE /users/deleted/{archiveId}` — same any-of mount (`/api/v1/admin/dsgvo`, `[dsgvo.access_report, dsgvo.delete]`), orchestrator re-checks `dsgvo.delete` defense-in-depth. Uniform 200/400/403/404/401/500 + router 404/405 JSON envelopes.
  > **Review note (2026-09-18):** the delete endpoint verb is `POST /users/{id}/delete`, not `DELETE /users/{userId}` — the reason travels in the body and proxies strip DELETE bodies (the repo's `POST /{id}/archive`-style convention).
- **SPA** (`web/src/pages/admin/AdminDsgvoPage.tsx`): the 3.3 delete-tab placeholder becomes a two-step delete flow — user picker, the user's `display_name` typed EXACTLY + a mandatory non-empty Begründung, "Endgültig löschen" enabled only when both valid, German copy "Unumkehrbar · Audit-Pflicht", mismatch → inline error + blocked, `busy` during the call, success confirmation, 401→login, 403→leave module. A second section "Gelöschte Konten" (same tab, `dsgvo.delete`) lists archived accounts (name/email/date/reason) with a per-row "Jetzt endgültig löschen" purge action (window.confirm) and an empty-state note; purge success removes the row and confirms. Tab hidden for non-`dsgvo.delete` holders.
- **Tests:** orchestrator (delete matrix: permission/self/empty-reason/unknown/order/audit; list+purge: permission, purge cascades, already-purged 404), tool port (rewrite both tables + idempotent no-op), user port (archive snapshot + scrub + state + login_attempts + re-login rejection via a fresh login attempt), purge (archive + tombstone deleted, audit SET NULL), http (all statuses + router 404/405), composition mount, SPA (two-step gating, name mismatch, empty reason, confirm→success, purge flow, 401/403).

**Ask First:**
- None.

**Never:**
- NO hard delete at deletion time — the account is soft-deleted + archived; the only hard delete is the admin-initiated on-demand purge.
- No re-activation path from `deleted` (no approve/reject/deactivate touches it).
- No change to the inspection data model or the "Deleted User" rendering seam.
- No module writes another's SQL (AD-8 — the orchestrator composes the lifecycle ports + audit).
- No secrets (password hash / TOTP / OTP) in the archive snapshot or the audit detail.
- No report-surface changes (3.3 done).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| DELETE_OK | holder, existing non-self user, valid reason | 200 German confirmation; refs → "Deleted User"; account soft-deleted + archived; re-login blocked | n/a |
| DELETE_SELF | target == actor | 400 German | 400 |
| DELETE_EMPTY_REASON | trimmed reason empty | 400 German | 400 |
| DELETE_LONG_REASON | reason > 2000 runes | 400 German | 400 |
| DELETE_UNKNOWN | target user absent | 404 German | 404 |
| DELETE_GATED | caller lacks `dsgvo.delete` | 403 envelope, no data | 403 |
| ARCHIVE_OK | account deleted | `dsgvo_deleted_accounts` row holds the personal snapshot (no secrets); users row scrubbed, state `deleted` | n/a |
| LIST_DELETED | admin lists archived accounts | archive rows newest-first (name/email/date/reason) | 403 non-holder |
| PURGE_OK | admin purges an archived account | archive row + users tombstone hard-deleted (one tx); audit `dsgvo.purge` | 403/404 |
| PURGE_MISSING | archive id already purged | 404 German | 404 |
| DELETE_ANON | deleted user's inspections rendered | history/status report show "Deleted User" | n/a |
| DELETE_AUDIT | deletion + purge recorded | immutable audit rows; prior audit actor refs SET NULL on purge | best-effort |
| DELETE_401 | expired/revoked session | 401 envelope → login | 401 |
| SPA_TWO_STEP | delete tab, user picked | name + reason fields; "Endgültig löschen" enabled only when both valid | n/a |
| SPA_NAME_MISMATCH | typed name ≠ display_name | inline error, delete blocked | n/a |
| SPA_PURGE | archived account row purged | confirm → row removed + confirmation | inline German on error |

</frozen-after-approval>

## Code Map

- `internal/dsgvo/core/dsgvo.go` -- `DeleteAccount`, `ListDeletedAccounts`, `PurgeDeletedAccount`; sentinels `ErrDsgvoDeleteSelf`/`ErrDsgvoReasonInvalid`; `DeleteAccountResult{Message}`; `DeletedAccountRow` type.
- `internal/tools/ports/ports.go` + `core/inspections.go` -- `AnonymizeUserReferences`; `DeletedUserID` const; `queries.sql` `UpdateInspectionsInspector :exec` + `UpdateReinstatementsActor :exec`; transactional repo method.
- `internal/user/ports/ports.go` + `core` -- `SoftDeleteAndArchive` on `Service`+repo; `queries.sql` `GetUserFullForArchive :one` (no secrets), `InsertDeletedAccount :one`, `SetUserDeletedAndScrub :one`, `DeleteLoginAttemptsByEmail :exec`, `DeleteUserByID :exec` (purge), `GetDeletedAccount :one`, `DeleteDeletedAccount :exec`, `ListDeletedAccounts :many`; `user_writes_repo.go` transactional methods.
- `migrations/000030_dsgvo_account_deletion.{up,down}.sql` -- `dsgvo_deleted_accounts` table + `users.state` CHECK extended with `'deleted'`; `sqlc.yaml` schema source + README.
- `internal/admin/adapters/http/dsgvo.go` -- three handlers (delete/list/purge) + DTOs + reuse `mapDsgvoError`.
- `cmd/server/main.go` -- no new mount (same `/api/v1/admin/dsgvo`); orchestrator already wired with tool/user services (3.3).
- `web/src/pages/admin/AdminDsgvoPage.tsx` (+css, +test) -- delete tab two-step + "Gelöschte Konten" list + purge.
- `web/src/auth/users.ts` -- `deleteUserAccount`, `listDeletedAccounts`, `purgeDeletedAccount` clients.
- Tests -- orchestrator (delete/list/purge matrix), tool/user ports, http, mount, `AdminDsgvoPage.test.tsx`.

## Tasks & Acceptance

**Execution:**
- [x] Tool lifecycle port (sentinel const + rewrite queries + transactional method) -- backend
- [x] Migration 000030 (archive table + `deleted` state) + user SoftDeleteAndArchive/scrub/purge repo -- backend
- [x] Orchestrator DeleteAccount/ListDeletedAccounts/PurgeDeletedAccount + HTTP (delete/list/purge) + audit -- backend
- [x] SPA two-step delete tab + "Gelöschte Konten" purge list -- SPA
- [x] Tests -- orchestrator/tool/user/http/mount/SPA -- verification

**Acceptance Criteria:**
- Given a `dsgvo.delete` holder with a valid reason, when they delete a user, then the account is soft-deleted (re-login permanently blocked) and its personal data is ARCHIVED (never hard-deleted at deletion time), while every inspection/reinstatement reference renders "Deleted User" with all timestamps/results/items/OOS intact (FR-24/AD-8/FR-18).
- Given an archived account, when an admin triggers the on-demand purge, then the archive row and the users tombstone are hard-deleted in one transaction (FR-24).
- Given a deletion/purge, when it completes, then immutable audit entries record the operation (NFR-O2) and the events are structured-logged (NFR-O1).
- Given the two-step SPA flow, when the typed name does not match or the reason is empty, then "Endgültig löschen" stays disabled with an inline German error (UX-DR7/DR8).
- Given a non-holder or a self-deletion, when they attempt it, then the server answers 403 / 400 and no data is exposed (AD-6).

## Spec Change Log

- **Review patches (review 1, 2026-09-18):** `deleted` tombstones are NON-EXISTENT to the admin surface — `ListUsers` excludes them, `UpdateAdminUser` refuses a tombstone (409 German), and the DSGVO picker omits them (the "no re-activation path" rule is now enforced end-to-end); `DeleteAccount` reorders to resolve actor + verify target BEFORE the Tool rewrite (no mutation before all guards pass, nil-actor guard); self-deletion compares case-insensitively; the delete endpoint changed from `DELETE /users/{id}` (body) to `POST /users/{id}/delete` (proxy-safe); the purge hard-delete requires `state='deleted'` + RowsAffected checks (a corrupted archive row can never delete a live account); `ListDeletedAccounts` is capped at 500; the dead `actorID` param was dropped from `AnonymizeUserReferences`; `@deleted.local` registrations are rejected (reserved email); the SPA disables the form during `busy` (no selection race); re-login rejection is pinned by a real login attempt; `ListDeletedAccounts` newest-first is pinned with two rows; the 000030 down-migration limitation is documented. KEEP: the soft-delete+archive lifecycle, the on-demand purge as the sole hard delete, and the sentinel rewrite + "Deleted User" seam all stand as implemented.

## Design Notes

- **"Saved somewhere, purged on demand" (user decision 2026-09-18):** deletion NEVER hard-deletes — the account becomes a scrubbed `deleted` tombstone (login blocked) and its personal data moves to the `dsgvo_deleted_accounts` archive. The archive + tombstone are hard-deleted ONLY when an admin invokes the purge (the sole hard delete). This overrides the epic's original "personal data is purged at deletion time" — the right of erasure is served by the archived-then-purgeable lifecycle.
- **"Deleted User" needs no literal rewrite:** `inspector_id`/`actor_id` are FK-less plain uuids (AD-8); the rewrite to the `DeletedUserID` sentinel is explicit and auditable, and the resolver already maps the sentinel to "Deleted User".
- **Cross-module atomicity is orchestrator-ordered:** the Tool rewrite is idempotent and the User soft-delete/archive is one transaction; a failure between them converges on re-run. Each module owns its own transactional store write (AD-8).
- **Email placeholder frees the UNIQUE key:** the scrubbed tombstone's email becomes `deleted.<id>@deleted.local`, so the erased address can be re-registered while the tombstone remains addressable only by its id.

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -count=1 -p 1 ./cmd/... ./internal/...` -- expected: all pass incl. the delete/archive/purge matrix + cascade/anonymization integration
- `npx vitest run` in web/ -- expected: all pass incl. the delete-tab + purge cases
- `npm --prefix web run lint && npm --prefix web run typecheck && npm --prefix web run build` -- expected: clean

**Manual checks (if no CLI):**
- As an admin on DSGVO → Konto löschen: pick a user, type their name + reason → "Endgültig löschen"; the account appears under "Gelöschte Konten" with its data archived; its inspection history shows "Deleted User"; login fails; "Jetzt endgültig löschen" purges it for real.

## Suggested Review Order

**Orchestrator delete/purge (entry point)**

- `DeleteAccount` (guards before any mutation: self/unknown/reason/actor → Tool rewrite → soft-delete+archive → audit) + `ListDeletedAccounts`/`PurgeDeletedAccount`.
  [`dsgvo.go:300`](../../internal/dsgvo/core/dsgvo.go#L300)

**Lifecycle ports**

- Tool: the sentinel rewrite (idempotent, no dead actorID).
  [`dsgvo_deletion.go:40`](../../internal/tools/core/dsgvo_deletion.go#L40)

- User: `SoftDeleteAndArchive` (snapshot → scrub + `deleted` state → login_attempts) + purge guard (`state='deleted'` + RowsAffected).
  [`dsgvo_deletion.go:40`](../../internal/user/core/dsgvo_deletion.go#L40)

- The archive table + extended state CHECK (migration 000030).
  [`000030_dsgvo_account_deletion.up.sql:1`](../../migrations/000030_dsgvo_account_deletion.up.sql#L1)

**Admin surface + tombstone isolation**

- The three HTTP handlers (POST delete, GET list, POST purge).
  [`dsgvo.go:60`](../../internal/admin/adapters/http/dsgvo.go#L60)

- Tombstones non-existent: `ListUsers` excludes `deleted`, `UpdateAdminUser` refuses them.
  [`queries.sql:640`](../../internal/user/adapters/postgres/queries.sql#L640)

**SPA**

- The two-step delete tab + "Gelöschte Konten" purge list (busy-disabled form, picker omits tombstones).
  [`AdminDsgvoPage.tsx:460`](../../web/src/pages/admin/AdminDsgvoPage.tsx#L460)

**Tests**

- Delete/list/purge matrix + tombstone isolation + re-login rejection + ordering.
  [`dsgvo_test.go:60`](../../internal/dsgvo/core/dsgvo_test.go#L60)

- Postgres purge guard + archive/scrub + login-attempt cleanup.
  [`dsgvo_test.go:60`](../../internal/user/adapters/postgres/dsgvo_test.go#L60)