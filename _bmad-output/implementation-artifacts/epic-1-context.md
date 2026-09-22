# Epic 1 Context: Account & Authentication

<!-- Compiled from planning artifacts. Edit freely. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Deliver the identity foundation of G.E.A.R.: a cold-start project scaffold with a wired PostgreSQL data layer, a dashboard home surface, self-registration (pending admin approval), email/password authentication with signed sessions and progressive lockout, optional TOTP MFA, self-service password management (change own password, forgot-password reset), flexible user attributes, dual-admin credential recovery, and the auth UX foundation. All identity/auth behavior is owned by the User Directory & Auth module; later epics consume it through its single auth port.

## Stories

- Story 1.1: Project Scaffold & Database Foundation
- Story 1.2: Dashboard Foundation (App Home)
- Story 1.3: Self-Registration with Admin Pending Approval
- Story 1.4: Email & Password Authentication
- Story 1.5: Progressive Login Lockout
- Story 1.6: Optional TOTP Multi-Factor Authentication
- Story 1.7: Change Own Password
- Story 1.8: Self-Service Password Reset (Forgot Password)
- Story 1.9: Flexible User Attributes
- Story 1.10: Dual-Admin Credential Recovery
- Story 1.11: Auth UX Foundation

## Requirements & Constraints

- Password policy (FR-2): minimum 10 characters, no composition rules; shorter passwords rejected with a clear inline validation error on every password surface.
- Progressive lockout (FR-3): 3 consecutive failed logins on an account → 30s block (HTTP 429); 4+ → 60s block; never indefinite; tracked per account, no cross-account cascade; 429s emitted to structured auth logs.
- Self-registration (FR-5): new accounts are created `pending_approval`, blocked from auth even with correct credentials, and only admin approval enables login. All registration/reset/recovery responses are uniform anti-enumeration confirmations that never reveal account existence.
- MFA (FR-4): optional TOTP; enrollment shows a shared secret + QR code and only completes after confirming a valid 6-digit code; when enabled, login requires a current 6-digit TOTP code before a session is issued; disabling requires re-authentication with a valid code.
- Sessions (NFR-S2/NFR-S3): cryptographically signed session tokens, idle expiry (default 8h), server-side invalidation on logout and on password change/reset; permission sets re-resolved server-side per request, never session-cached; permission denials return HTTP 403.
- Change own password (FR-25): must confirm current password first; new password ≥10 chars; success revokes all other sessions instantly, keeps the current session active, is audited, and the password is never stored or logged in plaintext.
- Password reset (FR-26): active accounts receive a single transactional email with a single-use, hashed-at-rest, 30-minute reset token; one active token per user (new requests invalidate older ones); expired/used tokens rejected; deactivated/pending accounts get only the uniform confirmation. When SMTP is unconfigured, no email is sent — user is told admins will provide a one-time password and the account is flagged for mandatory password change at next login.
- Admin credential recovery (FR-27/AD-13): exactly two seeded `admin` accounts (out-of-band credentials, never in VCS); a locked-out admin cannot self-reset via the normal flow; recovery is gated by the `admin.recovery.approve` permission exercised by the second admin, with mandatory Begründung + confirmation checkbox; approvals are high-severity, immutable audit events. Last-admin recovery is an out-of-band manual procedure with mandatory audit trail.
- All handler errors use the uniform JSON envelope `{"error":{"code","message","details?"}}` with matching HTTP status (401, 403, 429).

## Technical Decisions

