---
title: 'Out of Service Flagging — Foundation (FR-14/AD-4/AD-5)'
type: 'feature'
created: '2026-09-14'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'e3bcebb28cabe8fc58aeb593ec9686e44c8c57f2'
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-5-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** A failed inspection must make a tool unsafe-to-use immediately, but no inspection records, failure persistence, or derived-status function exist — the inspection surface (Story 5.2) is a UX-only placeholder with no backend (no `inspections` table, no status/clock code).

**Approach (foundation scope, user-resolved 2026-09-14):** build the OOS backend foundation that Stories 5.4/5.5 will feed and 6.1 will render: an `inspections` + `inspection_items` + `reinstatements` data model (migration 000027), a `SubmitInspection` persistence seam (permission + qualification re-validated, persisted, audited, returns the record + derived status), the shared **derived-status/clock function** (OOS / Red / Orange / Green + next_due, AD-4/AD-5), and the SPA consequence copy ("⛔ Wird als Außer Betrieb gesperrt") in the existing submit confirmation. OOS is DERIVED on read — never stored as a flag.

## Boundaries & Constraints

**Always:**
- **Migration 000027** `inspections.{up,down}.sql` (Tool-module-owned): `inspections` (id uuid PK, tool_id uuid FK→tools, inspector_id uuid — plain, no FK (AD-8/3.4 DSGVO rewrite), mode text CHECK pass_fail|checklist, overall_result text CHECK pass|fail, notes text, submitted_at timestamptz DEFAULT now()), `inspection_items` (id, inspection_id FK→inspections ON DELETE CASCADE, item_id uuid plain, label text — snapshot at submit, position int, result text CHECK pass|fail), `reinstatements` (id, tool_id FK→tools, actor_id uuid plain, reason text NOT NULL, created_at). Append 000027 to the **tools** sqlc schema block (`sqlc.yaml` ~L74-87); `just sqlc-generate`.
- **Derived status, never stored** (AD-4/AD-5): new pure `internal/tools/core/status.go` — `ToolStatusCode` (`oos|red|orange|green`), `ToolStatus{Status, NextDue *time.Time}`, `scheduleInterval(unit, magnitude) time.Duration` (year=365d, quarter=91d, month=30d, week=7d, day=1d — documented approximation), and `deriveToolStatus(latest *Inspection, lastSuccessAt, lastReinstatedAt *time.Time, interval, now, orangeWindowDays)`.
- **`SubmitInspection` core** (`internal/tools/core/inspections.go`): re-checks `inspection.submit` (AD-6) AND re-validates the tool-type qualification on submit (never trusts the client, FR-11); loads `GetToolWithTypeQualification`; rejects archived/unknown tool; validates mode matches the type, overall_result pass|fail, notes ≤ 4000 runes, and for checklist mode the item set exactly equals the type's checklist (all answered, no extras, each pass|fail) — pass_fail mode rejects provided items. Persists via a new `InspectionStore` (transactional insert of inspection + items), audits `inspection.submit`, returns the persisted record + `deriveToolStatus` output.
- **HTTP seam** `POST /api/v1/tools/{id}/inspection` behind the existing `inspection.submit` gate (add to the existing `InspectionRoutes()` in `internal/tools/adapters/http/tools.go`): DTO `{mode, result, notes, items[]}` → `{inspection, status}`; 400 German for validation (sentinel `ErrInspectionInvalid`), 403 uniform for gating, 404 `ErrToolNotFound`.
- **SPA consequence copy** (`web/src/pages/InspectionPage.tsx` confirmation ~L356-360): when the outcome is a fail (pass_fail result `'fail'` or checklist failedCount > 0), append "⛔ Wird als Außer Betrieb gesperrt" to the `role="status"` confirmation — copy only, the placeholder seam is untouched (no fetch; 5.4/5.5 wire it). Reuse the canonical term "Außer Betrieb" (`web/src/types/filters.ts:1`).
- Orange window = **static 14 days** for now (app_settings `inspection_orange_window_days` consumer adoption is deferred per deferred-work.md; the follow-up adoption story threads it).

**Ask First:**
- None.

