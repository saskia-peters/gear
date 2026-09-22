---
title: 'Resolution of Active Permission Set'
type: 'feature'
created: '2026-09-06'
status: 'done'
review_loop_iteration: 0
baseline_commit: '9f078f580961835221a1684e8e09a6ed5b86da10'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Only one permission code (`admin.recovery.approve`) is seeded; the other 20 AD-12 base codes are absent, the base role → permission matrix (admin = all 21, helfende = dashboard.view + inspection.submit, …) is not wired, and the User module does not expose a first-class "resolve permission set" capability through its port for other modules (Tool, Admin) to consume (AD-2/AD-6).

**Approach:** Seed the full 21-code base permission series and the base-role→permission matrix, and expose `ResolvePermissionSet` on the User module's Service port — the additive union of group memberships + direct grants (AD-12), resolved live per request (immediate revocation, AD-2/AD-6/FR-21/FR-22) — so every protected action is authorized server-side against a current set and returns 403 when the code is missing.

## Boundaries & Constraints

**Always:**
- Seed all **21 base permission codes** from the architecture spine (AD-12): dashboard.view, inspection.submit, inspection.history.view, report.export, tool.reinstate, tools.manage, tool_types.manage, users.view, users.approve, users.manage, user_groups.manage, roles.create, roles.edit, roles.assign, qualifications.manage, dsgvo.access_report, dsgvo.delete, admin.recovery.approve, admin.settings.email, admin.settings.backup, schedules.manage.
- Seed the **base role → permission matrix** (AD-12): `admin` = all 21 codes; `helfende` = dashboard.view + inspection.submit; `schirrmeister` = dashboard.view, inspection.submit, tools.manage, tool_types.manage; `fuehrende` = dashboard.view, inspection.submit, inspection.history.view, report.export, tool.reinstate. (Matrix from the architecture spine role-matrix table.)
- `ResolvePermissionSet(ctx, user)` is a **Service method on the User module's port** (AD-2): returns the additive union (set-union, no precedence) of the user's permission-group memberships and direct grants; no deny permissions; never subtracts.
- Resolution is **live per request** — no cache — so a permission/membership change takes effect on the very next request (AD-2/AD-6/FR-21/FR-22).
- An action lacking its required code returns HTTP 403 (AD-6); the gateway already enforces this and is the single check site.
- The existing `ListPermissionsByUser` query (union + DISTINCT) is the persistence backbone; Story 2.2 adds the seed, the matrix wiring, the Service method, and verification.

**Ask First:**
- None.

**Never:**
- No deny/negative permissions (AD-12 additive only).
- No permission-set caching across requests (revocation must be immediate).
- No client-side authorization — only the server resolves and enforces.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| RESOLVE_ADMIN | admin user (admin role) | Resolved set = all 21 base codes | n/a |
| RESOLVE_HELFENDE | user in helfende role | Resolved set = dashboard.view + inspection.submit | n/a |
| RESOLVE_MULTI_ROLE | user in helfende + schirrmeister | Resolved set = union of both roles' codes (additive, no precedence) | n/a |
| RESOLVE_DIRECT_GRANT | user with a direct grant | Direct grant added to the union (user_permissions) | n/a |
| RESOLVE_NO_PERM | user with no roles/grants | Empty set | n/a |
| REVOCATION_IMMEDIATE | permission removed from a role, next request | Next resolution reflects the removal (no cache delay) | n/a |
| GATEWAY_403 | action requires a code the user lacks | Server returns HTTP 403 (AD-6) | 403 forbidden |

</frozen-after-approval>

## Code Map

- `migrations/000010_base_permissions.{up,down}.sql` -- Seed the 21 base permission codes (INSERT if not exists) and the base role → permission matrix (`permission_group_permissions` rows). Down removes only the rows this migration added.
- `internal/user/adapters/postgres/queries.sql` -- Reuse `ListPermissionsByUser` (already union + DISTINCT); add nothing unless the seed needs a helper. Re-run `just sqlc-generate` if queries change.
- `internal/user/core/auth.go` or a new `internal/user/core/permission.go` -- `ResolvePermissionSet(ctx, user *User) ([]string, error)` Service method wrapping the repo's `ListPermissionsByUser`; returns the resolved set.
- `internal/user/core/service.go` / `internal/user/ports/ports.go` -- Add `ResolvePermissionSet` to the Service interface + port.
- `internal/user/adapters/http/handler.go` -- Optionally a `GET /api/v1/auth/me/permissions` (auth-gated) exposing the caller's resolved set for the SPA/other modules to inspect (server-authoritative). Keep small.
- `internal/user/core/permission_test.go` -- Unit tests: admin = 21, helfende subset, multi-role union, direct grant, empty, revocation-immediate.
- `internal/user/adapters/postgres/repository_test.go` -- Integration test: after the seed migration, assert admin resolves all 21, helfende resolves dashboard.view + inspection.submit, a direct grant is included.
- `internal/platform/auth/gateway_test.go` -- Verify 403 on missing code remains correct with the full seed.

