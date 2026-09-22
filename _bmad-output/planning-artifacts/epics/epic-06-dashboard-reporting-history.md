## Epic 6: Dashboard, Reporting & History

Users see the current operational truth, and leadership reviews and exports it. All authenticated users view a color-coded status dashboard (Red/Orange/Green, derived) filterable by status; Fuehrung/Admin export the current filtered view as PDF and inspect full per-tool inspection history.

### Story 6.1: Color-Coded Status Dashboard

As any authenticated user,
I want to see every tool color-coded by inspection status on the dashboard,
So that I know at a glance what is safe, due, and overdue.

**Acceptance Criteria:**

**Given** I have at least `dashboard.view`,
**When** I open the dashboard (Story 1.2 foundation),
**Then** every tool is listed color-coded: Red = past due or Out of Service, Orange = due within the next 14 calendar days, Green = current (due > 14 days) (FR-16/AD-5) — with `Keine Werkzeuge vorhanden` still the empty state (UX-DR6).

**Given** the derived statuses,
**When** the dashboard renders,
**Then** status is computed on read via the single shared clock/status function (AD-4/AD-5), never stored, and a never-inspected tool is Red (AD-4).

**Given** the summary counts,
**When** I see the 2×2 summary grid,
**Then** the count blocks show totals per status and are tappable — activating the matching filter (FR-16/UX-DR5).

**Given** the filter chips,
**When** I tap a status chip,
**Then** the visible list is filtered to that status, with multi-active filter support; the filters reflect the current derived state (FR-16/UX-DR5).

**Given** a tool is inspected or reinstated,
**When** I return to the dashboard,
**Then** the status, counts, and colors are refreshed (FR-16/AD-4/UX-DR6).

### Story 6.2: Status Report Export (PDF)

As a Fuehrung/Admin,
I want to export the current filtered tool list as a PDF,
So that I can share an accurate status report for audits or readiness reviews.

**Acceptance Criteria:**

**Given** I have the `report.export` permission,
**When** I open the dashboard with any active status filters,
**Then** I can export the **currently visible/filtered** tool list as PDF (FR-17/UX-DR7) — the export reflects the same filters and derived statuses currently shown (FR-17/AD-5).

**Given** I export,
**When** the PDF is generated,
**Then** it includes for each tool: name, tool type, current status, last inspection date, and last inspector (FR-17).

**Given** I lack the `report.export` permission,
**When** I attempt to export,
**Then** I receive HTTP 403 and no data is exposed (AD-6)
**And** the export control is hidden from my UI (AD-6/UX-DR6).

### Story 6.3: Inspection History per Tool

As a Fuehrung/Admin,
I want to view the full inspection history of any tool,
So that I can audit a tool's serviceability over time.

**Acceptance Criteria:**

**Given** I have the `inspection.history.view` permission,
**When** I open a tool's detail,
**Then** I see the full inspection history for that tool, reverse-chronological (newest first) (FR-18).

**Given** each listed inspection,
**When** I view it,
**Then** it shows the inspector (or `Deleted User` for anonymized accounts from Story 3.4), timestamp, overall result, inspection mode (pass/fail or checklist), and per-checklist-item results where applicable (FR-18/FR-12, Story 3.4).

**Given** I lack the `inspection.history.view` permission,
**When** I attempt access,
**Then** I receive HTTP 403 and no history is exposed (AD-6)
**And** the history entry point is hidden from my UI (AD-6/UX-DR6).
