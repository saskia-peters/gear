# internal/admin — Admin hexagon

**Owns:** the three operational-settings tables (`smtp_settings`,
`backup_destinations`, `schedules`) and triggers for user-lifecycle/DSGVO
orchestration (AD-8/AD-11/AD-14/AD-15/AD-16). It configures tools and users
through *their* ports and owns no other tables.

Layout mirrors the other hexagons:

| Path        | Purpose |
|-------------|---------|
| `core/`     | Domain core (empty until Epic 3). |
| `ports/`    | Port interfaces — the settings/schedule ports consumed read-only by other modules. |
| `adapters/` | Outbound adapters (sqlc PostgreSQL store arrives with Epic 3). |
