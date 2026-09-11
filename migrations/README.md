# migrations

The **single** golang-migrate migration set — the one schema authority for the
whole application (NFR-R2, AD-11). Each table is owned by exactly one module;
each story ships its own incremental up/down pair and must not modify earlier
migrations.

| File | Story | Content |
|------|-------|---------|
| `000001_cold_start.up/down.sql` | 1.1 | User-owned identity tables the seeder needs, `admin.recovery.approve` permission, the four base roles and the two seeded admin accounts (AD-12/AD-13). |

> The migrations between `000001` and `000021` (Epics 1–3, stories 1.2–4.1)
> ship one incremental up/down pair each: `000002` user registration, `000003`
> auth sessions, `000004` login attempts, `000005` user TOTP, `000006` audit
> log, `000007` user profile, `000008` password reset, `000009` admin recovery,
> `000010` base permissions, `000011` fuehrende tools, `000012` user groups &
> qualifications, `000013` admin rework, `000014` admin OTP, `000015`
> drop-qualification-vocab-expiry, `000016` user-account-approve permission,
> `000017` SMTP settings, `000018` backup destinations, `000019` schedules,
> `000020` schedule seed names German. Each is documented alongside its story;
> this file highlights the milestone cold-start and the current Story 4.2/4.3
> set.

The table below lists the current story set:

| File | Story | Content |
|------|-------|---------|
| `000021_tool_types.up/down.sql` | 4.2 | Tool-owned `tool_types` (FKs to Admin `schedules` + User `qualifications`, `attributes` JSONB, soft archive) + ordered `tool_type_checklist_items` child rows. |
| `000022_tool_type_checklist_items_unique_position.up/down.sql` | 4.2 review | `UNIQUE (tool_type_id, position)` on the ordered child rows (nondeterministic-ordering backstop). |
| `000023_tool_type_qualification_optional.up/down.sql` | 4.2 review | The tool type's required qualification becomes OPTIONAL (`required_qualification_id` DROP NOT NULL). |
| `000024_tools.up/down.sql` | 4.3 | Tool-owned `tools` (intra-module FK to `tool_types`, optional per-tool `schedule_id` override FK to Admin `schedules`, UNIQUE name, `attributes` JSONB, soft archive) + `tools_tool_type_id_idx`. |

Naming: `NNNNNN_snake_case.up.sql` / `NNNNNN_snake_case.down.sql`.

Apply from the root `justfile`: `just migrate-up` / `just migrate-down`.
sqlc generates the per-module stores from these forward migrations
(`just sqlc-generate`).
