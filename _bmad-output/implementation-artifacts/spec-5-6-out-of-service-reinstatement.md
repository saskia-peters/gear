---
title: 'Out of Service Lifecycle — Not-Inspectable + Reinstatement (FR-14/FR-15/AD-4/AD-9)'
type: 'feature'
created: '2026-09-14'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'a40ab496deff13a74760e876ed59c1c341486340'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-5-context.md'
  - '{project-root}/_bmad-output/implementation-artifacts/spec-5-3-out-of-service-flagging.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** An OOS tool is currently still inspectable — anyone can start and submit a pass inspection on it, which (correctly) does NOT clear OOS, so tools get stuck "Außer Betrieb" with no way out (no reinstatement surface).

**Approach (user decision, 2026-09-14):** complete the OOS lifecycle in one story — (a) an OOS tool is NOT available for inspection (start AND submit reject it, German 403; the dashboard start button is disabled), and (b) Story 5.6: a Fuehrung/Admin **reinstates** an OOS tool with a mandatory reason, which resets the clock (next_due = reinstatement + interval) and immediately derives the new status.

## Boundaries & Constraints

**Always:**
- **OOS block — core** (`internal/tools/core/inspections.go`): a helper `toolIsOutOfService(ctx, toolID) (bool, error)` using `GetToolInspectionStatus` (`inspections.go:253`): OOS iff `LatestFailAt != nil && (LastReinstatedAt == nil || !LatestFailAt.Before(*LastReinstatedAt))` (no schedule resolution needed). `StartInspection` (`:82`) and `SubmitInspection` (`:274`): after loading the tool (archived/unknown → `ErrToolNotFound`), if OOS → `ErrToolOutOfService` (403), BEFORE the qualification gate. New sentinel `ErrToolOutOfService` + German `MsgToolOutOfService` ("Das Gerät ist außer Betrieb und kann nicht geprüft werden.") mapped to 403 in `mapInspectionError` (`http/tools.go:454`).
- **Reinstatement — core** (`internal/tools/core/inspections.go`): `ToolReinstatePermission = "tool.reinstate"`; `ReinstateTool(ctx, actorID, toolID, reason) (*ReinstateResult, error)` re-checks `tool.reinstate` (`requireToolsPermission`, `tools.go:236`), loads the tool (missing/archived → `ErrToolNotFound`), validates the reason (trimmed non-empty, ≤ 2000 runes → `ErrInspectionInvalid` 400 with German `MsgReinstatementReasonRequired` / `MsgReinstatementReasonTooLong`), persists via `InsertReinstatement`, audits `tool.reinstate` (`auditTool`), and returns the new derived status (`GetToolInspectionStatus` + `resolveToolScheduleInterval` + `deriveToolStatus`) — reinstatement resets the clock (the derivation already yields not-OOS for a fail before the latest reinstatement; `next_due = lastReinstatedAt + interval`).
- **Reinstatement — store** (`internal/tools/adapters/postgres/`): `InspectionStore` gains `InsertReinstatement(ctx, toolID, actorID, reason)` — one insert into the existing `reinstatements` table (migration 000027), `created_at` = now; sqlc query + repo method (transaction-free single row).
- **Reinstatement — HTTP** (`internal/tools/adapters/http/tools.go`): `POST /api/v1/tools/{id}/reinstatement` behind `tool.reinstate` (its OWN gate, `auth.RequirePermission(sessionManager, userRepo, toolscore.ToolReinstatePermission)`), mounted at `/{id}/reinstatement` in the combined toolsSurface router (`cmd/server/main.go:193-197`, alongside the inspection mount). Body `{reason}` (reject unknown fields + trailing JSON like the submit handler). Response `{ status: {status, next_due}, message }` with German `MsgToolReinstated` ("Das Gerät wurde wiederhergestellt.").
- **SPA** (`web/src/auth/tools.ts` + `web/src/pages/DashboardPage.tsx`): `reinstateTool(toolId, reason)` client + `REINSTATE_PERMISSION = 'tool.reinstate'`. On an OOS row: the "Prüfung starten" button is DISABLED (OOS not inspectable — no start click); a "Wiederherstellen" button renders ONLY for `hasPermission(REINSTATE_PERMISSION)` holders. Clicking opens the existing `PromptDialog` (`web/src/components/PromptDialog.tsx`) asking for the mandatory reason → POST → on success refetch the dashboard list (statuses/colors/counts update) + a confirmation; on error show the German message inline. Non-holders never see the button.
- **Tests:** backend core (start/submit OOS → 403; ReinstateTool permission/validation/persist/derived-status/audit), http (reinstate 200/400/403/404/401; start/submit OOS 403), postgres (InsertReinstatement round-trip + fail-before-reinstatement not-OOS), composition (reinstate mount gate), SPA (OOS row start disabled, reinstate button visible to holders / hidden otherwise, PromptDialog reason → POST body, post-reinstate refresh).

**Ask First:**
- None.

