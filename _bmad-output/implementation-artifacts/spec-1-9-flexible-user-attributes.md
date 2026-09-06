---
title: 'Flexible User Attributes'
type: 'feature'
created: '2026-09-06'
status: 'done'
review_loop_iteration: 0
baseline_commit: '0b8357af4c01508d5ae0b961449fca8b260a22d2'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Custom, non-core metadata about volunteers (e.g. local notes, internal tags) cannot be stored without a schema migration, and the user profile API does not expose any extensible attribute surface.

**Approach:** Expose the existing `users.attributes JSONB` column (created in Story 1.1) through the User module's profile API (FR-7/AD-3): the `Profile` payload and `UpdateProfileInput` gain an `attributes` map, writes persist to the JSONB column with validation, reads return it, and the promotion path (JSONB → typed column via golang-migrate + backfill) is verified.

## Boundaries & Constraints

**Always:**
- Core/known attributes remain first-class typed columns (email, names, display name, state, MFA flag, etc.); extensible custom attributes live in the single `users.attributes JSONB` column (FR-7/AD-3).
- Custom attributes are read and written through the User module's profile port only — no other module reads/writes the JSONB directly (AD-1/AD-11).
- Attribute keys are unique per semantic meaning app-wide: no two modules assign a different shape to the same key (AD-3). A key added here must not collide with a future module's key.
- Writes are validated: `attributes` must be a JSON object (map) — arrays/scalars are rejected; values are JSON-serializable; a reasonable size cap (e.g. 16 KB serialized) prevents abuse; keys are non-empty strings with a sane length cap.
- Reading an attribute value is served as valid JSON through the profile API; a malformed stored value (should not happen) is surfaced as a clear error, never a crash.
- All API errors use the uniform envelope `{"error":{"code","message","details?"}}`; profile updates remain auth-gated (self-ownership, AD-12).
- The promotion path is verified: a custom attribute promoted to a real typed column is added via a golang-migrate migration with a backfill, keeping the JSONB column for continued flexibility (AD-3/NFR-R2).

**Ask First:**
- None.

**Never:**
- No per-attribute schema validation (attributes are free-form by design, FR-7).
- No new custom attributes in core (e.g. no "shoe size" default) — the story ships the mechanism, not specific attributes.
- No dedicated attributes management UI — attributes are edited via the existing profile API/UI payload (the profile page may render them read-only or as a JSON field; keep minimal).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| PROFILE_READ_EMPTY | User with empty `attributes` JSONB | `GET /profile` returns `attributes: {}` | n/a |
| PROFILE_READ_WITH_ATTRS | User with custom attributes stored | `GET /profile` returns them as valid JSON | n/a |
| PROFILE_UPDATE_ATTRS | Authenticated user sets `attributes: {"note":"..."}` | Persisted to JSONB, 200 with updated profile incl. attributes | n/a |
| PROFILE_UPDATE_CLEAR | User sends `attributes: {}` | JSONB cleared/reset to `{}`, no orphan keys | n/a |
| ATTR_NOT_OBJECT | `attributes` is an array or scalar (e.g. `[1,2]`) | Rejected, no change | 400 invalid_request |
| ATTR_BAD_KEY | Empty or over-long attribute key | Rejected, no change | 400 invalid_request |
| ATTR_TOO_LARGE | Serialized attributes exceed the size cap | Rejected, no change | 400 invalid_request |
| ATTR_INVALID_JSON | Value not JSON-serializable (e.g. undefined in JS) | Rejected, no change | 400 invalid_request |
| UNAUTHENTICATED | No/invalid session token | 401 unauthorized | 401 unauthorized |

</frozen-after-approval>

## Code Map

- `internal/user/core/profile.go` -- Add `Attributes map[string]any` to `Profile` (JSON `attributes`); add `Attributes` to `UpdateProfileInput` + validation (object-only, key/length caps, size cap).
- `internal/user/core/user.go` -- `User.Attributes` already exists (`json:"attributes,omitempty"`).
- `internal/user/adapters/postgres/queries.sql` -- `UpdateUserProfile` gains an `attributes` param (upsert into the JSONB column, e.g. `attributes = COALESCE($5, '{}'::jsonb)`); `GetUserByEmail`/session/user SELECTs already return `attributes`. Re-run `just sqlc-generate`.
- `internal/user/adapters/postgres/repository.go` -- Map `attributes` in `UpdateUserProfile` (marshalling the map to JSONB), `profileFromUser`/row mapping already carries it.
- `internal/user/adapters/http/handler.go` -- `POST /profile` accepts and returns `attributes`; `GET /profile` returns them; validation error mapping (400).
- `internal/user/core/profile_test.go` -- Unit tests for the I/O matrix (read empty/with attrs, update, clear, non-object, bad key, too-large, unauth).
- `internal/user/adapters/postgres/repository_test.go` -- Integration test: set custom attrs, read back, clear, promote-to-column migration path verified.
- `migrations/` -- No migration needed in this story (column exists). The promotion path is demonstrated by a test fixture/note, not a shipped migration.

## Tasks & Acceptance

