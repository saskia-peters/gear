---
title: 'Auth UX Foundation'
type: 'feature'
created: '2026-09-06'
status: 'done'
review_loop_iteration: 0
baseline_commit: '214e87880f5bd4ef9c3c1c2f9f64541ecd8bbb50'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The auth surfaces (login, registration, password reset/change, 2FA, profile, admin recovery) were built across many stories and may not be fully consistent — they must uniformly consume the DESIGN.md token system, be responsive and accessible, and show consistent German inline feedback and anti-enumeration microcopy.

**Approach:** Audit and polish every auth surface (UX-DR4/5/6/7/8/9/10): ensure they consume the DESIGN.md color/typography/rounded/spacing tokens with light+dark pairs, are mobile-first responsive (single-column auth, container ≤ desktop max width, touch targets ≥48px, 200% zoom safe), show inline German validation/status feedback (never toast-only) with red/blue underline input states, use the canonical anti-enumeration microcopy (UX-DR7/UX-DR8), and meet the accessibility floor (keyboard-operable, focus order, screen-reader announcements, no icon-only controls) — with the 2-Faktor, Sperre (lockout), and password surfaces matching the DESIGN.md/EXPERIENCE.md IA.

## Boundaries & Constraints

**Always:**
- Every auth surface consumes the DESIGN.md tokens (color / typography / rounded / spacing) with light + `-dark` pairs (UX-DR4) and follows the component specs (primary button, input fields, status chips) (UX-DR5).
- Auth screens are responsive mobile-first, single-column, with a container ≤ desktop max width (UX-DR10); touch targets ≥48px; 200% browser zoom without breakage (UX-DR9).
- Validation errors and state changes (submitting, lockout, success) are shown as **inline German text** (never toast-only), with the red/blue underline input states per the DESIGN.md input-field spec (UX-DR5/6/8/9).
- Anti-enumeration microcopy is consistent across surfaces (UX-DR7/UX-DR8): "Wenn deine E-Mail registriert ist, erhältst du einen Link", "Wenn deine E-Mail bereits registriert ist, erhältst du eine Bestätigung", lockout "Zu viele Fehlversuche — 30/60 Sekunden warten", "→ Andere Sitzungen beendet" — no account-existence leak.
- Accessibility floor (UX-DR9): keyboard-operable, focus traversal in reading order, screen-reader announcements for status/errors, no icon-only controls (labels always present).
- The 2-Faktor, Sperre (lockout), and password surfaces match the DESIGN.md/EXPERIENCE.md IA and component behavior.
- German UI throughout; documents output in German.

**Ask First:**
- None (polish is driven by the existing DESIGN.md/EXPERIENCE.md contracts).

**Never:**
- No new auth features or flows (this is a consistency/polish story over existing surfaces).
- No backend changes (no schema, no API changes) unless a microcopy fix requires a constant update.
- No toast-only feedback (inline text is mandatory).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| TOKEN_CONSISTENCY | Any auth surface, light mode | All colors/typography/rounded/spacing from DESIGN.md tokens (no stray hex literals) | n/a |
| TOKEN_DARK | Any auth surface, dark mode | Tokens flip to `-dark` pairs (surface/ink/status) | n/a |
| RESPONSIVE | Mobile / tablet / desktop widths | Single-column auth, container ≤ max width, no horizontal scroll | n/a |
| ZOOM_200 | Auth surface at 200% zoom | No layout breakage, no clipped German compound words | n/a |
| TOUCH_TARGET | Touch device on any auth control | Interactive controls ≥48px | n/a |
| INLINE_FEEDBACK | Submit with an invalid field | Inline German field error with red underline state (not toast) | n/a |
| SUBMITTING_STATE | Submit in progress | Inline "Wird gesendet..." + disabled controls (blue/focus state) | n/a |
| ANTI_ENUM | Login / forgot / register errors | Canonical anti-enumeration German microcopy, no existence leak | n/a |
| SCREEN_READER | Status/error changes | `role="alert"`/`aria-live` announcements; focus moves to first error | n/a |
| LOCKOUT_SURFACE | Account in 429 lockout | "Zu viele Fehlversuche — 30/60 Sekunden warten" surface, no retry until timer, accessible (UX-DR9) | n/a |
| MFA_SURFACE | 2-Faktor step | Matches EXPERIENCE.md IA: "MFA aktiv" indicator, 6-digit step | n/a |

</frozen-after-approval>

## Code Map

- `web/src/styles/tokens.css` -- DESIGN.md token source (colors/typography/rounded/spacing + dark pairs); audit ensures auth surfaces reference it.
- `web/src/components/Header.tsx`, `Sidebar.tsx`, `AppShell.tsx` -- App shell (token-consistent, responsive, accessible).
- `web/src/pages/LoginPage.tsx` -- login + lockout + 2-Faktor surfaces; anti-enumeration microcopy.
- `web/src/pages/RegisterPage.tsx` -- registration; anti-enumeration + pending message.
- `web/src/pages/ForgotPasswordPage.tsx`, `ResetPasswordPage.tsx` -- forgot/reset; anti-enumeration + FR-2 inline.
- `web/src/pages/MfaPage.tsx` -- MFA settings surface.
- `web/src/pages/ChangePasswordPage.tsx` -- change password; "→ Andere Sitzungen beendet".
- `web/src/pages/ProfilePage.tsx` -- profile surface.
- `web/src/pages/AdminRecoveryPage.tsx` -- admin recovery surface.
- `web/src/pages/*.module.css` -- per-surface CSS; audit for stray hex literals + token usage.
- `web/src/test/setup.ts`, `web/src/**/*.test.tsx` -- add/update accessibility + responsiveness assertions.

