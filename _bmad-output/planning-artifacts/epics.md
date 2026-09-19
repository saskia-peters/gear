---
stepsCompleted: ["validate-prerequisites", "design-epics", "create-stories", "final-validation"]
inputDocuments:
  - _bmad-output/planning-artifacts/prds/prd-app-ovsingen-2026-08-29/prd.md
  - _bmad-output/planning-artifacts/prds/prd-app-ovsingen-2026-08-29/addendum.md
  - _bmad-output/planning-artifacts/architecture/architecture-app-ovsingen-2026-08-30/ARCHITECTURE-SPINE.md
  - _bmad-output/planning-artifacts/ux-designs/ux-app-ovsingen-2026-08-30/DESIGN.md
  - _bmad-output/planning-artifacts/ux-designs/ux-app-ovsingen-2026-08-30/EXPERIENCE.md
---

# G.E.A.R. - Epic Breakdown

## Overview

This document provides the complete epic and story breakdown for G.E.A.R., decomposing the requirements from the PRD, UX Design if it exists, and Architecture requirements into implementable stories.

## Requirements Inventory

### Functional Requirements

FR-1: Email-Based Authentication — any user authenticates with registered email + secure password; correct credentials return a secure session token, incorrect return HTTP 401.
FR-2: Password Policy — minimum 10 characters, no character-composition complexity rules; shorter passwords rejected with clear validation error.
FR-3: Progressive Login Lockout — after 3 consecutive failed logins the account is blocked for 30s (HTTP 429); after 4, for 60s.
FR-4: Multi-Factor Authentication (MFA) — optional TOTP via authenticator app; when enabled login requires a valid 6-digit code after password; invalid/expired codes rejected.
FR-5: Self-Registration and Admin Approval — new volunteers self-register; account created in `pending_approval` and blocked from auth; only Admin can approve to `active`.
FR-6: Group & Permission Management — admins assign users to User Groups, map permissions/Permission Groups (e.g. Helfer*in, Fuehrung, Admin) to users/groups; access controlled by resolved active permission set of the logged-in user.
FR-7: Flexible User Attributes — dynamic metadata attributes on user profiles via JSON field without DB migration.
FR-8: Tool Type Management — create/edit Tool Types defining name, default Inspection Schedule (from named schedule catalog FR-30), required Qualification, inspection mode (pass/fail vs checklist); mode persists and controls inspection UI.
FR-9: Tool Management — create/edit individual Tools belonging to a Tool Type, optional per-tool schedule overriding the type default; bulk CSV import with per-row error report without partial silent failures.
FR-10: Flexible Attributes on Tools & Tool Types — arbitrary custom metadata via JSON field without DB migration.
FR-11: Qualification-Gated Inspection — Helfer*in and Fuehrung may submit an inspection only if they hold the Qualification required by the Tool Type; otherwise a clear access error.
FR-12: Checklist Inspection — checklist-mode Tool Types present configured items; all items must be answered before submit; per-item pass/fail persisted and visible in history.
FR-13: Pass/Fail Inspection — pass/fail-mode Tool Types present a simple pass/fail toggle with optional notes field.
FR-14: Out of Service Flagging — any failed item or failed pass/fail result transitions the Tool to Out of Service immediately; failure event records inspector, timestamp, failed item(s), defect notes; OOS shows Red on dashboard.
FR-15: Out of Service Reinstatement — Fuehrung/Admin may clear OOS with mandatory non-empty reason; clock resets (next due = reinstatement + schedule); resulting color derived immediately; reinstatement logged with actor/timestamp/reason; Helfer*in cannot reinstate.
FR-16: Color-Coded Status Dashboard — all authenticated users view every Tool color-coded: Red = past due or OOS, Orange = due within next 14 calendar days, Green = current (due > 14 days); filterable by status.
FR-17: Status Report Export (PDF) — Fuehrung/Admin export PDF of currently visible/filtered tool list incl. tool name, type, status, last inspection date, last inspector.
FR-18: Inspection History — full history per Tool stored and accessible to Fuehrung/Admin; per-inspection: inspector, timestamp, overall result, mode, checklist item results; reverse-chronological display.
FR-19: Admin Module Access Isolation — admin routes return HTTP 403 for non-Admin users; admin module existence hidden (no links/menu entries/UI hints).
FR-20: User Approval Workflow — admins view `pending_approval` users, approve (→ `active`, allows auth) or reject (removes pending record).
FR-21: User & Group Administration — admins create/edit/deactivate user accounts, create/edit User Groups, assign users to groups; deactivated accounts cannot authenticate; removing a user from a group revokes inherited permissions immediately.
FR-22: Qualification Management — admins create Qualifications, assign/revoke on users; assignment immediately grants inspect right, revocation immediately removes it.
FR-23: Tool & Tool Type Configuration — admin exposes FR-8/FR-9 incl. checklist-item definition per Tool Type and CSV import; added/removed checklist items reflect on future inspections, historical records unchanged.
FR-24: DSGVO Compliance Operations — admin generates data-access report (profile fields, login history, qualifications) and executes account deletion; deletion purges personal data, retains inspection history anonymized to "Deleted User", account cannot be re-activated.
FR-25: Change Own Password (Authenticated) — any logged-in user changes own password after confirming current password; new password ≥ 10 chars (FR-2); successful change revokes all other active sessions; audited (NFR-O1); never stored/logged in plaintext.
FR-26: Self-Service Password Reset (Forgot Password) — login offers "Forgot password?" accepting email; if `active` account, sends single transactional email with single-use, 30-min, hashed reset link/token; uniform response for unknown email (no enumeration); multiple requests invalidate earlier tokens; audited.
FR-27: Admin Credential Recovery (Dual-Control) — exactly two pre-seeded admin accounts (out-of-band credentials, never in VCS); locked-out admin cannot self-reset via FR-26 alone; recovery requires second admin's approval (`admin.recovery.approve`), recorded as high-severity immutable audit event; last-admin recovery is out-of-band documented manual procedure with mandatory audit; FR-2 applies.
FR-28: SMTP Configuration (Admin Interface) — admin panel exposes email-settings surface (host, port, security none/STARTTLS/TLS, sender/display name, username); SMTP password encrypted at rest and write-only/masked; saves migrate-backed and apply without redeploy; "send test email" action; failures logged not silent; gated by `admin.settings.email`.
FR-29: Backup Destination Configuration (Admin Interface) — admin panel exposes backup-destination surface (≥1 destination required; mechanism type S3-compatible/FTP·SFTP/local, endpoint/host, bucket/path, credentials, optional schedule); credentials encrypted at rest, write-only/masked; "test connection" action; destinations used by backup job without redeploy; failures logged/surfaced; gated by `admin.settings.backup`.
FR-30: Schedule Catalog Management (Admin Interface) — admin panel exposes schedule-catalog surface (create/edit/archive named schedules: name + repeating interval unit/magnitude); rows carry reserved weekday-set + time-of-day fields nullable/unused in V1 (cron-like) so a future composite engine needs no migration; Tool Types pick default, Tools pick individual schedule by FK; archiving does not delete tool/tool-type history; gated by `schedules.manage`.

