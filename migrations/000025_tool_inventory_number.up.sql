-- G.E.A.R. Tool inventory-number system + tool.edit permission (Story 4-3b,
-- deferred from 4.3, FR-9/AD-3/AD-6): every physical tool gets a unique,
-- human-readable `inventory_number` (text — char, not number-only). Manual
-- creation auto-assigns 'GEAR' + a zero-padded incrementing number from a
-- dedicated sequence (in-SQL nextval, atomic, monotonic); the number stays
-- EDITABLE afterward by tool.edit/tools.manage holders. The UNIQUE index is
-- case-insensitive (lower(...)) across ALL rows (active AND archived) — an
-- archived tool's number remains "taken", which is the Story 4.5 CSV/Excel
-- import backstop: a duplicate-number line is rejected by the DB instead of
-- silently deduplicated.
--
-- The tool.edit permission seed + grants are Tool-owned in the sense that this
-- story ships them; sqlc ignores the data INSERTs below (DML), so appending
-- this file to the tools sqlc block is safe (per-module scoping, AD-8/AD-11).

-- 1. The dedicated sequence (start 1, INCREMENT 1): the one source of the
--    auto-assigned numbers.
CREATE SEQUENCE tools_inventory_number_seq START 1 INCREMENT 1;

-- 2. The nullable column first (NOT NULL only after the backfill fills it).
ALTER TABLE tools ADD COLUMN inventory_number text;

-- 3. Backfill existing rows (e.g. "B - auf dem GKW") with 'GEAR' + zero-padded
--    sequence values, so NOT NULL is satisfiable before the constraint lands.
UPDATE tools SET inventory_number = 'GEAR' || lpad(nextval('tools_inventory_number_seq')::text, 6, '0');

-- 4. NOT NULL + bounded length + UNIQUE. The UNIQUE index is on
--    lower(inventory_number) — CASE-INSENSITIVE over ALL rows (active AND
--    archived), matching the core edit guard — so 'GEAR000001' and
--    'gear000001' collide. An archived tool's number remains "taken", which is
--    the Story 4.5 CSV/Excel import backstop: a duplicate-number line is
--    rejected by the DB instead of silently deduplicated.
ALTER TABLE tools ALTER COLUMN inventory_number SET NOT NULL;
ALTER TABLE tools ADD CONSTRAINT tools_inventory_number_max_length CHECK (char_length(inventory_number) <= 16);
CREATE UNIQUE INDEX tools_inventory_number_key ON tools (lower(inventory_number));

-- 5. Seed the `tool.edit` permission (Story 4-3b, AD-6): a scoped holder can
--    VIEW + EDIT tools (incl. the inventory number) WITHOUT create/archive —
--    create/archive stay `tools.manage`. Granted to the admin, schirrmeister
--    and fuehrende base roles, following the 000016 pattern (idempotent,
--    ON CONFLICT DO NOTHING).
INSERT INTO permissions (code, description)
VALUES ('tool.edit', 'Edit tool details (incl. inventory number)')
ON CONFLICT (code) DO NOTHING;

INSERT INTO permission_group_permissions (permission_group_id, permission_id)
SELECT g.id, p.id
FROM (VALUES
    ('admin', 'tool.edit'),
    ('schirrmeister', 'tool.edit'),
    ('fuehrende', 'tool.edit')
) AS m(role, code)
JOIN permission_groups g ON g.name = m.role
JOIN permissions p ON p.code = m.code
ON CONFLICT DO NOTHING;