## Tasks & Acceptance

**Execution:**
- [x] `migrations/000010_base_permissions.{up,down}.sql` -- Seed 21 base permission codes + base role → permission matrix (AD-12) -- full base series + matrix
- [x] `internal/user/core/permission.go` -- `ResolvePermissionSet` Service method (additive union via `ListPermissionsByUser`) -- AD-2 port capability
- [x] `internal/user/ports/ports.go` / `service.go` -- Add `ResolvePermissionSet` to the Service interface + port -- module boundary
- [x] `internal/user/adapters/http/handler.go` -- `GET /api/v1/auth/me/permissions` (auth-gated) exposing the caller's resolved set -- server-authoritative inspection
- [x] `internal/user/core/permission_test.go` -- Unit tests: admin=21, helfende subset, multi-role union, direct grant, empty, revocation-immediate -- I/O matrix
- [x] `internal/user/adapters/postgres/repository_test.go` -- Integration: after seed, admin=21, helfende=2, direct-grant included -- persistence verification
- [x] `internal/platform/auth/gateway_test.go` -- Verify 403-on-missing-code with the full seed -- gateway coverage

**Acceptance Criteria:**
- Given the additive permission model (AD-12), when a user's permission set is resolved, then it is the additive union of group memberships + direct grants (no denies, never subtracts), mapping each action to one of the 21 base codes.
- Given the base role matrix, when a user belongs to a base role, then their set reflects the matrix (admin = all 21; helfende = dashboard.view + inspection.submit).
- Given a permission/membership change, when committed, then the effective set updates on the next request (revocation immediate, no cache) — AD-2/AD-6/FR-21/FR-22.
- Given the User module owns permission resolution, when another module needs authorization, then it consumes the set through the User module's port, and an action lacking its code returns HTTP 403 (AD-2/AD-6).

## Spec Change Log

## Design Notes

- **Seed, not a new query:** the `ListPermissionsByUser` union query already exists and is used by the gateway. Story 2.2's real work is seeding the 21-code series + the role matrix (currently only `admin.recovery.approve` → admin exists) and exposing a Service method.
- **Live resolution:** no caching; each call re-runs the union so revocation is immediate (AD-2/FR-21/FR-22).
- **Port for other modules:** `ResolvePermissionSet` on the User Service port is the sanctioned way Tool/Admin modules authorize (AD-2), complementing the gateway's per-request check.

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go and web tests pass, 0 lint issues
- `just db-up` -- expected: migration 000010 applies cleanly; `SELECT count(*) FROM permissions` = 21
- `just migrate-down && just migrate-up` -- expected: schema rebuilds cleanly
- `curl` `/api/v1/auth/me/permissions` as admin (21) and as helfende (2) -- expected: correct sets per I/O matrix

## Suggested Review Order

**Entry point**

- The seed migration is the heart of the story — it installs the 21-code series and the role matrix that every other layer reads.
  [`000010_base_permissions.up.sql:19`](../../migrations/000010_base_permissions.up.sql#L19)

**Resolution capability**

- Service method: additive union over the existing union+DISTINCT query, live per request, nil/empty-ID guarded.
  [`permission.go:19`](../../internal/user/core/permission.go#L19)

- Port contract: `ResolvePermissionSet` added to the User Service inbound port so Tool/Admin modules authorize through it.
  [`ports.go:55`](../../internal/user/ports/ports.go#L55)

- Endpoint wiring: `GET /me/permissions` registered inside the auth-gated group.
  [`handler.go:55`](../../internal/user/adapters/http/handler.go#L55)

- Handler: resolves the caller's live set, client-abort guard, 401/500 envelope mapping.
  [`handler.go:556`](../../internal/user/adapters/http/handler.go#L556)

**Down migration**

- Reverses only this migration's rows; preserves Story 1.1's `admin.recovery.approve`.
  [`000010_base_permissions.down.sql:1`](../../migrations/000010_base_permissions.down.sql#L1)

**Tests**

- Core unit tests: admin=21, helfende=2, multi-role union, direct grant, empty, revocation-immediate, nil/empty-ID.
  [`permission_test.go:63`](../../internal/user/core/permission_test.go#L63)

- Integration: seed content (all 21 present), role matrix incl. schirrmeister/fuehrende, dedup, cleanup.
  [`repository_test.go:1390`](../../internal/user/adapters/postgres/repository_test.go#L1390)

- Gateway: exact-code authorization — unrelated code missing still allowed; required code missing → 403.
  [`gateway_test.go:1`](../../internal/platform/auth/gateway_test.go#L1)

- Handler tests: Routes()-level happy path through real middleware, empty-set, 500 envelope, client-abort.
  [`handler_test.go:2934`](../../internal/user/adapters/http/handler_test.go#L2934)