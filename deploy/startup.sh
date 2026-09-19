#!/usr/bin/env bash
# G.E.A.R. first-boot provisioning (Story 7.6). Idempotent: safe to re-run.
# Used as the GCP Compute Engine startup script AND mirrored (adapted) by the
# `just deploy-local-proof` recipe, so the same secret-on-host + compose-pull
# flow is proven locally before any cloud. Works on any Docker-capable host
# (GCP VM, IONOS Cube/DCD VM, self-hosted) — no cloud-specific secret store.
#
# Environment contract (all optional here — defaults suit a first boot):
#   GEAR_APP_DIR       dir with compose files (default /opt/gear)
#   GEAR_IMAGE_REPO    image reference to pull (default gear-app:latest)
#   GEAR_COMPOSE_FILE  compose file to run (default deploy/compose.prod.yaml)
#   GEAR_HTTP_PORT     host port mapped to the app :8080 (default 8080)
#
# Produces:
#   $GEAR_APP_DIR/.env         0600 secrets file (DB password + encryption key)
set -euo pipefail

APP_DIR="${GEAR_APP_DIR:-/opt/gear}"
IMAGE_REPO="${GEAR_IMAGE_REPO:-gear-app:latest}"
COMPOSE_FILE="${GEAR_COMPOSE_FILE:-deploy/compose.prod.yaml}"
HTTP_PORT="${GEAR_HTTP_PORT:-8080}"

echo "[gear] provisioning ${APP_DIR} (image ${IMAGE_REPO}, port ${HTTP_PORT})"

# --- 0. Required tools --------------------------------------------------------
for tool in docker openssl curl seq; do
  command -v "$tool" >/dev/null 2>&1 || { echo "[gear] required tool missing: $tool" >&2; exit 1; }
done
docker compose version >/dev/null 2>&1 || { echo "[gear] docker compose plugin not found" >&2; exit 1; }

# --- 1. Secrets: generate a 0600 .env ONLY if absent (idempotent) -----------
ENV_FILE="${APP_DIR}/.env"
regenerate=false
if [ -f "${ENV_FILE}" ] && grep -q '^GEAR_DB_PASSWORD=.\+' "${ENV_FILE}" && grep -q '^GEAR_ENCRYPTION_KEY=.\+' "${ENV_FILE}"; then
  echo "[gear] .env already present with both keys; keeping existing secrets (idempotent)"
else
  echo "[gear] generating .env with random DB password + encryption key"
  regenerate=true
  umask 077
  {
    printf 'GEAR_DB_PASSWORD=%s\n' "$(openssl rand -hex 16)"
    printf 'GEAR_ENCRYPTION_KEY=%s\n' "$(openssl rand -hex 32)"
  } > "${ENV_FILE}"
  chmod 0600 "${ENV_FILE}"
fi

# --- 2. Make sure the compose file + repo dir are present ----------------------
if [ ! -f "${APP_DIR}/${COMPOSE_FILE}" ]; then
  echo "[gear] ${COMPOSE_FILE} missing in ${APP_DIR}; clone/copy the repo there first" >&2
  exit 1
fi

cd "${APP_DIR}"

# --- 3. Pull + start (host pulls; it never builds) ---------------------------
set -a
# shellcheck disable=SC1090
. "${ENV_FILE}"
set +a

export GEAR_IMAGE="${IMAGE_REPO}"
export GEAR_APP_ORIGIN="${GEAR_APP_ORIGIN:-http://localhost:${HTTP_PORT}}"
export GEAR_HTTP_PORT="${HTTP_PORT}"

echo "[gear] pulling ${IMAGE_REPO}"
docker compose -f "${COMPOSE_FILE}" pull

echo "[gear] starting compose stack"
docker compose -f "${COMPOSE_FILE}" up -d

# --- 4. Wait for readiness ---------------------------------------------------
echo "[gear] waiting for /healthz on :${HTTP_PORT}"
for i in $(seq 1 60); do
  if curl -fsS "http://127.0.0.1:${HTTP_PORT}/healthz" >/dev/null 2>&1; then
    echo "[gear] healthy after ${i}s"
    exit 0
  fi
  sleep 1
done
echo "[gear] app did not become healthy within 60s" >&2
exit 1