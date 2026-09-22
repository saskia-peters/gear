---
title: 'Idempotency Hardening (NFR-R1/FR-12, FR-15)'
type: 'feature'
created: '2026-09-20'
status: 'done'
review_loop_iteration: 0
baseline_commit: '272130bc6d707284682f650d804ff116710cc9e8'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-7-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** `POST /api/v1/tools/{id}/inspection` and `POST /api/v1/tools/{id}/reinstatement` are append-only with NO unique guard: a retried request (timeout/500 where the server committed, or a double-click racing the SPA guard) persists duplicate rows. The only mitigations today are client-side button disabling.

**Approach:** Make both write paths at-most-once via a **client-supplied idempotency key**. The inspection and reinstatement tables gain a `idempotency_key uuid NOT NULL` column with a UNIQUE constraint per tool; the SPA generates a `crypto.randomUUID()` per submit intent and reuses it across retries; the repository answers a duplicate key by **returning the already-persisted record** (idempotent replay), not an error, so a retried client sees the same committed result.

## Boundaries & Constraints

**Always:**
- **Schema:** migration `000032` adds `idempotency_key uuid NOT NULL` to `inspections` and `reinstatements`, backfilled (existing rows get a UUID via `uuidv7()` — PG18 built-in), then a UNIQUE constraint on `(tool_id, idempotency_key)`. Backfill-then-NOT NULL follows the `000025` pattern.
- **Key contract:** the key is a client-supplied UUID carried in the request body — `idempotency_key` field on the inspection submit body and on the reinstatement body. The HTTP layer validates it is a parseable UUID; a MISSING or INVALID key → `400 invalid_request` (German). The key is required (NOT NULL), never generated server-side.
- **Replay semantics:** the core checks for an existing record by `(tool_id, idempotency_key)` EARLY — right after the permission gate and tool load, BEFORE the OOS gate and the insert. A found record is a replay: the service returns the existing inspection WITH its items + the derived status (same post-commit derivation), skipping the OOS/qualification/validation gates and the insert. This ordering matters: a retried submit of an already-committed FAIL (or a retried reinstate after it flipped the tool serviceable) must replay 200, NOT hit the OOS gate and 403. The insert's `ON CONFLICT DO NOTHING` remains as the race-free backstop for CONCURRENT_RACE. Replay never re-audits and never double-inserts.
- **Key stays stable per submit intent:** the SPA generates the key when the user starts a submit (inspection page `handleSubmit`, dashboard reinstate dialog) and REUSES the same key for that intent's retries (user retries after an inline error). A fresh submit intent (new inspection of another tool / new reinstate) gets a fresh key.
- **Spine rule:** no changes to the check/status logic, no changes to existing read surfaces, no new third-party dependency.

**Ask First:**
- None.

