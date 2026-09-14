---
title: 'Configurable System Settings — Infrastructure + System Tab'
type: 'feature'
created: '2026-09-13'
status: 'done'
review_loop_iteration: 1
baseline_commit: 'c4f6da11383f6227dd9c77373c6628969b128a2d'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/configurable-settings-proposal-2026-09-13.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** 14 system-wide values (timeouts, TTLs, lockout policy, attribute caps, inventory format, business thresholds) are hardcoded and several duplicated across the codebase, but there is no place to view or change them — a system administrator cannot tune them without editing code or shipping a migration.

**Approach:** Lay the foundation: an Admin-owned `app_settings` store (migration 000026, seeded with the 14 proposal defaults — modeled as **21 atomic rows**, one value column per row, since A3/B1/C1/C2 expand to pairs/groups), an Admin core + read-only `AppSettingsPort`, an HTTP surface `GET/PUT /api/v1/admin/settings/system` behind a new `admin.settings.system` permission, and a fourth "System" tab in the SPA settings page using a **table layout** with a per-row **"?" popup** explaining each setting. **Consumer adoption (threading the values into the SMTP/backup/user/tools adapters) is DEFERRED to a follow-up story** (recorded in deferred-work.md) — this story ships the surface + store so the values exist and are editable, but consumers still read their current static constants until adoption lands.

## Boundaries & Constraints

**Always:**
- **Migration 000026** `app_settings.{up,down}.sql` — Admin-owned, one row per setting: `key text PRIMARY KEY`, `value_type text CHECK (duration|integer|text)`, `duration_value bigint NULL`, `int_value bigint NULL`, `text_value text NULL` (exactly one value column set per row), `updated_at`. **Seed all 14 defaults** from the proposal — modeled as **21 atomic rows** (A3 backup dial+protocol, B1 lockout threshold×2+duration×2+max-count, C1 attribute key+size, C2 inventory prefix+width each expand into separate rows): A1 SMTP dial 10s, A2 SMTP protocol 30s, A3 backup dial/protocol 10s each, A4 reset TTL 30min, A5 recovery TTL 30min, A6 forgot throttle 60s, A7 OTP TTL 15min, A8 OTP length 10, A9 MFA window 10min, B1 lockout {threshold 3/4, duration 30/60s, cap 10}, C1 attribute {key 64, size 16KB}, C2 inventory {prefix GEAR, width 6}, D1 orange-window 14 days, D2 qualification window 30 days. Down reverses. Append to the **admin** sqlc block; `just sqlc-generate`.
- **Admin core** `internal/admin/core/app_settings.go` (new) + `settings.go` consts: `AppSettingsPermission = "admin.settings.system"`, typed `AppSettings` struct (one field per setting), `CurrentAppSettings(ctx) (*AppSettings, error)` (resolve from rows), `UpdateAppSettings(ctx, actorID, key, value)` (per-setting upsert; audit best-effort). Add `appSettingsStore` to the `Service`.
- **Read-only consumer port** `AppSettingsPort { CurrentAppSettings(ctx) (*AppSettings, error) }` in `internal/admin/ports/ports.go`, implemented by `admcore.Service` (mirrors `SchedulesPort`). This story does NOT wire consumers to it (deferred) — the port exists for the follow-up.
- **HTTP surface** `internal/admin/adapters/http/app_settings.go` (new) + `SystemRoutes()` on the shared Handler: `GET /api/v1/admin/settings/system` (200 all 21 typed atomic rows), `PUT /api/v1/admin/settings/system/{key}` (per-setting update; 200 + audit). Unknown key → 400 German; type mismatch → 400 German; non-whole number → 400 German; below the setting's minimum (all settings non-negative, most min 1) → 400 German; above the type/overflow cap → 400 German; lockout threshold pair inverting short<long → 400 German; empty text → 400 German. Uniform envelope.
- **Mount** in `cmd/server/main.go`: `/api/v1/admin/settings/system` behind `auth.RequireAnyPermission(..., []string{admcore.AppSettingsPermission}, ...)` (mirror the schedules mount; do NOT widen the SMTP/backup gates).
- **SPA "System" tab** — extend `AdminEinstellungenPage` `Tab` union + tab button with a `SystemSettingsTab`:
  - **Table layout** (one row per setting): name (German), current value (formatted per value_type), an editable input, and a **"?" button**.
  - The "?" opens a **popup** explaining that setting — a new lightweight accessible `InfoPopup` component (no Modal/Tooltip exists to reuse): a button toggles a popover; close on Escape/outside-click/close button; `aria-expanded` on the trigger, `role="dialog"`-style popover with a labelled German description. The popup is read-only help.
  - Editable inputs typed per value_type (duration in seconds or a friendly format, integer, text); save per row with inline German feedback; the server is authoritative (400s surface inline).
  - Gated by `admin.settings.system`; add the client permission const + API client in `web/src/auth/settings.ts` (getSystemSettings, updateSystemSetting).
