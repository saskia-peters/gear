---
name: G.E.A.R.
description: Visual identity for the G.E.A.R. — a regulated, safety-critical equipment-inspection web app for Ortsverband Singen.
status: final
created: '2026-08-30'
updated: '2026-08-31'
sources:
  - _bmad-output/planning-artifacts/prds/prd-app-ovsingen-2026-08-29/prd.md
  - _bmad-output/planning-artifacts/prds/prd-app-ovsingen-2026-08-29/addendum.md
  - _bmad-output/planning-artifacts/architecture/architecture-app-ovsingen-2026-08-30/ARCHITECTURE-SPINE.md
  - docs/docs/management-overview.md
  - docs/src/css/custom.css
  - _bmad-output/planning-artifacts/ux-designs/ux-app-ovsingen-2026-08-30/.memlog.md
  - _bmad-output/planning-artifacts/ux-designs/ux-app-ovsingen-2026-08-30/wireframes/
colors:
  # Brand — GEAR corporate palette inherited from docs site custom.css
  brand-blue: '#003399'
  brand-blue-dark: '#002d88'
  brand-navy: '#001a66'
  brand-navy-deep: '#00124d'
  brand-orange: '#f5821f'
  brand-blue-darkmode: '#7aa2ff'
  brand-navy-darkmode: '#2a3f8f'
  brand-navy-deep-darkmode: '#0d1b4d'
  # Status — traffic-light from management-overview.md
  status-green: '#2e7d32'
  status-green-dark: '#4caf50'
  status-orange: '#f5821f'
  status-orange-dark: '#ffa726'
  status-red: '#c62828'
  status-red-dark: '#ef5350'
  status-oos: '#1c1c1c'
  status-oos-dark: '#424242'
  # Surface — light mode
  surface-base: '#F5F6F8'
  surface-raised: '#FFFFFF'
  surface-overlay: '#FFFFFF'
  # Surface — dark mode
  surface-base-dark: '#0F1729'
  surface-raised-dark: '#1A2340'
  surface-overlay-dark: '#1A2340'
  # Ink / text — light mode
  ink-primary: '#1A1D23'
  ink-secondary: '#5A6070'
  ink-disabled: '#B0B5BF'
  ink-on-brand: '#FFFFFF'
  ink-on-orange: '#1A1D23'
  # Ink / text — dark mode
  ink-primary-dark: '#E8EAF0'
  ink-secondary-dark: '#9BA0B0'
  ink-disabled-dark: '#4A5060'
  ink-on-brand-dark: '#0A0F1A'
  ink-on-orange-dark: '#1A1D23'
  # Border
  border-hairline: '#DDE0E6'
  border-hairline-dark: '#2A3050'
typography:
  display:
    fontFamily: 'Inter, system-ui, sans-serif'
    fontSize: 28px
    fontWeight: '700'
    lineHeight: '1.2'
    letterSpacing: '-0.01em'
  title:
    fontFamily: 'Inter, system-ui, sans-serif'
    fontSize: 20px
    fontWeight: '600'
    lineHeight: '1.3'
  body:
    fontFamily: 'Inter, system-ui, sans-serif'
    fontSize: 16px
    fontWeight: '400'
    lineHeight: '1.5'
  meta:
    fontFamily: 'Inter, system-ui, sans-serif'
    fontSize: 13px
    fontWeight: '400'
    lineHeight: '1.4'
  label:
    fontFamily: 'Inter, system-ui, sans-serif'
    fontSize: 14px
    fontWeight: '500'
    lineHeight: '1.4'
rounded:
  sm: 4px
  md: 8px
  lg: 12px
  xl: 16px
  full: 9999px
  DEFAULT: 8px
spacing:
  '1': 4px
  '2': 8px
  '3': 12px
  '4': 16px
  '5': 20px
  '6': 24px
  '8': 32px
  '10': 40px
  '12': 48px
  gutter: 16px
  margin-mobile: 16px
