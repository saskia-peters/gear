---
title: 'Admin Rework Effort 2 — Spreadsheet & UI'
type: 'feature'
created: '2026-09-08'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'e657598ac89ba134563c898da6f7717f94f00cdc'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The admin surfaces (Stories 2.4–2.7 + Effort 1 backend) work but the UI does not reflect the reworked model: the user list is a tall card list (not a compact spreadsheet), there is no status filter (default = aktiv), user-group membership is not visible in the list, roles are not assignable to user-groups in the UI, qualifications are not assignable from the user detail with a per-user valid-until, and the resolved permission set with its source(s) is not shown.

**Approach:** Effort 2 is the UI polish on top of the Effort 1 backend (Spec 2.9, already shipped):
- **User list → sortable spreadsheet-style table**: columns Vorname · Nachname · E-Mail · Status, default filter = aktiv, chips Aktiv/Pending/Deaktiviert/Alle, user-group membership as inline tags.
- **User detail**: three source sections (Rollen · Benutzergruppen · Direkte Berechtigungen) PLUS a collapsed "Alle Berechtigungen" expandable view showing the resolved union with the source in parentheses (provenance from Effort 1).
- **Qualification assignment from the user detail**: assign/revoke qualifications + set/update the per-user valid-until (Effort 1 endpoints; fuehrende/schirrmeister can do this, users.* read-only otherwise).
- **User-group roles UI**: assign/remove roles on a user-group (Effort 1 endpoints).
- **User↔user-group membership** surfaced clearly (currently buried at the page bottom).

## Boundaries & Constraints

**Always:**
- **User list spreadsheet (UX-DR6/8/10):** a real `<table>` with columns **Vorname · Nachname · E-Mail · Status**. **Sortable** by clicking any column header (ARIA-sortable: ascending/descending/off). **Default filter = aktiv**; filter chips Aktiv · Pending · Deaktiviert · Alle. The filter drives `?status=` on `ListUsers` (Effort 1 backend). **User-group membership visible** as inline tags under/beside the name (e.g. "Gruppe Ost"), from the user-group memberships already returned by the summary or a lightweight lookup — I'll add group names to the `AdminUserSummary` server payload if not already present (verify; Effort 1 returns id/names/email/status only). Rows are compact (one line per user), not tall cards.
- **User detail (UX-DR6/8):** keep the three source sections (Rollen · Benutzergruppen · Direkte Berechtigungen) as-is; add a **collapsed "Alle Berechtigungen"** `<details>` that expands to the resolved union, each permission annotated `code (Quelle: ...)` — source from `resolved_permissions` (Effort 1): "Rolle: X", "Benutzergruppe: X", or "Direkt". Multi-source permissions list each source. This is a pure render of the Effort 1 provenance.
- **Qualification assignment on the user detail:** for callers holding `users.qualifications.manage` (fuehrende/schirrmeister/admin), the Qualifikationen section becomes editable: add an existing qualification (choose from the vocabulary), set the per-user valid-until (or "Unbegrenzt" for unlimited quals), revoke. Uses the Effort 1 endpoints. For holders WITHOUT the code, the section stays read-only (status badges only). The admin additionally uses this as the primary assignment path.
- **User-group roles UI:** in the user-groups section (Benutzer page), each group gains a "Rollen" action opening a role-assignment editor (checkbox list of permission groups) using the Effort 1 `GET/POST /user-groups/{id}/roles` endpoints. Gated by `user_groups.manage`. Show the group's current roles.
- **Membership visibility:** the user-groups section stays on the Benutzer page but is clearly visible (it already exists with the "Mitglieder" editor); add the inline group tags to the user list so membership is visible at a glance.
- **API client (web/src/auth/users.ts):** add `listUsers(status?)`, `listUserGroupRoles(id)`, `assignUserGroupRoles(id, roleIds)`, `assignUserQualification(userId, qualId, expiresAt?)`, `revokeUserQualification(userId, qualId)`, `updateUserQualificationExpiry(userId, qualId, expiresAt?)` calls hitting the Effort 1 endpoints.
- **Gating:** all the Effort 1 permission rules are enforced server-side; the SPA only hides/disables UI for callers lacking the code (defense-in-depth stays server-authoritative). fuehrende/schirrmeister see the spreadsheet + user detail + qualification editing; they do NOT see create/edit/deactivate (users.manage/admin-only) and do NOT see group management (user_groups.manage/admin-only).
- **German microcopy, a11y (UX-DR6/8/9):** table with proper `<th>`/`scope`, sort buttons keyboard-operable, filter chips, ≥48px targets, `aria-sort`, empty states ("Keine aktiven Benutzer"), loading skeleton, inline feedback. Preserve existing patterns.

