---
title: 'Tool Details with Inspection & Reinstatement History (FR-18)'
type: 'feature'
created: '2026-09-16'
status: 'done'
review_loop_iteration: 0
baseline_commit: '324679752ffe7e2247b33470b5c5176a0b92703d'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-6-context.md'
  - '{project-root}/_bmad-output/implementation-artifacts/epic-5-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Nothing shows a tool's inspection or reinstatement history — Fuehrung/Schirrmeister/Admin have no way to review the audit trail of a specific tool (FR-18).

**Approach:** Add a tool details page `/tools/:toolId` reachable by URL (the dashboard row navigation lands in a follow-up story). The page shows the tool header (from the dashboard list — all users) and, gated by `inspection.history.view`, the reverse-chronological inspections (date, inspector, outcome, notes, mode, per-checklist-item results) and reinstatements (date, actor, reason).

## Boundaries & Constraints

**Always:**
- **Details page** (`web/src/pages/ToolDetailsPage.tsx` NEW, route `/tools/:toolId` in `App.tsx` next to `/inspection/:toolId` L257-264): header (tool name, Gerätenummer, Gerätetyp, status chip) resolved from router state OR a `listDashboardTools()` refetch on deep-link/refresh (dashboard.view — every authenticated user). History section: fetch `GET /api/v1/tools/{id}/history`.
- **History gating (user decision):** every authenticated user can open the details page and see the header; the HISTORY content is gated by `inspection.history.view`. The SPA pre-checks `hasPermission('inspection.history.view')` and skips the fetch for non-holders (shows the German no-permission note); the SERVER is the real gate (403 → uniform envelope, no data). A 401 → `clearAuthState()` + `/login`; other errors → inline German.
- **Backend history endpoint** `GET /api/v1/tools/{id}/history`: NEW router `HistoryRoutes()` (mirroring `InspectionRoutes` tools.go L184-191) mounted in main.go L202-207 as `toolsSurface.Mount("/{id}/history", ...)`, gated by `auth.RequirePermission(sessionManager, userRepo, toolscore.InspectionHistoryViewPermission)` (new const in core/inspections.go next to L32/L38). Response: `{ inspections: [ { id, inspector_id, inspector_name, mode, overall_result, notes, submitted_at, items: [{item_id,label,position,result}] } ], reinstatements: [ { id, actor_id, actor_name, reason, created_at } ] }` — inspections ordered `submitted_at DESC, id DESC`, reinstatements `created_at DESC, id DESC` (the existing 000027 indexes already support both).
- **Display names** (new read-only seam, Tool module never joins user tables — AD-1/AD-8): tools core gains a narrow `DisplayNameResolver { ResolveDisplayNames(ctx, userIDs) (map[string]string, error) }` (precedent: `PermissionResolver` core/tools.go L185-187) + a param on `NewService` (tool_types.go L217). User postgres `Repository` implements it via a new `ListUsersByIDs :many` query (`SELECT id, display_name FROM users WHERE id = ANY($1)`). A user id absent from the result (deleted account, Story 3.4 not yet built) renders as the literal "Deleted User" in `inspector_name`/`actor_name` — never a 404, never an empty string.
- **Tool-exists check:** the core method loads the tool first (`GetToolWithTypeQualification` — archived/missing → `ErrToolNotFound` 404 German), then the history reads.
- **SPA client** (`web/src/auth/tools.ts`): `HISTORY_PERMISSION = 'inspection.history.view'` const (mirror `REINSTATE_PERMISSION` L25-29) + `listToolHistory(toolId)` client + `ToolHistory`/`ToolInspectionHistory`/`ToolReinstatementHistory`/`ToolHistoryItem` types (share `InspectionResult`/`InspectionMode`).
- **Tests:** backend core (ordering, permission 403, missing-user → "Deleted User", archived/unknown tool → 404, empty history), postgres (both list queries order + items join), http (DTO shape, 403 gate, 404), main composition mount; SPA (details page: header from state + deep-link refetch, history render reverse-chrono with German outcome/notes/inspector, reinstatements, no-permission note, empty, 401, 403 inline).

**Ask First:**
- None.

**Never:**
- No reinstatement ACTION on the details page (user decision — history display only; the dashboard keeps the "Wiederherstellen" action).
- No dashboard row layout / row-click navigation changes (deferred to a follow-up story — the details page is reachable by URL only in this story).
- No backend write path changes — the endpoint is read-only.
- No change to status derivation, the dashboard list contract, or the inspection submit/reinstate flows.
- No SQL JOIN across module boundaries (inspector/actor names resolve through the user seam, never in queries.sql of the tools module).
- No stored status/derivation changes (AD-4).
- No PDF export (6.2).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| HIST_OK | holder, tool with inspections + reinstatements | 200, inspections newest-first (each: inspector name, timestamp, outcome, notes, mode, items), reinstatements newest-first (actor, reason) | n/a |
| HIST_EMPTY | tool with no records | 200 with empty arrays | n/a |
| HIST_GATED | caller lacks `inspection.history.view` | 403 uniform envelope, no data | 403 |
| HIST_UNKNOWN / HIST_ARCHIVED | unknown or archived tool id | 404 German | 404 |
| HIST_DELETED_USER | inspector/actor id has no user row | that name renders "Deleted User" | n/a |
| HIST_401 | expired/revoked session | clearAuthState + /login | 401 |
| SPA_DEEPLINK | direct visit without router state | header via listDashboardTools refetch | n/a |
| SPA_NOHISTORY | user lacks `inspection.history.view` | details header renders, history section shows German no-permission note (no fetch) | n/a |

