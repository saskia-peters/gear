---
title: 'Backup Destination Configuration (admin.settings.backup)'
type: 'feature'
created: '2026-09-09'
status: 'done'
review_loop_iteration: 0
baseline_commit: '9ad87e267edad6c1aa9940506ed9b068b58c8ff4'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-3-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The backup job (NFR-R3) needs ≥1 external destination, but there is no way to configure destinations (S3-compatible/FTP/SFTP/local) at runtime — targets would otherwise be hard-coded in deploy config, requiring redeploys and risking plaintext credentials.

**Approach:** Extend the Story 3.1 Admin hexagon with a second Admin-owned settings surface: a migration-backed `backup_destinations` table (AD-15), a multi-row backup settings service (list/create/update/delete), credentials encrypted at rest and write-only/masked (NFR-S4), a "Verbindung testen" action that exercises the mechanism inline, a read-only `BackupDestinationsPort` consumer seam for the future backup job (AD-15), and a warning when fewer than one destination exists (NFR-R3). Gated by `admin.settings.backup` (AD-6).

## Boundaries & Constraints

**Always:**
- **Migration 000018** `backup_destinations.{up,down}.sql` — Admin-owned multi-row table: `id` (uuidv7), `name`, `mechanism` CHECK (`s3`|`ftp`|`sftp`|`local`), `endpoint`, `bucket_or_path`, `username`, `password_encrypted`, `schedule` (nullable, optional), `created_at`, `updated_at`. No single-row guard (multi-row by design). Down reverses.
- **sqlc.yaml:** append `000018` to the **admin** block schema list only; the **user** block stays at 000016. Re-run `just sqlc-generate`.
- **Admin core** `internal/admin/core`: add `BackupSettingsPermission = "admin.settings.backup"` Go const, `AuditOperationBackupSettingsUpdate`/`...Delete`/`...Test`, `ErrBackupDestinationsInvalid`, and a `backup.go` service: `ListBackupDestinations` (returns DTOs with `credential_configured`, never the ciphertext), `CreateBackupDestination` (encrypt-on-write), `UpdateBackupDestination` (keep-existing-credential-on-absent, atomic COALESCE), `DeleteBackupDestination`, `TestBackupDestination` (decrypt in memory, run the test port, return inline German result). Every method re-checks the live permission defense-in-depth (AD-6); writes + tests audited (NFR-O2).
- **Store port** `BackupDestinationsStore` (List/Get/Create/Update/Delete) + **consumer port** `BackupDestinationsPort` (read-only, List) in `internal/admin/ports/ports.go` — the first backup-job seam; the job reads destinations here, never a copy (AD-15).
- **HTTP** `internal/admin/adapters/http`: `backup.go` handlers under a `Routes()` mounted at `/api/v1/admin/settings/backup` behind its OWN gate `admin.settings.backup` (one permission per surface, AD-6): `GET` (list, no credentials), `POST` (create), `PUT /{id}` (update, absent credential keeps existing), `DELETE /{id}` (delete), `POST /{id}/test` (test connection). Uniform envelope + German messages, mirroring the SMTP handlers.
- **Test-connection depth (user decision, stdlib partial):**
  - `local` — full: create + delete a test file in the configured path (real round-trip).
  - `s3` — minimal hand-rolled AWS SigV4 PUT via stdlib `net/http` (no SDK): PUT a test object, verify 2xx, best-effort delete.
  - `ftp` — stdlib-only `net`: TCP dial, banner read, USER/PASS auth attempt, QUIT.
  - `sftp` — SSH auth handshake via the already-present `golang.org/x/crypto/ssh` (no NEW dep): dial, password auth, close.
  - All mechanisms bounded by a dial + protocol timeout; failures logged structured (NFR-O1), generic German message inline (no endpoint/TLS detail leak).
- **Adapter** `internal/admin/adapters/backup/tester.go` implements the `BackupDestinationTester` port used by the core (mirrors `adapters/smtp`), wired at the composition root.
- **SPA** `web/src/pages/admin/AdminEinstellungenPage.tsx` (+css) — add a Backup tab section gated by `admin.settings.backup` alongside the E-Mail tab (single route `/admin/einstellungen`, tab switch; E-Mail tab already gated by `admin.settings.email`). Backup tab: destination list + create/edit form (mechanism, endpoint, bucket/path, username, masked credential, optional schedule), "Verbindung testen" per row, delete, inline German feedback, empty-list warning "Mindestens ein Backup-Ziel ist erforderlich." (NFR-R3). New API client functions in `web/src/auth/settings.ts` following the SMTP pattern.