### NonFunctional Requirements

NFR-S1: Transport Security — all client-server communication TLS 1.2+; plain HTTP rejected or redirected.
NFR-S2: Authentication Hardening — session tokens cryptographically signed, expire after configurable idle period (default 8h), invalidated server-side on logout.
NFR-S3: Authorization Enforcement — permission checks enforced server-side on every request; frontend visibility controls supplementary only.
NFR-S4: Secrets Management — no credentials/API keys/secrets in source code or VCS; secrets injected via env vars or secrets manager at runtime.
NFR-S5: Dependency Security — third-party dependencies auditable and updated for known CVEs within 30 days of disclosure.
NFR-P1: Response Time — API responses (dashboard load, inspection submission, status updates) within 500ms at 95th percentile at ≤50 concurrent users.
NFR-P2: Mobile Usability — frontend fully functional on iOS Safari and Android Chrome without native app; key workflows usable on 4G.
NFR-R1: Containerization — backend, frontend, database deployable as Docker containers via a single docker-compose configuration.
NFR-R2: Database Migrations — all schema changes via versioned, incremental migration files; forward migrations supported; rollback documented.
NFR-R3: Backup & Restore — configurable automated backups of DB and media/file storage to ≥1 destination; documented and tested restore procedure from initial deployment.
NFR-R4: Availability — 99% monthly target, excluding scheduled maintenance windows.
NFR-M1: Modularity — feature modules decoupled at codebase level; new module integrated without modifying existing module business logic.
NFR-M2: Test Coverage — all FRs have corresponding automated tests (unit and/or integration); CI fails on test failure.
NFR-M3: Linting & Code Style — all code passes linter checks in CI; PRs failing lint not merged.
NFR-M4: Documentation — public API contracts and module integration points documented in the docs site; kept current with each release.
NFR-O1: Structured Logging — backend emits JSON structured logs for auth events, permission denials, inspection submissions, OOS transitions, reinstatements, DSGVO operations.
NFR-O2: Audit Trail — all DSGVO-relevant operations (account deletion, data-access report) produce immutable audit log entry with actor, timestamp, operation type.
NFR-C1: DSGVO / GDPR — data minimization; right to access (FR-24 report) and erasure (FR-24 deletion); compliance first-class.
NFR-PL1: Browser Support — two most recent major versions of Chrome, Firefox, Safari, Edge.
NFR-PL2: Form Factor — responsive web app for mobile, tablet, desktop; no native app for V1.

