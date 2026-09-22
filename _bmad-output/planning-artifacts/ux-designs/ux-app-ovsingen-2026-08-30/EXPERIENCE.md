---
name: G.E.A.R.
status: final
sources:
  - _bmad-output/planning-artifacts/prds/prd-app-ovsingen-2026-08-29/prd.md
  - _bmad-output/planning-artifacts/prds/prd-app-ovsingen-2026-08-29/addendum.md
  - _bmad-output/planning-artifacts/architecture/architecture-app-ovsingen-2026-08-30/ARCHITECTURE-SPINE.md
  - _bmad-output/planning-artifacts/ux-designs/ux-app-ovsingen-2026-08-30/wireframes/
  - docs/docs/management-overview.md
  - _bmad-output/planning-artifacts/ux-designs/ux-app-ovsingen-2026-08-30/.memlog.md
updated: '2026-08-31'
---

# G.E.A.R. — Experience Spine

## Foundation

A responsive, mobile-first web application. No UI system named — custom-built with brand tokens (see `DESIGN.md`). Three self-contained modules: Öffentlich/Auth (public and authentication), Geräte/Protokoll (device inspection), Admin-Modul (protected admin; 403 for non-admins). UI language is German for all labels, errors, and microcopy; documentation remains English.

Light and dark mode ship in V1. The app targets volunteers spanning a wide age range, working in warehouse and field conditions — accessibility is a regulatory floor, not an aspiration. WCAG AA contrast, large touch targets, high-contrast status colors.

The visual language is a workbench: fast, glanceable, minimal ceremony. Dashboard → act → done. No decorative ambition. Every surface exists to complete a safety-critical task and move on.

## Information Architecture

### Öffentlich / Auth (11 surfaces)

| Surface | Reached from | Purpose |
|---|---|---|
| Anmelden | App entry (unauthenticated) | Email + password login form. Links to "Passwort vergessen?" and "Registrieren?" |
| 2-Faktor | Anmelden (credentials valid) | 6-digit code from Authenticator app. "MFA aktiv" indicator. One step after password. |
| Sperre | Anmelden (≥3–4 failed attempts) | Lockout screen. "Zu viele Fehlversuche — 30/60 Sekunden warten." HTTP 429 state. |
| Passwort vergessen | Anmelden ("Passwort vergessen?" link) | Email input, "Link senden" CTA. Anti-enumeration: no indication whether email exists. |
| Bestätigung (Passwort) | Passwort vergessen (submitted) | "Wenn deine E-Mail registriert ist, erhältst du einen Link." No leak. |
| Neues Passwort | Bestätigung (link clicked, <30 min) | New password (min 10 Zeichen) + repeat. "Passwort speichern" CTA. |
| Registrierung | Anmelden ("Registrieren?" link) | Vorname, Nachname, E-Mail, Passwort/Wiederholung (min 10 Zeichen). Self-registration creates pending_approval. |
| Bestätigung (Registrierung) | Registrierung (submitted) | "Wenn deine E-Mail bereits registriert ist, erhältst du eine Bestätigung." Anti-enumeration. |
| Antrag eingereicht | Bestätigung (Registrierung) | "Dein Konto ist in Bearbeitung. Login erst möglich nach Admin-Freigabe." pending_approval state. |
| Passwort ändern | Profil/Einstellungen (authenticated) | Current + new password (min 10) + repeat. "→ Andere Sitzungen beendet" on success. |
| Dual-Admin-Wiederherstellung | Admin A (locked out, self-reset blocked) | Admin B sees recovery request; "Begründung (Pflicht)" + checkbox "☐ Bestätige Freigabe" → "Genehmigen & Zugang setzen" → "→ Hochschwerer Audit-Eintrag." Edge case: last admin → no self-reset, manual out-of-band with audit. |

→ Wireframe references: `flow-auth-password-2026-08-30`, `flow-registration-2026-08-30`, `flow-dual-admin-recovery-2026-08-30`

### Geräte / Protokoll (10 surfaces)

