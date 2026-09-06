# GEAR - single command interface (casey/just)
# Docs site lives in ./docs (Docusaurus). Recipes below are the docs lifecycle.

set shell := ["bash", "-cu"]

# Alias: list all available recipes when run without arguments
default:
    @just --list

# Install docs dependencies
docs-install:
    cd docs && npm install

# Start the Docusaurus dev server (default http://localhost:3000)
docs-start:
    cd docs && npm start

# Build the static docs site into docs/build
docs-build:
    cd docs && npm run build

# Build then serve the static site locally (validate production output)
docs-serve:
    cd docs && npm run build && npm run serve

# Build and run the docs Playwright browser tests (mermaid overlay, etc.)
docs-test:
    cd docs && npm run build && npx playwright test

# Clear Docusaurus caches
docs-clear:
    cd docs && npm run clear

# ============================================================================
# App lifecycle (Story 1.1 scaffold)
# Pinned tool versions — go run <module>@<version> so no global CLI installs
# are required. Override DATABASE_URL via the environment if needed.
# ============================================================================

MIGRATE_VERSION := "v4.19.1"
SQLC_VERSION    := "v1.31.1"
GOLANGCI_VERSION := "v2.13.2"
DATABASE_URL    := env_var_or_default("DATABASE_URL", "postgres://gear:gear@localhost:5432/gear?sslmode=disable")
DB_CONTAINER    := env_var_or_default("DB_CONTAINER", "gear-db")

# Abort with an actionable message when podman/podman-compose are missing
podman-check:
    @command -v podman >/dev/null 2>&1 || { echo "G.E.A.R. requires podman (podman-compose >= 1.0.6) to run the dev stack. Please install podman and retry." >&2; exit 1; }
    @command -v podman-compose >/dev/null 2>&1 || { echo "G.E.A.R. requires podman-compose >= 1.0.6. Please install it and retry." >&2; exit 1; }
    @podman compose version >/dev/null 2>&1 || { echo "podman compose is not functional. Check the podman-compose plugin and retry." >&2; exit 1; }

# Bring the db container up and wait until it accepts connections
# The compose healthcheck allows ~50s (10 x 5s); give the wait loop the same
# budget so a cold first start (image pull + initdb) is not aborted early.
db-wait: podman-check
    podman compose up -d db
    @i=0; until podman exec {{DB_CONTAINER}} pg_isready -U gear -d gear >/dev/null 2>&1; do i=$((i+1)); [ $i -ge 60 ] && { echo "database not ready after 60s" >&2; exit 1; }; sleep 1; done

# Start PostgreSQL 18 and apply pending migrations (idempotent)
db-up: migrate-up
    podman compose ps

# Stop the db container (keeps the named volume)
db-down: podman-check
    podman compose down

alias db-stop := db-down
alias db-shutdown := db-down

# Apply pending forward migrations
migrate-up: db-wait
    go run -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@{{MIGRATE_VERSION}} -path ./migrations --database "{{DATABASE_URL}}" up

# Roll back all applied migrations (used to prove a clean rebuild)
migrate-down: db-wait
    go run -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@{{MIGRATE_VERSION}} -path ./migrations --database "{{DATABASE_URL}}" down -all

# Ensure web dependencies exist before running the SPA (fresh checkout)
web-deps:
    @test -d web/node_modules || npm --prefix web ci

# Local-dev only: generate/persist GEAR_ENCRYPTION_KEY into .env so MFA works
# locally. The key is never committed (.env is gitignored). Idempotent — reuses
# an existing key.
dev-key:
    @if [ -f .env ] && grep -q '^GEAR_ENCRYPTION_KEY=.\+' .env 2>/dev/null; then \
        echo "GEAR_ENCRYPTION_KEY already set in .env"; \
    else \
        key=$(openssl rand -hex 32 2>/dev/null || od -An -N32 -tx1 /dev/urandom | tr -d ' \n'); \
        grep -v '^GEAR_ENCRYPTION_KEY=' .env 2>/dev/null > .env.tmp || true; \
        printf 'GEAR_ENCRYPTION_KEY=%s\n' "$key" >> .env.tmp; \
        mv .env.tmp .env; \
        echo "Generated GEAR_ENCRYPTION_KEY in .env"; \
    fi

