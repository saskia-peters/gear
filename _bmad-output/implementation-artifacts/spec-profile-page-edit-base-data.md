---
title: 'Profile Page (Edit Base Data)'
type: 'feature'
created: '2026-09-05'
status: 'done'
review_loop_iteration: 0
baseline_commit: '27a9978f8db29bc6edf3d489f4e0731e84fdf151'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** A logged-in user has no way to view or edit their own base data (name, display name, email) in one place, and the MFA management and password-change surfaces are only reachable by direct URL — not discoverable from the app shell.

**Approach:** Add a **Profil** page (German UI) reachable by clicking the user's name in the header bar. It shows the logged-in user's base data, lets them edit and save it, and hosts two entry points: "Zwei-Faktor-Authentifizierung verwalten" (→ `/mfa`) and "Passwort ändern" (→ `/password`). Editing email is staged as `pending_email` (user stays active on the current email) and requires an admin approval to apply.

## Boundaries & Constraints

**Always:**
- The Profile page is named "Profil" (German UI) and is a separate route, reachable by **clicking the logged-in user's name in the header bar** (the name becomes a link to the profile).
- It shows the logged-in user's base data: Vorname (first_name), Nachname (last_name), Anzeigename (display_name), E-Mail-Adresse (email).
- The user can edit and save all base data fields (Vorname, Nachname, Anzeigename) directly — changes take effect immediately for the authenticated user.
- Editing the email is **staged**: the new email is stored in a nullable `pending_email` column (user stays `active` on the current email), and an admin must approve it before it becomes the real `email` (admin approval UI lands in the Epic 2 admin workflow). Until approval, the current email remains in use for login.
- Only an authenticated user can access the profile page (auth-gated via `RequireAuth`, self-ownership — no permission code needed beyond authentication, AD-12).
- The profile page hosts two navigation entries: "Zwei-Faktor-Authentifizierung verwalten" (→ `/mfa`) and "Passwort ändern" (→ `/password`).
- The header shows a link to the profile when a user is logged in (name becomes clickable; logout button and theme toggle remain).
- All API errors use the uniform envelope `{"error":{"code","message","details?"}}`.
- Save operations write an audit event (NFR-O1/NFR-O2) via the existing `audit_log` table (`profile.update`, `email.change.request`).

**Ask First:**
- None.

