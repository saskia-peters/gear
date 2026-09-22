---
title: 'Admin Rework Effort 1 — Model & Backend'
type: 'feature'
created: '2026-09-08'
status: 'done'
review_loop_iteration: 0
baseline_commit: '49bab5d6dea7d6cfb8a1dfa48f77f7a8e7769062'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The admin surfaces (Stories 2.4–2.7) work in isolation but are not "glued together": (a) permissions can only be granted by individual role or direct grant — user-groups (teams) grant no access, so "role inherited via a team" is impossible; (b) qualification valid-until is vocabulary-level, not per-user, and fuehrende/schirrmeister cannot assign qualifications at all; (c) the permission set for reading the user directory is admin-only.

**Approach:** Rework the permission and qualification model per the human's decisions. This is Effort 1 (model + backend); the spreadsheet/UI polish is Effort 2. Specifically:
- **Three-way additive permission resolution**: a user's set = individual roles ∪ roles inherited via user-groups ∪ direct grants. New `user_group_permission_groups` join table; `ListPermissionsByUser` gains a third UNION branch. Resolution stays live per request (FR-21).
- **New permission `users.qualifications.manage`** (assign/revoke qualifications + per-user valid-until), granted to fuehrende + schirrmeister + admin.
- **`users.view` granted to fuehrende + schirrmeister** so they can open the user directory (read-only except qualifications).
- **Per-user qualification `expires_at`**: `user_qualifications.expires_at` (nullable). "Unbegrenzt gültig" qualification checkbox → no per-user expiry needed; fixed qualifications → per-user `expires_at` REQUIRED at assignment. Status derivation reads per-assignment `expires_at` first, then vocabulary `expires_at`.
- Backend: user-group role assignment endpoints, per-user qualification assignment with `expires_at`, `?status=` filter on the user list, resolved-permission provenance on the user detail, `qualificationStatus` update.

## Boundaries & Constraints

**Always:**
- **Migration 000013** (single migration, four changes):
  1. `user_group_permission_groups` (user_group_id → user_groups, permission_group_id → permission_groups, both FK, PK(user_group_id, permission_group_id)).
  2. `user_qualifications.expires_at timestamptz NULL` (per-user valid-until).
  3. Seed new permission `users.qualifications.manage`.
  4. Grants: `users.view` + `users.qualifications.manage` to `fuehrende` and `schirrmeister`; `users.qualifications.manage` to `admin`. (Admin already holds `users.view`.)
- **Resolution (AD-12, additive, no precedence, no subtraction):** `ListPermissionsByUser` returns the union of
  1. individual roles: `user_permission_groups` → `permission_group_permissions`
  2. **inherited via user-groups:** `user_group_members` → `user_group_permission_groups` → `permission_group_permissions`
  3. direct grants: `user_permissions`
  Deduplicated (DISTINCT), ORDER BY code, live per request (never cached — FR-21/FR-22).
