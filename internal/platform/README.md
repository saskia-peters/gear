# internal/platform

**Cross-cutting infrastructure, never business logic** (structural seed):
logger, config, httpapi, middleware, and health.

| Package   | Purpose |
|-----------|---------|
| `config`  | Runtime config from `GEAR_*` env vars with local-dev defaults matching `compose.yaml`/`justfile`. |
| `logger`  | Shared structured-JSON slog logger (NFR-O1). |
| `httpapi` | Uniform error envelope `{"error":{"code","message","details?"}}` + response helpers — the JSON contract every handler must use. |
| `middleware` | Composition-root HTTP middleware (request logging + panic recovery now; auth gateway in later stories). |
| `health`  | `/healthz` probe pinging the pgx pool: 200 up / 503 with envelope down. |
| `router`  | The chi router wiring: middleware chain, JSON 404/405 handlers, `/healthz` mount — a single assembly shared by the server and its tests. |

Migrations stay justfile-only: the `justfile` drives the shared golang-migrate
set through the pinned CLI (`just migrate-up`/`migrate-down`), keeping a single
command entry point (adopted during Story 1.1).

These packages must not import `internal/user|tools|admin` — they sit below
the modules.
