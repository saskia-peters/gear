---
title: 'SMTP Configuration (admin.settings.email)'
type: 'feature'
created: '2026-09-09'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'ccae51267edad6c1aa9940506ed9b068b58c8ff4'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-3-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Operational emails (FR-26 password-reset links) are never delivered — the composition root wires a `resetEmailStub` that only logs "SMTP not configured" and falls back to the `must_change_password` flag. There is no way to configure a real SMTP server without redeploying.

**Approach:** Materialize the Admin hexagon (`internal/admin`) with Story 3.1's SMTP settings: a migration-backed `smtp_settings` table (Admin-owned, AD-11/AD-14), an Admin core+ports+adapters store, a settings HTTP surface gated by `admin.settings.email`, and a real SMTP sender that replaces the Epic-1 stub so the User module's FR-26 reset email is delivered through the configured server. The password is encrypted at rest (write-only/masked, NFR-S4) and a "Sendetest-E-Mail" action exercises the live config with inline German feedback (FR-28).

## Boundaries & Constraints

**Always:**
- **Materialize `internal/admin` as a real hexagon** (AD-1): `core/` (SMTP settings service), `ports/` (settings port consumed by the User reset sender), `adapters/postgres` (sqlc-generated Admin store), `adapters/http` (settings router). A **second `sql:` block in `sqlc.yaml`** generates the Admin store (`out: ./internal/admin/adapters/postgres`). The Admin module owns `smtp_settings` and never authors other modules' tables.
- **Migration 000017** `smtp_settings.{up,down}.sql` — Admin-owned table: `id`, `host`, `port`, `security` (`none|starttls|tls`), `sender_address`, `sender_name`, `username`, `password_encrypted` (write-only, never returned), `created_at`, `updated_at`. Down reverses.
- **Settings HTTP surface** under `/api/v1/admin/settings/smtp`, gated by `admin.settings.email` (defense-in-depth re-check in the Admin core):
  - `GET` → returns host, port, security, sender_address, sender_name, username, and `password_configured: bool` (NEVER the plaintext or ciphertext).
  - `PUT` → persists the settings atomically; if `password` is present it is encrypted at rest and replaced, if absent the existing encrypted password is kept (write-only edit). Applies immediately to subsequent sends (no redeploy). Audited (`admin.settings.email.update`).
  - `POST /test` → sends a test email to the acting admin's address through the configured server; inline German success or error; failures logged structured (NFR-O1) and audited (`admin.settings.email.test`).
- **Password encryption-at-rest**: reuse `internal/platform/crypto.SecretCipher` (`EncryptSecret`/`DecryptSecret`, AES-256-GCM, `GEAR_ENCRYPTION_KEY`) — the same cipher already used for TOTP secrets. The password is write-only/masked; it is never returned by GET, never logged.
- **Real SMTP sender** replaces `resetEmailStub` at `cmd/server/main.go:87`: it implements the existing `ResetEmailSender` port (`SendPasswordResetEmail(ctx, email, resetLink)`, `Configured() bool`), reads the settings at send time via the Admin settings port, and `Configured()` returns true only when a working server is configured. Supports `none`/`STARTTLS`/`TLS` (implicit TLS via `tls.Dial` + `smtp.NewClient`, since stdlib `net/smtp` has no implicit TLS). FR-26 call site and port contract stay unchanged.
- **SPA**: replace `AdminEinstellungenPage` placeholder with the Einstellungen → E-Mail surface (host, port, security, sender, username, masked password, "Sendetest-E-Mail"), gated by `admin.settings.email`, following the existing editor/form patterns (sticky actions, server-authoritative German messages, 401→login, 403→leave module).
- **Audit (NFR-O1/O2)**: settings update + test-email audited with actor, timestamp, operation.

