---
title: 'Qualification Management'
type: 'feature'
created: '2026-09-07'
status: 'done'
review_loop_iteration: 0
baseline_commit: '3be0613d424a198a4ef26c6b05d227c70c432e21'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The qualification vocabulary and assignment tables exist (migration 000012, Story 2.6) with display seams, but there is no way for an admin to create/edit qualifications, assign them to volunteers, or remove them. The "Qualifikationen" surface (Story 2.3) is a placeholder, and eligibility gating (AD-7, FR-22) has no admin tooling.

**Approach:** Add Qualification Management: a backend CRUD for the qualification vocabulary (create, list, update) and assignment editing (assign/remove on a user) under `/api/v1/admin/qualifications`, gated by `qualifications.manage` (AD-6/FR-19), plus the "Qualifikationen" SPA surface (qualification list with status indicators, create/edit form, per-user assignment editor). Assigning/removing takes effect immediately on the next check because qualification resolution is live per request (AD-7/FR-22). Never-expiring qualifications (`unlimited`) never enter the `Bald ablaufend`/`Abgelaufen` states (FR-22).

## Boundaries & Constraints

**Always:**
- **Backend, permission-gated (AD-6/FR-19):** all qualification endpoints live under `/api/v1/admin/qualifications` (and a sibling `/users/{id}/qualifications` assignment surface or a flat `/qualifications/{id}/assignees` — pick one and keep it consistent with Story 2.6's `/user-groups` pattern), gated by `qualifications.manage` via the `RequireAdminPermission` middleware (single code). A caller without it gets the uniform 403 hidden-existence envelope (FR-19).
- **List:** `GET /api/v1/admin/qualifications` returns every qualification with `{id, name, description, expiry_kind('unlimited'|'fixed'), expires_at}` plus, for each, its **status indicator** derived server-side: `Unbegrenzt` (unlimited), `Gültig`, `Bald ablaufend`, `Abgelaufen` (fixed, from `expires_at` vs now; the same 30-day window as Story 2.6's `qualificationStatus`). Ordered by name. Also return the full user roster (id, display name) so the assignment editor can pick assignees in one round-trip.
- **Create:** `POST /api/v1/admin/qualifications` with `{name, description, expiry_kind, expires_at?}` — creates a qualification. `unlimited` requires no `expires_at`; `fixed` requires `expires_at` (a future timestamp). Name unique (case-insensitive guard like user groups) → 409. Unknown/empty name → 400.
- **Update:** `PUT /api/v1/admin/qualifications/{id}` with the same body — replaces name/description/expiry model atomically. Unknown id → 404; dup name → 409. Editing the expiry model does not rewrite existing assignments (each assignment inherits the qualification's current expiry model on read — keep it simple: status derives from the qualification's `expires_at`, not a per-assignment expiry copy).
- **Assignment:** `POST /api/v1/admin/qualifications/{id}/assignees` with `{user_ids:[...]}` replaces the assignee set atomically (delete-then-insert, Story 2.5 lesson: separate statements in one transaction). `GET .../{id}/assignees` lists current assignees. Removing a qualification from a volunteer revokes eligibility immediately (AD-7/FR-22) because resolution is live.
- **Status derivation (FR-22/AD-7):** `unlimited` → `Unbegrenzt` (never expiring; the Bald ablaufend/Abgelaufen states never apply). `fixed` → `Gültig`/`Bald ablaufend`/`Abgelaufen` from `expires_at` vs now, using the existing `qualificationStatus` window logic (Story 2.6) — reuse it, do not duplicate.
- **Immediate effect (AD-7/FR-22):** assignment/removal and expiry changes reflect on the next qualification check; no caching. Verify with a test (assign → user detail shows it; remove → gone; fixed-expired → `Abgelaufen`). The AD-7 gating of inspection-start by qualification is Epic 5 (Story 5.1); this story provides the admin tooling + live-resolution foundation only.
- **SPA "Qualifikationen" surface (UX-DR6/8/9):** replace the AdminQualifikationenPage placeholder: qualification list with status badges (Unbegrenzt/Gültig/Bald ablaufend/Abgelaufen), "Neue Qualifikation" action, create/edit form (name, description, expiry-kind radio, expires_at date picker for fixed), and an assignee editor per qualification (checkbox list of volunteers). German inline feedback, ≥48px targets, keyboard/focus/SR, loading skeleton, empty state. Gated by `qualifications.manage` client-side (Story 2.3 nav).

**Ask First:**
- None.

**Never:**
- No client-side authorization — the server gates every endpoint (AD-2/AD-6).
- No secret material in any response.
- No existence leak to callers without `qualifications.manage` (FR-19).
- No data-modifying CTE for assignee replacement (Story 2.5 lesson) — separate statements in one transaction.
- No qualification/assignment caching (immediate revocation, AD-7/FR-22).
- Do not implement the AD-7 inspection-start gating itself (that is Story 5.1); this story only provides the vocabulary CRUD + assignment + status.
- Do not duplicate the Story 2.6 `qualificationStatus` derivation — reuse it.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| LIST_QUALS | admin (qualifications.manage) | 200, all qualifications + status indicators + user roster | n/a |
| LIST_FORBIDDEN | caller without qualifications.manage | Uniform 403, no admin hint (FR-19) | 403 hidden |
| CREATE_UNLIMITED | {name, expiry_kind:unlimited, no expires_at} | 201, qualification created, status Unbegrenzt forever | n/a |
| CREATE_FIXED | {name, expiry_kind:fixed, expires_at future} | 201, status derives from expires_at | n/a |
| CREATE_DUP_NAME | existing name, different case | 409 uniform | 409 conflict |
| CREATE_INVALID | empty name / fixed without expires_at / past expires_at | 400 uniform | 400 invalid |
| UPDATE_VALID | edit name/expiry of an existing qual | 200, replaced atomically | n/a |
| UPDATE_UNKNOWN | nonexistent id | Uniform 404 | 404 not_found |
| UPDATE_DUP_NAME | rename to taken name | 409 uniform | 409 conflict |
| ASSIGN_VALID | add assignees to a qualification | 200, assignee set replaced; user detail reflects immediately | n/a |
| ASSIGN_REMOVE | remove a volunteer from assignees | 200, gone from assignees; eligibility revoked immediately (AD-7/FR-22) | n/a |
| ASSIGN_UNKNOWN | unknown qual or user ids | Uniform 404/400 | 404/400 |
| STATUS_UNLIMITED | unlimited qualification | Unbegrenzt; never Bald ablaufend/Abgelaufen | n/a |
| STATUS_FIXED_EXPIRED | fixed, expires_at in the past | Abgelaufen | n/a |
| STATUS_FIXED_SOON | fixed, within 30-day window | Bald ablaufend | n/a |
| IMMEDIATE_EFFECT | assign then fetch user detail | Assignment present on next request (AD-7/FR-22) | n/a |
| CLIENT_LIST | admin opens /admin/qualifikationen | Qualification list with status badges + user roster | n/a |
| CLIENT_EDITOR | admin creates/edits a qualification | Form round-trip, success feedback, list refreshes | n/a |
| CLIENT_ASSIGN | admin assigns/removes a volunteer | Assignee editor round-trip, German feedback | n/a |

</frozen-after-approval>

## Code Map

- `internal/user/adapters/postgres/queries.sql` -- Add: `CreateQualification` (:one, INSERT RETURNING), `UpdateQualification` (:one, name/description/expiry_kind/expires_at), `QualificationNameExists`/`QualificationNameExistsExcept` (:one, case-insensitive dup guard), `ListQualificationAssignees` (:many, user id+name for a qualification), `ReplaceQualificationAssignees` (delete + insert — as two queries: `DeleteQualificationAssignees` + `InsertQualificationAssignees`). Reuse `ListQualifications`, `AddQualificationToUser`, `RemoveQualificationFromUser`, `ListUsers` (for the roster). Re-run `just sqlc-generate`.
- `internal/user/adapters/postgres/qualifications.go` -- New small repository file: `ListQualificationVocabulary`, `CreateQualification`, `UpdateQualification`, `ListQualificationAssignees`, `ReplaceQualificationAssignees` (transactional). Keep god-class small.
- `internal/user/core/qualifications.go` -- New small core file: `Qualification`, `QualificationWithStatus`, `QualificationAssignmentStatus` constants (reuse Story 2.6 `QualificationStatus*`); `ListQualifications(ctx, actor)`, `CreateQualification(ctx, actor, input)`, `UpdateQualification(ctx, actor, id, input)`, `ListQualificationAssignees(ctx, actor, id)`, `AssignQualificationUsers(ctx, actor, id, userIDs)`; validation (name ≤120 runes, expiry_kind in {unlimited,fixed}, fixed→expires_at required+future, unlimited→expires_at ignored/cleared); dup-name/unknown sentinel errors + German messages; defense-in-depth `qualifications.manage` re-check; status derivation reused from `qualificationStatus`.
- `internal/user/core/service.go` / `internal/user/ports/ports.go` -- Expose the new methods + types on Service and repo ports.
- `internal/user/adapters/http/admin_qualifications.go` -- New small handler file: `GET/POST /qualifications`, `PUT /qualifications/{id}`, `GET/POST /qualifications/{id}/assignees` with uniform 400/403/404/409 mapping + German messages.
- `internal/user/adapters/http/admin.go` -- Register the qualifications sub-mount gated by `qualifications.manage` (`RequireAdminPermission`).
- `web/src/auth/qualifications.ts` -- New small module: API calls + types (list/create/update/assignees) consuming the server roster.
- `web/src/pages/admin/AdminQualifikationenPage.tsx` (+ `.module.css`) -- Replace placeholder: qualification list with status badges, Neue Qualifikation, create/edit form, assignee editor.
- `web/src/components/QualificationEditor.tsx` (+ `.module.css`) -- create/edit form (name, description, expiry-kind radio, expires_at date picker).
- `web/src/components/AssigneeEditor.tsx` (+ `.module.css`) -- per-qualification volunteer checkbox list.
- Tests: `internal/user/core/qualifications_test.go`, `internal/user/adapters/postgres/qualifications_test.go` (integration: CRUD/assign/status/immediate-effect/dup/atomic), `internal/user/adapters/http/admin_qualifications_test.go`, `web/src/pages/admin/AdminQualifikationenPage.test.tsx`, `web/src/components/QualificationEditor.test.tsx`, `web/src/components/AssigneeEditor.test.tsx`.

## Tasks & Acceptance

**Execution:**
- [x] `queries.sql` + `sqlc-generate` -- CreateQualification/UpdateQualification/name-guards/assignee list + replace queries -- persistence
- [x] `postgres/qualifications.go` -- transactional create/update/assignee-replacement + vocabulary list -- repository
- [x] `core/qualifications.go` -- List/Create/Update/Assignees methods + validation + status reuse + errors -- domain
- [x] `ports.go`/`service.go` -- expose the methods + types -- module boundary
- [x] `http/admin_qualifications.go` + `admin.go` -- qualifications sub-mount (qualifications.manage gate) + handlers -- API
- [x] `core/qualifications_test.go` -- unit: CRUD, dup, invalid, unknown, forbidden, status (unlimited/fixed/expired/soon), immediate-effect -- I/O matrix
- [x] `postgres/qualifications_test.go` -- integration: CRUD/assign/atomic/status/immediate-effect -- persistence verification
- [x] `http/admin_qualifications_test.go` -- handler + gate contracts -- HTTP contract
- [x] `web/src/auth/qualifications.ts` -- API + types -- SPA data
- [x] `web/src/pages/admin/AdminQualifikationenPage.tsx` (+css) -- list + create/edit + assignee editor -- SPA surface
- [x] `web/src/components/QualificationEditor.tsx` / `AssigneeEditor.tsx` (+css) -- forms -- SPA components
- [x] Web tests -- page + editors: list/create/edit/assign/status/error -- frontend matrix

**Acceptance Criteria:**
- Given the "Qualifikationen" surface, when I open it, then I see the qualification list with per-qualification status indicators (Gültig, Bald ablaufend, Abgelaufen, or Unbegrenzt for never-expiring qualifications) (FR-22/AD-7/UX-DR8).
- Given I create or edit a qualification, when I save it, then it persists with a name and either a fixed validity period or no expiry at all (unbegrenzt gültig, lasts forever) (FR-22/AD-7).
- Given a qualification is unbegrenzt (no expiry), when eligibility is checked, then it never expires — the "Bald ablaufend"/"Abgelaufen" states never apply and assignments retain inspection rights indefinitely.
- Given a qualification has a fixed validity period, when its expiry date is reached, then the volunteer's eligibility for tasks requiring it is revoked (AD-7).
- Given I assign a qualification to a volunteer, when I save the assignment, then it is reflected immediately in the volunteer's profile and in scheduling eligibility (FR-22/AD-7).
- Given I remove a qualification from a volunteer, when the removal is saved, then eligibility for tasks requiring it is revoked immediately (FR-22/AD-2/AD-6).

## Spec Change Log

## Design Notes

- **Reuse Story 2.6 groundwork:** the tables (000012), `ListQualifications`, `Add/RemoveQualificationToUser`, and the `qualificationStatus` derivation already exist. Story 2.7 adds create/update, the assignee-replacement surface, and the SPA. This keeps the story focused.
- **Status from the qualification, not per-assignment copies:** each assignment inherits the qualification's current expiry model on read. Editing a qualification's `expires_at` changes every assignment's status immediately — simple, consistent with live resolution, and avoids a per-assignment expiry copy column.
- **Assignee replacement via `qualifications/{id}/assignees`** mirrors the `/user-groups/{id}/members` pattern from Story 2.6 (delete + insert in one transaction, no data-modifying CTE).
- **AD-7 inspection gating is Story 5.1** — this story provides the vocabulary CRUD, assignment, and live-resolution foundation only.

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go and web tests pass, 0 lint issues
- `just migrate-up` -- expected: migration state unchanged (no new migration; 000012 tables reused)
- `curl` `GET /api/v1/admin/qualifications` as admin -- expected: 200 with status indicators + roster; as helfende -- expected: uniform 403
- `curl` create unlimited + fixed qualifications, assign a volunteer, then fetch their user detail -- expected: 201/200, assignment reflected immediately; expired fixed qual shows Abgelaufen
- Live: remove an assignment, then fetch the user detail -- expected: gone immediately (AD-7/FR-22)

## Suggested Review Order

**Entry point**

- Domain: vocabulary CRUD + assignee replacement, `qualifications.manage` re-check, status derivation reused from Story 2.6, validation, sentinel errors.
  [`qualifications.go:194`](../../internal/user/core/qualifications.go#L194)

- Route wiring: /qualifications sub-mount gated by qualifications.manage.
  [`admin.go:87`](../../internal/user/adapters/http/admin.go#L87)

**HTTP contract**

- Five handlers: list (status + roster), create, update, assignee list + replace, uniform 400/403/404/409.
  [`admin_qualifications.go:33`](../../internal/user/adapters/http/admin_qualifications.go#L33)

**Persistence**

- Transactional create/update/assignee-replacement (delete+insert, no data-modifying CTE).
  [`qualifications.go:1`](../../internal/user/adapters/postgres/qualifications.go#L1)

**SPA surfaces**

- Qualifikationen page: list with status badges, create/edit, assignee editor, 401/403 handling, error reset.
  [`AdminQualifikationenPage.tsx:25`](../../web/src/pages/admin/AdminQualifikationenPage.tsx#L25)

- QualificationEditor: expiry-kind radio + date picker (end-of-day alignment), server success microcopy.
  [`QualificationEditor.tsx:42`](../../web/src/components/QualificationEditor.tsx#L42)

- AssigneeEditor: roster merge (no hidden assignees), German feedback.
  [`AssigneeEditor.tsx:34`](../../web/src/components/AssigneeEditor.tsx#L34)

- API layer: types + calls.
  [`qualifications.ts:1`](../../web/src/auth/qualifications.ts#L1)

**Tests**

- Core unit: CRUD, dup, invalid, unknown, forbidden, status (unlimited/fixed/expired/soon), dedupe, immediate-effect.
  [`qualifications_test.go:1`](../../internal/user/core/qualifications_test.go#L1)

- Postgres integration: CRUD/assign/atomic, update-unknown-404, dup-name branches, live-resolution.
  [`qualifications_test.go:1`](../../internal/user/adapters/postgres/qualifications_test.go#L1)

- Web: list/create/edit/assign/status/error/401.
  [`AdminQualifikationenPage.test.tsx:1`](../../web/src/pages/admin/AdminQualifikationenPage.test.tsx#L1)