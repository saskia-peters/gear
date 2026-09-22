---
title: 'User Approval Workflow'
type: 'feature'
created: '2026-09-07'
status: 'done'
review_loop_iteration: 0
baseline_commit: '4cb690f39b582ef7361165dcd14a5ae70566d1fd'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Self-registered users land in `pending_approval` state (Story 1.3) but there is no admin surface to review and approve/reject them — the "Ausstehende Anträge" section on the Verwaltung landing is a static empty state, and a pending user can never reach `active`. Only vetted volunteers must gain access (FR-20).

**Approach:** Add the User Approval Workflow: a backend surface under `/api/v1/admin/users` (list pending requests; approve; reject) gated by the `users.approve` permission code (AD-6, FR-20), wired into the Story 2.3 admin shell's "Ausstehende Anträge" section on the Verwaltung landing (replacing the static empty state with a live pending list). Approve moves the user to `active` and seeds the default `helfende` role (AD-2); reject removes the pending record, prevents re-registration, and records an audit-log entry (NFR-O1/AD-8). Empty state remains "Keine ausstehenden Anträge" (UX-DR6/8).

## Boundaries & Constraints

**Always:**
- **Backend, permission-gated (AD-6/FR-20):** all three endpoints live under `/api/v1/admin/users` (inside the admin router from Story 2.1) and are gated by the `users.approve` permission code. A caller without it gets the uniform 403 hidden-existence behavior — no admin existence leak (FR-19).
- **List pending:** `GET /api/v1/admin/users/pending` returns pending-approval users with their submitted details — Vorname, Nachname, E-Mail, plus created_at and id. Sorted by created_at ascending (oldest first). Server-authoritative; the client only displays.
- **Approve:** `POST /api/v1/admin/users/{id}/approve` — moves the user `pending_approval → active`, seeds the default `helfende` role (AD-2) via `user_permission_groups` (idempotent — an already-active or already-in-helfende user is not duplicated). Only pending users can be approved; a non-pending or unknown id returns the uniform not-found/conflict error (no existence leak beyond what the admin already sees).
- **Reject:** `POST /api/v1/admin/users/{id}/reject` — removes the pending record so the user cannot register/log in again with that pending state (FR-20). Records an audit-log entry (NFR-O1/AD-8) with actor (admin), operation `user.reject`, detail containing the target email (never password material). Only pending users can be rejected.
- **Atomicity:** approve and reject each run in a transaction — the state transition and the role seed / audit write are all-or-nothing. No partial approvals.
- **Audit:** approve also records an audit entry (`user.approve`, actor + target email, severity normal) so both actions are traceable (NFR-O1). Follow the existing `InsertAuditEvent` best-effort pattern (failure logged, not rolled back) — but the transition itself must not be silently lost.
- **Empty state:** when no pending users exist the surface shows "Keine ausstehenden Anträge" (UX-DR6/8) — unchanged from Story 2.3.
- **Login gating:** a user approved to `active` can log in with their resolved permissions (AD-2/AD-6). This is already enforced by the existing login path (`canAuthenticate` requires `StateActive`); Story 2.4 verifies it, it does not re-implement it.
- **Client:** the "Ausstehende Anträge" section on the Verwaltung landing fetches the pending list via the authenticated admin API, renders each request as a row (Vorname, Nachname, E-Mail) with "Freigeben" / "Ablehnen" CTAs (EXPERIENCE.md Verwaltung — Start), shows German inline feedback (loading skeleton, empty state, success/error toasts), meets the a11y floor (≥48px targets, keyboard, focus order, SR announcements), and only renders for callers holding `users.approve`/`users.view` (Story 2.3 gating preserved).

**Ask First:**
- None.

**Never:**
- No client-side authorization — the server validates `users.approve` on every request (AD-2/AD-6).
- No password/sensitive material in any response or audit detail.
- No existence leak to callers without `users.approve` (FR-19) — the 403 body must not hint at the admin module.
- No changes to the registration flow or login flow beyond verification.
- No role assignment beyond the default `helfende` seed on approve (custom role assignment is Story 2.5/2.6).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| LIST_PENDING | admin with users.approve; 3 pending users | 200, array sorted oldest-first, each {id, vorname, nachname, email, created_at} | n/a |
| LIST_PENDING_EMPTY | no pending users | 200, empty array → client shows "Keine ausstehenden Anträge" | n/a |
| LIST_FORBIDDEN | caller without users.approve | Uniform 403, no admin hint (FR-19) | 403 hidden |
| APPROVE_VALID | pending user + admin (users.approve) | User → active; helfende role seeded; audit entry; login now succeeds (AD-2/6) | n/a |
| APPROVE_NONPENDING | user already active/deactivated | No state change; error (uniform) | 409/404-style, no leak |
| APPROVE_UNKNOWN | nonexistent id | Uniform not-found error | 404-style, no leak |
| APPROVE_IDEMPOTENT | pending user already in helfende | helfende row not duplicated | n/a |
| REJECT_VALID | pending user + admin | Pending record removed; audit entry (user.reject, target email); cannot log in / re-register with that pending state | n/a |
| REJECT_NONPENDING | user active/deactivated | No change; uniform error | 409/404-style |
| REJECT_UNKNOWN | nonexistent id | Uniform not-found error | 404-style |
| LOGIN_AFTER_APPROVE | approved user logs in | Login succeeds with resolved permissions (AD-2/6) | n/a |
| CLIENT_EMPTY | list returns [] | "Keine ausstehenden Anträge" empty state | n/a |
| CLIENT_ERROR | fetch fails | German inline error, no crash | inline error |