components:
  button-primary:
    background: '{colors.brand-blue}'
    foreground: '{colors.ink-on-brand}'
    radius: '{rounded.md}'
    typography: '{typography.label}'
  button-primary-dark:
    background: '{colors.brand-blue-darkmode}'
    foreground: '{colors.ink-on-brand-dark}'
    radius: '{rounded.md}'
    typography: '{typography.label}'
  button-danger:
    background: '{colors.status-red}'
    foreground: '#FFFFFF'
    radius: '{rounded.md}'
    typography: '{typography.label}'
  button-danger-dark:
    background: '{colors.status-red-dark}'
    foreground: '#FFFFFF'
    radius: '{rounded.md}'
    typography: '{typography.label}'
  status-chip-green:
    background: '{colors.status-green}'
    foreground: '#FFFFFF'
    radius: '{rounded.full}'
    typography: '{typography.meta}'
  status-chip-orange:
    background: '{colors.status-orange}'
    foreground: '{colors.ink-on-orange}'
    radius: '{rounded.full}'
    typography: '{typography.meta}'
  status-chip-red:
    background: '{colors.status-red}'
    foreground: '#FFFFFF'
    radius: '{rounded.full}'
    typography: '{typography.meta}'
  status-chip-oos:
    background: '{colors.status-oos}'
    foreground: '#FFFFFF'
    radius: '{rounded.full}'
    typography: '{typography.meta}'
  status-chip-green-dark:
    background: '{colors.status-green-dark}'
    foreground: '#FFFFFF'
    radius: '{rounded.full}'
    typography: '{typography.meta}'
  status-chip-orange-dark:
    background: '{colors.status-orange-dark}'
    foreground: '{colors.ink-on-orange-dark}'
    radius: '{rounded.full}'
    typography: '{typography.meta}'
  status-chip-red-dark:
    background: '{colors.status-red-dark}'
    foreground: '#FFFFFF'
    radius: '{rounded.full}'
    typography: '{typography.meta}'
  status-chip-oos-dark:
    background: '{colors.status-oos-dark}'
    foreground: '#FFFFFF'
    radius: '{rounded.full}'
    typography: '{typography.meta}'
  pass-chip:
    background: '{colors.status-green}'
    foreground: '#FFFFFF'
    radius: '{rounded.md}'
    typography: '{typography.body}'
  pass-chip-dark:
    background: '{colors.status-green-dark}'
    foreground: '#FFFFFF'
    radius: '{rounded.md}'
    typography: '{typography.body}'
  fail-chip:
    background: '{colors.status-red}'
    foreground: '#FFFFFF'
    radius: '{rounded.md}'
    typography: '{typography.body}'
  fail-chip-dark:
    background: '{colors.status-red-dark}'
    foreground: '#FFFFFF'
    radius: '{rounded.md}'
    typography: '{typography.body}'
  input-field:
    background: '{colors.surface-raised}'
    border: '{colors.border-hairline}'
    foreground: '{colors.ink-primary}'
    radius: '{rounded.sm}'
    typography: '{typography.body}'
  input-field-dark:
    background: '{colors.surface-raised-dark}'
    border: '{colors.border-hairline-dark}'
    foreground: '{colors.ink-primary-dark}'
    radius: '{rounded.sm}'
    typography: '{typography.body}'
  filter-chip:
    background: '{colors.surface-base}'
    border: '{colors.brand-blue}'
    foreground: '{colors.brand-blue}'
    radius: '{rounded.full}'
    typography: '{typography.meta}'
  filter-chip-active:
    background: '{colors.brand-blue}'
    foreground: '{colors.ink-on-brand}'
    radius: '{rounded.full}'
    typography: '{typography.meta}'
  filter-chip-dark:
    background: '{colors.surface-base-dark}'
    border: '{colors.brand-blue-darkmode}'
    foreground: '{colors.brand-blue-darkmode}'
    radius: '{rounded.full}'
    typography: '{typography.meta}'
  filter-chip-active-dark:
    background: '{colors.brand-blue-darkmode}'
    foreground: '{colors.ink-on-brand-dark}'
    radius: '{rounded.full}'
    typography: '{typography.meta}'
  summary-count:
    typography: '{typography.display}'
    radius: '{rounded.md}'
  card:
    background: '{colors.surface-raised}'
    radius: '{rounded.md}'
  card-dark:
    background: '{colors.surface-raised-dark}'
    radius: '{rounded.md}'
  toggle:
    radius: '{rounded.full}'
---

## Brand & Style

G.E.A.R. is a workbench. The visual posture is corporate GEAR — the authoritative blue and signal orange of a German federal relief organization — applied to a digital tool that must feel fast, glanceable, and free of ceremony. There is no decorative ambition here. Every pixel earns its place by helping a volunteer or administrator complete a safety-critical task and move on.

