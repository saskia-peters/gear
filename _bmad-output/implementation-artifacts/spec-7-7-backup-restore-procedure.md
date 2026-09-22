---
title: 'Backup & Restore Procedure (NFR-R3)'
type: 'feature'
created: '2026-09-20'
status: 'done'
review_loop_iteration: 0
baseline_commit: '2ac1586916a3a58e619095cd9cd0b593bee2fc8e'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-7-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Destinations are admin-configurable (FR-29/AD-15, Story 3.2) but nothing produces or ships a database backup, and there is no documented or tested restore path. NFR-R3 requires automated backups to ≥1 configured destination, a TESTED restore procedure, and failures that are logged — never silent.

**Approach:** Add an **in-process backup job** in `cmd/server`: a background ticker (interval configurable via a new `backup_interval` app setting) that runs `pg_dump -Fc` against the DB and ships the dump to every configured destination whose mechanism supports real transfer. Per the user's stdlib-partial depth decision: **local and s3 get real file shipping** (reusing the Story 3.2 tester's SigV4 PUT + local round-trip); **ftp/sftp stay handshake-only** — the job logs them as "configured but not shippable in V1" (never silent), not as failures. Ship the job's per-destination outcome structured + audit; the dump is stored to local as a dated file and to s3 as a dated object. Add a **documented + TESTED restore procedure**: a `deploy/restore.sh` script + `just backup-restore-proof` that dumps → restores into a scratch schema → verifies → tears down.

## Boundaries & Constraints

**Always:**
- **Scheduling:** a goroutine started at the `cmd/server` composition root runs `RunBackup` once at startup (delayed a few seconds so migrations/settings are warm) and then on a ticker whose interval is the new `backup_interval` app setting (duration, seeded default e.g. `86400` = daily). The ticker is stopped on shutdown (clean context cancel). No new third-party dependency.
- **pg_dump source:** the runtime image gains the pg client (`pg_dump`). The Dockerfile's runtime stage is changed so `pg_dump` is on PATH in the app container (e.g. copy the binary + libs from a `postgres:18`-based stage, or a slim stage that installs it) — the job shells out via `exec.Command("pg_dump", "-Fc", ...)` against `GEAR_DATABASE_URL`. The custom-format dump (`-Fc`) is the standard restorable artifact.
- **Shipping depth (user decision):** the job reuses the Story 3.2 `adapters/backup.Tester` machinery — but that adapter only tests reachability. The job needs a small **store** step: for `local`, write the dump to `<bucket_or_path>/gear-<date>.dump` (create dir if needed); for `s3`, SigV4 PUT `<bucket_or_path>/gear-<date>.dump` (reuse the existing `signRequest`/SigV4 helpers); for `ftp`/`sftp`, log "configured but not shippable in V1 (handshake-only)" and continue — this is NOT a failure and does NOT fail the run. Credentials are decrypted in-memory only (NFR-S4), never logged.
- **Failure contract:** a destination that fails (local write error, s3 non-2xx, decrypt error) is logged structured (`slog.Error`, NFR-O1) + audited (`backup.run` with the destination + outcome) and the run continues to the NEXT destination — one bad target never aborts the whole run. A run with zero shippable destinations logs a warning (never silent). The job never panics.
- **Restore:** `deploy/restore.sh` takes a dump path + target DB URL and runs `pg_restore` (idempotent `--clean --if-exists`); `just backup-restore-proof` proves it locally: `pg_dump` the dev DB → restore into a scratch schema `gear_restore_proof` → assert a known row (e.g. the two seeded admins) → drop the scratch. Documented in the deployment docs (NFR-R3 "tested from initial deployment").
- **Spine rule:** no changes to destination config, the admin surface, or the tester's handshake behavior. No new third-party dependency.

**Ask First:**
- None (the three design decisions — shipping depth, in-process ticker, pg client in image — were already confirmed by the human).

