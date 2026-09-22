---
name: G.E.A.R. V1
type: ux-design
purpose: design-and-experience
altitude: feature
scope: G.E.A.R. V1 ecosystem — visual identity, information architecture, interaction & accessibility
status: final
created: '2026-08-30'
updated: '2026-08-31'
binds: [Epic 1 (Account & Authentication), Epic 2 (Permissions & Administration), Epic 3 (System Configuration), Epic 4 (Equipment Catalogue), Epic 5 (Inspection), Epic 6 (Dashboard & Reporting)]
sources: [_bmad-output/planning-artifacts/ux-designs/ux-app-ovsingen-2026-08-30/DESIGN.md, _bmad-output/planning-artifacts/ux-designs/ux-app-ovsingen-2026-08-30/EXPERIENCE.md]
companions: []
---

# UX Design — G.E.A.R. V1

An interactive, mobile-first responsive web app for equipment inspection. Corporate G.E.A.R. blue/orange, a status traffic-light, German microcopy, and a WCAG AA accessibility floor. The two governing contracts are `DESIGN.md` (visual identity) and `EXPERIENCE.md` (behavior, IA, accessibility). The wireframes below are the composition references; **the spines win on any conflict with a mockup**.

## Design Language

- **Personality:** workbench — fast, glanceable, minimal ceremony. Dashboard → act → done.
- **Brand:** GEAR-Blau `#003399`, navy `#001a66`/`#00124d`, orange `#f5821f` (mirrors the docs site tokens), light + dark mode in V1.
- **Status traffic-light:** Green `#2e7d32` (safe/in-time), Orange `#f5821f` (due ≤14 days), Red `#c62828` (overdue/failed), OOS `#1c1c1c` (out of service).
- **Type:** Inter/system-ui ramp — display 28/700, title 20/600, body 16/400, meta 13/400, label 14/500.
- **Language:** UI in German; docs stay English.
- **Accessibility floor:** WCAG AA, touch targets ≥48px, focus traversal, screen-reader announcements, 200% zoom without breakage.

## Information Architecture

The IA closes at **33 surfaces** across three modules plus the embedded Werkzeugverwaltung:

- **Öffentlich/Auth (11):** Anmelden, 2-Faktor, Sperre, Passwort vergessen, Bestätigung, Neues Passwort, Registrierung, Bestätigung (Reg.), Antrag eingereicht, Passwort ändern, Dual-Admin-Wiederherstellung.
- **Geräte/Protokoll (10):** Dashboard, Prüfung (Checkliste), Defekt-Erfassung, Gespeichert, Prüfung (Pass/Fail), Tool detail, Verlauf, Reaktivierung, PDF-Bericht, Profil/Einstellungen.
- **Admin-Modul (9):** Verwaltung — Start, Einstellungen, DSGVO — Löschen, Rollen, Rolle bearbeiten, Benutzer, Benutzer-Detail, Qualifikationen, Zuordnung.
- **Werkzeugverwaltung (3):** Werkzeuge (Tabs), Werkzeugtyp bearbeiten, CSV-Import — Ergebnis.

## Wireframes

> Wireframe sources live in `_bmad-output/planning-artifacts/ux-designs/ux-app-ovsingen-2026-08-30/wireframes/` (Excalidraw).

### Overview — Information Architecture (IA)

![IA overview](/img/ia-2026-08-30.png)

The three modules grouped across the app: Öffentlich/Auth, Geräte/Protokoll, and Admin-Modul. Left to right: Anmelden → Registrierung → Dashboard → Prüfung → Verwaltung → Einstellungen.

### Module 1 — Öffentlich / Auth

Auth stack, password reset, dual-admin recovery, and self-registration flows.

![Auth & password flow](/img/flow-auth-password-2026-08-30.png)

Login (email + password), TOTP 2-factor, lockout (HTTP 429), password reset (anti-enumeration), and change-password (revokes other sessions).

![Registration flow](/img/flow-registration-2026-08-30.png)

Self-registration capturing Vorname, Nachname, E-Mail, Passwort / Wiederholung — creates a `pending_approval` account (FR-5).

![Dual-admin recovery](/img/flow-dual-admin-recovery-2026-08-30.png)

Dual-control admin credential recovery (FR-27): self-reset blocked, second admin approves with mandatory Begründung + audit entry; last-admin edge case is out-of-band.

### Module 2 — Geräte / Protokoll

The volunteer device-inspection surface.

![Mobile inspection](/img/flow-mobile-inspection-2026-08-30.png)

Dashboard counts + filter chips → checklist-mode inspection (single-screen pass/fail chips, "Alle bestanden") → defect capture → confirmed "Gespeichert" with auto-return.

![Pass/fail inspection & profile](/img/flow-passfail-profile-2026-08-30.png)

Pass/fail-mode inspection (BESTANDEN / NICHT BESTANDEN toggle, optional note) and the profile/settings surface.

![Führung reinstate, history & export](/img/flow-fuehrende-reinstate-history-export-2026-08-30.png)

Dashboard with summary counts, per-tool history, OOS reinstatement (mandatory Begründung), and PDF export of the current filtered view.

### Module 3 — Admin-Modul

Administration, configuration, and compliance surfaces.

![Admin](/img/flow-admin-2026-08-30.png)

Admin login, Verwaltung — Start (approve/reject pending users), Einstellungen (SMTP + backup), and the heavy two-step DSGVO deletion.

![Roles, users & qualifications](/img/flow-admin-roles-qualifications-2026-08-30.png)

Additive permission model (roles A), user administration with immediate deactivation (B), and qualification management with immediate revocation (C).

### Werkzeugverwaltung — Schirrmeister

![Schirrmeister catalogue & CSV](/img/flow-schirrmeister-catalogue-2026-08-30.png)

Tool-type/tool tabs, type editor (mode, schedule, qualification, flexible attributes), and CSV import with per-row error reporting.

## Key Flows

Four named user journeys anchor the experience (from `EXPERIENCE.md`), bound to stories in Epics and supporting the PRD user journeys UJ-1..UJ-4:

- **UJ-1 Joshi (Helfer*in, on-site):** filter chips → tool detail → single-screen checklist → "Alle bestanden" → "Prüfung speichern" → inline "✅ Kettensäge SG-01 → GRÜN" confirmation with auto-return.
- **UJ-2 Nico (Führung):** OOS summary count → PDF export (current view) → history → reinstatement with Begründung → clock reset.
- **UJ-3 Saskia (Admin):** approve pending volunteer (group + qualification assignment) → heavy-two-step DSGVO deletion with audit.
- **UJ-4 Torsten (Schirrmeister):** tool-type editor (mode/schedule) → CSV import → per-row error report → fix & re-import.

## Related Documents

- [Product Brief](../product-brief)
- [PRD](../prd)
- [Addendum](../addendum)
- [Architecture Spine](../architecture-spine)
- [Management Overview](../../management-overview)