**Never:**
- No color-coded dashboard rendering (Story 6.1 owns it — this story only produces the derived status).
- No reinstatement WRITE path (Story 5.6 owns it; here only the table + the derivation consult it).
- No SPA fetch to the submit endpoint (5.4/5.5 wire the real record call).
- No OOS/status column stored anywhere; no cross-module SQL joins to user tables (AD-8/AD-11).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| SUBMIT_PASSFAIL | pass_fail mode, result pass | 200 inspection (pass, no items) + derived status (Green if fresh) | 400 if no/mismatched result |
| SUBMIT_FAIL | pass_fail result fail | 200 inspection (fail) + status `oos`, NextDue nil | n/a |
| SUBMIT_CHECKLIST | checklist mode, all items answered, ≥1 fail | 200 inspection (fail, items persisted incl. label snapshot) + status `oos` | 400 if any item unanswered or extra item |
| SUBMIT_GATED | caller lacks inspection.submit or qualification | 403 uniform, no record | 403 |
| SUBMIT_ARCHIVED | archived tool | 404/400 German, no record | 404 |
| SUBMIT_UNKNOWN | nonexistent tool id | 404 German | 404 |
| SUBMIT_NOTES | notes > 4000 runes | 400 German | 400 |
| DERIVE_OOS | latest inspection fail, no reinstatement after it | status `oos`, Red | n/a |
| DERIVE_REINSTATED | reinstatement after the last fail | not OOS; next_due = reinstatement + interval | n/a |
| DERIVE_NEVER | no inspections | status `red`, NextDue nil | n/a |
| DERIVE_DUE | last success long ago | `red` (past due) / `orange` (≤14d) / `green` (>14d) | n/a |

</frozen-after-approval>

## Code Map

- `migrations/000027_inspections.{up,down}.sql` -- new tables; append to tools sqlc block (`sqlc.yaml` ~L74-87); `just sqlc-generate` (justfile:173).
- `internal/tools/adapters/postgres/queries.sql` + `inspections_repo.go` (new) -- `InsertInspection` (transactional: inspection + items via the existing `beginTx`/`txBeginner` pattern at `tool_types_repo.go:346-362`) and `GetToolInspectionStatus` (latest inspection, latest success submitted_at, latest reinstatement created_at, one query or three). Reuse `parseToolTypeID`/`parseOptionalUUID`/`toolFromRow` helpers. Extend the `Repository`; compile-pin `var _ core.InspectionStore`.
- `internal/tools/core/inspections.go` -- extend with `SubmitInspection` + `Inspection`/`InspectionItem` core types + `ErrInspectionInvalid` (German `MsgInspectionInvalid`) + item/result validation; reuse the qualification re-check (`UserHoldsQualification`, `ErrToolQualificationMissing`) and `auditTool` pattern (`tools.go:233`).
- `internal/tools/core/status.go` (new) -- `scheduleInterval`, `ToolStatus`, `deriveToolStatus` (pure, unit-tested); orange window const 14d.
- `internal/tools/core/tools.go` -- extend `toolModuleStore` (L201-204) with `InspectionStore`.
- `internal/tools/ports/ports.go` -- add `SubmitInspection` to `Service` (L26-81).
- `internal/tools/adapters/http/tools.go` -- `InspectionRoutes()` (L129-135) add `r.Post("/", h.SubmitInspection)`; handler + DTO + `mapInspectionError` (L354-372) maps `ErrInspectionInvalid`→400, `ErrToolQualificationMissing`→403, `ErrToolNotFound`→404.
- `cmd/server/main.go` -- no mount change needed: the inspection mount (L183/L195) is already gated by `inspection.submit`; verify the new sub-path inherits it.
- `web/src/pages/InspectionPage.tsx` (~L356-360) -- consequence sentence in the confirmation when outcome is fail (outcome computed at L194-203); test at `InspectionPage.test.tsx` SUBMIT/checklist-fail cases (~L337-361, L570-588).
- Tests: status matrix (core), SubmitInspection gating/validation/persistence/round-trip (core+postgres), endpoint 200/400/403/404 (http), composition mount, web confirmation copy.

## Tasks & Acceptance