- **Catalog:** `admin.settings.system` in `AdminModuleAccessCodes()` (users_admin_permissions.go) + `BasePermissionCodes`/label (roles.go + web roles.ts) so holders reach the module + surface.
- **No consumer adoption in this story** — the values are editable + stored, but SMTP/backup/user/tools still read their current constants. The port + seeded rows make adoption a clean follow-up.

**Ask First:**
- None (the split is user-resolved; the rest follows the proposal + the existing settings-surface patterns).

**Never:**
- No consumer adoption here (deferred to the follow-up story).
- No cross-module SQL reads of `app_settings` (AD-11).
- No changes to frozen strings, password policy, TOTP/Argon2id params, or design tokens.
- No session/HTTP server timeouts moved into admin settings (infra/env, excluded by the proposal).
- No widening of the SMTP/backup/schedules gates.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| GET_ALL | app_settings seeded | 200 all 21 atomic settings (typed values) | n/a |
| PUT_SETTING | valid value for a known key | 200 updated + audited | n/a |
| PUT_UNKNOWN | key not in the catalog | 400 German | 400 |
| PUT_TYPE_MISMATCH | duration key gets text / int key gets text | 400 German | 400 |
| PUT_NEGATIVE | negative duration/width/len | 400 German (bounds) | 400 |
| PUT_WHOLE_NUMBER | 10.5 for an integer/duration key | 400 German | 400 |
| PUT_BELOW_MIN | value below the setting minimum (e.g. otp_length 0, TTL 0) | 400 German (all settings non-negative; security-relevant keys min 1) | 400 |
| PUT_TOO_LARGE | above the type/overflow cap | 400 German | 400 |
| PUT_THRESHOLD_ORDER | lockout short >= long (threshold or duration) | 400 German | 400 |
| PUT_EMPTY_TEXT | empty text value | 400 German | 400 |
| SPA_TABLE | System tab renders | Table with all rows: name, value, input, "?" | n/a |
| SPA_POPUP | "?" clicked | Popover explains the setting; closes on Escape/outside/close | n/a |
| SPA_EDIT | value edited + saved | Row updates, inline feedback | 400 German from server |
| FORBIDDEN | caller lacks admin.settings.system | Uniform 403, no data exposed (AD-6) | 403 |

</frozen-after-approval>

## Code Map

- `migrations/000026_app_settings.{up,down}.sql` -- typed key/value store + seed 14 defaults.
- `sqlc.yaml` -- append 000026 to the admin block; `just sqlc-generate`.
- `internal/admin/ports/ports.go` -- `AppSettingsPort { CurrentAppSettings(ctx) }`.
- `internal/admin/core/app_settings.go` (new) + `settings.go` consts -- `AppSettingsPermission`, typed `AppSettings`, `CurrentAppSettings`/`UpdateAppSettings`, audit.
- `internal/admin/adapters/postgres/` -- app_settings queries + repo (list all, upsert one key).
- `internal/admin/adapters/http/app_settings.go` (new) -- `SystemRoutes()` + GET/PUT handlers + DTO.
- `cmd/server/main.go` -- mount `/api/v1/admin/settings/system` behind `admin.settings.system` (no consumer wiring yet).
- `internal/user/core/users_admin_permissions.go` + `roles.go` (+ web `roles.ts`) -- `admin.settings.system` in the outer gate + catalog + label.
- `web/src/auth/settings.ts` -- `SYSTEM_SETTINGS_PERMISSION` + getSystemSettings/updateSystemSetting client.
- `web/src/components/InfoPopup.tsx` (+css, new) -- accessible per-row "?" popover.
- `web/src/pages/admin/AdminEinstellungenPage.tsx` (+css) -- 4th "System" tab (table layout, per-row "?" popup, per-setting save).
- Tests: admin core (Current/Update/validation/audit), postgres (seed count 21 + upsert), http (GET/PUT/400/403), composition mount gate, web (table render, popup open/close, edit/save, 403).

