# internal/tools — Tool Maintenance hexagon

**Owns:** tool types, tools, inspections, reinstatements and the derived
status/inspection clock (AD-4/AD-5/AD-9). Consumes the User module's auth port
(qualifications, AD-7) and reads the Admin module's schedule catalog
(AD-16).

Layout mirrors the other hexagons:

| Path        | Purpose |
|-------------|---------|
| `core/`     | Domain core — Story 4.2 materializes tool-type management (`tool_types.go`); Story 4.3 materializes tool management (`tools.go`). |
| `ports/`    | Port interfaces (inbound config service + consumed module ports). |
| `adapters/` | Outbound adapters — sqlc PostgreSQL store + HTTP surface (Stories 4.2/4.3). |

The tool-type surface (`/api/v1/admin/tool-types`) is gated by
`tool_types.manage`; the tool surface (`/api/v1/admin/tools`) is gated by its
OWN `tools.manage` code — one permission per surface (AD-6). Each physical tool
belongs to exactly one Tool-owned tool type and carries an OPTIONAL per-tool
schedule override that is a first-class FK to the Admin `schedules` catalog
(never JSONB); an empty override is stored as SQL NULL and means the tool
inherits its type's default schedule at resolution time (AD-5).

No other module writes its tables; configuration goes through its exported
configuration port (AD-10).