**Never:**
- No new third-party dependency (pg client comes from the existing postgres image, not a new module).
- No FTP/SFTP file transfer (handshake-only stays).
- No changes to the admin backup-settings HTTP surface or the destination schema.
- No cloud-specific backup (the job runs wherever the app runs; destinations are already generic).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| RUN_OK | ≥1 shippable dest (local/s3), DB up | dump produced, shipped to each, each logged ok + audited | n/a |
| LOCAL_WRITE | local dest, path writable | dated file in path | write error → logged+audited, continue |
| S3_PUT | s3 dest, endpoint reachable | 2xx PUT of dated object | non-2xx/network → logged+audited, continue |
| FTP_SFTP_DEST | ftp/sftp dest configured | logged "not shippable in V1", NOT a failure, run continues | n/a |
| NO_DEST | zero destinations configured | run logs warning (never silent), no dump needed | n/a |
| DECRYPT_FAIL | stored credential unreadable | dest skipped, logged+audited, continue | n/a |
| PG_DUMP_FAIL | pg_dump exits non-zero | whole run logged as failed, no dest attempted | logged+audited |
| PARTIAL_FAIL | dest A ok, dest B fails | A shipped, B logged+audited, run continues | per-dest isolation |
| RESTORE_PROOF | dump file + scratch schema | pg_restore succeeds, seeded admins present, scratch dropped | restore failure → proof fails red |
| INTERVAL | `backup_interval` app setting | ticker fires every interval; startup run fires once | n/a |

</frozen-after-approval>

## Code Map

- `internal/admin/core/app_settings.go` -- add `BackupInterval time.Duration` to `AppSettings` (line 102) + a `backup_interval` catalog entry (A3-style, seeded default `86400` s, `min 1`) mirroring `backup_dial_timeout` (line 235).
- `migrations/000033_backup_interval.up.sql` (new) + `.down.sql` -- seed `backup_interval` duration 86400 (`INSERT ... ON CONFLICT (key) DO NOTHING`, matching 000026 style); down deletes the key. This is a data-only migration (no schema) — no sqlc impact.
- `cmd/server/main.go` -- the composition root: after `adminSettingsService` (line 135) and the pool, start the backup job goroutine. New small `backupjob` package (or a `RunBackup` helper in `internal/platform/backupjob`):
  - resolves `BackupInterval` from the `AppSettingsPort`,
  - `exec.Command("pg_dump", "-Fc", "--no-owner", "--no-acl", dsn)` streaming to a temp file,
  - for each destination (via `BackupDestinationsPort`), decrypts + ships per mechanism,
  - logs + audits per destination (`AuditOperationBackupRun` new const in `internal/admin/core`),
  - `ticker` loop with a startup run; cancel on ctx done.
- `internal/admin/core/backup.go` -- new `AuditOperationBackupRun = "backup.run"` const; nothing else (the core already exposes `CurrentBackupDestinations` at line 124 and the cipher is reachable via the existing `SecretCipher` wiring in main.go:122).
- `internal/admin/adapters/backup/tester.go` -- the SigV4 helpers (`signRequest`, `sigV4CanonicalRequest`, ... lines 189-249) are reused by the job's s3 store step. Add small store helpers OR a new `store.go` in the same package: `StoreLocal(ctx, path, data)` and `StoreS3(ctx, params, key, data)` (both reachable/unit-testable like the tester).
- `Dockerfile` -- runtime stage gains `pg_dump` (e.g. a `postgres:18` stage copying `/usr/bin/pg_dump` + its lib deps, or a slim runtime that installs `postgresql-client`); `just container-build` re-verifies.
- `deploy/restore.sh` (new) -- `pg_restore --clean --if-exists --no-owner -d "$DB_URL" "$DUMP"`; documented in `docs/docs/planning/deployment-ionos.md` (a new "Backup & Restore" runbook section).
- `justfile` -- `backup-restore-proof` recipe: dump dev DB → restore into `gear_restore_proof` → assert the 2 seeded admins via psql → drop the scratch (mirrors `deploy-local-proof` style).
- Tests: `internal/admin/adapters/backup/store_test.go` (local write + s3 PUT with a fake HTTP server); `internal/platform/backupjob/*_test.go` (a `fakeDestinations`, `fakeDumper`, `fakeCipher` drive RUN_OK / PARTIAL_FAIL / FTP_SFTP_DEST / NO_DEST / PG_DUMP_FAIL / DECRYPT_FAIL); `cmd/server` composition test asserting the job starts with the interval setting. Docs: deployment runbook + `just backup-restore-proof` is the NFR-R3 "tested" evidence.