## Tasks & Acceptance

**Execution:**
- [x] `migrations/000026_app_settings.{up,down}.sql` -- typed store + seed -- schema
- [x] `sqlc.yaml` + `just sqlc-generate` -- admin block append -- persistence
- [x] `internal/admin/core/app_settings.go` + `ports.go` -- typed struct, Current/Update, AppSettingsPort -- core/ports
- [x] `internal/admin/adapters/postgres/` -- app_settings queries + repo -- persistence
- [x] `internal/admin/adapters/http/app_settings.go` -- SystemRoutes + handlers/DTO -- API
- [x] `cmd/server/main.go` -- mount behind admin.settings.system -- composition root
- [x] `internal/user/core/users_admin_permissions.go` + `roles.go` (+ web roles.ts) -- gate + catalog -- gate
- [x] `web/src/auth/settings.ts` -- client + permission const -- SPA
- [x] `web/src/components/InfoPopup.tsx` (+css) -- "?" popover -- SPA
- [x] `web/src/pages/admin/AdminEinstellungenPage.tsx` (+css) -- System tab table -- SPA
- [x] Tests -- admin core/http/postgres/composition/web incl. I/O rows -- verification
- [x] `migrations/README.md` -- document 000026 -- docs

**Acceptance Criteria:**
- Given an admin with `admin.settings.system`, when they open Einstellungen → System, then the settings are listed in a table, each with a "?" popup explaining the setting.
- Given a value is changed + saved, then it persists in `app_settings` (audited) and reads back on GET.
- Given a caller lacks `admin.settings.system`, then every endpoint answers uniform 403 with no data exposed (AD-6).

## Spec Change Log

- **Scope split (2026-09-13, user chose [S]):** the consumer-adoption half (threading A1–A9/B1/C1/C2/D1/D2 into the SMTP/backup/user/tools adapters, incl. SQL literal→param changes) is deferred to a follow-up story, recorded in deferred-work.md. This spec covers the app_settings infrastructure + System tab UI only.
- **Review reconciliation (2026-09-14, user decisions):** the "14" wording is amended to "14 proposal defaults modeled as 21 atomic rows" everywhere; the I-O matrix now documents the added validation checks (whole-number, below-min, too-large, threshold-order); the security-relevant duration keys gain min 1 and the lockout duration pair joins the threshold-order invariant.

## Design Notes

- **Foundation-only, adoption-ready:** the seeded `app_settings` rows + `AppSettingsPort` make the follow-up story a clean wiring job — consumers read `CurrentAppSettings` (or a structural `AppSettingsReader`) instead of their constants.
- **One surface, many future consumers:** the SPA table + admin API are one surface; the value threading is deliberately deferred so each spec stays reviewable.
- **Popup is net-new:** no Modal/Tooltip component exists; `InfoPopup` is a lightweight accessible popover (button toggles; Escape/outside/close dismisses; `aria-expanded` + labelled popover).

## Verification

**Commands:**
- `just sqlc-generate` && `just migrate-up` -- expected: 000026 applies; `SELECT count(*) FROM app_settings` = 21
- `just build` && `just vet` && `just test -p 1` && `just lint` -- expected: all Go/web tests pass, 0 lint issues
- `curl` as admin: GET /api/v1/admin/settings/system (200, 21); PUT a known key (200); PUT unknown/type-mismatch/negative (400) -- expected per matrix
- `curl` as non-holder: GET -- expected: uniform 403
- `npx vitest run` in web/ -- expected: all pass incl. System tab + popup tests

**Manual checks (if no CLI):**
- Einstellungen → System renders the table; each "?" opens a popup explaining the setting; editing a value saves and it reads back on reload.

### Review Findings

**Decision needed:**

