---
title: 'Deployable Container + Portable GCP IaC (NFR-R1/R2/R4, NFR-S1/S4)'
type: 'feature'
created: '2026-09-19'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'd6aa9825b7d8d2ca597d2b87262a690e367fa067'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-7-context.md'
  - '{project-root}/docs/docs/planning/architecture-spine.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** G.E.A.R. runs only as a dev stack — no Docker image, no production serving path for the SPA, and no cloud IaC — so it cannot be deployed to a cloud platform (NFR-R1/R4).

**Approach:** Make the complete app a single deployable Docker image (Go server serving the built SPA + the API, with Postgres as a companion service), add an image-pinned production compose definition, and provide **OpenTofu IaC for Google Cloud Platform** (a single Compute Engine VM running the compose stack — NO Cloud SQL, NO Cloud Run, NO Secret Manager) structured so the cloud provider is a **swappable layer**. The deployment is **registry-driven and provable locally with podman**: build → push to a Docker Registry v2 (local `registry:2` container = same contract as IONOS/GCP Artifact Registry) → the host pulls → compose up. IONOS is the portability target (its Private Container Registry is Docker v2), and the same image + compose + `.env` contract runs on any Docker-capable host (security-driven portability).

## Boundaries & Constraints

**Always:**
- **Single app image:** a multi-stage Dockerfile builds `web/dist` (vite) + the Go binary and produces one minimal runtime image. The Go server serves BOTH the API (`/api/*`, unchanged) and the built SPA (`/` + client-side fallback) from `GEAR_WEB_DIST` (default `./web/dist`). No Vite process at runtime.
- **SPA serving (Story 7.2 slice):** add a router `WithMount("/", spa)` catch-all served AFTER all `/api` mounts; non-API unknown routes fall back to `index.html` (client-side routing); `/api/*` unknown stays the JSON 404 envelope (never the SPA fallback). Static assets served with immutable caching for hashed filenames, no-store for `index.html`. Served from disk (`http.Dir` on `GEAR_WEB_DIST`) — NOT `go:embed` (embed cannot traverse up from `cmd/server` to `web/dist`).
- **Registry-driven deployment:** the app image is pushed to a Docker Registry v2 (`image_repo` input) and the host PULLS it — never builds on the host. Local proof uses a `registry:2` container on `localhost:5000` (same HTTP v2 API as IONOS/GCP). `compose.yaml` keeps `build: .` for local dev; a prod/image-pinned variant (and the local-proof variant) use `image:` + pull.
- **Secrets (portable, NO cloud secret store):** the DB password + `GEAR_ENCRYPTION_KEY` are generated at first boot by the startup script / `deploy-local-proof` recipe into a **`0600 .env` on the host** (idempotent — never regenerated if present); passed to containers via env; **never in terraform state, never in VCS**. Postgres listens compose-internal only (never published to the host/edge).
- **GCP IaC (minimal cost, portable):** OpenTofu provisions a **single Compute Engine VM** (`e2-micro` default, `machine_type` variable; optional spot/preemptible switch) + firewall (allow 80/443 edge-facing; SSH from an operator source range) + optional static IP/DNS + a minimal service account (`roles/artifactregistry.reader`). The VM runs an idempotent `startup.sh`: install docker/compose, generate `.env` if absent, `compose pull && up -d`, poll `/healthz`. **NO Artifact Registry required for the local proof**; the tf may include an optional Artifact Registry repo (variable-gated) for the GCP push path.
- **justfile recipes:** `container-build`, `container-run`, `deploy-local-proof`, `infra-validate`, `infra-plan`, `infra-apply` — consistent with the existing recipe style; migrations still run via the pinned `migrate` CLI against `DATABASE_URL` (NFR-R2).
- **Docs:** update `deploy/README.md` + `infra/README.md` from placeholders to operational runbooks (build, push, local proof, plan/apply, migrate, health check, secret provisioning; the IONOS path = provision a Cube/DCD VM + the same docker/.env/compose steps).

**Ask First:**
- Exact GCP project id / billing / region: IaC is parameterized (no hardcoded project), but `tofu plan/apply` against a real project needs the caller's project. Provisioning is NOT run here — this story delivers the container + IaC + runbooks + local proof; applying to a live cloud is the operator's step.
- Whether the app image also embeds migrations (runtime migrate on boot) vs the external `migrate` CLI (current contract). Default: keep the external CLI contract; embedding is a follow-up.

