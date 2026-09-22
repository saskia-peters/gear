---
title: 'Pass/Fail Inspection Execution (FR-13)'
type: 'feature'
created: '2026-09-14'
status: 'done'
review_loop_iteration: 0
baseline_commit: '37f9b2fe451164afc9bd3da5d6280ad4875a06db'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-5-context.md'
  - '{project-root}/_bmad-output/implementation-artifacts/spec-5-3-out-of-service-flagging.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The pass/fail inspection screen (Story 5.2) still uses the `submitInspectionPlaceholder` — a submit that persists nothing. Story 5.3 built the real backend (`POST /api/v1/tools/{id}/inspection`: gating, validation, persistence, derived status incl. OOS), but the SPA never calls it, so a volunteer's inspection is not actually recorded and the consequence copy is client-guessed.

**Approach:** Wire the pass/fail submit in the SPA to the real endpoint. The screen sends `{ mode: 'pass_fail', result, notes, items: [] }`, records identity/timestamp/result/notes server-side (FR-13/AD-4), and the confirmation is driven by the SERVER-authoritative derived status (`oos` → "⛔ Wird als Außer Betrieb gesperrt") — closing the 5.3 review gap. Validation/gating failures surface as inline German errors with no navigation. Checklist mode keeps its placeholder seam (Story 5.5 wires it).

## Boundaries & Constraints

**Always:**
- **Client** `web/src/auth/tools.ts`: new `submitInspection(toolId, input)` → `POST ${DASHBOARD_TOOLS_URL}/${toolId}/inspection` with `{ mode, result, notes, items }`, typed `InspectionSubmitInput` / `InspectionSubmitResult` (`{ inspection, status: { status: 'oos'|'red'|'orange'|'green', next_due: string|null } }`), following the `createToolType` POST pattern (L89-95). Types mirror the server DTO.
- **InspectionPage** (`web/src/pages/InspectionPage.tsx`): widen the `submitInspection` prop (L59-61) from `() => Promise<void>` to `(input: InspectionSubmitInput) => Promise<InspectionSubmitResult>`; the default calls the real client bound to `useParams` `toolId` (L86). `handleSubmit` (L235-256) for PASS_FAIL builds `{ mode: 'pass_fail', result, notes: comment, items: [] }` (comment state L112, textarea L334-349) and awaits the prop; a 200 stores the server result + `setSubmitted(true)`; any error (400/403/404/network) shows an inline German `role="alert"` and does NOT navigate or confirm. 401 → `clearAuthState()` + `/login` (the load-path pattern L138-142).
- **Server-authoritative consequence:** for pass_fail, the confirmation (L362-366) appends "⛔ Wird als Außer Betrieb gesperrt." only when the RETURNED `status.status === 'oos'` — never from local `isFailure` (the server derives OOS from the full history). `outcomeLabel` (L195-204) stays client-side German.
- **Checklist mode (5.4):** the placeholder submit stays — no fetch, local consequence, unchanged tests. Story 5.5 wires it (+ "Alle bestanden").
- **Preserved:** the double-submit guard (`submitPendingRef`), auto-return ~2s (Reduce Motion aware), submit button disabled while submitting/submitted.
- **Tests:** new `web/src/auth/tools.test.ts` client test (POST url/method/headers/body, response cast); extend `InspectionPage.test.tsx` with fetch-stubbed pass_fail submit cases.
- Checklist/items payload type is defined now (server contract) but unused until 5.5.

**Ask First:**
- None.

