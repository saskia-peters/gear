---
title: 'Tool Management (tools.manage)'
type: 'feature'
created: '2026-09-09'
status: 'done'
review_loop_iteration: 0
baseline_commit: '619d636a106dc08239cf0a3f21637cfb58f48f99'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-4-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Individual physical tools don't exist — a Schirrmeister/Admin can define tool types (Story 4.2) but cannot track each physical tool that belongs to a type, nor give one an optional schedule override.

**Approach:** Add a second Tool-module surface — tool management. A `tools.manage` holder opens the Tool catalogue → "Werkzeuge" tab and creates/edits individual tools belonging to exactly one tool type, with an optional per-tool schedule override (first-class FK to the Admin `schedules` catalog; empty → inherit the type default). Core fields map to typed columns + `attributes JSONB` (FR-10/AD-3); the write path goes through the Tool configuration port (AD-10); gated by `tools.manage` (AD-6). The inventory-number system (auto-assigned `GEAR` numbers, `tool.edit`) is deferred to a follow-up story (spec split).

## Boundaries & Constraints

**Always:**
- **Migration 000024** `tools.{up,down}.sql` — Tool-owned table: `id` (uuidv7), `name` (text UNIQUE — the "identifier"), `tool_type_id` uuid NOT NULL REFERENCES `tool_types(id)` (intra-module), `schedule_id` uuid NULL REFERENCES `schedules(id)` (optional per-tool override, Admin catalog), `attributes` jsonb NOT NULL DEFAULT '{}', `archived_at` timestamptz NULL (soft archive), `created_at`, `updated_at`. `CREATE INDEX tools_tool_type_id_idx`. Down reverses. Append `000024` to the **tools** sqlc block; `just sqlc-generate`.
- **Optional schedule override (FR-9/AD-10):** empty `schedule_id` persists as SQL NULL and means "inherit the tool type's default schedule" (AD-5); a non-empty `schedule_id` is validated against the Admin `SchedulesPort` (active only) and is a first-class FK, never JSONB. Mirror the optional-qualification precedent: empty → skip the port check and store NULL (repo `parseToolTypeID` empty→`pgtype.UUID{}`→NULL).
- **tool_type_id validation:** the tool's type must EXIST and be ACTIVE — validated via the Tool module's own store `ToolTypeExistsActive` (intra-module, no permission needed, no cross-module join). A missing/archived type → German 400. Archiving a tool_type with active tools is allowed (soft; the tools list JOIN keeps showing the type name).
- **Tool core** `internal/tools/core/tools.go` (new): `ToolsManagePermission = "tools.manage"` const, audit ops (`tool.create`/`tool.update`/`tool.archive`), sentinels + German `Msg*` consts, domain `Tool` (ID, Name, ToolTypeID, ScheduleID empty=inherit, Attributes, ArchivedAt), `ToolInput`, `ToolStore` port (ListTools active with type name via JOIN / ToolExistsActive / CreateTool / UpdateTool / ArchiveTool), permission re-check, best-effort audit, validation (name required + bounded + global-unique guard; type exists-active; empty override allowed; non-empty override validated against `SchedulesPort`). Same six `NewService` deps as tool_types suffice (store + SchedulesPort + QualificationCatalogPort + perms + audit + logger) — the QualificationCatalogPort is unused by tools but kept for the shared constructor.
- **HTTP** `internal/tools/adapters/http/tools.go` (new): `ToolRoutes()` handlers + DTOs (name, tool_type_id, schedule_id, attributes passthrough) mounted at `/api/v1/admin/tools` behind its own `RequireAnyPermission([...ToolsManagePermission])` gate in `cmd/server/main.go`. Endpoints: `GET` (active list, each with tool_type name), `POST` (create), `PUT /{id}` (update), `POST /{id}/archive`. Uniform envelope, German messages, 403 → no tool data exposed.
- **SPA:** materialize the "Werkzeuge" tab in `AdminWerkzeugePage.tsx` (replace the EmptyState placeholder) with a `ToolsTab` mirroring `ToolTypesTab` minus the checklist/mode editor: Name, tool_type dropdown (reuse `listToolTypes` client; degrade to empty + disabled save on 403 like the Typen tab), optional schedule-override dropdown (reuse `listSchedules`; explicit default option "Standard für diesen Typ" meaning empty/inherit), archive per row with confirm; inline German feedback, ≥48px targets, sticky actions. Add `TOOLS_PERMISSION` const + `Tool`/`ToolInput` types + client in `web/src/auth/tools.ts` (list/create/update/archive).
- **Audit (NFR-O1/O2):** create/update/archive recorded with actor, timestamp, operation, best-effort.

