---
title: 'Dual-Admin Credential Recovery'
type: 'feature'
created: '2026-09-06'
status: 'done'
review_loop_iteration: 0
baseline_commit: '82734c2dbc0e6aebde9c2f700b8ee5b05786dac4'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The two pre-seeded admin accounts are not protected by dual-control recovery: an admin who is locked out or forgets credentials cannot be safely restored, and a compromised single admin could otherwise take over the system.

**Approach:** Implement FR-27 dual-admin credential recovery: a locked-out admin (A) can request recovery, which creates a recovery request that ONLY the other admin (B) — gated by the `admin.recovery.approve` permission — can approve, with a mandatory Begründung and confirmation checkbox. On approval, admin A gains a single-use reset token to set a new password (FR-2). Every attempt is a high-severity immutable audit event (NFR-O2) and structured log (NFR-O1). Recovery of the LAST remaining admin is disabled (documented out-of-band manual procedure), and admins cannot self-reset via the FR-26 flow.

## Boundaries & Constraints

**Always:**
- There are exactly two pre-seeded admin accounts (credentials out-of-band, never in VCS — FR-27/AD-13).
- An admin CANNOT self-recover via the FR-26 forgot-password flow: the forgot-password request for an admin account does NOT issue a self-service reset token (it returns the uniform confirmation and flags/notifies — no actionable self-reset).
- Admin A requests recovery → a recovery request is created that ONLY admin B can act on, gated by the `admin.recovery.approve` permission (AD-12/AD-13).
- Admin B approves with a **mandatory Begründung (reason)** and a **confirmation checkbox** (no approval without both). On approval, admin A gains a single-use, hashed, time-limited reset token to set a new password (≥10 chars, FR-2).
- The approving admin (B) and the recovered admin (A) must be DIFFERENT accounts; an admin cannot approve their own recovery.
- Recovery of the **last remaining admin** is deliberately NOT reachable via any self-service path — it is a documented out-of-band manual bootstrap (both admins or a designated recovery sponsor), with a mandatory audit entry.
- Every recovery event (request, approval, denial, completion, last-admin attempt) is emitted to structured auth logging (NFR-O1) and audited as a high-severity immutable event (NFR-O2) via the `audit_log`.
- Recovery tokens reuse the existing `password_reset_tokens` mechanics (single-use, hashed at rest, 30-min expiry, invalidated on use), but are marked as admin-recovery tokens.
- All API errors use the uniform envelope; recovery routes are admin-only (`admin.recovery.approve`).

**Ask First:**
- None.

**Never:**
- No single-admin self-recovery (FR-27).
- No FR-26 self-reset for admin accounts (blocked).
- No recovery of the last admin via self-service.
- Never store/log/return the raw recovery token or passwords.
- No MFA bypass on the approving admin (admin B must be an authenticated `admin.recovery.approve` holder).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| RECOVERY_REQUEST | Admin A (locked out) requests recovery for their own account | A recovery request is created; only admin B can act on it; audit `admin.recovery.request` | n/a |
| RECOVERY_APPROVE_VALID | Admin B approves with Begründung + confirmation | Admin A gets a single-use hashed 30-min reset token; audit `admin.recovery.approve` (high-severity, actor=B, target=A) | n/a |
| RECOVERY_APPROVE_NO_REASON | Admin B approves without Begründung | Rejected, no token | 400 invalid_request |
| RECOVERY_APPROVE_NO_CONFIRM | Admin B approves without the confirmation checkbox | Rejected, no token | 400 invalid_request |
| RECOVERY_SELF_APPROVE | Admin A tries to approve their own recovery | Rejected (self-approval forbidden) | 403 forbidden |
| RECOVERY_NO_PERMISSION | A non-admin (or admin without `admin.recovery.approve`) tries to approve | 403 forbidden (permission-gated) | 403 forbidden |
| RECOVERY_LAST_ADMIN | The only remaining admin requests recovery | Self-recovery disabled; documented out-of-band manual procedure + mandatory audit | 400 last_admin_recovery_blocked |
| RECOVERY_SET_PASSWORD | Admin A opens the recovery link and sets a new password (≥10, match) | Password set (Argon2id), token invalidated, sessions revoked, audit `admin.recovery.complete` | n/a |
| ADMIN_FORGOT_SELF_RESET | An admin uses the FR-26 forgot flow on an admin email | Uniform confirmation; NO actionable self-reset token (blocked) | n/a |