**Never:**
- No GCP SDK/client libraries or provider-specific code in `internal/` or `cmd/` (portability invariant).
- No secrets committed: no real passwords, encryption keys, or admin bootstrap credentials in the tf/compose/Dockerfile.
- No SPA build checked into git (`web/dist` stays gitignored; the Dockerfile builds it).
- No changes to the API routes, gates, or the auth/config behavior of the app (pure packaging + serving addition).
- No actual cloud provisioning in this story (deliverables are the container + IaC + runbooks + local proof; `apply` is the operator's step).
- No Cloud Run / Cloud SQL / Secret Manager resources (user decision — portability).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| SPA_ROOT | GET / (no API) | index.html served | 200 |
| SPA_FALLBACK | GET /tools/xyz (client route) | index.html served (fallback) | 200 |
| API_OK | GET /api/v1/... | unchanged Go handler response | per handler |
| API_UNKNOWN | GET /api/v1/nonexistent | JSON 404 envelope (NOT SPA) | 404 |
| ASSET_HASHED | GET /assets/app-<hash>.js | served with immutable cache | 200 |
| ASSET_INDEX | GET /index.html | served with no-store | 200 |
| IMAGE_BUILD | `just container-build` | multi-stage image produced, minimal runtime | build fails loudly |
| LOCAL_PROOF | `just deploy-local-proof` | registry:2 up, image pushed+pulled, compose up, healthz 200 | readiness waits on db |
| INFRA_VALIDATE | `just infra-validate` | tofu validates (init+validate) without creds | config error surfaces |
| INFRA_PLAN | `just infra-plan` | plan shown, no apply | requires backend init |
| SECRETS | tf/compose env vars | no secret literal in any file (refs only) | n/a |
| PORT_COLLISION | local :8080 already used (dev server) | local proof uses a distinct host port (e.g. 8081) | 200 via :8081 |

</frozen-after-approval>

## Code Map

- `Dockerfile` (new, repo root) -- multi-stage: node build `web/dist`, go build `-tags postgres ./cmd/server`, runtime image (distroless/static or alpine) with `GEAR_WEB_DIST` (default `./web/dist`) + non-root user; healthcheck `wget -qO- localhost:8080/healthz`; `ARG GEAR_IMAGE_VERSION`/label.
- `.dockerignore` (new) -- exclude node_modules, .git, _bmad-output, docs/build, web/dist (built in-image), test artifacts, .env.
- `internal/platform/config/config.go` -- add `WebDist string` (`envOr("GEAR_WEB_DIST", "./web/dist")`); no other behavior change.
- `internal/platform/router/router.go` -- confirm `WithMount("/", spa)` works as a catch-all; `New` at `router.go:56-69` mounts options in order (SPA must be LAST).
- `internal/platform/spa/spa.go` (new) -- `SPAHandler{root http.FileSystem}` serving from disk: for a request path, if the file exists serve it (hashed assets → `Cache-Control: public, max-age=31536000, immutable`; `index.html` → `no-store`); else if not an `/api/` path serve `index.html` (fallback); `/api/*` falls through to the router's JSON NotFound. Provide `NewSPAHandler(dir string)`.
- `cmd/server/main.go` -- after `router.New(...)` mount list add `router.WithMount("/", spa.NewSPAHandler(cfg.WebDist))`; wire `cfg.WebDist`.
- `compose.yaml` -- add `app` service: `build: .`, `image: ${GEAR_IMAGE:-gear-app}`, `ports: "8081:8080"` (host 8081 to avoid dev collisions), env from `.env` (`GEAR_DATABASE_URL` points at the `db` service hostname), `depends_on: db (healthy)`, healthcheck `/healthz`; db service unchanged (internal-only, no published port change).
- `deploy/compose.local-proof.yaml` (new) -- image-pinned app (`image: localhost:5000/gear:local`, `pull_policy: missing`), db as in compose, distinct host ports; the pull-based proof target.
- `deploy/startup.sh` (new) -- idempotent first-boot script (used by GCP IaC and mirrored by `deploy-local-proof`): install docker/compose (GCP path), generate `0600 .env` if absent (random DB password + `GEAR_ENCRYPTION_KEY`), `compose pull && up -d`, poll `/healthz`.
- `infra/` (new) -- provider-neutral layout:
  - `infra/backend.tf` -- gcs/remote backend params (bucket, prefix) parameterized, not hardcoded.
  - `infra/provider.tf` -- google provider (project/region inputs only).
  - `infra/variables.tf` -- project, region, machine_type (default e2-micro), disk size, spot (bool), image_repo, app origin, ingress source ranges, optional static IP + DNS, artifact registry enabled (bool).
  - `infra/main.tf` -- GCE VM (boot disk + `startup.sh`), firewall rules (80/443 from ingress ranges, SSH from operator range), optional static IP + DNS record, optional Artifact Registry repo + minimal SA `roles/artifactregistry.reader` (variable-gated).
  - `infra/startup.sh` (template) -- the idempotent first-boot script content (or referenced from deploy/).
  - `infra/README.md` -- init/plan/apply runbook + "add a second cloud" seam (IONOS: Cube/DCD VM + same docker/.env/compose).
- `justfile` -- `container-build`, `container-run`, `deploy-local-proof`, `infra-validate`, `infra-plan`, `infra-apply` recipes; keep existing recipes.
- `deploy/README.md` + `infra/README.md` -- operational runbooks (local proof, GCP, IONOS).
- Tests: spa handler (fallback/404/cache), config `GEAR_WEB_DIST` default; router mount order guard.

## Tasks & Acceptance

**Execution:**
- [x] `internal/platform/config/config.go` -- add `GEAR_WEB_DIST` (default `./web/dist`) -- config
- [x] `internal/platform/spa/spa.go` (new) -- disk-serve SPA handler (index fallback, immutable assets, /api passthrough) -- serving
- [x] `cmd/server/main.go` + `internal/platform/router/router.go` -- mount SPA catch-all after API; wire `cfg.WebDist` -- composition root
- [x] `Dockerfile` + `.dockerignore` (new) -- multi-stage build, minimal runtime, non-root -- container
- [x] `compose.yaml` -- add `app` service (build, image-pinned, env from .env, depends_on db healthy, healthz) -- compose
- [x] `deploy/compose.local-proof.yaml` + `deploy/startup.sh` (new) -- pull-based proof target + idempotent first-boot -- local proof
- [x] `infra/` (new) -- portable OpenTofu IaC: provider-neutral, single GCE VM + firewall + optional static IP/DNS + optional Artifact Registry/SA -- IaC
- [x] `justfile` -- container-build/run + deploy-local-proof + infra-validate/plan/apply -- recipes
- [x] `deploy/README.md` + `infra/README.md` -- operational runbooks (local, GCP, IONOS) -- docs
- [x] Tests -- spa handler (fallback/404/cache), config default -- verification

**Acceptance Criteria:**
- Given the Dockerfile, when `just container-build` runs, then a minimal multi-stage image is produced with the Go binary + built SPA and no Vite process (NFR-R1).
- Given the app image, when a request hits `/` or an unknown non-API route, then the SPA is served; when it hits an unknown `/api/*`, then the JSON 404 envelope is returned (never the SPA).
- Given `just deploy-local-proof`, when it runs on a machine with podman, then a local `registry:2` is started, the image is built+pushed+pulled, the compose stack comes up, and `/healthz` returns 200 — proving the same registry-driven flow IONOS/GCP use, without a cloud (NFR-R1, portability).
- Given `infra/`, when `tofu validate` + `tofu plan` run with a project's inputs, then a single GCE VM + firewall + optional Artifact Registry plan is produced with NO secrets in the files and provider-specific resources isolated behind inputs (portable to another cloud later).
- Given a fresh instance, when the operator follows `infra/README.md` + `deploy/README.md`, then they can build, push, provision, migrate, and bootstrap without touching application code (NFR-R2/R4, NFR-S1/S4).

## Spec Change Log

- **2026-09-19, review patches (review 1):** the review found the *deployed* compose path was `compose.yaml` (dev file: publishes postgres + builds) while the proof exercised a separate file — fixed by making `deploy/compose.prod.yaml` THE deployed file (image-pinned, db internal-only) and pointing startup.sh + infra + `just deploy-local-proof` at it; the proof now exercises the exact artifact GCP/IONOS run. Also: uniform SPA API-404 message (`route not found`); symlink-escape + root-dir containment guard in the SPA handler; non-GET/HEAD SPA requests answer 405; `runHealthcheck` uses `net.SplitHostPort` (tolerates `0.0.0.0:port`) + a correct DB-coupled-readiness comment; Dockerfile HEALTHCHECK is valid exec-form (no shell `|| exit 1`); GCP IaC SSH default tightened to a TEST-NET placeholder (operator must set), `dns_managed_zone` + `repo_url` variables added, instance depends on compute API enablement, startup.tftpl clones the repo when missing; composed-router SPA-mount test + `runHealthcheck` tests added; justfile `container-run` depends on `container-build`, the proof recipe uses single-`$` shell + explicit `GEAR_APP_ORIGIN`; `.env` regeneration caveat (wipe the pgdata volume) documented. KEEP: the local-proof-first approach (proves the deployed compose before any cloud), and the portable unit (image + compose + .env, no cloud-managed services).

## Design Notes

- **Portability = swap the resource layer, keep the contract:** the app is cloud-agnostic (env-only config, no SDK). The GCP tf module is the first backend; IONOS (favourite) reuses the identical image + compose + `.env` via its Docker-v2 Private Container Registry and a Cube/DCD VM. The container is the unit of portability.
- **No managed services (user decision):** Cloud SQL / Cloud Run / Secret Manager are GCP couplings that don't exist on "just run docker containers" platforms. A single VM running compose is the smallest portable footprint; postgres-internal-only + host `.env` (0600) replaces the cloud secret store.
- **Registry-driven + local proof:** pushing to a local `registry:2` container exercises the exact Docker Registry HTTP v2 API IONOS uses, so the local proof validates the cloud path without cloud spend. Hosts only pull; they never build.
- **SPA serving from disk, not `go:embed`:** embed cannot traverse up from `cmd/server` to `web/dist`; `GEAR_WEB_DIST` (default `./web/dist`) keeps one runtime model for dev and prod. The Dockerfile copies the built dist into the image.
- **SPA catch-all must be last:** chi matches most-specific first; mounting `/` after all `/api` mounts guarantees API 404s stay JSON. The fallback is scoped to non-`/api` paths.
- **No provisioning in this story:** the IaC is real and `tofu plan`-able, but applying to a live GCP project is the operator's deploy step (requires their project/billing). This keeps the repo free of real secrets and lets the human review the plan before any cloud spend.

## Verification

**Commands:**
- `just build` && `just vet` && `go test ./internal/platform/... ./cmd/server/...` -- expected: all pass incl. new spa handler + config tests
- `just lint` -- expected: 0 issues
- `just container-build` (podman) -- expected: image builds; `podman image ls` shows it
- `just deploy-local-proof` -- expected: registry:2 up, push+pull OK, compose up, `/healthz` 200; `curl -i localhost:8081/` → SPA; `curl -i localhost:8081/api/v1/nope` → JSON 404; `curl -I localhost:8081/tools/xyz` → 200 (fallback)
- `just infra-validate` -- expected: tofu validates (may need `tofu init -backend=false`)
- `npx vitest run` in web/ -- expected: unchanged/all pass (no SPA code change beyond the router contract)

**Manual checks (if no CLI):**
- Inspect `infra/`, `compose.yaml`, `Dockerfile`, `deploy/*.yaml` for secrets: `rg -i "password|secret|key"` should only hit parameter references/placeholder names, never literals.
- Confirm postgres is not published to the host in the prod/local-proof compose (no `ports:` on db).

## Suggested Review Order

**Entry point — the SPA serving contract**

- Read first: the deployed SPA handler — index fallback for non-API routes, JSON 404 for /api/*, immutable hashed assets, path-escape guard
  [`spa.go:42`](../../internal/platform/spa/spa.go#L42)

**Composition root + container probe**

- The SPA catch-all mounted last (after every /api mount) + the `-healthcheck` probe the container HEALTHCHECK runs
  [`main.go:59`](../../cmd/server/main.go#L59)
- Composed-router test that pins the SPA/API routing contract end-to-end
  [`main_test.go:2028`](../../cmd/server/main_test.go#L2028)

**The deployed compose (the artifact GCP/IONOS run)**

- Image-pinned (pull, never build), db internal-only (no published port), secrets from the 0600 .env
  [`compose.prod.yaml:1`](../../deploy/compose.prod.yaml#L1)
- Idempotent first-boot provisioning: generate .env if absent, pull, up, healthz wait
  [`startup.sh:1`](../../deploy/startup.sh#L1)
- The local proof recipe that exercises the deployed compose under podman
  [`justfile:194`](../../justfile#L194)

**Portable GCP IaC**

- The single Compute Engine VM + firewall + optional static IP/DNS + minimal registry-reader SA (the only GCP surface)
  [`main.tf:41`](../../infra/main.tf#L41)
- Startup template that installs docker and clones the repo when missing
  [`startup.tftpl:1`](../../infra/startup.tftpl#L1)

**Container + config**

- Multi-stage image (vite → Go binary → distroless runtime)
  [`Dockerfile:1`](../../Dockerfile#L1)
- `GEAR_WEB_DIST` config default
  [`config.go:31`](../../internal/platform/config/config.go#L31)

**Runbooks**

- Local proof + GCP + IONOS deployment instructions
  [`README.md:1`](../../deploy/README.md#L1)