---
title: 'User & Group Administration'
type: 'feature'
created: '2026-09-07'
status: 'done'
review_loop_iteration: 0
baseline_commit: '915674f44238d6c64766bc4f686e0de1daf5394b'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Admins cannot manage the volunteer directory. There is no user list, no way to create/edit/deactivate a user, no user-group (team) management, and no way to add direct permission grants or view a user's qualifications. The "Benutzer" surface (Story 2.3) is a placeholder, and the spine tables `user_groups`, `user_group_members`, `qualifications`, `user_qualifications` do not exist yet.

**Approach:** Add User & Group Administration: a migration (000012) creating the four missing spine tables; backend under `/api/v1/admin/users` for listing, creating, editing, and deactivating users; user-group (team) CRUD + membership assignment; direct permission grants; and the "Benutzer" SPA surface with a user list (status aktiv/pending/deaktiviert), a user detail (roles, user groups, direct grants, qualification view), a create/edit form, and deactivation with confirmation. Every change takes effect immediately on the next request because permission/qualification resolution is live per request (AD-2/FR-21). Organisational user groups grant **no** permission (AD-12) — only the permission-group/direct-grant path affects access.

## Boundaries & Constraints

**Always:**
- **Migration 000012:** creates `user_groups` (id, name unique, description, created_at), `user_group_members` (user_id, user_group_id, PK), `qualifications` (id, name, description, expiry_kind, expires_at? — per spine table 9), `user_qualifications` (user_id, qualification_id, assigned_at, PK). All FK to the owning User module. Down drops only these four tables. Follows the spine table shapes (rows 261–262, 268–269) and AD-3/AD-11 (each table owned by User).
- **Backend, permission-gated (AD-6/FR-19):** all user-management endpoints live under `/api/v1/admin/users` (the Story 2.4 sub-mount already gates on `users.approve`; the user-management surface is gated by the broader `users.manage`/`users.view` set — reuse the `RequireAnyPermission` middleware from Story 2.5). A caller with no relevant `users.*` code gets the uniform 403 hidden-existence envelope (FR-19).
- **List users:** `GET /api/v1/admin/users` returns all users with `{id, vorname, nachname, email, status(aktiv/pending/deaktiviert)}`, server-authoritative. Gated by `users.view` (or any users.* code). No secret material.
- **User detail:** `GET /api/v1/admin/users/{id}` returns the user's profile + roles (permission groups), user groups (teams), direct permission grants, and qualification assignments (with per-qualification status). Gated by `users.view`.
- **Create user:** `POST /api/v1/admin/users` (name, email, optional role/group/direct-grant assignments) creates an account. Gated by `users.manage`. Email unique → uniform 409. New accounts default to a chosen status (active or pending) — admin-created users are typically `active` (they have credentials provisioned out-of-band) but the form allows pending.
- **Edit user:** `PUT /api/v1/admin/users/{id}` updates profile fields and replaces the assignment sets (roles via `user_permission_groups`, user groups via `user_group_members`, direct grants via `user_permissions`) atomically — delete-then-insert in one transaction (the Story 2.5 `ReplaceGroupPermissions` lesson: do it as separate DELETE + INSERT statements inside one transaction, never a data-modifying CTE). Gated by `users.manage`. Unknown id → 404.
- **Deactivate:** `POST /api/v1/admin/users/{id}/deactivate` — sets status to `deactivated`; the user cannot authenticate at all ("→ Sofort kein Login", FR-21). Requires confirmation from the client. Audited (NFR-O1). Gated by `users.manage`. Only active users can be deactivated; already-deactivated/pending → uniform 409/404.
- **User groups (teams):** `GET/POST /api/v1/admin/user-groups` + `POST /api/v1/admin/user-groups/{id}/members` (assign/remove members) — or fold into the user edit flow. `user_groups.manage` gates the endpoints; user groups are **organisational only** and grant no permission (AD-12) — enforced by the resolution query (never joins user_groups into the permission set, already true).
- **Direct grants:** the user detail can add/remove direct permission grants (`user_permissions`) — this is the additive direct-grant path of AD-12, separate from roles.
- **Qualification view:** the user detail shows the user's qualification assignments with status (Gültig / Bald ablaufend / Abgelaufen / Unbegrenzt) — the assignment editing itself is Story 2.7 (Qualification Management); Story 2.6 only **displays** them on the user detail.
- **Immediate effect (AD-2/FR-21):** live per-request resolution means a role/group/direct-grant change or a deactivation takes effect on the very next request. Verify with tests (no cache); do not re-implement resolution.
- **SPA "Benutzer" surface (UX-DR6/8/9):** replace the AdminBenutzerPage placeholder: user list (status badges aktiv/pending/deaktiviert), a user detail (roles + user groups + direct grants + qualification view), a create/edit form, and a deactivate flow with a confirmation step ("→ Sofort kein Login"). German inline feedback, ≥48px targets, keyboard/focus/SR, loading skeletons, empty states. Gated by `users.*` client-side (Story 2.3 nav).