- Stack: Go 1.27, chi v5.3.2+ (pinned to avoid GO-2026-4316 open-redirect), sqlc v1.31.1, pgx v5, golang-migrate v4.19.1, PostgreSQL 18, TypeScript 6 / React 19 / Vite 8 SPA, single Docker/podman Compose source, root `justfile` as the only command entry point (`just db-up`, `just dev`).
- Structural seed (Story 1.1): `cmd/server/` composition root wires module hexagons + adapters with no business logic in handlers/adapters/repositories (AD-1); `internal/{user,tools,admin,platform}/`; `web/` (no business logic); `migrations/`; `deploy/`; `infra/`; `docs/`.
- User module ownership (AD-2/AD-3/AD-13): single owner of users, groups, permissions, sessions, MFA/TOTP, credentials, recovery tokens, account state (`pending_approval`/`active`/`deactivated`), and qualifications; exposes one `Service` port resolving identity, granted qualifications, and the resolved permission set; revocation takes effect on the very next check.
- Database (AD-3/AD-11): one shared golang-migrate set, each table owned by exactly one module — User owns tables `users`, `user_groups`, `user_group_members`, `permission_groups`, `permissions`, `permission_group_permissions`, `user_permission_groups`, `user_permissions`, `qualifications`, `user_qualifications`, `audit_log`, `password_reset_tokens`. Core profile attributes are first-class typed columns plus a single `attributes JSONB` extension column (FR-7); JSONB keys have one shared semantic meaning app-wide. UUID v7 PKs; UTC RFC 3339 timestamps. Each story adds its own incremental migration; the finished set builds the full schema in one fresh `just db-up` run.
- Cold-start seed: identity tables required by the seeder plus the `admin.recovery.approve` permission; seeds the four base roles (`helfende`, `schirrmeister`, `fuehrende`, `admin`) and two admin accounts; approved users are seeded with `helfende`.
- Permission model (AD-12/AD-6): every action maps to exactly one permission code; resolved set = additive union of group memberships + direct grants; flat (no nested groups); user groups are organisational only. Epic 1 depends on `dashboard.view` (granted to all base roles) and `admin.recovery.approve` (admin only). Server-side authorization is the only source of truth; frontend visibility is supplementary.
- Credentials: passwords hashed with Argon2id (never plaintext, never logged); reset tokens are cryptographically random, hashed at rest, single-use, 30-minute expiry.
- FR-26 email uses a baseline SMTP transactional sender reading Admin-owned `smtp_settings` via the Admin settings port (full SMTP admin surface ships in Epic 3).

## UX & Interaction Patterns

- Auth surfaces (EXPERIENCE.md IA): `Anmelden` (login), `2-Faktor`, `Sperre` (lockout), `Passwort vergessen`, `Bestätigung (Passwort)`, `Neues Passwort`, `Registrierung`, `Antrag eingereicht`, `Passwort ändern`; wireframe references `flow-auth-password-2026-08-30`, `flow-registration-2026-08-30`, `flow-dual-admin-recovery-2026-08-30`.
- All auth surfaces consume the DESIGN.md token foundation (color/typography/rounded/spacing with light + `-dark` pairs), are mobile-first single-column with touch targets ≥48px and 200% zoom without breakage, and follow the input-field spec (floating label, blue focus / red error underline).
- German microcopy is fixed: lockout "Zu viele Fehlversuche — 30/60 Sekunden warten"; registration confirmation "Dein Konto ist in Bearbeitung. Login erst möglich nach Admin-Freigabe"; reset confirmation "Wenn deine E-Mail registriert ist, erhältst du einen Link"; password change confirmation "→ Andere Sitzungen beendet".
- Feedback is inline German text (never toast-only); anti-enumeration microcopy across all auth flows; submitting states disable buttons (no double-submit); accessibility floor applies (keyboard-operable, focus in reading order, screen-reader announcements, no icon-only controls).

## Cross-Story Dependencies

- Story 1.1 scaffold is the prerequisite for every other story in this and later epics (DB layer, module structure, error envelope, justfile).
- Story 1.8 (password reset) depends on baseline SMTP delivery; the admin-side "create one-time password" surface is in Epic 2 (FR-21), and the full SMTP admin panel in Epic 3 (FR-28).
- The dashboard placeholder (Story 1.2) is gated by `dashboard.view`; live status counts/colors arrive in Epic 6 (FR-16); admin approval of `pending_approval` users is Epic 2 (FR-20).
- All later epics' permission gating consumes the User module auth port resolved here (AD-2/AD-6).
