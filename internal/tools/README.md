# internal/tools — Tool Maintenance hexagon

**Owns:** tool types, tools, inspections, reinstatements and the derived
status/inspection clock (AD-4/AD-5/AD-9). Consumes the User module's auth port
(qualifications, AD-7) and reads the Admin module's schedule catalog
(AD-16).

Layout mirrors the other hexagons:

| Path        | Purpose |
|-------------|---------|
| `core/`     | Domain core — Story 4.2 materializes tool-type management (`tool_types.go`). |
| `ports/`    | Port interfaces (inbound config service + consumed module ports). |
| `adapters/` | Outbound adapters — sqlc PostgreSQL store + HTTP surface (Story 4.2). |

No other module writes its tables; configuration goes through its exported
configuration port (AD-10).
