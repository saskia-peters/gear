---
title: 'Schedule Catalog Management (schedules.manage)'
type: 'feature'
created: '2026-09-09'
status: 'done'
review_loop_iteration: 0
baseline_commit: '91ff1067c48e93205b3fd7c77ea7baf3102d7185'
context:
  - '{project-root}/docs/docs/planning/architecture-spine.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Recurring intervals (inspection intervals in V1) are hard-coded and have no single, reusable catalog — a future module must either duplicate timing values or rename them inconsistently.

**Approach:** Add a third Admin settings surface — a generic named schedule catalog (FR-30/AD-16). Admins with `schedules.manage` open "Einstellungen → Zeitpläne" and create, edit, and archive named schedules (name + interval unit/magnitude), persisted in the Admin-owned `schedules` table with reserved nullable weekday-set/time-of-day fields (cron-like, unused in V1). Archive is soft (`archived_at`) so future Tool Types/Tools referencing by FK keep history intact. Gated by `schedules.manage` (AD-6), with `schedules.manage` also added to the Admin-module outer gate so holders reach the module.

## Boundaries & Constraints

**Always:**
- **Migration 000019** `schedules.{up,down}.sql` — Admin-owned table: `id` (uuidv7), `name` (text, UNIQUE), `interval_unit` CHECK (`year|quarter|month|week|day`), `interval_magnitude` int CHECK (> 0), `weekday_set` text[] NULL (reserved), `time_of_day` time NULL (reserved), `archived_at` timestamptz NULL (soft-delete), `created_at`, `updated_at`. Seed the catalog with the canonical intervals "1 year", "1 quarter", "1 month", "2 weeks", "3 days" (addendum.md seed vocabulary). Down reverses. Append `000019` to the **admin** sqlc block only (user block stays at 000016); `just sqlc-generate`.
- **Soft archive, never hard delete:** archive sets `archived_at = now()`; list queries filter `archived_at IS NULL`; archived rows are never returned by the active surface and cannot be edited (archive is irreversible via the surface in V1 — no unarchive). No `DELETE` endpoint. FK references (added in Story 4.2) resolve through the FK, not by deleting rows.
- **Admin core** `internal/admin/core/schedules.go` (new): `SchedulesPermission = "schedules.manage"` const, interval-unit consts, audit ops (`schedule.create`/`schedule.update`/`schedule.archive`), `ErrScheduleNotFound` + invalid sentinel + German messages, domain `Schedule` + input, `SchedulesStore` port (List/Get/Create/Update/Archive), permission re-check, best-effort audit, validation (name + unit + magnitude; weekday/time leniently ignored — nullable, not validated), duplicate-name guard (case-insensitive). Add `schedulesStore` to the `Service` struct + `NewService`.
- **Ports** `internal/admin/ports/ports.go`: extend `Service` with the schedule methods; add read-only `SchedulesPort` (List active) for future Tool-module consumers (AD-16) — no copies embedded.
- **Go outer gate:** add `"schedules.manage"` to `AdminModuleAccessCodes()` in `internal/user/core/users_admin_permissions.go` (opens `/api/v1/admin` for schedules-only holders) and update the pinning test `users_admin_test.go` required slice. `schedules.manage` is already seeded (000010) + in both permission catalogs — no new migration for the permission.
- **HTTP** `internal/admin/adapters/http`: `schedules.go` (new) handlers + DTOs wired into a `ScheduleRoutes()` method on the shared Handler; mount `/api/v1/admin/settings/schedules` behind its own `RequireAnyPermission([...SchedulesPermission])` gate in `cmd/server/main.go` (mirrors SMTP/backup mounts). Endpoints: `GET` (active list), `POST` (create), `PUT /{id}` (update), `POST /{id}/archive`. Uniform envelope, German messages, 403 → no schedule data exposed.
- **SPA:** add `'schedules.manage'` to the `einstellungen` nav codes in `web/src/auth/permissions.ts`; add a third "Zeitpläne" tab to `AdminEinstellungenPage.tsx` gated by `schedules.manage` (mirrors E-Mail/Backup tabs); API client in `web/src/auth/settings.ts` (list/create/update/archive). Surface: schedule list (name + "Jährlich − 1 year" display), create/edit form (name, unit dropdown, magnitude number input), archive per row with confirm; inline German feedback; archived schedules not shown in the active list.
- **Audit (NFR-O1/O2):** create/edit/archive recorded with actor, timestamp, operation.

