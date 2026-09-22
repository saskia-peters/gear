---
title: 'Flexible Attributes on Tools & Tool Types (FR-10/AD-3)'
type: 'feature'
created: '2026-09-12'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'ae15642af869bf69826e5ffada353e492d34e52b'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-4-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Tools and tool types have an `attributes JSONB` column (from Stories 4.2/4.3) but no way to set, read back, or validate custom metadata — tool-type attributes are never written at all, and the SPA has no editor.

**Approach:** Make the existing `attributes JSONB` columns a first-class, validated, editable extension surface on both tools and tool types (FR-10/AD-3): the Tool-module port carries attributes through Create/Update with a consistent update contract ("absent = unchanged, `{}` = clear"), a shared SPA key/value editor ("Eigene Felder") on both editors, and a documented AD-3 promotion path for promoting an attribute to a real column.

## Boundaries & Constraints

**Always:**
- **Storage stays in the owning `attributes JSONB` column** — `tools.attributes` (000024) and `tool_types.attributes` (000021), both `jsonb NOT NULL DEFAULT '{}'`. NO new column per attribute, no migration for the surface itself (the columns already exist).
- **Update contract (user decision, mirrors the User-module profile precedent):** an ABSENT `attributes` field in the write body leaves the stored JSONB unchanged; an EXPLICIT `{}` clears it; a non-empty object replaces it. This applies to both tools (PUT/POST) and tool types (PUT/POST).
- **Tool types become writable for attributes:** `ToolTypeInput` gains an `attributes` field (currently absent); `CreateToolType` persists it, `UpdateToolType` honors absent-vs-`{}`-vs-object. (Currently tool-type attributes are never written.)
- **Validation (mirror the User-module `validateAttributes`):** attribute KEYS are non-empty, trimmed, bounded (≤64 runes, mirroring `MaxAttributeKeyRunes`); values must be JSON-serializable; the whole map ≤ 16KB serialized (`MaxAttributesSize`); non-object input rejected (German 400). Empty/nil map is valid.
- **HTTP DTOs:** `toolDTO` already carries `attributes` (writable today); `toolTypeDTO` GAINS `attributes` (write + read). The dashboard `dashboardToolDTO` stays MINIMAL (no attributes — noise on the Werkzeugliste; existing no-leak test stays).
- **Permission gating:** attribute writes flow through the owning entity's existing write path — tools PUT any-of `[tools.manage, tool.edit]` (a `tool.edit` holder can edit tool attributes via the existing PUT), tool types POST/PUT `tool_types.manage` only. NO separate attribute endpoint. Core re-checks the same code (AD-6).
- **Shared SPA editor:** a new generic `AttributesEditor` component (key/value pairs: add, edit value, remove; JSON-serializable values with inline German validation, ≤16KB feedback) wired into BOTH `ToolsTab` and `ToolTypesTab` as an "Eigene Felder" section. The SPA reads stored attributes on edit and submits them per the contract (`attributes` omitted when untouched, `{}` to clear). `web/src/auth/tools.ts` gains `attributes` on `ToolType`/`ToolTypeInput` and stops hardcoding `attributes: {}` on every tool save.
- **AD-3 promotion path documented:** add a short "Eigene Felder zu einer echten Spalte machen (AD-3)" section to `migrations/README.md` (add column via golang-migrate + backfill existing rows, retain the JSONB column) — the `000025` inventory-number migration is the concrete precedent. Optionally one line in `internal/tools/README.md`.
- **Retrieval unchanged (FR-10/AD-3):** attributes read back byte-for-byte (valid JSON round-trip through the port), archived rows preserve their attributes.

**Ask First:**
- None (the update contract is user-resolved; the rest follows existing conventions).