- **User-group roles:** `GET/POST /api/v1/admin/user-groups/{id}/roles` (replace the group's role set atomically; transactional delete+insert, Story 2.5 lesson — separate statements, never a data-modifying CTE). Gated by `user_groups.manage` (defense-in-depth re-check in core). Audit write on role-set change. Unknown group → 404. Removing a role from a group revokes it from all members on the next request (live resolution).
- **`users.qualifications.manage`:** gates assign/revoke of a qualification on a user AND editing a user's per-qualification `expires_at`. Granted to fuehrende, schirrmeister, admin.
- **`users.view`:** gates reading the user list/detail. Granted to fuehrende, schirrmeister, admin. For fuehrende/schirrmeister the detail is READ-ONLY except the qualification assignment/`expires_at` surfaces (`users.manage`/`users.approve` remain admin-only for edits/deactivation/approval).
- **Per-user valid-until (human decision A):**
  - Qualification has a "Unbegrenzt gültig" checkbox → `expiry_kind='unlimited'` → assignments need no `expires_at`, never expire.
  - Qualification not "unbegrenzt" → fixed validity → assigning to a user REQUIRES a per-user `expires_at` (400 if missing).
  - Editing the qualification's vocabulary `expires_at` later affects only NEW assignments; existing per-user `expires_at` values are frozen.
  - `qualificationStatus` reads the per-assignment `expires_at` FIRST (if set), then the vocabulary `expires_at`; `unlimited` → `Unbegrenzt` always.
- **User list status filter:** `ListUsers` accepts an optional `status` filter (`active`|`pending_approval`|`deactivated`); absent = all. Server-authoritative.
- **Resolved-permission provenance:** the user detail includes the RESOLVED permission set, each permission annotated with its source(s) — role name, user-group name, or "direct". Multi-source permissions list all sources (e.g. `tools.manage` from both a role and a team). This powers Effort 2's "Alle Berechtigungen" view.
- **Audit (NFR-O1):** group-role assignment (`user_group.roles.assign`), qualification assignment/revoke (`qualification.assign`/`qualification.revoke`), and valid-until edit (`qualification.valid_until.update`) all audited (best-effort like existing patterns).
- **Doc sync:** `ARCHITECTURE-SPINE.md` updated: user-groups CAN grant access when roles are assigned; new codes `users.qualifications.manage`; `users.view` now held by fuehrende/schirrmeister.

**Ask First:**
- None (decisions A–D, Q1/Q2/1/2 all resolved by the human).

**Never:**
- No deny permissions, no subtraction (AD-12 additive only).
- No client-side authorization — the server gates every endpoint (AD-2/AD-6).
- No permission-set or qualification caching (immediate revocation, AD-2/FR-21/FR-22).
- No data-modifying CTE for delete-then-insert (Story 2.5 lesson).
- No vocabulary CRUD access for fuehrende/schirrmeister (`qualifications.manage` stays admin-only).
- No per-user `expires_at` for `unlimited` qualifications.
- No changes to already-committed migrations.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| RESOLVE_INDIVIDUAL | user holds helfende role directly | Set includes helfende's codes | n/a |
| RESOLVE_VIA_GROUP | user in "Gruppe Ost"; group holds schirrmeister-like role | Set includes that role's codes (inherited) | n/a |
| RESOLVE_UNION | user: individual role + team role + direct grant, overlapping codes | Deduplicated union (code once) | n/a |
| RESOLVE_REVOKE_VIA_GROUP | role removed from a group | Member's next resolution drops those codes (no cache) | n/a |
| GROUP_ROLE_ASSIGN | admin assigns roles to a group | Group role set replaced atomically; audit | n/a |
| GROUP_ROLE_REMOVE | admin removes a role from a group | Gone from group; members lose it next request | n/a |
| GROUP_ROLE_UNKNOWN | nonexistent group id | Uniform 404 | 404 not_found |
| GROUP_ROLE_FORBIDDEN | caller without user_groups.manage | Uniform 403 (FR-19) | 403 hidden |
| QUAL_ASSIGN_UNLIMITED | assign an "unbegrenzt" qual to a user | OK, no expires_at, status Unbegrenzt forever | n/a |
| QUAL_ASSIGN_FIXED_MISSING | assign a fixed qual without expires_at | 400 invalid (valid-until required) | 400 |
| QUAL_ASSIGN_FIXED_OK | assign fixed qual with expires_at | OK; status derives from per-user expires_at | n/a |
| QUAL_EDIT_VALID_UNTIL | fuehrende/schirrmeister/admin edit a user's expires_at | Persists; status updates next request | n/a |
| QUAL_EDIT_FORBIDDEN | caller without users.qualifications.manage | Uniform 403 | 403 |
| QUAL_UNLIMITED_NEVER_EXPIRES | unlimited qual, past date in vocab expires_at | Status Unbegrenzt (vocab date ignored for unlimited) | n/a |
| QUAL_STATUS_EXPIRED | fixed, per-user expires_at in the past | Abgelaufen | n/a |
| USER_LIST_STATUS | ?status=active | Only active users | n/a |
| USER_LIST_ALL | no status param | All users | n/a |
| DETAIL_PROVENANCE | user detail fetch | Resolved set with source per permission (role/team/direct, multi-source) | n/a |
| FUEHRENDE_READONLY | fuehrende opens user detail | Can view all; edit ONLY qualifications+expires_at (no edit/deactivate/approve) | n/a |

</frozen-after-approval>

## Code Map

- `migrations/000013_admin_rework.{up,down}.sql` -- `user_group_permission_groups`; `user_qualifications.expires_at`; seed `users.qualifications.manage`; grants (fuehrende/schirrmeister get `users.view` + `users.qualifications.manage`, admin gets `users.qualifications.manage`). Down reverses all four.
- `internal/user/adapters/postgres/queries.sql` -- Update `ListPermissionsByUser` (third UNION branch); add `ListUserGroupRoles`/`ReplaceUserGroupRoles` (delete+insert), `ListGroupRolesByUser` (provenance), `UpdateUserQualificationExpiry`, `ListQualifications` stays; `ListUsers` gains optional `$status` filter; add per-assignment `expires_at` to `ListUserQualifications`/`CreateQualification`/`UpdateQualification` as needed. Re-run `just sqlc-generate`.
- `internal/user/adapters/postgres/groups.go` / `qualifications.go` / `users_admin.go` -- New repo methods: `ReplaceUserGroupRoles`, `ListUserGroupRoles`, `AssignQualification` (with expires_at), `UpdateUserQualificationExpiry`, provenance query. Keep god-class small — split new files if they grow.
- `internal/user/core/` -- New `rework.go` (or extend `roles.go`/`qualifications.go`/`users_admin.go`): `AssignUserGroupRoles`, `ListUserGroupRoles`, `AssignUserQualification` (with expires_at), `UpdateUserQualificationExpiry`; update `qualificationStatus` to per-assignment-first; provenance assembly; status filter plumbing; `users.qualifications.manage` + `users.view` defense-in-depth re-checks; sentinel errors + German messages; audit writes.
- `internal/user/core/service.go` / `internal/user/ports/ports.go` -- Expose new methods + types.
- `internal/user/adapters/http/admin.go` / new handler files -- `GET/POST /user-groups/{id}/roles`; user-qualification assignment/expiry endpoints; `?status=` on `ListUsers`; provenance in the user-detail response.
- `ARCHITECTURE-SPINE.md` -- Update matrix (users.view in fuehrende/schirrmeister; users.qualifications.manage; user-groups grant access), AD-12 note, qualification note.
- Tests: core unit (all matrix rows), postgres integration (resolution union incl. revoke-via-group; group-role replace; qual assign/expiry rules; provenance; status filter), http handler/gate contracts (403/404/400), web: update `users.ts`/`roles.ts` types + any affected SPA tests.

## Tasks & Acceptance

**Execution:**
- [x] `migrations/000013_admin_rework.{up,down}.sql` -- join table + expires_at + new code + grants -- schema
- [x] `queries.sql` + `sqlc-generate` -- third UNION branch, group-role replace, qual expiry, status filter, provenance -- persistence
- [x] repo methods -- group-role replace, qual assign/expiry, provenance -- repository
- [x] core methods + `qualificationStatus` update + provenance + gates -- domain
- [x] `ports.go`/`service.go` -- expose new methods -- module boundary
- [x] http handlers + routes -- group-role, qual assign/expiry, status filter, provenance -- API
- [x] `ARCHITECTURE-SPINE.md` -- matrix + AD-12 + qualification notes -- docs
- [x] core unit tests -- I/O matrix rows -- verification
- [x] postgres integration tests -- resolution union/revoke, group-role, qual expiry, provenance -- persistence verification
- [x] http handler/gate tests -- 400/403/404 contracts -- HTTP contract
- [x] web types/tests -- updated to new codes/shapes -- SPA sync

**Acceptance Criteria:**
- Given the additive permission model, when a user's permission set is resolved, then it is the union of individual roles, roles inherited via user-groups, and direct grants (no precedence, no subtraction), live per request.
- Given a user-group with assigned roles, when a user belongs to it, then they inherit those roles' permissions immediately; removing a role from the group revokes it on the next request (FR-21).
- Given an admin edits a user-group's roles, when saved, then the role set is replaced atomically, audited, and effective immediately.
- Given fuehrende/schirrmeister/admin, when they open the user directory, then they see it read-only (users.view); and they can assign/revoke qualifications and edit per-user valid-until (users.qualifications.manage).
- Given a qualification marked "unbegrenzt gültig", when assigned to a user, then no per-user expires_at is needed and it never expires.
- Given a qualification with fixed validity, when assigned to a user, then a per-user expires_at is required and its status derives from it.
- Given a user detail fetch, then the response includes the resolved permission set with per-permission source(s).

## Spec Change Log

- **Review fixes applied (2026-09-08):** `UpdateUserQualificationExpiry` now rejects a per-assignment expiry on an `unlimited` qualification (never expires rule); `AssignQualificationToUser` uses a targeted `GetQualificationExpiryKindByID` lookup instead of scanning the whole vocabulary; re-assigning an already-assigned qualification UPDATES the per-assignment `expires_at` (`ON CONFLICT DO UPDATE`) instead of silently no-oping; `RevokeQualificationFromUser` returns 404 for an unassigned pair (execrows) consistent with the expiry update; added a `permission_group_id` index on `user_group_permission_groups`; `AssignUserGroupRolesHandler` rejects a missing `role_ids` field (400) instead of silently clearing all roles; wrong microcopy for an unknown role replaced with `MsgRoleNotFound`; date-only `YYYY-MM-DD` `expires_at` accepted (nullableTime); stale `DeleteUserGroup` comment corrected; dead `QualificationName` field removed; migration idempotency comment corrected; tests added for status filter (core+HTTP), unknown-role/empty-body/404/unlimited-reject branches, date-only input, admin's new grant, and provenance-vs-enforcement divergence.

## Design Notes

- **Human decisions locked:** three-way union; `users.view` + `users.qualifications.manage` for fuehrende/schirrmeister; per-user `expires_at` (required for fixed, checkbox for unlimited); Effort 1 = backend, Effort 2 = UI.
- **Provenance is Effort 2's data:** the resolved-permission-with-source is computed server-side here (Effort 1) so Effort 2's "Alle Berechtigungen" view is a pure render.
- **`users.view` grant is a matrix change** (fuehrende/schirrmeister gain read access to the directory). The Benutzer surface gate in the SPA nav/route uses the existing `users.*` codes — Effort 2 adjusts the visible actions; Effort 1 makes the backend honor read-only-for-non-admin.
- **`qualificationStatus` precedence:** per-user `expires_at` (if set) → vocabulary `expires_at` (for fixed) → `Unbegrenzt` (for unlimited). The existing Story 2.6 boundary test (exact instant) must keep passing — reuse `!before` for the expired check.

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go and web tests pass, 0 lint issues
- `just migrate-up` -- expected: migration 000013 applies cleanly; `\d user_group_permission_groups`; `\d user_qualifications` shows expires_at; `SELECT code FROM permissions WHERE code='users.qualifications.manage'`
- `SELECT g.name FROM permission_groups g JOIN ... WHERE p.code IN ('users.view','users.qualifications.manage')` -- expected: fuehrende, schirrmeister, admin hold both; admin already had users.view
- `curl` resolve permissions for a user in a team with a role -- expected: inherited codes present; remove role from team -- expected: gone next request
- `curl` assign a fixed qual without expires_at -- expected: 400; with expires_at -- expected: OK; edit expires_at as fuehrende -- expected: OK; as a user without the code -- expected: 403

## Suggested Review Order

**Entry point**

- The domain methods that glue the surfaces together: user-group role assignment + per-user qualification assignment with the fixed-vs-unlimited rule, gates, audit.
  [`rework.go:49`](../../internal/user/core/rework.go#L49)

- The three-way resolution query (individual roles + team roles + direct grants).
  [`queries.sql:104`](../../internal/user/adapters/postgres/queries.sql#L104)

**Schema**

- Migration 000013: join table + index, per-user expires_at, new code + grants.
  [`000013_admin_rework.up.sql:1`](../../migrations/000013_admin_rework.up.sql#L1)

**HTTP contract**

- Group-role + per-user qualification handlers (nullableTime date handling, empty-body guard, uniform 400/403/404).
  [`admin_rework.go:91`](../../internal/user/adapters/http/admin_rework.go#L91)

- Users sub-mount gate widened to `users.qualifications.manage`; group-roles routes.
  [`admin.go:49`](../../internal/user/adapters/http/admin.go#L49)

**Persistence**

- Per-user qualification assignment/revoke/expiry (targeted expiry-kind lookup, ON CONFLICT UPDATE, execrows 404).
  [`qualifications.go:253`](../../internal/user/adapters/postgres/qualifications.go#L253)

- Group-role replace + provenance assembly.
  [`users_admin.go:573`](../../internal/user/adapters/postgres/users_admin.go#L573)

- Status-filtered user list.
  [`users_admin.go:342`](../../internal/user/core/users_admin.go#L342)

**Tests**

- Core unit: group-role list/assign/revoke, qual assign (unlimited/fixed/missing/unknown/reassign/revoke-unassigned), expiry edit (unlimited reject), read-only fuehrende, status filter.
  [`rework_test.go:1`](../../internal/user/core/rework_test.go#L1)

- Postgres integration: three-way union + revoke-via-group, group-role replace, qual expiry, provenance-vs-enforcement, admin grant.
  [`rework_test.go:29`](../../internal/user/adapters/postgres/rework_test.go#L29)

- HTTP: group-role/qual handler contracts, status filter, date-only, empty-body, 404/400 branches.
  [`admin_rework_test.go:1`](../../internal/user/adapters/http/admin_rework_test.go#L1)