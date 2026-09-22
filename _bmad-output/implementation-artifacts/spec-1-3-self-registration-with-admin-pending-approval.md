---
title: 'Self-Registration with Admin Pending Approval'
type: 'feature'
created: '2026-09-05'
status: 'done'
review_loop_iteration: 0
baseline_commit: '53026d85eb772dfbdbf73dbe21234ae14b57adc3'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** New volunteers cannot create an account to request access to the G.E.A.R. application, and the system lacks password policy enforcement, secure credential hashing (Argon2id), and anti-enumeration protections for self-service registration.

**Approach:** Implement self-registration in the User module with Argon2id password hashing, password policy enforcement (≥10 chars, FR-2), database migration for user credentials/names, an anti-enumeration `POST /api/v1/auth/register` endpoint (FR-5/UX-DR7), and a German registration form at `/register` displaying the pending approval notice (UX-DR8).

## Boundaries & Constraints

**Always:**
- Enforce the password policy of ≥10 characters without arbitrary character complexity rules (FR-2); reject shorter passwords with a clear inline German validation error ("Das Passwort muss mindestens 10 Zeichen lang sein.").
- Store passwords as secure Argon2id hashes (AD-13, never plaintext or weakly hashed).
- Self-registered users are created with `state = 'pending_approval'` (FR-5); they cannot authenticate until approved by an administrator in Epic 2 (FR-20).
- Anti-enumeration protection (FR-5 / UX-DR7): Submitting an already-registered or invalid email returns a uniform success response ("Wenn deine E-Mail bereits registriert ist, erhältst du eine Bestätigung.") without revealing whether the account exists.
- Show the German pending approval message upon registration completion: "Dein Konto ist in Bearbeitung. Login erst möglich nach Admin-Freigabe." (UX-DR8).
- Store first-class typed columns `first_name`, `last_name`, and `password_hash` in the User-owned `users` table, while maintaining `attributes JSONB` for extensible metadata (FR-7/AD-3).
- All API error responses must adhere to the uniform JSON envelope `{"error":{"code","message","details?"}}`.
- UI must follow DESIGN.md tokens, light/dark mode pairs, accessible form controls (min touch target ≥48px), and responsive layout.

**Ask First:**
- None.

**Never:**
- No email notification dispatch yet (SMTP transactional sender is configured in Admin stories, automated notification emails are a non-goal).
- No login authentication or session creation yet (login flow ships in Story 1.4).
- No admin approval UI in this story (admin approval workflow lands in Epic 2 / Story 2.4).
- No plaintext passwords stored, logged, or returned in API responses.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| HAPPY_PATH | Valid Vorname, Nachname, new Email, valid Password (≥10 chars), matching PasswordConfirm | User created in DB with `state = 'pending_approval'`, Argon2id `password_hash`; API returns 201/200; UI displays pending approval confirmation message | n/a |
| SHORT_PASSWORD | Password < 10 characters | API returns 400 with `invalid_request` envelope; UI displays inline German error "Das Passwort muss mindestens 10 Zeichen lang sein." | Field validation error |
| PASSWORD_MISMATCH | Password and PasswordConfirm do not match | API returns 400 with `invalid_request` envelope; UI displays inline German error "Die Passwörter stimmen nicht überein." | Field validation error |
| DUPLICATE_EMAIL | Registration with an email that already exists | API returns uniform 200 anti-enumeration response; no duplicate user created; no account existence leaked | Anti-enumeration uniform confirmation |
| INVALID_EMAIL | Malformed email address (e.g. `invalid-email`) | API returns 400 with `invalid_request` envelope; UI displays inline German error "Bitte gib eine gültige E-Mail-Adresse ein." | Field validation error |
| MISSING_FIELDS | Any required field empty | API returns 400 with `invalid_request` envelope; UI marks missing fields | Field validation error |

</frozen-after-approval>

## Code Map

- `migrations/000002_user_registration.up.sql` -- Migration adding `first_name`, `last_name`, `password_hash` to `users` table.
- `migrations/000002_user_registration.down.sql` -- Reversible migration dropping the added columns.
- `internal/user/adapters/postgres/queries.sql` -- sqlc query for inserting a new user with registration details and querying by email.
- `internal/platform/crypto/password.go` -- Argon2id hashing and verification utility adhering to security standards (AD-13).
- `internal/platform/crypto/password_test.go` -- Unit tests verifying Argon2id hashing, verification, and salting.
- `internal/user/core/user.go` -- User domain model, registration input validation, password length rule (FR-2).
- `internal/user/ports/ports.go` -- Port interfaces for User repository and registration service.
- `internal/user/adapters/http/handler.go` -- HTTP handler for `POST /api/v1/auth/register` with anti-enumeration logic and uniform error envelopes.
- `internal/user/adapters/http/handler_test.go` -- Unit tests for registration handler (happy path, short password, duplicate email, mismatch).
- `internal/platform/router/router.go` -- Router mount for `/api/v1/auth/register`.
- `cmd/server/main.go` -- Composition root wiring User registration service and handler to router.
- `web/vite.config.ts` -- Proxy configuration routing `/api` to `http://localhost:8080`.
- `web/src/pages/RegisterPage.tsx` -- Registration page component with German form, validation, and confirmation view.
- `web/src/pages/RegisterPage.module.css` -- CSS module styled with DESIGN.md tokens (inputs, primary button, cards, error text).
- `web/src/pages/RegisterPage.test.tsx` -- Component tests for registration form, validation errors, submission, and confirmation.
- `web/src/App.tsx` -- Client route `/register` added to router.

## Tasks & Acceptance