# Run the full dev stack: DB + API + Vite SPA
dev: web-deps dev-key db-up
    set -a; [ -f .env ] && . ./.env; set +a; \
    npm --prefix web run dev & \
    vite_pid=$!; \
    sleep 2; \
    kill -0 "$vite_pid" 2>/dev/null || { echo "vite failed to start (see output above)" >&2; exit 1; }; \
    trap 'kill "$vite_pid" 2>/dev/null; kill $(jobs -p) 2>/dev/null' EXIT INT TERM; \
    go run ./cmd/server

# Build all Go packages
build:
    go build ./cmd/... ./internal/...

# Run all Go and web tests
test:
    go test ./cmd/... ./internal/...
    npm --prefix web run test

# Vet all Go packages
vet:
    go vet ./cmd/... ./internal/...

# Lint Go (via pinned golangci-lint) and web (via eslint)
lint: vet
    go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@{{GOLANGCI_VERSION}} run ./cmd/... ./internal/...
    npm --prefix web run lint

# Regenerate per-module stores from migrations/ via pinned sqlc (config: sqlc.yaml)
sqlc-generate:
    go run github.com/sqlc-dev/sqlc/cmd/sqlc@{{SQLC_VERSION}} generate

# Local-dev only: set/reset a user's password hash (e.g. unlock a seeded admin).
# Pass optional EMAIL and PASSWD to run non-interactively:
#   just set-admin-password admin.1@gear.local 'NewPassw0rd!'
# Without them, prompts for email + password. NOT for production — real admin
# credentials are provisioned out-of-band (AD-13 / FR-27). Never run against
# production data.
set-admin-password EMAIL='' PASSWD='': db-up
    go run -tags dev ./cmd/devadmin {{EMAIL}} {{PASSWD}}

# WSL2 (Option A): print the current WSL IP and the admin-PowerShell commands to
# expose `just dev` to devices on the same LAN via a Windows port proxy. The WSL
# NAT IP is NOT reachable from the LAN directly, so Windows must forward
# 5173 (Vite) and 8080 (API) to it. Run the printed commands in an elevated
# PowerShell on Windows, then open http://<windows-lan-ip>:5173 from a phone.
wsl-portproxy-setup:
    @wsl_ip=$(hostname -I | awk '{print $1}'); \
    echo "WSL IP: $wsl_ip"; \
    echo ""; \
    echo "Run these in an ADMIN PowerShell on Windows (replace <LAN_IP> with your"; \
    echo "Windows LAN IP from 'ipconfig', e.g. 192.168.1.20):"; \
    echo ""; \
    echo "  netsh interface portproxy add v4tov4 listenport=5173 listenaddress=0.0.0.0 connectport=5173 connectaddress=$wsl_ip"; \
    echo "  netsh interface portproxy add v4tov4 listenport=8080 listenaddress=0.0.0.0 connectport=8080 connectaddress=$wsl_ip"; \
    echo '  netsh advfirewall firewall add rule name="GEAR dev 5173" dir=in action=allow protocol=TCP localport=5173'; \
    echo '  netsh advfirewall firewall add rule name="GEAR dev 8080" dir=in action=allow protocol=TCP localport=8080'; \
    echo ""; \
    echo "Then open http://<LAN_IP>:5173 from your phone."

# WSL2 (Option A): print the commands to remove the Windows port proxy + firewall
# rules created by wsl-portproxy-setup. Run them in an ADMIN PowerShell.
wsl-portproxy-teardown:
    @echo 'Run these in an ADMIN PowerShell on Windows:'; \
    echo '  netsh interface portproxy delete v4tov4 listenport=5173 listenaddress=0.0.0.0'; \
    echo '  netsh interface portproxy delete v4tov4 listenport=8080 listenaddress=0.0.0.0'; \
    echo '  netsh advfirewall firewall delete rule name="GEAR dev 5173"'; \
    echo '  netsh advfirewall firewall delete rule name="GEAR dev 8080"'
