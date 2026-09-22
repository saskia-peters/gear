# cmd/server

**Composition root (AD-1).** This package is the only place that wires module
hexagons and their adapters together and mounts the HTTP surface. Handlers,
adapters and repositories contain no business logic — they delegate to the
modules. The auth gateway and server-side policy (AD-2/AD-6) attach here in
later stories.

Run: `just dev` (or `go run ./cmd/server`). Configuration via `GEAR_*` env
vars (see `internal/platform/config`).

## `GEAR_ENCRYPTION_KEY` (TOTP MFA, NFR-S4)

The TOTP shared secrets stored at rest are encrypted with AES-256-GCM using a
32-byte app-level key. Provide it as hex or base64 via `GEAR_ENCRYPTION_KEY`
(generate with `openssl rand -hex 32` or `openssl rand -base64 32`). If it is
missing or invalid the server logs a clear startup warning and MFA endpoints
answer `503 "MFA ist derzeit nicht verfügbar."`; ordinary login/register are
unaffected. Never commit the key — supply it from a secret manager in
production.