| Surface | Reached from | Purpose |
|---|---|---|
| Dashboard | Anmelden (authenticated) | 4 status summary counts (Grün/Orange/Rot/OOS) + filter chips ("Fällig jetzt", "≤14 Tage", "OOS") + sortable/filterable tool list. Primary landing surface. |
| Prüfung — Checkliste | Dashboard tool row tap (checklist-mode tool) | Single-screen pass/fail chips per checklist item (e.g., Kettenspannung, Zündkerze, Kettenbremse, Schalldämpfer). "Alle bestanden" shortcut + one "Prüfung speichern" submit. |
| Defekt-Erfassung | Prüfung (any item failed) | Mandatory defect description text field. "NICHT BESTANDEN" status. "⛔ Wird als Außer Betrieb gesperrt." "Sperren & protokollieren" CTA. |
| Gespeichert | Prüfung or Defekt-Erfassung (submitted) | Inline green confirmation: "✅ Kettensäge SG-01 → GRÜN" or "✅ Generator G-02 → Außer Betrieb." Next inspection date. Auto-return to refreshed dashboard. |
| Prüfung — Pass/Fail | Dashboard tool row tap (pass/fail-mode tool) | Single BESTANDEN / NICHT BESTANDEN toggle. Optional note field. "Prüfung speichern" CTA. |
| Tool detail | Dashboard tool row tap | Status badge, qualification check ("Chainsaw-Zertifikat ✓"), next inspection date, "Prüfung starten" CTA. |
| Verlauf | Tool detail or Dashboard (history link) | Per-tool inspection history: "12.03 — Tim — bestanden", "Gestern — Joshi — FEHLGESCHLAGEN" with defect note. |
| Reaktivierung | Verlauf (OOS tool, Führende/Admin only) | Mandatory "Begründung (Pflichtfeld)" text. "Wieder in Betrieb nehmen" CTA. "→ Uhr-Reset: heute + Zeitplan." FR-15/AD-9: only Führung/Admin can reinstate. |
| PDF-Bericht | Dashboard (PDF-Export, Führende/Admin) | PDF export respects active filters ("export current view"). Vorschau with Name · Typ · Status · Prüfer columns. "Herunterladen" CTA. |
| Profil / Einstellungen | Dashboard or nav (authenticated) | Groups, qualifications, "Passwort ändern" link. |

→ Wireframe references: `ia-2026-08-30`, `flow-mobile-inspection-2026-08-30`, `flow-passfail-profile-2026-08-30`, `flow-fuehrende-reinstate-history-export-2026-08-30`

### Admin-Modul (9 surfaces)

| Surface | Reached from | Purpose |
|---|---|---|
| Verwaltung — Start | Admin nav (403 for non-admin) | Approve/reject pending self-registered users. "Tim Müller — self-registered." Group and qualification assignment. "Freigeben" / "Ablehnen" CTAs. |
| Einstellungen | Admin nav | SMTP email host/port/Test-E-Mail. Backup targets S3 endpoint/bucket/"Verbindung testen." Schedule catalog (1J/1Q/1Mon/2W/3T). SMTP-Passwort/Backup-Keys encrypted (NFR-S4), masked in UI (FR-28/FR-29). |
| DSGVO — Löschen | Admin nav | Heavy two-step confirm: Schritt 2/2, typed name, mandatory "Begründung," "Endgültig löschen" CTA. "→ Unumkehrbar · Audit-Pflicht." |
| Rollen | Admin nav | Role list: helfende, schirrmeister, fuehrende, admin. "+ Neue Rolle" CTA. Additive permissions model. |
| Rolle bearbeiten | Rollen (role row tap) | Checkbox grid: dashboard.view, inspection.submit, tools.manage, report.export, dsgvo.delete, roles.assign. "Speichern" CTA. Base roles editable; groups flat. |
| Benutzer | Admin nav | User list: Tim Müller (aktiv), Lea Kern (aktiv), Nia Stein (pending), Oli Born (deaktiviert). |
| Benutzer-Detail | Benutzer (user row tap) | Rollen drop-downs, Teams (Gruppe Ost), direkte Berechtigungen. "Deaktivieren" → "→ Sofort kein Login" (FR-21). |
| Qualifikationen | Admin nav | Qualification list: Chainsaw-Zertifikat, Generator-Lizenz. "+ Neue Qualifikation." Entzug hebt Prüfrecht sofort (AD-7). |
| Zuordnung | Qualifikationen (user tap) | Per-user qualification assignment: "Chainsaw-Zertifikat ✕", "Generator-Lizenz ✕." Remove = immediate revocation. |