</frozen-after-approval>

## Code Map

- `migrations/000009_admin_recovery.{up,down}.sql` -- Extend `password_reset_tokens` with recovery semantics: `recovery_target_admin boolean NOT NULL DEFAULT false` (or reuse an existing column); add an `approved_by_user_id uuid NULL` (the approving admin B) column. (The table is already User-owned and single-use/hashed/expiring — spine table 19.)
- `internal/user/adapters/postgres/queries.sql` -- sqlc queries: `CreateAdminRecoveryRequest` (creates a recovery-marked token row for admin A), `ListAdminRecoveryRequest`, `ApproveAdminRecovery` (sets approved_by), `SetUserPassword` (reuse), audit insert. Re-run `just sqlc-generate`.
- `internal/user/core/admin_recovery.go` -- Dual-admin recovery use-cases: `RequestAdminRecovery`, `ApproveAdminRecovery` (Begründung + confirm + self-approval guard + last-admin guard), `CompleteAdminRecovery` (reuse password-reset token consumption but for admin-recovery tokens).
- `internal/user/core/auth.go` / `reset.go` -- Block admin self-reset in the FR-26 forgot flow (admin accounts get no actionable self-reset token); recovery tokens marked `recovery_target_admin`.
- `internal/user/adapters/http/handler.go` -- `POST /api/v1/auth/admin/recovery/request`, `POST /api/v1/auth/admin/recovery/approve` (gated by `admin.recovery.approve`), reuse `/password/reset` for the completion. Map errors to the uniform envelope.
- `internal/platform/auth/gateway.go` -- Mount the approve route behind `RequirePermission("admin.recovery.approve")`.
- `cmd/server/main.go` -- Wire the admin-recovery service.
- `internal/user/core/admin_recovery_test.go` -- Unit tests for the full I/O matrix.
- `internal/user/adapters/http/handler_test.go` -- Handler tests (approve requires Begründung/confirm, self-approval 403, no-permission 403, last-admin 400).
- `internal/user/adapters/postgres/repository_test.go` -- Integration test: create request → approve → complete → audit rows.

## Tasks & Acceptance

**Execution:**
- [x] `migrations/000009_admin_recovery.{up,down}.sql` -- Extend `password_reset_tokens` with recovery-target + approver columns -- dual-admin recovery storage (FR-27/AD-13)
- [x] `internal/user/adapters/postgres/queries.sql` -- sqlc queries (create recovery request, list, approve, complete) + re-run `just sqlc-generate` -- type-safe persistence
- [x] `internal/user/core/admin_recovery.go` -- `RequestAdminRecovery` / `ApproveAdminRecovery` (Begründung+confirm, self-approval guard, last-admin guard) / `CompleteAdminRecovery` -- FR-27 core
- [x] `internal/user/core/reset.go` / `auth.go` -- Block admin self-reset via FR-26; recovery tokens are admin-marked -- no admin self-recovery
- [x] `internal/user/adapters/http/handler.go` -- Recovery request/approve endpoints (approve gated by `admin.recovery.approve`) -- API contract
- [x] `internal/platform/auth/gateway.go` & `cmd/server/main.go` -- Permission-gate the approve route; wire service -- composition root
- [x] `internal/user/` -- Unit/integration tests covering the full I/O matrix + high-severity audit -- automated verification
- [x] `web/src/pages/AdminRecoveryPage.tsx` -- German dual-admin recovery surface (request for A, review+approve for B with Begründung + checkbox) -- recovery UX
- [x] `web/src/App.tsx` -- Admin recovery route (admin-gated) -- navigable surface

**Acceptance Criteria:**
- Given an admin is locked out, when they request recovery, then a recovery request is created that only the other admin can act on, gated by `admin.recovery.approve` (FR-27).
- Given admin B reviews the request, when B approves, then they must provide a mandatory Begründung and a confirmation checkbox, and on approval admin A gains a single-use reset token to set a new password (≥10 chars, FR-2), recorded as a high-severity immutable audit event (NFR-O2).
- Given the last remaining admin, when recovery is requested, then self-recovery is disabled (documented out-of-band manual procedure) with a mandatory audit trail (FR-27/AD-13).
- Given an admin is locked out, when they use the FR-26 forgot flow, then they cannot self-reset via that flow (FR-27 overrides FR-26 for admins).

## Spec Change Log