The inherited brand is already established on the docs site: GEAR-Blau (`{colors.brand-blue}`), navy (`{colors.brand-navy}`, `{colors.brand-navy-deep}`), and signal orange (`{colors.brand-orange}`). These carry over directly. The app adds a status traffic-light vocabulary — green, orange, red, out-of-service — derived from the inspection cycle, not chosen for aesthetics. Dark mode ships in V1, mirroring the docs site's token strategy with lightened blues (`{colors.brand-blue-darkmode}`, `{colors.brand-navy-darkmode}`, `{colors.brand-navy-deep-darkmode}`) and elevated surface tones.

The app is regulated and safety-critical. This means the visual language must support accessibility at a floor, not an aspiration: WCAG AA contrast, large touch targets for volunteers in warehouse light or reading glasses, and microcopy precision in German — no ambiguous labels, no decorative icons replacing words. The interface reads like a checklist, not a marketing page.

## Colors

The palette is corporate GEAR plus a traffic-light status system. No additional hues are invented.

### Brand

- **GEAR-Blau (`{colors.brand-blue}`)** is the primary brand color, the dominant interaction color, and the nav background. Every primary button, active filter chip, and focused element uses this blue. Dark mode shifts to `{colors.brand-blue-darkmode}` for legibility.
- **Navy (`{colors.brand-navy}`)** appears on the navbar and footer background in light mode. `{colors.brand-navy-deep}` is the deepest navy, used for footer and hero gradient endpoints. Dark mode uses `{colors.brand-navy-darkmode}` and `{colors.brand-navy-deep-darkmode}` respectively.
- **Orange (`{colors.brand-orange}`)** is the signal accent. It appears on primary button text in wireframe style, on the orange status chip, and sparingly on interactive highlights. Never used for large fills; it's a signal, not a background.

### Status

- **Green (`{colors.status-green}`)** means inspected, on time, safe to use. Appears on the dashboard summary count chip, tool list status badges, and the post-inspection confirmation. Dark mode variant: `{colors.status-green-dark}`.
- **Orange (`{colors.status-orange}`)** means inspection due within 14 days. Same usage patterns as green. Dark mode variant: `{colors.status-orange-dark}`.
- **Red (`{colors.status-red}`)** means overdue or inspection-failed. Also used on danger/destructive action buttons (e.g., DSGVO deletion, lockout). Dark mode variant: `{colors.status-red-dark}`.
- **Out of Service (`{colors.status-oos}`)** means failed and locked — awaiting reinstatement. Darkest neutral in the palette. Dark mode variant: `{colors.status-oos-dark}`.

### Surface

- **Surface Base (`{colors.surface-base}`)** is the app canvas — a light neutral gray. `{colors.surface-base-dark}` is its dark-mode counterpart.
- **Surface Raised (`{colors.surface-raised}`)** lifts cards, dropdowns, and modals above the base. `{colors.surface-raised-dark}` is the dark-mode version.
- **Surface Overlay (`{colors.surface-overlay}`)** is reserved for modals and floating panels. Identical to raised in V1; the token exists to support future elevation semantics.

### Ink / Text

- **Ink Primary (`{colors.ink-primary}`)** is body text and headings. High contrast against both surface-base and surface-raised. `{colors.ink-primary-dark}` inverts for dark mode.
- **Ink Secondary (`{colors.ink-secondary}`)** is meta text, timestamps, and secondary labels. `{colors.ink-secondary-dark}` in dark mode.
- **Ink Disabled (`{colors.ink-disabled}`)** is inactive controls and placeholder text. `{colors.ink-disabled-dark}` in dark mode.
- **Ink on Brand (`{colors.ink-on-brand}`)** is white text on blue-filled buttons and chips. `{colors.ink-on-brand-dark}` adapts for dark-mode blue fills.
- **Ink on Orange (`{colors.ink-on-orange}`)** is dark text on orange backgrounds — used on orange status chips where white text would fail contrast. `{colors.ink-on-orange-dark}` for dark mode.

### Border

- **Border Hairline (`{colors.border-hairline}`)** separates list items, underlines input fields, and outlines cards at the lowest visible contrast. `{colors.border-hairline-dark}` in dark mode.

Avoid: additional chromatic hues, gradient surfaces, decorative fills. The palette is brand-plus-status and stops.

## Typography