→ Wireframe references: `flow-admin-2026-08-30`, `flow-admin-roles-qualifications-2026-08-30`

### Werkzeugverwaltung (embedded in Admin, Schirrmeister) (3 surfaces)

| Surface | Reached from | Purpose |
|---|---|---|
| Werkzeuge (Tabs: Werkzeugtypen \| Werkzeuge) | Admin nav or Schirrmeister nav | Tabbed catalogue view. "CSV-Import" + "+ Neu" CTAs. |
| Werkzeugtyp bearbeiten | Werkzeugtypen (type row tap) | Name, Modus (Checkliste / Pass/Fail), Standard-Zeitplan, required Qualifikation, checklist items (Ölstand, Kraftstoff, Start), flexible key/value attributes. "Eigener Zeitplan" override (FR-8/FR-9). |
| CSV-Import — Ergebnis | CSV upload (result screen) | "✅ 87 angelegt / ❌ 3 Zeilen fehlerhaft." Per-row error table: Zeile · Fehlergrund (e.g., "Zeile 12 — ungültige E-Mail", "Zeile 30 — Typ unbekannt"). "Erneut importieren" CTA. |

→ Wireframe reference: `flow-schirrmeister-catalogue-2026-08-30`

**Total: 33 surfaces** across 4 groups (Öffentlich/Auth 11, Geräte/Protokoll 10, Admin-Modul 9, Werkzeugverwaltung 3). The IA is closed — every stated need has a surface.

## Voice and Tone

Microcopy rules. Brand voice and aesthetic posture live in `DESIGN.md`.

| Do | Don't |
|---|---|
| "Kettensäge SG-01 — fällig in 5 Tagen" — name the tool, name the state, name the time | "Gerät fällig" — vague, unnamed, no urgency signal |
| "NICHT BESTANDEN — Anlasser defekt" — precise, two words + defect | "Fehler aufgetreten" — ambiguous, no actionable information |
| "⛔ Wird als Außer Betrieb gesperrt" — consequence stated before the action | "Möchten Sie fortfahren?" — generic confirmation without consequence |
| "✅ Kettensäge SG-01 → GRÜN — Nächste Prüfung: +12 Monate" — full result confirmation | "Gespeichert" — no tool name, no status, no next step |
| "Endgültig löschen — Unumkehrbar · Audit-Pflicht" — irreversible-by-design language | "Löschen bestätigen" — understates the stakes |
| "Wenn deine E-Mail registriert ist, erhältst du einen Link" — anti-enumeration, no indication of email existence | "E-Mail nicht gefunden" — leaks account existence |
| "→ Sofort kein Login" — immediate, plain-language consequence | "Der Benutzer wird deaktiviert" — passive, no urgency |
| "Login erst möglich nach Admin-Freigabe" — dependency stated upfront | "Konto in Bearbeitung" — incomplete, no explanation of what's blocking |

All microcopy is in German. Precision is non-negotiable — this is a regulated, safety-critical system. Every confirmation, error, and status message names the specific entity and the specific consequence.

## Component Patterns

Behavioral. Visual specs live in `DESIGN.md.Components`.