### Additional Requirements

- Greenfield build; no starter template specified — Epic 1 Story 1 is the cold-start scaffold (structural seed below).
- AD-1: Modular monolith, hexagonally partitioned per module — each module an isolated Go package exposing only port interfaces; adapters (HTTP, DB) wired only at the composition root (`cmd/server`); no cross-module import of adapters/handlers/repositories; no in-memory shared cross-module state.
- AD-2: User Directory & Auth is single owner of identity, users, groups, permissions, sessions, MFA/TOTP, credentials, recovery tokens, account state, qualifications; exposes one `Service` port (identity + granted qualifications + resolved permission set); revocation takes effect immediately per request; approved users seeded with `helfende`.
- AD-3: First-class typed columns for core attributes + one `attributes JSONB` column on Users, Tools, Tool Types; JSONB is the no-migration extension surface; promotion path via golang-migrate + backfill.
- AD-4: Tool status (Red/Orange/Green/OOS) always derived on read (inspection records + due-date math), never stored; OOS derived from latest failed inspection/checklist item not since reinstated; reinstatement sole exit from OOS; never-inspected tool shown Red.
- AD-5: Single shared inspection clock — `next_due = last_successful_inspection + resolved schedule interval` (per-tool `schedule_id` else tool-type default); reinstatement resets clock; thresholds Red=past due/OOS, Orange=≤14 days, Green=>14 days; single shared function all consumers call.
- AD-6: Server-side authorization is only source of truth — every action maps to exactly one permission code; server re-validates on each request (HTTP 403); admin existence hidden in SPA.
- AD-7: Qualification gating — qualification data owned by User module; Tool module obtains required + granted qualifications via auth port; assign/revoke effective immediately.
- AD-8: Cross-module user lifecycle & DSGVO flows — composition-root orchestration calls each owning module through its exports; deletion anonymizes inspection history (`"Deleted User"`); runs in one transaction; immutable audit entry.
- AD-9: OOS reinstatement Fuehrung/Admin only; mandatory reason; sole exit from OOS; clock reset.
- AD-10: Tool/Tool-Type config write path single owner (Tool module) — Admin configures via Tool module's configuration port; per-tool schedule is a first-class FK to `schedules`, never JSONB.
- AD-11: Cross-module schema/reference ownership — one golang-migrate migration set; every table has exactly one owning module; cross-module refs are FKs read via owning module's port; Admin owns exactly 3 config tables (`smtp_settings`, `backup_destinations`, `schedules`).
- AD-12: Action-matched, additive permission model — every action ↔ exactly one permission code; resolved set = additive union of permission-group memberships + direct grants; no deny permissions; roles are named permission groups; base roles `helfende`, `schirrmeister`, `fuehrende`, `admin`; admins create/edit named groups and may edit base roles; flat (no groups-in-groups in V1); user groups are organisational only (grant no access).
- AD-13: Self-service password management + dual-admin recovery — FR-25/26/27 flows in User module through `Service` port; Argon2id hashes; single-use 30-min hashed reset tokens; two seeded admin accounts; `admin.recovery.approve` gated dual-control; last-admin recovery out-of-band manual.
- AD-14: Admin-owned SMTP settings (`smtp_settings`, `admin.settings.email`) — password encrypted at rest, write-only/masked; User email adapter consumes via Admin settings port; test-email action.
- AD-15: Admin-owned backup destinations (`backup_destinations`, `admin.settings.backup`) — ≥1 required; mechanism type, endpoint, bucket/path, encrypted credentials; test-connection action; consumed by backup job via settings port.
- AD-16: Named schedule catalog (`schedules`, `schedules.manage`) — name + interval_unit/interval_magnitude in V1; reserved weekday-set + time-of-day fields nullable/unused; Tool Types/Tools reference by FK via Admin schedule port; lenient validation.
- Stack: Go 1.27, chi v5.3.2 (>=v5.2.4 to avoid GO-2026-4316 open-redirect), sqlc v1.31.1, pgx v5, golang-migrate v4.19.1, PostgreSQL 18, TypeScript 6.x, React 19, Vite 8.x, Docker/podman-compose (single Compose source), just (single command runner), OpenTofu v1.12.6 Google provider 5.x (Cloud Run, Artifact Registry, Cloud SQL; min_instance_count=0), Docusaurus docs.
- Structural seed: `cmd/server/` composition root; `internal/{user,tools,admin,platform}/`; `web/` React+Vite+TS SPA (no business logic); `migrations/`; `deploy/` compose + backup; `infra/` OpenTofu; root `justfile`; `docs/`.
- Database: single migration set, 22 tables (1-11, 19 owned by User; 12-18 by Tool; 20-22 by Admin); UUID v7 PKs; UTC RFC 3339 timestamps; uniform JSON error envelope `{"error":{"code","message","details?"}}`.
- Database tables: NOT created upfront in the scaffold — each story adds its own incremental migration (AD-11) for exactly the tables it needs; the completed single migration set builds the full schema (all 22 tables) in one `just db-up` run on a fresh setup.
- Base permission series (21 codes): `dashboard.view`, `inspection.submit`, `inspection.history.view`, `report.export`, `tool.reinstate`, `tools.manage`, `tool_types.manage`, `users.view`, `users.approve`, `users.manage`, `user_groups.manage`, `roles.create`, `roles.edit`, `roles.assign`, `qualifications.manage`, `dsgvo.access_report`, `dsgvo.delete`, `admin.recovery.approve`, `admin.settings.email`, `admin.settings.backup`, `schedules.manage`.
- Base role matrix (additive): `helfende` = dashboard.view, inspection.submit; `schirrmeister` = + tools.manage, tool_types.manage, inspection.history.view; `fuehrende` = + inspection.history.view, report.export, tool.reinstate; `admin` = all 21 codes.
- Deferred to build epics (not blocking stories): frontend framework internals (router/state/component library) chosen in frontend epic; operational envelope/deploy topology details and CI/CD pipeline provider+workflow files in deploy epic; concrete TOTP and SMTP libraries in User epic; composite schedule engine (weekday/time) deferred to future enhancement (schema fields exist).
- User journeys UJ-1 (Joshi chainsaw inspection), UJ-2 (Nico readiness audit + PDF export), UJ-3 (Saskia approves helper + qualification), UJ-4 (Torsten catalogue maintenance + history check) from PRD §2.3 bind to Epic 2 and Epic 3 stories.

