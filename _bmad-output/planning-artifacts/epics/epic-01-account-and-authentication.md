## Epic 1: Account & Authentication

Users can register, authenticate, and manage their own identity securely. A volunteer self-registers for a `pending_approval` account, logs in with email + password (with optional TOTP MFA and progressive lockout), manages their profile, changes their own password (revoking other sessions), and recovers a forgotten password via a secure email link. Seeded admin accounts are protected by dual-admin credential recovery.

### Story 1.1: Project Scaffold & Database Foundation

As a developer on the G.E.A.R. team,
I want a cold-start repository scaffold with a wired PostgreSQL data layer and a single-command runnable stack,
So that every subsequent story builds on a real, runnable application with a valid database.

**Acceptance Criteria:**

**Given** an empty repository on the `gear` project,
**When** the scaffold is created per the architecture Structural Seed,
**Then** the repository contains `cmd/server/`, `internal/{user,tools,admin,platform}/`, `web/`, `migrations/`, `deploy/`, `infra/`, `docs/`, and a root `justfile`, each with a documented purpose
**And** `cmd/server` is the composition root wiring the module hexagons and adapters (no business logic in adapters/handlers/repositories, per AD-1).

**Given** the chosen stack (Go 1.27, chi v5.3.2, sqlc v1.31.1, pgx v5, golang-migrate v4.19.1),
**When** the Go module is initialized,
**Then** `go build ./...`, `go vet ./...`, and `go test ./...` pass with no errors
**And** chi is pinned to v5.3.2 or higher (avoiding GO-2026-4316 open-redirect, per Stack).

**Given** the database requirements (PostgreSQL 18, single shared migration set per AD-3/AD-11, NFR-R2),
**When** the local stack is started via the Compose definition (Docker or Podman),
**Then** `just db-up` provisions a PostgreSQL 18 container and `golang-migrate` applies the migrations present in `migrations/`
**And** the migration infrastructure is in place so each later story adds its own incremental migration for exactly the tables it needs (AD-11: one migration set, each table owned by one module) — the finished set creates all tables in a single fresh `just db-up`
**And** a connection pool (pgx) is established at `cmd/server` and exposed to the module repositories through their adapter ports
**And** the cold-start migration creates only the base tables required for the seeder (identity + `admin.recovery.approve` permission) and seeds the four base roles (`helfende`, `schirrmeister`, `fuehrende`, `admin`) and the two pre-seeded admin accounts (credentials delivered out-of-band, never in VCS, per FR-27/AD-13) (AD-12/AD-13).

**Given** the JSON error contract (Architecture error shape),
**When** any handler returns an error,
**Then** the response body is the uniform envelope `{ "error": { "code", "message", "details?" } }` with the matching HTTP status (e.g. 401, 403, 429).

**Given** the app is started with the root `justfile`,
**When** a single command (e.g. `just dev`) runs the full stack (API + SPA + DB),
**Then** the API responds to a health check, confirming the database connection is live
**And** no database command is duplicated outside the `justfile` recipes.

### Story 1.2: Dashboard Foundation (App Home)

As a user,
I want a dashboard home screen right after the app scaffold,
So that every subsequent flow has a real place to land and return to.

**Acceptance Criteria:**

**Given** the scaffold from Story 1.1,
**When** I open the app,
**Then** the SPA has a dashboard home route (gated server-side by `dashboard.view`, granted to all base roles), and unauthenticated visitors are redirected to the login page (AD-6) — where the login flow itself ships in the following auth stories.

**Given** no tools exist yet,
**When** the dashboard renders,
**Then** it shows the dashboard layout with the 2×2 summary count grid (empty at this stage, per UX-DR5) and the status filter chips, plus the empty state "Keine Werkzeuge vorhanden" (UX-DR6/UX-DR8).

**Given** the dashboard layout,
**When** rendered on mobile, tablet, or desktop,
**Then** it follows UX-DR10: mobile 2×2 summary grid, tablet/responsive columns, filter chips one-tap (UX-DR5).

**Given** the app shell/token system,
**When** the SPA is first built,
**Then** it implements and consumes the full DESIGN.md foundation — color tokens with light + `-dark` pairs (UX-DR1), the typography ramp (display/title/body/meta/label, German compound nouns not truncated) (UX-DR2), and the spacing/rounded/elevation scales (UX-DR3) — so every later surface builds on one token foundation (UX-DR1/UX-DR2/UX-DR3).

