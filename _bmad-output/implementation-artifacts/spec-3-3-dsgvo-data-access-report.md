---
title: 'DSGVO Data-Access Report (FR-24/AD-8/NFR-O2)'
type: 'feature'
created: '2026-09-17'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'faa2673a2aa5050ae6199dc1213afb0f93da3bad'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-3-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The DSGVO admin surface is a placeholder — the organization cannot fulfill a user's right of access and show exactly what data is held (FR-24).

**Approach:** A real DSGVO surface (`/admin/dsgvo`) with a "Datenauskunft" report: an admin (`dsgvo.access_report`) picks a user and generates a data-access report that orchestrates each owning module's export through a new composition-root DSGVO orchestrator (AD-8) — profile + auth history + qualifications + group memberships + inspection records — assembled as structured JSON for review/download, with an immutable audit entry (NFR-O2).

## Boundaries & Constraints

**Always:**
- **DSGVO orchestrator** (`internal/dsgvo/core` NEW — a composition-root wiring point, AD-8): `Service` with `GenerateAccessReport(ctx, actorID, targetUserID)` returning a `*AccessReport`. It consumes narrow read ports: a USER export port (profile/roles/groups/grants/qualifications/sessions/login-attempts) and a TOOL export port (per-user inspection records). NO module writes another's SQL — the orchestrator calls each module's exported port. Wired in `cmd/server/main.go` with the existing `userService`/`userRepo` + a new tool-side port.
- **User export port** (`internal/user`): a new read-only `DSGVOExportPort` (in `user/ports/ports.go`), implemented by the user core `Service`/repo, exposing ONE `ExportUserData(ctx, userID) (*core.UserDataExport, error)`. It aggregates: the full profile (`GetUserByEmail`-style full row incl. attributes + created_at/updated_at, with secrets excluded), roles, user-group memberships, direct grants, qualification assignments (all from the existing `GetUserDetail` composition), the user's SESSIONS (new query `ListSessionsByUser :many` — table `sessions` has `user_id_idx`), and LOGIN-ATTEMPT state (`GetLoginAttempts` by email). A missing user → `ErrAdminUserNotFound`.
- **Tool export port** (`internal/tools`): a new read-only `DSGVOInspectionExportPort` (in `tools/ports/ports.go`), implemented by the tool core `Service`/repo: `ExportUserInspectionData(ctx, userID) (*core.UserInspectionDataExport, error)` returning the user's inspections (new query `ListInspectionsByInspector :many` — `WHERE inspector_id=$1 ORDER BY submitted_at DESC, id DESC`, backed by a NEW index `inspections_inspector_id_idx` in a small migration) + reinstatements (`ListReinstatementsByActor :many`, NEW index `reinstatements_actor_id_idx`) with per-item results (reuse `ListInspectionItemsByTool` per inspection — N+1 accepted at report scale) and the TOOL-OWNED summary (counts, per-tool grouping). Actor names are NOT resolved (they are the report subject's own).
- **Admin HTTP surface** `internal/admin/adapters/http/dsgvo.go` NEW: `DsgvoRoutes()` mounted at `/api/v1/admin/dsgvo` behind `auth.RequireAnyPermission(..., []string{"dsgvo.access_report", "dsgvo.delete"}, "dsgvo access denied", ...)` in main.go (one permission per surface, AD-6). Endpoints: `GET /reports/{userId}` → 200 with the `AccessReport` JSON (reviewable) + `Content-Disposition: attachment`-style `application/json` download; 403 for non-holders, 404 unknown user, 400/500 mapped to the uniform German envelope. Audits `dsgvo.access_report` (actor, timestamp, operation type, target user id) via the `AuditWriter` seam (NFR-O2).
- **SPA** (`web/src/pages/admin/AdminDsgvoPage.tsx` replace the placeholder): a tab bar "Datenauskunft" / "Konto löschen" gated per code (`dsgvo.access_report` / `dsgvo.delete`). The report tab: user picker (reuse `listUsers`), a "Bericht erstellen" action, then the assembled report rendered read-only (profile, roles/groups/grants/qualifications, auth history incl. sessions + login-attempt state, inspection summary + per-inspection rows) with a "Herunterladen (JSON)" action; inline German feedback, skeleton, 401→login, 403→leave module. The delete tab is Story 3.4's surface — render a placeholder "Konto löschen (Story 3.4)" note behind the delete tab here.
- **Tests:** orchestrator (aggregation, missing user, 401/403), user export port (profile+roles+groups+quals+sessions+login-attempts), tool export port (inspections/reinstatements by inspector + items + counts), http (200 report, 403 gate, 404, 400, audit row), main composition mount, SPA (tab gating, report render, download, 401/403, empty).