### UX Design Requirements

UX-DR1: Design token foundation — implement the full color system from DESIGN.md tokens (brand: GEAR-blau `#003399`, navy `#001a66`, navy-deep `#00124d`, orange `#f5821f`, plus dark-mode variants; status traffic-light green `#2e7d32`, orange `#f5821f`, red `#c62828`, OOS `#1c1c1c` and dark variants; surface-base/raised/overlay; ink-primary/secondary/disabled/on-brand/on-orange; border-hairline — each with `-dark` pair) as consumable CSS custom properties/tokens consumed by the SPA.
UX-DR2: Typography ramp — five roles (display 28/700, title 20/600, body 16/400, meta 13/400, label 14/500, Inter/system-ui, per DESIGN.md.typography) implemented as reusable tokens; all support dynamic scaling and 200% zoom without layout breakage; German compound nouns must not truncate or clip.
UX-DR3: Spacing / rounded / elevation scales — 4-base spacing scale (4..48px, gutter 16px, margin-mobile 16px), rounded scale (sm 4 / md 8 / lg 12 / xl 16 / full 9999), and tonal elevation (no shadows by default; soft shadow only on modal/confirmation overlay `0 4px 12px`) applied consistently; inspection checklist always single-column.
UX-DR4: Light + dark mode token pairs ship in V1 — every DESIGN.md color/component token has a `-dark` counterpart; theme toggles per DESIGN.md; user can select and system persists it.
UX-DR5: Reusable UI component library — implement as reusable, testable components (per DESIGN.md.Components): (1) Button-primary (GEAR-blau fill, white text), (2) Button-danger (status-red, white text, irreducible/high-stakes actions only), (3) Status chip (green/orange/red/OOS pills, distinct token per color), (4) Pass/Fail chip (large ≥48px tappable, green OK/BESTANDEN vs red FEHLER/NICHT BESTANDEN), (5) Input field (underline style, floating label, blue focus / red error underline), (6) Filter chip (pill, active GEAR-blau fill, one-tap, multi-active), (7) Summary count (display number in colored block, tap activates filter), (8) Card (surface-raised, no shadow) and (9) Toggle (BESTANDEN/NICHT BESTANDEN binary).
UX-DR6: Comprehensive states — implement cold-open (cached + background refresh), empty (`Keine Werkzeuge vorhanden`; `Keine ausstehenden Anträge`), loading (skeleton, no spinner-over-content), submitting (button disabled + action verb, no double-submit), lockout (HTTP 429 `Zu viele Fehlversuche — 30/60 Sekunden warten`), offline/network (inspection data queues locally, retries on reconnect), inline form validation errors (red underline + inline text, never toast-only), 403 (redirect to Dashboard + `Zugriff verweigert`), and post-submit inline confirmation with auto-return to refreshed Dashboard (~2s).
UX-DR7: Interaction primitives — fewest-taps inspection (single-screen checklist, `Alle bestanden` shortcut, one submit), one-submit-one-confirmation (no chained modals), auto-return to Dashboard post-inspection, PDF export current-filtered-view, anti-enumeration on all auth flows (registration, password reset, account recovery — uniform response, never reveal email existence), irreversible DSGVO deletion (typed name + mandatory Begründung + `Endgültig löschen`), dual-admin recovery (second-admin approval with Begründung + checkbox), qualification-gated inspection start (disabled `Prüfung starten` with explanation when qualification absent), one-tap status filter chips.
UX-DR8: German microcopy / Voice & Tone standard — implement German precision microcopy: name-the-entity confirmations/errors (`Kettensäge SG-01 → GRÜN — Nächste Prüfung: +12 Monate`; `NICHT BESTANDEN — Anlasser defekt`; `⛔ Wird als Außer Betrieb gesperrt`; `→ Sofort kein Login`; `Login erst möglich nach Admin-Freigabe`; `Endgültig löschen — Unumkehrbar · Audit-Pflicht`); never vague (`Gespeichert` alone, `E-Mail nicht gefunden`, generic `Fehler aufgetreten`). UI language German; docs English.
UX-DR9: Accessibility floor (regulated) — WCAG AA contrast on all text/chips/controls (incl. traffic-light colors on light and dark), interactive touch targets ≥48px (pass/fail chips may be larger), focus traversal follows reading order, no focus traps except modals (DSGVO/lockout, return focus on close), screen-reader announcements for status changes/form errors/post-submission, no icon-only buttons, keyboard-operable CTAs/filter chips/toggles/fields, Reduce Motion skips auto-return animation.
UX-DR10: Responsive & platform — mobile-first responsive web (NFR-P2/PL2): breakpoints sm 640 / md 768 / lg 1024; mobile single-column, dashboard 2×2 summary grid, admin nav collapses to hamburger/bottom; tablet two-column dashboard + persistent admin sidebar; desktop full sidebar; inspection checklist always single-column at all widths (safety-critical input never split); one codebase, touch-first + desktop; UI language German. 10 wireframes in `ux-app-ovsingen-2026-08-30/wireframes/` are the composition references.

