# infra — G.E.A.R. IaC (Story 7.6)

**OpenTofu** IaC for deploying the G.E.A.R. container to **Google Cloud Platform**
as a **single Compute Engine VM running the compose stack**. The deployment is
**registry-driven and portable**: the same app image + compose + `.env` contract
runs on GCP, IONOS, or any Docker-capable host (see `../deploy/README.md`).

## Design invariants

- **No managed services** (user decision): no Cloud Run, no Cloud SQL, no
  Secret Manager. The app + postgres run as containers on one VM; secrets live
  in a `0600 .env` on the host (generated at first boot).
- **Provider is the only GCP-specific surface.** `provider.tf` + `main.tf` are
  GCP; the app image / compose / `.env` are not. A future cloud (IONOS is the
  favourite) adds a second provider + resource file and reuses the same image.
- **Minimal cost trial:** `e2-micro` default, spot optional, ephemeral IP
  default, small disk, optional Artifact Registry (variable-gated).
- **Host pulls, never builds** — `image_repo` input; no build on the VM.

## Prerequisites

- `tofu` (v1.12.x) — see `../justfile`
- A GCP project + billing; `gcloud auth application-default login` (or
  service-account key exported as `GOOGLE_APPLICATION_CREDENTIALS`)
- The image pushed to a Docker v2 registry the VM can reach (GCP Artifact
  Registry, Docker Hub, or your own). Default `image_repo` = `gear-app:latest`
  (local/pulled from wherever `image_repo` points).

## Usage

```bash
# 1. Validate + init (local state for the trial; configure backend.tf for remote)
just infra-validate          # tofu init -backend=false && tofu validate
just infra-plan               # tofu plan

# 2. Apply (OPERATOR STEP — provisions real GCP resources)
tofu -chdir=infra apply \
  -var project_id=your-project \
  -var region=us-central1 \
  -var zone=us-central1-a \
  -var image_repo=your-registry/gear:tag \
  -var app_origin=https://gear.example.org
```

The VM's startup script installs docker, generates `/opt/gear/.env` (0600) on
first boot, and runs `deploy/startup.sh` (compose pull + up, healthz wait).

## Adding a second cloud (portability seam)

1. **Provision a VM** on the target (e.g. IONOS Cube / DCD VM, 1 vCPU / 2 GB
   minimum — see `../deploy/README.md`).
2. **Copy the repo** (or at least `compose.yaml` + `deploy/startup.sh` +
   `.dockerignore`) to `/opt/gear` on the VM.
3. **Run `deploy/startup.sh`** with the target's `image_repo` env —
   it generates `.env`, pulls the image, and starts the stack. No cloud-specific
   step.

That is the whole portability story: the container + compose + `.env` are the
unit; GCP's IaC here is only the first host.

## Layout

- `backend.tf` — OpenTofu backend (parameterized, commented for local state)
- `provider.tf` — google provider (inputs only)
- `variables.tf` — project/region/machine/spot/disk/image/ingress/optional DNS
- `main.tf` — VM + firewall + optional static IP/DNS + optional Artifact Registry + minimal SA
- `startup.tftpl` — Debian 12 startup script (docker + shared provisioning)