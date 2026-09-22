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
> this file highlights the milestone cold-start and the current Story 4.2/4.3/4-3b/5-2b
> set.

The table below lists the current story set:

| File | Story | Content |
|------|-------|---------|
| `000021_tool_types.up/down.sql` | 4.2 | Tool-owned `tool_types` (FKs to Admin `schedules` + User `qualifications`, `attributes` JSONB, soft archive) + ordered `tool_type_checklist_items` child rows. |
| `000022_tool_type_checklist_items_unique_position.up/down.sql` | 4.2 review | `UNIQUE (tool_type_id, position)` on the ordered child rows (nondeterministic-ordering backstop). |
| `000023_tool_type_qualification_optional.up/down.sql` | 4.2 review | The tool type's required qualification becomes OPTIONAL (`required_qualification_id` DROP NOT NULL). |
| `000024_tools.up/down.sql` | 4.3 | Tool-owned `tools` (intra-module FK to `tool_types`, optional per-tool `schedule_id` override FK to Admin `schedules`, UNIQUE name, `attributes` JSONB, soft archive) + `tools_tool_type_id_idx`. |
| `000025_tool_inventory_number.up/down.sql` | 4-3b | `tools.inventory_number` (text, NOT NULL, CHECK ≤ 16) + `tools_inventory_number_seq` + backfill of existing rows + UNIQUE across ALL rows (Story 4.5 import backstop) + `tool.edit` permission seeded/granted to admin/schirrmeister/fuehrende. |
| `000026_app_settings.up/down.sql` | 5-2b | Admin-owned typed `app_settings` key/value store (one row per atomic setting, one of `duration_value`/`int_value`/`text_value` set per row, durations in seconds) seeded with the 14 proposal defaults (21 atomic rows) + `admin.settings.system` permission seeded/granted to admin. |
| `000027_inspections.up/down.sql` | 5.3 | Tool-owned `inspections` + `inspection_items` (snapshotted checklist results) + `reinstatements` ledger (FR-12/FR-13/FR-15) — status stays DERIVED on read (AD-4), never stored — + the per-tool history/status indexes (FR-18). |
| `000028_schirrmeister_inspection_history.up/down.sql` | 6.3 follow-up | Grants the EXISTING `inspection.history.view` permission (000010) to the `schirrmeister` base role (the frozen spec names Schirrmeister as a history viewer). Idempotent INSERT; the down only removes the schirrmeister grant row — the code itself is never deleted. |
| `000029_dsgvo_inspector_indexes.up/down.sql` | 3.3 | Pure read indexes for the DSGVO data-access export (FR-24): `inspections_inspector_id_idx` (inspector_id, submitted_at DESC), `reinstatements_actor_id_idx` (actor_id, created_at DESC) — the per-user `ListInspectionsByInspector` / `ListReinstatementsByActor` reads — and `sessions_user_id_created_at_idx` (user_id, created_at DESC) — the per-user `ListSessionsByUser` sort. No schema change. |
| `000030_dsgvo_account_deletion.up/down.sql` | 3.4 | User-owned `dsgvo_deleted_accounts` archive (the soft-deleted account's personal snapshot, secrets excluded; `original_user_id` a PLAIN no-FK uuid mirroring the tool-snapshot pattern) + the `users.state` CHECK extended to admit `deleted` (the scrubbed tombstone — login permanently blocked). Deletion never hard-deletes; the archive + tombstone are hard-purged only on admin demand (FR-24). |
| `000031_tool_settings_adoption.up/down.sql` | 5-2c | Tool-settings adoption (D1/C2/FR-30): renames `inspection_orange_window_days` → `inspection_orange_window_percent` (seed 25, a consumed percentage of the inspection interval), bumps the `inventory_width` seed 6 → 9 ONLY when the stored value is still the seed (never clobbering an admin override), and seeds the missing `1 Woche` schedule (week, 1). |
| `000032_idempotency_keys.up/down.sql` | 7.5 | At-most-once hardening (NFR-R1/FR-12/FR-15): `inspections` + `reinstatements` gain a REQUIRED `idempotency_key uuid` (backfilled with `uuidv7()` before NOT NULL, per the `000025` pattern) + a UNIQUE index per tool `(tool_id, idempotency_key)` — the client-supplied key makes both append-only write paths replay-safe under retry/concurrency. |
| `000033_backup_interval.up/down.sql` | 7.7 | DATA-ONLY: seeds the `backup_interval` app-settings row (86400s = daily, the in-process backup job's ticker interval, NFR-R3). No schema change, no sqlc impact; the down deletes only that row. |

Naming: `NNNNNN_snake_case.up.sql` / `NNNNNN_snake_case.down.sql`.

Apply from the root `justfile`: `just migrate-up` / `just migrate-down`.
sqlc generates the per-module stores from these forward migrations
(`just sqlc-generate`).

## Eigene Felder zu einer echten Spalte machen (AD-3)

Tools and tool types carry a no-migration extension surface: the
`attributes JSONB` column (`tool_types` 000021, `tools` 000024, both
`jsonb NOT NULL DEFAULT '{}'`). Custom metadata lives there with NO schema
change per attribute (Story 4.4, FR-10/AD-3) — validation is app-level,
mirroring the User-module profile precedent (Story 1.9).

When an attribute later becomes **core/queryable** (needs a real column, an
index, or a CHECK), promote it through a normal golang-migrate pair — the
`000025` inventory-number migration is the concrete precedent:

1. **Add the column** with a golang-migrate pair
   (`NNNNNN_promote_<attr>.up/down.sql`), e.g.
   `ALTER TABLE tools ADD COLUMN <attr> text;`.
2. **Backfill existing rows** from the JSONB BEFORE adding NOT NULL — the
   `000025` backfill (`UPDATE tools SET inventory_number = ... ;` before
   `SET NOT NULL`) is the pattern: read the attribute out of `attributes->>'<attr>'`
   with a sensible default for rows that never set it.
3. **Then** apply `NOT NULL`/`CHECK`/`UNIQUE`/index constraints, so the
   constraints are satisfiable against the backfilled data.
4. **Retain the `attributes JSONB` column** — it stays the no-migration
   surface for the remaining custom metadata; the promoted attribute becomes a
   typed, queryable column while everything else keeps living in JSONB. Do NOT
   drop the column as part of the promotion.
5. **Append the new forward migration to its owning module's `sqlc.yaml`
   schema list** (in numeric order) and `just sqlc-generate`, so the generated
   store matches the shipped schema.
