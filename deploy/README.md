# deploy — G.E.A.R. deployment runbooks (Story 7.6)

This directory holds the deployment-side artifacts for running the G.E.A.R.
container in production-shaped setups. The deployment is **registry-driven and
portable**: build the image once, push to any Docker v2 registry, and the host
pulls it (it never builds). Secrets live in a `0600 .env` on the host —
**no cloud-specific secret store** (NFR-S4). Postgres runs as an internal-only
companion container (never published to the host/edge).

## The portable unit

- `../Dockerfile` — multi-stage image (vite → Go binary → minimal runtime) that
  serves BOTH the API and the SPA (`GEAR_WEB_DIST`). One image, no Vite at runtime.
- `../compose.yaml` — local dev stack (db + app, `build: .`).
- `compose.prod.yaml` — the **deployed** compose: image-pinned (pull, never
  build), db internal-only (no published host port), secrets from the 0600 `.env`.
- `startup.sh` — idempotent first-boot provisioning (secrets → pull → up → healthz).

## Prove it locally (podman) — the full flow, zero cloud cost

`just deploy-local-proof` runs the SAME registry-driven flow a GCP/IONOS host
uses — a local `registry:2` container is the stand-in for IONOS's / GCP's Docker
v2 registry (identical HTTP v2 API), and it runs **`compose.prod.yaml` itself**
(the exact file `startup.sh` deploys).

```bash
just container-build          # build the image
just deploy-local-proof       # registry up → push → pull → prod compose up → healthz
curl -i localhost:8081/       # SPA
curl -i localhost:8081/api/v1/nope   # JSON 404 (never the SPA)
just deploy-local-proof-down  # teardown (stack + registry + .env.prod)
```

That proves the container + registry + `.env` + deployed-compose mechanism
end-to-end, including the postgres-internal-only posture. `tofu validate`/`plan`
(see `../infra/README.md`) cover the IaC without provisioning.

## Deploy to GCP

1. `just container-build` and push the image to a registry (GCP Artifact
   Registry, Docker Hub, or your own) — set `image_repo` accordingly.
2. Follow `../infra/README.md` — `tofu apply` provisions a single Compute
   Engine VM whose startup script installs docker + runs `startup.sh`.

## Deploy to IONOS (favourite) or any Docker host

IONOS provides a **Private Container Registry** (Docker Registry HTTP v2,
RBAC, robot-account tokens, ~$0.048/GB/30d, FRA region) — the same `docker push`
/ `docker pull` flow. Smallest fit: **Basic Cube XS** (1 vCPU / 2 GB / 60 GB,
~$5.76/30d); Cube S (2 vCPU / 4 GB) is the comfortable option. The pull-only
deployment is light enough for 2 GB at V1 scale.

```bash
# On the IONOS VM (provision via DCD / API / Terraform ionoscloud provider):
# 1. install docker + compose plugin
# 2. copy the repo to /opt/gear  (compose.yaml + deploy/startup.sh)
# 3. run the shared first-boot provisioning:
GEAR_APP_DIR=/opt/gear \
GEAR_IMAGE_REPO=<ionos-registry-fqdn>/gear:tag \
GEAR_APP_ORIGIN=https://gear.example.org \
bash /opt/gear/deploy/startup.sh
```

`startup.sh` is idempotent: it generates `/opt/gear/.env` (0600, random DB
password + `GEAR_ENCRYPTION_KEY`) only if absent, pulls the image, `compose
up -d`, and waits for `/healthz`.

## Post-deploy

- **Migrations (NFR-R2):** the deployed db has NO published host port (internal
  only), so run migrations from a one-off container on the same compose network:
  ```bash
  cd /opt/gear && docker compose -f deploy/compose.prod.yaml run --rm --no-deps \
    --entrypoint sh migrate-tool -c \
    'go run -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.1 \
     -path ./migrations --database "$GEAR_DATABASE_URL" up'
  ```
  (or exec into the db container and run SQL against it). The app container
  does NOT embed a migrate binary in this story.
- **Dual-admin bootstrap (AD-13):** the two `admin` accounts are seeded by the
  cold-start migration; their passwords are set out-of-band (NFR-S4) — never in
  VCS. Use a secure admin bootstrap step (e.g. a scoped one-off) to set them.
- **`.env` regeneration caveat:** `startup.sh` only generates `.env` when it is
  absent. If you ever delete it, the DB password changes but the persistent
  `gear_prod_pgdata` volume keeps the old one — wipe the volume
  (`docker compose -f deploy/compose.prod.yaml down -v`) when regenerating.
- **Backups (NFR-R3):** see Story 7.7; destinations are already admin-configurable
  (FR-29/AD-15). The app container ships `pg_dump` (the in-process backup job
  shells out to it on the `backup_interval` setting). See the **Backup & Restore
  runbook** below.
- **TLS (NFR-S1):** terminate TLS at the edge/CDN layer (Cloudflare or bunny.net,
  spine candidate) in front of the app port; the VM firewall + compose are ready
  for it. Plain-language setup for both providers (with our `sassisuperdomain.de`
  domain) is documented in `docs/docs/planning/deployment-ionos.md` §6.

## Backup & Restore runbook (Story 7.7, NFR-R3)

Backups are **automated by the app itself**: the backup job runs in the app
container, dumps the database (`pg_dump -Fc`, custom format) and ships the dated
artifact `gear-<YYYYMMDDHHMMSS>.dump` to **every configured destination** whose
mechanism supports real transfer in V1 — **local** (a dated file in the
configured path) and **s3** (a dated object via SigV4 PUT). FTP/SFTP stay
**handshake-only** in V1: the job logs them as "configured but not shippable in
V1" (never silent, never a failure). The job runs once a few seconds after boot
and then every `backup_interval` seconds (Einstellungen → System, seeded
86400 = daily). Every per-run and per-destination outcome is logged structured
and audited as `backup.run`; a failing destination never aborts the run.

**Configure the destinations** via the Admin UI (Einstellungen → Backup), then
watch the app log for the per-run `backup.run` lines.

**Tested restore procedure** — `just backup-restore-proof` (dev, needs the dev
DB up) dumps the dev DB, restores it into a throwaway scratch database
`gear_restore_proof` through the **real** `deploy/restore.sh`, asserts the two
seeded admins are present and drops the scratch:

```bash
just backup-restore-proof
```

**Restoring a backup manually** (e.g. after a disaster): run `deploy/restore.sh`
on a machine with the postgres client tools. It is idempotent
(`pg_restore --clean --if-exists`) and restores the custom-format dump into the
target database:

```bash
bash deploy/restore.sh /path/to/gear-20260921120000.dump 'postgres://gear:...@db:5432/gear?sslmode=disable'
```

> Restoring into the **running** app's database while the app is up is
> deliberate: stop the app first (`docker compose stop app`), restore, then
> start it — the backup job uses `pg_dump`, which needs a live connection, so
> `--clean` on an idle target is the safe path. Retention/rotation of old dumps
> is out of scope for V1 (a later story adds it).