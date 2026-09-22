---
title: G.E.A.R.
status: draft
created: 2026-08-29
updated: 2026-08-29
---

# PRD: G.E.A.R.

## 0. Document Purpose
This Product Requirement Document (PRD) defines the scope, functional requirements, and non-functional guidelines for the first release (V1) of the **G.E.A.R.** It is designed for product managers, system architects, software developers, and quality assurance engineers to guide design, implementation, and verification. The document is structured with glossary-anchored terms, globally numbered functional requirements, inline assumptions (indexed in Section 9), and explicit MVP boundaries. This V1 specification builds directly on the initial [Product Brief](file:///home/saskia/devprojects/gear/_bmad-output/planning-artifacts/briefs/brief-app-ovsingen-2026-08-29/brief.md).

## 1. Vision
In the German Federal Agency for Technical Relief (THW) Ortsverband Singen, the operational readiness of specialized tools and equipment is a matter of life-saving importance. Yet, managing periodic equipment inspections, verifying personnel qualifications (such as chainsaw or generator operation), and tracking tool statuses has historically been manual and fragmented. The **G.E.A.R.** modernizes and centralizes this process, ensuring that every tool in the Ortsverband is inspected on time, by qualified volunteers, with transparent and immediate tracking.

Crucially, the tool maintenance module is only the first component of a broader, extensible **G.E.A.R. Ecosystem**. The application's architecture is designed from day one with a strict focus on **modularization** and decoupling. The user management directory, authorization framework, and shared data persistence layers are built as a standalone, reusable foundation. This ensures that future Ortsverband modules (e.g., event scheduling, vehicle logs, or operational reporting) can be seamlessly integrated into the ecosystem without refactoring the core directory.

For volunteers (**Helfer*in**), it offers a simple, mobile-friendly interface to quickly log inspections. For leadership (**Fuehrung**), it provides an immediate dashboard of overall equipment health. For administrators (**Admin**), it serves as a central registry to manage permissions and configure inspection parameters. By enforcing safety standards and qualification checks, the app directly contributes to operational safety while maintaining strict compliance with European data privacy laws (DSGVO) on a robust, future-proof software architecture.

## 2. Target User

### 2.1 Jobs To Be Done
* **Helfer*in (Volunteer/Helper) [Joshi]**
  * *Functional:* Quickly determine which tools need inspection, verify if my qualifications permit me to inspect them, and complete inspections on a mobile device at the warehouse.
  * *Emotional/Social:* Feel safe knowing the tools I carry into a crisis are fully certified and operational.
* **Fuehrung (Leadership) [Nico]**
  * *Functional:* Maintain a clear, real-time dashboard of all equipment inspection statuses to ensure regulatory and operational readiness.
  * *Emotional/Social:* Confidently verify compliance and reduce operational risks without chasing down volunteers or paperwork.
* **Admin (Administrator) [Saskia]**
  * *Functional:* Securely approve new accounts, assign groups and safety qualifications, and configure tool inspection schedules/checklists.
  * *Emotional:* Keep user access and directory configurations clean and secure with minimal administrative effort.
* **Schirrmeister (Equipment Caretaker) [Torsten]**
  * *Functional:* Maintain the equipment catalogue — add/import tools, define tool types and their inspection checklists — and review inspection history to keep the on-site tools inspection-ready.
  * *Emotional:* Feel confident the equipment he is responsible for is fully catalogued, inspected on time, and documented for the Ortsverband.

### 2.2 Non-Users (V1)
* **External THW Members:** Members of other Ortsverbände (outside of Singen) are out of scope for V1.
* **External Repair Contractors:** The application does not handle tracking or direct communication with external service technicians.
* **Public / Unauthenticated Users:** Anyone without an approved, authenticated account is barred from accessing the directories or status dashboards.

### 2.3 Key User Journeys
* **UJ-1. Joshi performs a qualified chainsaw inspection**
  * **Persona + context:** Joshi is a volunteer who is certified to operate chainsaw equipment and wants to complete the scheduled inspection for chainsaw SG-01.
  * **Entry state:** Authenticated on his mobile phone via secure email-based login.
  * **Path:** He opens the app, views the dashboard, and notices chainsaw SG-01 is colored **Orange** (upcoming inspection due in 5 days). He taps the tool, reads that it requires a "Chainsaw Certificate" (which is linked to his account), and taps "Start Inspection". Because chainsaws require a checklist inspection, he answers the 3 checkbox questions (chain tension, spark plug condition, chain brake functionality).
  * **Climax:** He checks "Pass" on all items and taps Submit. The chainsaw status immediately turns **Green** on the dashboard, and a successful inspection log is written.
  * **Resolution:** Joshi returns to the dashboard knowing the tool is safe for the next 12 months.
  * **Edge case:** If Joshi flags a check item (e.g., chain brake) as "Fail", the app forces him to enter a description of the defect, automatically transitions the tool's status to **Red** ("Out of Service"), and registers a failure event.

* **UJ-2. Nico audits the Ortsverband's operational readiness**
  * **Persona + context:** Nico, the commander, needs to verify if all critical rescue equipment is ready before a training exercise.
  * **Entry state:** Authenticated on his tablet device.
  * **Path:** He opens the dashboard, filters the equipment list by "Out of Service" (Red), and spots a generator that failed its inspection yesterday. He taps it to see who performed the inspection (Joshi) and reads the logged defect comments.
  * **Climax:** Nico exports a PDF status report showing the active list of "Out of Service" and "Due Now" tools to hand to the maintenance officer for quick remediation.
  * **Resolution:** Nico plans the exercise with the remaining operational tools, knowing exactly which ones are safe to use.

* **UJ-3. Saskia approves a new helper and assigns their safety qualifications**
  * **Persona + context:** Saskia, the admin, needs to approve a new recruit's account and grant him permission to inspect tools.
  * **Entry state:** Authenticated on her desktop admin console.
  * **Path:** She logs in to the isolated admin module and sees user "Tim" has self-registered and is waiting for approval. She reviews his email, clicks "Approve Account", and assigns him to the "Helfer*in" user group. She then adds a qualification certificate: "Chainsaw Certificate".
  * **Climax:** Tim is approved and receives an automated confirmation email; he is now able to authenticate and immediately inherits permission to inspect chainsaws.
  * **Resolution:** Saskia logs out of the admin panel.

* **UJ-4. Torsten maintains the equipment catalogue and checks inspection readiness**
  * **Persona + context:** Torsten, the Schirrmeister, needs to add the newly delivered generator to the equipment catalogue and verify the on-site tools are inspection-ready.
  * **Entry state:** Authenticated on a tablet at the warehouse.
  * **Path:** He opens the app and adds the new generator under the existing "Generator" tool type; where the type's checklist doesn't cover the new model, he edits the tool type's inspection checklist. Right away he opens the inspection history of an older tool flagged as due to confirm when it was last passed and by whom.
  * **Climax:** The new generator appears on the dashboard with its due date set from the tool type's default schedule, and the checklist update is ready for the next inspection.
  * **Resolution:** Torsten closes the app knowing the catalogue is current and the next inspections are scheduled correctly.

## 3. Glossary
* **Helfer\*in** — A standard volunteer user who belongs to the Ortsverband Singen.
* **Fuehrung** — A leadership role with view-only oversight of tool statuses, reports, and checklist history.
* **Admin** — An administrator role with access to the isolated configuration module.
* **Schirrmeister** — The equipment caretaker role (permission group `schirrmeister`), responsible for maintaining the tool catalogue and tool types; can manage tools and tool types and view inspection history (AD-12).
* **Tool Type** — A definition category for physical equipment (e.g., chainsaw, generator) specifying default schedules and qualification requirements.
* **Tool** — An individual physical item of equipment belonging to a Tool Type.
* **Inspection Schedule** — The frequency at which a Tool must be inspected (e.g., every 12 months, or overridden to a custom interval).
* **Qualification** — A required certification or license (e.g., chainsaw certificate) linked to a user to allow inspections of specific Tool Types.
* **Out of Service** — A status flag applied automatically to any Tool that fails an inspection.
* **Status Dashboard** — The user interface displaying color-coded status states (Red/Orange/Green) for equipment.

## 4. Features
*FRs are numbered globally and permanently (FR-N). New FRs receive the next available number regardless of insertion position; cross-reference links are updated on change, FR numbers are not.*

### Epic 1: User Directory & Authentication

#### 4.1 Standalone User Directory & Authentication
**Description:**
This feature provides the security and user foundation for the entire ecosystem. It allows users to register, log in securely, configure Multi-Factor Authentication (MFA), manage their own password (change while logged in, or recover via a forgot-password reset), and assigns them to permission groups. The ecosystem boots with two pre-seeded admin accounts whose recovery is protected by dual control. To keep this module modular and reusable, the user database and authentication systems are isolated from the tool maintenance business logic.

**Functional Requirements:**

#### FR-1: Email-Based Authentication
Any user can authenticate using their registered email address and secure password.
- **Consequences (testable):**
  - System returns a secure session/token on correct credentials.
  - System returns HTTP 401 on incorrect credentials.

#### FR-2: Password Policy
The system enforces a minimum password length without requiring arbitrary character-type complexity rules.
- **Consequences (testable):**
  - Registration or password change requests with passwords shorter than 10 characters are rejected with a clear validation error.
  - Passwords of 10 or more characters are accepted regardless of character composition.

#### FR-3: Progressive Login Lockout
To defend against brute-force attacks, the system enforces progressive, time-based login lockouts on repeated authentication failures for a given account.
- **Consequences (testable):**
  - After 3 consecutive failed login attempts, the system blocks further login attempts for that account for 30 seconds (returns HTTP 429).
  - After 4 consecutive failed login attempts, the block duration increases to 60 seconds.

#### FR-4: Multi-Factor Authentication (MFA)
Approved users can optionally configure and enforce Time-based One-Time Password (TOTP) MFA via an authenticator app.
- **Consequences (testable):**
  - If MFA is enabled on a user profile, the login flow requires a valid 6-digit TOTP code after password validation.
  - Login attempts with invalid or expired TOTP codes are rejected.

#### FR-5: Self-Registration and Admin Approval
New volunteers can request account registration via the login interface.
- **Consequences (testable):**
  - Self-registered accounts are created in a `pending_approval` state.
  - Users in `pending_approval` state are blocked from authenticating.
  - Only an **Admin** can approve accounts, changing their state to `active`.

#### FR-6: Group & Permission Management
Admins can assign users to User Groups, and map individual permissions or Permission Groups (e.g., `Helfer*in`, `Fuehrung`, `Admin`) to users or groups.
- **Consequences (testable):**
  - Access to APIs, administrative modules, and frontend pages is strictly controlled using the resolved active permission set of the logged-in user.

#### FR-7: Flexible User Attributes
System administrators can store arbitrary, flexible metadata attributes on user profiles for future extensions.
- **Consequences (testable):**
  - The user database schema supports dynamic metadata attributes (e.g., via a JSON field) without requiring database migration for new attributes.

### Epic 2: Tool Maintenance Module

#### 4.2 Tool Maintenance Module
**Description:**
This is the first functional module of the G.E.A.R. Ecosystem. It handles the full lifecycle of equipment inspection within the Ortsverband. Qualified **Helfer\*in** and **Fuehrung** can submit inspections (pass/fail or checklist-based, depending on Tool Type configuration), with the system enforcing qualification gating. Tool statuses are automatically maintained, including Out of Service flagging on failed inspections. **Fuehrung** can additionally view the Status Dashboard, reinstate tools, and export reports. Realizes UJ-1 and UJ-2.

**Functional Requirements:**

#### FR-8: Tool Type Management
Admins can create and edit Tool Types, each defining a name, a default Inspection Schedule (chosen from the named schedule catalog, FR-30), the required Qualification to inspect it, and the inspection mode (pass/fail or checklist).
- **Consequences (testable):**
  - A Tool Type can be saved with all four attributes populated.
  - The inspection mode selection (pass/fail vs. checklist) persists and controls the inspection UI for all Tools of that type.

#### FR-9: Tool Management
Admins can create and edit individual Tools belonging to a Tool Type, optionally choosing a specific Inspection Schedule (FR-30) that overrides the Tool Type's default. Tools can be created manually or imported in bulk via CSV upload.
- **Consequences (testable):**
  - A Tool with an individually chosen schedule uses that schedule (not the Tool Type default) when calculating inspection due dates.
  - A CSV import with valid columns creates the corresponding Tool records.
  - A CSV import with malformed rows returns a clear per-row error report without partial silent failures.

#### FR-10: Flexible Attributes on Tools & Tool Types
Both Tools and Tool Types support arbitrary custom metadata attributes, using the same JSON field mechanism as FR-7.
- **Consequences (testable):**
  - A custom attribute set on a Tool Type does not require a database migration.
  - A custom attribute set on an individual Tool does not require a database migration.

#### FR-11: Qualification-Gated Inspection
**Helfer\*in** and **Fuehrung** users may submit an inspection for a Tool only if their profile holds the Qualification required by that Tool's Tool Type. Realizes UJ-1.
- **Consequences (testable):**
  - A user without the required Qualification cannot initiate an inspection; the system returns a clear access error.
  - A Helfer\*in or Fuehrung user with the required Qualification can initiate and submit the inspection.

#### FR-12: Checklist Inspection
Where a Tool Type is configured for checklist mode, the inspection UI presents the configured checklist items. Each item stores an individual pass/fail result. Realizes UJ-1 (edge case).
- **Consequences (testable):**
  - All checklist items must be answered before the inspection can be submitted.
  - The individual result per checklist item is persisted and visible in inspection history.

#### FR-13: Pass/Fail Inspection
Where a Tool Type is configured for pass/fail mode, the inspection UI presents a simple pass/fail toggle with an optional notes field.
- **Consequences (testable):**
  - The inspection can be submitted with a pass or fail result and an optional text note.

#### FR-14: Out of Service Flagging
Any inspection where any item (checklist item or pass/fail result) is marked fail automatically transitions the Tool's status to Out of Service and records the failure event with defect details. Realizes UJ-1 (edge case).
- **Consequences (testable):**
  - A failed inspection sets the Tool status to Out of Service immediately upon submission.
  - A Tool flagged Out of Service appears as Red on the Status Dashboard.
  - The failure event record includes: inspector, timestamp, failed item(s), and any defect notes.

#### FR-15: Out of Service Reinstatement
**Fuehrung** and **Admin** users can manually clear the Out of Service status on a Tool, reinstating it to Usable. The reinstatement is logged as an event with a mandatory reason note.
- **Consequences (testable):**
  - A Fuehrung or Admin user can trigger reinstatement on any Tool currently in Out of Service status.
  - Reinstatement requires a non-empty reason/notes field before it can be confirmed.
  - Upon reinstatement, the Tool's Inspection Schedule clock **resets from the reinstatement date** — the next due date is calculated as reinstatement date + Inspection Schedule interval.
  - The Tool's resulting status (Green, Orange, or Red) is derived from this new due date immediately.
  - The reinstatement event is recorded in the Tool's inspection history with: actor (user), timestamp, and reason note.
  - A Helfer\*in user cannot trigger reinstatement.

#### FR-16: Color-Coded Status Dashboard
All authenticated users can view the Status Dashboard showing every Tool color-coded by inspection urgency. Realizes UJ-1 and UJ-2.
- **Consequences (testable):**
  - **Red**: Tool whose last successful inspection has expired (past due date). Out of Service tools are also displayed Red.
  - **Orange**: Tool whose inspection is due within the next 14 calendar days.
  - **Green**: Tool whose inspection is current (due date is more than 14 days away).
  - Dashboard can be filtered by status (Red / Orange / Green / Out of Service).

#### FR-17: Status Report Export (PDF)
**Fuehrung** and **Admin** users can export a PDF report of the current Status Dashboard, including the filtered list of Tools with their statuses, last inspection date, and inspector. Realizes UJ-2.
- **Consequences (testable):**
  - Exporting generates a downloadable PDF containing the currently visible/filtered tool list.
  - The PDF includes: Tool name, Tool Type, current status (Red/Orange/Green/Out of Service), last inspection date, and name of last inspector.

#### FR-18: Inspection History
Full inspection history per Tool is stored and accessible to **Fuehrung** and **Admin** users. Realizes UJ-2.
- **Consequences (testable):**
  - Each inspection record stores: inspector (user reference), timestamp, overall result, inspection mode, and individual checklist item results if applicable.
  - History is displayed in reverse-chronological order per Tool.

### Epic 3: Admin & Configuration Module

#### 4.3 Admin & Configuration Module
**Description:**
The admin module is a separate, isolated interface within the ecosystem, accessible exclusively to users holding the Admin permission. It exposes all configuration surfaces: user approvals, group and qualification management, Tool Type and Tool configuration, **operational settings (SMTP/email delivery FR-28, backup destinations FR-29, and the named schedule catalog FR-30)**, and DSGVO compliance operations. Realizes UJ-3.

**Functional Requirements:**

#### FR-19: Admin Module Access Isolation
The admin panel and all its routes are accessible only to users with the explicit Admin permission.
- **Consequences (testable):**
  - All admin routes return HTTP 403 for any non-Admin authenticated user.
  - The admin module's existence is not exposed (no links, menu entries, or UI hints) to non-Admin users.

#### FR-20: User Approval Workflow
Admins can view a list of users in `pending_approval` state and approve or reject individual registrations. Realizes UJ-3.
- **Consequences (testable):**
  - Approving a user transitions their state to `active` and permits authentication (feeds into FR-5).
  - Rejecting a user removes the pending account record.

#### FR-21: User & Group Administration
Admins can create, edit, and deactivate user accounts, create and edit User Groups, and assign users to groups.
- **Consequences (testable):**
  - A deactivated user account cannot authenticate.
  - Removing a user from a group revokes the permissions inherited from that group immediately.

#### FR-22: Qualification Management
Admins can create Qualifications and assign or revoke them on individual user profiles. Realizes UJ-3.
- **Consequences (testable):**
  - A Qualification assigned to a user profile immediately grants them the ability to inspect the corresponding Tool Type (feeds into FR-11).
  - A revoked Qualification immediately removes that inspection right.

#### FR-23: Tool & Tool Type Configuration
The admin module exposes full Tool Type and Tool management (FR-8 and FR-9), including checklist item definition per Tool Type and CSV bulk import.
- **Consequences (testable):**
  - Checklist items added to a Tool Type appear on the inspection form for all Tools of that type.
  - Checklist items removed from a Tool Type no longer appear on future inspections (historical records are not altered).

#### FR-24: DSGVO Compliance Operations
Admins can generate a user data access report and execute an account deletion ("right to be forgotten") workflow.
- **Consequences (testable):**
  - The access report lists all personal data held for a given user (profile fields, login history, qualifications).
  - Account deletion purges all personal data from the user record (name, email, qualifications, login metadata).
  - Inspection history records linked to a deleted user are **retained** but anonymized: the inspector reference is replaced with the string `"Deleted User"` and no personally identifying data is preserved in the record.
  - A deleted account cannot be re-activated; if the same person re-joins, they must self-register again.

#### FR-25: Change Own Password (Authenticated)
Any logged-in user can change their own password without administrator involvement.
- **Consequences (testable):**
  - The user must confirm their **current** password before the change is accepted.
  - The new password must satisfy the password policy (≥ 10 characters, FR-2); violations are rejected with a clear validation error.
  - A successful password change invalidates all of the user's other active sessions (server-side) and forces a re-login.
  - The change is recorded in the audit log (NFR-O1) with actor and timestamp, but the password itself is never stored or logged in plaintext.

#### FR-26: Self-Service Password Reset (Forgot Password)
An authenticated-agnostic password recovery flow lets a user who has forgotten their password regain access without administrator involvement.
- **Consequences (testable):**
  - The login interface offers a "Forgot password?" flow that accepts the user's email address.
  - If the email matches an existing `active` account, the system sends a **transactional email** containing a single-use, expiring reset link/token.
  - A reset token is valid for at most 30 minutes, can be used only once, and is invalidated immediately upon use or expiry.
  - The flow **does not reveal whether an email exists** in the system (uniform response for unknown email) to avoid account enumeration.
  - Multiple outstanding reset requests for the same account invalidate earlier tokens (only the latest token is valid).
  - The system logs the reset request and completion to the audit log; it does not notify the user's other sessions automatically.
  - Note: this transactional account-recovery email is **distinct from, and does not reopen, the out-of-scope automated inspection/notification emails** (see §5, §6.2).

#### FR-27: Admin Credential Recovery (Dual-Control)
The system ships with **two pre-seeded admin accounts** from first deployment (the "dual admin" principle). Neither admin can reset the other's credentials unilaterally when the target is locked out or has forgotten their password.
- **Consequences (testable):**
  - Exactly two `admin`-role accounts are seeded at deployment time; their credentials are generated, distributed out-of-band to the two admin holders, and never stored in version control (NFR-S4).
  - A locked-out or forgotten-password admin cannot self-reset their admin credentials via the standard FR-26 flow alone.
  - Admin credential recovery requires **dual approval**: the second admin initiates/approves the recovery, and a recovery of an admin account is recorded as a high-severity immutable audit event (NFR-O2).
  - A recovery of the **last remaining** admin account is not permitted through a single self-service path; the operational bootstrap override is a documented, out-of-band manual procedure requiring both admins (or a designated recovery sponsor) and leaves a mandatory audit entry.
  - Password policy (≥ 10 characters, FR-2) applies to any recovered admin password.

#### FR-28: SMTP Configuration (Admin Interface)
Administrators can configure SMTP email-delivery parameters directly in the admin interface, so the Forgotten-Password emails (FR-26) can be adjusted without redeploying or editing environment configuration.
- **Consequences (testable):**
  - The admin panel exposes an email-settings surface for host, port, connection security (none/STARTTLS/TLS), sender address & display name, and username.
  - The SMTP password/secret is accepted and stored **encrypted at rest** (app-level encryption key from env/secret-manager, NFR-S4); it is **never displayed** in plaintext and is write-only in the UI (masked).
  - Saving settings persists them (migration-backed, NFR-R2) and applies to subsequent FR-26 reset emails without a redeploy.
  - The admin can trigger a **test email** to verify the saved configuration and receives a clear success/failure result.
  - Invalid or unreachable SMTP configuration surfaces a clear validation/send error rather than failing silently; any FR-26 send failure is logged (NFR-O1).
  - Access to this surface is limited to the Admin permission (FR-19) and an admin-only action permission (`admin.settings.email`).

#### FR-29: Backup Destination Configuration (Admin Interface)
Administrators can configure the automated-backup delivery targets directly in the admin interface, so backup destinations can be added or changed without redeploying or editing environment configuration.
- **Consequences (testable):**
  - The admin panel exposes a backup-destination surface where at least one destination can be configured (NFR-R3 makes ≥1 required); each destination has a **mechanism type** (S3-compatible, FTP/SFTP, local filesystem, or other), endpoint/host, bucket/path, and credentials.
  - Credentials are stored **encrypted at rest** (app-level key from env/secret-manager, NFR-S4) and are **write-only/masked** in the UI (never displayed in plaintext).
  - The admin can trigger a **"test connection"** that verifies reachability and writes a test object, returning a clear success/failure result.
  - Saved destinations are used by the backup job without a redeploy; a failed backup is logged (NFR-O1) and surfaced, never silently dropped.
  - Access is limited to the Admin permission (FR-19) and an admin-only action permission (`admin.settings.backup`).

#### FR-30: Schedule Catalog Management (Admin Interface)
Administrators manage a **named schedule catalog** directly in the admin interface. Schedules are reusable, named timing definitions (e.g. "1 year", "1 quarter", "1 month", "2 weeks", "3 days") that Tool Types use as their default Inspection Schedule and individual Tools can choose from (FR-8/FR-9). The catalog is cross-cutting — Admin-owned so future modules (e.g. vehicle logs, event scheduling) can reuse the same schedules.
- **Consequences (testable):**
  - The admin panel exposes a schedule-catalog surface to create, edit, and archive schedules. Each schedule has a **name** and a repeating **interval** (a unit + magnitude, e.g. "2 weeks").
  - Schedule rows can be assigned as the default for Tool Types and chosen per Tool; choosing/assigning an existing schedule requires no new schedule creation.
  - Schedules are **prepared for future enhancement**: each row also carries reserved **weekday** (one or several days of week) and **time-of-day** fields, similar in spirit to cron — present in the schema but **nullable and unused in V1** (no need to fill them to save a schedule). A later enhancement can activate them without a schema migration.
  - Access is limited to the Admin permission (FR-19) and an admin-only action permission (`schedules.manage`); archiving a schedule does not delete tool/tool-type history.

## 4.4 Cross-Cutting Non-Functional Requirements

### Security
* **NFR-S1: Transport Security** — All client-server communication must use TLS 1.2 or higher. Plain HTTP connections must be rejected or redirected.
* **NFR-S2: Authentication Hardening** — Session tokens must be cryptographically signed and expire after a configurable idle period (default: 8 hours). Tokens must be invalidated server-side on logout.
* **NFR-S3: Authorization Enforcement** — Permission checks are enforced server-side on every request. Frontend visibility controls are supplementary only and do not substitute server-side gating.
* **NFR-S4: Secrets Management** — No credentials, API keys, or secrets are stored in source code or version control. Secrets are injected via environment variables or a secrets manager at runtime.
* **NFR-S5: Dependency Security** — Third-party dependencies must be auditable and updated in response to known CVEs within 30 days of disclosure.

### Performance
* **NFR-P1: Response Time** — API responses for standard user interactions (dashboard load, inspection submission, status updates) must complete within 500 ms at the 95th percentile under normal load (≤ 50 concurrent users).
* **NFR-P2: Mobile Usability** — The frontend must be fully functional on mobile devices (iOS Safari, Android Chrome) without a native app. Key workflows (inspection submission, dashboard view) must be usable on a 4G connection.

### Reliability & Operations
* **NFR-R1: Containerization** — The entire application (backend, frontend, database) must be deployable as Docker containers via a single `docker-compose` configuration.
* **NFR-R2: Database Migrations** — All schema changes must be managed via versioned, incremental migration files. The system must support forward migrations; rollback procedures must be documented.
* **NFR-R3: Backup & Restore** — The system must support configurable, automated backups of the database and any media/file storage to at least one destination. A documented and tested restore procedure must exist from initial deployment.
* **NFR-R4: Availability** — Target availability for the deployed instance is 99% measured monthly, excluding scheduled maintenance windows.

### Maintainability
* **NFR-M1: Modularity** — Feature modules (User Directory, Tool Maintenance, Admin, future modules) must be decoupled at the codebase level such that a new module can be integrated without modifying existing module business logic.
* **NFR-M2: Test Coverage** — All functional requirements must have corresponding automated tests (unit and/or integration). The CI pipeline must fail on test failure.
* **NFR-M3: Linting & Code Style** — All code must pass linter checks as part of the CI pipeline. Pull requests failing lint checks must not be merged.
* **NFR-M4: Documentation** — Public-facing API contracts and module integration points must be documented in the project's documentation site. Documentation must be kept current with each release.

### Observability
* **NFR-O1: Structured Logging** — The backend must emit structured logs (JSON format) for all authentication events, permission denials, inspection submissions, Out of Service transitions, reinstatements, and DSGVO operations.
* **NFR-O2: Audit Trail** — All DSGVO-relevant operations (account deletion, data access report generation) must produce an immutable audit log entry with actor, timestamp, and operation type.

### Compliance
* **NFR-C1: DSGVO / GDPR** — The system must collect only data necessary for its stated functions (data minimization). Users have the right to access their data (FR-24 access report) and the right to erasure (FR-24 deletion workflow). Compliance mechanisms are first-class features, not afterthoughts.

### Platform
* **NFR-PL1: Browser Support** — The frontend must support the two most recent major versions of Chrome, Firefox, Safari, and Edge.
* **NFR-PL2: Form Factor** — The application is a responsive web application supporting mobile, tablet, and desktop. No native app is required for V1.

## 5. Non-Goals (Explicit)
* **No external system integrations:** No API connections to Helferportal, Hermine, THW Extranet, or any other external THW digital systems in V1.
* **No tool reservations or booking:** The app does not support tool checkout, booking, or reservation workflows.
* **No repair & issues tracking:** No workflow for logging external repair tasks, ordering spare parts, or tracking a tool's service history beyond inspection records.
* **No automated alerting:** No email or push notifications for upcoming or overdue inspections. Status visibility is provided exclusively through the color-coded Status Dashboard. *(The single, self-service **transactional** account-recovery email in FR-26 is in scope and does not reopen this non-goal — it is triggered only by an explicit user action, never automatically.)*
* **No multi-Ortsverband support:** The application serves only Ortsverband Singen. Cross-location access, shared user directories, or federated tooling are out of scope for V1.
* **Future ecosystem modules:** Vehicle logs, event scheduling, operational reporting, and other planned modules are explicitly deferred to future releases. The modular foundation is built in V1; the modules themselves are not.

## 6. MVP Scope

### 6.1 In Scope
* **Epic 1 – User Directory & Authentication:** Email login, password policy (min 10 chars), progressive lockout, optional TOTP MFA, self-registration with Admin approval, group/permission management, flexible user attributes, **self-service password management (change own password, forgot-password recovery email), dual-admin bootstrap and dual-control credential recovery** (FR-1 through FR-7, **FR-25 through FR-27**).
* **Epic 2 – Tool Maintenance Module:** Tool Type and Tool management (tool types choose a default schedule and tools may pick their own from the Admin schedule catalog, FR-30), CSV import, flexible attributes, qualification-gated inspections for Helfer\*in and Fuehrung, checklist and pass/fail inspection modes, Out of Service flagging, Out of Service reinstatement by Fuehrung/Admin (clock reset), color-coded Status Dashboard, PDF report export, inspection history (FR-8 through FR-18).
* **Epic 3 – Admin & Configuration Module:** Isolated admin panel, user approval workflow, user/group administration, qualification management, tool/tool type configuration, **SMTP/email-settings and backup-destination configuration, and the named schedule catalog** on which Epic 2's tool-type defaults and per-tool schedule choices depend (FR-19 through FR-24, **FR-28, FR-29, FR-30**).

> **Ordering note:** Epic 2's Tool Type(s) reference the schedule catalog (FR-8/FR-9). The schedule catalog (FR-30, Admin-owned) is a prerequisite slice of Epic 3 that must land before Epic 2's schedule assignment is usable; it ships with a few seeded schedules (e.g. "1 year", "1 quarter", "1 month").
* **Infrastructure:** Docker containerization, persistent database with versioned migrations, configurable automated backups and restore.

### 6.2 Out of Scope for MVP
* Future ecosystem modules (vehicle logs, event scheduling, operational reporting, etc.) — deferred to V2+.
* External API integrations — deferred to V2+. `[NOTE FOR PM]` If THW Helferportal integration becomes a strategic priority, this could move forward.
* Automated email/push notifications — deferred to V2+. *(The transactional account-recovery email for FR-26 is in scope and separate from automated notifications.)*
* Tool reservations, repair tracking — explicitly out of scope (see §5).

## 8. Open Questions
No unresolved open questions remain at this stage of the PRD. All questions raised during Discovery have been resolved:
1. ~~Should Fuehrung be able to initiate inspections?~~ → **Resolved:** Yes, Fuehrung can initiate and submit inspections if qualified (FR-11).
2. ~~What happens to the inspection clock after Out of Service reinstatement?~~ → **Resolved:** Clock resets from reinstatement date (FR-15).
3. ~~Should inspection history be preserved or deleted on account deletion?~~ → **Resolved:** Logs retained and anonymized (FR-24).
