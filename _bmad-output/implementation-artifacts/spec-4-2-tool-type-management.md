---
title: 'Tool Type Management (tool_types.manage)'
type: 'feature'
created: '2026-09-09'
status: 'done'
review_loop_iteration: 0
baseline_commit: '8f07021ea0b63072703d621976ee71d74c7c5846'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-4-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Tool types don't exist — every physical tool would have no consistent inspection template (name, default schedule, required qualification, inspection mode, checklist items), so tools can't inherit inspection behaviour and the equipment universe has no structure.

**Approach:** Materialize the Tool Maintenance module (`internal/tools`) with Story 4.2's tool-type management: a migration-backed `tool_types` table (typed columns + `attributes JSONB`, FK to the Admin `schedules` catalog and to the User `qualifications` vocabulary) with ordered `tool_type_checklist_items` child rows, a Tool-module core service + postgres store + HTTP surface, the Tool configuration port (AD-10), and a SPA Tool catalogue "Typen" tab. Gated by `tool_types.manage` (AD-6).

## Boundaries & Constraints

**Always:**
- **Migration 000021** `tool_types.{up,down}.sql` — Tool-owned table: `id` (uuidv7), `name` (text UNIQUE), `default_schedule_id` uuid NOT NULL REFERENCES `schedules(id)` (Admin-owned catalog, AD-16), `required_qualification_id` uuid NOT NULL REFERENCES `qualifications(id)` (User-owned vocabulary, AD-7/AD-11), `inspection_mode` text CHECK (`pass_fail`|`checklist`), `attributes` jsonb NOT NULL DEFAULT '{}', `archived_at` timestamptz NULL (soft archive, mirrors the schedules convention), `created_at`, `updated_at`. Plus `tool_type_checklist_items` (id uuidv7, `tool_type_id` FK ON DELETE CASCADE, `position` int NOT NULL, `label` text NOT NULL, created_at/updated_at) — ordered child rows, the codebase's first ordered-child table (position is explicit, matching the spine's "ordering is code-level" note). `attributes` default `'{}'::jsonb` follows the `users.attributes` precedent (AD-3). Down reverses both.
- **Single required qualification** (FR-8/FR-11 are singular): `required_qualification_id` is ONE FK column, not a join table. The architecture spine's `tool_type_qualifications` name predates the epic AC and is superseded by it.
- **sqlc.yaml:** add a **tools** sql block (`schema: ["./migrations/000021_tool_types.up.sql"]`, `queries: "./internal/tools/adapters/postgres/queries.sql"`, `out: "./internal/tools/adapters/postgres"`, pgx/v5, emit_json_tags). Run `just sqlc-generate`. Existing user/admin blocks unchanged.
- **Tool core** `internal/tools/core/tool_types.go` (new): `ToolTypesManagePermission = "tool_types.manage"` const, audit ops (`tool_type.create`/`tool_type.update`/`tool_type.archive`), sentinels + German `Msg*` consts, domain `ToolType` + `ToolTypeChecklistItem`, input types, `ToolTypeStore` port (List/Get/Create/Update/Archive), permission re-check, best-effort audit. `NewService` wires: the store, a read-only `SchedulesPort` (Admin, validates `default_schedule_id` is an ACTIVE schedule), a read-only qualification catalog port (User, validates `required_qualification_id` exists), the `AuditWriter` (user repo), and a logger.
- **Qualification catalog port (new):** the User module has no ungated read-only qualification port; add `QualificationCatalogPort` (e.g. `QualificationExists(ctx, id)`) implemented on the user core, wired at the composition root — so the Tool write path validates the FK without joining user tables (AD-7/AD-11).
- **HTTP** `internal/tools/adapters/http` (new): `tool_types.go` handlers + DTOs + `ToolTypeRoutes()` (chi router, uniform envelope, no gate inside). Mount `/api/v1/admin/tool-types` behind its own `RequireAnyPermission([...ToolTypesManagePermission])` in `cmd/server/main.go`. Endpoints: `GET` (active list with checklist items), `POST` (create), `PUT /{id}` (update incl. full checklist-item replacement), `POST /{id}/archive`. 403 → no tool-type data exposed.
- **Checklist items:** create/update take an ordered `items: [{label}]` array — full replacement on update (the surface always submits the whole ordered list); archived or historical items are never touched (FR-23: changes reflect on future inspections only; history lives with the Tool module's future inspection records).
- **SPA:** materialize `AdminWerkzeugePage` into the Tool catalogue with a "Typen" tab (route `/admin/werkzeuge` already wired, nav already gated by `tool_types.manage`/`tools.manage`). Type editor: Name, default-schedule dropdown (reuses `listSchedules` client), required-qualification dropdown (reuses `listQualifications` client), inspection-mode switch, ordered checklist-item editor (add/remove/up/down) when mode is checklist, archive per row; inline German feedback, ≥48px targets, sticky actions.
- **Audit (NFR-O1/O2):** create/update/archive recorded with actor, timestamp, operation, best-effort.

**Ask First:**
- None (epic AC + FR-8/FR-11 resolve the design; the spine's join-table name is superseded by the epic's singular FK).

**Never:**
- No hard delete of tool types (archive only; FK history preserved).
- No JSONB for core fields (default_schedule_id/required_qualification_id/inspection_mode are typed FK/CHECK columns, AD-3/AD-10).
- No join-table `tool_type_qualifications` (single FK per FR-8/FR-11).
- No Tool module joining user/qualification tables or admin/schedules tables directly — always through the ports (AD-7/AD-10/AD-11).
- No changes to committed migrations; user/admin sqlc blocks unchanged.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| GET_LIST_EMPTY | no tool types | 200 `[]` | n/a |
| GET_LIST | active + archived | 200 only active, each with ordered checklist items | n/a |
| CREATE_VALID | name + valid schedule FK + valid qualification FK + mode | 200/201 created, audited | n/a |
| CREATE_INVALID | bad mode / empty name / missing FK | 400 German | 400 |
| CREATE_BAD_SCHEDULE | default_schedule_id not an active schedule | 400 German (port lookup) | 400 |
| CREATE_BAD_QUALIFICATION | required_qualification_id unknown | 400 German (port lookup) | 400 |
| CREATE_DUPLICATE | same name (case-insensitive) | 400 German duplicate-name | 400 |
| UPDATE_REPLACE_ITEMS | edit name/mode + new ordered items | Persisted (full item replacement), audited | n/a |
| UPDATE_ARCHIVED | update an archived type | 404 sentinel | 404 |
| ARCHIVE | archive active type | `archived_at` set, leaves active list, audited | n/a |
| FORBIDDEN | caller lacks tool_types.manage | Uniform 403, no data exposed (AD-6) | 403 |

</frozen-after-approval>

## Code Map

- `migrations/000021_tool_types.{up,down}.sql` -- tool_types + tool_type_checklist_items (FKs to schedules + qualifications, position-ordered child rows).
- `sqlc.yaml` -- add the **tools** sql block; `just sqlc-generate`.
- `internal/user/core/qualifications.go` / `internal/user/ports/ports.go` -- add the ungated `QualificationCatalogPort` (e.g. `QualificationExists`) so the Tool write path validates the FK (AD-7).
- `internal/tools/core/core.go` (seed) + `tool_types.go` (new) -- service, domain, validation, permission re-check, audit, store port.
- `internal/tools/ports/ports.go` (seed) -- inbound `Service` port + read-only consumer ports.
- `internal/tools/adapters/postgres/` (new) -- sqlc store: queries (list-active incl. items, get, create with items, update with item replacement, archive), repo.
- `internal/tools/adapters/http/` (new) -- `tool_types.go` handlers + DTOs + `ToolTypeRoutes()`.
- `cmd/server/main.go` -- wire tools store/core + `SchedulesPort` + `QualificationCatalogPort` + `AuditWriter`; mount `/api/v1/admin/tool-types` behind its own `tool_types.manage` gate.
- `web/src/pages/admin/AdminWerkzeugePage.tsx` (+css) -- materialize the Tool catalogue with the "Typen" tab (list + editor + checklist-item ordering + archive).
- Tests: tools core (validation/duplicate/permission/archive/audit/FK-port checks), postgres (CRUD + item replacement + FK constraints + seed-free), http (200/400/404/403), composition mount gate, web (tab, editor, checklist ordering, 401/403).

## Tasks & Acceptance

**Execution:**
- [x] `migrations/000021_tool_types.{up,down}.sql` -- tables + FKs + ordered child rows -- schema
- [x] `sqlc.yaml` + `just sqlc-generate` -- tools sql block -- persistence
- [x] `internal/user/core/qualifications.go` (+ports) -- `QualificationCatalogPort` -- FK validation port
- [x] `internal/tools/core/tool_types.go` (+core.go consts) -- service/domain/validation/audit -- core
- [x] `internal/tools/ports/ports.go` -- Service + consumer ports -- ports
- [x] `internal/tools/adapters/postgres/` -- sqlc store + repo (create-with-items, item replacement, archive) -- persistence
- [x] `internal/tools/adapters/http/tool_types.go` -- handlers/DTOs + ToolTypeRoutes -- API
- [x] `cmd/server/main.go` -- tools wiring + mount under tool_types.manage gate -- composition root
- [x] `web/src/pages/admin/AdminWerkzeugePage.tsx` (+css) -- Tool catalogue "Typen" tab -- SPA
- [x] Tests -- core/postgres/http/composition/web incl. I/O rows -- verification
- [x] `docs/docs/planning/architecture-spine.md` -- align the qualification-link naming with the epic's single FK if needed -- docs

**Acceptance Criteria:**
- Given an admin/Schirrmeister with `tool_types.manage`, when they open Tool catalogue → "Typen", then they can create/edit a tool type with Name, default schedule (FK from catalog), required Qualification (FK), and inspection mode pass/fail vs checklist (FR-8/UX-DR5/UX-DR8).
- Given a checklist-mode tool type, when the admin manages its checklist items, then items can be added, ordered, and removed (FR-23); changes affect future inspections only.
- Given a saved tool type, then core fields map to typed columns and `attributes` JSONB is available for custom metadata (FR-10/AD-3), and the write path goes through the Tool module's configuration port (AD-10).
- Given a caller lacks `tool_types.manage`, then every endpoint answers uniform 403 with no tool-type data exposed (AD-6).

## Spec Change Log

- **Review patches applied (review 1, 2026-09-09):** SPA Typen tab now loads tool types as the primary content and degrades the schedule/qualification catalog dropdowns on 403/error instead of ejecting the module (Schirrmeister/Führende hold `tool_types.manage` but not `schedules.manage`/`qualifications.manage`); FK violations (23503) mapped to a German 400 (race between port check and insert no longer yields a raw 500); checklist mode requires ≥1 item; nil FK ports fail loudly instead of silently skipping validation; duplicate-name semantics made coherent (case-insensitive among active, archived exact-name reuse rejected via DB UNIQUE → German 400, case-variant of archived name allowed); lean `ToolTypeExistsActive` replaces item-fetching `GetToolType` on the update path; migration 000022 adds `UNIQUE(tool_type_id, position)`; duplicate checklist labels rejected; save button disabled + German hint when schedule/qualification not selected; spine table numbering renumbered; migrations README gap noted; tablist/tabpanel a11y + visible focus rings; postgres list-order test pins oldest-first with name tiebreaker.

## Design Notes

- **Single FK, not a join table:** FR-8 ("required Qualification") and FR-11 ("the Qualification required by the Tool Type") are singular; `required_qualification_id` is one FK column. The spine's `tool_type_qualifications` table name predates the epic AC and is superseded.
- **Checklist items = ordered child rows:** first `position`-column table in the codebase (spine: "ordering is code-level"). Update semantics are full-replacement of the ordered item list (the SPA always submits the whole list), matching "add/order/remove" with history preserved at the inspection layer.
- **Cross-module FKs validated through ports:** `default_schedule_id` is validated against the Admin `SchedulesPort` (active only), `required_qualification_id` against the new User `QualificationCatalogPort` — the Tool module never joins another module's tables (AD-7/AD-10/AD-11).

## Verification

**Commands:**
- `just sqlc-generate` && `just migrate-up` -- expected: 000021 applies; `\d tool_types`; `\d tool_type_checklist_items`
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go/web tests pass, 0 lint issues
- `curl` as admin: GET /api/v1/admin/tool-types (200); POST create (200, incl. checklist items); PUT update replacing items (200); POST /{id}/archive (200); GET no longer lists archived -- expected per matrix
- `curl` as non-admin: GET -- expected: uniform 403 with no tool-type data
- `npx vitest run` in web/ -- expected: all pass incl. Typen-tab tests

**Manual checks (if no CLI):**
- Tool catalogue → "Typen" renders the list; create/edit with schedule + qualification dropdowns + checklist-item ordering; archive with confirm; inline German feedback.