**Ask First:**
- None.

**Never:**
- No client-side authorization — the server gates every endpoint (AD-2/AD-6).
- No secret material (password hash, tokens) in any list/detail response.
- No existence leak to callers without a `users.*` code (FR-19).
- No data-modifying CTE for delete-then-insert (Story 2.5 lesson) — use separate statements in one transaction.
- No permission-set or qualification caching (immediate revocation, AD-2/FR-21/FR-22).
- Organisational user groups must never affect the resolved permission set (AD-12).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| LIST_USERS | admin (users.view) | 200, all users with status badges (aktiv/pending/deaktiviert) | n/a |
| LIST_FORBIDDEN | caller with no users.* code | Uniform 403, no admin hint (FR-19) | 403 hidden |
| DETAIL_VALID | admin opens a user | 200, profile + roles + user groups + direct grants + qualifications | n/a |
| DETAIL_UNKNOWN | nonexistent id | Uniform 404 | 404 not_found |
| CREATE_VALID | {name, email, role} | 201, user created (active or pending) | n/a |
| CREATE_DUP_EMAIL | existing email | 409 uniform, no user created | 409 conflict |
| EDIT_VALID | change roles/groups/grants | 200, assignment sets replaced atomically; resolution updates next request | n/a |
| EDIT_UNKNOWN | nonexistent id | Uniform 404 | 404 not_found |
| DEACTIVATE_VALID | active user, confirmed | 200, status → deactivated; cannot log in ("→ Sofort kein Login"); audited | n/a |
| DEACTIVATE_NONACTIVE | pending/deactivated user | Uniform 409/404, no change | 409/404 |
| DEACTIVATE_AUDIT | deactivation occurs | Audit entry (NFR-O1) with actor + target | n/a |
| GROUP_MANAGE | user_groups.manage holder | Create/assign/remove team members; membership grants no permission (AD-12) | n/a |
| DIRECT_GRANT | add a direct grant | Joins the user's additive set immediately (AD-12) | n/a |
| QUAL_VIEW | user detail | Qualification assignments shown with status (display only, editing in 2.7) | n/a |
| IMMEDIATE_EFFECT | edit role/group/grant of active user | Next resolution reflects change, no re-login (AD-2/FR-21) | n/a |
| CLIENT_LIST | admin opens /admin/benutzer | User list with status badges | n/a |
| CLIENT_DETAIL | admin opens a user detail | Roles/groups/grants/qualifications shown; deactivate has confirmation | n/a |

</frozen-after-approval>

## Code Map

