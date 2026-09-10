# Epic 3 Context: System Configuration & Compliance

<!-- Compiled from planning artifacts. Edit freely. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Give administrators two runtime-editable operational configuration surfaces — SMTP email delivery and backup destinations — that apply immediately without redeploying, and satisfy DSGVO compliance: per-user data-access reports and irreversible account deletion that purges personal data while keeping inspection history intact and anonymized. All settings are owned by the Admin module, persist migration-backed with secrets encrypted at rest, and DSGVO operations run as single composition-root orchestrations across owning modules with immutable audit entries.

## Stories

- Story 3.1: SMTP Configuration
- Story 3.2: Backup Destination Configuration
- Story 3.3: DSGVO Data-Access Report
- Story 3.4: DSGVO Account Deletion

## Requirements & Constraints

- Every surface/action binds to exactly one permission code, re-validated server-side (AD-6): `admin.settings.email`, `admin.settings.backup`, `dsgvo.access_report`, `dsgvo.delete`. Missing permission → HTTP 403 and no data exposed; the surfaces are never surfaced to non-admins.
- SMTP (FR-28): surface shows host, port, security (none/STARTTLS/TLS), sender address & display name, username, and a masked password. Saving persists migration-backed and applies to subsequent transactional email without redeploy. Password is encrypted at rest (app-level key from env/secret-manager, NFR-S4) and write-only/masked — never displayed, logged, or returned in plaintext. A test-email action exercises the live config with an inline success or German error; send failures are logged via structured logging, never silent (NFR-O1).
- Backup destinations (FR-29): surface shows mechanism (S3-compatible / FTP / SFTP / local), endpoint/host, bucket or path, masked credentials, and an optional schedule. Credentials encrypted at rest and write-only/masked. A test-connection action verifies reachability and writes a test object, with inline result; failures logged, never silent. At least one destination is required (NFR-R3); the system warns when fewer than one exists.
- DSGVO access report (FR-24): covers the user's profile fields, login/authentication history, qualifications, group memberships, and inspection-related records; produced by orchestrating each owning module's exports through the composition root and assembled for review/download. Every generation is recorded as an immutable audit entry with actor, timestamp, and operation type (NFR-O2).
- DSGVO deletion (FR-24): irreversible, runs as one transaction orchestrating module-owning exports; personal data is purged and the account can never be re-activated (re-login permanently rejected). All inspection records stay fully intact — the inspector reference is rewritten to the literal "Deleted User" while timestamps, results, checklist items, and OOS states remain unchanged and visible in history. Recorded as an immutable audit entry.
- All DSGVO-relevant events are emitted to structured logging (NFR-O1).

## Technical Decisions

- Modular monolith, hexagon per module (AD-1); Admin lives in `internal/admin`; the composition root (`cmd/server`) wires modules and the auth gateway.
- Admin owns exactly two of its three config tables for this epic — `smtp_settings` and `backup_destinations` (AD-11/AD-14/AD-15) — persisted via the single golang-migrate schema authority; it never authors other modules' tables.
- Consumers read settings through the Admin module's exported settings port at runtime: the User module's email adapter (replacing the Epic-1 baseline sender for the FR-26 reset email) and the backup job read destinations — no copies are embedded.
- Secrets (SMTP password, backup credentials) are encrypted at rest with an app-level key from env/secret-manager and masked in the UI; never in VCS or plaintext.
- Cross-module DSGVO flows (AD-8) execute through the composition root, calling each owning module via its exported port — never by one module writing another's SQL. On deletion, the Tool module's user-lifecycle port rewrites inspector references to "Deleted User" while the User module purges personal fields; the whole flow is one transaction plus an immutable audit entry (NFR-O2).
- Server-side authorization is the only source of truth (AD-6); uniform JSON error envelope; UUID v7 PKs, UTC RFC 3339; structured JSON logging.

## UX & Interaction Patterns

- Admin nav exposes Einstellungen (E-Mail + Backup) and DSGVO → Löschen; non-admins get a Dashboard redirect with a "Zugriff verweigert" toast and no admin surfaces rendered (403).
- Settings surfaces: SMTP host/port/security/sender/username with masked password and a "Sendetest-E-Mail" action; backup mechanism/endpoint/bucket/path with masked credentials and a "Verbindung testen" action. Results appear inline (success or German error), never toast-only.
- DSGVO deletion is a heavy two-step confirm: step 2/2, the user's name typed out, a mandatory "Begründung", and "Endgültig löschen" enabled only when both are filled; language states "Unumkehrbar · Audit-Pflicht". The red danger button is reserved for this irreversible action; a typed-name mismatch shows an inline German error and blocks deletion.
- German microcopy that names the entity and consequence; inline error states (red underline/text), loading skeletons, one primary CTA per surface; light and dark mode.

## Cross-Story Dependencies

- Story 3.1's settings are consumed by Epic 1's FR-26 forgot-password email via the Admin settings port, replacing the baseline sender.
- Stories 3.3 and 3.4 rely on the User module's lifecycle authority (Epic 1/AD-2) and the Tool module's user-lifecycle and inspection ports (Epic 2/AD-8) through the composition root.
- Story 3.2's destinations feed the automated backup job (NFR-R3), which enforces ≥1 destination.
- Stories 3.3 and 3.4 share the AD-8 composition-root orchestration pattern; 3.4's anonymization depends on Epic 2's inspection-history integrity.