**Ask First:**
- None (story ACs + the user's "everything settings belongs in an Admin module" decision resolve the design).

**Never:**
- No plaintext or ciphertext password in GET responses, logs, or SPA state (NFR-S4).
- No emailing the SMTP password anywhere.
- No changes to already-committed migrations.
- No new SMTP third-party dependency (stdlib `net/smtp` + `crypto/tls` suffice).
- No caching of settings — each send reads the live row (FR-28/FR-22-style immediacy).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| GET_INITIAL | no settings row yet | Returns zero defaults, `password_configured: false`; Configured()=false → FR-26 keeps must-change fallback | n/a |
| PUT_VALID | full settings incl. password | Persisted, password encrypted at rest, applied immediately, audited | n/a |
| PUT_NO_PASSWORD | edit without password field | Existing encrypted password kept; other fields updated | n/a |
| PUT_INVALID | bad security value / empty host | 400 invalid_request, German message | 400 |
| PUT_FORBIDDEN | caller lacks admin.settings.email | Uniform 403, no settings data exposed | 403 |
| TEST_OK | configured server, admin sends test | Email delivered; inline German success; audited | n/a |
| TEST_FAIL | SMTP unreachable / auth rejected | Inline German error; failure logged structured + audited | 200-style result with error? See note |
| SEND_RESET | reset requested, settings configured | FR-26 reset email delivered via real sender (replaces stub) | logged, uniform confirmation still returned |
| SEND_RESET_UNCONFIGURED | no settings / Configured()=false | must_change_password fallback (unchanged behavior) | n/a |
| DECRYPT_FAIL | stored ciphertext unreadable | Test/send fails with German error; logged | error surfaced |

</frozen-after-approval>

## Code Map

- `migrations/000017_smtp_settings.{up,down}.sql` -- Admin-owned `smtp_settings` table (next after 000016).
- `sqlc.yaml` -- add a second `sql:` block: `out: ./internal/admin/adapters/postgres`, own `queries.sql`, same glob schema. Re-run `just sqlc-generate`.
- `internal/admin/ports/ports.go` -- materialize: the `SmtpSettingsPort` (read settings) + service port the HTTP/router and the User reset sender consume. The User module consumes it read-only (AD-14).
- `internal/admin/core/` (new) -- `settings.go`: the SMTP settings service (get/update with encrypt-on-write, keep-existing-password-on-absent, test-send). Sentinels + German messages + audit constants (`admin.settings.email.update`, `admin.settings.email.test`).
- `internal/admin/adapters/postgres/` (new) -- sqlc store: `settings.sql` (get/upsert smtp_settings), repository methods.
- `internal/admin/adapters/http/` (new) -- `settings.go`: `GET/PUT /api/v1/admin/settings/smtp`, `POST /api/v1/admin/settings/smtp/test`, gated by `admin.settings.email`; modeled on the user-module admin handlers (uniform envelope).
- `cmd/server/main.go` -- mount the Admin settings router behind a `RequireAnyPermission(..., admin.settings.email, ...)` sub-gate under `/api/v1/admin/settings`; build a real SMTP sender from the settings port and `userService.SetResetEmailSender(...)` (replace `resetEmailStub`). Wire the `SecretCipher` into the Admin core.
- `internal/user/ports/ports.go` -- `ResetEmailSender` interface unchanged (:264-271) — the new sender implements it.
- `web/src/auth/settings.ts` (new) -- `getSmtpSettings()`, `updateSmtpSettings(input)`, `testSmtpEmail()` via `request`/`authTokenHeaders` from `./http.ts`.
- `web/src/pages/admin/AdminEinstellungenPage.tsx` (+ `.module.css`) -- replace the placeholder with the E-Mail surface (fields, masked password, Sendetest-E-Mail, sticky actions, German feedback, 401/403 handling).
- Tests: Admin core (get/update/encrypt-keep/test), postgres integration (migration + store), http (200/400/403, no password in GET), web page (render, save, test-email, 401/403), and a main.go-level check that the real sender replaces the stub.

## Tasks & Acceptance

**Execution:**
- [x] `migrations/000017_smtp_settings.{up,down}.sql` -- Admin-owned table -- schema
- [x] `sqlc.yaml` + `just sqlc-generate` -- second Admin sql block + generated store -- persistence
- [x] `internal/admin/ports/ports.go` -- settings port(s) -- hexagon boundary
- [x] `internal/admin/core/settings.go` -- get/update/test-send + encrypt-on-write + audit -- domain
- [x] `internal/admin/adapters/postgres/` -- sqlc store + repo -- persistence
- [x] `internal/admin/adapters/http/settings.go` -- GET/PUT/test handlers gated by admin.settings.email -- API
- [x] `cmd/server/main.go` -- mount settings sub-gate + real SMTP sender replacing the stub -- composition root
- [x] `web/src/auth/settings.ts` -- API client -- SPA
- [x] `web/src/pages/admin/AdminEinstellungenPage.tsx` (+css) -- E-Mail settings surface -- SPA
- [x] Tests -- core/postgres/http/web incl. I/O matrix rows -- verification
- [x] `ARCHITECTURE-SPINE.md` -- note the materialized Admin hexagon + smtp_settings -- docs

**Acceptance Criteria:**
- Given an admin with `admin.settings.email`, when they open Einstellungen → E-Mail, then they see host, port, security, sender, username, and a masked password field (FR-28).
- Given the admin saves changed SMTP settings, then they persist in `smtp_settings` (migration-backed), the password is encrypted at rest and write-only/masked (never displayed/logged/returned), and they apply immediately without redeploy (FR-28/NFR-S4).
- Given SMTP is configured, when the admin presses "Sendetest-E-Mail", then a test email is sent to their address and the result shows inline (success or German error); failures are logged structured, never silent (FR-28/UX-DR8/NFR-O1).
- Given SMTP is configured, when the User module sends a transactional email (FR-26 reset link), then it is delivered through the configured server via the Admin settings port (AD-14), replacing the Epic-1 baseline stub.
- Given a caller lacks `admin.settings.email`, then every settings endpoint answers uniform 403 with no settings data exposed (AD-6).

## Spec Change Log

- **Review patches applied (review 1, 2026-09-09):** SMTP conversation deadlines + context-aware TLS dial (no stalled-send hang); `sender_name` now rendered into the RFC 5322 From header; password-less anonymous/no-auth relays are `Configured()`/sendable when no username is set (password still required with a username or implicit TLS); `Date:` + `Message-ID:` headers added; test-send surfaces a generic German message (detailed error only logged, NFR-O1); keep-existing-password update is atomic via a single COALESCE upsert; `Configured()` read wrapped in a 5s timeout; whitespace-only passwords treated as absent; HTTP handler nil-logger guarded; sqlc schema scoped per module (user store excludes smtp_settings, admin store excludes user tables — AD-8/AD-11); composition-root mount-gating test added. Also fixed a pre-existing web typecheck break (AssigneeEditor.test.tsx stale `expires_at` from the 000015 qualification rework).

## Design Notes

- **Materializing the Admin hexagon is the point of Epic 3.** Every Epic-2 admin surface was hosted in the user module; the settings stories belong to Admin. Story 3.1 stands up `internal/admin` as a real hexagon (core/ports/adapters) with its own sqlc store and HTTP router — the pattern later stories (3.2 backup, 3.3/3.4 DSGVO) build on.
- **Write-only password pattern:** GET returns `password_configured: bool`, never the ciphertext; PUT encrypts a present password and keeps the existing one when absent. Decryption happens only inside the sender/test path, in memory.
- **`Configured()` drives FR-26:** the real sender returns true only when a valid row + decryptable password exist; otherwise the existing must-change-password fallback stays active — no behavioral regression when unconfigured.
- **TLS (implicit, port 465):** stdlib `net/smtp` cannot implicit-TLS; use `tls.Dial` then `smtp.NewClient` over the TLS conn. STARTTLS uses `smtp.SendMail`/`StartTLS`; none is plain.

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go and web tests pass, 0 lint issues
- `just migrate-up` -- expected: migration 000017 applies; `\d smtp_settings`
- `just sqlc-generate` -- expected: Admin store regenerated under internal/admin/adapters/postgres
- `curl` as admin: GET /api/v1/admin/settings/smtp -- expected: 200 with no password; PUT with password -- expected: 200, GET shows `password_configured: true`; POST /test -- expected: inline result
- `curl` as non-admin: GET -- expected: uniform 403 with no admin hint
- `curl` FR-26 reset with SMTP configured -- expected: real email sent (log), no must-change fallback

**Manual checks (if no CLI):**
- Einstellungen → E-Mail in the SPA renders the surface with masked password; save + Sendetest-E-Mail give inline German feedback.