| Component | Use | Behavioral rules |
|---|---|---|
| Button (primary) | Every surface CTA | One primary CTA per surface. GEAR-Blau fill. Label is the action verb (e.g., "Anmelden," "Prüfung speichern," "Registrieren"). |
| Button (danger) | Irreversible actions only | DSGVO deletion, defect lockout. Red fill. Never used for routine confirmation. Requires typed confirmation or mandatory reason before enabling. |
| Status chip | Dashboard counts, inline status | Read-only. Green/orange/red/OOS. Pill-shaped. One chip per status value; no mixing. |
| Pass/Fail chip | Inspection checklist, pass/fail toggle | Large tappable target (≥48px). Green = OK/BESTANDEN, red = FEHLER/NICHT BESTANDEN. "Alle bestanden" shortcut submits all-pass without individual taps. |
| Input field | Forms (login, registration, defect, password) | Underlined style. Focused: blue underline. Error: red underline + red error text. Labels are always visible, never placeholder-only. |
| Filter chip | Dashboard status filters | One-tap filter. Active state fills with GEAR-Blau. Multiple chips can be active simultaneously. Clears on second tap. |
| Summary count | Dashboard top section | Four instances: one per status color. Display-size number inside colored block. Tapping a count activates the corresponding filter. |
| Card | Tool rows, admin list items, panels | Tonal elevation only (no shadow). Tap opens detail. No hover animation on mobile; subtle outline on desktop focus. |
| Toggle | BESTANDEN / NICHT BESTANDEN (pass/fail tools) | Green-left = BESTANDEN, red-right = NICHT BESTANDEN. Neutral = `{colors.ink-disabled}`. One toggle per tool; position is binary. |

## State Patterns

| State | Surface | Treatment |
|---|---|---|
| Cold open (authenticated) | Dashboard | Show cached status counts + tool list immediately. Refresh from server in background. |
| Cold open (unauthenticated) | Anmelden | Login form. No splash screen. Direct to auth. |
| Empty (no tools) | Dashboard | "Keine Werkzeuge vorhanden." Link to Werkzeuge catalogue (Admin/Schirrmeister only). |
| Empty (no pending users) | Verwaltung — Start | "Keine ausstehenden Anträge." No placeholder content. |
| Loading | Any | Skeleton placeholders matching the target layout. No spinner over content. |
| Submitting | Any CTA | Button disables, shows "Speichern…" or action verb. Prevents double-submit. |
| Lockout | Anmelden | "Zu viele Fehlversuche — 30/60 Sekunden warten." HTTP 429 state. No retry button; timer is visual. |
| Offline / network error | Any | "Verbindung unterbrochen — Daten werden gespeichert, wenn die Verbindung wiederhergestellt ist." Inspection data queues locally for retry. |
| Error (form validation) | Any form | Inline red text below the failing field. Red underline on the input. No toast-only errors. |
| 403 (non-admin accessing admin) | Admin-Modul | Redirect to Dashboard. "Zugriff verweigert" toast. No admin surfaces rendered. |
| Post-submission confirmation | Gespeichert | Inline green block: tool name + resulting status + next inspection date. Auto-return to Dashboard after brief display (≈2s). |

## Interaction Primitives

- **Fewest-taps inspection.** Single-screen checklist with "Alle bestanden" shortcut. One submit button. No multi-step wizard for routine inspection.
- **One submit, one confirmation.** Every CTA triggers exactly one backend action. No chained modals. Confirmation is inline (Gespeichert surface), not a separate dialog.
- **Auto-return to Dashboard.** Post-inspection, the app returns to the refreshed Dashboard automatically. The volunteer does not need to navigate back.
- **Export current view.** PDF export respects active dashboard filters. The volunteer exports exactly what they see — no separate export configuration screen.
- **Anti-enumeration on all auth flows.** Registration, password reset, and account recovery never reveal whether an email exists. Same confirmation message regardless of outcome.
- **Irreversible DSGVO deletion.** Heavy two-step: typed name + mandatory Begründung + "Endgültig löschen" CTA. The language states "Unumkehrbar · Audit-Pflicht" before the action fires.
- **Dual-admin recovery.** No single admin can self-reset. The other admin must approve with a Begründung and checkbox confirmation. "→ Hochschwerer Audit-Eintrag" is stated in the flow. Last-admin edge case: manual out-of-band procedure with audit trail.
- **Qualification-gated inspection start.** The app checks the volunteer's qualification before opening the checklist. "Chainsaw-Zertifikat ✓" is shown; if absent, "Prüfung starten" is disabled with an explanation.
- **One-tap status filter.** Dashboard filter chips respond to a single tap. No multi-select dropdown. The filter state is visible and clearable inline.

