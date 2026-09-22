---
title: 'Admin UX Foundation'
type: 'feature'
created: '2026-09-07'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'ae34e01d6144aab64181fba61b46fbb95554a092'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The ADMIN module is a placeholder (`AdminPage` says "folgt in Epic 2") with no usable navigation, and the tool-management surface is only reachable by `schirrmeister` (and `admin`), not by `fuehrende`. The admin shell must become a welcoming, plain-language, non-technical-friendly hub whose surfaces are reachable through a responsive, permission-filtered navigation (UX-DR6/AD-6).

**Approach:** Build the Admin UX foundation in the SPA: a friendly "Verwaltung" landing hub (big clear cards, German plain-language microcopy, no jargon, empty states), a responsive admin sub-navigation (the 7 EXPERIENCE.md entries filtered by the caller's resolved permission set), placeholder surfaces behind each nav entry gated to their permission codes, and the accessibility floor (≥48px targets, keyboard, focus order, SR announcements). Ship the user decision that `fuehrende` may add/maintain tools like `schirrmeister` (migration 000011 adds `tools.manage` + `tool_types.manage` to the `fuehrende` role; ARCHITECTURE-SPINE matrix updated).

## Boundaries & Constraints

**Always:**
- **Role matrix (user decision):** `fuehrende` gains `tools.manage` + `tool_types.manage` (matching `schirrmeister`). Implement via **migration 000011** (idempotent `ON CONFLICT DO NOTHING`, add the two rows to `permission_group_permissions` for `fuehrende`; down removes only those rows). Update `ARCHITECTURE-SPINE.md` base-role matrix + reading note to match.
- **Nav (UX-DR6/AD-6):** the admin sub-nav lists exactly the 7 EXPERIENCE.md entries — Übersicht · Benutzer · Rollen · Qualifikationen · Werkzeuge · Einstellungen · DSGVO — filtered by the resolved permission set. Only entries whose gating code(s) the caller holds are shown; a caller with none of a module's codes never sees that entry (anti-enumeration, FR-19).
  - Übersicht → any admin-module code (landing)
  - Benutzer → `users.view`/`users.approve`/`users.manage`
  - Rollen → `roles.create`/`roles.edit`/`roles.assign`
  - Qualifikationen → `qualifications.manage`
  - Werkzeuge → `tools.manage`/`tool_types.manage`
  - Einstellungen → `admin.settings.email`/`admin.settings.backup`
  - DSGVO → `dsgvo.access_report`/`dsgvo.delete`
- **Client gate:** the ADMIN module in the top-level sidebar is shown when the caller's **resolved permission set** (from `GET /api/v1/auth/me/permissions`, server-authoritative) contains **any** admin-module code — replacing the binary `is_admin` cache for module visibility. A caller with `tools.manage` but no other admin code sees the ADMIN module and within it only Werkzeuge (+ Übersicht). A caller with no admin-module code sees no ADMIN entry at all (FR-19). On network failure, fail closed (no ADMIN entry).
- **Landing page (Verwaltung — Start):** a warm, plain-language hub for people who do not work with IT systems every day. Big tappable cards (≥48px), one card per permitted entry, each with a short German plain-language purpose (no jargon, no permission codes), a "Start" heading with a friendly subtitle, an empty state ("Keine ausstehenden Anträge") where approvals will appear, and the Dual-Admin-Wiederherstellung link for admins. German microcopy throughout (UX-DR4/5/8).
- **Placeholder surfaces:** each nav entry has a route + placeholder page (styled like the current AdminPage placeholder) that later stories (2.4–2.8) fill; each is route-gated server-side (RequireAuth + permission guard) so a caller without the code is redirected to the Dashboard (never a "Zugriff verweigert" leak, FR-19). `Werkzeuge` must be reachable by `schirrmeister`, `fuehrende`, and `admin`.
- **Accessibility floor (UX-DR9):** every interactive element ≥48px target; keyboard operable; logical tab/focus order; visible focus rings; `aria-live`/`role="status"` for dynamic announcements; semantic landmarks (nav/main/heading hierarchy). Preserve the existing focus-first-error and token patterns.
- **Design tokens + components (UX-DR10):** consume `tokens.css` (light + `-dark` via the existing theme) and the reusable components (card, button, status chip, input) — reuse `EmptyState`, `Header`, existing module CSS conventions; extend tokens only where a new need is real.

**Ask First:**
- None.

**Never:**
- No permission-code names or jargon in user-facing landing/nav microcopy (keep codes server-side).
- No hardcoded `is_admin`-only gate for admin-module visibility — the resolved permission set drives the nav.
- No route that leaks admin existence to a caller without any admin-module code (FR-19).
- No changes to already-committed migration 000010 (Story 2.2); the `fuehrende` change is a new migration only.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| NAV_ADMIN | admin (all 21 codes) | ADMIN module visible; all 7 nav entries shown | n/a |
| NAV_SCHIRRMEISTER | schirrmeister (tools.manage + tool_types.manage) | ADMIN visible; only Werkzeuge (+ Übersicht) entries | n/a |
| NAV_FUEHRENDE | fuehrende, after 000011 | ADMIN visible; Werkzeuge entry shown (tools.manage now held) | n/a |
| NAV_HELFENDE | helfende (no admin codes) | No ADMIN module entry anywhere (FR-19) | n/a |
| NAV_FETCH_FAIL | /me/permissions network error | ADMIN entry hidden (fail closed, FR-19) | no crash, no entry |
| ROUTE_BLOCKED | caller hits /admin/benutzer without users.* | Redirect to Dashboard; no existence leak | 302/redirect |
| LANDING_EMPTY | admin with no pending approvals | Empty state text "Keine ausstehenden Anträge" | n/a |
| MOBILE | viewport ≤640px | Admin nav collapses to hamburger/bottom nav (UX-DR10) | n/a |
| TABLET_DESKTOP | 641–1024px | Persistent admin sidebar | n/a |
| WIDE | >1024px | Full admin sidebar | n/a |
| FUEHRENDE_AFTER_REVOKE | fuehrende granted then revoked tools.manage | Werkzeuge entry disappears on next nav render (live set) | n/a |

</frozen-after-approval>

## Code Map

- `migrations/000011_fuehrende_tools.{up,down}.sql` -- Add `tools.manage` + `tool_types.manage` to `fuehrende` (idempotent); down removes only those two rows.
- `_bmad-output/planning-artifacts/architecture/.../ARCHITECTURE-SPINE.md` -- Update base-role matrix (`fuehrende` column gets ✔ for `tools.manage`/`tool_types.manage`) + reading note (leadership also caretaker for tool administration per user decision).
- `web/src/auth/authState.ts` -- Add a `permissions` cache + `hasPermission(code)` helper, fed from `GET /api/v1/auth/me/permissions` (server-authoritative); `clearAuthState` clears it.
- `web/src/auth/permissions.ts` -- New small module: the admin-module nav model (entry → gating codes → route → plain-language label/description) + `filteredAdminNav(perms)` + `hasAnyAdminCode(perms)`. Keep it a separate file (god-class convention).
- `web/src/components/AdminNav.tsx` (+ `.module.css`) -- Responsive admin sub-nav: the 7 entries filtered by the resolved set; hamburger/bottom on ≤640px, persistent sidebar 641–1024px, full sidebar >1024px; active state, aria-current, ≥48px targets.
- `web/src/pages/AdminPage.tsx` -- Rebuild the landing as the friendly "Verwaltung — Start" hub: heading + subtitle, one card per permitted entry (plain-language purpose), empty state, Dual-Admin link for admins.
- `web/src/pages/admin/*.tsx` -- Placeholder pages for Benutzer / Rollen / Qualifikationen / Werkzeuge / Einstellungen / DSGVO (styled placeholders; later stories fill them).
- `web/src/App.tsx` -- Add routes under `/admin/*`; gate the ADMIN module visibility on the resolved permission set; wire AdminNav into the admin layout.
- `web/src/components/AppShell.tsx` -- Optional: render AdminNav within the admin module layout.
- Tests: `web/src/components/AdminNav.test.tsx`, `web/src/pages/AdminPage.test.tsx` (extend), `web/src/auth/permissions.test.ts` (nav filtering), `web/src/auth/authState.test.ts` (permissions cache), `web/src/App.test.tsx` (route gating).
- `internal/user/adapters/postgres/repository_test.go` -- Update `fuehrende` expectation to 7 codes (was 5) after 000011.
- `docs/scripts/generate-docs.js` -- No change expected (nav is app, not docs).

## Tasks & Acceptance

**Execution:**
- [x] `migrations/000011_fuehrende_tools.{up,down}.sql` -- Add `tools.manage` + `tool_types.manage` to `fuehrende`; down removes only those rows -- user decision
- [x] `ARCHITECTURE-SPINE.md` -- Update base-role matrix + reading note for `fuehrende` -- source-of-truth sync
- [x] `web/src/auth/permissions.ts` -- Admin-nav model: entry → gating codes → route → plain-language label; `filteredAdminNav`; `hasAnyAdminCode` -- nav filtering core
- [x] `web/src/auth/authState.ts` -- permissions cache + `hasPermission(code)`; cleared on logout/401 -- server-authoritative cache
- [x] `web/src/components/AdminNav.tsx` (+css) -- responsive permission-filtered sub-nav (hamburger ≤640 / persistent 641–1024 / full >1024) -- UX-DR10
- [x] `web/src/pages/AdminPage.tsx` -- friendly "Verwaltung — Start" landing (cards, plain German, empty state) -- non-IT friendly
- [x] `web/src/pages/admin/*.tsx` -- placeholder surfaces for the 6 non-landing entries -- route-gated placeholders
- [x] `web/src/App.tsx` -- `/admin/*` routes; ADMIN module visibility from resolved set; permission guards -- AD-6/FR-19
- [x] `web/src/components/AppShell.tsx` -- render AdminNav in the admin layout -- shell integration
- [x] Tests -- nav filtering, permissions cache, landing, route gating, fuehrende=7 integration -- I/O matrix
- [x] `internal/user/adapters/postgres/repository_test.go` -- fuehrende resolves 7 codes after 000011 -- regression

**Acceptance Criteria:**
- Given the design tokens and component library, when the admin surfaces are built, then they consume DESIGN.md tokens (light + `-dark`) and reusable components with German microcopy (UX-DR4/5/8).
- Given access on mobile/tablet/desktop, when any admin surface renders, then it is responsive (≤640px hamburger/bottom; 641–1024px persistent sidebar; >1024px full sidebar) (UX-DR10).
- Given the admin nav, when I use it, then it exposes only entries my permission set allows (Übersicht · Benutzer · Rollen · Qualifikationen · Werkzeuge · Einstellungen · DSGVO) (UX-DR6/AD-6).
- Given forms/lists interactions, when state changes or an error occurs, then feedback is inline German text (loading skeletons, inline errors, empty states like "Keine ausstehenden Anträge", success confirmations) (UX-DR6/8), and interactive elements meet the a11y floor (≥48px, keyboard, focus order, SR) (UX-DR9).
- Given the admin surfaces, when built, then they match the EXPERIENCE.md IA for Admin-Modul and Werkzeugverwaltung and use route-gating to the contained permission codes (AD-6), and `fuehrende` can add/maintain tools like `schirrmeister` (user decision).

## Spec Change Log

## Design Notes

- **`fuehrende` + tools is a user product decision** made after Story 2.2 shipped. It is a data change (migration 000011) + spine sync, not a rewrite of 2.2; the resolved-set query already picks the new codes up automatically.
- **Module gate moves from `is_admin` to "has any admin-module code".** This is what lets `schirrmeister`/`fuehrende` reach Werkzeuge while `helfende` sees nothing (FR-19). The existing `GET /me/permissions` (Story 2.2) is the feed.
- **Plain language, no codes:** the landing/nav show German purpose text ("Geräte verwalten", "Mitglieder freigeben") — permission codes stay server-side. This is the non-IT-friendly design ask.
- **Placeholders are honest:** each nav entry routes to a small placeholder surface (like today's AdminPage) so the shell is navigable now; Stories 2.4–2.8 replace them with real surfaces. Werkzeuge is deliberately reachable by `schirrmeister`/`fuehrende`/`admin` from this story.

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go and web tests pass, 0 lint issues
- `npm --prefix docs run generate && npm --prefix docs run build` -- expected: docs unaffected (no generator change)
- `just migrate-up` then `SELECT code FROM permission_group_permissions pgp JOIN permission_groups g ON g.id=pgp.permission_group_id WHERE g.name='fuehrende' ORDER BY 1` -- expected: 7 codes incl. `tools.manage`, `tool_types.manage`
- Live check as a `fuehrende` user (created via SQL): ADMIN module visible, only Übersicht + Werkzeuge entries; as `helfende`: no ADMIN entry.

## Suggested Review Order

**Entry point**

- The nav model is the single source of truth: the 7 EXPERIENCE.md entries, their gating codes, routes, and plain-language labels — everything else derives from it.
  [`permissions.ts:24`](../../web/src/auth/permissions.ts#L24)

**Security gate (highest risk)**

- RequireAdminModule re-validates server-side on every admin mount — defeats forged localStorage, fail-closed, refreshes cache before children render.
  [`App.tsx:165`](../../web/src/App.tsx#L165)

- Route guards derive codes from ADMIN_NAV_ENTRIES (no inline duplication); per-entry anti-enumeration redirect.
  [`App.tsx:236`](../../web/src/App.tsx#L236)

- Permission cache + clear-on-logout in authState.
  [`authState.ts:1`](../../web/src/auth/authState.ts#L1)

**Admin shell**

- Responsive AdminNav: hamburger ≤640px, persistent 200px 641–1024px, 240px >1024px; Escape/outside-click close; ≥48px targets; aria-current.
  [`AdminNav.tsx:1`](../../web/src/components/AdminNav.tsx#L1)

- Friendly "Verwaltung — Start" landing: cards per permitted entry, plain German, approvals section gated on users.approve/view, empty state, Dual-Admin link.
  [`AdminPage.tsx:18`](../../web/src/pages/AdminPage.tsx#L18)

**Data change (user decision)**

- Migration 000011 grants fuehrende tools.manage + tool_types.manage; down removes only those rows; idempotent.
  [`000011_fuehrende_tools.up.sql:1`](../../migrations/000011_fuehrende_tools.up.sql#L1)

**Tests**

- Nav-model unit tests: exact 7 entries, filteredAdminNav by role, hasAnyAdminCode, anti-enumeration.
  [`permissions.test.ts:8`](../../web/src/auth/permissions.test.ts#L8)

- App-level: forged-cache security (cold + SPA nav), ROUTE_BLOCKED parameterized over 4 entries, schirrmeister/fuehrende nav, fail-closed fetch.
  [`App.test.tsx:1`](../../web/src/App.test.tsx#L1)

- AdminNav a11y tests: escape/outside-click/link-close, aria-current, mobile toggle.
  [`AdminNav.test.tsx:9`](../../web/src/components/AdminNav.test.tsx#L9)