</frozen-after-approval>

## Code Map

- `web/src/App.tsx` (428 lines) -- add `/tools/:toolId` route after `/inspection/:toolId` (L257-264), wrapped in `AuthenticatedPage`.
- `web/src/pages/ToolDetailsPage.tsx` (+css) NEW -- header dl (mirror InspectionPage L411-430) + history list + reinstatement list; German microcopy; skeleton + inline error + no-permission note; 401→login. Tool id from `useParams()`. Deep-link header via `listDashboardTools()` find-by-id.
- `web/src/auth/tools.ts` (447 lines) -- `HISTORY_PERMISSION` const (after L29), `ToolHistory`/`ToolInspectionHistory`/`ToolReinstatementHistory`/`ToolHistoryItem` types, `listToolHistory(toolId)` client `GET ${DASHBOARD_TOOLS_URL}/{id}/history`.
- `internal/tools/adapters/http/tools.go` -- `HistoryRoutes()` (mirror L184-208) + `ListInspectionHistory` handler (user := auth.UserFrom(ctx), 401 guard, `id := chi.URLParam`), history DTOs (reuse `inspectionItemDTO` L73-79), `mapInspectionError` covers 403/404/400.
- `internal/tools/core/inspections.go` -- `const InspectionHistoryViewPermission = "inspection.history.view"` (next to L32/L38); `InspectionStore` (L305-313) += `ListInspectionsByTool` + `ListReinstatementsByTool` (+ items read); `Service.ListInspectionHistory(ctx, actorID, toolID)` resolves permission + tool, reads both lists, resolves names once, maps "Deleted User".
- `internal/tools/core/tools.go` or `tool_types.go` -- `DisplayNameResolver` narrow seam (precedent `PermissionResolver` tools.go L185-187) + `NewService` param (tool_types.go L217) threaded through main.go L145-148 (pass `userRepo`).
- `internal/tools/adapters/postgres/queries.sql` (inspection section L218-289) -- `ListInspectionsByTool :many` (submitted_at DESC, id DESC), `ListReinstatementsByTool :many` (created_at DESC, id DESC), `ListInspectionItemsByTool :many` (JOIN inspections by tool_id, ORDER BY inspection_id, position); `just sqlc-generate`.
- `internal/user/adapters/postgres/queries.sql` + `repository.go` -- `ListUsersByIDs :many` (`SELECT id, display_name FROM users WHERE id = ANY($1::uuid[])`) + `Repository.ResolveDisplayNames` returning the map (absent ids → missing key → "Deleted User").
- `cmd/server/main.go` -- mount `historySurface := auth.RequirePermission(sessionManager, userRepo, toolscore.InspectionHistoryViewPermission)(toolHandler.HistoryRoutes())` and `toolsSurface.Mount("/{id}/history", historySurface)` (after L207).
- `migrations/000028_schirrmeister_inspection_history.up/down.sql` NEW -- grant `inspection.history.view` to the `schirrmeister` base role (the architecture spine AD-12 matrix, epic 6 and this spec's frozen intent all name Schirrmeister as a history viewer; migration 000010 seeded it only for admin + fuehrende). Up: `INSERT INTO permission_group_permissions ... ON CONFLICT DO NOTHING` (mirror 000016). Down: `DELETE` only the added schirrmeister grant row.
- Tests -- `internal/tools/core/inspections_test.go`, `internal/tools/adapters/postgres/inspections_test.go`, `internal/tools/adapters/http/tools_test.go`, `cmd/server/main_test.go`; NEW `web/src/pages/ToolDetailsPage.test.tsx`; seed/role tests asserting schirrmeister's permission set are updated to include `inspection.history.view` where they pin it.

## Tasks & Acceptance

**Execution:**
- [x] Backend history endpoint (queries + core + display-name seam + HTTP + mount + gate) -- backend
- [x] Migration 000028 -- grant schirrmeister `inspection.history.view` (align the live seed with AD-12 + the intent) -- backend
- [x] SPA details page `/tools/:toolId` (header + history + reinstatements + gating) -- SPA
- [x] Tests -- backend (history/order/403/404/Deleted User/empty) + seed grant + SPA (details header/history/no-permission/401) -- verification

**Acceptance Criteria:**
- Given a `inspection.history.view` holder opens `/tools/{toolId}`, when the history loads, then inspections are listed newest-first, each naming the inspector, timestamp, outcome, notes, mode and per-checklist-item results, and reinstatements newest-first with actor + reason (FR-18).
- Given a non-holder opens the details page, when it renders, then the header shows but the history section shows a German no-permission note and no data (AD-6: server 403 is the gate).
- Given an inspection whose inspector account no longer exists, when the history renders, then the name reads "Deleted User" (Story 3.4 forward-compatible).
- Given the details page is visited directly (deep link), when it loads, then the header is resolved from the dashboard list and the history renders.

## Spec Change Log

- **bad_spec (implementer review, 2026-09-16):** the frozen intent names Schirrmeister as a history viewer, but migration 000010 seeds `inspection.history.view` only for admin + fuehrende — so a Schirrmeister user would 403 on the new endpoint. The spec's Tasks omitted the seed-grant migration. Amended: added migration 000028 (grant schirrmeister the code, mirroring 000016's idempotent grant pattern + a scoped down migration) + updated the seed/role tests. KEEP: the endpoint/gate/display-name seam design and the SPA gating all stand as implemented — the amendment only adds the missing seed grant.

