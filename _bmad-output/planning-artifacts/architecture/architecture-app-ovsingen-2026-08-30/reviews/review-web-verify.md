# Web-Verification Review — Architecture Spine (G.E.A.R. V1)

**Reviewer role:** skeptical architecture reviewer — verify committed decisions were reality-checked, not asserted from training data.
**Reviewed artifact:** `ARCHITECTURE-SPINE.md` (created 2026-08-30), focus on **Stack (Seed)** table (lines 100–117), the GO-2026-4316 security note (line 117), Stack-relevant ADs (AD-1, AD-3), and the "Modules" consistency convention (line 98).
**Review date:** 2026-08-30
**Method:** every named version/claim below was checked against live web sources (release feeds, advisory databases, versioning policies). Web search only — no training-data assumptions.

---

## Verdict: VERIFIED — with 1 high + 3 moderate/low factual corrections

3 of the spine's version claims are stale or factually wrong (Go 1.24 "current stable" is false and the compiler line is EOL; Vite "latest stable (6.x)" is two majors old; TypeScript "5.x" is two majors old). The two explicit "web-verified at authoring" claims for chinese-pinned deps (chi, sqlc, golang-migrate, pgx) check out. The GO-2026-4316 note reaches the right pin but for the wrong stated reason.

---

## Per-technology verification

### 1. go-chi/chi v5.3.2 — VERIFIED (current; 10 days old at authoring)
- v5.3.2 published **2026-08-20** (github.com/go-chi/chi/releases/tag/v5.3.2; API publish timestamp 2026-08-20T10:04:47Z). Latest of the v5.2.x/v5.3.x line; exact tag the spine pins. Good.
- **Sub-claim refuted:** spine line 106 says "chi v5.3.2 supports Go 1.20+". v5.3.2's `go.mod` declares **`go 1.23`** with the comment "Chi supports the four most recent major versions of Go" (raw.githubusercontent.com/go-chi/chi/v5.3.2/go.mod; pkg.go.dev lists v5.3.2 "Go: 1.23"). Go 1.20 is also long EOL. The parenthetical should be deleted or corrected to "min go 1.23" — and note v5.3.2's own minimum is *already* satisfied by any current Go (1.26/1.27).

### 2. GO-2026-4316 scope claim — REFUTED (reasoning wrong; conclusion happens to be right)
- Advisory: **GO-2026-4316** = **GHSA-mqqf-5wvp-8fh8** = **CVE-2025-69725** (pkg.go.dev/vuln/GO-2026-4316; osv.dev/vulnerability/GO-2026-4316), published 2026-01-23.
- Spine line 117 writes: *"GO-2026-4316 (open redirect in `RedirectSlashes`) is scoped to deprecated chi v2, not v5.3.x."* **False.**
- The advisory explicitly lists `github.com/go-chi/chi/v5` among affected packages, in the OSV affected range **`introduced: 5.2.2`, `fixed: 5.2.4`** with affected symbol `chi/v5/middleware.RedirectSlashes`. GitHub advisory: "Affected versions `>=5.2.2`, `[patched] 5.2.4`". The v1 and v2 module paths are *also* listed ("and 3 more"), but v5 is squarely in scope for `5.2.2`–`5.2.3`.
- **Bottom line:** the spine's *mitigation* is correct — pinning v5.3.2 is above the fixed-in 5.2.4, so the pinned version is NOT vulnerable (CVSS 4.7 MODERATE, CWE-601). But the stated *scope* is factually wrong and would mislead a future audit into believing v5 was never affected. Rewrite the note: "affects chi v5 versions 5.2.2–5.2.3 (fixed in 5.2.4, Jan 2026); our pin v5.3.2 is unaffected; keep CI dependency scan per NFR-S5."

### 3. sqlc v1.31.1 — VERIFIED (current)
- v1.31.1 released **2026-04-22** (docs.sqlc.dev/en/stable/reference/changelog.html; github.com/sqlc-dev/sqlc/releases). Still the latest release as of 2026-08 per repology.org/project/sqlc/versions (nixpkgs 26.05 pinned at 1.31.1). Fits stated purpose: pgx/v5 codegen backend is built in (changelog 1.31.0: "Map xid8 to pgtype.Uint64 for pgx/v5", "Remove github.com/jackc/pgx/v4 dependency"); Postgres JSONB columns are emitted as typed `[]byte`/overridable types, compatible with the "typed helpers" convention (line 97). Minor: sqlc 1.31.x is built with a Go 1.26 toolchain — a Go 1.24 machine would auto-fetch a newer toolchain for `go install`; non-blocking.