- `migrations/000012_user_groups_qualifications.{up,down}.sql` -- Create `user_groups`, `user_group_members`, `qualifications`, `user_qualifications` (spine tables 2/3/9/10). Down drops the four.
- `internal/user/adapters/postgres/queries.sql` -- Add: `ListUsers` (:many, id/names/email/state, ordered), `GetUserDetail` (:one or compose), `ListUserRoles`/`ListUserGroups`/`ListUserDirectGrants`/`ListUserQualifications` (:many), `CreateAdminUser` (:one), `UpdateUserProfileAdmin` (:one), `ListUserGroups`/`CreateUserGroup`/`DeleteUserGroup` (:many/:one/:exec), `ReplaceUserGroupMembers`/`ReplaceUserRoles`/`ReplaceUserDirectGrants` (delete+insert), `ListQualifications` (:many), `AddQualificationToUser`/`RemoveQualificationFromUser`. Re-run `just sqlc-generate`.
- `internal/user/adapters/postgres/users_admin.go` -- New repository file: transactional user create/edit, group membership replacement, direct-grant replacement, deactivate (with audit). Keep repo god-class small.
- `internal/user/core/users_admin.go` -- New small core file: `AdminUserSummary`, `AdminUserDetail`, `UserGroup`, `QualificationAssignment` types; `ListUsers`, `GetUserDetail`, `CreateAdminUser`, `UpdateAdminUser`, `DeactivateUser`, `ListUserGroups`, `CreateUserGroup`, `AssignUserGroupMembers`; validation + sentinel errors (`ErrAdminUserEmailTaken`, `ErrAdminUserNotFound`, `ErrUserNotActiveForDeactivate`, `ErrUserGroupNameTaken`, `ErrUserGroupNotFound`); defense-in-depth `users.*` re-checks; audit writes on deactivate (NFR-O1).
- `internal/user/core/service.go` / `internal/user/ports/ports.go` -- Expose the new methods + types on the Service and repo ports.
- `internal/user/adapters/http/admin_users.go` (extend) -- Add the new handlers (`GET/POST /users`, `GET/PUT /users/{id}`, `POST /users/{id}/deactivate`, `GET/POST /user-groups`, group-member endpoints) with uniform 400/403/404/409 mapping and German messages. Reuse the Story 2.5 `RequireAnyPermission` for the `users.manage`/`users.view` gate.
- `web/src/auth/users.ts` -- New small module: user-management API calls + types (list/detail/create/update/deactivate/groups).
- `web/src/pages/admin/AdminBenutzerPage.tsx` (+ `.module.css`) -- Replace placeholder: user list with status badges, user detail view, create/edit form, deactivate confirmation. German inline feedback, a11y.
- `web/src/components/UserEditor.tsx` (+ `.module.css`) -- create/edit form (name, email, status, roles, user groups, direct grants).
- `web/src/components/UserDetail.tsx` (+ `.module.css`) -- read view (roles, user groups, direct grants, qualification assignments with status).
- Tests: `internal/user/core/users_admin_test.go`, `internal/user/adapters/postgres/users_admin_test.go` (integration: create/edit/list/deactivate/groups/grants/immediate-effect/audit), `internal/user/adapters/http/admin_users_test.go` (extend), `web/src/pages/admin/AdminBenutzerPage.test.tsx`, `web/src/components/UserEditor.test.tsx`, `web/src/components/UserDetail.test.tsx`.

## Tasks & Acceptance

**Execution:**
- [x] `migrations/000012_user_groups_qualifications.{up,down}.sql` -- 4 spine tables (user_groups, user_group_members, qualifications, user_qualifications) -- schema
- [x] `queries.sql` + `sqlc-generate` -- user list/detail/create/edit/deactivate, groups, direct grants, qualification view -- persistence
- [x] `postgres/users_admin.go` -- transactional create/edit/group/grant/deactivate + audit -- repository
- [x] `core/users_admin.go` -- ListUsers/GetUserDetail/CreateAdminUser/UpdateAdminUser/DeactivateUser/group methods + validation + errors -- domain
- [x] `ports.go`/`service.go` -- expose the methods + types -- module boundary
- [x] `http/admin_users.go` (extend) -- user/group endpoints gated by users.* (RequireAnyPermission) -- API
- [x] `core/users_admin_test.go` -- unit: list/detail/create/edit/deactivate, dup email, unknown, forbidden, immediate-effect -- I/O matrix
- [x] `postgres/users_admin_test.go` -- integration: create/edit/atomic, deactivate+audit, group membership, direct grants, live-resolution -- persistence verification
- [x] `http/admin_users_test.go` (extend) -- handler + gate contracts -- HTTP contract
- [x] `web/src/auth/users.ts` -- API calls + types -- SPA data
- [x] `web/src/pages/admin/AdminBenutzerPage.tsx` (+css) -- user list + detail + create/edit + deactivate confirm -- SPA surface
- [x] `web/src/components/UserEditor.tsx` / `UserDetail.tsx` (+css) -- forms + read view -- SPA components
- [x] Web tests -- AdminBenutzerPage + UserEditor + UserDetail: list/detail/create/edit/deactivate/error -- frontend matrix

**Acceptance Criteria:**
- Given the "Benutzer" surface, when I open it, then I see the user list with each user's status (aktiv, pending, deaktiviert) (FR-21/UX-DR8).
- Given I edit a user, when I open a user detail, then I can assign roles and user groups (e.g. "Gruppe Ost"), add direct permission grants, and see their qualification assignments (FR-21/UX-DR5).
- Given I create or edit a user, when I change their profile/group membership, then the change persists and their resolved permission set updates immediately (AD-2/FR-21).
- Given I create a user group, when I set its name, then I can assign users to it, and membership grants the group's permissions (organisational groups grant no access, AD-12).
- Given I deactivate a user account, when I confirm deactivation, then the user's status becomes deaktiviert, they cannot authenticate at all ("→ Sofort kein Login", FR-21/UX-DR8), and the action is audited (NFR-O1).
- Given I remove a user from a group, when the removal is saved, then any permissions inherited from that group are revoked immediately on the user's next request (FR-21/AD-2/AD-6).
- Given a user is deactivated, when they attempt to log in, then authentication is rejected, consistent with Story 1.4.