### FR Coverage Map

FR-1: Epic 1 - Email-based authentication (login session token / HTTP 401)
FR-2: Epic 1 - Password policy (min 10 chars, clear validation)
FR-3: Epic 1 - Progressive login lockout (3→30s / 4→60s HTTP 429)
FR-4: Epic 1 - Optional TOTP MFA (6-digit code after password)
FR-5: Epic 1 (self-registration → pending_approval) / Epic 2 (admin approval) - Self-Registration and Admin Approval
FR-6: Epic 2 - Group & permission management (permission groups, resolved active permission set)
FR-7: Epic 1 - Flexible user attributes (JSON metadata on profiles)
FR-8: Epic 4 - Tool type management (name, default schedule, qualification, inspection mode)
FR-9: Epic 4 - Tool management (per-tool schedule override, bulk CSV import with per-row error report)
FR-10: Epic 4 - Flexible attributes on tools & tool types (JSON metadata)
FR-11: Epic 5 - Qualification-gated inspection (access error if no required qualification)
FR-12: Epic 5 - Checklist inspection (all items answered, pass/fail per item persisted)
FR-13: Epic 5 - Pass/Fail inspection (toggle + optional notes)
FR-14: Epic 5 - Out of Service flagging (failed item/result → OOS immediately, failure record)
FR-15: Epic 5 - OOS reinstatement (Fuehrung/Admin only, mandatory reason, clock reset)
FR-16: Epic 6 - Color-coded status dashboard (Red/Orange/Green, filterable)
FR-17: Epic 6 - Status report export PDF (Fuehrung/Admin, current filtered view)
FR-18: Epic 6 - Inspection history (reverse-chronological per tool)
FR-19: Epic 2 - Admin module access isolation (HTTP 403, hidden existence)
FR-20: Epic 2 - User approval workflow (approve → active, reject → remove pending)
FR-21: Epic 2 - User & group administration (create/edit/deactivate, revoke inherited permissions)
FR-22: Epic 2 - Qualification management (create/assign/revoke, immediate effect)
FR-23: Epic 4 - Tool & tool type configuration (checklist items, CSV import, historical records unchanged)
FR-24: Epic 3 - DSGVO compliance operations (data-access report, account deletion, anonymized history)
FR-25: Epic 1 - Change own password (revokes other sessions, audited)
FR-26: Epic 1 - Self-service password reset (single-use 30-min link, anti-enumeration)
FR-27: Epic 1 - Admin credential recovery (dual-control, high-severity audit, last-admin out-of-band)
FR-28: Epic 3 - SMTP configuration (admin interface, encrypted/masked secrets, test email)
FR-29: Epic 3 - Backup destination configuration (admin interface, encrypted credentials, test connection)
FR-30: Epic 4 - Schedule catalog management (named schedules, FK references, archiving preserves history)