**Ask First:**
- None (story ACs + AD-16 resolve the design).

**Never:**
- No hard delete of schedules (archive only, FK history preserved).
- No changes to committed migrations; user sqlc block must not gain 000019.
- No weekday/time parsing or validation in V1 (reserved nullable fields only).
- No Tools-module code (tool_types/tools reference schedules in Story 4.2).
- No cache of the catalog — consumers read through `SchedulesPort` at runtime.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| GET_LIST_EMPTY | no schedules | 200 `[]` | n/a |
| GET_LIST | active + archived | 200 only active rows | n/a |
| CREATE_VALID | name + unit + magnitude | 201/200 created, audited | n/a |
| CREATE_DUPLICATE | same name (case-insensitive) | 400 German duplicate-name error | 400 |
| CREATE_INVALID | empty name / bad unit / magnitude ≤ 0 | 400 German | 400 |
| UPDATE_VALID | edit name/interval | Updated, audited; weekday/time ignored | n/a |
| UPDATE_ARCHIVED | update an archived row | 404/conflict sentinel | 404 |
| ARCHIVE | archive active schedule | `archived_at` set, row leaves active list, audited | n/a |
| ARCHIVE_ARCHIVED | archive already-archived row | Idempotent/conflict | 404 or no-op |
| FORBIDDEN | caller lacks schedules.manage | Uniform 403, no schedule data exposed (AD-6) | 403 |

</frozen-after-approval>

## Code Map

- `migrations/000019_schedules.{up,down}.sql` -- Admin-owned catalog table + canonical seed rows (AD-16).
- `sqlc.yaml` -- append `./migrations/000019_schedules.up.sql` to the admin block only; `just sqlc-generate`.
- `internal/admin/core/settings.go` -- add `SchedulesPermission` + schedule audit-op consts next to the SMTP/backup consts; extend `Service` struct + `NewService` with `schedulesStore`.
- `internal/admin/core/schedules.go` (new) -- domain, validation, permission re-check, audit, duplicate-name guard, store port (List/Get/Create/Update/Archive).
- `internal/admin/ports/ports.go` -- extend `Service` + add read-only `SchedulesPort`.
- `internal/admin/adapters/postgres/queries.sql` + `schedules_repo.go` (new) -- list-active/archive-single/get/create/update/archive; `archived_at IS NULL` filter.
- `internal/admin/adapters/http/handler.go` + `schedules.go` (new) -- `ScheduleRoutes()` + handlers/DTOs.
- `cmd/server/main.go` -- `schedulesSurface` gate (`schedules.manage`) + `router.WithMount("/api/v1/admin/settings/schedules", ...)`.
- `internal/user/core/users_admin_permissions.go` -- add `"schedules.manage"` to `AdminModuleAccessCodes()`; update `internal/user/core/users_admin_test.go` required slice.
- `web/src/auth/permissions.ts` -- add `'schedules.manage'` to `einstellungen` codes.
- `web/src/auth/settings.ts` -- `SCHEDULES_PERMISSION` const + schedule API client.
- `web/src/pages/admin/AdminEinstellungenPage.tsx` (+css) -- third "Zeitpläne" tab gated by `schedules.manage`.
- Tests: core (validation/duplicate/permission/archive/audit), postgres (CRUD + archive filter + seed rows), http (200/400/404/403, no leak), composition mount gate, web (tab gating, form, archive, 401/403).

## Tasks & Acceptance

