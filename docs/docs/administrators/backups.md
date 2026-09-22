---
sidebar_position: 5
---

# Backups & Restore

G.E.A.R. ships an **automated backup job** (NFR-R3) and a **tested restore
procedure** — so the data is never a single point of failure.

## Backup destinations

Admin-configured destinations live in **Einstellungen → Backup**:

| Mechanism | Purpose | Shipping in V1 |
|---|---|---|
| **local** | a path on the server's filesystem | ✅ real dated dump file |
| **s3** | an S3-compatible object store | ✅ real dated dump object |
| **ftp / sftp** | reachability + auth handshake | ⚠️ reachability-tested; file shipping not in V1 |

Credentials are stored **encrypted at rest** and are **write-only/masked** —
a client only ever sees a `credential_configured` boolean (NFR-S4). A
**"Verbindung testen"** action verifies each destination inline.

<figure className="gear-shot gear-shot-frame-desktop">
  <img src="/gear/screenshots/screenshot-3-backup-desktop.png" alt="The backup destinations settings" loading="lazy" />
  <figcaption>Admin — backup destinations.</figcaption>
</figure>

## The automated job

- Runs **shortly after server start** and then every `backup_interval`
  (default daily).
- Produces a **custom-format dump** (`pg_dump -Fc`) and ships it to every
  destination whose mechanism supports real transfer.
- **Per-destination isolation** — one failing destination is logged + audited
  and the run continues; it never aborts the others.
- Every outcome is **logged structured** and **audited** (`backup.run`),
  failures at high severity — never silent.
- An **ftp/sftp** destination is logged as "configured but not shippable in
  V1" and is not a failure.
- The DB password is passed via `PGPASSWORD`, never on the command line.

## Restore

`deploy/restore.sh` restores a custom-format dump via `pg_restore`
(`--clean --if-exists --no-owner`), and `just backup-restore-proof` proves the
whole path locally: **dump → restore → verify the seeded admins → drop the
scratch**. The same body runs in CI against the postgres service container —
the restore procedure is tested, not just documented.

## Operator notes

- Retention/rotation of old dumps is **not** in V1 — the job writes dated
  artifacts per run; add a rotation job when retention policy is decided.
- The seed `backup_interval` is 86400 seconds (daily); change it in
  Einstellungen → System — the job picks it up on the next tick without a
  restart.