**Never:**
- No backend changes (the 5.3 endpoint + derived status are the contract; verify only).
- No checklist submit wiring, no "Alle bestanden" (Story 5.5).
- No color-coded dashboard rendering (6.1).
- No reinstatement surface (5.6).
- No change to the qualification gating or the `inspection.submit` permission.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| SUBMIT_PASS | result pass, optional notes | 200 green → confirmation "BESTANDEN" (no consequence), then ~2s auto-return | n/a |
| SUBMIT_FAIL | result fail | 200 oos → confirmation "NICHT BESTANDEN" + "⛔ Wird als Außer Betrieb gesperrt.", auto-return | n/a |
| SUBMIT_400 | server validation error | Inline German `role="alert"`, no navigation, no confirmation | 400 |
| SUBMIT_401 | expired/revoked session | `clearAuthState()` + navigate `/login` | 401 |
| SUBMIT_403 | qualification revoked / permission lost | Inline German message, no navigation | 403 |
| SUBMIT_404 | tool archived/gone | Inline German message, no navigation | 404 |
| SUBMIT_NETWORK | connection failure | Inline German "Verbindung…" error | 0 |
| CHECKLIST_MODE | checklist tool submits | Placeholder unchanged (no fetch) in 5.4 | n/a |

</frozen-after-approval>

## Code Map

- `web/src/auth/tools.ts` -- add `submitInspection` client + `InspectionSubmitInput`/`InspectionSubmitResult` types near `startInspection` (L302-307); follow `createToolType` (L89-95) POST + cast pattern; `DASHBOARD_TOOLS_URL` = `/api/v1/tools` (L262); `ApiError`/`request`/`authTokenHeaders` already imported (L12).
- `web/src/pages/InspectionPage.tsx` -- seam prop (L59-61, default L85), `toolId` (L86), `comment` state (L112) + textarea (L334-349), `handleSubmit` (L235-256: add error `catch`, server-result state, checklist placeholder branch), confirmation (L362-366: server-driven consequence), load-error render pattern (L274-277) reused for the submit error.
- `web/src/auth/tools.test.ts` (new) -- client contract test (mirrors `settings.test.ts` style: stubOk, assert url/method/headers/body).
- `web/src/pages/InspectionPage.test.tsx` -- helpers (L27-57): `renderPage`/`renderLoaded`/`eligibleStart`/`stubFetch`/`stubMatchMedia`; SUBMIT test (L337-361) + double-submit (L374-399) get fetch stubs for the real client; add 200-green, 200-oos, 400, 401, 403, network cases; checklist tests (L570+) unchanged.

## Tasks & Acceptance

**Execution:**
- [x] `web/src/auth/tools.ts` -- `submitInspection` client + input/result types -- SPA client
- [x] `web/src/pages/InspectionPage.tsx` -- widen seam, real pass_fail submit, error state, server-driven consequence, checklist placeholder branch -- SPA
- [x] `web/src/auth/tools.test.ts` (new) -- client contract -- verification
- [x] `web/src/pages/InspectionPage.test.tsx` -- fetch-stubbed pass/fail submit cases (200 green/oos, 400, 401, 403, network, double-submit, auto-return) -- verification

**Acceptance Criteria:**
- Given a pass/fail-mode tool and a selected result, when I submit, then the inspection is recorded with my identity, timestamp, result and optional notes (FR-13/AD-4/NFR-O1).
- Given I submitted NICHT BESTANDEN, when the submit succeeds, then the confirmation names the OOS consequence using the server's derived status (FR-13/FR-14/UX-DR8).
- Given a submit fails validation or gating, when the error returns, then a German error shows inline and the screen does not navigate or claim success.
- Given a successful submit, when the confirmation renders, then the page auto-returns to a refreshed Dashboard (~2s, immediate under Reduce Motion).

## Spec Change Log

- **Review patches (review 1, 2026-09-14, user-approved):** the confirmation outcome now reads the server-persisted `inspection.overall_result` (not the local chip); the dead `submitInspection` prop seam was removed (the page calls the client directly; its unreachable wrong-message guard dropped); prose comments corrected to the real OOS semantics; tests added for non-empty notes in the body, retry-after-error, 500/generic, orange/red responses (no OOS copy), the real pass-on-OOS-tool divergence, server-record outcome, invalid-server-response, and robust 401 extraction; notes textarea `maxLength` 2000 → 4000 (server bound); the unwired 5.5 checklist-items test was removed (5.5 covers it) and the "no type change" comment corrected (5.5 derives the checklist `result`); in-flight submit feedback added (`aria-busy` + "Wird gespeichert…"); `DASHBOARD_TOOLS_URL` is exported and imported by the test.
- **Backend deviation (2026-09-14, user-approved):** the review surfaced a safety-critical bug in the Story 5.3 OOS derivation — a passing inspection implicitly cleared OOS, contradicting "reinstatement is the sole exit from OOS". Per the user's decision it is FIXED in this story (a deviation from the frozen "No backend changes"): `deriveToolStatus` now keys OOS on the latest FAILED inspection (at-or-after the latest reinstatement), the status read returns `LatestFailAt`, and the SPA's server-driven consequence is therefore genuinely authoritative. Full correction recorded in the spec-5-3 change log.

