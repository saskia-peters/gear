---
title: 'Inspection UX Foundation (FR-8/UX-DR3/DR5/DR6/DR7/DR8/DR9/DR10)'
type: 'feature'
created: '2026-09-12'
status: 'done'
review_loop_iteration: 0
baseline_commit: '7eb727d1a6869b5430daa9a06678c23c805fef17'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-5-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The inspection screen is a stub (from 5.1) showing only the tool name + mode. Volunteers need a real single-screen, safety-focused inspection flow with fewest taps.

**Approach:** Build the inspection UX foundation in the SPA (user decision: **UX-only, no backend**): the single-screen surface (always single-column at all widths), large ≥48px green/red Pass/Fail chips, the tool name/identifier + type's mode, one submit → inline German confirmation → ~2s auto-return to a refreshed Dashboard (Reduce Motion skips the delay), and the accessibility floor. The submit is a clearly-marked UX placeholder seam — the real record endpoint (persistence, identity/timestamp, per-item results, OOS) lands with Stories 5.3/5.4/5.5, which define the persisted shape. No migration, no inspection record table.

## Boundaries & Constraints

**Always:**
- **SPA-only.** Replace the `InspectionPage` stub's content (keep its data loading: router state or `startInspection` re-fetch). The page stays at `/inspection/:toolId` behind `AuthenticatedPage`.
- **Single-column, safety-critical** (UX-DR3/DR10): the inspection content is always one column at every width — never split/side-by-side. Reuse the existing `.main`/`.section` column layout.
- **Tool header**: tool name (and, when available, the inventory number as the identifier) + the type's mode label (Pass/Fail or Checkliste). Extend the `InspectionStart` data to carry `inventory_number` (add it to the dashboard navigation state from the tool list, which already has it) so the header shows the identifier; falls back to tool name.
- **Pass/Fail chips** (UX-DR5/DR9): large ≥48px tappable toggle chips per the DESIGN spec — green (OK/BESTANDEN) vs red (FEHLER/NICHT BESTANDEN), using the existing `--gear-color-pass-chip-*`/`--gear-color-fail-chip-*` tokens and `--gear-radius-md`. Pattern: visually-hidden `<input type="radio">` inside a `<label>` with the active class on the label + a keyboard focus ring via `:has(input:focus-visible)` (mirror the AdminWerkzeugePage mode switch). Chips are the UX-only foundation (5.4/5.5 wire them to the actual mode execution).
- **One submit** (UX-DR7/DR8): a single "Prüfung speichern" button (du-form German, ≥48px). Clicking it renders an inline confirmation (`role="status"`, German) — a placeholder that names the tool and a generic saved consequence — then auto-returns to `/` after ~2s, which remounts DashboardPage and re-fetches the tool list ("refreshed Dashboard"). Under `prefers-reduced-motion: reduce`, skip the delay and redirect immediately after the confirmation renders. No chained modals, no second step.
- **Accessibility floor** (UX-DR9): ≥48px targets, keyboard-operable chips (focus ring, arrow/space to toggle the radios), SR announcements via `role="status"`/`role="alert"`, no icon-only buttons, Reduce Motion handled (a `matchMedia('(prefers-reduced-motion: reduce)')` check or a small hook).
- **Double-submit guard**: disable the submit button while the placeholder submit is "in flight" (and during the auto-return delay).
- **No backend changes**: no migration, no new endpoint, no inspection record table. The submit handler is a clearly-commented UX placeholder (no fetch to a nonexistent endpoint); keep the seam so 5.4/5.5 replace it with the real record call.
- **Copy**: du-form German throughout; the stub subtitle "Die Prüfungsoberfläche wird in einer späteren Version bereitgestellt." is replaced.

**Ask First:**
- None (the UX-only scope is user-resolved; the rest follows the epic ACs + wireframes + existing patterns).

