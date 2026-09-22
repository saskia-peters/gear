---
title: 'Role & Permission-Group Management'
type: 'feature'
created: '2026-09-07'
status: 'done'
review_loop_iteration: 0
baseline_commit: '900569adb67379ed2bf9c48fea7e4026208ca7b7'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Permission groups (roles) can only be seeded by migration — there is no admin surface to create or edit named permission groups, and no way to tailor what a group of volunteers can do beyond the four base roles (AD-12). The "Rollen" surface (Story 2.3) is a placeholder.

**Approach:** Add the Role & Permission-Group Management surface: a backend CRUD for named permission groups (list, create, update) under `/api/v1/admin/groups`, gated by the `roles.*` permission codes (AD-6/FR-19), plus the SPA "Rollen" surface (list of roles incl. the four base roles + custom named groups; an editor with an additive checkbox grid over the 21 base codes; save with name + checked set). Editing a role takes effect immediately on the next request because permission resolution is live per request (AD-2/AD-6/FR-6). Groups stay flat (no groups-in-groups in V1, AD-12); base roles remain editable as the named starting point for the matrix.

## Boundaries & Constraints

**Always:**
- **Backend, permission-gated (AD-6/FR-19):** all role-management endpoints live under `/api/v1/admin/groups` inside the admin router. They are reachable only by holders of a `roles.*` code — the exact codes the SPA nav uses for the Rollen entry (`roles.create`, `roles.edit`, `roles.assign`). A caller without any of them gets the uniform 403 hidden-existence envelope (FR-19). Use a new any-of permission gateway middleware (the existing `RequirePermission`/`RequireAdminPermission` take a single code; roles needs any-of three).
- **List roles:** `GET /api/v1/admin/groups` returns every permission group — the four base roles (`helfende`, `schirrmeister`, `fuehrende`, `admin`) and any custom named groups (AD-12) — each with `{id, name, description, is_base_role, permissions:[code...]}`. Sorted base-roles-first then name. No secret material.
- **Create:** `POST /api/v1/admin/groups` with `{name, description, permissions:[code...]}` — creates a named group (is_base_role=false) and its permission rows atomically. Name is unique (case-insensitive); a duplicate name returns a uniform 409. Gated by `roles.create`.
- **Update:** `PUT /api/v1/admin/groups/{id}` with the same body — replaces the group's name/description and its permission set atomically (delete-then-insert of `permission_group_permissions` in one transaction). Base roles are editable (they remain the named matrix starting point; AD-12 allows editing all four). Gated by `roles.edit`. Unknown id → uniform 404. Renaming to a taken name → 409.
- **Additive-only (FR-6/AD-12):** permissions are checkboxes — checked = granted; there are no deny permissions and nothing ever subtracts. The server accepts only the 21 base codes (any code not in `permissions` is rejected with a uniform 400); it never stores a deny.
- **Flat groups (AD-12):** a permission group contains only permissions, never other groups — no parent/child field exists or is added. Enforced by schema (unchanged) and never exposed in the API.
- **Immediate effect (AD-2/AD-6/FR-6):** `ListPermissionsByUser` resolves live per request with no cache, so a role edit changes every affected user's effective set on the very next request. Verify with a test (edit helfende → a helfende user's resolved set changes without re-login); do not add caching.
- **Permission catalog:** the SPA editor needs the full 21-code catalog with German display labels. Serve it from the backend (`GET /api/v1/admin/groups` may include an `available_permissions` field, or a sibling catalog endpoint) so the code list is server-authoritative and never drifts from the seed.
- **SPA "Rollen" surface (UX-DR6/UX-DR8/UX-DR9):** replace the AdminRollenPage placeholder with a real surface: a role list (base roles + custom, each with its permission count and a base-role badge), a "Neue Rolle" action, and an editor (create/edit) showing the 21-code additive checkbox grid with German labels, name input, save/cancel. German inline feedback (loading, errors, success), ≥48px targets, keyboard/focus/SR. The Rollen nav entry already gates on `roles.*` client-side (Story 2.3); the surface must also respect it.
- **Assignability:** a created role becomes assignable to users via the existing `AddUserToGroup` machinery (Story 2.4) — Story 2.6 builds the user editor; this story just ensures the role exists and is listable/assignable (the data path already works).