## Epic List

### Epic 1: Account & Authentication
**Stories:** [epics/epic-01-account-and-authentication.md](epics/epic-01-account-and-authentication.md)
Users can register, authenticate, and manage their own identity securely. A volunteer self-registers for a `pending_approval` account, logs in with email + password (with optional TOTP MFA and progressive lockout), manages their profile, changes their own password (revoking other sessions), and recovers a forgotten password via a secure email link. Seeded admin accounts are protected by dual-admin credential recovery.
**FRs covered:** FR-1, FR-2, FR-3, FR-4, FR-5 (self-registration), FR-7, FR-25, FR-26, FR-27
**Implementation notes:** Greenfield structural seed (Epic 1 Story 1) scaffolds `cmd/server`, `internal/{user,tools,admin,platform}`, `web/` React+Vite+TS SPA, migrations, deploy, infra, justfile. User module owns identity/auth/permissions/qualifications (AD-2, AD-3, AD-13). Password reset (FR-26) depends on working email delivery; full admin SMTP panel (FR-28) ships in Epic 3 — Epic 1 uses a baseline sender. UX: UX-DR4/5/6/7/8/9 (auth surfaces, MFA, lockout, anti-enumeration microcopy, accessibility).

### Epic 2: Permissions, Roles & Administration Foundation
**Stories:** [epics/epic-02-permissions-roles-administration-foundation.md](epics/epic-02-permissions-roles-administration-foundation.md)
Administrators manage who belongs and what they are allowed to do. Admins approve pending self-registered volunteers, create/edit/deactivate users, manage user groups and roles, assign qualifications, and maintain the additive action-matched permission model. The admin module is isolated and returned HTTP 403 (with hidden existence) for non-admins.
**FRs covered:** FR-5 (admin approval), FR-6, FR-19, FR-20, FR-21, FR-22
**Implementation notes:** User module owns identity/permissions/qualifications (AD-2); additive permission model with base roles helfende/schirrmeister/fuehrende/admin and 21 permission codes (AD-12); server-side authorization is the only source of truth, admin existence hidden in SPA (AD-6). Every later epic's permission gating depends on this. UX: UX-DR5/6/7/8/9 (admin approve/reject surface, user/role/qualification management, 403 handling, German microcopy).

