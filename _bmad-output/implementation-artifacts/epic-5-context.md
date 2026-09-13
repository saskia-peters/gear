# Epic 5 Context: Inspection Execution & Serviceability

<!-- Compiled from planning artifacts. Edit freely. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Let qualified volunteers perform safety-critical inspections and manage availability. Qualified Helfer*in/Fuehrung run checklist-mode or pass/fail-mode inspections; a failed item flips the tool to Out of Service immediately with a full failure record; Fuehrung/Admin reinstate an OOS tool with a mandatory reason, resetting the inspection clock.

## Stories

- Story 5.1: Qualification-Gated Inspection Start
- Story 5.2: Inspection UX Foundation
- Story 5.3: Out of Service Flagging
- Story 5.4: Pass/Fail Inspection Execution
- Story 5.5: Checklist Inspection Execution
- Story 5.6: Out of Service Reinstatement

## Requirements & Constraints

- Qualification gating (FR-11/AD-7): only a user holding the Tool Type's required Qualification may start/submit an inspection; the server re-validates on submit (never trusts the client); the "Prüfung starten" control is disabled with a German explanation when the qualification is absent; any attempted call → 403. Tool Types declare the required qualification via a cross-module FK to the User vocabulary; granted qualifications resolve through the auth port.
- Pass/Fail inspection (FR-13): a single large BESTANDEN/NICHT BESTANDEN toggle + optional notes; recorded with identity, timestamp, result, notes.
- Checklist inspection (FR-12): all configured checklist items from the tool type, each a large per-item pass/fail chip; an "Alle bestanden" shortcut; ALL items must be answered before submit; per-item + overall results persisted.
- Out of Service (FR-14/AD-4): any failed item / NICHT BESTANDEN transitions the tool to OOS immediately, derived on read (latest failed inspection not since reinstated), NEVER stored as a flag; feeds the shared inspection clock.
- Reinstatement (FR-15/AD-9): Fuehrung/Admin only; mandatory non-empty reason; sole exit from OOS; clock resets so next due = reinstatement + resolved schedule interval; Helfer*in cannot reinstate.
- Inspection history (FR-18): per-tool history with inspector, timestamp, overall result, mode, per-item results; reverse-chronological.
- Accessibility (UX-DR9): ≥48px targets, keyboard-operable, SR announcements, no icon-only buttons, Reduce Motion skips auto-return animation.
- Submission UX (UX-DR7/DR8/DR6): one submit performs the whole inspection, auto-return to a refreshed Dashboard (~2s), inline confirmation naming the consequence (e.g. "Wird als Außer Betrieb gesperrt").

## Technical Decisions

- Inspection records are Tool-module-owned (AD-1); the Tool hexagon already exists from Epic 4 (tool types, tools, checklist items, attributes, inventory).
- Status is DERIVED on read via a single shared clock/status function (AD-4/AD-5): next_due = last_successful_inspection + resolved schedule (per-tool override else type default); Red = past due / OOS, Orange = due ≤14 days, Green = due >14 days; never-inspected tool = Red. OOS derived from latest failed inspection not since reinstated.
- Required qualification is resolved through the auth/qualification port (AD-7) — the Tool module never joins user tables; it consumes the qualification data via a read-only port.
- The inspection clock (AD-5) and schedule resolution (AD-16) were partly prepared in Epic 4 (schedule catalog with interval unit/magnitude; per-tool override as FK).
- Cross-module orchestration (AD-8) for DSGVO (Stories 3.3/3.4, deferred) will rewrite inspector references to "Deleted User" — inspection records must reference the inspector in a way that supports that (deferred, not built here).

## UX & Interaction Patterns

- Inspection screen: single checklist screen, always single-column (never split, safety-critical input) at all widths (UX-DR3/DR10); shows tool name/identifier + its type's mode.
- Large pass/fail chips: green OK/BESTANDEN vs red FEHLER/NICHT BESTANDEN (≥48px).
- Fewest-taps: single-screen checklist, "Alle bestanden" shortcut, one submit, one confirmation (no chained modals), auto-return to refreshed Dashboard.
- German UI throughout.

## Cross-Story Dependencies

- 5.1 (gating) is a prerequisite for 5.2/5.4/5.5; 5.2 (UX foundation) is the shared surface 5.4/5.5 build on.
- 5.3 (OOS) depends on the inspection data model introduced by 5.2/5.4/5.5; 5.6 (reinstatement) depends on 5.3.
- Depends on Epic 4 (tool types with required qualification FK + checklist items, tools with optional schedule override, schedule catalog); the inspection clock resolves schedules via the shared function.
- Dashboard color-coded status (Story 6.1) consumes the derived status this epic produces.