</frozen-after-approval>

## Code Map

- `internal/user/adapters/postgres/queries.sql` -- Add `ListPendingUsers` (:many, state='pending_approval', ORDER BY created_at) and `SetUserState` (:one or :exec, transition to active; also used later for deactivate) — or a dedicated `ApproveUser` query. Re-run `just sqlc-generate`.
- `internal/user/adapters/postgres/repository.go` -- Wire the new queries into the repository (`ListPendingUsers`, `ApproveUser`/`RejectUser`), the role-seed (`AddUserToGroup` already exists for helfende), and audit insertion (`InsertAuditEvent` already exists).
- `internal/user/core/approval.go` -- New small core file: `ListPending(ctx, actor)`, `ApproveUser(ctx, actor, userID)`, `RejectUser(ctx, actor, userID)` — actor admin-state/active checks, permission is enforced at the gateway (handler gate), transaction orchestration, `helfende` seed, audit writes. Keep the file small (god-class convention).
- `internal/user/core/service.go` / `internal/user/ports/ports.go` -- Add the three methods to the Service struct + inbound port.
- `internal/user/adapters/http/admin.go` -- Register `GET /users/pending`, `POST /users/{id}/approve`, `POST /users/{id}/reject` on the admin router (still under the Story 2.1 mount; the router itself is gated by `RequireAdminPermission` in the composition root — confirm the mount uses `users.approve` or add a per-route gate).
- `internal/user/adapters/http/admin_users.go` -- New small handler file with the three handlers (uniform envelope, German messages, 403/404/409 mapping). Keep separate (god-class convention).
- `web/src/pages/AdminPage.tsx` -- Replace the static pending section with a live `PendingApprovals` component: fetch pending list, render rows + Freigeben/Ablehnen CTAs, German inline feedback, empty state.
- `web/src/components/PendingApprovals.tsx` (+ `.module.css`) -- The live pending-approvals widget (fetch, list, approve/reject actions, inline states).
- Tests: `internal/user/core/approval_test.go` (approve/reject/list unit tests), `internal/user/adapters/postgres/repository_test.go` (integration: list/approve/reject/role-seed/audit), `internal/user/adapters/http/admin_users_test.go` (handler + 403/404/409 mapping), `web/src/components/PendingApprovals.test.tsx` + `web/src/pages/AdminPage.test.tsx` (live list, approve/reject, empty, error).

## Tasks & Acceptance

**Execution:**
- [x] `queries.sql` + `sqlc-generate` -- `ListPendingUsers` + `ApproveUser`(transition) queries -- persistence
- [x] `repository.go` -- wire new queries + role seed + audit -- repository layer
- [x] `core/approval.go` -- `ListPending`/`ApproveUser`/`RejectUser` service methods (transactional, helfende seed, audit) -- domain
- [x] `ports.go`/`service.go` -- expose the three methods on the inbound port -- module boundary
- [x] `admin.go`/`admin_users.go` -- register + implement the three admin endpoints gated by `users.approve` -- API
- [x] `core/approval_test.go` -- unit: approve/reject/list, non-pending, unknown, idempotent -- I/O matrix
- [x] `repository_test.go` -- integration: list order, approve→active+helfende+audit, reject→removed+audit, login-after-approve -- persistence verification
- [x] `admin_users_test.go` -- handler: 200/403/404/409 mapping, hidden-existence -- HTTP contract
- [x] `web/src/components/PendingApprovals.tsx` (+css) -- live pending widget (fetch/list/approve/reject/inline feedback) -- SPA
- [x] `web/src/pages/AdminPage.tsx` -- wire PendingApprovals into the approvals section (gating preserved) -- SPA integration
- [x] Web tests -- PendingApprovals + AdminPage: list, approve, reject, empty, error -- frontend matrix