### Epic 3: System Configuration & Compliance
**Stories:** [epics/epic-03-system-configuration-compliance.md](epics/epic-03-system-configuration-compliance.md)
Administrators configure operational infrastructure and satisfy legal compliance. Admins manage SMTP email settings (test email, encrypted/masked secrets) and backup destinations (test connection, encrypted credentials); handle DSGVO operations including data-access reports and irreversible account deletion with anonymized inspection history.
**FRs covered:** FR-24, FR-28, FR-29
**Implementation notes:** Admin module owns exactly 3 config tables — `smtp_settings`, `backup_destinations`, `schedules` (AD-11, AD-14, AD-15); secrets encrypted at rest and write-only/masked (NFR-S4, FR-28/FR-29); DSGVO deletion runs in one transaction with immutable audit entries (AD-8, NFR-O2, FR-24). Placed before catalogue/inspection so operational email/backup and compliance are in place early. UX: UX-DR5/6/7/8/9 (DSGVO heavy two-step delete, settings forms, test actions).

### Epic 4: Equipment Catalogue & Scheduling
**Stories:** [epics/epic-04-equipment-catalogue-scheduling.md](epics/epic-04-equipment-catalogue-scheduling.md)
The Schirrmeister administers the equipment universe. Admins/Schirrmeister define tool types (name, default schedule, required qualification, checklist vs pass/fail mode), individual tools (optional per-tool schedule override), flexible attributes, the named schedule catalog, and bulk CSV import with per-row error reporting.
**FRs covered:** FR-8, FR-9, FR-10, FR-23, FR-30
**Implementation notes:** Tool module owns tool config write path (AD-10); schedule catalog owned by Admin (AD-16); per-tool schedule is a first-class FK (FR-9/AD-10); CSV import (FR-9/FR-23) with per-row error table. Gated by Epic 2 permissions (tools.manage, tool_types.manage, schedules.manage). UX: UX-DR5/6/7/8/9/10 (catalogue tabs, type editor, CSV result screen, responsive).