## Accessibility Floor

Behavioral. Visual contrast lives in `DESIGN.md`.

- WCAG AA contrast on all text, status chips, and interactive elements. The traffic-light status colors pass AA against both white (light mode) and dark (dark mode) backgrounds.
- Touch targets ≥ 48px on all interactive elements — pass/fail chips, filter chips, buttons, list rows. This is the minimum; inspection checklist chips may be larger.
- Focus traversal follows reading order on every surface. No focus traps except modals (DSGVO deletion, lockout), which return focus to the trigger on close.
- Screen reader announcements: status changes (Dashboard counts), form errors (inline), post-submission confirmation ("Gespeichert" text). All interactive elements have explicit labels — no icon-only buttons.
- 200% browser zoom supported without layout breakage on every surface. German compound nouns must not clip or overflow at zoom.
- Reduce Motion: skip auto-return animation on Gespeichert; show confirmation statically, then redirect without transition.
- Keyboard navigation: all CTA buttons, filter chips, toggles, and form fields reachable and operable via keyboard. Tab order follows visual reading order.

## Key Flows

### Flow 1 — Mobile inspection (Joshi, Helfende, midday on-site)

1. Joshi opens the app. Dashboard loads with cached status counts: 12 Grün, 2 Orange, 1 Rot, 1 OOS.
2. He taps the "Fällig jetzt" filter chip — the tool list narrows to "Kettensäge SG-01 — ORANGE — fällig in 5 Tagen."
3. He taps the Kettensäge SG-01 row. Tool detail opens: Status Orange, Nächste Prüfung +5 Tage, Chainsaw-Zertifikat ✓. He taps "Prüfung starten."
4. Checkliste opens single-screen: Kettenspannung (OK ✓ / FEHLER), Zündkerze, Kettenbremse, Schalldämpfer — all big pass/fail chips.
5. He taps OK ✓ on each item. All four pass. He taps "Alle bestanden" shortcut (pre-checks all).
6. He taps "Prüfung speichern."
7. **Climax:** Gespeichert confirmation appears inline: "✅ Kettensäge SG-01 → GRÜN — Nächste Prüfung: +12 Monate." After ≈2s, auto-return to refreshed Dashboard. Kettensäge SG-01 now shows GRÜN.

