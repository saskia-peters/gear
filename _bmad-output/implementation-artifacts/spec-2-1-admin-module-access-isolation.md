---
title: 'Admin Module Access Isolation'
type: 'feature'
created: '2026-09-06'
status: 'done'
review_loop_iteration: 0
baseline_commit: '2e9965713b3a050765843629accb1254aa77f1b6'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The admin module is currently only client-hidden (the sidebar's ADMIN entry is gated by `is_admin`) but not server-isolated: `/admin` and the admin surfaces are reachable by any authenticated user, and there is no single admin-route gateway enforcing FR-19 (403 + hidden existence) or the AD-6 permission model server-side.

**Approach:** Build the admin access-isolation gateway (FR-19/AD-6): a server-side admin route group gated by an admin-role check that returns HTTP 403 (with hidden existence / anti-enumeration) for any non-admin, structured-logged (NFR-O1); mount the existing admin surfaces (`/api/v1/admin/...`) behind it. On the client, gate the ADMIN module routes (not just the sidebar link) so a force-navigating non-admin sees a "Zugriff verweigert" state and is redirected to the Dashboard (UX-DR6), with the admin module's existence never hinted (FR-19).

## Boundaries & Constraints

**Always:**
- Every admin route returns HTTP 403 for a non-admin authenticated user, with NO admin data exposed (FR-19, server-side only, AD-6).
- Admin-module **existence is hidden** from non-admins: the 403 response is the uniform envelope and carries no hint that an admin module exists (anti-enumeration of admin existence, FR-19).
- An unauthenticated request to an admin route is not granted content and gives no admin-existence indication (FR-19).
- Each admin action maps to exactly one permission code and requires it (AD-6/AD-12) — enforced server-side by the gateway resolving the caller's live permission set.
- The gateway re-resolves the permission set per request (no cache) so revocation is immediate (AD-2/AD-6/FR-21/FR-22).
- Any 403 trigger is emitted to structured auth logging (NFR-O1).
- Client: the ADMIN module is hidden for non-admins (no links/menu/route hints); if a non-admin force-navigates to an admin path, the SPA shows a "Zugriff verweigert" state (or a clean redirect to the Dashboard) and renders no admin surfaces (UX-DR6/UX-DR8).
- The existing admin-recovery surface (`/api/v1/auth/admin/recovery`, gated by `admin.recovery.approve`) is kept; it becomes one member of the isolated admin surface.

**Ask First:**
- None.

**Never:**
- No admin data/functionality exposed to non-admins under any code path.
- No client-trusted authorization — the server is authoritative; the client only hides UI.
- No deny permissions (AD-12 additive model only).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| ADMIN_ACCESS_ADMIN | Admin (all 21 codes / admin role) requests an admin route | 200, admin surface served | n/a |
| ADMIN_ACCESS_NONADMIN | Authenticated non-admin requests an admin route | HTTP 403 with uniform envelope, no admin data, structured log | 403 forbidden |
| ADMIN_ACCESS_UNAUTH | Unauthenticated requests an admin route | Not granted content, no admin-existence hint | 401 unauthorized |
| ADMIN_HIDDEN_EXISTENCE | Non-admin requests any admin route | 403 body identical to a generic forbidden — no admin-module hint | 403 forbidden |
| ADMIN_PERMISSION_REVOKED | Admin revoked from the admin role, requests an admin route | 403 immediately (live re-resolution, no cache) | 403 forbidden |
| CLIENT_NONADMIN_NAV | Non-admin force-navigates to /admin | SPA shows "Zugriff verweigert" / redirects to Dashboard; no admin UI | n/a |
| CLIENT_ADMIN_NAV | Admin navigates to /admin | Admin module renders | n/a |

</frozen-after-approval>

## Code Map

- `internal/platform/auth/gateway.go` -- Add `RequireAdminRole` (or a general `RequirePermission` reuse) resolving the caller's live permission set and requiring an admin-only permission; ensure 403 + hidden-existence envelope.
- `internal/user/adapters/postgres/queries.sql` -- Ensure the permission-resolution query (`ListPermissionsByUser`) is reused; the `admin` role already grants the 21 codes. Verify no new query needed.
- `internal/platform/router/router.go` / `cmd/server/main.go` -- Mount an isolated admin route group `/api/v1/admin/...` behind the admin gateway (e.g. a placeholder admin root endpoint returning a 200 for admins).
- `internal/user/adapters/http/handler.go` -- Optionally add a minimal admin root handler proving the isolation (list of admin permissions or a health-style admin status), gated server-side.
- `web/src/App.tsx` -- Add an admin-route guard: wrap `/admin` (and future admin routes) so a non-admin force-navigating sees "Zugriff verweigert" / redirect to Dashboard (UX-DR6); hide admin module existence for non-admins.
- `web/src/pages/AdminPage.tsx` -- The existing stub becomes the gated admin home (or an explicit "Zugriff verweigert" branch).
- `web/src/components/Sidebar.tsx` -- ADMIN entry already gated by `is_admin` (Story 1.8) — verify no admin hints leak for non-admins.
- Tests: `internal/platform/auth/*_test.go` (gateway 403/200/401 + hidden-existence + live re-resolution), `web/src/App.test.tsx` + `Sidebar.test.tsx` (client nav guard, no admin hints for non-admin).

## Tasks & Acceptance

**Execution:**
- [x] `internal/platform/auth/gateway.go` -- Admin-route gateway enforcing 403 + hidden existence via live permission resolution (FR-19/AD-6) + structured log (NFR-O1)
- [x] `internal/platform/router/router.go` & `cmd/server/main.go` -- Mount an isolated `/api/v1/admin` group behind the admin gateway with a minimal admin root handler -- server-side isolation
- [x] `internal/platform/auth/*_test.go` -- Gateway tests: admin 200, non-admin 403 (hidden existence), unauth 401, permission-revoked 403 -- I/O matrix verification
- [x] `web/src/App.tsx` -- Admin-route guard: non-admin force-navigation → "Zugriff verweigert"/redirect to Dashboard; no admin hints (UX-DR6/FR-19)
- [x] `web/src/pages/AdminPage.tsx` -- Gated admin home / Zugriff-verweigert branch
- [x] `web/src/components/Sidebar.tsx` -- Verify ADMIN entry hidden for non-admins (already `is_admin`-gated) -- no admin hints
- [x] `web/src/**/*.test.tsx` -- Client tests: non-admin sees no admin hints + Zugriff-verweigert; admin sees the module

**Acceptance Criteria:**
- Given a non-admin authenticated user, when they request any admin route, then the server returns HTTP 403 and no admin data is exposed (FR-19, server-side only, AD-6).
- Given a non-admin (or unauthenticated) user in the SPA, when the client renders, then the admin module's existence is hidden (no links/menu/route hints), and force-navigation to an admin path redirects to the Dashboard (FR-19/AD-6/UX-DR6).
- Given the permission model, when admin actions are attempted, then each maps to exactly one permission code and requires it, enforced server-side (AD-6).
- Given an unauthorized access attempt, when the 403 triggers, then it is structured-logged (NFR-O1) and the UI shows "Zugriff verweigert" with no admin surfaces rendered (UX-DR6/UX-DR8).

## Spec Change Log

- 2026-09-06 (Story 2.1 implementation): built FR-19/AD-6 admin-module access isolation — a server-side admin route group `/api/v1/admin` gated by an admin-only permission (`admin.recovery.approve`) via the `RequirePermission` gateway with live permission re-resolution, returning 403 with hidden existence for non-admins and 401 for unauthenticated. A minimal admin root handler proves the isolation. Client: `RequireAdmin` route guard + hidden sidebar ADMIN entry (Story 1.8), with the recovery surface relocated under the isolated group.

- **2026-09-06 — review findings applied (frozen contract untouched).** Two review-driven clarifications, both within the frozen boundaries (the recovery surface "becomes one member of the isolated admin surface"; the client "only hides UI" / server is authoritative):
  - **Admin-recovery relocation:** the FR-27 recovery surface moved from `/api/v1/auth/admin/recovery` into the isolated admin group at `/api/v1/admin/recovery` (request/approve/deny/pending), so the admin module lives in ONE gated URL space. `handler.Routes()` no longer mounts the recovery routes under `/api/v1/auth`.
  - **Server-authoritative client guard:** the SPA `RequireAdmin` guard resolves `is_admin` from `GET /api/v1/auth/profile` on mount (never the forgeable cached flag); the `AdminPage` "Zugriff verweigert" branch was removed (the guard redirects non-admins to the Dashboard, so no admin-existence hint is rendered). A 403 from any `/api/v1/admin` API call clears the cached admin flag and redirects to the Dashboard.
  - **NFR-O1 denial log:** the admin gateway (`RequireAdminPermission`) emits a denial-specific structured log line (`admin access denied`, email, path, `permission_required`) distinct from the router-level request log.
  - **I/O-matrix evidence:** added composed tests on the REAL `AdminRoutes()` (handler-level + real postgres wiring), including nested-sub-path hidden existence, byte-identity with a generic forbidden route, admin-route-gate 200 for a genuine admin, and immediate 403 on permission revocation (AD-2 live re-resolution).

## Design Notes

- **Reuse the gateway:** the existing `RequirePermission` (Story 1.4/1.10) already validates the bearer token, resolves the live permission set, and returns 401/403 with the uniform envelope. Story 2.1 adds an admin route group gated by an admin-only permission and a minimal admin root handler, proving the isolation end-to-end.
- **Admin role = all 21 codes:** the `admin` base role already resolves to all base permission codes (AD-12); gating on any admin-only code (or a dedicated admin-module check) isolates the module.
- **Client guard:** the sidebar already hides ADMIN via `is_admin`; the missing piece is the route guard for force-navigation, plus the "Zugriff verweigert" surface.

## Verification

**Commands:**
- `just build` && `just vet` && `just test` && `just lint` -- expected: all Go and web tests pass, 0 lint issues
- `curl` admin-route matrix (admin 200, non-admin 403, unauth 401) -- expected: 200/403/401 per I/O matrix
- `npm --prefix web run test` -- expected: client isolation tests pass

## Suggested Review Order

**Server-Side Admin Isolation**

- isolated admin group (status root + recovery) in one gated URL space
  [`admin.go:19`](../../internal/user/adapters/http/admin.go#L19)

- admin-only gateway with hidden-existence 403 + denial log (NFR-O1)
  [`gateway.go:57`](../../internal/platform/auth/gateway.go#L57)

- composition-root mount of /api/v1/admin behind the admin gate
  [`main.go:107`](../../cmd/server/main.go#L107)

**Client Admin Guard**

- server-authoritative RequireAdmin (resolves is_admin from profile, not cached flag)
  [`App.tsx:120`](../../web/src/App.tsx#L120)

- 403 downgrade (clears cached admin flag + redirects)
  [`authState.ts:98`](../../web/src/auth/authState.ts#L98)

- non-admin sees no ADMIN link/hint
  [`Sidebar.test.tsx:21`](../../web/src/components/Sidebar.test.tsx#L21)

**Automated Verification**

- composed tests on the REAL AdminRoutes (200/403/401 + byte-identity + revocation)
  [`admin_test.go:1`](../../internal/user/adapters/http/admin_test.go#L1)