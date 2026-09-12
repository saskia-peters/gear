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
`tool_types.manage`. The tool surface (`/api/v1/admin/tools`) is gated by
**ANY-of `[tools.manage, tool.edit]`** (Story 4-3b, one permission set per
surface, AD-6): the reads (GET/PUT) are the any-of gate, while the writes
(POST create + POST /{id}/archive) re-apply a `tools.manage`-ONLY gate inside
the router (the write-only sub-gate) — so a `tool.edit`-only holder (e.g. a
Führende) can view + edit tools but NOT create/archive. Each physical tool
belongs to exactly one Tool-owned tool type, carries an OPTIONAL per-tool
schedule override that is a first-class FK to the Admin `schedules` catalog
(never JSONB); an empty override is stored as SQL NULL and means the tool
inherits its type's default schedule at resolution time (AD-5). Since Story
4-3b every tool also carries a unique human-readable `inventory_number`
(auto-assigned `GEAR` + zero-padded sequence value on manual create, editable
afterward; UNIQUE over ALL rows incl. archived — the Story 4.5 import
backstop). Since Story 4.4 both tools and tool types expose their `attributes
JSONB` column as a validated, editable "Eigene Felder" extension surface
(absent = unchanged, `{}` = clear; promoting an attribute to a real column
follows the AD-3 path in `migrations/README.md`).

No other module writes its tables; configuration goes through its exported
configuration port (AD-10).