Failure/empty: any checklist item fails → Defekt-Erfassung surface opens. Mandatory "Beschreibung des Defekts (Pflicht)" text field. "⛔ Wird als Außer Betrieb gesperrt." Joshi types the defect, taps "Sperren & protokollieren." Confirmation: "✅ Kettensäge SG-01 → Außer Betrieb." OOS tool disappears from his filter view (he can't reinstate — FR-15 restriction).

→ Wireframe reference: `flow-mobile-inspection-2026-08-30`

### Flow 2 — Dashboard, export, history, reinstate (Nico, Führende, end of week)

1. Nico opens the app. Dashboard shows: 40 Grün, 6 Orange, 3 Rot, 1 OOS.
2. He taps the OOS summary count. The tool list filters to OOS tools. "Generator G-02 — AUSSER BETRIEB" appears.
3. He taps "PDF-Export." PDF-Bericht opens: Vorschau shows Generatoren with OOS filter applied — Name · Typ · Status · Prüfer columns. He taps "Herunterladen."
4. **Climax (export):** PDF downloads — the current filtered view is exported. No separate export configuration was needed.
5. He taps the Generator G-02 row. Verlauf opens: "Gestern — Joshi — FEHLGESCHLAGEN — Defekt: Anlasser defekt." Earlier entries: "12.03 — Tim — bestanden."
6. He taps "Wieder in Betrieb nehmen." Reaktivierung surface opens: "Begründung (Pflichtfeld)" — he types "Reparatur abgeschlossen." Taps "Bestätigen."
7. **Climax (reinstate):** "→ Uhr-Reset: heute + Zeitplan." Generator G-02 returns to Green status. Dashboard refreshes; OOS count drops to 0.

Failure/empty: no OOS tools → OOS filter returns "Keine Außer-Betrieb-Werkzeuge." No error — the empty state is informational.

→ Wireframe reference: `flow-fuehrende-reinstate-history-export-2026-08-30`

### Flow 3 — Approve pending user and DSGVO deletion (Saskia, Admin, morning admin session)

1. Saskia logs in as admin. Verwaltung — Start opens: "Wartend auf Freigabe: Tim Müller — self-registered."
2. She reviews the pending request. Group: Helfer*in. Qualifikation: Chainsaw-Zertifikat. She taps "Freigeben."
3. **Climax (approval):** Tim Müller moves to aktiv status. He can now log in. The pending queue is empty.
4. Later, she navigates to DSGVO — Löschen. She selects a user to delete. Schritt 2/2 opens.
5. She types the name (typed confirmation), enters "Begründung (Pflicht)" — e.g., "Austritt." The "Endgültig löschen" button is enabled only after both fields are filled.
6. She taps "Endgültig löschen."
7. **Climax (deletion):** "→ Unumkehrbar · Audit-Pflicht." User data is permanently removed. An audit entry is created. The user cannot log in.

Failure/empty: no pending users → "Keine ausstehenden Anträge." DSGVO deletion fails if typed name doesn't match → inline error: "Name stimmt nicht überein." No deletion occurs.

→ Wireframe references: `flow-admin-2026-08-30`, `flow-admin-roles-qualifications-2026-08-30`

### Flow 4 — Catalogue management and CSV import (Torsten, Schirrmeister, setting up new tools)

1. Torsten navigates to Werkzeuge. Tabs: "Werkzeugtypen | Werkzeuge." He taps "Werkzeugtypen."
2. He taps "+ Neu" or edits an existing type. Werkzeugtyp bearbeiten opens: Name, Modus (Checkliste / Pass/Fail), Standard-Zeitplan, required Qualifikation.
3. He selects Modus: Checkliste. Checklist items appear: "Ölstand · Kraftstoff · Start" — he adds or edits items. He sets "Eigener Zeitplan" override if needed (FR-9).
4. He taps "Speichern." The type is saved.
5. He switches to the "Werkzeuge" tab and taps "CSV-Import." He uploads a CSV file.
6. CSV-Import — Ergebnis opens: "✅ 87 angelegt / ❌ 3 Zeilen fehlerhaft." A per-row error table shows: "Zeile 12 — ungültige E-Mail," "Zeile 30 — Typ unbekannt," "Zeile 41 — Datum falsch."
7. **Climax:** Torsten sees exactly which rows failed and why. He taps "Erneut importieren" to fix and re-upload. The 87 successful tools are already in the catalogue.

Failure/empty: CSV is empty or malformed → "Datei enthält keine gültigen Daten" error before upload. CSV with 100% errors → "❌ 0 angelegt" + full error table, no success count.

→ Wireframe reference: `flow-schirrmeister-catalogue-2026-08-30`

## Responsive & Platform

The G.E.A.R. is a responsive web application, mobile-first. Phone-frame wireframes represent the narrowest viewport (≈375px). The same surfaces scale to tablet and desktop.

- **Mobile (≤640px):** Single-column. Dashboard summary counts stack or flow in a 2×2 grid. Tool list full-width. Admin nav collapses to hamburger or bottom navigation. Inspection checklist is single-column, one-screen.
- **Tablet (640–1024px):** Dashboard may show the tool list alongside summary counts in a two-column layout. Admin surfaces gain a persistent sidebar. Inspection checklist remains single-column.
- **Desktop (≥1024px):** Full sidebar navigation on admin. Dashboard shows summary counts + tool list in a wider layout. Two-column possible on admin list surfaces. Inspection checklist still single-column — safety-critical input must never be split.

All surfaces are built for touch-first interaction (volunteers use phones on-site) and desktop use (admin, Schirrmeister). No platform-specific builds — one codebase, responsive breakpoints. Dark mode token pairs (`{colors.*-dark}`) apply at all viewports.