## Tasks & Acceptance

**Execution:**
- [x] `web/src/pages/*.module.css` -- Replace any stray hex/RGBA literals with DESIGN.md token references (light+dark) -- UX-DR4 token consistency
- [x] `web/src/pages/*.tsx` -- Ensure every auth surface uses token-based classes; remove inline `style` props -- UX-DR4
- [x] Auth surfaces -- Add/responsive: single-column, max-width container, no horizontal scroll at 200% zoom -- UX-DR10/UX-DR9
- [x] Auth surfaces -- Ensure interactive controls ≥48px touch targets -- UX-DR9
- [x] Auth forms -- Inline German field errors with red underline state + `role="alert"`; submitting state disables + "Wird gesendet..." -- UX-DR5/6/8/9
- [x] Auth surfaces -- Canonical anti-enumeration microcopy on login/forgot/register; no account-existence leak -- UX-DR7/UX-DR8
- [x] Lockout surface -- "Zu viele Fehlversuche — 30/60 Sekunden warten" with countdown, no retry until expiry, accessible -- UX-DR6/UX-DR8/UX-DR9
- [x] 2-Faktor surface -- "MFA aktiv" indicator + 6-digit step per EXPERIENCE.md IA -- UX-DR5/UX-DR6
- [x] `web/src/**/*.test.tsx` -- Add accessibility/responsiveness/microcopy assertions across auth surfaces -- automated verification

**Acceptance Criteria:**
- Given any auth surface, when rendered, then it consumes the DESIGN.md tokens (light+dark pairs) and follows the component specs (UX-DR4/UX-DR5).
- Given any auth screen on touch/desktop across breakpoints, when rendered, then it is responsive mobile-first single-column, container ≤ max width, touch targets ≥48px, and 200% zoom safe (UX-DR9/UX-DR10).
- Given a validation error or state change on any auth form, when it occurs, then feedback is inline German text (never toast-only) with red/blue underline input states (UX-DR5/6/8/9).
- Given any auth error/confirmation, when rendered, then it uses the anti-enumeration microcopy and never leaks account existence (UX-DR7/UX-DR8).
- Given the auth screens, when built, then they meet the accessibility floor (keyboard, focus order, screen-reader announcements, no icon-only controls) and the 2-Faktor/Sperre/password surfaces match the DESIGN.md/EXPERIENCE.md IA (UX-DR9).

## Spec Change Log

- 2026-09-06 (Story 1.11 implementation): audited and polished all auth surfaces (login, register, forgot/reset, MFA, change-password, profile, admin recovery) against the DESIGN.md token system and EXPERIENCE.md IA — token consistency with theme-aware focus/status pairs, responsive/touch-target fixes (≥48px), inline German validation feedback with red/blue underline states, canonical anti-enumeration microcopy, lockout (Sperre) as a screen with a live countdown and no retry button, 2-Faktor "MFA aktiv" indicator, and the accessibility floor (aria/role, focus-visible, no icon-only controls). Frontend-only; no backend/schema/API changes.

- **2026-09-06 — Review-loop fixes (frozen contract untouched):** (1) anti-enumeration microcopy pinned by leaky-body tests (client never renders an existence-leaking server message); (2) lockout countdown live-tick test (30→29→27); (3) Sperre `aria-live` restructured (static message announced once, countdown `aria-live="off"`); (4) focus moves to the first invalid field on submit (login/register/forgot/reset/change-password); (5) status-orange token naming unified; (6) profile button copy restored to "Wird gespeichert..."; (7) submitting-state recovery + input-disabled assertions across all forms incl. admin-recovery approve/complete; (8) dark-mode focus/status tokens consolidated to a single source (`--gear-dark-*`); (9) login fetch got an abort timeout so a hung request can't leave the form disabled forever.

## Design Notes

- This is a **frontend-only polish/consistency story** — no backend/schema/API changes. It audits and hardens the auth surfaces built across Stories 1.1–1.10 against the DESIGN.md (UX-DR1–DR10) and EXPERIENCE.md contracts.
- Any microcopy inconsistency with the canonical anti-enumeration strings is resolved toward the frozen strings already in the codebase.

## Verification

**Commands:**
- `npm --prefix web run test` -- expected: all web tests pass (incl. new accessibility/responsiveness/microcopy assertions)
- `npm --prefix web run typecheck` && `npm --prefix web run build` -- expected: clean
- `just build` && `just vet` && `just test` && `just lint` -- expected: all pass, 0 lint issues
- Manual: open each auth surface in light/dark, mobile/desktop, and 200% zoom -- expected: token-consistent, responsive, accessible

## Suggested Review Order

**Design Token Foundation**

- theme-aware focus/status tokens + consolidated dark-mode source
  [`tokens.css:160`](../../web/src/styles/tokens.css#L160)

**Login Surface (Lockout + 2-Faktor + Anti-Enumeration)**

- Sperre screen: static alert + aria-live-off countdown, no retry until expiry
  [`LoginPage.tsx:279`](../../web/src/pages/LoginPage.tsx#L279)

- hung-request abort timeout (no permanent disabled form)
  [`LoginPage.tsx:140`](../../web/src/pages/LoginPage.tsx#L140)

**Accessibility**

- focus-to-first-invalid-field helper
  [`focus.ts:11`](../../web/src/auth/focus.ts#L11)

**Automated Verification**

- anti-enumeration, live countdown tick, hung-request, and focus tests
  [`LoginPage.test.tsx:296`](../../web/src/pages/LoginPage.test.tsx#L296)