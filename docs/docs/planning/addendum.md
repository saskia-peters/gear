# Addendum: G.E.A.R.

*This document preserves technical decisions, options considered, and implementation context that informs downstream work (architecture, solution design) but does not belong in the PRD itself.*

---

## Technology Stack Decisions

These choices were made by the product owner and recorded here for downstream architecture and development work.

| Layer | Technology | Notes |
|---|---|---|
| **Backend** | Go | Statically typed, high-performance, strong stdlib for HTTP servers |
| **Frontend** | TypeScript | Type safety for the web client; framework to be determined in architecture phase |
| **Linting** | prek | Enforced in CI pipeline (see NFR-M3) |
| **Code Storage** | GitHub | Source of truth for all code; CI/CD pipeline anchored here |
| **Documentation** | Docusaurus | Project documentation site; API contracts and module integration points published here (see NFR-M4) |

> **Note for architecture phase:** Framework choices within Go (e.g. routing library, ORM, migration tool) and TypeScript (e.g. React vs. Vue, component library) are deferred to the architecture document. The PRD's NFRs (modularity, test coverage, linting, structured logging, DB migrations) constrain those choices but do not prescribe them.

---

## Password Management Decisions

Added to support FR-25, FR-26, and FR-27 (self-service password management and dual-admin recovery).

| Concern | Decision | Notes |
|---|---|---|
| Password storage | Argon2id (memory-hard) via a maintained Go library | Never store passwords in plaintext or reversible form; hashing is out of band from the app's log data. Exact library pinned in the User epic. |
| Email sending | **SMTP-compatible transactional sender** for V1; a SaaS provider is a possible later swap. **Delivery parameters are configured in the Admin UI (FR-28)** and **owned by the Admin module** (its `smtp_settings` table); the User module's email adapter reads them via Admin's settings port | Host, port, security (none/STARTTLS/TLS), sender & display name, username, and password are editable at runtime with no redeploy. The **password is stored encrypted at rest** (app-level key from env/secret-manager, NFR-S4) and is write-only/masked in the UI. Includes a "send test email" action. Deferred: exact library/provider to the User epic. Used **only** for the FR-26 transactional reset email; no automated/notification emails in V1 (PRD §5). |
| Reset tokens | Cryptographically random, high-entropy, hashed-at-rest, single-use, 30-minute expiry | One active token per user (new request invalidates older ones). Token hash stored, not the plaintext. |
| Admin bootstrap | **Two pre-seeded `admin` accounts**, credentials generated and distributed out-of-band | Neither stored in VCS (NFR-S4). Dual-control recovery, FR-27. |
| Dual-control recovery | Second admin authorizes recovery of a locked-out/forgotten admin; recovery of the last admin is a documented manual out-of-band procedure with a mandatory audit entry | Prevents single-account takeover; FR-27. |

## Operational Settings Decisions

System-wide operational settings for V1 are **owned by the Admin module** (the module responsible for configuration). Each is a small Admin-owned table, edited once from the Admin panel, and consumed at runtime by the relevant worker/port. This covers SMTP (FR-28), backup destinations (FR-29), and the named schedule catalog (FR-30).

| Concern | Decision | Notes |
|---|---|---|
| SMTP ownership | Admin module owns `smtp_settings` (FR-28, AD-14) | Single system-wide delivery config, not user business logic. User module consumes it via Admin's settings port for the FR-26 email. |
| Backup destination ownership | Admin module owns `backup_destinations` (FR-29, AD-15) | Replaces the earlier "env-driven/deferred" stance: destinations (S3-compatible, FTP/SFTP, local filesystem) are admin-configurable, editing them needs no redeploy. |
| Schedule ownership | Admin module owns `schedules` (FR-30, AD-16) | Cross-cutting named catalog (e.g. "1 year", "1 quarter", "1 month", "2 weeks", "3 days"). Tool Types pick a default schedule and individual Tools may pick their own (FK → `schedules`); admins create/edit via `schedules.manage`. Reusable by future modules (vehicle logs, event scheduling), not tool-specific logic. |
| Schedule shape | Name + repeating interval (unit/magnitude) in V1; reserved weekday-set + time-of-day fields (cron-like) unused in V1 | Signposts future composite timing (e.g. "every Monday & Thursday at 08:00") with **no schema migration needed** — the fields exist now but are nullable/ignored (AD-16). Lenient validation: name + interval required. |
| Secrets in settings | Encrypted at rest (app-level key) + write-only/masked in UI | Applies to the SMTP password and backup credentials (NFR-S4); never returned or logged in plaintext. |
| Connectivity test | In-panel "send test email" / "test connection" actions | Verify SMTP delivery and backup reachability/write from the Admin panel. |
| Seed schedules | Ship the catalog pre-seeded ("1 year", "1 quarter", "1 month", "2 weeks", "3 days") | Lets Epic 2 assign schedules immediately; admins can add more/rename (FR-30). |
