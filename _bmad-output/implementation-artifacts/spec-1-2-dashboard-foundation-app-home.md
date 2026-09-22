---
title: 'Dashboard Foundation (App Home)'
type: 'feature'
created: '2026-09-05'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'eb61df5936d14954e074c963ff696fb1229ecf8f'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The SPA currently renders a static Vite placeholder with no design tokens, no routing, and no dashboard shell for subsequent authentication, inspection, and administration flows to land on and return to.

**Approach:** Implement the DESIGN.md CSS design token system (colors, light/dark mode pairs, typography ramp, spacing, elevation, radii) and build the responsive Dashboard Home page with the 2×2 summary count grid, status filter chips, empty state ("Keine Werkzeuge vorhanden"), and client routing shell.

## Boundaries & Constraints

**Always:**
- Implement the full DESIGN.md token foundation in CSS custom properties with light + dark mode pairs for brand, status (green, orange, red, oos), surfaces, ink, borders, typography ramp, and spacing (UX-DR1, UX-DR2, UX-DR3).
- Responsive mobile-first layout (UX-DR10): mobile 2×2 summary count grid (`< 640px`), expanding to responsive columns on tablet/desktop (`≥ 640px`); filter chips are one-tap interactive pills (UX-DR5).
- Use German microcopy throughout: "G.E.A.R.", "Übersicht", "Keine Werkzeuge vorhanden", "Alle", "Einsatzbereit", "Ausstehend", "Überfällig", "Außer Betrieb" (UX-DR6/UX-DR8).
- Client routing supporting `/` (dashboard home) and `/login` (placeholder redirect target for subsequent auth stories) (AD-6).
- Minimum touch target size ≥48px on touch devices for interactive chips and buttons; WCAG AA contrast across light and dark modes.
- Support 200% browser zoom without layout breakage or clipping German compound words (UX-DR2).

**Ask First:**
- Client router: use `react-router-dom` v7 (or latest compatible) as standard SPA router. Default is `react-router-dom` unless another routing approach is requested.

**Never:**
- No live tool data or inspection database queries yet (data is empty in Story 1.2; live counts/colors arrive in Epic 6 / FR-16).
- No auth login forms or token validation yet (Story 1.3 self-registration, Story 1.4 login).
- No admin navigation or admin-only views in the main volunteer dashboard shell (AD-2 isolation).
- No hardcoded inline hex values in components — all styling must consume the CSS custom property tokens.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| HAPPY_PATH | Open SPA at `/` | Header with "G.E.A.R.", 2×2 summary count grid (zeros/empty), filter chips with "Alle" active, empty state "Keine Werkzeuge vorhanden" | n/a |
| FILTER_SELECT | Click/tap "Überfällig" filter chip | Chip switches to active/filled state; "Keine Werkzeuge vorhanden" empty state remains visible | n/a |
| DARK_MODE | System prefers dark theme or toggle activated | Canvas flips to `surface-base-dark`, text to `ink-primary-dark`, brand/status tokens use dark-mode variants | n/a |
| UNKNOWN_ROUTE | Navigate to `/unbekannt` | Client router redirects to `/` or renders clean 404 with link back to dashboard | n/a |

</frozen-after-approval>

## Code Map

- `web/src/styles/tokens.css` -- CSS custom properties for all DESIGN.md tokens (brand, status, surface, ink, typography, spacing, rounded, dark mode overrides).
- `web/src/index.css` -- Base stylesheet importing tokens, resetting defaults, applying theme variables to body and root.
- `web/src/components/Header.tsx` -- Top navigation bar with "G.E.A.R." brand title, Ortsverband Singen subtitle/badge, theme toggle.
- `web/src/components/SummaryGrid.tsx` -- Responsive 2×2 mobile / 4-column desktop summary count cards (Einsatzbereit, Ausstehend, Überfällig, Außer Betrieb).
- `web/src/components/FilterChips.tsx` -- One-tap status filter chip row with active state indicators.
- `web/src/components/EmptyState.tsx` -- Accessible empty state card displaying "Keine Werkzeuge vorhanden".
- `web/src/pages/DashboardPage.tsx` -- Main Dashboard layout composing Header, SummaryGrid, FilterChips, and EmptyState.
- `web/src/App.tsx` -- App root component wiring client routing (`/`, `/login`) and theme provider.
- `web/package.json` -- Package configuration including dependencies and npm scripts (`lint`, `test`, `typecheck`, `build`).

## Tasks & Acceptance

**Execution:**
- [x] `web/package.json` -- Add `react-router-dom` and testing dependencies (`vitest`, `@testing-library/react`), plus `lint` and `test` scripts -- foundational SPA libraries and test gate
- [x] `web/src/styles/tokens.css` -- Define complete DESIGN.md CSS variables for light and dark themes (brand, status, surfaces, ink, borders, typography, spacing, radii) -- UX-DR1/2/3 design foundation
- [x] `web/src/index.css` -- Wire token imports and global resets, Inter font stack, root background and text color bindings -- baseline presentation
- [x] `web/src/components/Header.tsx` -- Build topbar with G.E.A.R. branding, Ortsverband Singen identifier, and dark/light mode toggle -- navigation shell
- [x] `web/src/components/SummaryGrid.tsx` -- Build 2×2 mobile / 4-column desktop summary count cards using status color tokens -- UX-DR5/10 glanceable status
- [x] `web/src/components/FilterChips.tsx` -- Build one-tap filter chip pill list with active state styling -- UX-DR5/UX-DR8 filtering
- [x] `web/src/components/EmptyState.tsx` -- Build empty state container with German message "Keine Werkzeuge vorhanden" -- UX-DR6/UX-DR8 empty state
- [x] `web/src/pages/DashboardPage.tsx` -- Assemble responsive dashboard home page combining all components -- App Home foundation
- [x] `web/src/App.tsx` -- Wire `react-router-dom` routes (`/` for DashboardPage, `/login` placeholder) -- client routing shell
- [x] `web/src/App.test.tsx` -- Add unit tests verifying component rendering, filter chip selection, and empty state visibility -- I/O matrix verification
- [x] `justfile` -- Wire `npm --prefix web run lint` and `npm --prefix web run test` into `just lint` and `just test` -- single command entry point quality gates