## Spec Change Log

## Design Notes

- **New migration 000012** — this story creates the four missing spine tables. The permission-resolution query already never joins user_groups (AD-12 isolation), so organisational groups grant no access by construction; Story 2.6 adds the CRUD + membership.
- **Story 2.5 lesson applied:** assignment replacement (roles/groups/direct grants) uses separate DELETE + INSERT statements inside one transaction, never a data-modifying CTE (the latter silently drops rows under PostgreSQL's same-statement unique-index behavior).
- **Deactivate vs approve/reject:** Story 2.4's reject already moves pending→deactivated. This story's deactivate is the active→deactivated transition with confirmation + audit; both share `SetUserState`. The existing `SetUserState` query (Story 2.4) is reused.
- **Qualification display only:** the user detail shows qualification assignments + status; create/edit of qualifications and assignment is Story 2.7. This keeps 2.6 focused on the directory/access model.
- **Gate composition:** the Story 2.4 `/users` sub-mount uses `users.approve`; the user-management endpoints (list/detail/create/edit/deactivate) broaden this to any of `users.view`/`users.manage`/`users.approve` via `RequireAnyPermission`, with per-action defense-in-depth re-checks in core (list=any, edit/deactivate=`users.manage`).

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go and web tests pass, 0 lint issues
- `just migrate-up` -- expected: migration 000012 applies cleanly; `\dt` shows user_groups, user_group_members, qualifications, user_qualifications
- `curl` `GET /api/v1/admin/users` as admin -- expected: 200 user list with status; as helfende -- expected: uniform 403
- `curl` create a user, edit its roles/group/grants, deactivate -- expected: 201/200; dup email -- 409; deactivated user cannot log in
- Live: edit an active user's role, then resolve their permissions -- expected: changed set immediately (AD-2/FR-21)

## Suggested Review Order

**Entry point**

- Domain: ListUsers/GetUserDetail/CreateAdminUser/DeactivateUser + group methods, defense-in-depth users.* re-checks, English qualification wire codes, sentinel errors.
  [`users_admin.go:318`](../../internal/user/core/users_admin.go#L318)

- Route wiring: /users (any users.*) and /user-groups (user_groups.manage) sub-mounts.
  [`admin.go:49`](../../internal/user/adapters/http/admin.go#L49)

**HTTP contract**

- User/group handlers with uniform 400/403/404/409 mapping incl. self-deactivation, member assignment, delete.
  [`admin_users.go:238`](../../internal/user/adapters/http/admin_users.go#L238)

**Persistence**

- Transactional create/edit/deactivate (+session revoke) and group-member replacement; Story 2.5 delete-then-insert discipline.
  [`users_admin.go:1`](../../internal/user/adapters/postgres/users_admin.go#L1)

- Migration 000012: the four spine tables.
  [`000012_user_groups_qualifications.up.sql:1`](../../migrations/000012_user_groups_qualifications.up.sql#L1)

**SPA surfaces**

- User list + detail + editor + deactivate flow; permission-gated fetches (no batch 403), status badges.
  [`AdminBenutzerPage.tsx:44`](../../web/src/pages/admin/AdminBenutzerPage.tsx#L44)

- UserEditor: create/edit with roles, teams, direct grants; server confirmation messages.
  [`UserEditor.tsx:36`](../../web/src/components/UserEditor.tsx#L36)

- UserDetail: read view with qualification statuses + deactivate confirmation.
  [`UserDetail.tsx:29`](../../web/src/components/UserDetail.tsx#L29)

- API layer: types, status label maps with fallback, group member calls.
  [`users.ts:1`](../../web/src/auth/users.ts#L1)

**Tests**

- Core unit: list/detail/create/edit/deactivate/groups/assign/delete, dup, unknown, forbidden, immediate-effect.
  [`users_admin_test.go:49`](../../internal/user/core/users_admin_test.go#L49)

- Postgres integration: atomicity, deactivate session-revoke, audit, live-resolution.
  [`users_admin_test.go:1`](../../internal/user/adapters/postgres/users_admin_test.go#L1)

- Web: list/detail/create/edit/deactivate/groups/members/error.
  [`AdminBenutzerPage.test.tsx:1`](../../web/src/pages/admin/AdminBenutzerPage.test.tsx#L1)