A custom web type ramp using Inter (or system-ui fallback). Five roles, no ornamental faces. The font choice prioritizes legibility at a glance — volunteers read these screens in warehouse conditions, often with reading glasses.

- **Display (`{typography.display}`)** is 28px/700 bold for dashboard summary counts and page-level hero moments. It appears sparingly — the four status numbers on the dashboard, a confirmation heading. Not a general heading.
- **Title (`{typography.title}`)** is 20px/600 semibold for section headings, screen titles, and tool names in the list. The primary structural type.
- **Body (`{typography.body}`)** is 16px/400 regular for all running text, checklist item labels, form descriptions, and inspection notes. The default reading face.
- **Meta (`{typography.meta}`)** is 13px/400 regular for timestamps, status badges, filter chip labels, secondary labels, and table metadata. Small but always legible at AA contrast.
- **Label (`{typography.label}`)** is 14px/500 medium for button text, input field labels, navigation items, and interactive affordances. Slightly heavier than body to signal interactivity.

All type roles honor dynamic scaling. The inspection checklist — the most safety-critical screen — must remain fully readable at 200% browser zoom without layout breakage. German compound nouns (e.g., "Werkzeugtyp-Bearbeitung") are the primary content; the type ramp accommodates long labels without truncation.

## Layout & Spacing

Scale: 4 / 8 / 12 / 16 / 20 / 24 / 32 / 40 / 48 px. The 4-base grid keeps elements aligned on a predictable rhythm. Named tokens `{spacing.gutter}` (16px) and `{spacing.margin-mobile}` (16px) establish the default horizontal margins.

The app is a responsive web application, mobile-first. The phone-frame wireframes represent the narrowest viewport; the same surfaces stretch to tablet and desktop widths. Single-column on mobile; the dashboard and admin list surfaces may adopt a two-column layout at wider viewports, but the inspection checklist always remains single-column — safety-critical input must not be split across columns.

Mobile margins: 16px (`{spacing.margin-mobile}`) on all sides. The inspection checklist uses tighter vertical spacing (8px between items) to fit all items on one screen without scrolling when possible. Dashboard summary counts use 24px (`{spacing.6}`) gaps between the four status chips.

Breakpoints follow standard web conventions: mobile-first with `sm` (640px), `md` (768px), `lg` (1024px) thresholds. The admin module sidebar navigation collapses to a hamburger or bottom nav on mobile.

## Elevation & Depth

Minimal. Surfaces are distinguished by tonal layering — `surface-base` versus `surface-raised` — not by shadow. This is a workbench, not a layered material design.

- **Cards and tool list rows** sit on `surface-raised` against `surface-base`. No shadow in the default state; the tonal difference is sufficient.
- **Modals and confirmation dialogs** (DSGVO deletion, lockout, CSV import results) use `surface-overlay` with a single soft shadow: `0 4px 12px rgba(0,0,0,0.15)` in light mode, `0 4px 12px rgba(0,0,0,0.4)` in dark mode.
- **Filter chips and summary counts** float on the base surface with no shadow; they are flat interactive elements, not elevated cards.

No elevation hierarchy. No z-index stacking games. Depth comes from layout position and tonal contrast.

## Shapes

Corners are functional, not decorative.

- **`{rounded.sm}` (4px)** on input fields, small inline badges, and the underline-style text inputs. Tight radius reads "form field" — the volunteer knows this is something to fill in.
- **`{rounded.md}` (8px)** on cards, buttons, pass/fail chips, and dialog panels. The default corner radius for interactive containers.
- **`{rounded.lg}` (12px)** on the dashboard summary count cards and larger informational panels. Slightly softer for grouped content.
- **`{rounded.xl}` (16px)** on full-width confirmation panels (post-inspection success, lockout message). Reserved for surfaces that span the viewport width.
- **`{rounded.full}` (9999px)** on status chips, filter chips, and toggle switches. Fully rounded = badge or pill. This is the only context for pill shapes.

No rounded-none. No sharp corners. The floor is `{rounded.sm}`. The aesthetic is "clean industrial form" — readable, not playful.

## Components