**Ask First:**
- None.

**Never:**
- No deny permissions, no subtraction (AD-12 additive only).
- No group-in-group nesting (V1 flat, AD-12).
- No client-side authorization — the server gates every group endpoint with `roles.*` (AD-2/AD-6).
- No existence leak to callers without any `roles.*` code (FR-19).
- No permission-set caching (immediate revocation, AD-2/FR-6).
- No changes to already-committed migrations; if a migration were needed it must be additive — but none is expected (schema already supports groups + memberships).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| LIST_GROUPS | admin (roles.*) | 200, all groups sorted base-first incl. 4 base roles + custom, each with permissions | n/a |
| LIST_FORBIDDEN | caller with no roles.* code | Uniform 403, no admin hint (FR-19) | 403 hidden |
| LIST_CATALOG | admin | 21 base codes with German labels included | n/a |
| CREATE_VALID | {name:"gerätewart", permissions:[tools.manage]} | 201, group created (is_base_role=false), permission rows stored | n/a |
| CREATE_DUP_NAME | existing name, different case | 409 uniform, no group created | 409 conflict |
| CREATE_BAD_CODE | permissions contains unknown code | 400 uniform, no group created | 400 invalid |
| CREATE_EMPTY_PERMS | permissions: [] | Group created with no permissions (valid additive empty set) | n/a |
| UPDATE_VALID | edit helfende, remove dashboard.view | helfende members' set changes on next request (verified live) | n/a |
| UPDATE_UNKNOWN | nonexistent id | Uniform 404 | 404 not_found |
| UPDATE_DUP_NAME | rename to taken name | 409 uniform | 409 conflict |
| UPDATE_BASE_ROLE | edit admin base role | Allowed (editable matrix); change applies live | n/a |
| IMMEDIATE_EFFECT | helfende user active; edit helfende perms | Next resolution reflects change, no re-login (AD-2/FR-6) | n/a |
| CLIENT_LIST | admin opens /admin/rollen | Role list with badges + permission counts | n/a |
| CLIENT_EDITOR | admin edits a role, saves | POST/PUT round-trip, success feedback, list refreshes | n/a |

</frozen-after-approval>

## Code Map