### Epic 5: Inspection Execution & Serviceability
**Stories:** [epics/epic-05-inspection-execution-serviceability.md](epics/epic-05-inspection-execution-serviceability.md)
Volunteers perform safety-critical inspections and manage availability. Qualified Helfer*in/Fuehrung run checklist-mode or pass/fail-mode inspections; a failed item flips the tool to Out of Service immediately with a full failure record; Fuehrung/Admin reinstate a tool with a mandatory reason, resetting the inspection clock.
**FRs covered:** FR-11, FR-12, FR-13, FR-14, FR-15
**Implementation notes:** Tool module owns inspection + OOS derivation (AD-4, AD-5, AD-7, AD-9); status always derived on read, never stored; qualification-gated start (FR-11/AD-7); OOS reinstate Fuehrung/Admin only with clock reset (FR-15/AD-9). UX: UX-DR5/6/7/8/9/10 (single-screen checklist, pass/fail toggle, defect capture, OOS, post-submit auto-return).

### Epic 6: Dashboard, Reporting & History
**Stories:** [epics/epic-06-dashboard-reporting-history.md](epics/epic-06-dashboard-reporting-history.md)
Users see the current operational truth, and leadership reviews and exports it. All authenticated users view a color-coded status dashboard (Red/Orange/Green, derived) filterable by status; Fuehrung/Admin export the current filtered view as PDF and inspect full per-tool inspection history.
**FRs covered:** FR-16, FR-17, FR-18
**Implementation notes:** Status derived on read via single shared clock (AD-4, AD-5); PDF export reflects active filters (FR-17); history reverse-chronological with per-initiation details (FR-18); gated by dashboard.view, report.export, inspection.history.view. UX: UX-DR5/6/7/8/9/10 (dashboard summary counts + filter chips, PDF export current view, history).

### Epic 7: Hardening & Deploy
**Stories:** [epics/epic-07-hardening-deploy.md](epics/epic-07-hardening-deploy.md)
G.E.A.R. becomes releasable and dependable. The CI pipeline gates every push/PR (lint + tests + docs + dependency audit); the Go binary serves the production SPA; the cold-start seed and the full DB-backed suite are verified by integration tests that run isolated in parallel; the two append-only write paths are hardened with idempotency keys; the app deploys to staging (Google Cloud Run, scaled to zero) and production (self-hosted single host, compose) with TLS, managed secrets, and a tested backup/restore procedure.
**FRs covered:** none new (operational)
**Implementation notes:** The deploy epic/site the spine defers to (NFR-R1–R4, NFR-S1/S4, NFR-M2/M3, AD-13 dual-admin bootstrap). Firm spine choices: single `justfile`; OpenTofu IaC; Cloud Run staging (min_instance_count = 0); production self-hosted compose; edge/CDN = Cloudflare or bunny.net. Pulls the deferred-work items: CI pipeline, production SPA serving, integration/seed assertions, parallel-DB test isolation, idempotency keys, backup/restore.