**Ask First:**
- None (Effort 2 decisions B/C/D already resolved: spreadsheet columns Vorname/Nachname/E-Mail/Status + sorting; resolved view = three sources separate + collapsed union with source in parentheses; split from Effort 1).

**Never:**
- No client-side authorization — server remains authoritative (AD-2/AD-6).
- No caching of the resolved set or qualifications (live per request).
- No permission-code names in user-facing microcopy beyond the provenance view's German source labels.
- Do not rebuild the Effort 1 backend; only consume it.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| LIST_TABLE | admin opens /admin/benutzer | Spreadsheet table, default aktiv filter, sortable headers | n/a |
| FILTER_ACTIVE | default | Only active users shown (server ?status=active) | n/a |
| FILTER_ALL | chip Alle | All users shown | n/a |
| FILTER_PENDING | chip Pending | Only pending shown | n/a |
| SORT_EMAIL | click E-Mail header | Rows sorted by email asc/desc | n/a |
| GROUP_TAGS | user in "Gruppe Ost" | Inline tag "Gruppe Ost" under their name | n/a |
| DETAIL_PROVENANCE | open user detail | Three source sections + collapsed "Alle Berechtigungen" with sources | n/a |
| QUAL_ASSIGN_FIXED | fuehrende assigns fixed qual with valid-until | Assignment persisted; status derives from it | n/a |
| QUAL_ASSIGN_UNLIMITED | fuehrende assigns unlimited qual | No valid-until needed; Unbegrenzt | n/a |
| QUAL_REVOKE | fuehrende revokes | Gone immediately | n/a |
| QUAL_READONLY | users.view-only holder opens detail | Qualification section read-only (no edit UI) | n/a |
| GROUP_ROLE_ASSIGN | admin assigns roles to a group | Role set replaced; members inherit next request | n/a |
| GROUP_ROLE_REMOVE | admin removes a role from a group | Gone from group; members lose it | n/a |
| GROUP_ROLE_READONLY | no user_groups.manage | No roles editor (group section hidden) | n/a |
| FETCH_FAIL | user list load fails | German inline error, no crash | inline error |
| 401_EXPIRED | in-page fetch returns 401 | Clear auth state + redirect to /login | redirect |

</frozen-after-approval>

## Code Map

- `internal/user/core/users_admin.go` / `queries.sql` -- If the `AdminUserSummary` lacks user-group names, add them (a `ListUserGroupNamesByUsers` query or extend `ListUsers`); verify Effort 1 summary shape first. Re-run `just sqlc-generate` if queries change.
- `web/src/auth/users.ts` -- Add API calls: `listUsers(status?)`, `listUserGroupRoles`, `assignUserGroupRoles`, `assignUserQualification`, `revokeUserQualification`, `updateUserQualificationExpiry`; add `PermissionSource` render types already present; ensure `AdminUserSummary` includes `user_groups?: string[]`.
- `web/src/pages/admin/AdminBenutzerPage.tsx` (+ `.module.css`) -- Replace the card list with a **sortable `<table>`** (Vorname · Nachname · E-Mail · Status), **status filter chips** (default aktiv), inline **group tags**; keep the user-groups section (members + NEW roles editor); wire the filter to `listUsers(status)`.