- **Button (primary)** — GEAR-Blau fill (`{colors.brand-blue}`), white text (`{colors.ink-on-brand}`), `{rounded.md}` corners, `{typography.label}` type. Used on the main CTA of every surface: "Anmelden", "Prüfung speichern", "Registrieren", "Genehmigen & Zugang setzen". In dark mode: `{colors.brand-blue-darkmode}` fill, `{colors.ink-on-brand-dark}` text.
- **Button (danger/destructive)** — Status-red fill (`{colors.status-red}`), white text, `{rounded.md}`. Used exclusively on irreversible or high-stakes actions: "Endgültig löschen" (DSGVO), "Sperren & protokollieren" (defect submission). Never used for routine confirmation. Dark mode: `{colors.status-red-dark}`.
- **Status chip (green/orange/red/OOS)** — Pill-shaped (`{rounded.full}`), `{typography.meta}` text. Background and foreground map to the corresponding `{colors.status-*}` tokens. Appears on the dashboard summary count block and inline next to tool names. Each status has a dedicated token pair; no generic "status" component — the specific color IS the component.
- **Pass/Fail chip** — `{rounded.md}` corners, `{typography.body}` type. Green background for "OK ✓" / "BESTANDEN"; red background for "FEHLER" / "NICHT BESTANDEN". These are large, tappable, single-screen inspection inputs — at least 48px touch target. Appear in pairs on each checklist item or as a single toggle on pass/fail tools. Active state: full saturation; inactive: `{colors.surface-base}` background with border.
- **Input field** — Underlined style: `{colors.surface-raised}` background, `{colors.border-hairline}` bottom border (no full border box), `{typography.body}` text. Labels float above or sit as `{typography.label}` placeholders. Focused state: `{colors.brand-blue}` underline. Error state: `{colors.status-red}` underline + error text in `{colors.status-red}`. Dark mode: `{colors.surface-raised-dark}` background, `{colors.border-hairline-dark}` border.
- **Filter chip** — `{rounded.full}` pill, `{typography.meta}` text. Inactive: `{colors.surface-base}` background, `{colors.brand-blue}` border and text. Active/filled: `{colors.brand-blue}` background, white text. Used on the dashboard for "Fällig jetzt", "≤14 Tage", "OOS" one-tap filters. Dark mode: `{colors.brand-blue-darkmode}` for active fill and inactive border.
- **Summary count** — `{typography.display}` numbers, `{rounded.md}` container. Four instances on the dashboard: one per status color. Background maps to `{colors.status-*}`, text is white. Each count is a glanceable number inside a colored block — the first thing a volunteer sees.
- **Card** — `{colors.surface-raised}` fill, `{rounded.md}` corners, no shadow by default. Used for tool list rows, admin list items (Benutzer, Rollen, Qualifikationen), and informational panels. Hover/focus state: subtle `{colors.border-hairline}` outline. Dark mode: `{colors.surface-raised-dark}`.
- **Toggle / Switch** — `{rounded.full}` track, `{rounded.full}` thumb. Used for the BESTANDEN / NICHT BESTANDEN pass/fail toggle on pass/fail inspection tools. Active-left (BESTANDEN): `{colors.status-green}` track. Active-right (NICHT BESTANDEN): `{colors.status-red}` track. Neutral: `{colors.ink-disabled}`.

## Do's and Don'ts

| Do | Don't |
|---|---|
| Use `{colors.brand-blue}` for all primary actions — one blue, one CTA per surface | Invent new brand hues or chromatic accents beyond blue + orange + status |
| Use the specific status chip token pair for each status (green/orange/red/OOS) | Use a generic colored text or icon without a chip for status indication |
| Keep the inspection checklist single-column and on one screen | Split checklist items across scrollable pages or multi-column layouts |
| Use `{typography.label}` for all button text — consistent, readable, 14px medium | Set button text in display or title sizes |
| Use `{rounded.full}` only for pills (chips, badges, toggles) | Use pill shapes for buttons, cards, or input fields |
| Support 200% browser zoom without layout breakage on every surface | Design fixed-width layouts that clip German compound nouns |
| Use German microcopy that names the specific tool and status (e.g., "Kettensäge SG-01 → GRÜN") | Use vague confirmations ("Gespeichert" alone, without the tool name and resulting status) |
| Ship light and dark mode with `{colors.*-dark}` token pairs in V1 | Ship only light mode and defer dark mode |
| Use `{colors.status-red}` exclusively for danger/destructive actions and overdue/OOS status | Use red for warnings, informational highlights, or non-destructive emphasis |
| Keep admin module visually separated — isolated, protected, 403 for non-admins | Surface admin actions or data in non-admin surfaces |