**Never:**
- No new column per attribute; no migration for the surface.
- No separate attributes endpoint (reuse the entity's write path).
- No attributes on the dashboard DTO.
- No tool-type attributes left unwritable (this story fixes that).
- No removal of the JSONB column when promoting (AD-3 retains it).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| CREATE_TOOL_ATTRS | create tool with attributes | Stored in tools.attributes, retrievable unchanged | n/a |
| UPDATE_TOOL_ATTRS | PUT tool with attributes object | Replaces stored attributes, audited | n/a |
| UPDATE_TOOL_ABSENT | PUT tool without attributes field | Stored attributes unchanged (absent = unchanged) | n/a |
| UPDATE_TOOL_CLEAR | PUT tool with `attributes: {}` | Attributes cleared to `{}` | n/a |
| CREATE_TYPE_ATTRS | create tool type with attributes | Stored in tool_types.attributes (was impossible before) | n/a |
| UPDATE_TYPE_ABSENT | PUT type without attributes | Unchanged (was previously always untouched) | n/a |
| VALID_INVALID_KEY | empty/over-long key | 400 German (key error) | 400 |
| VALID_NON_OBJECT | attributes is an array/string/primitive | 400 German (must be object) | 400 |
| VALID_TOO_LARGE | serialized map > 16KB | 400 German (size) | 400 |
| VALID_BAD_VALUE | value not JSON-serializable | 400 German | 400 |
| ROUND_TRIP | set → read back | Byte-for-byte unchanged JSON | n/a |
| ARCHIVED | archive tool/type with attributes | Attributes preserved on the archived row | n/a |
| FORBIDDEN | caller lacks owning permission | Uniform 403, no data exposed (AD-6) | 403 |
| DASHBOARD | dashboard.view holder | attributes NOT in the dashboard DTO | n/a |

</frozen-after-approval>

## Code Map

- `internal/tools/core/tool_types.go` -- `ToolTypeInput.Attributes` + validation wiring; `CreateToolType`/`UpdateToolType` honor the contract (absent=unchanged, `{}`=clear, object=replace).
- `internal/tools/core/tools.go` -- align `CreateTool`/`UpdateTool` attribute handling to the same contract (currently full-replace-always); add shared attributes validation (mirror the user-module rules).
- `internal/tools/core/attributes.go` (new) -- shared `validateAttributes` + `attributesUnchanged`/`attributesCleared` helpers + sentinels/German messages (or extend an existing file).
- `internal/tools/adapters/postgres/queries.sql` + `tool_types_repo.go` -- tool-types INSERT sets attributes, UPDATE sets/keeps per contract; `tools_repo.go` `marshalToolAttributes` updated for the absent/clear semantics.
- `internal/tools/adapters/http/tool_types.go` -- `toolTypeDTO.Attributes` (write + read); flip the existing `attributes`-absent test.
- `internal/tools/adapters/http/tools.go` -- confirm `toolDTO` passthrough stays; no change to `dashboardToolDTO`.
- `web/src/auth/tools.ts` -- `attributes` on `ToolType`/`ToolTypeInput`; `buildToolBody`/`buildToolTypeBody` honor absent/`{}` semantics (stop hardcoding `{}`).
- `web/src/components/AttributesEditor.tsx` (+css, new) -- shared key/value editor (add/remove, inline German validation, size feedback) reused by both tabs.
- `web/src/pages/admin/AdminWerkzeugePage.tsx` -- wire `AttributesEditor` into `ToolsTab` + `ToolTypesTab` as "Eigene Felder".
- `migrations/README.md` (+`internal/tools/README.md`) -- AD-3 promotion-path section.
- Tests: core (round-trip, absent/clear/replace, key/size/non-object/bad-value 400s, both entities), postgres (jsonb round-trip with non-empty values, `'{}'` default, archived preservation), http (DTO write+read both surfaces, 403, dashboard no-leak stays), web (editor on both tabs, save/load, absent/clear).

## Tasks & Acceptance

**Execution:**
- [x] `internal/tools/core/attributes.go` (new) -- shared validation + contract helpers -- core
- [x] `internal/tools/core/tool_types.go` -- ToolTypeInput.Attributes + contract in Create/Update -- core
- [x] `internal/tools/core/tools.go` -- align Create/Update to the contract + validate -- core
- [x] `internal/tools/adapters/postgres/queries.sql` + `tool_types_repo.go` (+`tools_repo.go`) -- jsonb write paths -- persistence
- [x] `internal/tools/adapters/http/tool_types.go` -- DTO gains attributes -- API
- [x] `web/src/auth/tools.ts` -- ToolType attributes + body semantics -- SPA
- [x] `web/src/components/AttributesEditor.tsx` (+css) -- shared key/value editor -- SPA
- [x] `web/src/pages/admin/AdminWerkzeugePage.tsx` -- "Eigene Felder" on both tabs -- SPA
- [x] `migrations/README.md` (+tools README) -- AD-3 promotion section -- docs
- [x] Tests -- core/postgres/http/web incl. I/O rows -- verification

**Acceptance Criteria:**
- Given a tool or tool type, when custom attributes are set, then they are stored in the owning row's `attributes JSONB` column (never a new column) (FR-10/AD-3).
- Given custom attributes are saved, when read back, then they are retrievable unchanged through the Tool module's port with valid JSON serialization and validation (FR-10/AD-1/AD-3).
- Given a custom attribute later becomes core/queryable, when promoted, then the documented AD-3 path (golang-migrate column + backfill, retain JSONB) is followed (AD-3/NFR-R2).
- Given a caller lacks the owning permission, then attribute writes answer uniform 403 with no data exposed (AD-6).

## Spec Change Log

- **Review patches applied (review 1, 2026-09-12):** stale-editor-rows bug fixed — both tabs now bump a `formVersion` on every reset/edit and key the `AttributesEditor` by it (create-reset remounts fresh); Save is gated by the editor's inline `onValidityChange` so invalid attributes can no longer be silently dropped while reporting success; the client size counter uses `TextEncoder` UTF-8 bytes (matching the server's 16KB byte cap); trimmed-duplicate keys are rejected server-side (matching the client); the documented `attributesUnchanged`/`attributesCleared` contract helpers are now actually used in the update paths; HTTP-layer `{}`-clear-signal pins added for both entities; string values round-trip verbatim (no trimming/type-conversion of " Werkstatt "/"true"/"1200"); key input capped at 64 to match the cap; Typen-tab clear-submit test added; tools GET list pins the nil→`{}` normalization; empty-add no longer fires dirty; JSON-`null`-as-absent documented. Cross-module contract duplication (tools vs user validateAttributes) and per-value size pre-check deferred as non-blocking.