**Given** later flows complete an action (e.g. inspection, admin action),
**When** they return,
**Then** they land on this refreshed dashboard home (UX-DR6/7) — the shell and empty-state foundation provided here; live status counts and colors fill in when tool/inspection data exists (FR-16, fully in Epic 6).

### Story 1.3: Self-Registration with Admin Pending Approval

As a new volunteer,
I want to register an account with my personal details,
So that I can be considered for access to the equipment-inspection app once an admin approves me.

**Acceptance Criteria:**

**Given** I am an unauthenticated visitor on the registration page (German UI),
**When** I submit Vorname, Nachname, E-Mail, Passwort and Passwort-Wiederholung,
**Then** a `pending_approval` account is created and I cannot log in until an admin approves it (FR-5)
**And** the password is validated to ≥10 characters (FR-2) with a clear inline German validation error if shorter
**And** the account is created in a single transaction; the password is stored as an Argon2id hash (AD-13, never plaintext).

**Given** I submit a registration email that already exists or is otherwise invalid,
**When** the form is submitted,
**Then** the response is a uniform anti-enumeration confirmation ("Wenn deine E-Mail bereits registriert ist, erhältst du eine Bestätigung") that does not reveal whether the email exists (FR-5/UX-DR7)
**And** no error reveals account-existence to any caller.

**Given** the registration completes,
**When** I am shown the confirmation,
**Then** I see "Dein Konto ist in Bearbeitung. Login erst möglich nach Admin-Freigabe" (UX-DR8) describing the `pending_approval` dependency.

**Given** I attempt to log in with a `pending_approval` account,
**When** I submit credentials,
**Then** authentication is rejected even with correct credentials (FR-5 pending state blocks auth)
**And** the rejection does not leak whether the account exists.

**Given** the user module schema,
**When** a profile is created,
**Then** core profile fields map to first-class typed columns and the `attributes JSONB` column is available for extensible custom attributes (FR-7/AD-3).

### Story 1.4: Email & Password Authentication

As a registered volunteer (status `active`),
I want to log in with my email and password,
So that I can access the app under a secure authenticated session.

**Acceptance Criteria:**

**Given** I have an `active` account,
**When** I submit my registered email and the correct password,
**Then** I receive a secure, cryptographically signed session token (FR-1/NFR-S2) and am granted access
**And** the session token expires after the configured idle period (default 8h) and is invalidated server-side on logout (NFR-S2).

**Given** I submit an incorrect password or a non-existent email,
**When** authentication is attempted,
**Then** HTTP 401 is returned with German microcopy and the response does not reveal whether the email or password was wrong (UX-DR7 anti-enumeration).

**Given** my account status is `pending_approval` or `deactivated`,
**When** I attempt to log in with correct credentials,
**Then** authentication is rejected and no session is issued (FR-5/FR-21), without leaking account existence.

**Given** the auth gateway per AD-2/AD-6,
**When** any protected request arrives,
**Then** the server validates the session token and resolves my active permission set (all server-side, never client-trusted)
**And** permission checks return HTTP 403 when the resolved set lacks the required code.

**Given** a logged-in user requests logout,
**When** logout completes,
**Then** the session token is invalidated server-side, and future requests with that token are rejected.

### Story 1.5: Progressive Login Lockout

As the authentication service,
I want to progressively block repeated failed logins,
So that brute-force attempts are slowed without locking out legitimate users permanently.

**Acceptance Criteria:**

**Given** a login attempt fails 3 times in a row for the same account,
**When** a further login attempt is made,
**Then** the account is blocked for 30 seconds and the response is HTTP 429 (FR-3).

**Given** a login attempt fails 4 or more times in a row for the same account,
**When** a further login attempt is made,
**Then** the account is blocked for 60 seconds and the response is HTTP 429 (FR-3).

**Given** an account is in lockout,
**When** the lockout period expires,
**Then** login attempts are accepted again and the failure counter is available for a fresh cycle
**And** the counter does not permanently lock the account (no indefinite lockout from repeated failures alone).

**Given** a blocked user is shown the lockout screen (German UI),
**When** they attempt to retry,
**Then** the UI shows "Zu viele Fehlversuche — 30/60 Sekunden warten" with no retry button until the timer expires (UX-DR6/UX-DR8) and the screen is accessible (UX-DR9).

**Given** any 429 lockout is triggered,
**When** the event occurs,
**Then** it is emitted to structured logging for auth events (NFR-O1).

**Given** the lockout is per account,
**When** multiple accounts are targeted,
**Then** lockout does not cascade across unrelated accounts (each account tracked independently).