**Execution:**
- [x] `migrations/000027_inspections.{up,down}.sql` + `sqlc.yaml` + `just sqlc-generate` -- inspections/inspection_items/reinstatements -- schema
- [x] `internal/tools/adapters/postgres/` queries + `inspections_repo.go` -- transactional insert + status read -- persistence
- [x] `internal/tools/core/status.go` -- scheduleInterval + deriveToolStatus -- core
- [x] `internal/tools/core/inspections.go` + `tools.go` + `ports/ports.go` -- SubmitInspection (gating/validation/persist/audit/status) -- core/ports
- [x] `internal/tools/adapters/http/tools.go` -- POST /inspection handler + DTO + error mapping -- API
- [x] `web/src/pages/InspectionPage.tsx` -- consequence copy on fail outcome -- SPA
- [x] Tests -- status matrix, submit (gating/validation/persistence), http 200/400/403/404, composition, web copy -- verification

**Acceptance Criteria:**
- Given a failing inspection (pass/fail `NICHT BESTANDEN` or any failed checklist item), when it is submitted, then the failure event persists inspector, timestamp, failed item(s) and notes, and the derived status reads `oos` (Red) — never stored as a flag (FR-14/AD-4).
- Given a tool's status is read, when no reinstatement exists after its latest failed inspection, then OOS is derived from the latest failed inspection not since reinstated and feeds the shared clock (AD-4/AD-5).
- Given the inspection confirmation, when the submitted outcome is a failure, then the inline confirmation names the consequence ("⛔ Wird als Außer Betrieb gesperrt") (UX-DR6/UX-DR8).
- Given a submit attempt, when the caller lacks `inspection.submit` or the required qualification, then it answers uniform 403 with no record (AD-6/FR-11).

## Spec Change Log

- **As-built correction (implementation, 2026-09-14):** the Code Map's "no mount change needed" note was factually stale — `InspectionRoutes` was mounted at `/{id}/inspection/start` (so `POST /` was the start; two same-path POST routes can't coexist). The implementation mounted it at `/{id}/inspection` and moved start to `/start`: start keeps `POST /api/v1/tools/{id}/inspection/start`, submit is `POST /api/v1/tools/{id}/inspection` as the frozen contract requires; the `inspection.submit` gate is inherited by both. Verified live against the real composition root.
- **Review patches (review 1, 2026-09-14):** submit decoder rejects unknown fields + trailing JSON (400); HTTP submit nil-guards `result`/`Inspection`; error-mapper log wording made path-neutral; invalid schedule interval fails loudly (never a silent 0-duration clock); OOS tie boundary = fail at-or-after reinstatement is OOS; trim-before-validate (Result/Notes normalized once); inspection-item re-read moved inside the write transaction; post-commit status read is best-effort (log + derive from the record — never a 500 after commit); empty-checklist submit rejected; schedule-override branch + non-nil next_due wire + loud-failure paths tested; SummaryGrid adopts the canonical `OOS_STATUS` constant; fail-fast item-count guard; HTTP fake snapshots all items; notes-bound comment corrected (runes ≠ UTF-16 maxLength).

## Design Notes

- **OOS derivation:** `oos` iff the latest inspection is a fail AND its `submitted_at` is after the latest reinstatement (or none exists). Otherwise `base = max(last successful inspection, latest reinstatement)`; `next_due = base + interval`; `red` if next_due < now, `orange` if ≤ now+14d, else `green`. Never-inspected = `red` (AD-5). NextDue is nil for `oos` (reinstatement resets it, Story 5.6).
- **Schedule approximation:** calendar math for year/quarter/month (365/91/30 days) is a documented simplification — the seeds (000019) use 1 year/1 quarter/1 month/2 weeks/3 days; 6.1 renders, the follow-up app_settings adoption story threads the 14d window.
- **Audit-snapshot design:** `inspection_items.label` snapshots the item text at submit so history stays self-contained even if the tool type's checklist later changes; `item_id`/`position` are plain uuids/ints (no FK) for the same reason. `inspector_id` is a plain uuid (no FK) so the deferred DSGVO rewrite (3.4) can replace it without a FK constraint.

## Verification