**Never:**
- No admin approval UI in this story (that is the Epic 2 admin workflow) — only the staging of `pending_email` and the endpoint to update it.
- No email verification / one-time token for the staged email in this story (login email stays current until admin approval).
- No profile photos / avatars.
- No changing the account `state` from the profile page.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| PROFILE_VIEW | Authenticated user opens /profil | Shows Vorname, Nachname, Anzeigename, E-Mail; links to MFA + Passwort ändern | n/a |
| PROFILE_UPDATE | Authenticated user edits Vorname/Nachname/Anzeigename and saves | Base data updated immediately, 200 confirmation, audit event `profile.update` | n/a |
| EMAIL_STAGE | Authenticated user enters a new email and saves | New email stored in `pending_email`; account stays active on current email; audit `email.change.request`; UI confirms "E-Mail-Änderung wartet auf Admin-Freigabe" | n/a |
| EMAIL_STAGE_DUPLICATE | New email already registered to another user | Staged email rejected (unique constraint on `pending_email` too) with a German error | 400 invalid_request |
| EMAIL_STAGE_INVALID | New email malformed | Rejected with a German validation error | 400 invalid_request |
| EMAIL_STAGE_SAME | New email equals current email | Rejected as no-op | 400 invalid_request |
| UNAUTHENTICATED | No/invalid session token | 401 unauthorized — endpoint is auth-gated | 401 unauthorized |
| NOT_FOUND | Profile of another user requested | 403 forbidden (self-ownership; users cannot edit others' profiles) | 403 forbidden |

</frozen-after-approval>

## Code Map

- `migrations/000007_user_profile.{up,down}.sql` -- Add `pending_email text NULL` (with a UNIQUE constraint so a staged email cannot collide with an existing/other pending email) to `users`. Existing `email` stays unique.
- `internal/user/adapters/postgres/queries.sql` -- sqlc queries: `UpdateUserProfile` (set first_name/last_name/display_name), `StagePendingEmail`, `ClearPendingEmail`. Re-run `just sqlc-generate`.
- `internal/user/core/profile.go` -- Profile use-case: `GetProfile` (from the authenticated session user), `UpdateProfile` (validate + persist base data + audit), `StageEmailChange` (validate + unique-check + persist `pending_email` + audit).
- `internal/user/core/service.go` / `ports.go` -- Extend `Service` + ports with profile methods; extend `Repository` with the profile queries.
- `internal/user/adapters/http/handler.go` -- `GET /api/v1/auth/profile` and `POST /api/v1/auth/profile` (auth-gated), plus `POST /api/v1/auth/profile/email` for staging. Uniform envelope mapping.
- `cmd/server/main.go` -- Wire profile routes (already mounted via the auth group).
- `web/src/components/Header.tsx` -- Make the user name a clickable `Link` to `/profil` (when logged in).
- `web/src/pages/ProfilePage.tsx` -- German Profil page: base data form (Vorname, Nachname, Anzeigename), email field with "ändert sich nach Admin-Freigabe" note, save actions; navigation cards to `/mfa` and `/password`.
- `web/src/pages/ProfilePage.module.css` -- DESIGN.md token styling.
- `web/src/App.tsx` -- Route `/profil` (protected).
- `web/src/pages/ProfilePage.test.tsx` -- Component tests.

## Tasks & Acceptance

**Execution:**
- [x] `migrations/000007_user_profile.{up,down}.sql` -- Add `pending_email` (nullable, UNIQUE) to `users` -- staged email storage
- [x] `internal/user/adapters/postgres/queries.sql` -- Add `UpdateUserProfile`, `StagePendingEmail`, `ClearPendingEmail` sqlc queries + re-run `just sqlc-generate` -- type-safe persistence
- [x] `internal/user/core/profile.go` -- Implement `GetProfile`, `UpdateProfile`, `StageEmailChange` (validate, unique-check, audit) -- profile core
- [x] `internal/user/adapters/http/handler.go` -- Add `GET`/`POST /api/v1/auth/profile` + `POST /api/v1/auth/profile/email` behind `RequireAuth` -- API contract
- [x] `internal/user/` -- Unit/integration tests covering the I/O matrix -- automated verification
- [x] `web/src/components/Header.tsx` -- User name becomes a clickable link to `/profil` (when logged in) -- discoverable profile
- [x] `web/src/pages/ProfilePage.tsx` -- German Profil page with base-data form, email staging note, and MFA/Passwort links -- profile UX
- [x] `web/src/App.tsx` & tests -- Protected `/profil` route -- navigable surface

**Acceptance Criteria:**
- Given a logged-in user, when they click their name in the header bar, then they land on the "Profil" page showing their base data (Vorname, Nachname, Anzeigename, E-Mail) plus entry points to "Zwei-Faktor-Authentifizierung verwalten" and "Passwort ändern".
- Given the profile page, when the user edits and saves Vorname/Nachname/Anzeigename, then the changes take effect immediately.
- Given the profile page, when the user changes the email, then the new email is staged as `pending_email` (account stays active) and the UI confirms the change awaits admin approval; the current email remains in use for login.
- Given a staged email, when an admin approves it (Epic 2), then `pending_email` becomes the real `email` and `pending_email` is cleared.

## Spec Change Log

- 2026-09-06 (Profile page implementation): added the German "Profil" page (protected `/profil` route), reached by clicking the user's name in the header (name is now a `Link`). Shows and edits base data (Vorname, Nachname, Anzeigename) immediately; email changes are staged as `pending_email` (migration 000007, UNIQUE) with the account staying active and a confirmation "E-Mail-Änderung wartet auf Admin-Freigabe."; hosts navigation to "Zwei-Faktor-Authentifizierung verwalten" (`/mfa`) and "Passwort ändern" (`/password`). Audit events `profile.update` / `email.change.request`. Core: `GetProfile`/`UpdateProfile`/`StageEmailChange`.

- 2026-09-06 (Profile page review loop — code patches, frozen contract untouched): (1) stale session snapshot fixed — `RefreshSessionUser` refreshes the cached session user after saves so `GET /profile` and the header reflect edits immediately; (2) DB-level TOCTOU guard — `StagePendingEmail` is a conditional UPDATE with `NOT EXISTS` (case-insensitive) covering both email and pending_email collisions, no-row → `ErrEmailInUse`; (3) re-staging the already-pending email is a no-op (`ErrEmailAlreadyPending`, no duplicate audit row); (4) ProfilePage stale-closure bug fixed (`baseOk` gates the email POST); (5) client validation for empty email + empty name fields; (6) form re-synced from the server response; (7) header cache (`setDisplayName`) test added; (8) pgx `23505` mapped structurally (not string matching) to `ErrEmailInUse`/`ErrUserAlreadyExists`; (9) active-state guard on profile writes (defense-in-depth); (10) NOT_FOUND/403 mapping documented (see note below).

- **2026-09-06 (review findings, no frozen-contract change):** The I/O matrix row
  `NOT_FOUND` ("Profile of another user requested") is documented as **403
  forbidden**. Because the profile endpoints act exclusively on the authenticated
  session user (there is no user-id parameter), the self-ownership violation is
  enforced as a defense-in-depth guard in the core (`ErrForbidden`, fired when
  persistence returns a different user) and answers **403 `forbidden`**. The
  HTTP-observable not-found case — the authenticated user's row vanishing
  mid-session (`UpdateUserProfile` affects 0 rows) — answers **400
  `invalid_request` "Das Konto wurde nicht gefunden."**, matching the existing
  change-password mapping. `StagePendingEmail` affecting 0 rows maps to
  **400 `invalid_request`** "Diese E-Mail-Adresse wird bereits verwendet."
  (review finding: "no row updated" == the in-use case). No frozen contract
  text was modified.

## Design Notes

- **Staged email:** `pending_email` is UNIQUE so two users can never stage the same address, and it cannot equal the current email (rejected as a no-op). The admin approval (Epic 2) moves `pending_email` → `email` and clears the staging column. Until then login uses the current email.
- **Reuse:** the audit_log table (Story 1.7) records `profile.update` and `email.change.request`; the RequireAuth gateway (Story 1.6) gates the endpoints.

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go and web tests pass, 0 lint issues
- `just db-up` -- expected: migration 000007 applies cleanly
- `just migrate-down && just migrate-up` -- expected: schema rebuilds cleanly
- `curl` profile flow (view → update base data → stage email → audit rows) -- expected: 200/400/401/403 per I/O matrix

## Suggested Review Order

**Profile Core & Data Model**

- GetProfile/UpdateProfile/StageEmailChange with email-collision + audit logic
  [`profile.go:139`](../../internal/user/core/profile.go#L139)

- staged pending_email column with UNIQUE constraint
  [`000007_user_profile.up.sql:15`](../../migrations/000007_user_profile.up.sql#L15)

**HTTP Contract**

- profile endpoints (GET, POST, email stage) with error mapping
  [`handler.go:389`](../../internal/user/adapters/http/handler.go#L389)

**Frontend Profile UX**

- header user name becomes the link to /profil
  [`Header.tsx:64`](../../web/src/components/Header.tsx#L64)

- German Profil page with base-data form, staged-email notice, MFA/Passwort cards
  [`ProfilePage.tsx:44`](../../web/src/pages/ProfilePage.tsx#L44)

**Automated Verification**

- profile I/O matrix incl. TOCTOU, session refresh, and header-cache tests
  [`profile_test.go:1`](../../internal/user/core/profile_test.go#L1)