### Story 1.6: Optional TOTP Multi-Factor Authentication

As a volunteer,
I want to optionally enable TOTP two-factor authentication on my account,
So that my account is protected by a second factor beyond my password.

**Acceptance Criteria:**

**Given** I am an authenticated user on the MFA settings surface (German UI),
**When** I enable TOTP,
**Then** I am shown a shared secret and a QR code to scan into an authenticator app, and only after I confirm a valid 6-digit code is MFA enabled (FR-4)
**And** the secret is never transmitted in plaintext after provisioning and is protected at rest (NFR-S4).

**Given** MFA is enabled on my account,
**When** I log in with a valid email and password,
**Then** I am required to provide a current 6-digit TOTP code before a session is issued, and the UI shows an "MFA aktiv" indicator (FR-4/UX-DR6).

**Given** I provide an invalid or expired TOTP code,
**When** the code is validated,
**Then** login is rejected with German microcopy and no session is issued, and the rejection does not reveal why (UX-DR7 anti-enumeration).

**Given** MFA is enabled,
**When** I choose to disable it,
**Then** I must re-authenticate with a valid current TOTP code before MFA is removed.

**Given** the MFA enrollment/validation flow,
**When** any TOTP operation occurs,
**Then** relevant events are emitted to structured auth logging (NFR-O1) and the flow meets the accessibility floor (UX-DR9).

### Story 1.7: Change Own Password

As an authenticated user,
I want to change my own password,
So that I can keep my credentials current and revoke any other active sessions.

**Acceptance Criteria:**

**Given** I am logged in,
**When** I open the "Passwort ändern" surface,
**Then** I can enter my Aktuelles Passwort, a Neues Passwort, and a Wiederholung (German UI) (FR-25).

**Given** I submit the change,
**When** the current password is incorrect,
**Then** the change is rejected with a clear German error and my password remains unchanged.

**Given** I submit a new password,
**When** it is shorter than 10 characters,
**Then** it is rejected with a clear inline validation error (FR-2/UX-DR8) and no change occurs.

**Given** I successfully change my password,
**When** the change is committed,
**Then** the password is stored as an Argon2id hash, never plaintext or logged (FR-25/NFR-O1/AD-13)
**And** all other active sessions for my account are revoked instantly (FR-25), forcing re-authentication on those devices
**And** the change is audited with actor, timestamp, and operation type (NFR-O1/NFR-O2).

**Given** the change completes,
**When** I am shown a confirmation,
**Then** the UI shows "→ Andere Sitzungen beendet" (UX-DR8) and I remain logged in on the current session.

### Story 1.8: Self-Service Password Reset (Forgot Password)

As a logged-out user,
I want to reset my forgotten password,
So that I can regain access to my account without an admin.

**Acceptance Criteria:**

**Given** I am on the login page and click "Passwort vergessen" and enter my email,
**When** SMTP email delivery is configured,
**Then** I am shown the uniform confirmation "Wenn deine E-Mail registriert ist, erhältst du einen Link" (FR-26/UX-DR7 anti-enumeration), regardless of whether the account exists.

**Given** my account is `active` and SMTP email delivery is configured,
**When** I submit the reset request,
**Then** the system sends a single transactional email containing a single-use, hashed, 30-minute reset link (FR-26/AD-13), and no automated notification email is sent to other addresses (FR-26).

**Given** SMTP email delivery is **not** configured (as in early deployment, before Epic 3),
**When** I submit the reset request,
**Then** I am informed that "the admins have been notified" and will provide a one-time password (German microcopy), and no email is sent
**And** the account is flagged so the next login requires a mandatory password change.

**Given** an admin has created a one-time password for my account (created via the admin management surface in Epic 2, transmitted out-of-band in a secure way),
**When** I log in with that one-time password,
**Then** I am forced to change it immediately before accessing the app (the one-time password cannot be reused after the change).

**Given** the email reset link,
**When** I open it,
**Then** I can set a new password (min 10 chars, FR-2) plus a repeat; on success the old password is replaced with an Argon2id hash (AD-13)
**And** earlier reset tokens are invalidated if I requested multiple resets (FR-26)
**And** an expired (>30 min) or already-used (single-use) token is rejected, requiring a new link.

**Given** my account is `deactivated` or `pending_approval`,
**When** I request a reset,
**Then** the system returns the uniform confirmation without sending an actionable reset or leaking account state (anti-enumeration).

