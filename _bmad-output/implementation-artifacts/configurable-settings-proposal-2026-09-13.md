# Configurable System Settings — Proposal

Date: 2026-09-13
Status: proposal (not yet implemented)
Scope: system-wide values currently static in code that could become admin-configurable settings.

## Purpose

The Admin module already owns three operational-settings surfaces (`smtp_settings`, `backup_destinations`, `schedules` — AD-14/15/16). This proposal catalogs the values that are still hardcoded across the codebase and recommends which should become configurable system parameters under a new Admin-owned `app_settings` surface (a fourth "System" settings tab), versus which should stay fixed.

Each item below records: current value, file:line anchor, what it governs, and a recommendation (**Yes** = make configurable, **No** = leave fixed) with the reason.

---

## A. Timeouts & durations

| # | Setting | Current | File:Line | Governs | Rec | Reason |
|---|---------|---------|-----------|---------|-----|--------|
| A1 | SMTP dial timeout | 10s | `internal/admin/adapters/smtp/sender.go:29` | SMTP connection dial | **Yes** | Slow-but-legit relays false-fail today |
| A2 | SMTP protocol timeout | 30s | `internal/admin/adapters/smtp/sender.go:34` | Entire SMTP conversation | **Yes** | Biggest "send hangs" lever |
| A3 | Backup test-connection timeout | 10s / 10s | `internal/admin/adapters/backup/tester.go:43,47` | TCP dial + FTP/SSH/S3 exchange | **Yes** | Remote endpoints vary widely |
| A4 | Password-reset link TTL | 30 min | `internal/user/core/reset.go:35` | Reset link validity | **Yes** | Org requirement (15min–24h); fixes duplicated "30 Minuten" email text (`sender.go:292`) |
| A5 | Dual-admin recovery TTL | 30 min | `internal/user/core/admin_recovery.go:44` | Recovery token validity | **Yes** | Keep in lockstep with A4 |
| A6 | Forgot-password throttle interval | 60s | `internal/user/core/reset.go:42` | Forgot per-email rate gate | **Yes** | Rate policy varies by org |
| A7 | OTP TTL | 15 min | `internal/user/core/otp.go:32` | One-time password validity | **Yes** | Operational window |
| A8 | OTP length | 10 chars | `internal/user/core/otp.go:27` | OTP entropy | **Yes** | Security/UX trade-off, tied to A7 |
| A9 | MFA enrollment window | 10 min | `internal/user/core/totp.go:23` | Pending-secret TTL | **Yes** | Enrollment friction |
| A10 | Inspection auto-return delay | 2s | `web/src/pages/InspectionPage.tsx:13` | Post-submit UX timing | **No** | Low value, client-side |
| A11 | Session idle lifetime | 8h | `internal/user/core/session.go:63` (env override `GEAR_SESSION_IDLE`) | Session lifetime | **No** | Already env-configurable; keep as deployment config |
| A12 | HTTP server timeouts + shutdown | 5/15/15/60s, 10s | `cmd/server/main.go:211-219` | HTTP server | **No** | Infra-level, deployment config |

## B. Security & auth policy

| # | Setting | Current | File:Line | Governs | Rec | Reason |
|---|---------|---------|-----------|---------|-----|--------|
| B1 | Login lockout thresholds/durations | 3→4 tries, 30/60s, cap 10 | `internal/user/core/lockout.go:59-67` + duplicated in `internal/user/adapters/postgres/queries.sql:91-94` | Progressive login lockout | **Yes** | Classic admin policy; **fixes a two-place drift hazard** |
| B2 | Password min length | 10 | `internal/user/core/user.go:126`, `password.go:50` | FR-2 | **No** | Likely spec-frozen |
| B3 | Password max length | 1024 | `internal/user/core/user.go:130`, `password.go:53` | Abuse guard | **No** | Rarely touched |
| B4 | TOTP period/digits/algorithm | 30s / 6 / SHA1 | `internal/user/core/totp.go:69-71` | RFC 6238 | **No** | Must match authenticator apps |
| B5 | Argon2id cost | 64MB / 3 / 4 | `internal/platform/crypto/password.go:34-40` | Hashing cost | **No** | Changing breaks verify; security-relevant |
| B6 | Anti-enumeration strings | frozen German | `reset.go:80`, `service.go:261`, `auth.go:101` | Forgot/registration confirmations | **No** | Explicitly frozen by spec (UX-DR7) |
| B7 | Must-change-password behavior | binary | `internal/user/core/auth.go:226-259` | Forced-change fallback | **No** | Spec-driven |