**Never:**
- No inspection record persistence, no OOS, no status/clock derivation (Stories 5.3/5.4/5.5, AD-4/AD-5).
- No fabricated inspection POST to a nonexistent endpoint.
- No two-column/split layout for inspection content.
- No icon-only buttons; no chained modals.
- No changes to the backend `POST /api/v1/tools/{id}/inspection/start` or the inspection.submit gate.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| RENDER | start data present | Single-column screen: tool name + identifier + mode + chips + submit | n/a |
| RENDER_NO_STATE | refresh/deep link, state gone | Re-fetches via startInspection (existing behavior), then renders | 401 → login; error → alert |
| CHIP_TOGGLE | user taps a chip | Selected chip fills (green/red active), others clear, keyboard/SR work | n/a |
| SUBMIT | click Prüfung speichern | Inline `role="status"` confirmation named tool, button disabled | n/a |
| AUTO_RETURN | ~2s after confirmation | navigate('/') → Dashboard remounts + refetches tool list | n/a |
| REDUCE_MOTION | prefers-reduced-motion: reduce | Redirect immediately after confirmation renders (no delay) | n/a |
| UNSUBMITTED | user leaves without submitting | No confirmation, no navigation (state preserved per router) | n/a |

</frozen-after-approval>

## Code Map

- `web/src/pages/InspectionPage.tsx` (+`.module.css`) -- replace the stub content: header (tool + identifier + mode), Pass/Fail chips (foundation), submit + confirmation + auto-return (Reduce Motion aware), a11y, placeholder-submit seam.
- `web/src/components/` -- a small `PassFailChips.tsx` (or inline in InspectionPage) for the two large toggle chips (green/red, radio pattern, focus ring); reuse tokens.
- `web/src/auth/tools.ts` -- carry `inventory_number` through the dashboard → inspection navigation state (the tool list already has it); the `InspectionStart` type gains `inventory_number` if the start response should include it.
- `web/src/pages/DashboardPage.tsx` -- pass the tool's inventory number in the inspection navigation state.
- `web/src/pages/InspectionPage.test.tsx` -- extend: chips render + toggle, submit → confirmation → auto-return (fake timers), Reduce Motion skip, SR announcements, single-column layout, disabled-while-submitting.
- Tests: component + page tests only (no Go/DB changes).

## Tasks & Acceptance

**Execution:**
- [x] `web/src/pages/InspectionPage.tsx` (+css) -- replace the stub with the single-screen foundation -- SPA
- [x] Pass/Fail chips component -- large green/red toggle chips (radio pattern, focus ring) -- SPA
- [x] Submit → confirmation → auto-return (Reduce Motion aware) + double-submit guard -- SPA
- [x] `web/src/auth/tools.ts` + `DashboardPage.tsx` -- carry inventory_number into the inspection header -- SPA
- [x] `InspectionPage.test.tsx` (+ App/Dashboard tests as needed) -- cover the I/O rows -- verification

**Acceptance Criteria:**
- Given a user starts an inspection, when the inspection screen renders, then it is a single checklist screen, always single-column at all widths, showing the tool name/identifier and its type's mode (FR-8/UX-DR6).
- Given the mode is pass/fail or checklist, when each item/toggle renders, then it uses large ≥48px tappable Pass/Fail chips — green OK/BESTANDEN vs red FEHLER/NICHT BESTANDEN (UX-DR5/UX-DR9).
- Given a user completes an inspection, when they submit, then one submit performs the whole inspection (no chained modals) and they are auto-returned to a refreshed Dashboard (~2s) with a post-submit inline confirmation (UX-DR7/DR8/DR6).
- Given the inspection surface, when it is built, then it meets the accessibility floor (≥48px targets, keyboard-operable, SR announcements, no icon-only buttons, Reduce Motion skips auto-return) (UX-DR9).

## Spec Change Log