**Given** any reset request or completion,
**When** the event occurs,
**Then** it is emitted to structured auth logging and audited (NFR-O1).

> **Note:** the admin-side "create one-time password" surface is a story in Epic 2 (User & Group Administration, FR-21).

### Story 1.9: Flexible User Attributes

As a system owner,
I want to store flexible attributes on a user profile without schema migration,
So that the organization can track custom, non-core metadata about volunteers as needs evolve.

**Acceptance Criteria:**

**Given** the user profile schema,
**When** a user profile is created or updated,
**Then** all known/core attributes map to first-class typed columns and extensible custom attributes are stored in the single `attributes JSONB` column (FR-7/AD-3).

**Given** I set a custom attribute on a user profile,
**When** the profile is saved,
**Then** the attribute is persisted and retrievable with no database migration (FR-7)
**And** JSONB attribute keys are unique per semantic meaning app-wide (no two modules assign a different shape to the same key, AD-3).

**Given** the profile API/adapter,
**When** attributes are read or written,
**Then** they are served/consumed through the User module's port with valid JSON serialization and validation (AD-1/AD-3).

**Given** a custom attribute later becomes core/queryable,
**When** it is promoted,
**Then** it is migrated to a real typed column via golang-migrate and backfilled, retaining the JSONB column for continued flexibility (AD-3/NFR-R2).

### Story 1.10: Dual-Admin Credential Recovery

As the system,
I want to protect the two pre-seeded admin accounts with dual-control credential recovery,
So that no single compromised admin can take over the system.

**Acceptance Criteria:**

**Given** the system starts with exactly two pre-seeded admin accounts (credentials delivered out-of-band, never in VCS, FR-27/AD-13),
**When** an admin is locked out or forgets credentials,
**Then** they cannot self-reset recovery via the password-reset flow alone (FR-26 does not apply to admin self-recovery).

**Given** admin A is locked out,
**When** admin A requests recovery,
**Then** a recovery request is created that only admin B can act on, gated by the `admin.recovery.approve` permission (FR-27/AD-12/AD-13).

**Given** admin B reviews the recovery request,
**When** admin B approves,
**Then** they must provide a mandatory Begründung and a confirmation checkbox, and on approval admin A gains access to set a new password (min 10 chars, FR-2)
**And** the approval is recorded as a high-severity, immutable audit event with actor, timestamp, and operation type (FR-27/NFR-O2).

**Given** the last remaining admin is locked out,
**When** recovery is requested,
**Then** self-recovery is disabled and the documented out-of-band manual procedure is applied, with a mandatory audit trail (FR-27/AD-13).

**Given** any admin-recovery attempt (successful or attempted),
**When** it occurs,
**Then** it is emitted to structured auth logging (NFR-O1) and audited as high-severity (NFR-O2).

### Story 1.11: Auth UX Foundation

As a user,
I want auth screens that are consistent, accessible, and clearly worded in German,
So that I can register, log in, and manage my account confidently across devices and in light or dark mode.

**Acceptance Criteria:**

**Given** the app design tokens (from DESIGN.md),
**When** the auth surfaces are built,
**Then** they consume the DESIGN.md color / typography / rounded / spacing tokens with light + `-dark` pairs (UX-DR4) and follow the component specs (buttons, input fields, status language) (UX-DR5).

**Given** I use the auth surfaces on touch or desktop across breakpoints,
**When** any auth screen is rendered,
**Then** it is responsive (mobile-first, single-column auth, container ≤ desktop max width) (UX-DR10), with touch targets ≥48px and 200% zoom without breakage (UX-DR9).

**Given** I interact with auth forms (login, registration, password reset/change, 2FA),
**When** a validation error or state changes (submitting, lockout, success),
**Then** feedback is shown as inline German text (never toast-only), with red/blue underline states per DESIGN.md input-field spec (UX-DR5/UX-DR6/UX-DR8/UX-DR9).

**Given** any auth error or confirmation,
**When** it is rendered,
**Then** it uses the anti-enumeration microcopy from UX-DR7/UX-DR8 (e.g. "Wenn deine E-Mail registriert ist, erhältst du einen Link") and never leaks account existence.

**Given** the auth screens,
**When** they are built,
**Then** they meet the accessibility floor: keyboard-operable, focus traversal in reading order, screen-reader announcements for status changes and errors, no icon-only controls (UX-DR9)
**And** the 2-Faktor, Sperre (lockout), and password surfaces match the DESIGN.md/EXPERIENCE.md IA and component behavior.

