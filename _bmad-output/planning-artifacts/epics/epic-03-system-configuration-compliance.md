## Epic 3: System Configuration & Compliance

Administrators configure operational infrastructure and satisfy legal compliance. Admins manage SMTP email settings (test email, encrypted/masked secrets) and backup destinations (test connection, encrypted credentials); handle DSGVO operations including data-access reports and irreversible account deletion with anonymized inspection history.

### Story 3.1: SMTP Configuration (admin.settings.email)

As an admin,
I want to configure the SMTP email settings in the admin panel,
So that operational emails (e.g. password-reset links, FR-26) are delivered through a real server without redeploying.

**Acceptance Criteria:**

**Given** I have the `admin.settings.email` permission,
**When** I open "Einstellungen → E-Mail" in the admin module,
**Then** I see the email-settings surface: Host, Port, Security (none/STARTTLS/TLS), Sender address, Display name, Username, and a masked Password field (FR-28).

**Given** I save changed SMTP settings,
**When** I submit the form,
**Then** the settings persist in `smtp_settings` via a migrate-backed Admin table (AD-11/AD-14) and apply immediately without redeploy (FR-28)
**And** the password is encrypted at rest and stored write-only/masked — never displayed, logged, or returned in plaintext (FR-28/NFR-S4).

**Given** SMTP settings are saved,
**When** I press "Sendetest-E-Mail",
**Then** a test email is sent to my address through the configured server and the result is shown inline (success or German inline error) (FR-28/AD-14/UX-DR8)
**And** send failures are logged via structured logging (NFR-O1), never silent (FR-28).

**Given** SMTP settings are configured,
**When** the User module sends transactional email (e.g. FR-26 reset link),
**Then** it consumes the settings through the Admin settings port (AD-14) — the Epic-1 baseline sender is replaced by the configured server.

**Given** I lack the `admin.settings.email` permission,
**When** I attempt to access the surface,
**Then** I receive HTTP 403 and no settings data is exposed (AD-6).

### Story 3.2: Backup Destination Configuration (admin.settings.backup)

As an admin,
I want to configure backup destinations in the admin panel,
So that the automated backup job can store backups to ≥1 external destination without redeploying.

**Acceptance Criteria:**

**Given** I have the `admin.settings.backup` permission,
**When** I open "Einstellungen → Backup" in the admin module,
**Then** I see the backup-destination surface: mechanism (S3-compatible / FTP / SFTP / local), endpoint/host, bucket or path, credentials (masked), and an optional schedule (FR-29).

**Given** I save a backup destination,
**When** I submit the form,
**Then** it persists in `backup_destinations` via the Admin-owned table (AD-11/AD-15) and the backup job uses it without redeploy (FR-29)
**And** credentials are encrypted at rest and write-only/masked (FR-29/NFR-S4).

**Given** a destination is saved,
**When** I press "Verbindung testen",
**Then** a test connection runs against the configured mechanism/endpoint and the result is shown inline (success or German inline error) (FR-29/UX-DR8)
**And** failures are logged via structured logging (NFR-O1), never silent (FR-29).

**Given** the backup configuration,
**When** fewer than 1 destination exists,
**Then** the system warns that at least one backup destination is required (≥1, FR-29/NFR-R3).

**Given** I lack the `admin.settings.backup` permission,
**When** I attempt to access the surface,
**Then** I receive HTTP 403 and no destination data is exposed (AD-6).

### Story 3.3: DSGVO Data-Access Report

As an admin,
I want to generate a DSGVO data-access report for any user,
So that the organization can fulfill a user's right of access and show exactly what data is held.

**Acceptance Criteria:**

**Given** I have the `dsgvo.access_report` permission,
**When** I open "DSGVO → Datenauskunft" and select a user,
**Then** I can generate a data-access report covering the user's profile fields, login/authentication history, qualifications, group memberships, and inspection-related records (FR-24).

**Given** I generate the report,
**When** I confirm,
**Then** the report is produced by orchestrating each owning module's exports through the composition root (AD-8), assembled in one place, and presented for review/download (FR-24)
**And** the operation is recorded as an immutable audit entry with actor, timestamp, and operation type (NFR-O2).

**Given** any DSGVO-relevant event,
**When** it occurs,
**Then** it is emitted to structured logging (NFR-O1).

**Given** I lack the `dsgvo.access_report` permission,
**When** I attempt access,
**Then** I receive HTTP 403 and no personal data is exposed (AD-6).

### Story 3.4: DSGVO Account Deletion

As an admin,
I want to irreversibly delete a user's account per DSGVO right of erasure,
So that the user's personal data is purged while all inspection records stay intact.

**Acceptance Criteria:**

**Given** I have the `dsgvo.delete` permission,
**When** I open "DSGVO → Konto löschen" and select a user,
**Then** a heavy two-step delete flow appears: type the user's name, provide a mandatory Begründung, and confirm with "Endgültig löschen" (FR-24/UX-DR7/UX-DR8).

**Given** I confirm deletion,
**When** the deletion runs,
**Then** it executes as one transaction orchestrating module-owning exports (AD-8): the user's personal data is purged and the account cannot be re-activated (FR-24)
**And** **all inspection records stay fully intact** — the anonymous placeholder "Deleted User" is used in place of the personal reference, so inspection timestamps, results, checklist items, and OOS states are preserved unchanged and remain visible in history (FR-24/AD-8/FR-18)
**And** the deletion is recorded as an immutable audit entry with actor, timestamp, and operation type (NFR-O2/FR-24).

**Given** a deleted user,
**When** login or re-activation is attempted,
**Then** authentication is permanently rejected (FR-24).

**Given** I lack the `dsgvo.delete` permission,
**When** I attempt access,
**Then** I receive HTTP 403 and no personal data is exposed (AD-6).