## Design Notes

- **Server-authoritative gating + client courtesy:** the details page is open to all; the history fetch is skipped client-side when `inspection.history.view` is absent (no doomed 403 round-trip), but the server gate is the contract — the response envelope is still 403-gated.
- **One bulk name resolution:** all inspector/actor ids across inspections + reinstatements are resolved in ONE `ResolveDisplayNames` call (no N+1 user reads; the Tool module never sees user tables).
- **Deleted-account forward-compat:** because `inspector_id`/`actor_id` are FK-less plain uuids (AD-8/3.4), a missing user row maps cleanly to "Deleted User" with no schema change.
- **Reverse-chrono tiebreak:** `submitted_at DESC, id DESC` / `created_at DESC, id DESC` — deterministic ordering for equal timestamps (matches the status-read queries' tiebreak).

## Verification

**Commands:**
- `just build && just vet && just test -p 1 && just lint` -- expected: all Go/web tests pass, 0 lint issues
- `npx vitest run` in web/ -- expected: all pass incl. the ToolDetailsPage cases
- `npm --prefix web run build` -- expected: SPA builds

**Manual checks (if no CLI):**
- Open `/tools/<toolId>` as Fuehrung/Admin: history newest-first with inspector/outcome/notes/items + reinstatements; as Helfer*in: header + no-permission note; a deleted-account inspector renders "Deleted User".

## Suggested Review Order

**History endpoint (entry point)**

- The gated read that resolves permission + tool-exists (404) then returns the newest-first history with resolved names.
  [`inspections.go:658`](../../internal/tools/core/inspections.go#L658)

- The new permission code and the "Deleted User" literal the seam falls back to.
  [`inspections.go:46`](../../internal/tools/core/inspections.go#L46)

- The ToolHistory payload types (inspections + reinstatements with names).
  [`inspections.go:527`](../../internal/tools/core/inspections.go#L527)

**Display-name seam**

- The narrow read-only seam the Tool module consumes (never joins user tables).
  [`tool_types.go:198`](../../internal/tools/core/tool_types.go#L198)

- The user-side implementation via one `ListUsersByIDs` query.
  [`repository.go:83`](../../internal/user/adapters/postgres/repository.go#L83)

**HTTP + mount**

- The gated history router.
  [`tools.go:254`](../../internal/tools/adapters/http/tools.go#L254)

- The `inspection.history.view` gate + `/{id}/history` mount in the composition root.
  [`main.go:201`](../../cmd/server/main.go#L201)

**Seed grant**

- Migration 000028 grants schirrmeister `inspection.history.view` (was admin+fuehrende only).
  [`000028_schirrmeister_inspection_history.up.sql:1`](../../migrations/000028_schirrmeister_inspection_history.up.sql#L1)

**SPA details page**

- The details page header (state or dashboard-list refetch) + gated history/reinstatement rendering.
  [`ToolDetailsPage.tsx:65`](../../web/src/pages/ToolDetailsPage.tsx#L65)

- The client (history fetch + permission const + malformed-body guard).
  [`tools.ts:524`](../../web/src/auth/tools.ts#L524)

- The `/tools/:toolId` route.
  [`App.tsx:265`](../../web/src/App.tsx#L265)

**Tests**

- History ordering, permission 403, Deleted User, 404, empty.
  [`inspections_test.go:658`](../../internal/tools/core/inspections_test.go#L658)

- The equal-timestamp `id DESC` tiebreak.
  [`inspections_test.go:299`](../../internal/tools/adapters/postgres/inspections_test.go#L299)

- App-level route + unauthenticated redirect.
  [`App.test.tsx:830`](../../web/src/App.test.tsx#L830)

- Tool-switch stale-flash regression + Deleted-User render.
  [`ToolDetailsPage.test.tsx:428`](../../web/src/pages/ToolDetailsPage.test.tsx#L428)