- [x] [Review][Decision] Spec counts "14" vs the shipped 21 atomic rows — the spec freezes "14" in Intent/Constraints/I-O matrix/Verification, but `app_settings` ships 21 atomic rows (A3/B1/C1/C2 expand to pairs/groups). **Resolved (2026-09-14, user):** the spec is amended to "14 proposal defaults modeled as 21 atomic rows" everywhere (matches README + all code/tests).
- [x] [Review][Decision] Validation hardening beyond the spec's I-O matrix — the implementation adds whole-number, min:1, max-cap and lockout threshold-order checks (and rejects `otp_length=0`), while the six security durations (SMTP/backup timeouts, reset/recovery TTL, forgot throttle, MFA window) still accept `0`; the lockout duration pair (short/long) is also unenforced. **Resolved (2026-09-14, user):** accept the hardening + the nine security/business durations gained `min: 1`, the lockout duration pair joined the threshold-order invariant, and the spec's I-O matrix now documents all added checks.

**Patches:**

- [x] [Review][Patch] main_test.go `compSettingsService.GetAppSettings` assigns raw ints to `time.Duration` (ns) — every duration row renders `0` on the wire; the mount-gating test only asserts `otp_length` + row count, so the duration projection is untested. Multiply by `time.Second`. [cmd/server/main_test.go:178]
- [x] [Review][Patch] main_test.go `compSettingsService.UpdateAppSettings` ignores `input.Value` and returns the pre-update row — the PUT write path is not exercised. Echo the submitted value. [cmd/server/main_test.go:204]
- [x] [Review][Patch] `toSystemSettingDTO` nil-derefs if `AppSettingFor` returns nil (latent panic; reachable today only through a nil-returning service path). Add a nil guard. [internal/admin/adapters/http/app_settings.go:118]
- [x] [Review][Patch] GET surface fabricates plausible zero rows for catalog keys whose DB row is missing/drifted (skipped by `CurrentAppSettings`) — a deleted seed row ships as `value: 0`/`""`, indistinguishable from a real zero. Render only rows actually resolved from the store. [internal/admin/core/app_settings.go:318]
- [x] [Review][Patch] Cross-key lockout invariant is silently skipped when the current-value read fails (`curErr != nil`) — a threshold update can invert the progressive policy. Fail closed (return the error) for `lockout_threshold_*` updates. [internal/admin/core/app_settings.go:430]
- [x] [Review][Patch] PUT accepts trailing content after the JSON object (second `Decode` never checked for `io.EOF`). Reject it. [internal/admin/adapters/http/app_settings.go:92]
- [x] [Review][Patch] `AdminModuleAccessCodes()` pinning list in `users_admin_test.go` was not extended with `admin.settings.system`. Add it. [internal/user/core/users_admin_test.go:65]
- [x] [Review][Patch] No PUT→GET round-trip test through the HTTP handler chain (core-only today). Add one. [internal/admin/adapters/http/app_settings_test.go]
- [x] [Review][Patch] `TestPostgresAppSettingsStore` asserts every catalog row equals the seed default and restores only its own two edits inline — an admin editing ANY key via the new PUT surface makes it fail and mutates the shared dev DB without `t.Cleanup` (the SMTP data-loss hazard). Snapshot/restore in `t.Cleanup` + relax the seed-exactness assertion. [internal/admin/adapters/postgres/app_settings_test.go]
- [x] [Review][Patch] InfoPopup focuses the trigger on mount (the `else` branch of the focus effect runs on initial render) — all 21 "?" buttons steal focus when the System tab opens. Only return focus on close after an open. [web/src/components/InfoPopup.tsx:52]
- [x] [Review][Patch] InfoPopup's focus contract (into dialog on open, back to trigger on close) has zero tests. Add `toHaveFocus` assertions. [web/src/components/InfoPopup.test.tsx]
- [x] [Review][Patch] InfoPopup close button is a 32px target, below the page's ≥48px a11y contract. Enlarge. [web/src/components/InfoPopup.module.css:309]
- [x] [Review][Patch] System-tab "?" popover is pinned `left:0` with no viewport flip — rows at the right edge render the popover off-screen. Anchor/flip it. [web/src/components/InfoPopup.module.css:279]
- [x] [Review][Patch] `SystemSettingInput` interface is declared but unused (updateSystemSetting inlines `{ value }`). Remove. [web/src/auth/settings.ts:311]
- [x] [Review][Patch] The client-side empty-numeric guard has no test and its comment cites a review artifact. Add a test (cleared numeric field → inline error, no PUT) and rewrite the comment. [web/src/pages/admin/AdminEinstellungenPage.tsx:1329]
- [x] [Review][Patch] The `unit` display branch of `formatCurrentValue` is never exercised — the fixture omits `unit` on every row. Add `unit` to fixtures and assert "16384 Bytes"/"14 Tage". [web/src/pages/admin/AdminEinstellungenPage.test.tsx:721]
- [x] [Review][Patch] No duration-row save test (seconds on the wire + friendly-label update). Add one. [web/src/pages/admin/AdminEinstellungenPage.test.tsx]
- [x] [Review][Patch] A row input stays editable while its save is in flight and `setDrafts` then overwrites the draft with the server value — typing during the request is silently discarded. Disable the input while `busy`. [web/src/pages/admin/AdminEinstellungenPage.tsx:1342]
- [x] [Review][Patch] A numeric input like `1e309` becomes `Infinity` → `JSON.stringify` sends `value:null`. Guard `Number.isFinite` client-side. [web/src/pages/admin/AdminEinstellungenPage.tsx:1336]
- [x] [Review][Patch] A zero-row server response renders an empty table with no message. Add an empty-state line. [web/src/pages/admin/AdminEinstellungenPage.tsx:1373]
- [x] [Review][Patch] The new `admin.settings.system` holder's admin-module exposure (sidebar ADMIN link, Einstellungen landing card, `/admin/einstellungen` route guard) is unpinned — `TAB_GATING_SYSTEM` mounts the page directly. Add nav/landing/guard tests for a system-only holder. [web/src/auth/permissions.test.ts]
- [x] [Review][Patch] The German label for `admin.settings.system` ("System-Einstellungen verwalten") is unpinned in the fallback catalog. Assert it. [web/src/pages/admin/AdminRollenPage.test.tsx:184]
- [x] [Review][Patch] `SPA_EDIT_TEXT` is titled "PUTs the trimmed string" but the client sends raw (the server trims). Fix the title or add a client trim. [web/src/pages/admin/AdminEinstellungenPage.test.tsx:823]