**Acceptance Criteria:**
- Given one or more pending_approval users, when I open "Verwaltung — Start", then I see the pending requests (e.g. "Tim Müller — pending — self-registered") with Vorname, Nachname, E-Mail (FR-20/UX-DR8).
- Given I review a pending request, when I approve it, then the user moves to active and can log in (FR-20/FR-5), and is seeded with the default helfende role (AD-2).
- Given I reject a pending request, when I confirm rejection, then the pending record is removed and the user cannot register/log in again with that pending state (FR-20), and the rejection is recorded to audit logging (NFR-O1/AD-8).
- Given the approval surface is empty, when I open it, then the empty state reads "Keine ausstehenden Anträge" (UX-DR6/UX-DR8).
- Given a user was approved, when they attempt to log in, then login succeeds with their resolved permissions, per AD-2/AD-6.

## Spec Change Log

## Design Notes

- **Permission code `users.approve` gates the whole surface** (AD-6/FR-20): the admin router mount in the composition root currently uses `admin.recovery.approve` for the /api/v1/admin root. This story adds a per-route (or a dedicated users sub-mount) `RequireAdminPermission(..., "users.approve", ...)` gate so the approval endpoints are only reachable by holders of `users.approve`. A tools-only schirrmeister who reaches the landing sees the section gated out client-side and is denied server-side (Story 2.3 gating + FR-19).
- **Approve = transition + helfende seed, atomic.** Reuses the existing `AddUserToGroup` (helfende) inside the same transaction as the state flip; idempotent via ON CONFLICT / existence check. Login gating already exists (`canAuthenticate` requires StateActive) — verify, don't rebuild.
- **Reject = remove + audit, atomic.** The audit detail carries the target email (recoverable identity) — never a password hash or token. The `user.reject` operation name follows the audit table's existing vocabulary.
- **Client gating preserved:** PendingApprovals only mounts when the caller holds `users.approve`/`users.view` (Story 2.3 `hasPermission`), so a tools-only caller never sees the widget.

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go and web tests pass, 0 lint issues
- `just db-up` -- expected: migration state unchanged (no new migration needed; queries only)
- `curl` `GET /api/v1/admin/users/pending` as admin (users.approve) -- expected: 200 pending list; as helfende -- expected: uniform 403
- `curl` approve a pending user then log in as them -- expected: 200, user active, login succeeds with helfende set
- Live SPA check: register a user, view Verwaltung landing as admin, approve/reject with German feedback

## Suggested Review Order

**Entry point**

- The users sub-mount is gated by `users.approve` via the real gateway — the whole surface's authorization boundary.
  [`admin.go:45`](../../internal/user/adapters/http/admin.go#L45)

**Domain logic**

- Core service: ListPending/ApproveUser/RejectUser with defense-in-depth permission re-check, audit writes, uniform errors.
  [`approval.go:83`](../../internal/user/core/approval.go#L83)

- Permission + default-role constants (users.approve, helfende) — single source.
  [`approval.go:27`](../../internal/user/core/approval.go#L27)

**Persistence**

- Atomic approve: state flip + helfende seed in one tx, seed verified, rollback proven by test.
  [`repository.go:754`](../../internal/user/adapters/postgres/repository.go#L754)

- ListPendingUsers (oldest first, no secret material) / SetUserState / AddUserToGroup queries.
  [`queries.sql:437`](../../internal/user/adapters/postgres/queries.sql#L437)

**HTTP contract**

- List/Approve/Reject handlers with uniform 401/403/404 mapping (incl. malformed-UUID → 404).
  [`admin_users.go:32`](../../internal/user/adapters/http/admin_users.go#L32)

**SPA widget**

- PendingApprovals: live list, two-step reject confirm, server microcopy, a11y live region.
  [`PendingApprovals.tsx:45`](../../web/src/components/PendingApprovals.tsx#L45)

- Landing wiring: widget gated on users.approve only.
  [`AdminPage.tsx:26`](../../web/src/pages/AdminPage.tsx#L26)

**Tests**

- Core unit: list/approve/reject, non-pending/unknown, forbidden, audit best-effort.
  [`approval_test.go:36`](../../internal/user/core/approval_test.go#L36)

- Postgres integration: list order, approve→active+helfende+audit, rollback, reject→deactivated+audit, login-after-approve.
  [`repository_test.go:656`](../../internal/user/adapters/postgres/repository_test.go#L656)

- Web: list/approve/reject-confirm/cancel/empty/error/403/live-region.
  [`PendingApprovals.test.tsx:79`](../../web/src/components/PendingApprovals.test.tsx#L79)