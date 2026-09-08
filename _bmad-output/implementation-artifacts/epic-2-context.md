# Epic 2 Context: Permissions, Roles & Administration Foundation

<!-- Compiled from planning artifacts. Edit freely. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Deliver the administration and authorization foundation of G.E.A.R.: a fully isolated admin module (server-side 403 with hidden existence for non-admins), server-side resolution of each user's additive permission set, a consistent admin UX shell, the user approval workflow, role/permission-group management, user & group administration, qualification management, and admin-issued one-time passwords for accounts locked out while SMTP is unconfigured. Two rework stories then evolve the model so user-groups inherit roles and qualifications carry a per-user valid-until, plus a spreadsheet-style admin UI. All authorization is enforced server-side through the User module's auth port; the admin module only configures and manages through owning-module ports.

## Stories

- Story 2.1: Admin Module Access Isolation
- Story 2.2: Resolution of Active Permission Set
- Story 2.3: Admin UX Foundation
- Story 2.4: User Approval Workflow
- Story 2.5: Role & Permission-Group Management
- Story 2.6: User & Group Administration
- Story 2.7: Qualification Management
- Story 2.8: Admin One-Time-Password (OTP) Issuance
- Story 2.9: Admin Rework Effort 1 — Model & Backend
- Story 2.10: Admin Rework Effort 2 — Spreadsheet & UI

## Requirements & Constraints

- Admin isolation (FR-19/AD-6): every admin route/action is bound to exactly one permission code; the server re-validates the caller's resolved set and returns HTTP 403 if absent. The admin module's existence is never surfaced to non-admins in the SPA (no links/menu/route hints); force-navigating to an admin path redirects non-admins to the Dashboard with a "Zugriff verweigert" toast and no admin surfaces rendered. Unauthenticated requests get no content and no existence indication (anti-enumeration). 403s are emitted to structured auth logging (NFR-O1).
- Additive permission model (AD-12): resolved set = set-union of all permission-group memberships (individual roles) + inherited team roles + direct grants; no deny/negative permissions; one permission code per action; changes take effect immediately on the next request (no cache delay). Four base roles pre-seeded (`helfende`, `schirrmeister`, `fuehrende`, `admin`); admins create/edit custom named groups; groups are flat (no nesting); base roles are editable. User groups are organisational (grant nothing alone) but inherit via assigned roles (Story 2.9).
- Approval (FR-20): self-registered users arrive `pending_approval`; admin approval → `active` and seeds the `helfende` base role; rejection removes the record and is audited. Empty state reads "Keine ausstehenden Anträge".
- User/group administration (FR-21): create/edit/deactivate users; statuses `aktiv`/`pending`/`deaktiviert`; deactivation → no login ("→ Sofort kein Login"); removing a user from a group revokes inherited permissions immediately.
- Qualifications (FR-22/AD-7): create qualifications with a fixed validity period OR no expiry ("unbegrenzt gültig"); eligibility for tasks requiring a qualification is revoked when a fixed validity expires or the assignment is removed. Per-user valid-until (`expires_at`) required for fixed-validity assignments, none for unlimited (Human decision A, Story 2.9).
- OTP (FR-21/Story 1.8): admin generates a one-time password for `active`/`deactivated` accounts; the account is flagged so the next login forces a mandatory password change; the OTP is single-use, shown once and never recoverable, transmitted out-of-band (never emailed); audited with actor, timestamp, target user.
- Uniform JSON error envelope `{"error":{"code","message","details?"}}` with matching HTTP status; all mutations cross-entity in DB transactions; actions audited (NFR-O1).

## Technical Decisions

- Modular monolith, hexagons per module (AD-1); composition root in `cmd/server` wires modules + the auth gateway (AD-2/AD-6). The admin module is `internal/admin`; it owns exactly three config tables (`smtp_settings`, `backup_destinations`, `schedules`) and otherwise configures through owning-module ports (AD-10/AD-11).
- User module owns identity, permissions, and qualifications (AD-2); exposes one `Service` port resolving identity, granted qualifications, and the resolved permission set. Tool and Admin consume only this port; never author their own SQL over user/qualification tables or implement their own permission checks.
- Permission vocabulary: 21 base codes including `users.view`, `users.approve`, `users.manage`, `users.qualifications.manage`, `user_groups.manage`, `roles.create/edit/assign`, `qualifications.manage`, `admin.recovery.approve`, `admin.settings.email`, `admin.settings.backup`, `schedules.manage`; each admin action maps to exactly one (AD-12). Base role matrix in ARCHITECTURE-SPINE (e.g. `admin` = all 21 codes; `fuehrende`/`schirrmeister` hold `users.view` + `users.qualifications.manage`).
- Database (AD-3/AD-11): single golang-migrate set; User owns `users`, `user_groups`, `user_group_members`, `permission_groups`, `permissions`, `permission_group_permissions`, `user_permission_groups`, `user_permissions`, `qualifications`, `user_qualifications`, `audit_log`. Rework (Story 2.9) adds `user_group_permission_groups` so teams can hold roles; user detail returns the resolved permission set with per-permission source (role/team/direct). Core attributes first-class columns + one `attributes JSONB`; UUID v7 PKs; UTC RFC 3339.
- Cold-start seed (AD-12/AD-13): four base roles, two `admin` accounts, and the `schedules` catalog seeded; approved users get `helfende`.
- Server-side authorization is the only source of truth (AD-6); frontend visibility is supplementary only. Frontend: React/Vite/TS SPA, no business logic.

## UX & Interaction Patterns

- Admin nav exposes only entry points the caller's permission set allows: Übersicht · Benutzer · Rollen · Qualifikationen · Werkzeuge · Einstellungen · DSGVO (UX-DR6/AD-6). Responsive: mobile collapses nav to hamburger/bottom nav; tablet/desktop persistent sidebar; ≥1024px full sidebar.
- Surfaces consume DESIGN.md tokens (light + `-dark`) and reusable components (cards, buttons, status chips, input fields); German microcopy; inline feedback (loading skeletons, inline errors, empty states like "Keine ausstehenden Anträge", success confirmations); accessibility floor (≥48px targets, keyboard/focus order, screen-reader announcements) (UX-DR4/5/8/9/10).
- Rework UI (Story 2.10): "Benutzer" becomes a compact sortable spreadsheet (Vorname · Nachname · E-Mail · Status) defaulting to active users, with filter chips (Aktiv/Pending/Deaktiviert/Alle) and user-group tags; user detail shows three source sections plus a collapsed "Alle Berechtigungen" view where each permission shows its source in parentheses (Rolle/Benutzergruppe/Direkt); sections are read-only (or hidden for user-groups) without the matching permission code; fetch/401 failures show a German inline error or redirect to login, never crashing.

## Cross-Story Dependencies

- Story 2.2 (permission resolution) is the prerequisite for all later stories' gating (2.4–2.10) and depends on the permission vocabulary and role matrix seeded in Epic 1 (AD-12).
- Story 2.8 (OTP) builds on Story 1.8's forced-password-change-on-next-login flag and baseline SMTP handling.
- Story 2.4 (approval) and Story 2.8 (OTP) consume account states (`pending_approval`/`active`/`deactivated`) and the auth port owned by the User module (Epic 1/AD-2).
- Stories 2.9 and 2.10 are a coupled rework: 2.10's UI depends on 2.9's model/backend changes (team-inherited roles, per-user `expires_at`, provenance).
- The admin module's `schedules.manage` surface and schedule catalog seed are prerequisites for Epic 2's tool-type default / per-tool schedule assignment (FR-8/FR-9/FR-30, AD-16).
