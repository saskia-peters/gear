---
title: 'Qualification-Gated Inspection Start (FR-11/AD-7)'
type: 'feature'
created: '2026-09-12'
status: 'done'
review_loop_iteration: 0
baseline_commit: '5eec9c60dcc341ff7da2b013779bf31ac33f1f17'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-5-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Any volunteer can potentially start an inspection on any tool — but a tool whose type requires a specific Qualification must only be inspectable by users who actually hold it (FR-11/AD-7). No gating exists and no inspection surface exists at all.

**Approach:** Add the first Epic 5 surface — a qualification-gated inspection START. The server resolves a tool's required qualification from its tool type (intra-module) and checks the caller's granted qualifications through the User module's auth port (expiry-aware); a `POST /api/v1/tools/{id}/inspection/start` answers 200 (eligible — the SPA opens the inspection screen) or 403 with a German explanation (ineligible). The dashboard "Prüfung starten" control is enabled only for eligible users and shows "Erforderliche Qualifikation fehlt" otherwise. The eventual submit (Stories 5.4/5.5) re-validates independently (AD-6).

## Boundaries & Constraints

**Always:**
- **Endpoint:** `POST /api/v1/tools/{id}/inspection/start` — mounted behind `auth.RequirePermission(..., "inspection.submit")` (all base roles hold it, mirroring the `/api/v1/tools` dashboard mount). Behavior:
  - Eligible caller → `200` with `{ tool_id, tool_name, tool_type_id, tool_type_name, inspection_mode }` (the SPA navigates to the inspection screen; the screen itself is Stories 5.2/5.4/5.5).
  - Ineligible caller (tool's type requires a qualification the user does not hold, or holds expired) → `403` uniform envelope with the German message "Erforderliche Qualifikation fehlt." (a NEW const `MsgToolQualificationMissing`).
  - Tool not found / archived → `404`; unauthenticated → `401`; no `inspection.submit` → `403`.
- **Required-qualification resolution (intra-module):** given a tool id, resolve `tools.tool_type_id → tool_types.required_qualification_id`. Add `required_qualification_id` to the Tool read path (extend the `ListTools` JOIN to select it, or add a lean `GetToolWithTypeQualification(id)` store method; prefer the lean method to avoid coupling the list DTO). When the type has NO required qualification, every `inspection.submit` holder is eligible.
- **User-granted-qualification port (extend `QualificationCatalogPort`):** add `UserHoldsQualification(ctx, userID, qualificationID string) (bool, error)` to `internal/user/ports/ports.go` (implemented ungated on the user core `Service` + a new `Repository` method reusing the `ListUserQualifications` query). The check is **expiry-aware**: a fixed assignment past `expires_at` counts as NOT held (live resolution, AD-7/FR-22; mirror `qualification_status.go`). Empty required qualification → eligible without the port call.
- **Tool core** `internal/tools/core/inspections.go` (new): `InspectionSubmitPermission = "inspection.submit"` const, `StartInspection(ctx, actorID, toolID)` — resolve the tool + its type's required qualification, call `UserHoldsQualification` when required, return eligible/`ErrToolQualificationMissing` (403) / `ErrToolNotFound` (404); re-check `inspection.submit` defense-in-depth. German messages.
- **HTTP** `internal/tools/adapters/http`: `StartInspection` handler + route in a new `InspectionRoutes()` (or on the tools handler) mounted at `/api/v1/tools/{id}/inspection/start` behind the `inspection.submit` gate. Uniform envelope.
- **SPA:** add a "Prüfung starten" button per DashboardPage Werkzeugliste row. On click → POST the start endpoint: `200` → navigate to `/inspection/:toolId` (new stub route in `App.tsx` showing the tool + its mode); `403` → show the German reason inline and disable the control (or disable on first ineligible response); `401` → login; other → inline error. The control can stay enabled-until-click (the 403 IS the gate); on a 403 the row shows "Erforderliche Qualifikation fehlt" and the button disables for the session.
- **No inspection data model in this story:** 5.1 introduces the START gate only; the inspection record, submit, OOS, and screen content are Stories 5.2–5.5. The `/start` response carries enough for a stub screen (tool + mode).
- **Audit (NFR-O1):** a failed gate (ineligible attempt) is logged structured (never silent); an eligible start may be audited `inspection.start` (best-effort).

**Ask First:**
- None (the endpoint + port-extension design follows the investigation and the ACs).

**Never:**
- No inspection submission/record creation here (Stories 5.4/5.5).
- No status derivation / OOS / due-date logic (Stories 5.3/5.6 / AD-4/AD-5).
- No Tool module joining user tables — user qualifications resolve ONLY through the extended `QualificationCatalogPort` (AD-7/AD-11).
- No client-side trust: the 403 is server-authoritative; the SPA button is a UX affordance only.
- No changes to committed migrations (the user_qualifications schema already exists).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| START_ELIGIBLE | tool's type has no required qual, caller holds inspection.submit | 200 with tool + mode | n/a |
| START_QUALIFIED | type requires qual Q, caller holds Q (active) | 200 with tool + mode | n/a |
| START_MISSING_QUAL | type requires Q, caller lacks Q | 403 "Erforderliche Qualifikation fehlt." | 403 |
| START_EXPIRED_QUAL | type requires Q, caller's Q assignment expired | 403 (expired = not held, live resolution) | 403 |
| START_NO_QUAL_TYPE | type's required_qualification_id is empty | 200 (no port call needed) | n/a |
| START_TOOL_NOT_FOUND | unknown / archived tool id | 404 sentinel | 404 |
| START_UNAUTHENTICATED | no session | 401 uniform envelope | 401 |
| START_FORBIDDEN | caller lacks inspection.submit | 403 uniform envelope, no tool data (AD-6) | 403 |
| SPA_403 | dashboard row, ineligible click | Button shows the German reason, disables | n/a |
| SPA_200 | eligible click | Navigates to /inspection/:toolId | n/a |

</frozen-after-approval>

## Code Map

- `internal/user/ports/ports.go` -- extend `QualificationCatalogPort` with `UserHoldsQualification(ctx, userID, qualificationID)`.
- `internal/user/core/qualifications.go` -- implement `UserHoldsQualification` ungated on the Service (reuse `ListUserQualifications` / qualification-status expiry logic).
- `internal/user/adapters/postgres/repository.go` (+queries if needed) -- the per-user granted-qualification read backing the port method.
- `internal/tools/core/inspections.go` (new) + `core.go` -- `InspectionSubmitPermission`, `StartInspection` (resolve tool + type required-qual, port check, 404/403 sentinels), German messages.
- `internal/tools/adapters/http/tools.go` (+ `InspectionRoutes()` or handler method) -- `StartInspection` handler + DTO.
- `cmd/server/main.go` -- mount `POST /api/v1/tools/{id}/inspection/start` behind `inspection.submit`.
- `web/src/auth/tools.ts` -- `startInspection(toolId)` client.
- `web/src/pages/DashboardPage.tsx` (+css) -- "Prüfung starten" per row: 200 → navigate, 403 → German reason + disable, 401 → login.
- `web/src/App.tsx` -- stub `/inspection/:toolId` route.
- Tests: core (eligible / qualified / missing / expired / no-qual-type / 404 / forbidden), postgres (user-granted-qualification read + expiry), http (200/403/404/401), composition mount gate, web (button navigate/disable/403/401).

## Tasks & Acceptance

**Execution:**
- [x] `internal/user/ports/ports.go` + `core/qualifications.go` + postgres -- `UserHoldsQualification` (expiry-aware) -- port
- [x] `internal/tools/core/inspections.go` -- `StartInspection` + sentinels + messages -- core
- [x] `internal/tools/adapters/http` -- handler + route + DTO -- API
- [x] `cmd/server/main.go` -- inspection.submit mount -- composition root
- [x] `web/src/auth/tools.ts` -- `startInspection` client -- SPA
- [x] `web/src/pages/DashboardPage.tsx` (+css) + `App.tsx` -- start button + stub route -- SPA
- [x] Tests -- core/postgres/http/composition/web incl. I/O rows -- verification

**Acceptance Criteria:**
- Given an authenticated user viewing a tool whose type requires a Qualification, then the inspection start is gated: only users holding that Qualification may start an inspection (FR-11/AD-7), and the server re-validates on the call, never trusting the client (AD-6/AD-7).
- Given a caller lacking the required Qualification, then the start call answers HTTP 403 with a clear German explanation, and the dashboard control shows "Erforderliche Qualifikation fehlt" and is disabled (FR-11/UX-DR7/UX-DR8).
- Given a caller holding the required Qualification, then the start call answers 200 and the inspection screen opens (FR-11); granted qualifications are resolved through the auth port (AD-7).

## Spec Change Log

- **Review patches applied (review 1, 2026-09-12):** `GetToolWithTypeQualification` now guards `tt.archived_at IS NULL` (a tool whose type was archived can no longer start inspections with the retired type's gating data); `InspectionPage` re-fetches when router state is absent (refresh/deep-link safe) and uses `||` fallbacks so empty strings don't bypass them; the HTTP 500 default branch (internal_error envelope, client-abort guard) is pinned by a new test; the composition start test now asserts the `{id}` path param really reaches the service; the route pattern is owned once (InspectionRoutes registers, composition mounts); `INSPECTION_SUBMIT_PERMISSION` added client-side; the 403-disable set persists across list refetches (wording corrected); a double-submit guard fires only one start request; `mapInspectionError`'s ErrForbidden + nil-user 401 branches are pinned; InspectionPage mode-label mapping (pass_fail/checklist/–) tested; buttons get per-tool aria-labels; two tautological/weak core-test assertions fixed; rowErrors cleared on reload; `newCompositionToolsRouter` renamed; subtitle is user-facing; the SQL NULL→"" qualification mapping uses an explicit `.Valid` check.

## Design Notes

- **Start is the gate, submit re-validates (AD-6):** because no inspection data model exists yet, Story 5.1's `POST .../inspection/start` IS the server call that answers 403 for the ineligible AC. The eventual submit endpoint (5.4/5.5) re-checks `UserHoldsQualification` independently — the gate is never only at the button.
- **Intra-module required-qual resolution:** `tools.tool_type_id → tool_types.required_qualification_id` is a Tool-owned JOIN/lookup, not a cross-module call (AD-8/AD-11 forbid only joining OTHER modules' tables). User granted qualifications are the ONLY cross-module resolution and go through the extended port.
- **Expiry-aware eligibility:** a fixed qualification assignment past `expires_at` is NOT held (mirror `qualification_status.go`), so a lapsed volunteer cannot start — live resolution per AD-7/FR-22.
- **SPA: 403-as-gate, not pre-query:** the button is enabled until clicked; the 403 carries the German reason and disables it for the session. This avoids threading actor-specific eligibility into the ungated dashboard list DTO and matches "one permission per surface".

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go/web tests pass, 0 lint issues
- `curl` as a qualified user: POST /api/v1/tools/{id}/inspection/start -- expected: 200 with tool + mode
- `curl` as a user lacking the required qualification: POST -- expected: 403 "Erforderliche Qualifikation fehlt."
- `curl` without a session / without inspection.submit -- expected: 401 / 403 uniform envelope
- `npx vitest run` in web/ -- expected: all pass incl. dashboard start-button tests

**Manual checks (if no CLI):**
- As a qualified volunteer, the dashboard row's "Prüfung starten" navigates to the stub inspection screen; as an unqualified user it shows "Erforderliche Qualifikation fehlt" and disables.