**Effort 2 follow-up (commit ae98ebb, 2026-09-08):** after review the following were added on top of the approved spec — the user-detail group setter, the groups card, and the sticky action bars:
- `internal/user/adapters/http/admin_rework.go` -- `AssignUserGroupsHandler` (`PUT /api/v1/admin/users/{userID}/groups`, replaces a user's group set atomically delete-then-insert, gated by `user_groups.manage`, audited NFR-O1). `internal/user/core/users_admin.go` -- `AssignUserGroups` core method + `MsgUserGroupsUpdated`. `internal/user/adapters/postgres/users_admin.go` -- `ReplaceUserGroupMemberships` repo method.
- `web/src/auth/users.ts` -- `assignUserGroups(userId, groupIds)` client call.
- `web/src/components/UserDetail.tsx` (+ `.module.css`) -- the Benutzergruppen section becomes an **editable checkbox list** (saved inline via the new PUT) for `user_groups.manage` holders, read-only badges otherwise.
- `web/src/pages/AdminPage.tsx` -- a "Benutzergruppen" landing card (gated by `user_groups.manage`) linking to the Benutzer surface where the groups live.
- `web/src/pages/admin/AdminBenutzergruppenPage.tsx` (+ `.module.css`, 2026-09-08) -- the group CRUD/member/role surfaces moved OUT of the Benutzer page into this dedicated page (route `/admin/benutzergruppen`, `user_groups.manage`). AdminBenutzerPage keeps only the user list + user detail (incl. the editable group-membership checkboxes).
- `web/src/components/UserEditor.tsx` / `RoleEditor.tsx` / `QualificationEditor.tsx` / `AssigneeEditor.tsx` (+ `.module.css`) -- the Speichern/Abbrechen actions moved to a **sticky top action bar** so they stay visible without scrolling in long editors.
- `web/src/components/UserTable.tsx` (+ `.module.css`) -- New: the spreadsheet table component (columns, sort state via `aria-sort`, row click → detail). Keep the page file small (god-class convention).
- `web/src/components/UserDetail.tsx` (+ `.module.css`) -- Add the collapsed "Alle Berechtigungen" provenance view; make the Qualifikationen section editable for `users.qualifications.manage` holders (assign/revoke/valid-until) using the Effort 1 endpoints; keep it read-only otherwise.
- `web/src/components/GroupRolesEditor.tsx` (+ `.module.css`) -- New: the role-assignment editor for a user-group (checkbox list of permission groups, save/cancel, German feedback).
- Tests: `web/src/components/UserTable.test.tsx` (columns, sort, filter chips, group tags, empty), `web/src/components/UserDetail.test.tsx` (provenance view, qual assign/revoke/readonly), `web/src/components/GroupRolesEditor.test.tsx` (list/assign/remove), `web/src/pages/admin/AdminBenutzerPage.test.tsx` (table, filter wiring, 401), plus any core/http test updates if the summary shape changed.

## Tasks & Acceptance

**Execution:**
- [x] Verify/extend `AdminUserSummary` with user-group names (server) if missing -- group tags data
- [x] `web/src/auth/users.ts` -- the Effort 1 API calls + summary type -- SPA data
- [x] `web/src/components/UserTable.tsx` (+css) -- sortable spreadsheet table + filter chips + group tags -- list surface
- [x] `web/src/pages/admin/AdminBenutzerPage.tsx` (+css) -- swap card list for the table, wire filter, keep/upgrade group section -- page integration
- [x] `web/src/components/UserDetail.tsx` (+css) -- collapsed "Alle Berechtigungen" provenance view + editable Qualifikationen (assign/revoke/valid-until, read-only otherwise) -- detail surface
- [x] `web/src/components/GroupRolesEditor.tsx` (+css) -- per-group role assignment editor -- group-role UI
- [x] Web tests -- UserTable/UserDetail/GroupRolesEditor/AdminBenutzerPage: table/sort/filter/tags/provenance/qual/roles/401 -- frontend matrix

**Acceptance Criteria:**
- Given the "Benutzer" surface, when I open it, then I see a compact sortable spreadsheet (Vorname · Nachname · E-Mail · Status) defaulting to active users, with filter chips (Aktiv/Pending/Deaktiviert/Alle) and user-group tags.
- Given a user detail, when I open it, then I see the three source sections and a collapsed "Alle Berechtigungen" view where each permission shows its source in parentheses (Rolle/Benutzergruppe/Direkt).
- Given I hold users.qualifications.manage, when I open a user detail, then I can assign/revoke qualifications and set the per-user valid-until (fixed quals require it; unlimited are Unbegrenzt); without the code the section is read-only.
- Given I hold user_groups.manage, when I open the user-groups section, then I can assign/remove roles on a group (members inherit immediately); without it the editor is hidden.
- Given any fetch fails or a 401 occurs, then the UI shows a German inline error or redirects to login (no crash).

## Spec Change Log

- **Effort 2 completion fix (the outer admin gate):** the composition-root gate was widened from `admin.recovery.approve`-only to any-of `AdminModuleAccessCodes()` so fuehrende/schirrmeister (users.view + users.qualifications.manage) can reach the module; the qualification vocabulary read opened to `users.qualifications.manage` holders; `AdminModuleAccessCodes` is a function returning a fresh slice (immutable gate input).
- **Review fixes applied (2026-09-08):** group-roles editor catalog available to `user_groups.manage`-only holders; fixed-qual client validation (Gültig bis required); composition tests updated to the production any-of gate + fuehrende-enters-module + superset invariants; qualification HTTP gate pinned for the new code (GET 200 / writes 403); stale UserDetail microcopy removed; GroupRolesEditor 401 handling; single row-activation in UserTable; mutually exclusive group overlays; dead ternary removed; empty-state + inactive-user hints added.
- **Effort 2 follow-up applied (commit ae98ebb, 2026-09-08):** user-detail group assignment (PUT /users/{id}/groups, editable Benutzergruppen checkboxes on the user detail), a Benutzergruppen card on the admin landing, and sticky Speichern/Abbrechen action bars in UserEditor/RoleEditor/QualificationEditor/AssigneeEditor. Code Map above lists the files.
- **Benutzergruppen promoted to its own surface (2026-09-08):** the group management (create, member assignment, team-role assignment, delete) moved out of the Benutzer page into a dedicated `AdminBenutzergruppenPage` (route `/admin/benutzergruppen`, gated by `user_groups.manage`). The admin nav gains a "Benutzergruppen" entry between "Benutzer" and "Rollen"; the landing card links to the new surface. The Benutzer page keeps the user detail's editable group-membership checkboxes but no longer hosts group CRUD.

## Design Notes

- **Effort 2 consumes Effort 1.** The backend (three-way resolution, group-roles, per-user qual expiry, provenance, ?status=) shipped in Spec 2.9. This story is the SPA surface; any server change is only to make `AdminUserSummary` carry group names if it does not already.
- **Spreadsheet is the UX ask** ("not consume so much height per user, more a spreadsheet style"): compact rows, sortable columns, default-active filter — a real table with proper semantics, not a card grid.
- **Provenance view is a pure render** of `resolved_permissions` (Effort 1): collapsed by default, source in parentheses, multi-source lists each.
- **Qualification editing lives on the user detail** (the primary path per the human's ask); the Qualifikationen surface keeps the vocabulary CRUD + assignee editor for admins. fuehrende/schirrmeister use the user detail.
- **Group roles editor** mirrors the members editor: checkbox list of roles, save replaces the set atomically.

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go and web tests pass, 0 lint issues
- `npm --prefix web run typecheck` -- expected: clean
- Live: as admin, /admin/benutzer shows the table defaulting to aktiv, sortable; open a user → collapsed "Alle Berechtigungen"; as a fuehrende user, assign a qualification with valid-until; assign a role to a user-group; verify a member inherits it on the next request

## Suggested Review Order

**Entry point**

- The widened outer admin gate — fuehrende/schirrmeister now reach the module (the purpose of Effort 2).
  [`main.go:102`](../../cmd/server/main.go#L102)

- The immutable admin-module code set + the status-filtered user list.
  [`users_admin.go:56`](../../internal/user/core/users_admin.go#L56)

**List surface**

- AdminBenutzerPage: table + filter wiring + group section (members, roles, delete).
  [`AdminBenutzerPage.tsx:48`](../../web/src/pages/admin/AdminBenutzerPage.tsx#L48)

- UserTable: sortable spreadsheet, filter chips, group tags, single row activation.
  [`UserTable.tsx:36`](../../web/src/components/UserTable.tsx#L36)

**Detail surface**

- UserDetail: three source sections + collapsed "Alle Berechtigungen" provenance view + editable qualification assignment (assign/revoke/valid-until, fixed-qual validation).
  [`UserDetail.tsx:64`](../../web/src/components/UserDetail.tsx#L64)

- GroupRolesEditor: per-group role assignment (401/403 handling, atomic replace).
  [`GroupRolesEditor.tsx:26`](../../web/src/components/GroupRolesEditor.tsx#L26)

- API client: the Effort 1 endpoint calls + summary type.
  [`users.ts:1`](../../web/src/auth/users.ts#L1)

**Tests**

- Composition: fuehrende-enters-module, no-code 403, superset invariant.
  [`admin_composition_test.go:202`](../../internal/user/adapters/http/admin_composition_test.go#L202)

- Qualifications gate: users.qualifications.manage GET 200 / writes 403.
  [`admin_qualifications_test.go:1`](../../internal/user/adapters/http/admin_qualifications_test.go#L1)

- Web: UserTable/GroupRolesEditor/UserDetail/AdminBenutzerPage (sort/filter/tags/provenance/qual/roles/401).
  [`UserTable.test.tsx:1`](../../web/src/components/UserTable.test.tsx#L1)