**Ask First:**
- None (the AC + existing conventions resolve the design; global-unique name chosen to match the tool_types codebase convention).

**Never:**
- No hard delete of tools (archive only).
- No JSONB for the core FK fields (tool_type_id/schedule_id are typed FK columns, AD-3/AD-10).
- No Tool module joining Admin `schedules` or User tables directly — schedule override validated through the `SchedulesPort` (AD-10/AD-11).
- No changes to committed migrations; tools sqlc block gains 000024, user/admin blocks unchanged.
- No checklist items on tools (type-level only, Story 4.2).
- No inventory-number work here (deferred to the follow-up story).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| GET_LIST_EMPTY | no tools | 200 `[]` | n/a |
| GET_LIST | active + archived tools | 200 only active, each with its tool_type name | n/a |
| CREATE_VALID | name + active type + no override | 200/201 created, `schedule_id` NULL, audited | n/a |
| CREATE_OVERRIDE | name + type + valid active schedule | 200/201 created with FK override, audited | n/a |
| CREATE_BAD_TYPE | tool_type_id missing/archived | 400 German (type must exist + be active) | 400 |
| CREATE_BAD_OVERRIDE | schedule_id unknown/archived | 400 German (port lookup) | 400 |
| CREATE_INVALID | empty name / too long | 400 German | 400 |
| CREATE_DUPLICATE | same name (case-insensitive, active) | 400 German duplicate-name | 400 |
| UPDATE_CLEAR_OVERRIDE | edit removing the override | `schedule_id` set to NULL → inherits type default | n/a |
| UPDATE_ARCHIVED | update an archived tool | 404 sentinel | 404 |
| ARCHIVE | archive active tool | `archived_at` set, leaves active list, audited | n/a |
| FORBIDDEN | caller lacks tools.manage | Uniform 403, no tool data exposed (AD-6) | 403 |

</frozen-after-approval>

## Code Map

- `migrations/000024_tools.{up,down}.sql` -- tools table (FKs to tool_types + optional schedule, UNIQUE name, attributes JSONB, soft archive).
- `sqlc.yaml` -- append 000024 to the tools block; `just sqlc-generate`.
- `internal/tools/core/tools.go` (new) + `core.go` consts -- service, domain, validation, permission re-check, audit, store port.
- `internal/tools/ports/ports.go` -- extend `Service` with the tool methods.
- `internal/tools/adapters/postgres/` -- tools queries (list JOIN tool_types for name, create, update with override-clear→NULL, archive) + `tools_repo.go` (reuse `parseToolTypeID`, `isUniqueViolation`, `isForeignKeyViolation`, `beginTx`).
- `internal/tools/adapters/http/tools.go` (new) -- `ToolRoutes()` + handlers/DTOs.
- `cmd/server/main.go` -- wire `tools.manage` gate + mount `/api/v1/admin/tools`.
- `web/src/auth/tools.ts` -- `TOOLS_PERMISSION`, `Tool`/`ToolInput`, list/create/update/archive client.
- `web/src/pages/admin/AdminWerkzeugePage.tsx` (+css) -- replace the Werkzeuge EmptyState placeholder with `ToolsTab` (list + editor + override dropdown + archive).
- Tests: tools core (validation/duplicate/permission/archive/audit/type-exists-active/bad-override), postgres (CRUD + override-clear→NULL + FK constraints + JOIN name, cleanup `tools` before `tool_types`), http (200/400/404/403), composition mount gate (`tools.manage` reaches /tools; tool_types.manage does not reach /tools and vice-versa), web (tab, editor, override select, archive, 401/403).

## Tasks & Acceptance

**Execution:**
- [x] `migrations/000024_tools.{up,down}.sql` -- tools table + index -- schema
- [x] `sqlc.yaml` + `just sqlc-generate` -- tools block append -- persistence
- [x] `internal/tools/core/tools.go` (+core.go consts + ports Service) -- domain/service -- core
- [x] `internal/tools/adapters/postgres/tools_repo.go` + queries -- CRUD + override-clear + JOIN -- persistence
- [x] `internal/tools/adapters/http/tools.go` -- handlers/DTOs + ToolRoutes -- API
- [x] `cmd/server/main.go` -- tools gate + mount -- composition root
- [x] `web/src/auth/tools.ts` -- client + TOOLS_PERMISSION -- SPA
- [x] `web/src/pages/admin/AdminWerkzeugePage.tsx` (+css) -- Werkzeuge tab -- SPA
- [x] Tests -- core/postgres/http/composition/web incl. I/O rows -- verification
- [x] `migrations/README.md` + `internal/tools/README.md` -- document 000024 + tools story -- docs