**Ask First:**
- None.

**Never:**
- No personal data on the wire without `dsgvo.access_report` (AD-6); no secrets (password hash, TOTP, OTP) in the report.
- No writes from the report path (it is read-only export); the deletion (3.4) is a separate story.
- No module writes another's SQL (AD-8 — the orchestrator composes ports only).
- No hard-delete or user-state mutation here.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| REPORT_OK | holder, existing user | 200 structured report: profile, roles/groups/grants/quals, sessions, login-attempt state, inspections + reinstatements with per-item results, counts | n/a |
| REPORT_NO_AUTH | user with no sessions/attempts | those sections render as empty ("Keine …") | n/a |
| REPORT_NO_INSPECTIONS | user never inspected | inspection section empty + zero counts | n/a |
| REPORT_GATED | caller lacks `dsgvo.access_report` | 403 envelope, no personal data | 403 |
| REPORT_UNKNOWN | unknown user id | 404 German | 404 |
| REPORT_SECRETS | report generated | password hash / TOTP / OTP never present in the payload | n/a |
| REPORT_401 | expired/revoked session | 401 envelope → login | 401 |
| SPA_TABS | holder opens /admin/dsgvo | tabs gated by code; report tab renders, delete tab shows the 3.4 placeholder | n/a |

</frozen-after-approval>

## Code Map

- `internal/dsgvo/core/dsgvo.go` NEW -- orchestrator `Service` + `AccessReport` type; `NewService(userPort, toolPort, perms, audit, log)` (`perms` = the User-module repository read-only seam for the defense-in-depth `dsgvo.access_report` re-check, AD-6/AD-12 — wired with `userRepo` in main.go); permission re-check; composes the two module exports; guards a `(nil, nil)` port return; audits `dsgvo.access_report`.
- `internal/user/ports/ports.go` -- `DSGVOExportPort { ExportUserData(ctx, userID) }` + core method (`internal/user/core/dsgvo.go`: `UserDataExport`/`UserExportProfile`/`UserSessionExport`/`LoginAttemptsExport`, `login_attempts` serializes ABSENT via `omitempty` when no attempts exist) aggregating profile/roles/groups/grants/quals/sessions/login-attempts; new `queries.sql` `GetUserByIDFull :one` + `ListSessionsByUser :many` (no token hash).
- `internal/tools/ports/ports.go` -- `DSGVOInspectionExportPort { ExportUserInspectionData(ctx, userID) }`; core method (`internal/tools/core/dsgvo.go`); new queries `ListInspectionsByInspector`/`ListInspectionItemsByInspector` (grouped items, one round-trip)/`ListReinstatementsByActor`/`ListToolNamesByIDs` (incl. archived tools) + `inspections_repo.go`/`tools_repo.go`; migration `000029_dsgvo_inspector_indexes.{up,down}.sql` (three CREATE INDEX incl. `sessions (user_id, created_at DESC)`, no schema change).
- `internal/admin/adapters/http/dsgvo.go` NEW -- `DsgvoRoutes()` + handlers + DTOs + `mapDsgvoError`; reuses `httpapi.WriteJSON`.
- `cmd/server/main.go` -- wire the orchestrator (userService/toolService as ports + userRepo as perms/audit), mount `dsgvoSurface := auth.RequireAnyPermission(..., []string{"dsgvo.access_report","dsgvo.delete"}, ...)(dsgvoHandler.DsgvoRoutes())` at `/api/v1/admin/dsgvo`.
- `web/src/pages/admin/AdminDsgvoPage.tsx` (+css, +test) -- replace placeholder: code-gated tabs, user picker, report render (incl. `pending_email` when present + the `generated_at` timestamp) + JSON download; null-guarded report sections; blank-page fallback; cancellation guard on the report fetch.
- `web/src/auth/users.ts` -- add the DSGVO report client `getDsgvoReport(userId)` (+ `DSGVO_URL`).
- Tests -- `internal/dsgvo/core/dsgvo_test.go`, user/tools port tests, `admin/adapters/http/dsgvo_test.go`, `cmd/server/main_test.go`, `AdminDsgvoPage.test.tsx`.

## Tasks & Acceptance

**Execution:**
- [x] Orchestrator + user/tool export ports + queries/indexes -- backend
- [x] Admin DSGVO HTTP surface + mount/gate/audit -- backend
- [x] SPA DSGVO page (tabs + report + download) -- SPA
- [x] Tests -- orchestrator/ports/http/mount/SPA -- verification