- `internal/platform/auth/gateway.go` -- Add `RequireAnyPermission(validator, resolver, required []string, denyMsg string, log *slog.Logger)` (or a variadic variant) reusing `requirePermission`'s envelope; used to gate the groups sub-mount on any-of `roles.create/edit/assign`.
- `internal/user/adapters/postgres/queries.sql` -- Add: `ListPermissionGroups` (:many, id/name/description/is_base_role, ORDER BY is_base_role DESC, name), `ListPermissionsByGroup` (:many, codes+labels for one group), `CreatePermissionGroup` (:one, INSERT RETURNING id), `ListGroupPermissionIdsByCodes` (:many, ids for given codes — used to resolve codes→permission rows), `InsertGroupPermissions` (:exec, bulk group_permissions insert), `ReplaceGroupPermissions` (:exec, DELETE all rows for group then INSERT), `UpdatePermissionGroup` (:one, name/description), `ListAllPermissions` (:many, the 21-code catalog with German labels). Re-run `just sqlc-generate`.
- `internal/user/adapters/postgres/repository.go` -- Wire the new queries; implement `ListGroups`, `CreateGroup`, `UpdateGroup` as repository methods (create/update in a transaction: group row + membership rows all-or-nothing). Keep file size in check (god-class convention) — extract to a new `internal/user/adapters/postgres/groups.go` if it grows.
- `internal/user/core/roles.go` -- New small core file: `RoleGroup`, `RoleGroupPermission` types; `ListRoles(ctx, actor)`, `CreateRole(ctx, actor, input)`, `UpdateRole(ctx, actor, id, input)`; validation (name non-empty, ≤120 chars; codes subset of the 21; no denies); defense-in-depth `roles.*` permission re-check like the approval path; uniform errors `ErrRoleNameTaken`, `ErrUnknownPermissionCode`, `ErrRoleNotFound`.
- `internal/user/core/service.go` / `internal/user/ports/ports.go` -- Add the three methods + input/result types to the Service struct and inbound port.
- `internal/user/adapters/http/admin_roles.go` -- New small handler file: `GET /groups`, `POST /groups`, `PUT /groups/{id}` with uniform 400/403/404/409 mapping and German messages.
- `internal/user/adapters/http/admin.go` -- Register the groups sub-mount gated by `RequireAnyPermission(..., roles.create, roles.edit, roles.assign, ...)` (like the users sub-mount pattern from Story 2.4).
- `web/src/auth/roles.ts` -- New small module: the 21-code catalog with German labels (or fetch from the backend catalog endpoint; prefer server-authoritative), plus a `useRoles`-style helper. Keep small.
- `web/src/pages/admin/AdminRollenPage.tsx` (+ `.module.css`) -- Replace placeholder: role list (base-role badge, permission count, edit/Neue Rolle), editor modal/inline form with the additive checkbox grid, German feedback.
- `web/src/components/RoleEditor.tsx` (+ `.module.css`) -- The create/edit form: name, description, 21-code checkbox grid (checked = granted), save/cancel, inline validation + success/error.
- Tests: `internal/user/core/roles_test.go` (create/update/list, dup name, bad code, unknown, forbidden, immediate-effect), `internal/user/adapters/postgres/repository_test.go` (integration: create/update/list/atomic/effect), `internal/user/adapters/http/admin_roles_test.go` (200/400/403/404/409, any-of gate), `internal/platform/auth/gateway_test.go` (RequireAnyPermission), `web/src/components/RoleEditor.test.tsx` + `web/src/pages/admin/AdminRollenPage.test.tsx` (list/editor/create/edit/error).

## Tasks & Acceptance

**Execution:**
- [x] `gateway.go` -- `RequireAnyPermission` (any-of) middleware -- role gating
- [x] `queries.sql` + `sqlc-generate` -- group CRUD + catalog queries -- persistence
- [x] `repository.go`/`groups.go` -- `ListGroups`/`CreateGroup`/`UpdateGroup` (transactional) -- repository
- [x] `core/roles.go` -- `ListRoles`/`CreateRole`/`UpdateRole` + validation + errors -- domain
- [x] `ports.go`/`service.go` -- expose the three methods -- module boundary
- [x] `admin.go`/`admin_roles.go` -- groups sub-mount (any-of gate) + three handlers -- API
- [x] `core/roles_test.go` -- unit: create/update/list, dup name, bad code, unknown, forbidden, immediate-effect -- I/O matrix
- [x] `repository_test.go` -- integration: create/update/list/atomic/effect/live-resolution -- persistence verification
- [x] `admin_roles_test.go` + `gateway_test.go` -- handler + any-of gate contracts -- HTTP contract
- [x] `web/src/auth/roles.ts` -- 21-code catalog with German labels (server-authoritative) -- SPA data
- [x] `web/src/components/RoleEditor.tsx` (+css) -- additive checkbox editor (name + 21 codes) -- SPA editor
- [x] `web/src/pages/admin/AdminRollenPage.tsx` (+css) -- role list + Neue Rolle + edit wiring -- SPA surface
- [x] Web tests -- RoleEditor + AdminRollenPage: list/create/edit/error -- frontend matrix

**Acceptance Criteria:**
- Given the permission model, when I open the "Rollen" surface, then I see the roles including the four base roles (helfende, schirrmeister, fuehrende, admin) and any custom named groups (AD-12).
- Given I create or edit a role, when I use the role editor, then each permission is an additive checkbox (checked = granted; no deny permissions) covering the 21 base codes (FR-6/AD-12), and I can save the role with a name and its checked permission set.
- Given I edit a role, when I change its checks and save, then affected users' effective permission sets update immediately on the next request (AD-2/AD-6/FR-6).
- Given the role model constraints (AD-12), when I manage roles, then groups are flat (no groups-in-groups in V1), and base roles are editable while remaining the named starting point for the matrix.
- Given I add a new role, when I create it, then it is available to assign to users via the user editor (FR-6).