**Acceptance Criteria:**
- Given an admin/Schirrmeister with `tools.manage`, when they open Tool catalogue → "Werkzeuge", then they can create/edit a tool belonging to exactly one tool type (Name/identifier; optional per-tool schedule override) (FR-9/UX-DR5/UX-DR8).
- Given a per-tool schedule override is set, then it is a first-class FK to `schedules` (never JSONB) and overrides the type's default for the tool's inspection clock (FR-9/AD-10).
- Given the override is left empty, then the tool inherits its schedule from its tool type's default (FR-9/AD-5).
- Given a saved tool, then core fields map to typed columns and `attributes` JSONB is available (FR-10/AD-3).
- Given a caller lacks `tools.manage`, then every endpoint answers uniform 403 with no tool data exposed (AD-6).

## Spec Change Log

- **Scope split (2026-09-09, user chose [S]):** the inventory-number system (unique `inventory_number` column, `tools_inventory_number_seq`, auto-assigned `GEAR%06d` on manual create, editable by a scoped `tool.edit` granted to admin/schirrmeister/fuehrende, and the Story 4.5 import-duplicate backstop) is deferred to a follow-up story, recorded in deferred-work.md. The schedule-override dropdown default is labeled "Standard für diesen Typ" (empty/inherit), per the user's naming request.
- **Review patches applied (review 1, 2026-09-09):** UpdateTool now resolves existence/archived (404 sentinel) BEFORE FK validation (archived/unknown id + invalid FK no longer answers 400); update-path duplicate-name guard pinned (keep-own-name succeeds via the `exceptID` exclusion; update-to-conflict → German 400, at core + postgres); a dedicated `parseOptionalUUID` helper replaces the misleading `parseToolTypeID` reuse for the schedule override; ToolsTab schedules-403 degrade covered by a new jsdom test (list renders, no eject, save enabled); the override select no longer renders a duplicate empty-value "Keine Zeitpläne vorhanden" option; `startEdit` guards a missing type from a degraded catalog (clears selection, canSave off) and a stale since-archived override (clears to inherit); the no-permission fallback no longer emits dangling `tab-werkzeuge` aria; `tools_created_at_idx` added to 000024 for the list sort; the tool DTO now carries nullable `archived_at`; `tools.ts` ends with a newline. `tools.manage` was already in `AdminModuleAccessCodes()` + the SPA `werkzeuge` nav codes (non-findings).

## Design Notes

- **Optional override via NULL, inherit at resolution time (AD-5):** an empty `schedule_id` is stored as SQL NULL; the shared due-date function (Epic 5) resolves a tool's schedule as its own row if set, else its type's default. The surface shows the default option "Standard für diesen Typ" (empty string → inherit).
- **tool_type_id is intra-module:** validated against the Tool module's own `ToolTypeExistsActive` store method — no permission check, no cross-module join (AD-8/AD-11 only forbid joining other modules' tables; the tools list JOINs Tool-owned `tool_types` for the display name).
- **Global-unique name:** chosen to match the `tool_types` codebase convention (the AC only says "Name/identifier"); archived-name reservation and case semantics follow the tool_types precedent (DB UNIQUE case-sensitive over all rows, core guard case-insensitive over active).
- **No child rows:** `Tool` carries no checklist items — the biggest structural difference from tool_types.

## Verification

**Commands:**
- `just sqlc-generate` && `just migrate-up` -- expected: 000024 applies; `\d tools`
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go/web tests pass, 0 lint issues
- `curl` as admin: GET /api/v1/admin/tools (200); POST create (200, with/without override); PUT update clearing the override (200); POST /{id}/archive (200); GET no longer lists archived -- expected per matrix
- `curl` as non-admin: GET -- expected: uniform 403 with no tool data
- `npx vitest run` in web/ -- expected: all pass incl. Werkzeuge-tab tests

**Manual checks (if no CLI):**
- Tool catalogue → "Werkzeuge" renders the list; create/edit with type dropdown + optional override select; archive with confirm; inline German feedback; the override select shows "Standard für diesen Typ".