**Acceptance Criteria:**
- Given the scaffold from Story 1.1, when the SPA is loaded at `/`, then the dashboard home renders with the G.E.A.R. header, responsive 2×2 / 4-column summary count grid, status filter chips, and empty state message "Keine Werkzeuge vorhanden".
- Given the DESIGN.md specification, when rendered in both light and dark modes, then all colors, typography sizes, spacing intervals, and border radiuses adhere to the token system with WCAG AA contrast.
- Given different viewports (mobile <640px, tablet/desktop ≥640px), when resized, then the layout adapts seamlessly without horizontal scrolling or text clipping on German compound nouns.

## Spec Change Log

- 2026-09-05 (Story 1.2 implementation): Delivered complete DESIGN.md token foundation and responsive App Home dashboard shell:
  - `web/src/styles/tokens.css`: complete DESIGN.md tokens (`--gear-color-*`, `--gear-font-*`, `--gear-space-*`, `--gear-radius-*`, `--gear-shadow-*`) with light and dark mode mappings.
  - `web/src/index.css`: global CSS reset, Inter font stack, root token bindings, zoom resilience, German word-break/hyphen handling.
  - `web/src/context/ThemeContext.tsx` & `web/src/context/useTheme.ts`: theme management supporting system preference and persistent user toggling via `data-theme` attribute on document root.
  - `web/src/components/Header.tsx`: topbar with "G.E.A.R." brand, "Ortsverband Singen" badge, and accessible light/dark mode toggle.
  - `web/src/components/SummaryGrid.tsx`: 2×2 mobile / 4-column desktop responsive grid for status counts (Einsatzbereit, Ausstehend, Überfällig, Außer Betrieb).
  - `web/src/components/FilterChips.tsx`: one-tap interactive filter pills with active state indicator and >=48px touch targets.
  - `web/src/components/EmptyState.tsx`: accessible empty state container with German microcopy "Keine Werkzeuge vorhanden".
  - `web/src/pages/DashboardPage.tsx`: assembled dashboard home with title section "Übersicht", summary grid, filter chips, and empty state.
  - `web/src/pages/LoginPage.tsx` & `web/src/pages/NotFoundPage.tsx`: placeholder pages for `/login` and unknown route 404 handling.
  - `web/src/App.tsx`: client router configuration (`/`, `/login`, `*`).
  - `web/src/App.test.tsx` + component tests: 13 unit/integration tests verifying rendering, I/O matrix, filter selection, theme toggling, routing.
  - `justfile`: wired `web` test and lint commands into `just test` and `just lint`.

## Design Notes

- **CSS Variables:** All DESIGN.md tokens are exposed as `--gear-color-*`, `--gear-font-*`, `--gear-space-*`, `--gear-radius-*`. Dark mode is toggled via `[data-theme="dark"]` attribute on `<html>` or `prefers-color-scheme: dark`.
- **Compound Noun Handling:** All container labels use `hyphens: auto` and `word-break: normal` with appropriate min-widths to prevent awkward truncation of German compound nouns.

## Verification

**Commands:**
- `npm --prefix web run typecheck` -- expected: exit 0, no TypeScript errors
- `npm --prefix web run build` -- expected: exit 0, Vite build completes
- `npm --prefix web run test` -- expected: exit 0, all unit tests pass
- `just build` && `just vet` && `just test` && `just lint` -- expected: all pass
- `just dev` -- expected: stack boots cleanly, SPA on `:5173` renders dashboard home

**Manual checks (if no CLI):**
- Verify light and dark mode toggle flips all surface, ink, and status tokens according to DESIGN.md.

## Suggested Review Order

**Design Token System & Theming**

- complete DESIGN.md tokens (brand, status, surfaces, ink, typography, spacing, radii)
  [`tokens.css:1`](../../web/src/styles/tokens.css#L1)

- global reset, Inter font stack, root token bindings, and German word-wrap
  [`index.css:1`](../../web/src/index.css#L1)

- safe theme provider managing system preference and user toggle
  [`ThemeContext.tsx:4`](../../web/src/context/ThemeContext.tsx#L4)

**Dashboard UI Components & Layout**

- header with brand title, Ortsverband badge, and theme toggle button
  [`Header.tsx:9`](../../web/src/components/Header.tsx#L9)

- responsive 2×2 mobile / 4-column desktop status count cards
  [`SummaryGrid.tsx:14`](../../web/src/components/SummaryGrid.tsx#L14)

- one-tap status filter pills with active state indicator
  [`FilterChips.tsx:9`](../../web/src/components/FilterChips.tsx#L9)

- accessible empty state card with German microcopy
  [`EmptyState.tsx:8`](../../web/src/components/EmptyState.tsx#L8)

- assembled App Home dashboard page layout
  [`DashboardPage.tsx:9`](../../web/src/pages/DashboardPage.tsx#L9)

**Routing, Error Boundary & Tooling**

- root routing with DashboardPage, LoginPage placeholder, and ErrorBoundary
  [`App.tsx:17`](../../web/src/App.tsx#L17)

- comprehensive test suite covering rendering, theming, filtering, and routing
  [`App.test.tsx:1`](../../web/src/App.test.tsx#L1)

- single command entry point quality gates including web tests and linting
  [`justfile:93`](../../justfile#L93)