## Tasks & Acceptance

**Execution:**
- [x] `internal/admin/core/app_settings.go` -- `BackupInterval` field + `backup_interval` catalog entry -- settings
- [x] `migrations/000033_backup_interval.up.sql` + `.down.sql` (new) -- seed duration 86400 -- schema
- [x] `internal/admin/core/backup.go` -- `AuditOperationBackupRun` const -- core
- [x] `internal/admin/adapters/backup/store.go` (new) -- local + s3 store helpers reusing SigV4 -- adapter
- [x] `internal/platform/backupjob` (new) -- the run + ticker loop (fake-friendly ports) -- job
- [x] `cmd/server/main.go` -- start the job goroutine with the interval setting -- wiring
- [x] `Dockerfile` -- add pg_dump to the runtime image -- image
- [x] `deploy/restore.sh` (new) + deployment docs runbook section -- restore procedure
- [x] `justfile` -- `backup-restore-proof` recipe -- proof
- [x] Tests (store + job + composition) + run the proof -- coverage

**Acceptance Criteria:**
- Given the app running with `backup_interval` set, when the job fires, then it dumps via pg_dump and ships to every local/s3 destination, logging + auditing each outcome; a failing destination never aborts the run (NFR-R3, never-silent).
- Given a local or s3 destination, when the job runs, then a dated dump artifact exists at the destination (file or object).
- Given an ftp/sftp destination, when the job runs, then it is logged "not shippable in V1" and the run still succeeds (no failure, no abort).
- Given `deploy/restore.sh` + a dump, when run, then the target DB is restored and (via `just backup-restore-proof`) the two seeded admins are present and the scratch is dropped.
- Given the full suites, when `go test ./...` + `just lint` + `just backup-restore-proof` run, then all pass.

## Spec Change Log

- **2026-09-20, review patches (review 1):** (a) the timer loop re-resolves `backup_interval` every tick (admin changes take effect live; a transient settings failure only affects that tick) and re-arms after each run, so runs never overlap; (b) `New` validates deps loudly + nil destinations skipped + a `recover` guard makes "never panics" true; (c) the dump streams per destination (one SHA-256 pass, `io.Reader` into store helpers) instead of buffering the whole dump in RAM; (d) local destinations no longer run credential decrypt (a rotated key no longer silently stops local backups); (e) S3 uploads honor the configurable `backup_protocol_timeout` (via a new `BackupTestParams.Timeout`) instead of the tester's fixed 10s; (f) the S3 object URL `url.PathEscape`s bucket+key; (g) one dated+sequenced artifact name per run (`gear-<sec>-<runSeq>.dump`) shared across destinations, empty local paths fail loudly; (h) the DB password is stripped from the `pg_dump`/`pg_restore` command line and passed via `PGPASSWORD`, and `restore.sh` masks it in output; (i) failed outcomes audit at `AuditSeverityHigh`; (j) `backup-restore-proof` drops the scratch on any failure + the URL rewrite is asserted; (k) the composition test asserts ≥1 artifact and settles the goroutine (no second-boundary flake); (l) `StoreLocal` `fsync`s before rename (crash-durable); (m) the anonymous-audit DB test pins non-blank `operation_detail`/`severity` persistence; (n) `deploy-local-proof` smoke-tests `pg_dump` in the image and CI runs the restore-proof body against the postgres service container (NFR-R3 evidence in the checked path); (o) the seeded `backup_interval`=86400 is pinned by a DB test. KEEP: per-destination isolation, ftp/sftp handshake-only-not-a-failure, anonymous `backup.run` audit, in-process ticker, pg client in the image.