### 4. jackc/pgx v5 — VERIFIED (current major line)
- `v5` is "the latest stable major version" per pkg.go.dev/github.com/jackc/pgx/v5; latest release **v5.10.0 (2026-06-03)** (CHANGELOG.md). "v5" in the spine is accurate; recommend pinning to a concrete minor at build time.
- Note for the Go bump (finding A): the pgx README states **"pgx supports Go 1.25 and higher"** and PostgreSQL 14+, so latest pgx does *not* support Go 1.24.

### 5. golang-migrate v4.19.1 — VERIFIED (latest tagged release)
- v4.19.1 released **2025-11-29** (github.com/golang-migrate/migrate releases; pkg.go.dev). Nothing newer *tagged* as of 2026-08 (only `v4.19.2-0.…` pseudo-versions on master). Fits AD-3's migration path. Its `go.mod` declares `go 1.24.0` — this is the *only* component that made Go 1.24 plausible, and even it runs fine on Go ≥1.24.

### 6. PostgreSQL 16 "LTS" — PARTIALLY valid (misnomer; still supported)
- PostgreSQL uses a **5-year major-version support policy**, not an "LTS" designation (postgresql.org/support/versioning). PG 16: first release 2023-09-14, supported **until 2028-11-09**, current minor **16.15** (2026-08-10) (endoflife.date/postgresql).
- Choice is defensible for a stability-first self-hosted Ortsverband deployment and pgx still supports it (PG 14+). But: (a) "LTS" is not a PostgreSQL term; (b) 16 is two majors behind current (18, released 2025-09-25). Greenfield would normally seed 17/18. Recommend: relabel the row ("PostgreSQL 16 — supported through Nov 2028") and record a deliberate "boring/stable" rationale, or bump to 17/18.

### 7. React 19 — VERIFIED (current major)
- react.dev/versions: latest version **19.2**; current patch **19.2.8**, released 2026-07-21 (github.com/react/react releases). No React 20 announced as of 2026-08 (Scrimba roundup, 2026-07-25). Row just says "19" — accurate.

### 8. Vite "latest stable (6.x)" — STALE (two majors behind)
- **Vite 7.0 released 2025-06-24** (vite.dev/blog/announcing-vite7). By 2026-08 the npm latest tag is **v8.2.2** (npmjs.com/package/vite, updated 2026-08-20). The supported-versions policy (vite.dev/releases) says current minor is **8.2**, with backports to **7.3** and 6.4 — i.e. **6.x is end-of-support except for security patches on 6.4**, and is not "latest stable".
- Frame right: the spine's "latest stable (6.x)" was clearly cargo-culted from a 2025-era note rather than re-verified (memlog line 15 only recorded beating the Go-side pins). Fix: "Vite 8.x (current stable line; 7.x/6.4 maintain/security-only)".

### 9. TypeScript "5.x" — STALE (two majors behind)
- TypeScript **6.0 released 2026-03-23** (devblogs.microsoft.com/typescript/announcing-typescript-6-0/); **7.0.2 (native/Go port) released 2026-07-08** and is the current stable (github.com/microsoft/typescript releases; npm `typescript` latest). 5.9.3 (2025-09-30) was the last 5.x line. "5.x" is ~a year old.
- Low practical risk — TS 5.x still works fine with Vite/React — but as a *seed column* it's stale. Recommend 6.x (the documented "bridge" release, API-compatible with 5.9) or 7.x, at frontend-epic time. Vite's own tooling supports both.

### 10. Go "1.24 (current stable line)" — FALSE; 1.24 is EOL (highest-severity finding)
- As of authoring date 2026-08-30: **Go 1.27.0 released 2026-08-19** (go.dev/blog/go1.27) and is the current stable line; **Go 1.26.x** (1.26.7, 2026-08-19) is the other supported line. Per endoflife.date/go and Go's two-major-releases policy: **Go 1.24 support ended 2026-02-10** (final minor 1.24.13, 2026-02-04); 1.25 ended 2026-08-19.
- So 1.24 is neither "current" nor supported — it has received **no security fixes for ~7 months** as of authoring, and the spine explicitly builds NFR-S5 (security scans in CI) on top of it. Keep-pinning 1.24 also locks the build below the current `sqlc`/`pgx` toolchains (sqlc 1.31 built with Go 1.26; pgx 5.10 needs Go 1.25+).
- Fix: seed Go **1.26 or 1.27** (all pinned Go deps are satisfied: chi needs ≥1.23, golang-migrate ≥1.24, pgx ≥1.25).