**Execution:**
- [x] `internal/user/core/profile.go` -- Add `Attributes` to `Profile` + `UpdateProfileInput` with object/size/key validation -- FR-7/AD-3 profile surface
- [x] `internal/user/adapters/postgres/queries.sql` -- `UpdateUserProfile` accepts and persists `attributes` JSONB + re-run `just sqlc-generate` -- type-safe JSONB writes
- [x] `internal/user/adapters/postgres/repository.go` -- Marshal/unmarshal attributes in the profile repository methods -- JSONB mapping
- [x] `internal/user/adapters/http/handler.go` -- `POST`/`GET /profile` expose `attributes` with error mapping -- API contract
- [x] `internal/user/core/profile_test.go` -- Unit tests covering the full I/O matrix -- automated verification
- [x] `internal/user/adapters/postgres/repository_test.go` -- Integration test: set/read/clear attributes + promotion-path demonstration -- persistence verification

**Acceptance Criteria:**
- Given the user profile schema, when a profile is created or updated, then all core attributes map to typed columns and custom attributes are stored in the single `attributes JSONB` column (FR-7/AD-3).
- Given I set a custom attribute on a user profile, when the profile is saved, then the attribute is persisted and retrievable with no database migration (FR-7).
- Given the profile API, when attributes are read or written, then they are served/consumed through the User module's port with valid JSON serialization and validation (AD-1/AD-3).
- Given a custom attribute that later becomes core, when it is promoted, then it is migrated to a real typed column via golang-migrate and backfilled, retaining the JSONB column (AD-3/NFR-R2).

## Spec Change Log

- 2026-09-06 (Story 1.9 implementation): exposed the existing `users.attributes JSONB` column through the profile API (FR-7/AD-3) — `Profile` and `UpdateProfileInput` gained an `attributes` map, persisted/read via the User module port with validation (object-only, trimmed keys ≤ 64 chars, JSON-serializable values, ≤ 16 KB), plus tests including a promotion-path fixture.

- **2026-09-06 — Review-loop fixes (frozen contract untouched).** The following clarifications and hardening were applied during implementation; none changes the frozen I/O & Edge-Case Matrix contract:
  - **Absent-vs-clear semantics (Design Notes updated):** an ABSENT (`nil`) `attributes` field leaves the stored JSONB **unchanged** (a name-only base-data save never wipes custom attributes — the current value is passed through); an explicit empty map `{}` **clears** them. This replaces the earlier "nil == `{}` == clear" reading. The frontend likewise only sends `attributes` when it actually loaded them.
  - **Key normalization + whitespace-only rejection:** keys are trimmed before storage (`" note "` → `"note"`); keys that are empty OR whitespace-only after trimming are rejected.
  - **Validation-error `details`:** the 400 `invalid_request` envelope now carries machine-readable `details` (`{"key","reason"}` or `{"reason":"attributes too large"}`) via a new `core.AttributeError` that unwraps to `ErrInvalidAttributes`.
  - **Malformed stored-value read path:** a stored non-object JSONB value (e.g. an array written out-of-band) now surfaces a clear error on reads instead of a silent `{}`/crash — the repository unmarshal propagates the failure.
  - **Brittle audit-order test relaxed** to a set-based presence assertion; the dead-`COALESCE` fallback is retained as a documented DB-level safety net (the repository always sends a concrete value).

## Design Notes

- **No migration in this story:** the `users.attributes JSONB` column already exists (Story 1.1, cold-start migration 000001). The promotion path is verified by test (a fixture migration + backfill) rather than a shipped migration, since no attribute is promoted in this story.
- **JSONB semantics:** absent (`nil`) attributes = **leave unchanged** (do not touch the column); an explicit empty map `{}` = **clear**. Updates REPLACE the whole attributes map (the payload is the full set), matching the additive-union-free JSONB contract. The DB-level `COALESCE($5, '{}'::jsonb)` is a safety net ensuring the column is never NULL (the repository always sends a concrete value).
- **Validation caps:** attributes must be a JSON object; keys are trimmed for storage, non-empty and ≤ 64 chars; values must be JSON-serializable; serialized size ≤ 16 KB. Invalid attributes are rejected with 400 `invalid_request` plus `details` identifying the offending key/reason.

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go and web tests pass, 0 lint issues
- `just db-up` -- expected: schema unchanged (version 8), attributes column intact
- `curl` profile flow (read empty → set attrs → read back → clear → non-object rejected) -- expected: 200/400 per I/O matrix

## Suggested Review Order

**Profile Core & Validation**

- attribute validation (object-only, trimmed keys, size cap) + detailed error
  [`profile.go:160`](../../internal/user/core/profile.go#L160)

- absent-vs-clear semantics and attributes in the Profile payload
  [`profile.go:96`](../../internal/user/core/profile.go#L96)

**Persistence & API**

- JSONB replace-wholesale persistence with safety-net COALESCE
  [`queries.sql:172`](../../internal/user/adapters/postgres/queries.sql#L172)

- attributes round-trip + malformed-stored-value error path
  [`repository.go:299`](../../internal/user/adapters/postgres/repository.go#L299)

- 400 invalid_request with machine-readable details
  [`handler.go:550`](../../internal/user/adapters/http/handler.go#L550)

**Frontend**

- attributes loaded/known gating so saves never wipe unloaded attributes
  [`ProfilePage.tsx:189`](../../web/src/pages/ProfilePage.tsx#L189)

**Automated Verification**

- attributes I/O matrix incl. round-trip and malformed-value tests
  [`profile_test.go:1`](../../internal/user/core/profile_test.go#L1)