- 2026-09-06 (Story 1.10 implementation): implemented FR-27 dual-admin credential recovery — admin A requests recovery, admin B approves (gated by `admin.recovery.approve`) with a mandatory Begründung + confirmation checkbox, A gains a single-use hashed 30-min reset token to set a new password (FR-2); last-admin self-recovery blocked; admin FR-26 self-reset blocked; every event audited (NFR-O2) + structured-logged (NFR-O1). Migration 000009 extends `password_reset_tokens` (recovery_target_admin, approved_by_user_id) and `audit_log` (operation_detail, severity).

- **2026-09-06 — Review-loop fixes (frozen contract untouched):** (1) **Dual-control enforced** — `requested_by_user_id` recorded; the requester can no longer approve their own request (a single admin cannot reset another admin). (2) **Deny flow** added (`admin.recovery.deny` with reason). (3) **Begründung + metadata persisted** in the immutable audit trail via new `operation_detail` + `severity` columns. (4) **Pending-request review** surface (`GET /admin/recovery/pending`) wired into the frontend. (5) **Fresh token expiry** on approval (30-min from approve time). (6) **FR-2 password validation before token consumption** (policy violation no longer burns the single-use token). (7) **Approve-time defense-in-depth** (approver active + permission, target active, last-admin re-check). (8) **MFA step-up** for an MFA-enabled approving admin. (9) **High-severity audit** (recovery rows written `severity='high'`). (10) **Seed verified** (`admin.recovery.approve` present + granted to admins). (11) **No raw token in a clickable URL** — rendered as a copyable input, submitted via POST body. (12) **Permission-gated approve route** pinned by a composition/router test (401/403/200). (13) `ListAdminRecoveryRequest` excludes `password_hash`. (14) `/password/reset` fallback propagates FR-2 policy errors instead of swallowing them as `invalid_token`. (15) Genuine persistence errors propagate (only `ErrNoRows` maps to invalid-token).

## Design Notes

- **Recovery token reuse:** admin recovery rides the existing `password_reset_tokens` table (single-use, hashed at rest, 30-min expiry, invalidated on use — spine table 19). A `recovery_target_admin` marker distinguishes admin-recovery tokens from ordinary FR-26 forgot tokens; an admin-recovery token is only consumable through the recovery completion path.
- **Self-approval & last-admin:** the approving admin must differ from the target; if the target is the last `active` admin, self-recovery is blocked and the documented out-of-band procedure applies.
- **High-severity audit:** recovery events are written with distinct operation codes (`admin.recovery.request`, `admin.recovery.approve`, `admin.recovery.deny`, `admin.recovery.complete`, `admin.recovery.last_admin_blocked`) in the immutable `audit_log` (NFR-O2).

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go and web tests pass, 0 lint issues
- `just db-up` -- expected: migration 000009 applies cleanly
- `just migrate-down && just migrate-up` -- expected: schema rebuilds cleanly
- `curl` recovery flow (A requests → B approves with Begründung+confirm → A sets new password → audit rows) -- expected: 200/400/403 per I/O matrix

## Suggested Review Order

**Dual-Admin Recovery Core**

- request flow with requester stamping and last-admin guard
  [`admin_recovery.go:212`](../../internal/user/core/admin_recovery.go#L212)

- approval flow: Begründung + confirm + MFA step-up + fresh expiry + deny
  [`admin_recovery.go:305`](../../internal/user/core/admin_recovery.go#L305)

- completion: FR-2 validation before token consumption, session revocation
  [`admin_recovery.go:506`](../../internal/user/core/admin_recovery.go#L506)

**Schema & Audit**

- recovery columns (requested_by/approved_by/recovery_target_admin) + audit detail/severity
  [`000009_admin_recovery.up.sql:23`](../../migrations/000009_admin_recovery.up.sql#L23)

**HTTP & Routing**

- permission-gated request/approve/deny/pending endpoints
  [`handler.go:58`](../../internal/user/adapters/http/handler.go#L58)

- real approve route gated by RequirePermission (401/403/200 pinned)
  [`composition_test.go:70`](../../internal/platform/auth/composition_test.go#L70)

**Frontend**

- German recovery surface: request, review pending, Begründung + checkbox, copyable token
  [`AdminRecoveryPage.tsx:59`](../../web/src/pages/AdminRecoveryPage.tsx#L59)

**Automated Verification**

- full recovery I/O matrix incl. dual-control, MFA step-up, and audit
  [`admin_recovery_test.go:1`](../../internal/user/core/admin_recovery_test.go#L1)