---
title: 'GEAR-Module Tool List (dashboard.view)'
type: 'feature'
created: '2026-09-09'
status: 'done'
review_loop_iteration: 0
baseline_commit: '653e257339e816a326c5e0a9592251554af48a78'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-4-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** A tool created in the Admin module is invisible to the GEAR module (non-admin) — the dashboard "Werkzeugliste" renders a static EmptyState and there is no non-admin tool-list endpoint, so regular users cannot see the tools that exist.

**Approach:** Add a minimal `dashboard.view`-gated read-only tool-list surface (GET /api/v1/tools) returning the ACTIVE tools (name, tool type name) and render that list on the DashboardPage instead of the EmptyState. Until the inspection data model exists (Epic 5), every listed tool is shown as "available" (green); the color-coded status dashboard stays Story 6.1 (Epic 6). No writes, no status derivation, no counts.

## Boundaries & Constraints

**Always:**
- **Endpoint:** `GET /api/v1/tools` — mounted behind `auth.RequirePermission(sessionManager, userRepo, "dashboard.view")` (all base roles hold it), mirroring the existing `WithProtected` pattern. Returns `200 []` (empty) or the active tools. No tool data exposed without `dashboard.view` (403 uniform envelope, AD-6).
- **Read path reuses the existing Tool store:** a new ungated core method `ListToolsForDashboard` (or similar) calls `s.store.ListTools(ctx)` WITHOUT the `tools.manage` permission re-check — the HTTP surface carries the `dashboard.view` gate instead. No new query needed (ListTools already JOINs `tool_types` for the display name and filters `archived_at IS NULL`).
- **DTO:** `{ id, name, tool_type_id, tool_type_name }` — minimal, no schedule/attributes/audit data on this surface (keep it small; the admin surface already exposes the full DTO).
- **SPA DashboardPage:** the "Werkzeugliste" section (`DashboardPage.tsx`) replaces the `EmptyState` with a real list fetched from `/api/v1/tools` (new `web/src/auth/tools.ts` `listDashboardTools` client or a dedicated dashboard client). Each row: tool name + type name, marked "verfügbar" (available, green accent). When empty → keep the existing EmptyState ("Keine Werkzeuge vorhanden"). Loading skeleton, inline German error, 401→login, 403→redirect (dashboard.view should never 403 for a logged-in user, but handle it defensively).
- **No status derivation (AD-4/AD-5):** this story does NOT compute Red/Orange/Green or due dates — that's Story 6.1 (Epic 6) once the inspection clock + inspection data exist. The minimal list shows "verfügbar" as a static label for every tool.
- **Tests:** Go — handler test for GET /api/v1/tools (200 list, 200 empty, 401, 403 for a caller without dashboard.view), core test that the dashboard read path does NOT require tools.manage, composition mount test (dashboard.view holder reaches it; a tools.manage-but-not-dashboard.view scenario 403s). Web — DashboardPage renders the list from the stub, shows EmptyState when empty, handles 401/403.

**Ask First:**
- None (the user chose "build minimal list now"; the scope is deliberately minimal and the color-coded dashboard stays in Epic 6).

**Never:**
- No writes, no create/update/archive on this surface (admin-only, Story 4.3).
- No status/due-date/color computation (Story 6.1 / Epic 5 dependency).
- No changes to the admin `/api/v1/admin/tools` surface, its DTO, or its `tools.manage` gate.
- No inventory number here (deferred follow-up story).
- No new DB queries beyond reusing `ListTools`/the store.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| GET_LIST | active tools exist | 200 with each tool's id/name/type name | n/a |
| GET_EMPTY | no tools | 200 `[]` → SPA keeps the EmptyState | n/a |
| GET_UNAUTHENTICATED | no session | 401 uniform envelope | 401 |
| GET_FORBIDDEN | lacks dashboard.view | 403 uniform envelope, no tool data (AD-6) | 403 |

</frozen-after-approval>

## Code Map

- `internal/tools/core/tools.go` -- add an ungated `ListToolsForDashboard(ctx)` (no `requireToolsPermission`); the store `ListTools` already returns active tools + type name.
- `internal/tools/adapters/http/tools.go` -- add a `DashboardToolsRoutes()` (or a handler on the tools Handler) serving `GET /` with a minimal DTO; mounted at `/api/v1/tools` behind `RequirePermission(..., "dashboard.view")`.
- `cmd/server/main.go` -- mount `/api/v1/tools` behind the `dashboard.view` gate (reuse the tools handler service).
- `web/src/auth/tools.ts` -- `listDashboardTools()` client (`GET /api/v1/tools`).
- `web/src/pages/DashboardPage.tsx` (+css) -- render the tool list (name + type + "verfügbar"), EmptyState when empty, skeleton + error handling.
- Tests: Go (handler 200/empty/401/403, core ungated-read, composition mount), web (list render, empty state, 401/403).

## Tasks & Acceptance

**Execution:**
- [x] `internal/tools/core/tools.go` -- ungated `ListToolsForDashboard` -- read path
- [x] `internal/tools/adapters/http/tools.go` -- `DashboardToolsRoutes()` + minimal DTO -- API
- [x] `cmd/server/main.go` -- mount `/api/v1/tools` behind `dashboard.view` -- composition root
- [x] `web/src/auth/tools.ts` -- `listDashboardTools` client -- SPA
- [x] `web/src/pages/DashboardPage.tsx` (+css) -- real tool list + empty/error states -- SPA
- [x] Tests -- Go + web incl. I/O matrix rows -- verification

**Acceptance Criteria:**
- Given an authenticated user with `dashboard.view`, when they open the dashboard, then the "Werkzeugliste" lists the ACTIVE tools (name + type, marked available) instead of the empty state; with no tools it still shows "Keine Werkzeuge vorhanden".
- Given a caller without `dashboard.view` (or unauthenticated), then GET /api/v1/tools answers the uniform 403/401 with no tool data exposed (AD-6).
- Given a tool archived or created in the Admin module, then the dashboard list reflects it (active tools appear; archived tools never appear).

## Spec Change Log

_(empty until first review loopback)_

## Design Notes

- **Why a separate ungated core method:** `ListTools` re-checks `tools.manage` in the core (defense-in-depth for the admin surface). The dashboard read must be reachable by any `dashboard.view` holder, so the HTTP gate carries the permission and the core method stays ungated — mirroring how `SchedulesPort`/`QualificationCatalogPort` expose ungated reads for cross-module consumption.
- **"verfügbar" is a static label, not derived status:** real Red/Orange/Green derivation (AD-4/AD-5) is Story 6.1 once inspection data exists. This minimal surface satisfies "the created tool is visible to the GEAR module" today without preempting the status dashboard.

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go/web tests pass, 0 lint issues
- `curl` as a user with dashboard.view: GET /api/v1/tools -- expected: 200 with the active tools (id/name/type name)
- `curl` without a session: GET /api/v1/tools -- expected: 401; caller lacking dashboard.view -- 403 uniform envelope
- `npx vitest run` in web/ -- expected: all pass incl. DashboardPage list tests

**Manual checks (if no CLI):**
- Open the dashboard as a regular (non-admin) user: the created tool appears in the Werkzeugliste marked "verfügbar"; with no tools the EmptyState shows.