### 11. Docker / docker-compose "latest stable" — ACCEPTABLE
- Generic-but-true; no numeric pin claimed. Multi-service compose is a fine unverifiable-in-advance placeholder. No action.

### 12. Paradigm fit — chi subrouter middleware supports the modular-hex-per-module design (VERIFIED)
- chi's `Router` interface provides `Group`, `Route`, `Mount`, `With` — each creating a **fresh middleware stack per subrouter** while parent middleware (logger, recoverer, CORS) still applies (pkg.go.dev/github.com/go-chi/chi; go-chi.io; docs routing.md). Explicit per-router `Use()` (e.g. per-module auth gating) and inline `With()` are first-class. This is exactly the per-module gateway composition the spine needs: composition root `cmd/server` mounts each module's HTTP-adapter router under its own prefix (`/api/users`, `/api/tools`, `/api/admin`) with the AD-2/AD-6 auth-port middleware applied per module — no cross-module imports required. README even markets the library as "designed for modular/composable APIs". Caveat worth recording: subrouter middleware *adds* to parent handlers; module isolation therefore rests on the discipline in AD-1 (ports only) and the composition root, not on the router — which the spine already states (line 200: deferred to epic).
- sqlc+pgx+JSONB and chi for typed-SQL purpose: fit confirmed (items 3–4); AD-3's first-class-columns-plus-JSONB is well served by pgx `jsonb` handling and typed `[]byte`/pgtype columns. UUID v7, unstructured conventions: unchanged.

---

## Findings summary (tagged)

| # | Severity | Finding | Evidence | Tag |
|---|---|---|---|---|
| A | **High** | Stack row "Go 1.24 (current stable line)" is false and the line is EOL — no security fixes since 2026-02-10; current is 1.27 (2026-08-19). Also blocks latest sqlc/pgx toolchains. | go.dev/blog/go1.27; go.dev/doc/devel/release; endoflife.date/go; pgx README ("supports Go 1.25+") | **autofix** — seed Go 1.26/1.27 before any Go code exists |
| B | **Medium** | "Vite \| latest stable (6.x)" — current stable is 8.2.x; 6.x ships only security backports on 6.4. Claim is from 2025-era data, not re-verified at authoring. | vite.dev/releases; npmjs.com/package/vite (8.2.2); vite.dev/blog/announcing-vite7 | **autofix** — bump seed to Vite 8.x (or 7.x) in the Stack table |
| C | **Medium-Low** | "TypeScript 5.x" — two majors behind (6.0.3 stable 2026-04, 7.0.2 stable 2026-07). Low practical risk for the SPA; still a stale seed pin. | devblogs.microsoft.com/typescript/announcing-typescript-6-0/; microsoft/typescript releases | **discuss** at frontend epic: 6.x (safe bridge) vs 7.x (native) |
| D | **Medium-Low** | GO-2026-4316 note: "scoped to deprecated chi v2, not v5.3.x" is wrong — the advisory covers `chi/v5` 5.2.2–5.2.3 (fixed 5.2.4) as well as v1/v2. Pinning v5.3.2 (above fix) is the right mitigation; the stated rationale is not. | pkg.go.dev/vuln/GO-2026-4316; osv.dev/vulnerability/GO-2026-4316; GHSA-mqqf-5wvp-8fh8 | **autofix** — rewrite security note with correct scope |
| E | **Low** | "chi v5.3.2 supports Go 1.20+" — false; go.mod requires go 1.23 ("four most recent majors"). Harmless (any current Go satisfies), but it's an unverified claim in the spine. | raw.githubusercontent.com/go-chi/chi/v5.3.2/go.mod | **autofix** — drop/correct parenthetical |
| F | **Low** | "PostgreSQL 16 LTS" — PostgreSQL has no LTS designation; 16 is supported to 2028-11-09 and is two majors behind current (18). Version choice itself is defensible for a self-hosted stable app. | postgresql.org/support/versioning; endoflife.date/postgresql | **discuss** — relabel or justify, don't silently keep "LTS" |

All other named Stack items verified current at 2026-08-30: chi v5.3.2 (2026-08-20), sqlc v1.31.1 (2026-04-22), golang-migrate v4.19.1 (2025-11-29), pgx v5 (latest 5.10.0), React 19 (19.2.8), Docker/compose. AD-1/AD-3 + "Modules" convention technology fit confirmed.