## Design Notes

- **Server-authoritative consequence:** the confirmation uses `status.status === 'oos'` from the response, not the client's `isFailure` — the server derives OOS from the full inspection + reinstatement history (an already-OOS tool, or a fail on a tool just reinstated, differ from a naive client guess). This is the 5.3 review-gap closure.
- **Seam widening is required:** the zero-arg `() => Promise<void>` seam cannot carry the payload or return the server result; widening it is the minimal change that makes the submit real and keeps the double-submit test (held-promise injection) working.
- **Checklist stays placeholder in 5.4** to keep this story pass/fail-only; the `items` payload field is already in the client contract so 5.5 wires it without a type change.
- The SPA textarea `maxLength` 2000 is tighter than the server's 4000-rune bound (5.3) — a stricter client cap is fine; the server stays authoritative.

## Verification

**Commands:**
- `npm --prefix web run lint` && `npm --prefix web run typecheck` -- expected: clean
- `npx vitest run` in web/ -- expected: all pass incl. the new submit cases
- `npm --prefix web run build` -- expected: SPA builds

**Manual checks (if no CLI):**
- Start a pass/fail inspection, pick NICHT BESTANDEN, add a note, submit → the confirmation names "⛔ Wird als Außer Betrieb gesperrt." and auto-returns; the record is persisted (visible via the 5.3 API). A passing submit shows no consequence.

## Suggested Review Order

**OOS derivation fix (safety-critical backend correction, user-approved)**

- The corrected OOS rule: latest FAILED inspection at-or-after the latest reinstatement — a pass never clears OOS (sole exit = reinstatement).
  [`status.go:80`](../../internal/tools/core/status.go#L80)

- The status-read bundle now carries `LatestFailAt`; submit passes it to the derivation.
  [`inspections.go:249`](../../internal/tools/core/inspections.go#L249)

- `GetLatestFailedInspection` query — latest fail anchor (nil-safe when never failed).
  [`inspections_repo.go:102`](../../internal/tools/adapters/postgres/inspections_repo.go#L102)

**SPA submit wiring**

- `handleSubmit`: real client call, error state, no navigation on failure, retry.
  [`InspectionPage.tsx:285`](../../web/src/pages/InspectionPage.tsx#L285)

- `serverOutcome` — the confirmation names the server-persisted overall_result, not the local chip.
  [`InspectionPage.tsx:137`](../../web/src/pages/InspectionPage.tsx#L137)

- Server-authoritative consequence (only `status === 'oos'`, now genuinely meaningful after the fix).
  [`InspectionPage.tsx:251`](../../web/src/pages/InspectionPage.tsx#L251)

- Confirmation JSX (outcome + consequence + auto-return copy).
  [`InspectionPage.tsx:460`](../../web/src/pages/InspectionPage.tsx#L460)

- The client: `submitInspection` POST + the wire types.
  [`tools.ts:392`](../../web/src/auth/tools.ts#L392)

**Tests**

- The two semantic OOS tests (pass-after-fail stays OOS; reinstatement clears it).
  [`status_test.go:51`](../../internal/tools/core/status_test.go#L51)

- Pass-on-OOS-tool divergence + server-record outcome + invalid-response cases.
  [`InspectionPage.test.tsx:841`](../../web/src/pages/InspectionPage.test.tsx#L841)

- Client contract (POST body/headers, 400/500 ApiError).
  [`tools.test.ts:1`](../../web/src/auth/tools.test.ts#L1)