## Spec Change Log

## Design Notes

- **Any-of gate for roles.** The Rollen nav entry gates on three codes (`roles.create`/`roles.edit`/`roles.assign`). The existing gateway only supports a single required code, so Story 2.5 adds `RequireAnyPermission`. The roles sub-mount uses it; create is additionally guarded by `roles.create` and update by `roles.edit` inside the handlers/core (defense-in-depth), so a `roles.assign`-only holder can list but not create/edit.
- **Server-authoritative catalog.** The 21-code list with German labels is served by the backend so the SPA editor never hardcodes a stale list (the codebase already had a hardcoded duplicate in Story 2.2 tests — this fixes the drift at the source). The web module consumes the catalog endpoint; fall back to a local copy only if the fetch fails.
- **Immediate effect is free.** Permission resolution is already live per request (Story 2.2, no cache). This story verifies it with a test rather than re-implementing.
- **Atomic group writes.** Create and update each run in a transaction (group row + membership rows), so a failed half-write never leaves a group with a partial permission set.

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go and web tests pass, 0 lint issues
- `just migrate-up` -- expected: migration state unchanged (no new migration)
- `curl` `GET /api/v1/admin/groups` as admin -- expected: 200 with the 4 base roles + catalog; as helfende -- expected: uniform 403
- `curl` create a "gerätewart" group then `PUT` edit it -- expected: 201/200; duplicate name -- 409; unknown code -- 400
- Live: as admin edit the `helfende` role's checks, then as a helfende user resolve permissions -- expected: changed set immediately

## Suggested Review Order

**Entry point**

- The domain model + validation: BasePermissionCodes (server-authoritative 21-code source), ListRoles/CreateRole/UpdateRole with per-action permission re-checks, additive-only, sentinel errors.
  [`roles.go:137`](../../internal/user/core/roles.go#L137)

- The any-of gateway middleware that gates the whole roles surface.
  [`gateway.go:71`](../../internal/platform/auth/gateway.go#L71)

**HTTP contract**

- The /groups sub-mount gated by RequireAnyPermission(roles.create/edit/assign).
  [`admin.go:62`](../../internal/user/adapters/http/admin.go#L62)

- ListRoles/CreateRole/UpdateRole handlers with uniform 400/403/404/409 mapping.
  [`admin_roles.go:36`](../../internal/user/adapters/http/admin_roles.go#L36)

**Persistence**

- Transactional group CRUD + grouped (non-N+1) permission assembly + DB↔core catalog drift test.
  [`groups.go:1`](../../internal/user/adapters/postgres/groups.go#L1)

**SPA surface**

- AdminRollenPage: role list, base-role badges, permission counts, per-action gating, 403 downgrade.
  [`AdminRollenPage.tsx:23`](../../web/src/pages/admin/AdminRollenPage.tsx#L23)

- RoleEditor: additive 21-code checkbox grid, name/description, inline validation, onForbidden.
  [`RoleEditor.tsx:31`](../../web/src/components/RoleEditor.tsx#L31)

- Server-authoritative catalog consumer with fallback.
  [`roles.ts:1`](../../web/src/auth/roles.ts#L1)

**Tests**

- Core unit: list/create/update, dup name, bad code, forbidden, immediate-effect, audit.
  [`roles_test.go:45`](../../internal/user/core/roles_test.go#L45)

- Postgres integration: create/update/list/atomic, live-resolution effect, catalog drift.
  [`groups_test.go:1`](../../internal/user/adapters/postgres/groups_test.go#L1)

- HTTP + any-of gate + web create/edit/create-only/edit-only gating.
  [`admin_roles_test.go:1`](../../internal/user/adapters/http/admin_roles_test.go#L1)