## Design Notes

- **Reuse over new code:** the Story 3.2 tester already implements the SigV4 signing chain and the local round-trip; the job's s3 store step reuses `signRequest` (pure, known-answer-tested) and only adds the dated-key PUT; local adds the dated-file write. FTP/SFTP need no new transfer code — the existing handshake proves reachability, and the job logs the V1 limitation.
- **Dated artifacts, no rotation in V1:** `gear-<YYYYMMDDHHMMSS>.dump` per run. Retention/rotation is explicitly out of scope (a later story can add it); NFR-R3 requires backups to exist + a restore path, not retention.
- **Custom-format dump (`-Fc`):** the portable, compressed pg_restore-compatible artifact — the standard choice for a restore procedure, and what `pg_restore --clean --if-exists` consumes idempotently.
- **Startup run + ticker:** one run a few seconds after boot (so a fresh deploy gets its first backup without waiting a full interval) then every `backup_interval`. Cancel via context so `just dev`/signal handling stays clean.

## Verification

**Commands:**
- `DATABASE_URL=... go test ./internal/admin/... ./internal/platform/backupjob/... ./cmd/...` -- expected: all pass (job matrix rows covered)
- `just lint` -- expected: 0 issues
- `just backup-restore-proof` -- expected: dump → restore → seeded admins verified → scratch dropped (NFR-R3 tested-from-deployment evidence)
- `just container-build` -- expected: image builds with pg_dump present
- Negative check: configure an unreachable local dest + a good one → the run ships the good one, logs+audits the bad one, exits 0.

**Manual checks (if no CLI):**
- Inspect the compose app container: `pg_dump` present; the job log shows per-destination `backup.run` audit + structured outcome after a run.

## Suggested Review Order

**Entry point — the backup job**

- Read first: the loop — startup delay, per-tick interval re-resolution, re-arm-after-run, in-flight guard, panic recovery
  [`job.go:222`](../../internal/platform/backupjob/job.go#L222)
- The run: list destinations → no-dest/shippable guards → streamed dump → per-destination shipping with strict isolation
  [`job.go:300`](../../internal/platform/backupjob/job.go#L300)
- Per-destination: local/s3 ship, ftp/sftp handshake-only-not-a-failure, decrypt only for s3, per-destination audit severity
  [`job.go:416`](../../internal/platform/backupjob/job.go#L416)

**Secret hygiene**

- pg_dump with the password stripped to PGPASSWORD (never in /proc/ps)
  [`job.go:101`](../../internal/platform/backupjob/job.go#L101)
- redactDSN: the URL-password strip (NFR-S4)
  [`job.go:118`](../../internal/platform/backupjob/job.go#L118)

**Storage adapters**

- StoreLocal: atomic temp+rename with fsync (crash-durable), MkdirAll
  [`store.go:23`](../../internal/admin/adapters/backup/store.go#L23)
- StoreS3: SigV4 PUT, url.PathEscape, configurable timeout, streamed body
  [`store.go:71`](../../internal/admin/adapters/backup/store.go#L71)

**Composition root**

- startBackupJob wiring (settings + destinations + cipher + audit + PgDumper)
  [`main.go:66`](../../cmd/server/main.go#L66)

**The NFR-R3 evidence**

- Composition test: job starts with the interval setting (settle + ≥1 artifact, no flake)
  [`main_test.go:2213`](../../cmd/server/main_test.go#L2213)
- Migration-033 seed pinned (backup_interval = 86400)
  [`app_settings_test.go:152`](../../internal/admin/adapters/postgres/app_settings_test.go#L152)
- Restore proof runs in the checked CI path against the postgres service container
  [`ci.yml:93`](../../.github/workflows/ci.yml#L93)