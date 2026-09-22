## Epic 5: Inspection Execution & Serviceability

Volunteers perform safety-critical inspections and manage availability. Qualified Helfer*in/Fuehrung run checklist-mode or pass/fail-mode inspections; a failed item flips the tool to Out of Service immediately with a full failure record; Fuehrung/Admin reinstate a tool with a mandatory reason, resetting the inspection clock.

### Story 5.1: Qualification-Gated Inspection Start

As the system,
I want to gate the start of an inspection on the user's qualification,
So that only volunteers holding the Tool Type's required Qualification can submit inspections.

**Acceptance Criteria:**

**Given** I am authenticated,
**When** I view a tool whose tool type requires a Qualification,
**Then** the inspection start is gated: only users holding that Qualification may start/submit an inspection (FR-11/AD-7)
**And** the server re-validates the qualification on submit, never trusting the client (AD-6/AD-7).

**Given** I lack the required Qualification,
**When** I try to start an inspection,
**Then** I get a clear access error — the "Prüfung starten" control is disabled with an explanation in German (e.g. "Erforderliche Qualifikation fehlt") (FR-11/UX-DR7/UX-DR8)
**And** any attempted server call returns HTTP 403 (FR-11/AD-6).

**Given** I hold the required Qualification,
**When** I start an inspection,
**Then** the inspection screen opens (FR-11) and my granted qualifications are resolved through the auth port (AD-7).

### Story 5.2: Inspection UX Foundation

As a volunteer,
I want a single-screen, safety-focused inspection flow,
So that I can inspect any tool confidently with fewest taps.

**Acceptance Criteria:**

**Given** I start an inspection,
**When** the inspection screen renders,
**Then** it is a single checklist screen, always single-column (never split, safety-critical input) for all widths (UX-DR3/UX-DR10), showing the tool name/identifier and its type's mode (FR-8/UX-DR6).

**Given** the mode is pass/fail or checklist,
**When** each item/toggle is rendered,
**Then** it uses the large ≥48px tappable Pass/Fail chips — green `OK`/`BESTANDEN` vs red `FEHLER`/`NICHT BESTANDEN` (UX-DR5/UX-DR9).

**Given** I complete an inspection,
**When** I submit,
**Then** one submit performs the whole inspection (no chained modals) and I am auto-returned to a refreshed Dashboard (~2s), with a post-submit inline confirmation (UX-DR7/UX-DR8/UX-DR6).

**Given** the inspection surface,
**When** it is built,
**Then** it meets the accessibility floor (≥48px targets, keyboard-operable, SR announcements, no icon-only buttons, Reduce Motion skips auto-return animation) (UX-DR9).

### Story 5.3: Out of Service Flagging

As the system,
I want any failed inspection to transition a tool to Out of Service immediately,
So that an unsafe tool is never shown as serviceable.

**Acceptance Criteria:**

**Given** any failed item in a checklist inspection or a `NICHT BESTANDEN` pass/fail result,
**When** the inspection is submitted,
**Then** the tool transitions to Out of Service immediately and shows Red on the dashboard (FR-14/AD-4).

**Given** the OOS transition,
**When** it is recorded,
**Then** the failure event persists the inspector, timestamp, failed item(s), and defect notes (FR-14/NFR-O1).

**Given** a tool is OOS,
**When** its status is read,
**Then** OOS is derived from the latest failed inspection not since reinstated — never stored as a flag (FR-14/AD-4)
**And** the derived status feeds the shared inspection clock/status function (AD-4/AD-5).

**Given** the OOS confirmation,
**When** I submit a failing inspection,
**Then** the inline confirmation names the consequence (e.g. "⛔ Wird als Außer Betrieb gesperrt") (UX-DR6/UX-DR8).

### Story 5.4: Pass/Fail Inspection Execution

As a qualified volunteer,
I want to perform a pass/fail inspection on a tool,
So that I can record whether it is serviceable in one tap.

**Acceptance Criteria:**

**Given** a tool whose tool type is pass/fail mode,
**When** I start an inspection (per Story 5.1),
**Then** the screen presents a single pass/fail toggle (`BESTANDEN` / `NICHT BESTANDEN`) with an optional notes field (FR-13/UX-DR5).

**Given** I set a result,
**When** I submit,
**Then** the inspection is recorded with my identity, timestamp, the pass/fail result, and optional notes (FR-13/AD-4, NFR-O1).

**Given** I set `NICHT BESTANDEN`,
**When** I submit,
**Then** the tool transitions to Out of Service immediately per Story 5.3 (FR-13/FR-14/AD-4) and the confirmation names the consequence (UX-DR8).

### Story 5.5: Checklist Inspection Execution

As a qualified volunteer,
I want to perform a checklist inspection on a tool,
So that I can verify every configured checklist item before submitting.

**Acceptance Criteria:**

**Given** a tool whose tool type is checklist mode,
**When** I start an inspection (per Story 5.1),
**Then** the screen presents all configured checklist items from the tool type's checklist, each with a large per-item pass/fail chip (FR-12/UX-DR5)
**And** an "Alle bestanden" shortcut is available (UX-DR7).

**Given** any checklist item is unanswered,
**When** I try to submit,
**Then** submission is blocked until all items are answered (every item required) (FR-12).

**Given** I answer all items,
**When** I submit,
**Then** the inspection record persists each per-item pass/fail result together with the overall result and my identity/timestamp (FR-12/FR-18/NFR-O1).

**Given** at least one item failed,
**When** I submit,
**Then** the tool transitions to Out of Service immediately per Story 5.3 (FR-12/FR-14/AD-4) and the confirmation names the consequence (UX-DR8).

### Story 5.6: Out of Service Reinstatement

As a Fuehrung/Admin,
I want to reinstate an Out of Service tool with a mandatory reason,
So that a safely-repaired tool returns to service with a reset inspection clock.

**Acceptance Criteria:**

**Given** a tool is Out of Service,
**When** I (as Fuehrung or Admin) open its detail,
**Then** I can reinstate it; Helfer*in users cannot reinstate (FR-15/AD-9).

**Given** I reinstate a tool,
**When** I submit the form,
**Then** a mandatory, non-empty reason is required (FR-15/AD-9/UX-DR8)
**And** the reinstatement is the sole exit from OOS — the clock resets so next due = reinstatement + resolved schedule interval, and the resulting status color derives immediately (FR-15/AD-5/AD-9).

**Given** the reinstatement,
**When** it is recorded,
**Then** it is logged with actor, timestamp, and reason (FR-15/NFR-O1/AD-9).

**Given** I try to reinstate without the `tool.reinstate` permission,
**When** I submit,
**Then** I receive HTTP 403 (FR-15/AD-6).
