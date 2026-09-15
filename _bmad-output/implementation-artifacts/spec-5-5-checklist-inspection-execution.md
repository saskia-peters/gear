---
title: 'Checklist Inspection Execution (FR-12)'
type: 'feature'
created: '2026-09-14'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'c846036dc8a5711e10df811ae6f3f18358b009ce'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-5-context.md'
  - '{project-root}/_bmad-output/implementation-artifacts/spec-5-4-pass-fail-inspection-execution.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Checklist-mode inspections still use the placeholder submit (Story 5.4 kept it), so a checklist inspection persists nothing — a failed checklist item can't trigger OOS. The backend `SubmitInspection` already handles checklist mode (per-item results + overall result + derived status).

**Approach:** Wire the checklist submit to the real endpoint (mirroring the 5.4 pass/fail wiring), add the "Alle bestanden" shortcut (FR-12/UX-DR7), and drive the confirmation from the server-persisted outcome + derived status (a failed item → OOS consequence). The screen already renders one chips group per item and blocks submit until every item is answered.

## Boundaries & Constraints

**Always:**
- **`InspectionPage` submit** (`web/src/pages/InspectionPage.tsx` `handleSubmit` L285-345): the checklist branch builds the real payload — `{ mode: 'checklist', result: failedCount > 0 ? 'fail' : 'pass', notes: comment, items: checklistItems.map(item => ({ item_id: item.id, result: itemResults[item.id] })) }` — and calls `submitInspection(toolId, input)` (the 5.4 client, `web/src/auth/tools.ts`), with the same response-shape guard, `setServerStatus`/`setServerOutcome`, 401→login, and inline-error handling as the pass_fail branch. Remove the `submitInspectionPlaceholder` checklist path (and the import if it becomes unused).
- **"Alle bestanden" shortcut** (`InspectionPage`): a ≥48px button in checklist mode that sets EVERY item's result to `'pass'` (`setItemResults` for all `checklistItems`), German label "Alle bestanden", disabled while `submitting || submitted`, hidden in non-checklist modes. It is a convenience, not a gate — per-item answers remain editable.
- **Confirmation** (`InspectionPage` L195-204 `outcomeLabel`, L242-254 consequence): for checklist, after a successful submit the confirmation names the SERVER-persisted outcome — `serverOutcome` (`'pass'` → "BESTANDEN", `'fail'` → "`N` von `M` Punkten NICHT BESTANDEN" from the response) — and appends "⛔ Wird als Außer Betrieb gesperrt." only when `serverStatus.status === 'oos'` (a failed item → OOS, Story 5.3/5.6). The local `failedCount` remains the pre-submit/blocking signal only.
- **Backend:** no changes — 5.3 `SubmitInspection` already validates (items exactly equal the type's checklist, overall result pass|fail) and derives status; 5.6 blocks checklist submits on OOS tools (403). Verify only.
- **Tests** (`web/src/pages/InspectionPage.test.tsx`): checklist submit now stubs the real fetch — all-pass → 200 green + "BESTANDEN", some-failed → 200 oos + "N von M ... NICHT BESTANDEN" + OOS consequence, incomplete → blocked (no fetch), "Alle bestanden" sets all items (then submit enabled), 400/401/403/500 inline, checklist submit on an OOS tool → 403 (5.6). Remove the placeholder-based checklist assertions.

**Ask First:**
- None.

**Never:**
- No backend changes (the endpoint + OOS block are the contract).
- No change to the pass/fail path (5.4).
- No change to the "every item required" rule (FR-12) or the qualification/OOS gates.
- No change to the derived-status semantics.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| CHECK_SUBMIT_OK | all items pass, optional note | 200 green → "BESTANDEN" confirmation, then ~2s auto-return | n/a |
| CHECK_SUBMIT_FAIL | ≥1 item fails | 200 oos → "N von M Punkten NICHT BESTANDEN" + "⛔ Wird als Außer Betrieb gesperrt.", auto-return | n/a |
| CHECK_SUBMIT_INCOMPLETE | an item unanswered | submit blocked, no fetch | n/a |
| CHECK_ALLE_BESTANDEN | shortcut tapped | every item set to pass; submit enabled | n/a |
| CHECK_SUBMIT_400 | server validation error | inline German, no navigation/confirmation | 400 |
| CHECK_SUBMIT_401 | expired/revoked session | clearAuthState + /login | 401 |
| CHECK_SUBMIT_403 | OOS tool (5.6) or gating | inline German, no navigation | 403 |
| CHECK_SUBMIT_500 | unexpected failure | inline German | 500 |
| CHECK_EMPTY | type has no checklist items | note shown, submit disabled | n/a |

</frozen-after-approval>

## Code Map

- `web/src/pages/InspectionPage.tsx` (469 lines) -- the whole touch surface.
  - L7 import: `startInspection, submitInspection, submitInspectionPlaceholder` — drop `submitInspectionPlaceholder` when the checklist branch is real.
  - `handleSubmit` L285-345: guards L290-295 (double-submit + `isChecklistComplete`/`result` gates); **checklist placeholder branch L300-303** (`await submitInspectionPlaceholder()` — replace with the real payload + `submitInspection(toolId, {...})`, mirroring the pass_fail branch L304-327); response-shape guard L321-324 (`!serverResult || !serverResult.status || !serverResult.inspection` → "Ungültige Serverantwort."); `setServerStatus`/`setServerOutcome` L325-326; catch L329-340 (401 → `clearAuthState()` + `/login`; else inline error from `err.message`).
  - `outcomeLabel` L224-237: currently checklist derives from local `failedCount` (`${failedCount} von ${checklistItems.length} Punkten NICHT BESTANDEN`); switch to server-driven after submit.
  - `isFailure` L242 + `oosConsequence` L251-258: checklist branch currently local-driven; switch both modes to `serverStatus?.status === 'oos'` (drop `isFailure` if unused).
  - `itemResults` state L116; `isChecklistComplete` L210-211, `failedCount` L212, `canSubmit` L217; `handleItemSelect` L263-273; checklist chips map L402-411 (one `PassFailChips` per item, `onSelect={(value) => handleItemSelect(item.id, value)}`), empty-checklist note L397-400 — add the "Alle bestanden" button here.
  - comment state L122, textarea L427-437 (`maxLength` 4000, label "Anmerkung (optional)"); submit button L444 `disabled={!canSubmit || submitting || submitted}`; confirmation L458-461 (`Die Prüfung für „{toolName}“ wurde gespeichert — Ergebnis: {outcomeLabel}. {oosConsequence}Du kehrst zur Werkzeugliste zurück.`).
- `web/src/auth/tools.ts` (459 lines) -- client already exists (5.4), verify-only.
  - `InspectionSubmitInput` L357-362 (`mode`, `result`, `notes`, `items`); `InspectionItemSubmitInput` L348-351 (`item_id`, `result`); `InspectionSubmitResult` L398-401 (`inspection` + `status`); `InspectionSubmitRecord` L378-387 (`overall_result` + `items: InspectionSubmitItem[]`).
  - `submitInspection` L410-416: `POST ${DASHBOARD_TOOLS_URL}/${encodeURIComponent(toolId)}/inspection`, body JSON, casts result; errors reject via `request` (`ApiError` with `status`/`message`).
  - `submitInspectionPlaceholder` L418-428: used ONLY at InspectionPage L7+L303 — safe to remove once the branch is real.
- `web/src/pages/InspectionPage.test.tsx` (981 lines) -- rewrite the placeholder-based checklist tests to stub the real fetch.
  - Helpers: `checklistItemsFixture()` L16-22 (3 items item-1/2/3), `renderPage` L26-38, `eligibleStart` L40-50, `stubFetch` L52-56, `stubMatchMedia` L62-75, `submitOkResponse(overallResult, status)` L83-101 (shape `{inspection:{..., overall_result, items:[]}, status:{status, next_due}}`), `submitErrorResponse` L104-106, `renderLoaded` L244-256, `CHECKLIST_ENTRY` L259-268 (checklist items via router state).
  - Placeholder-based checklist tests to convert: `CHECKLIST_SUBMIT_ALL_PASS` L589-615, `CHECKLIST_SUBMIT_SOME_FAILED` L617-635, `OOS_CONSEQUENCE_CHECKLIST_FAIL` L918-949 (they currently never stub fetch).
  - Pass_fail canonical pattern to mirror: `SUBMIT` L366-400 asserts `fetchMock.mock.calls[0]` URL/method/body + confirmation; error matrix L687-791 (400/401/403/404/500/network), retry L815-839, `OUTCOME_FROM_SERVER_RECORD` L841-855 (local pass + server fail), `SUBMIT_INVALID_SERVER_RESPONSE` L903-916.

## Tasks & Acceptance

**Execution:**
- [x] `web/src/pages/InspectionPage.tsx` -- real checklist submit (payload + client + server-driven confirmation), "Alle bestanden" shortcut, remove the placeholder path -- SPA
- [x] `web/src/auth/tools.ts` -- drop `submitInspectionPlaceholder` if unused -- SPA client
- [x] `web/src/pages/InspectionPage.test.tsx` -- checklist submit/alle-bestanden/error/OOS-block tests -- verification

**Acceptance Criteria:**
- Given a checklist-mode tool with items, when I answer every item and submit, then the inspection persists each per-item result + the overall result + my identity/timestamp, and the confirmation names the server outcome (FR-12/FR-18/NFR-O1).
- Given at least one item failed, when I submit, then the tool transitions to OOS immediately (FR-12/FR-14/AD-4) and the confirmation names the consequence.
- Given the checklist screen, when I tap "Alle bestanden", then every item is set to pass and the submit becomes available (FR-12/UX-DR7).
- Given an unanswered item or an OOS tool, when I submit, then it is blocked / answered 403 with nothing persisted.

## Spec Change Log

## Design Notes

- **Payload mirrors the server contract:** `items` is exactly the type's checklist (all answered, `item_id` + per-item result); the overall `result` is derived `failedCount > 0 ? 'fail' : 'pass'`. The server re-validates (exact-set match) — no client trust.
- **Server-authoritative confirmation (as 5.4):** after a real submit the checklist confirmation reads `inspection.overall_result` + `status.status` from the response; the local `failedCount` is only the pre-submit signal.
- **"Alle bestanden" is a convenience, not a shortcut around the rule:** each item stays editable; the every-item-required gate (FR-12) is unchanged.

## Verification

**Commands:**
- `npm --prefix web run lint` && `npm --prefix web run typecheck` -- expected: clean
- `npx vitest run` in web/ -- expected: all pass incl. the checklist submit / Alle-bestanden / OOS-block cases
- `npm --prefix web run build` -- expected: SPA builds

**Manual checks (if no CLI):**
- Start a checklist inspection, tap "Alle bestanden", submit → "BESTANDEN" and auto-return; fail one item, submit → "N von M ... NICHT BESTANDEN" + "⛔ Wird als Außer Betrieb gesperrt.", and the tool returns as "Außer Betrieb" on the dashboard (reinstatable by a Fuehrung/Admin).

## Suggested Review Order

**Submit wiring (entry point)**

- The real checklist submit: payload mirrors the server contract, response guard rejects malformed 200s, server snapshot drives the confirmation.
  [`InspectionPage.tsx:305`](../../web/src/pages/InspectionPage.tsx#L305)

- The response-shape guard hardened so only fields the confirmation consumes are accepted.
  [`InspectionPage.tsx:347`](../../web/src/pages/InspectionPage.tsx#L347)

- Checklist items must be an array before the confirmation reads the failure count.
  [`InspectionPage.tsx:364`](../../web/src/pages/InspectionPage.tsx#L364)

**Server-authoritative confirmation**

- `outcomeLabel` reads the server-persisted `overall_result`/per-item snapshot, not the local chips.
  [`InspectionPage.tsx:234`](../../web/src/pages/InspectionPage.tsx#L234)

- `oosConsequence` follows `serverStatus.status === 'oos'` for both modes — never a local guess (AD-4).
  [`InspectionPage.tsx:260`](../../web/src/pages/InspectionPage.tsx#L260)

**"Alle bestanden" shortcut**

- One-tap convenience that marks every item passed; stays a non-gate, per-item chips remain editable.
  [`InspectionPage.tsx:283`](../../web/src/pages/InspectionPage.tsx#L283)

- ≥48px secondary button rendered only in checklist mode, disabled while submitting/submitted.
  [`InspectionPage.tsx:461`](../../web/src/pages/InspectionPage.tsx#L461)

- Button styling.
  [`InspectionPage.module.css:198`](../../web/src/pages/InspectionPage.module.css#L198)

**Client contract**

- The shared `submitInspection` client the checklist branch reuses (5.4 contract, now checklist-wired).
  [`tools.ts:410`](../../web/src/auth/tools.ts#L410)

**Tests**

- All-pass posts the real payload and confirms from the server, then auto-returns.
  [`InspectionPage.test.tsx:637`](../../web/src/pages/InspectionPage.test.tsx#L637)

- Shortcut sets every item, stays editable, and submits an all-pass payload.
  [`InspectionPage.test.tsx:753`](../../web/src/pages/InspectionPage.test.tsx#L753)

- Consequence follows the server status even when the local chips say fail (server-driven negation).
  [`InspectionPage.test.tsx:900`](../../web/src/pages/InspectionPage.test.tsx#L900)

- Post-submit lock: every per-item chip and the shortcut disable after submit.
  [`InspectionPage.test.tsx:929`](../../web/src/pages/InspectionPage.test.tsx#L929)

- Non-empty Anmerkung travels as `notes` in the checklist body.
  [`InspectionPage.test.tsx:954`](../../web/src/pages/InspectionPage.test.tsx#L954)

- Malformed 200 (record lacks consumed fields) answers "Ungültige Serverantwort." instead of confirming.
  [`InspectionPage.test.tsx:980`](../../web/src/pages/InspectionPage.test.tsx#L980)