**Acceptance Criteria:**
- Given a `dsgvo.access_report` holder selects a user, when they generate the report, then the assembled report shows the profile fields, auth history (sessions + login attempts), qualifications, group memberships, and inspection records (FR-24/AD-8).
- Given the report is generated, when it is produced, then an immutable audit entry records the actor, timestamp, operation and target user (NFR-O2) and the event is structured-logged (NFR-O1).
- Given a non-holder, when they attempt access, then the server answers 403 and no personal data is exposed (AD-6).
- Given the report renders, when a section has no data, then it shows a German empty note, never an error.

## Spec Change Log

- **2026-09-18 (review iteration 1):** (a) the per-inspection item results are fetched via the grouped `ListInspectionItemsByInspector` query (one round-trip) instead of the planned per-inspection `ListInspectionItemsByTool` reuse — same report data, no N+1, mirroring the history-surface pattern (a frozen-Actually-better deviation, KEPT); (b) the SPA renders `pending_email` (a staged email change is data the org holds) and the report `generated_at` timestamp; (c) `migrations/000029` gains the composite `sessions (user_id, created_at DESC)` index for `ListSessionsByUser`; (d) the orchestrator's audit write is a pinned best-effort contract (a failed audit never fails the report; NFR-O1) and the orchestrator guards a `(nil, nil)` export-port return (500, never a `null` user/tools section). `NewService` carries the `perms` resolver seam (per the frozen Always section — wired with `userRepo` in main.go).

## Design Notes

- **Composition-root orchestration (AD-8):** the `internal/dsgvo/core` orchestrator is the single assembly point — it owns no tables, only composes the User and Tool export ports and the audit seam. Both module ports are read-only; the same orchestrator's ports extend naturally into 3.4's deletion transaction.
- **No secrets in the report:** the user export uses the full-row read but strips `PasswordHash`/`TotpSecretEncrypted`/`OneTimePasswordHash` (and the pending variants) before assembly — the report carries no authenticators.
- **Per-user inspection grouping:** the tool export groups the subject's inspections by tool (id + name) with per-inspection items and counts, so the report reads as "what this user did across the fleet".

## Verification

**Commands:**
- `go build ./... && go vet ./... && go test -count=1 -p 1 ./cmd/... ./internal/...` -- expected: all pass incl. orchestrator/ports/http/mount cases
- `npx vitest run` in web/ -- expected: all pass incl. the DSGVO page cases
- `npm --prefix web run lint && npm --prefix web run typecheck && npm --prefix web run build` -- expected: clean

**Manual checks (if no CLI):**
- As an admin with `dsgvo.access_report`: open DSGVO → Datenauskunft, pick a user, generate → the report renders profile/auth/qualification/group/inspection sections; download returns JSON; a non-holder sees a 403 and no data.

## Suggested Review Order

**Orchestrator (entry point)**

- The composition-root assembly: user + tool export ports, nil-guard, best-effort audit, permission re-check.
  [`dsgvo.go:60`](../../internal/dsgvo/core/dsgvo.go#L60)

**Module export ports**

- The user export (profile, roles/groups/grants/quals, sessions, login-attempts; secrets stripped).
  [`dsgvo.go:40`](../../internal/user/core/dsgvo.go#L40)

- The tool export (per-user inspections/reinstatements + items, per-tool summary).
  [`dsgvo.go:40`](../../internal/tools/core/dsgvo.go#L40)

**HTTP + mount**

- The DSGVO surface + uniform 404/405/403/400/500 envelope.
  [`dsgvo.go:60`](../../internal/admin/adapters/http/dsgvo.go#L60)

- The any-of gate mount.
  [`main.go:245`](../../cmd/server/main.go#L245)

- The inspector/actor/session indexes (migration 000029).
  [`000029_dsgvo_inspector_indexes.up.sql:1`](../../migrations/000029_dsgvo_inspector_indexes.up.sql#L1)

**SPA**

- The DSGVO page: code-gated tabs, user picker, report render (+ pending_email/generated_at), JSON download, cancellation guard.
  [`AdminDsgvoPage.tsx:60`](../../web/src/pages/admin/AdminDsgvoPage.tsx#L60)

**Tests**

- Failure paths: generic-error 500, nil-export 500, best-effort audit, 404/405 envelope.
  [`dsgvo_test.go:60`](../../internal/admin/adapters/http/dsgvo_test.go#L60)

- Orchestrator nil-guard + unknown-user short-circuit.
  [`dsgvo_test.go:60`](../../internal/dsgvo/core/dsgvo_test.go#L60)