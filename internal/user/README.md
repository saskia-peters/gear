# internal/user — User Directory & Auth hexagon

**Owns:** users, groups, permissions, sessions, MFA, credentials, recovery
tokens, account state and qualifications (AD-2).

Every feature module is an isolated Go package exposing only port interfaces
across its boundary (AD-1). This hexagon is laid out as:

| Path        | Purpose |
|-------------|---------|
| `core/`     | Domain core — business rules (empty until stories 1.3-1.10). |
| `ports/`    | Port interfaces — the single auth `Service` port + driven repository ports. |
| `adapters/` | Outbound adapters outside the hexagon. `adapters/postgres/` is the sqlc-generated PostgreSQL store. |

No other module may reach into this one's internals or storage; they consume
only the `Service` port.