**Execution:**
- [x] `migrations/000002_user_registration.{up,down}.sql` -- Create migration adding `first_name`, `last_name`, `password_hash` to `users` -- schema support for self-registration
- [x] `internal/user/adapters/postgres/queries.sql` & `sqlc.yaml` -- Add sqlc query `CreateRegisteredUser` and run `just sqlc-generate` -- type-safe database queries
- [x] `internal/platform/crypto/password.go` -- Implement Argon2id password hashing and constant-time verification -- AD-13 secure password storage
- [x] `internal/user/core/` & `internal/user/ports/` -- Define domain registration models, validation rules (≥10 chars, matching passwords, valid email), and repository ports -- business rules isolation
- [x] `internal/user/adapters/http/handler.go` -- Implement `POST /api/v1/auth/register` with anti-enumeration response and uniform JSON envelopes -- API contract
- [x] `internal/platform/router/router.go` & `cmd/server/main.go` -- Wire registration handler into chi router and composition root -- composition root wiring
- [x] `web/vite.config.ts` -- Add dev proxy `/api` -> `http://localhost:8080` -- seamless local full-stack integration
- [x] `web/src/pages/RegisterPage.tsx` -- Implement German registration UI with inline validation, pending approval message, and login/dashboard links -- volunteer registration UX
- [x] `web/src/App.tsx` & `web/src/pages/LoginPage.tsx` -- Wire `/register` route and add navigation links between login and register -- navigable auth shell
- [x] `internal/user/` & `web/src/pages/RegisterPage.test.tsx` -- Write comprehensive backend and frontend unit tests covering the I/O matrix -- automated verification

**Acceptance Criteria:**
- Given an unauthenticated visitor on `/register`, when submitting valid registration details with password ≥10 chars, then a `pending_approval` user is created with an Argon2id hash, and the UI displays "Dein Konto ist in Bearbeitung. Login erst möglich nach Admin-Freigabe."
- Given a password shorter than 10 characters or non-matching password confirmation, when submitted, then registration is rejected with an inline German validation error.
- Given an email that already exists in the database, when submitted, then the API returns a uniform 200 response without revealing whether the account exists.

## Spec Change Log

- 2026-09-05 (Story 1.3 implementation & review loop): Implemented volunteer self-registration with Argon2id hashing, database migration 000002, anti-enumeration protection, and React registration UI:
  - `migrations/000002_user_registration.{up,down}.sql`: schema extension adding `first_name`, `last_name`, `password_hash` to `users`.
  - `internal/platform/crypto/password.go`: Argon2id password hashing and constant-time verification with safety checks against empty hash digests.
  - `internal/user/core/user.go` & `service.go`: input validation (≥10 chars, max bounds, matching confirmation), `GetUserByEmail` error handling, duplicate-key race condition handling, and uniform confirmation response (`UniformSuccessMessage`).
  - `internal/user/adapters/http/handler.go`: `POST /api/v1/auth/register` with 1MB request body limit, error envelope formatting.
  - `cmd/server/main.go`: composition root wiring User service and HTTP handler into chi router.
  - `web/vite.config.ts`: `/api` dev proxy to `http://localhost:8080`.
  - `web/src/pages/RegisterPage.tsx` & `.module.css`: registration form with DESIGN.md styling, inline validation, pending approval message, title management, and credential clearing on success.
  - Verification: 23 frontend Vitest tests and all Go unit/database/integration tests passing cleanly.

## Design Notes

- **Argon2id Parameters:** Memory = 64 MB (65536 KiB), Iterations = 3, Parallelism = 4 threads, Salt = 16 random bytes, Key length = 32 bytes.
- **Anti-Enumeration Contract:** Always returns HTTP 200 with `{"message":"Wenn deine E-Mail bereits registriert ist, erhältst du eine Bestätigung.","status":"pending_approval"}` for valid payloads regardless of email existence.

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go and web tests pass, 0 lint issues
- `just db-up` -- expected: migration 000002 applies cleanly
- `just migrate-down && just migrate-up` -- expected: schema rebuilds cleanly
- `curl -X POST http://localhost:8080/api/v1/auth/register` with test payload -- expected: 200/201 uniform response

## Suggested Review Order

**Database & Cryptography**

- user table migration adding first_name, last_name, and password_hash
  [`000002_user_registration.up.sql:1`](../../migrations/000002_user_registration.up.sql#L1)

- Argon2id password hashing and constant-time verification utility
  [`password.go:58`](../../internal/platform/crypto/password.go#L58)

**User Domain & Business Logic**

- domain validation rules enforcing ≥10 char password policy and email syntax
  [`user.go:58`](../../internal/user/core/user.go#L58)

- registration workflow with anti-enumeration protection and duplicate handling
  [`service.go:44`](../../internal/user/core/service.go#L44)

**HTTP API & Server Composition**

- registration endpoint handler with 1MB body limit and error envelopes
  [`handler.go:38`](../../internal/user/adapters/http/handler.go#L38)

- composition root wiring User repository, hasher, service, and router
  [`main.go:38`](../../cmd/server/main.go#L38)

**Frontend Registration UI**

- dev proxy routing `/api` requests to the Go backend
  [`vite.config.ts:6`](../../web/vite.config.ts#L6)

- German volunteer registration form with inline validation and confirmation
  [`RegisterPage.tsx:15`](../../web/src/pages/RegisterPage.tsx#L15)

**Automated Tests & Quality Gates**

- backend service and anti-enumeration test suite
  [`service_test.go:59`](../../internal/user/core/service_test.go#L59)

- frontend registration form rendering, validation, and submission tests
  [`RegisterPage.test.tsx:23`](../../web/src/pages/RegisterPage.test.tsx#L23)