## C. Policy limits (field caps)

| # | Setting | Current | File:Line | Governs | Rec | Reason |
|---|---------|---------|-----------|---------|-----|--------|
| C1 | Attribute key length / map size | 64 / 16KB | `internal/user/core/profile.go:57,59` + `internal/tools/core/attributes.go:26,28` + `web/src/components/AttributesEditor.tsx:29-30` | Custom attributes contract | **Yes** | **Fixes a 3-place drift hazard** |
| C2 | Inventory-number prefix + width | `GEAR` + 6 | `internal/tools/adapters/postgres/queries.sql:159` | Auto-assigned inventory number | **Yes** | Orgs want own prefix/length; needs migration to read from settings |
| C3 | Name caps (users, groups, roles, quals, tools, types, checklist labels) | 100–255 | various (`user.go`, `roles.go:127`, `qualifications.go:206`, `tools.go:487`, `tool_types.go:471,500`) | Entity limits | **No** | Low value; extraction cost > benefit |
| C4 | Checklist items per type | 100 | `internal/tools/core/tool_types.go:107` | Defensive bound | **No** | Fixed |

## D. Business thresholds

| # | Setting | Current | File:Line | Governs | Rec | Reason |
|---|---------|---------|-----------|---------|-----|--------|
| D1 | Inspection color threshold (Orange ≤ 14 days) | not yet built (Story 6.1) | spec: `architecture-spine.md:81`, `epic-6.md:22` | Color-coded status dashboard | **Yes** | **Build configurable from day one** — don't hardcode when 6.1 lands |
| D2 | Qualification "expiring soon" window | 30 days | `internal/user/core/qualification_status.go:30` | "Bald ablaufend" flip | **Yes** | Business-visible lead time |
| D3 | Schedule interval units | year/quarter/month/week/day | `internal/admin/core/schedules.go:16-20` + DB CHECK `000019:25` | Allowed schedule units | **No** | DB-enumerated; widening needs migration |
| D4 | Dashboard status label | static "verfügbar" in SPA | `web/src/pages/DashboardPage.tsx:172` | Status label | **No** (but move server-side with real status in 6.1) | Label, not a parameter |

## E. Design / UX tokens (explicitly out of scope)

- Brand colors, spacing scale, typography, page max-widths — `web/src/styles/tokens.css` + page CSS. Already correctly centralized as design tokens; **not** admin settings.

---

## Recommended set (Yes)

**15 settings, grouped into a new Admin-owned `app_settings` table surfaced as a fourth "System" tab under `admin.settings.*`:**

1. **A1** SMTP dial timeout
2. **A2** SMTP protocol timeout
3. **A3** Backup test-connection timeout (dial + protocol)
4. **A4** Password-reset link TTL
5. **A5** Dual-admin recovery TTL
6. **A6** Forgot-password throttle interval
7. **A7** OTP TTL
8. **A8** OTP length
9. **A9** MFA enrollment window
10. **B1** Login lockout thresholds + durations
11. **C1** Attribute key length + map size
12. **C2** Inventory-number prefix + width
13. **D1** Inspection color threshold (Orange window)
14. **D2** Qualification "expiring soon" window

**Tied into the same recommendation:**

- **C2** and **D1** should be designed configurable *at build time*:
  - C2 needs a migration now (the `'GEAR' || lpad(nextval(...),6,'0')` SQL literal must read the prefix/width from a settings row).
  - D1 is spec-only until Story 6.1; when 6.1 implements the status dashboard, it reads the threshold from `app_settings` instead of hardcoding 14.

**Explicitly NOT recommended (No):** A10–A12 (UX/infra), B2–B7 (spec-frozen or security-critical), C3–C4 (low-value caps), D3 (DB-enumerated), D4 (label), and all design tokens (Section E).

## Notes / rationale

- **The strongest argument for the Yes set is drift hazard:** B1 (lockout) is duplicated between `lockout.go` and `queries.sql`; C1 (attributes) is triplicated across user core, tools core, and the SPA; C2 (inventory) is split between Go and a DB CHECK. Centralizing these into one settings row removes the two/three-place change traps.
- **Precedent exists:** session idle (`GEAR_SESSION_IDLE`) and app origin (`GEAR_APP_ORIGIN`) are already env-configurable; moving the recommended set into the DB-backed admin surface unifies the config story.
- **Anti-enumeration strings and password policy are deliberately frozen** (FR-2 / UX-DR7); they are excluded to avoid silently weakening spec guarantees.