**Never:**
- No auto-clear of OOS by a passing inspection (unchanged — reinstatement is the sole exit, FR-15).
- No change to the derived-status semantics, the schedule catalog, or the orange-window.
- No status column stored anywhere.
- No "Deleted User" inspector rewriting (Story 3.4, deferred) — `actor_id` stays a plain uuid.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| START_OOS | start on an OOS tool | 403 German, no inspection started | 403 |
| SUBMIT_OOS | submit on an OOS tool | 403 German, nothing persisted | 403 |
| REINSTATE_OK | Fuehrung/Admin + valid reason | 200, status not-oos, next_due = reinstatement + interval, audited | n/a |
| REINSTATE_EMPTY | empty/whitespace reason | 400 German | 400 |
| REINSTATE_LONG | reason > 2000 runes | 400 German | 400 |
| REINSTATE_GATED | caller without tool.reinstate | 403 uniform, no write | 403 |
| REINSTATE_ARCHIVED | archived tool | 404 German | 404 |
| REINSTATE_UNKNOWN | nonexistent tool id | 404 German | 404 |
| SPA_START_DISABLED | OOS row renders | "Prüfung starten" disabled; row shows "Außer Betrieb" | n/a |
| SPA_REINSTATE_HOLDER | caller holds tool.reinstate | "Wiederherstellen" button on the OOS row | n/a |
| SPA_REINSTATE_NONHOLDER | caller lacks it | no button (hidden, not the gate) | n/a |
| SPA_DIALOG | reinstate clicked | PromptDialog asks for the reason; POST carries it | inline German on 400 |
| SPA_REFRESH | reinstate succeeds | list refetches; the tool leaves OOS | n/a |

</frozen-after-approval>

## Code Map

- `internal/tools/core/inspections.go` -- `toolIsOutOfService` helper, `ErrToolOutOfService`/`MsgToolOutOfService`, OOS checks in `StartInspection` (`:82`) + `SubmitInspection` (`:274`); `ReinstateTool` + `ToolReinstatePermission` + `ReinstateResult`; reason validation sentinels (`MsgReinstatementReasonRequired`/`TooLong`).
- `internal/tools/core/inspections.go` `InspectionStore` (`:249`) + `internal/tools/adapters/postgres/inspections_repo.go` + `queries.sql` -- `InsertReinstatement` (sqlc `:one`/`:exec`, `just sqlc-generate`).
- `internal/tools/ports/ports.go` -- `ReinstateTool` on the `Service` interface (`:26-81`).
- `internal/tools/adapters/http/tools.go` -- `mapInspectionError` (`:454`) maps `ErrToolOutOfService`→403; `ReinstateRoutes()` + `ReinstateTool` handler + DTO; decoder hardening (unknown fields + trailing JSON, the submit pattern `:237-262`).
- `cmd/server/main.go` (`:193-197`) -- mount `/{id}/reinstatement` behind `tool.reinstate` in the toolsSurface router (its OWN gate, `auth.RequirePermission`).
- `web/src/auth/tools.ts` -- `reinstateTool(toolId, reason)` client + `REINSTATE_PERMISSION` + `ReinstateResult` type.
- `web/src/pages/DashboardPage.tsx` (+css) -- OOS row: disable "Prüfung starten"; "Wiederherstellen" button for `hasPermission` holders; `PromptDialog` reason flow; refetch on success; inline error.
- `web/src/components/PromptDialog.tsx` -- reuse (no change).
- Tests: `inspections_test.go` (OOS block + ReinstateTool), `tools_test.go` http (start/submit OOS 403, reinstate 200/400/403/404/401), `inspections_test.go` postgres (InsertReinstatement + derivation), `cmd/server/main_test.go` (reinstate mount gate), `DashboardPage.test.tsx` (start disabled, reinstate visibility + dialog + refresh).

## Tasks & Acceptance

**Execution:**
- [x] `internal/tools/core/inspections.go` -- OOS block (start/submit) + `ReinstateTool` + sentinels -- core
- [x] `internal/tools/adapters/postgres/` (queries + repo) -- `InsertReinstatement` -- persistence
- [x] `internal/tools/ports/ports.go` -- `ReinstateTool` on `Service` -- ports
- [x] `internal/tools/adapters/http/tools.go` + `cmd/server/main.go` -- reinstate endpoint + mount gate + OOS 403 mapping -- API
- [x] `web/src/auth/tools.ts` + `web/src/pages/DashboardPage.tsx` (+css) -- reinstate client, OOS-row start disabled, "Wiederherstellen" + PromptDialog + refresh -- SPA
- [x] Tests -- core/http/postgres/composition/SPA -- verification