**Ask First:**
- None (user's "stdlib partial depth" decision + story ACs resolve the design).

**Never:**
- No plaintext or ciphertext credentials in GET responses, logs, or SPA state (NFR-S4).
- No new third-party dependency beyond the already-present `golang.org/x/crypto` (no aws-sdk, no ftp/sftp libs).
- No changes to committed migrations; user sqlc block must not gain 000018.
- No backup job implementation (out of scope — only the config surface + consumer port + test connection).
- No protocol-level file read/write for FTP/SFTP (reachability + auth handshake only, per the user decision).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| GET_LIST_EMPTY | no destinations | 200 `[]`, SPA shows ≥1 warning | n/a |
| GET_LIST | 2 destinations | 200 list, `credential_configured` per row, no ciphertext | n/a |
| CREATE_VALID | local with path + credential | 201/200 created, credential encrypted at rest, audited | n/a |
| CREATE_NO_CREDENTIAL | s3 without credential | 400 German (S3/FTP/SFTP need credentials; local optional) | 400 |
| CREATE_INVALID | bad mechanism / empty name | 400 German | 400 |
| UPDATE_KEEP_CREDENTIAL | edit without credential field | Existing encrypted credential kept (atomic COALESCE) | n/a |
| DELETE | delete destination | Row removed; deletion audited | n/a |
| TEST_OK | local path writable | Inline German success | n/a |
| TEST_FAIL | unreachable / bad auth | Inline German error; detail logged, audited | n/a |
| TEST_DECRYPT_FAIL | stored ciphertext unreadable | German error, logged | error surfaced |
| TEST_FORBIDDEN | caller lacks admin.settings.backup | Uniform 403, no destination data exposed (AD-6) | 403 |

</frozen-after-approval>

## Code Map

- `migrations/000018_backup_destinations.{up,down}.sql` -- Admin-owned multi-row table (mechanism CHECK, credential column, nullable schedule).
- `sqlc.yaml` -- append 000018 to the admin block only; keep user block at 000016; `just sqlc-generate`.
- `internal/admin/ports/ports.go` -- add `BackupDestinationsStore` (outbound, core) + `BackupDestinationsPort` (read-only consumer, AD-15) + extend `Service` with the backup methods.
- `internal/admin/core/settings.go` -- add `BackupSettingsPermission`, audit consts, sentinels; `internal/admin/core/backup.go` (new) -- list/create/update/delete/test service + validation + permission re-check.
- `internal/admin/adapters/postgres/` -- `backup_repo.go` (new) + backup queries in `queries.sql`; `just sqlc-generate` regenerates models/queries.
- `internal/admin/adapters/http/` -- `backup.go` (new) handlers + DTOs (never credentials), wired into `Routes()`; mount under its own gate.
- `internal/admin/adapters/backup/tester.go` (new) -- `BackupDestinationTester` implementing local/s3/ftp/sftp test per the depth decision.
- `cmd/server/main.go` -- wire backup store/core/tester into the Admin service; mount `/api/v1/admin/settings/backup` behind `RequireAnyPermission([...BackupSettingsPermission])`.
- `web/src/auth/settings.ts` -- backup API client functions (list/create/update/delete/test).
- `web/src/pages/admin/AdminEinstellungenPage.tsx` (+css) -- add Backup tab (list/form/test/delete, empty-list ≥1 warning), gated per tab by the two settings codes.
- `docs/docs/planning/architecture-spine.md` -- confirm AD-15 already covers the surface; add the concrete table/mechanism notes if missing.
- Tests: core (validation/permission/encrypt-keep/audit), postgres (CRUD + COALESCE round-trip against dev DB), http (200/400/403, no credential leak), tester (local round-trip, s3 sigv4 against an httptest server, ftp/sftp handshake against in-process servers), web (tab gating, form, test/delete, empty warning, 401/403).

## Tasks & Acceptance

**Execution:**
- [x] `migrations/000018_backup_destinations.{up,down}.sql` -- multi-row Admin table -- schema
- [x] `sqlc.yaml` + `just sqlc-generate` -- append 000018 to admin block; regenerate -- persistence
- [x] `internal/admin/ports/ports.go` -- BackupDestinationsStore + BackupDestinationsPort + Service methods -- hexagon boundary
- [x] `internal/admin/core/backup.go` (+settings.go consts) -- list/create/update/delete/test + encrypt-on-write + audit -- domain
- [x] `internal/admin/adapters/postgres/backup_repo.go` + queries -- CRUD + atomic COALESCE keep-existing -- persistence
- [x] `internal/admin/adapters/http/backup.go` -- handlers/DTOs mounted under admin.settings.backup gate -- API
- [x] `internal/admin/adapters/backup/tester.go` -- local/s3/ftp/sftp test connection -- mechanism
- [x] `cmd/server/main.go` -- wire backup service + tester; separate gate mount -- composition root
- [x] `web/src/auth/settings.ts` -- backup client -- SPA
- [x] `web/src/pages/admin/AdminEinstellungenPage.tsx` (+css) -- Backup tab with list/form/test/delete/≥1 warning -- SPA
- [x] Tests -- core/postgres/http/tester/web incl. I/O matrix rows -- verification
- [x] `docs/docs/planning/architecture-spine.md` -- confirm AD-15 + table notes -- docs

**Acceptance Criteria:**
- Given an admin with `admin.settings.backup`, when they open Einstellungen → Backup, then they see the destination surface: mechanism (S3-compatible/FTP/SFTP/local), endpoint/host, bucket/path, masked credentials, optional schedule (FR-29/AD-15).
- Given the admin saves a destination, then it persists in `backup_destinations` (migration-backed), credentials are encrypted at rest and write-only/masked (never displayed/logged/returned), and the surface works without redeploy (FR-29/NFR-S4).
- Given a destination is saved, when the admin presses "Verbindung testen", then a test runs against the mechanism/endpoint and the result shows inline (success or German error); failures logged structured (FR-29/NFR-O1).
- Given fewer than 1 destination exists, then the system warns that ≥1 backup destination is required (FR-29/NFR-R3).
- Given a caller lacks `admin.settings.backup`, then every backup endpoint answers uniform 403 with no destination data exposed (AD-6).

## Spec Change Log

- **Review patches applied (review 1, 2026-09-09):** FTP/SFTP credential command injection fixed — CR/LF and NUL rejected in username + any provided password (both create/update), plus NUL hardening on the other fields; S3 SigV4 signer factored into pure helpers and the in-process test server now independently re-derives and validates the signature (a tampered request → 403), with a known-answer vector pinning the signing-key chain and canonical request; `updated_at = now()` added to the update statement (+ store test asserts it advances); FTP RFC 959 multi-line replies handled; tester self-guards (s3/ftp/sftp reject empty credentials at the port boundary); handler-level test proves raw engine errors never leak inline (generic German only); explicit `clear_credential` revoke signal threaded core→store→HTTP→SPA (mutually exclusive with a new password); local tester honors ctx; `dialAddr` unit tests (default-port append, scheme rejection) exposed + fixed a latent `ftp://host` split bug; core-level duplicate-name guard on create/update. Ciphertext is base64 (AES-256-GCM via crypto.SecretCipher) — safe in the `text` column.

## Design Notes

- **Multi-row vs SMTP single-row:** the SMTP service uses a single-row upsert; backup destinations are a list (NFR-R3 ≥1). The store port is List/Get/Create/Update/Delete and the consumer port exposes List — the future backup job iterates destinations.
- **Write-only credential pattern:** same as SMTP — GET returns `credential_configured: bool` per row, never the ciphertext; create encrypts; update keeps the existing credential when the field is absent (single-statement COALESCE, atomic).
- **Test-connection is the first backup-job consumer seam:** the core calls a `BackupDestinationTester` port (implemented by `adapters/backup`) — the same shape the future job's storage adapter will use. The port carries the decrypted-in-memory credential, never the ciphertext.
- **stdlib partial depth (user decision):** local does a real write+delete round-trip; s3 a minimal hand-rolled SigV4 PUT; ftp/sftp reachability + auth handshake only (sftp reuses the already-present `x/crypto/ssh`). Document the deferred protocol-level read/write for ftp/sftp in the spec.

## Verification

**Commands:**
- `just sqlc-generate` && `just migrate-up` -- expected: 000018 applies; `\d backup_destinations`
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go/web tests pass, 0 lint issues
- `curl` as admin: GET /api/v1/admin/settings/backup (200, no credentials); POST create (200); POST /{id}/test (inline result); DELETE (200) -- expected per matrix
- `curl` as non-admin: GET -- expected: uniform 403 with no destination data
- `npx vitest run` in web/ -- expected: all pass incl. backup tab tests

**Manual checks (if no CLI):**
- Einstellungen → Backup renders the list + form with masked credentials; create/test/delete give inline German feedback; empty list shows the ≥1 warning; E-Mail tab still works when only `admin.settings.email` is held.