**Never:**
- No `409 Conflict` for duplicate keys (the epic's at-most-once requirement is satisfied by replay, and the SPA retry story expects the same 200).
- No server-side key generation and no optional/missing-key fallback (a missing key must fail the request — otherwise the guard is silently bypassable).
- No changes to the inspection check/status derivation or the schedule/qualification gates.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| FRESH_SUBMIT | valid body + key, tool eligible | row inserted, 200 with record + derived status | n/a |
| RETRY_AFTER_COMMIT | same tool + same key, first attempt already committed | 200 with the EXISTING record + derived status (no second row) | n/a |
| RETRY_FAIL_TURNS_OOS | first submit was a FAIL (tool now OOS); retry with same key | 200 replay (NOT 403 — replay check runs BEFORE the OOS gate) | n/a |
| DOUBLE_CLICK | SPA sends same key twice rapidly | one row; second response replays the first | n/a |
| MISSING_KEY | body without `idempotency_key` | 400 `invalid_request`, German message | n/a |
| INVALID_KEY | `idempotency_key` not a UUID (e.g. `"abc"`) | 400 `invalid_request`, German message | n/a |
| KEY_REUSED_OTHER_TOOL | same key, different tool | both writes succeed (scope is per tool) | n/a |
| CONCURRENT_RACE | two identical submits race | DB unique constraint wins: one row, loser replays | 23505 → replay |
| SPA_RETRY | user retries after an inline 4xx/5xx error | SAME key resent → server replays if it had committed | n/a |
| SPA_FRESH_INTENT | user starts a new inspection/reinstate | NEW key generated | n/a |

</frozen-after-approval>

## Code Map

- `migrations/000032_*.up.sql` / `.down.sql` (new) -- add `idempotency_key uuid` to `inspections` + `reinstatements`, backfill `uuidv7()`, `SET NOT NULL`, `CREATE UNIQUE INDEX ... (tool_id, idempotency_key)`; down drops the columns.
- `sqlc.yaml` (tools block) -- append `000032` to the schema list so `just sqlc-generate` sees the new column (000025 did the same for its column).
- `internal/tools/adapters/postgres/queries.sql` -- `InsertInspection` adds `idempotency_key`; new `-- name: FindInspectionByToolAndKey :one` (`WHERE tool_id=$1 AND idempotency_key=$2`); `InsertReinstatement` adds `idempotency_key`; new `-- name: FindReinstatementByToolAndKey :one`. `InsertReinstatement` switches to `ON CONFLICT (tool_id, idempotency_key) DO NOTHING RETURNING ...` (rows==0 → fetch via Find…ByToolAndKey). `InsertInspection` stays a plain INSERT + `ON CONFLICT (tool_id, idempotency_key) DO NOTHING RETURNING` (rows==0 → fetch inspection WITH items via the existing item-grouped read, mirroring `GetInspectionByID`-style helper if present; else build from Find + items query).
- `internal/tools/adapters/postgres/inspections_repo.go` -- `InsertInspection` (line 28) and `InsertReinstatement` (line 146): add key param, do-nothing-on-conflict, replay-fetch path; reuse `isUniqueViolation` (tool_types_repo.go:366) or the `pgconn.PgError` 23505 pattern already in the module.
- `internal/tools/adapters/postgres/queries.sql.go` (generated) -- regenerated via `just sqlc-generate` (SQLC_VERSION pinned in justfile:181).
- `internal/tools/core/inspections.go` -- `Inspection` struct gains `IdempotencyKey`; `InsertInspection`/`InsertReinstatement` call sites pass it; store port methods (`InsertInspection` line 349, `InsertReinstatement` line 355) gain the key param; new store method `FindInspectionByToolAndKey(ctx, toolID, key)` / `FindReinstatementByToolAndKey(ctx, toolID, key)` for the early replay check.
- `internal/tools/core/inspections.go` `SubmitInspection` (line 422) / `ReinstateTool` (line 588) -- accept `idempotencyKey string`; right after the permission gate + tool load, query `Find…ByToolAndKey`; a hit → return the existing record + derived status (replay), skipping OOS/qualification/validation gates and the insert. No change to check/status derivation.
- `internal/tools/ports/ports.go` -- `SubmitInspection` (line 114) / `ReinstateTool` (line 122) signatures gain `idempotencyKey string`.
- `internal/tools/adapters/http/inspection.go` -- `SubmitInspection` (line 179) / `ReinstateTool` (line 232): decode `idempotency_key` from the body DTO, validate it is a parseable UUID (mirror `parseOptionalUUID` at tools_repo.go:421 for the parse), pass to the service; invalid/missing → `httpapi.WriteError(400, "invalid_request", …)`.
- `internal/tools/adapters/http/inspection.go` DTOs (`inspectionSubmitResponseDTO` is read-side; the submit INPUT DTO currently IS `toolscore.InspectionInput` decoded directly — add an `IdempotencyKey` field to the request decoding, e.g. a thin submit-request struct that embeds the input fields + key, or add `idempotency_key` into the decoded JSON via a wrapper; reinstatement DTO (line 72) gains `IdempotencyKey string \`json:"idempotency_key"\``).
- `web/src/auth/tools.ts` -- `submitInspection` (line 496) / `reinstateTool` (line 525) accept `idempotencyKey` and include `idempotency_key` in the JSON body.
- `web/src/pages/InspectionPage.tsx` `handleSubmit` (line 305) / `web/src/pages/DashboardPage.tsx` `handleReinstate` (line 282) -- generate `crypto.randomUUID()` when the submit intent starts, store in a ref/local state, REUSE on retry, reset on success.
- Tests: `internal/tools/adapters/postgres/inspections_test.go` (`TestPostgresInsertReinstatement` line 209, `TestPostgresInspectionStore` line 19) add key + duplicate-replay cases; `internal/tools/core/inspections_test.go` fake store (`tools_test.go:90/147`) + replay tests; `internal/tools/adapters/http/tools_test.go` add missing-key / invalid-key / replay cases (helpers: `toolsInspectionGateway` line 328, `submitPassBody` line 1442, `doRequest` line 162); `web/src/auth/tools.test.ts` and `web/src/pages/InspectionPage.test.tsx` / `DashboardPage.test.tsx` for key generation + reuse.

## Tasks & Acceptance

**Execution:**
- [x] `migrations/000032_*.up.sql` + `.down.sql` (new) -- add `idempotency_key` (backfill→NOT NULL) + UNIQUE per tool -- schema
- [x] `sqlc.yaml` -- append `000032` to the tools schema list -- codegen
- [x] `internal/tools/adapters/postgres/queries.sql` -- key in both INSERTs + do-nothing-on-conflict + `Find…ByToolAndKey` -- queries
- [x] `just sqlc-generate` -- regenerate queries.sql.go -- codegen
- [x] `internal/tools/adapters/postgres/inspections_repo.go` -- key param + replay-fetch on conflict -- repository
- [x] `internal/tools/core/inspections.go` -- `Inspection.IdempotencyKey`, store port + service signatures, replay short-circuit -- core
- [x] `internal/tools/ports/ports.go` -- service signature `idempotencyKey` -- port
- [x] `internal/tools/adapters/http/inspection.go` -- decode + validate key, pass through -- HTTP
- [x] `web/src/auth/tools.ts` -- send key in both bodies -- SPA client
- [x] `web/src/pages/InspectionPage.tsx` + `web/src/pages/DashboardPage.tsx` -- generate/reuse/reset key per intent -- SPA pages
- [x] Tests (postgres + core + http + web) -- replay, missing/invalid key, key reuse -- coverage

**Acceptance Criteria:**
- Given a valid submit with a new `idempotency_key`, when it POSTs, then exactly one row is persisted and 200 returns the record + derived status.
- Given a second submit with the same tool + key (the first committed), when it POSTs, then NO second row exists and 200 returns the FIRST record + derived status — including when the first submit was a FAIL and the tool is now OOS (replay check precedes the OOS gate).
- Given a body without `idempotency_key` or with a non-UUID key, when it POSTs, then 400 `invalid_request` (German) and nothing is persisted.
- Given the SPA, when a user submits/reinstates and then retries after an inline error, then the SAME key is resent; a fresh intent gets a fresh key.
- Given the full test suites, when `go test ./...` + `go test -tags integration` + web tests + `just lint` run, then all pass.

## Spec Change Log

- **2026-09-20, review patches (review 1):** (a) replay no longer double-audits under a concurrent race — `InsertInspection`/`InsertReinstatement` return a `replayed` flag the core branches on to skip the audit write (spec invariant "Replay never re-audits" now holds for the absorbed path too); (b) the early replay check moved BEFORE the tool load so an archived/unknown tool no longer blocks a replay (a committed record always replays 200); (c) core gained defense-in-depth idempotency-key validation (empty/non-UUID → German `InvalidInspectionError`), matching the HTTP backstop; (d) DashboardPage deletes the per-tool key only after the success flow resolves (survives the committed-but-refetch-failed window) and on dialog cancel; the key is bound to the reason, and a fresh-intent-after-success test pins it; (e) InspectionPage key is signature-bound — regenerated when toolId OR the submit payload changes after a failure (a changed payload is a new logical write), unchecked `!` removed; (f) added the missing concurrent-race postgres test (two goroutines, one row, exactly one `replayed`), the replay-schedule-failure fallback test, the `normalizeIdempotencyKey` unit test, and removed dead `errors.Is` branches in the replay tests; (g) `InsertReinstatement` returns the found row on replay (no discarded `ErrNoRows`). KEEP: replay-not-409, per-tool unique scope, early-check-before-OOS ordering, backfill-then-NOT-NULL, body-carried key.

## Design Notes

- **Replay, not 409:** the epic requires at-most-once; the SPA's retry story (a user retries after an inline 5xx) only works if the retried request returns the same 200 result the first committed attempt produced. A 409 would force the client to reconcile a conflict — unnecessary when the write is append-only and deterministic.
- **Early replay check, before the OOS gate:** `SubmitInspection` rejects an OOS tool with 403. A retried submit of an already-committed FAIL inspection arrives on a now-OOS tool — if the replay check ran only at the insert (after the OOS gate), the retry would 403 and the client would never see the committed record. The `Find…ByToolAndKey` check therefore runs right after the permission gate + tool load, before OOS/qualification/validation. The insert-time `ON CONFLICT DO NOTHING` stays as the race backstop for concurrent duplicates (which skip the early check only if they race the first insert — but then the unique constraint still dedupes and the loser replays).
- **UNIQUE per tool, not global:** the constraint is `(tool_id, idempotency_key)` so the same key value across two different tools is legal (the key only needs to be unique within a tool's append stream). A global unique would turn a reused key into a hard error across tools.
- **Backfill pattern:** the `000025` precedent — add nullable, backfill all existing rows with `uuidv7()`, then `SET NOT NULL`, then the unique index. Idempotency keys for the pre-existing dev rows are one-off values; they never need to be replayed.
- **Do-nothing + fetch is the race-free replay:** `INSERT … ON CONFLICT (tool_id, idempotency_key) DO NOTHING RETURNING …`; when `RowsAffected()==0` the repo fetches the existing row (inspection: the row + its items in the same tx) and returns it. The DB constraint is the authority under concurrency; the service cannot double-insert.
- **HTTP body vs header:** the key rides in the JSON body (`idempotency_key`), not a header — matching the project's envelope + German-error conventions and keeping the SPA's `request()` helper untouched.

## Verification

**Commands:**
- `DATABASE_URL=... go test ./internal/tools/... ./cmd/...` -- expected: all pass (fresh submit + replay + missing/invalid key)
- `go test -tags integration ./internal/user/adapters/postgres/` (via `just test-integration`) -- expected: seed assertions still green (migration 000032 does not disturb the seed)
- `cd web && npm test` -- expected: SPA tests pass (key generation + reuse)
- `just lint` -- expected: 0 issues
- Negative check: submit twice with the same key, assert exactly one row in `inspections`/`reinstatements` via psql.

**Manual checks (if no CLI):**
- Inspect migration `000032`: backfill before NOT NULL; unique index per tool; down restores.

## Suggested Review Order

**Entry point — the at-most-once contract**

- The core replay semantics: early check, absorbed-duplicate signal, skip-audit, replay derivation
  [`inspections.go:478`](../../internal/tools/core/inspections.go#L478)
- The replay never errors and skips the audit (spec invariant)
  [`inspections.go:648`](../../internal/tools/core/inspections.go#L648)

**Schema — the unique guard**

- Backfill before NOT NULL, then the per-tool UNIQUE index (ON CONFLICT target)
  [`000032_idempotency_keys.up.sql:29`](../../migrations/000032_idempotency_keys.up.sql#L29)

**Repository — the race-free replay**

- InsertInspection: ON CONFLICT DO NOTHING + fetch the winner's row, `replayed` flag
  [`inspections_repo.go:40`](../../internal/tools/adapters/postgres/inspections_repo.go#L40)
- The early lookup used by core before the OOS gate
  [`inspections_repo.go:190`](../../internal/tools/adapters/postgres/inspections_repo.go#L190)
- InsertReinstatement returns the found row (no discarded ErrNoRows)
  [`inspections_repo.go:258`](../../internal/tools/adapters/postgres/inspections_repo.go#L258)

**Domain validation**

- Defense-in-depth key validation (empty/non-UUID → German 400)
  [`inspection_validation.go:22`](../../internal/tools/core/inspection_validation.go#L22)

**HTTP binding**

- The key rides in the body (submit + reinstatement DTOs), normalized before the service
  [`inspection.go:73`](../../internal/tools/adapters/http/inspection.go#L73)
- normalizeIdempotencyKey: lowercase + trim, the load-bearing normalization
  [`inspection.go:114`](../../internal/tools/adapters/http/inspection.go#L114)

**SPA — key lifecycle per intent**

- DashboardPage: key bound to the reason, deleted after success resolves + on cancel
  [`DashboardPage.tsx:79`](../../web/src/pages/DashboardPage.tsx#L79)
- InspectionPage: signature-bound key — regenerated on toolId/payload change, no `!`
  [`InspectionPage.tsx:161`](../../web/src/pages/InspectionPage.tsx#L161)

**Tests (supporting)**

- Concurrent-race: two goroutines, one row, exactly one `replayed`
  [`inspections_test.go:409`](../../internal/tools/adapters/postgres/inspections_test.go#L409)
- Replay after commit, before OOS gate, schedule-failure fallback, archived-tool
  [`inspections_test.go:1295`](../../internal/tools/core/inspections_test.go#L1295)
- Key normalization unit test
  [`tools_test.go:1790`](../../internal/tools/adapters/http/tools_test.go#L1790)