**Execution:**
- [x] `migrations/000019_schedules.{up,down}.sql` -- table + seed -- schema
- [x] `sqlc.yaml` + `just sqlc-generate` -- admin block append -- persistence
- [x] `internal/admin/core/schedules.go` (+settings.go consts + Service) -- domain/service -- core
- [x] `internal/admin/ports/ports.go` -- Service methods + SchedulesPort -- ports
- [x] `internal/admin/adapters/postgres/schedules_repo.go` + queries -- CRUD + archive -- persistence
- [x] `internal/admin/adapters/http/schedules.go` + `ScheduleRoutes()` -- handlers/DTOs -- API
- [x] `cmd/server/main.go` -- schedules mount + gate -- composition root
- [x] `internal/user/core/users_admin_permissions.go` (+test) -- outer-gate code -- gate
- [x] `web/src/auth/permissions.ts` + `settings.ts` -- nav code + client -- SPA
- [x] `web/src/pages/admin/AdminEinstellungenPage.tsx` (+css) -- Zeitpläne tab -- SPA
- [x] Tests -- core/postgres/http/composition/web incl. I/O rows -- verification
- [x] `docs/docs/planning/architecture-spine.md` -- confirm AD-16 already covers the catalog (no change expected) -- docs

**Acceptance Criteria:**
- Given an admin with `schedules.manage`, when they open Einstellungen → Zeitpläne, then they see the schedule-catalog surface listing named schedules (name + interval, e.g. "Jährlich − 1 year", "Monatlich − 1 month") (FR-30/AD-16), with generic catalog naming (Zeitpläne).
- Given the admin creates or edits a schedule, then it persists in the Admin-owned `schedules` table (AD-11/AD-16) with the reserved weekday-set/time-of-day fields present but nullable/unused in V1.
- Given the admin archives a schedule, then it is archived (not deleted) and leaves the active list; tool types/tools referencing it by FK keep history intact (FR-30/AD-16).
- Given a caller lacks `schedules.manage`, then every schedule endpoint answers uniform 403 with no schedule data exposed (AD-6).

## Spec Change Log

- **Review patches applied (review 1, 2026-09-09):** UpdateSchedule now maps the DB UNIQUE (23505) violation to the German duplicate-name 400 (renaming to an archived schedule's name no longer yields a raw 500), with postgres + handler tests; dead `GetSchedule` method dropped from the SchedulesStore port/repo/query; `schedules.manage` label unified to "Zeitpläne verwalten" (roles.go + roles.ts); `schedules.manage` added to the `uebersicht` nav codes + permissions test full-admin fixture; schedule display now uses unit-only German descriptors with correct pluralization (e.g. "Wöchentlich − 2 Wochen"); create-audit now includes the persisted schedule id; composition gate test asserts schedules-only holders are denied the SMTP/backup surfaces; new composition test exercises POST/archive write verbs through the real mount; list query orders deterministically (created_at, name) and the seed test asserts the exact count of 5 in order; name-length validation counts runes (not bytes) aligned with the client maxLength; app-level magnitude cap documented (migration untouched).

## Design Notes

- **Soft archive is net-new:** no soft-delete precedent exists in the codebase (verified). The convention adopted is a nullable `archived_at` timestamp (not a status enum), filtering active lists with `archived_at IS NULL`, matching the AC's "archived-not-deleted with FK history preserved". No unarchive in V1.
- **Seed catalog:** the 5 canonical intervals ("1 year", "1 quarter", "1 month", "2 weeks", "3 days") are seeded by migration so the surface is non-empty on first open; admins can add more.
- **V1 lenient validation (AD-16):** a schedule must have a name and an interval (unit + magnitude); weekday_set/time_of_day are reserved nullable fields that are stored but not parsed or validated until the composite-timing enhancement.

## Verification

**Commands:**
- `just sqlc-generate` && `just migrate-up` -- expected: 000019 applies; `\d schedules`; seed rows present
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go/web tests pass, 0 lint issues
- `curl` as admin: GET /api/v1/admin/settings/schedules (200, seeds listed); POST create (200); POST /{id}/archive (200); GET no longer lists archived -- expected per matrix
- `curl` as non-admin: GET -- expected: uniform 403 with no schedule data
- `npx vitest run` in web/ -- expected: all pass incl. Zeitpläne tab tests

**Manual checks (if no CLI):**
- Einstellungen → Zeitpläne renders the seeded list; create/edit/archive give inline German feedback; archived schedules disappear from the active list; E-Mail/Backup tabs still work.