## Design Notes

- **Update contract (user decision):** "absent = unchanged, `{}` = clear" mirrors the User-module profile attributes semantics. This is the safe default for a JSONB extension surface — a client that doesn't touch attributes never wipes them, and an explicit `{}` is the intentional clear. The SPA omits `attributes` when the user didn't change the section.
- **Tool types become writable:** the biggest functional gap is `ToolTypeInput` lacking `attributes` entirely (CreateToolType hardcodes `{}`, UpdateToolType never writes). This story makes the type surface symmetric with the tool surface.
- **Validation mirrors the user module** (`MaxAttributeKeyRunes` 64, `MaxAttributesSize` 16KB, object-only) so the three modules agree on the JSONB extension contract. No CHECK on the column — validation is app-level (matching the user precedent); out-of-band non-object stored values still degrade to the existing `_unparseable` read fallback.

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go/web tests pass, 0 lint issues
- `curl` as admin: PUT /api/v1/admin/tools/{id} with attributes object (200, stored); PUT without attributes (unchanged); PUT with `{}` (cleared); POST/PUT tool type with attributes (200, stored) -- expected per matrix
- `curl` as non-holder: attribute write -- expected: uniform 403 with no data
- `npx vitest run` in web/ -- expected: all pass incl. AttributesEditor tests on both tabs

**Manual checks (if no CLI):**
- Tool catalogue → "Typen"/"Werkzeuge": the "Eigene Felder" section adds/edits/removes key-value pairs, saves and reloads unchanged; clearing shows the section empty; the dashboard list does NOT show attributes.