**Deferred (pre-existing / follow-up):**

- [x] [Review][Defer] `findAppSetting` re-lists the whole `app_settings` table to read one key after every upsert — a `GetByKey` query would be cleaner. — deferred, follow-up
- [x] [Review][Defer] `updated_at` is dropped from the wire DTOs; concurrent editors are silently last-write-wins with no staleness signal (ETag/If-Match or expose `updated_at`). — deferred, follow-up
- [x] [Review][Defer] The cross-key lockout invariant is a non-transactional read-then-write; concurrent threshold edits could interleave. Needs a store-level transaction (the fail-closed patch is the cheap mitigation). — deferred, follow-up
- [x] [Review][Defer] `role="dialog"` popover has no focus trap / `aria-modal`; Tab can continue into background content. — deferred, follow-up
- [x] [Review][Defer] Seconds-editing vs friendly-label display mismatch (admin converts "30 Tage" → 2592000 mentally) — acknowledged UX trade-off. — deferred, follow-up
- [x] [Review][Defer] Focus is not restored to the trigger if the component unmounts while the popup is open (tab switch). — deferred, follow-up
- [x] [Review][Defer] Unknown/drifted server settings render an empty "?" popup (server-authoritative; only reachable from drift). — deferred, follow-up
- [x] [Review][Defer] `TestPostgresSmtpSettingsStore`'s nil-on-empty assertion runs only when the shared dev DB has no SMTP row. — deferred, pre-existing
- [x] [Review][Defer] `NewService` has grown to 10 positional args — an options struct would reduce future churn. — deferred, follow-up
- [x] [Review][Defer] `BasePermissionCodes` is duplicated across 4 files (roles.go + 3 test fixtures) — future permission additions risk silent drift. — deferred, pre-existing
- [x] [Review][Defer] Orphaned/non-catalog `app_settings` rows are only logged; no admin-visible signal on the surface. — deferred, follow-up
- [x] [Review][Defer] Down migration 000026 removes the permission rows while Go code keeps the codes — inherent to down migrations (code and DB cannot roll back together). — deferred, pre-existing
- [x] [Review][Defer] The client-abort guard in `mapSystemSettingError` covers only the default (500) branch. — deferred, follow-up