**Commands:**
- `just sqlc-generate` && `just migrate-up` -- expected: 000027 applies; `\d inspections`
- `just build` && `just vet` && `just test -p 1` && `just lint` -- expected: all Go/web tests pass, 0 lint issues
- `curl` as qualified holder: POST /api/v1/tools/{id}/inspection (pass and fail bodies) -- expected: 200 with record + derived status (`oos` on fail)
- `curl` as non-holder / archived / unknown tool / invalid mode -- expected: 403 / 404 / 400 German

**Manual checks (if no CLI):**
- Submit a failing inspection in the SPA: the confirmation names "⛔ Wird als Außer Betrieb gesperrt"; a passing one does not.

## Suggested Review Order

**Derived status / clock — the design intent**

- Entry point: the pure OOS/Red/Orange/Green + next_due function, with the OOS tie now "at-or-after reinstatement".
  [`status.go:78`](../../internal/tools/core/status.go#L78)

- Calendar interval approximation (year/quarter/month/week/day) — documented simplification.
  [`status.go:46`](../../internal/tools/core/status.go#L46)

**Persistence model**

- The three append-only tables (inspections, snapshot items, reinstatements) + indexes.
  [`000027_inspections.up.sql:22`](../../migrations/000027_inspections.up.sql#L22)

- Transactional inspection+items insert; items re-read inside the tx (no post-commit read).
  [`inspections_repo.go:28`](../../internal/tools/adapters/postgres/inspections_repo.go#L28)

- The derived-status input read (latest inspection + pass/reinstatement anchors, nil-safe).
  [`inspections_repo.go:93`](../../internal/tools/adapters/postgres/inspections_repo.go#L93)

- sqlc source queries for the inspection writes + status reads.
  [`queries.sql:223`](../../internal/tools/adapters/postgres/queries.sql#L223)

**Submit core**

- SubmitInspection: gating, qualification re-check, persist, audit, best-effort status after commit.
  [`inspections.go:274`](../../internal/tools/core/inspections.go#L274)

- Effective-schedule resolution (override-else-default) — fails loudly on a bad interval.
  [`inspections.go:371`](../../internal/tools/core/inspections.go#L371)

- The submit contract validation (mode/result/notes/items, trim-before-validate, empty-checklist reject).
  [`inspections.go:402`](../../internal/tools/core/inspections.go#L402)

- The InspectionStore persistence port.
  [`inspections.go:249`](../../internal/tools/core/inspections.go#L249)

**HTTP surface**

- SubmitInspection handler: decoder hardening (unknown fields + trailing JSON) + nil-guard.
  [`tools.go:237`](../../internal/tools/adapters/http/tools.go#L237)

- The wire DTO — inspection record + derived status incl. next_due serialization.
  [`tools.go:521`](../../internal/tools/adapters/http/tools.go#L521)

- Shared start/submit error mapper (path-neutral logging; 400/403/404 mapping).
  [`tools.go:454`](../../internal/tools/adapters/http/tools.go#L454)

- Mount change to `/{id}/inspection` (start → `/start`); gate inherited.
  [`main.go:196`](../../cmd/server/main.go#L196)

- Port surface + combined store wiring.
  [`ports.go:89`](../../internal/tools/ports/ports.go#L89)

**SPA**

- Consequence copy appended to the confirmation on a failed outcome (placeholder seam).
  [`InspectionPage.tsx:209`](../../web/src/pages/InspectionPage.tsx#L209)

- Canonical `OOS_STATUS` constant (filters + consequence).
  [`filters.ts:4`](../../web/src/types/filters.ts#L4)

- Dashboard status card adopts the shared constant.
  [`SummaryGrid.tsx:44`](../../web/src/components/SummaryGrid.tsx#L44)

**Tests**

- Status matrix incl. the OOS tie + schedule-interval cases.
  [`status_test.go:37`](../../internal/tools/core/status_test.go#L37)

- Submit gating / validation / schedule-override / loud-failure / persistence round-trip.
  [`inspections_test.go:288`](../../internal/tools/core/inspections_test.go#L288)

- HTTP submit 200/400/403/404/401/405 + next_due wire + mount gate.
  [`tools_test.go:1226`](../../internal/tools/adapters/http/tools_test.go#L1226)

- Repo insert + rollback + read-after-write against the real DB.
  [`inspections_test.go:18`](../../internal/tools/adapters/postgres/inspections_test.go#L18)