**Acceptance Criteria:**
- Given a tool is OOS, when a start or submit is attempted, then it answers a German 403 and nothing is started/persisted (FR-14/AD-4).
- Given a Fuehrung/Admin holder, when they reinstate an OOS tool with a non-empty reason, then the reinstatement persists (actor, timestamp, reason), the tool is no longer OOS, and next_due = reinstatement + resolved schedule interval (FR-15/AD-9/AD-5).
- Given a non-holder or an empty reason, when a reinstatement is attempted, then it answers 403 / 400 German and nothing is persisted (AD-6).
- Given an OOS tool on the dashboard, when a Fuehrung/Admin holder views it, then "Prüfung starten" is disabled and "Wiederherstellen" is available; after reinstating, the list refreshes and the tool leaves OOS (UX-DR6).

## Spec Change Log

- **Review patches (review 1, 2026-09-14):** reinstatement now REQUIRES the tool to be OOS (`MsgToolNotOutOfService` 400 — reinstating a serviceable tool is rejected, which also makes duplicate reinstatements answer 400); the post-commit status derivation is best-effort (a committed reinstatement is never reported as a 500, so a retry can't duplicate it); the `tool.reinstate` gate/audit constants are consolidated; the SPA uses only the server's confirmation message, disables the row's "Wiederherstellen" while busy, restores the unmount cancel guard, clears the stale confirmation on the next action, captures the dialog target at open time, and the fake store models the write→read flip so the reinstate response is proven to reflect the persisted row. Tests added for all of the above incl. the generic-500 envelope and client-abort paths.

## Design Notes

- **Cheap OOS check:** the not-inspectable gate needs only `LatestFailAt` vs `LastReinstatedAt` (one `GetToolInspectionStatus` read) — no schedule resolution, unlike the full status.
- **Clock reset is free:** the derivation already keys OOS on `LatestFailAt` not-since-`LastReinstatedAt`; writing a reinstatement row immediately flips the derivation (not-OOS, `next_due = reinstatement + interval`). No derivation change needed.
- **PromptDialog reuse:** the mandatory-reason prompt reuses the existing single-input modal (built for the SMTP test-email receiver); no new modal.
- **Copy:** the earlier "wird als Außer Betrieb gesperrt" on a pass submit is now unreachable (submits on OOS tools are rejected), so no wording change is needed there.

## Verification

**Commands:**
- `just build` && `just vet` && `just test -p 1` && `just lint` -- expected: all Go/web tests pass, 0 lint issues
- `curl` as Fuehrung/Admin: POST /api/v1/tools/{id}/reinstatement with a reason -- expected: 200 + non-oos status; as non-holder -- 403; empty reason -- 400
- `curl` as any holder: POST .../inspection/start on an OOS tool -- expected: 403 German
- `npx vitest run` in web/ -- expected: all pass incl. the OOS-row/reinstatement cases

**Manual checks (if no CLI):**
- An OOS tool's row shows "Außer Betrieb" with "Prüfung starten" disabled; a Fuehrung/Admin sees "Wiederherstellen", enters a reason, and the tool returns to service with the clock reset.

## Suggested Review Order

**Backend OOS gate + reinstatement**

- Entry point: `ReinstateTool` — OOS precondition, mandatory reason, best-effort post-commit status, audit.
  [`inspections.go:480`](../../internal/tools/core/inspections.go#L480)

- The cheap OOS check (LatestFailAt vs LastReinstatedAt — no schedule needed) gating start AND submit.
  [`inspections.go:451`](../../internal/tools/core/inspections.go#L451)

- `StartInspection` applies the OOS block before the qualification gate.
  [`inspections.go:106`](../../internal/tools/core/inspections.go#L106)

**Store**

- `InsertReinstatement` — one row into the existing table; `GetToolInspectionStatus` already reads the clock anchor.
  [`inspections_repo.go:146`](../../internal/tools/adapters/postgres/inspections_repo.go#L146)

**HTTP**

- The `tool.reinstate`-gated route + the reinstatement handler (hardened decoder).
  [`tools.go:202`](../../internal/tools/adapters/http/tools.go#L202)

- The reinstatement error mapper (OOS 403, not-OOS/validation 400, uniform 500).
  [`tools.go:576`](../../internal/tools/adapters/http/tools.go#L576)

- Mounted at `/{id}/reinstatement` with its OWN gate beside the inspection surface.
  [`main.go:186`](../../cmd/server/main.go#L186)

**SPA**

- `handleReinstate` — PromptDialog reason → POST → refetch + server confirmation.
  [`DashboardPage.tsx:251`](../../web/src/pages/DashboardPage.tsx#L251)

- The OOS-row UX: start disabled, "Wiederherstellen" for `tool.reinstate` holders only, dialog target captured at open.
  [`DashboardPage.tsx:86`](../../web/src/pages/DashboardPage.tsx#L86)

**Tests**

- Core: OOS block, tie boundary, reinstated-not-blocked, reinstate OK/precondition/validation/gated/duplicate.
  [`inspections_test.go:671`](../../internal/tools/core/inspections_test.go#L671)

- SPA: start disabled, holder/non-holder visibility, dialog + trimmed reason, busy, refresh, captured target.
  [`DashboardPage.test.tsx:555`](../../web/src/pages/DashboardPage.test.tsx#L555)