- 2026-09-13 (post-review loopback 1): the review confirmed the single-column constraint is enforced structurally (jsdom does not evaluate layout — CSS modules are unprocessed in vitest, so `getComputedStyle` cannot prove `flex-direction: column`). The SPA unit test guards the single-column DOM contract (tool header + chips + submit are siblings in one section); the genuine single-column-at-all-widths check is a browser/e2e inspection, deferred (no e2e suite in Story 5.2). Also recorded decisions: (a) the submit requires a selected result chip (outcome-less saves are impossible — button disabled + `result === null` guard in `handleSubmit`); (b) tapping the already-selected chip deselects it; (c) the chips are disabled during submit + auto-return delay; (d) the confirmation names the outcome and uses delay-neutral copy ("Du kehrst zur Werkzeugliste zurück." — true for both the ~2s and reduced-motion paths); (e) the Gerätenummer row stays visible and falls back to the tool name when no inventory number exists.
- 2026-09-13 (review 1 patches): outcome-less save prevented (submit disabled until selected + guard); chips locked after submit (`disabled={submitting || submitted}`); confirmation names the outcome; `EMPTY_NAME_FALLBACK` restores the exact two-instance assertion; `PassFailChips.selectedClass` is a strict literal union (typo = type error); delay-neutral confirmation copy; test fixtures aligned on GEAR000001; fetched-path identifier-fallback test; dashboard's real navigation state asserted end-to-end; chip deselection on re-click; genuine double-submit guard via a held `submitInspectionPlaceholder()` promise (real seam prop). Fake-timer leaks fixed with `vi.useRealTimers()` in `afterEach`.
- 2026-09-13 (user feedback + mode-aware decision): the inspection surface is now MODE-AWARE (the user's feedback that it "does not care if it is a checklist or pass/fail item", resolved as "mode-aware now"). The `POST .../inspection/start` payload now includes the tool type's ordered `checklist_items` (backend: `GetToolWithTypeQualification` + `InspectionStartResult` + DTO, via the shared `GetToolTypeChecklistItems` query + a new `toChecklistItemDTOs` helper reused by the tool-type surface). The InspectionPage header shows the **Gerätetyp** (tool type name). `pass_fail` renders the single PassFailChips; `checklist` renders ONE chips group per checklist item (all-items-required before submit; confirmation names the failed-item count or overall BESTANDEN); unknown mode defaults to pass_fail; empty checklist disables submit with a German note. SPA navigation state forwards `tool_type_name` + `checklist_items`. Backend wiring pinned (postgres ordered items, DTO empty→`[]`).

## Design Notes

- **UX-only seam (user decision):** 5.2's ACs constrain the surface; persistence, identity/timestamp, per-item results, notes, and OOS are explicit 5.3/5.4/5.5 ACs that define the record shape. Shipping a record/migration now would force a throwaway contract. The submit handler is a clearly-commented placeholder (no fetch to a nonexistent endpoint) whose seam 5.4/5.5 replace with the real call.
- **"Refreshed Dashboard" = remount:** `navigate('/')` remounts DashboardPage, whose mount-only effect re-fetches the tool list — no server round-trip needed (the dashboard has no color-coded status until Story 6.1).
- **Chip pattern mirrors the mode switch:** visually-hidden radio + label + `:has(input:focus-visible)` ring gives keyboard + SR support with no new a11y machinery; tokens `--gear-color-pass-chip-*`/`fail-chip-*` exist.
- **Reduce Motion:** a `matchMedia('(prefers-reduced-motion: reduce)')` check (or tiny hook) makes the auto-return skip the ~2s delay and redirect immediately after the confirmation renders.

## Verification

**Commands:**
- `npm --prefix web run lint` && `npm --prefix web run typecheck` -- expected: clean
- `npx vitest run` in web/ -- expected: all pass incl. the extended InspectionPage tests
- `npm --prefix web run build` (or the docs build gate) -- expected: SPA builds

**Manual checks (if no CLI):**
- Start an inspection as a qualified user: the screen is single-column showing tool + identifier + mode; the chips toggle with keyboard; "Prüfung speichern" shows the confirmation then auto-returns to a refreshed dashboard (~2s; immediate under Reduce Motion); the stub copy is gone.