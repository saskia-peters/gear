---
title: 'Tool Inventory Numbers + tool.edit Permission (deferred from 4-3)'
type: 'feature'
created: '2026-09-09'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'c7a756f8824d71880762e9bd88fb076e2d1682ce'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-4-context.md'
  - '{project-root}/_bmad-output/implementation-artifacts/deferred-work.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Tools have no inventory number, so there is no human-readable unique identifier per physical tool; manual creation can't assign one and a CSV/Excel import has nothing unique to reject duplicates against.

**Approach:** Add the inventory-number system deferred from Story 4-3: a unique `inventory_number` column (text, char not number-only) on `tools`, auto-assigned `GEAR` + zero-padded incrementing number from a new sequence on manual creation, editable afterward via PUT, and a `UNIQUE` backstop so Story 4.5 import rows with an existing number are rejected. A new scoped `tool.edit` permission (granted to admin/schirrmeister/fuehrende) lets holders view + edit tools (incl. the inventory number) without create/archive; create/archive stay `tools.manage`.

## Boundaries & Constraints

**Always:**
- **Migration 000025** `tool_inventory_number.{up,down}.sql` — Tool-owned changes + User-owned permission seed in one file (sqlc ignores data INSERTs, so it's safe to append to the tools sqlc block):
  - `ALTER TABLE tools ADD COLUMN inventory_number text` then `UPDATE tools SET inventory_number = 'GEAR' || lpad(nextval('tools_inventory_number_seq')::text, 6, '0')` (backfill existing rows) then `ALTER TABLE tools ALTER COLUMN inventory_number SET NOT NULL` + `CREATE UNIQUE INDEX tools_inventory_number_key ON tools (inventory_number)` (UNIQUE across ALL rows incl. archived — the 4.5 import backstop). Plus `CREATE SEQUENCE tools_inventory_number_seq` (start 1, INCREMENT 1). Optional `CHECK (char_length(inventory_number) <= 16)`.
  - Seed `tool.edit` permission + grants to admin/schirrmeister/fuehrende, following the 000016 pattern (`INSERT INTO permissions ... ON CONFLICT DO NOTHING`, role grants via `permission_group_permissions` with `ON CONFLICT DO NOTHING`). Down reverses both halves (revoke grants, delete permission, drop column/sequence).
- **Auto-assignment on manual create (user requirement):** the store's `CreateTool` INSERT computes `'GEAR' || lpad(nextval('tools_inventory_number_seq')::text, 6, '0')` in-SQL (atomic, one round-trip, no new query). The client's `ToolInput` does NOT carry `inventory_number` on create (server-assigned). If the generated value collides with an existing row (manual edit took a future value), the INSERT trips the UNIQUE constraint → a bounded retry loop (re-fetch nextval + re-insert, ≤3 attempts) maps the final failure to a German error (mirror the `isUniqueViolation` mapping).
- **Editable afterward:** `UpdateTool` accepts an optional `inventory_number` (non-empty, bounded, case-insensitively unique among active tools via core guard + DB UNIQUE backstop for exact archived reuse). Any `tool.edit`/`tools.manage` holder can edit it.
- **Permission split (user decision):**
  - `GET` (list) + `PUT /{id}` (update) → any-of `[tools.manage, tool.edit]` (Führende with only `tool.edit` can view + edit tools incl. the inventory number).
  - `POST` (create) + `POST /{id}/archive` → `tools.manage` only.
  - Implement via the codebase's per-sub-surface gate precedent (mirror `internal/user/adapters/http/admin.go`): the outer mount gate in `main.go` becomes any-of `[tools.manage, tool.edit]`, and inside `ToolRoutes()` a write-only sub-router for `POST /` + `POST /{id}/archive` re-applies a `tools.manage`-only `RequireAnyPermission` (real auth middleware, tighter gate). Core re-checks: `ListTools`/`UpdateTool` → any-of, `CreateTool`/`ArchiveTool` → `tools.manage`-only (parameterize `requireToolsPermission` with the required code list).
- **Catalogs:** add `tool.edit` to `AdminModuleAccessCodes()` (internal/user/core/users_admin_permissions.go), `BasePermissionCodes` + German label in `roles.go` + `web/src/auth/roles.ts`, and the SPA `werkzeuge` nav codes in `permissions.ts` (so Führende reach the surface).
- **SPA:** `TOOL_EDIT_PERMISSION` const + `inventory_number` on `Tool`/`ToolInput`/`DashboardTool` in `web/src/auth/tools.ts`. `AdminWerkzeugePage` gates the Werkzeuge tab any-of `[tools.manage, tool.edit]`; the editor shows the inventory number as a read-only display on create (server auto-assigns) and an editable text input on edit; the list row shows it; create/archive buttons are hidden for `tool.edit`-only holders. The GEAR-module Dashboard `Werkzeugliste` also shows `inventory_number` as a row meta (read surface).
- **Story 4.5 import:** NOT built here — this story only lays the column + UNIQUE backstop so the import rejects duplicate-number lines later.
- **Audit (NFR-O1/O2):** create/update/archive audited as before; the inventory edit is part of `tool.update`.

**Ask First:**
- None (the deferred-work record + user's `tool.edit` decision resolve the design).

**Never:**
- No inventory number on the client create body (server auto-assigns `GEAR%06d`).
- No numeric-only restriction (text, char not number-only).
- No hard delete of tools; no changes to committed migrations.
- No changes to the dashboard `GET /api/v1/tools` gate (`dashboard.view`); the admin `/api/v1/admin/tools` surface gains the any-of read gate but POST/archive stay `tools.manage`.
- No CSV/Excel import implementation (Story 4.5).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| CREATE_AUTO | manual create, no inventory in body | Inventory auto-assigned `GEAR000001`, `GEAR000002`, … (zero-padded, monotonic) | n/a |
| CREATE_BACKFILL | existing tools before 000025 | Backfilled `GEAR%06d` from the seq; NOT NULL after | n/a |
| CREATE_COLLISION | generated GEAR value already taken (manual edit took it) | Retry loop (≤3): next seq value inserted | final failure → German 400 |
| CREATE_IGNORE_CLIENT | client sends inventory_number on create | Ignored — server-assigned | n/a |
| UPDATE_INVENTORY | PUT edits inventory number | Persisted, uniqueness enforced, audited (tool.update) | 400 German duplicate-inventory |
| UPDATE_CLEAR | PUT clears inventory | 400 (never empty; a tool always has an inventory number) | 400 |
| EDITOR_VIEW | tool.edit-only holder opens Werkzeuge tab | List + editor visible; create/archive buttons HIDDEN | n/a |
| GATE_GET | tool.edit-only GET /api/v1/admin/tools | 200 (any-of read) | n/a |
| GATE_CREATE | tool.edit-only POST /api/v1/admin/tools | 403 (tools.manage-only) | 403 |
| GATE_ARCHIVE | tool.edit-only archive | 403 (tools.manage-only) | 403 |
| GATE_UPDATE | tool.edit-only PUT | 200 (any-of) | n/a |
| DASHBOARD | dashboard.view holder | List shows inventory_number meta | n/a |
| FORBIDDEN | no tools.manage/tool.edit | Uniform 403, no tool data (AD-6) | 403 |

</frozen-after-approval>

## Code Map

- `migrations/000025_tool_inventory_number.{up,down}.sql` -- tools.inventory_number + seq + UNIQUE + backfill + tool.edit seed/grants.
- `sqlc.yaml` -- append 000025 to the tools block; `just sqlc-generate`.
- `internal/user/core/users_admin_permissions.go` -- `tool.edit` in `AdminModuleAccessCodes()`.
- `internal/user/core/roles.go` + `web/src/auth/roles.ts` -- `tool.edit` in `BasePermissionCodes` + German label ("Gerätenummer bearbeiten" or similar).
- `internal/tools/core/tools.go` -- `ToolEditPermission` const; `inventory_number` on `Tool`; parameterize `requireToolsPermission` for the any-of/single split (list/update any-of, create/archive tools.manage-only); optional inventory in `ToolInput`; validation (non-empty, bounded, unique).
- `internal/tools/adapters/postgres/queries.sql` + `tools_repo.go` -- `inventory_number` in List/Create/Update/Archive SELECTs; CreateTool INSERT computes `GEAR || lpad(nextval(...), 6, '0')` + retry loop; UpdateTool sets inventory_number (when provided); mapper adds it.
- `internal/tools/adapters/http/tools.go` -- `inventory_number` on `toolDTO` + `dashboardToolDTO`; write-only sub-router inside `ToolRoutes()` gated `tools.manage`-only (POST + archive), outer mount any-of.
- `cmd/server/main.go` -- outer mount gate any-of `[tools.manage, tool.edit]`.
- `web/src/auth/tools.ts` -- `TOOL_EDIT_PERMISSION`; `inventory_number` on `Tool`/`ToolInput`/`DashboardTool`.
- `web/src/pages/admin/AdminWerkzeugePage.tsx` (+css) -- any-of tab gate; editor inventory field (read-only on create, editable on edit); list row meta; hide create/archive for tool.edit-only.
- `web/src/pages/DashboardPage.tsx` -- inventory_number row meta.
- Tests: core (auto-assign/ignore-client/backfill/update-unique/permission-split/retry), postgres (seq + uniqueness + round-trip + backfill), http (per-action gates + DTO fields), composition mount (any-of read, tools.manage write), web (editor inventory, hide create/archive for tool.edit, dashboard meta, 401/403).

## Tasks & Acceptance

**Execution:**
- [x] `migrations/000025_tool_inventory_number.{up,down}.sql` -- column + seq + backfill + UNIQUE + tool.edit seed -- schema/permission
- [x] `sqlc.yaml` + `just sqlc-generate` -- tools block append -- persistence
- [x] `internal/user/core/users_admin_permissions.go` + `roles.go` (+ web roles.ts) -- tool.edit in gate + catalog -- gate
- [x] `internal/tools/core/tools.go` -- ToolEditPermission + inventory domain + permission split + validation -- core
- [x] `internal/tools/adapters/postgres/queries.sql` + `tools_repo.go` -- inventory in CRUD + auto-assign + retry + update -- persistence
- [x] `internal/tools/adapters/http/tools.go` -- DTO fields + write-only sub-gate -- API
- [x] `cmd/server/main.go` -- outer mount any-of gate -- composition root
- [x] `web/src/auth/tools.ts` + `permissions.ts` -- consts + types + client -- SPA
- [x] `web/src/pages/admin/AdminWerkzeugePage.tsx` (+css) + `DashboardPage.tsx` -- editor/list/dashboard inventory + tab gating -- SPA
- [x] Tests -- core/postgres/http/composition/web incl. I/O rows -- verification
- [x] `migrations/README.md` + `internal/tools/README.md` -- document 000025 + the any-of gate -- docs

**Acceptance Criteria:**
- Given a tool is created manually, then it is assigned a unique inventory number `GEAR` + zero-padded incrementing number (char, not number-only).
- Given a Führende/Schirrmeister/Admin, then they can change a tool's inventory number via edit (uniquely enforced); a Führende with `tool.edit` can view + edit but NOT create or archive.
- Given a caller lacks `tools.manage`/`tool.edit`, then every endpoint answers uniform 403 with no tool data (AD-6).
- Given an import row (Story 4.5) whose inventory number already exists, then the UNIQUE backstop rejects the line (prepared here).

## Spec Change Log

- **Review patches applied (review 1, 2026-09-09):** `tool.edit` added to the `TestAdminModuleAccessCodesCoversSubMountGates` required slice (dropping it now fails the superset invariant); the archived-tool inventory backstop is pinned (`TestPostgresToolInventoryArchivedBackstop` archives a tool then reuses its number → `MsgToolInventoryNumberTaken`, and a nextval landing on an archived number retries); the UNIQUE index is now a functional index on `lower(inventory_number)` so ALL-row uniqueness is case-insensitive, matching the core guard and the import backstop; `tool.edit` ordering aligned across roles.go/roles.ts/repository_test.go + stale "23" comments fixed; label renamed to "Geräte bearbeiten" (accurate for the view+edit grant); the 000025 down migration deletes ALL `tool.edit` grants (custom roles included) before the permission row; uniform gate deny-message ("tools.manage/tool.edit access denied"); SPA `maxLength={16}` mirror documented; new composed E2E test proves the auto-assigned `GEAR%06d` round-trips through the real wiring; the edit-only PUT assertion now verifies the inventory reached the fake service; negative seed pin (helfende lacks `tool.edit`); the 404-sentinel-wins-over-invalid-inventory ordering pinned; >999999 pad widening documented; `NewHandler` panics loudly on nil validator/resolver; the Werkzeuge tab default-guards the neither-code case + a `NO_SURFACE_CODE` web test. Recorded in deferred-work.md: the same cross-package parallel-DB contention now affects the new DB-backed tools/cmd tests (green with `-p 1`/`just test`), and the seeded `fuehrende` `tool.edit` grant is a no-op for base roles (fuehrende already holds `tools.manage` — the tool.edit-only scenario needs a custom role).

## Design Notes

- **Auto-assignment is in-SQL (atomic):** `'GEAR' || lpad(nextval('tools_inventory_number_seq')::text, 6, '0')` inside the `CreateTool` INSERT gives monotonic zero-padded numbers with no client input and no extra query. A manual edit can consume a future sequence value; the bounded retry loop (≤3) handles the collision.
- **Permission split mirrors the admin sub-surface precedent:** the outer mount gate is any-of for reads (GET/PUT), and a write-only chi sub-router inside `ToolRoutes()` re-applies `tools.manage`-only for POST/archive — the exact pattern used by `internal/user/adapters/http/admin.go` sub-mounts. The core re-checks the same split.
- **UNIQUE across all rows (incl. archived)** is intentional: it's the Story 4.5 import backstop (an archived tool's number is still "taken"). The core edit guard is case-insensitive among active tools; the DB constraint is exact over all rows.
- **Backfill:** existing tools (e.g. the user's "B - auf dem GKW") get `GEAR%06d` from the sequence on migration, so NOT NULL is satisfiable before the constraint is added.

## Verification

**Commands:**
- `just sqlc-generate` && `just migrate-up` -- expected: 000025 applies; `\d tools` shows inventory_number NOT NULL + UNIQUE; `SELECT * FROM permissions WHERE code='tool.edit'`; existing tools backfilled
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go/web tests pass, 0 lint issues
- `curl` as admin: POST /api/v1/admin/tools (200, inventory auto-assigned GEARxxxxxx); PUT changing inventory (200); GET shows it -- expected per matrix
- `curl` as Führende (tool.edit only): GET/PUT (200); POST/archive (403)
- `curl` as non-holder: GET -- expected: uniform 403 with no tool data
- `npx vitest run` in web/ -- expected: all pass incl. inventory field + tool.edit gating tests

**Manual checks (if no CLI):**
- Werkzeuge tab: create assigns GEARxxxxxx (read-only), edit lets you change it, list shows it; a Führende sees the list/editor without create/archive buttons; dashboard shows the inventory number.