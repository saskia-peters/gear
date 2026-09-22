# GOOD-SPINE RUBRIC WALKER — Review

- **Review type:** full-rubric assessment of architecture spine against its source PRD
- **Spine reviewed:** `_bmad-output/planning-artifacts/architecture/architecture-app-ovsingen-2026-08-30/ARCHITECTURE-SPINE.md`
- **Source spec:** `_bmad-output/planning-artifacts/prds/prd-app-ovsingen-2026-08-29/prd.md` + `addendum.md`
- **Date:** 2026-08-30
- **Verdict:** **CONCERNS**

---

## How this was judged

The spine is a *feature-altitude* substrate whose job is to keep Epics 1–3 (and their NFRs) from diverging. It fixes five high-value divergence points correctly (AD-1 module isolation, AD-2 single auth/identity owner, AD-3 JSONB-vs-columns, AD-4 derived tool status, AD-5 single due-date rule). The remaining findings are concentrated where the spec forces *cross-module coupling* and the spine's own "never reach into storage" law has no answer for it, plus a partially-silent operational envelope and a stack section whose "verified current" claim does not survive basic checking.

For each rubric item: a verdict, the evidence, and cross-references to the numbered findings (C1, H1…, M1…).

---

## Rubric Item 1 — Real divergence points fixed; none missed

**Verdict: PARTIAL — 5 of the 7 load-bearing divergence points are fixed; two real ones are missed.**

The spine correctly identifies and pins the subjectively highest-risk divergences at epic altitude:

| Divergence point | Pinned by | Correct? |
|---|---|---|
| Module boundary shape / how modules talk | AD-1 | yes — ports, no cross-imports, composition-root-only wiring |
| Who owns users/groups/permissions/qualifications | AD-2 | yes — single owner, all others consume a port |
| Store new attributes where | AD-3 | yes — first-class columns + exactly one JSONB column; promotion path via golang-migrate |
| Tool status value (stored vs derived) | AD-4 | yes — pure read-time derivation from inspection records |
| Next-due date: rule, override, reinstatement reset, color thresholds | AD-5 | yes — one shared function, formula and 14-day thresholds stated |
| Server-side authz truth | AD-6 | yes |
| Qualification gating semantics | AD-7 | yes — reads via auth port, no UI affordance substitute |

**Missed divergence points** (the ones that bite precisely because the spec *forces* cross-module coupling):

1. **FR-24 erasure/anonymization is a cross-module data mutation the spine never governs** (→ finding **C1**). The spine erects "no module reaches into another's storage" (AD-1) as law, yet the PRD mandates that deleting a user (User-module data) keeps and anonymizes inspection history (Tools-module data) — `"Deleted User"` string, both modules affected — and produces an immutable audit trail (NFR-O2). No port, event, delete-outbox, or denormalization decision exists anywhere in the spine, and this is NOT listed in Deferred. Epics 1 and 3 will each build an incompatible mechanism (cascade-hard-delete vs. referential-integrity collapse vs. in-place anonymize), and Epic 2's history display will be torn between them.

2. **FR-23 (Admin configures Tool Types/Tools/checklists) has no write-path owner** (→ finding **H1**). The data being configured lives in the tools module under AD-5 (FR-8/FR-9); the admin module must edit it (FR-23). No AD states whether Admin writes *through a tools-side port* or keeps its *own* copy/repository. That is exactly the "two independent implementations, two sources of truth" failure class AD-2 exists to prevent, just moved one module over.

3. **Cross-module data references in a single shared DB are ungoverned** (→ finding **H2**). The ERD itself draws `USERS ||--o{ INSPECTIONS` (tools-module rows referencing user-owned users) and silently *omits* the required `TOOL_TYPES → QUALIFICATIONS` reference from FR-8. With "never reach into storage" as law, nobody has decided how cross-module entities reference each other, whether modules own separate schemas, or how a single golang-migrate migration set avoids table-name collisions. This is the mechanical consequence of the two module-joins the spec cannot avoid.

4. **FR-9 CSV import semantics** ("clear per-row error report without partial silent failures") — all-or-nothing transaction vs. partial-import-with-report is unprepinned and undeferred. Low severity; the PRD-consequence wording tolerates either, but it is a real testable divergence (→ M3).

Not a finding: FR-12/23 checklist snapshot semantics ("removed items don't alter historical records") are *implied* satisfied at data-shape level by the separate `INSPECTION_ITEMS` entity in the ERD.

---

## Rubric Item 2 — Every AD's Rule enforceable and prevents its stated divergence

**Verdict: PASS with two paper cuts.**

- **AD-1** — enforceable: import discipline (compiler-enforceable via import analysis), single wiring point (`cmd/server`). Prevents its stated divergence.
- **AD-2** — enforceable: named port (`Service`) exposing identity + *resolved* permission set bounds all implementations. Consistent with AD-6/AD-7.
- **AD-3** — enforceable: "exactly one `attributes` JSONB column per entity" is checkable in migration review; promotion path is concrete.
- **AD-4** — enforceable, but the Rule text contains a typo: **"Complaint with equal force against every read path"** should read "Computed"/"Applied" (line 66). Cosmetic but it garbles the actual enforcement clause. (→ M3)
- **AD-5** — enforceable and precise: formula, override precedence, reinstatement reset, and Red/Orange/Green thresholds all stated; correctly consistent with FR-15/FR-16 (Out-of-Service → displayed Red unconditionally, past-due → Red).
- **AD-6** — enforceable in intent, but the *mechanism* is ambiguous, which weakens enforcement: AD-2 says FR-19 is enforced "in the composition-root gateway", AD-6 says "every request-bearing capability … re-validates against a server-side policy derived from AD-2", and the deployment diagram draws a central `GW` box in front of *all three* hexagons. Is there one gateway choke-point all traffic funnels through, or per-capability middleware calling the port? The two phrasings differ, and Epic 1 vs. Epics 2/3 can build different shapes (central gateway vs. ad-hoc middleware). (→ M3, H2-adjacent)
- **AD-7** — enforceable; correctly defers instant-effect semantics to the auth port check (FR-22).

Minor: AD-1 binds **NFR-M2 (test coverage)** — a modularity AD claiming a test-coverage binding is misdirected; NFR-M2 is really owned by the CI gates in Deferred. (→ M3) AD-2 lists FR-20 twice (in both Epic-1 and Epic-3 binding strings). (→ M3)

---

## Rubric Item 3 — Deferred section honesty

**Verdict: PASS — the Deferred list is honest; nothing load-bearing is hidden there.**

Each entry was checked for whether a lower level could diverge on it:

- Frontend internals (router/state/library/styling) — genuinely orthogonal to every invariant; safe to defer. ✓
- Per-route middleware composition — the *invariant* (server-side gate, existence-hiding, 403) is already set by AD-6; only mechanical composition is deferred. ✓
- Migration rollback mechanics — NFR-R2 already fixes forward-migrations-as-contract and *requires* documented rollback; deferring the per-migration procedure is fine. ✓
- Backup destination — NFR-R3 fixes "≥1 configurable destination + tested restore"; only the concrete target is deferred. ✓
- CI/CD provider + workflow files — gates (M3 lint, M2 tests, dependency audit) are already fixed. ✓
- TOTP library + email-send provider (FR-4/FR-5) — implementation choice only; the flow invariants live in the user hexagon. ✓

Notable corollary: precisely because Deferred is honest, the items that are *neither decided nor deferred* stand out — C1 (erasure/anonymization) and H1 (config write-path) are not hidden in Deferred; they are simply absent from the document.

---

## Rubric Item 4 — Greenfield: named tech verified-current, stack internally consistent

**Verdict: FAIL on "verified-current" — 3 of 10 rows are factually stale at the stated authoring date (2026-08-30). Verified 2026-08-30.**

| Stack row | Claimed | Verified current (2026-08-30) | Verdict |
|---|---|---|---|
| Go | `1.24 (current stable line)` | **1.27.0 shipped 2026-08-19**, 11 days before authoring; 1.24 is a ~1.5-yr-old line | ❌ claim stale |
| go-chi/chi | `v5.3.2`, "supports Go 1.20+" | v5.3.2 current (2026-08-20) but **min Go is 1.23** since v5.3.0 | ❌ rationale wrong |
| sqlc | `v1.31.1` | current (2026-04-22) | ✓ |
| jackc/pgx | `v5` | current major | ✓ |
| golang-migrate | `v4.19.1` | current | ✓ |
| PostgreSQL | `16 LTS` | 16 supported (EOL 2028-11) but PG has no "LTS" brand; current major line is 18 | ~ (choice fine, label off) |
| TypeScript | `5.x` | current | ✓ |
| React | `19` | current | ✓ |
| Vite | `latest stable (6.x)` | current stable is **8.2** (2026-08-20); 6.x is security-backport-only (6.4) | ❌ claim stale |
| Docker/-compose | latest stable (multi-service) | — | ~ (intentionally unpinned) |

Impact is bounded (it is a seed and the file says "code owns these once they exist"), so this is not load-bearing; but the header expressly claims "verified current at authoring", which the Go / Vite / chi-rationale rows now falsify. Note the internal-consistency angle: Go 1.24 does satisfy chi v5.3.2's real minimum, so the pin still *works* — the faulty parts are the stated *reasons*, not the pin. (→ M2)

---

## Rubric Item 5 — Spec coverage: every Epic and FR mapped

**Verdict: PARTIAL — all 3 Epics and all 24 FRs are attributable to a hexagon; two FRs' load-bearing parts are not actually governed, and 4 NFRs are unmapped.**

Capability→Map rows account for Epic 1, Epic 2, Epic 3, cross-module auth, dashboard/status, and DSGVO. Tally of the 24 FRs against AD bindings + map:

- **Epic 1 (FR-1..FR-7)** → `internal/user`, AD-2/AD-3. All covered (FR-1..FR-6 via AD-2's binding list, incl. session/TOTP/lockout/pending state; FR-7 via AD-3). ✓
- **Epic 2 (FR-8..FR-18)** → `internal/tools`, AD-4/AD-5/AD-7. All covered, including the CSR of every read path (Dashboard, PDF, tool detail) by AD-4. ✓
- **Epic 3 (FR-19..FR-24)** → `internal/admin`, AD-2/AD-6. FR-19 (403 + existence-hidden) is genuinely enforced server-side by AD-6. FR-20/21/22 sit on AD-2's ownership + AD-7. **FR-23 is attributable but ungoverned** — no AD fixes who may write Tool/Tool-Type/checklist data (→ H1). **FR-24 is attributable (map row "DSGVO ops") but its hard part — the erasure/anonymization write into the tools module + immutable audit trail — is ungoverned** (→ C1, NFR-O2).

NFR sweep (not all NFRs need an AD — but silence is either deliberate or a leak):
- **Unmapped/silent:** NFR-S1 (TLS), NFR-S4 (secrets management), NFR-O2 (immutable audit trail — the "immutable" qualifier is the missing bite; the consistency table's logging row only pins NFR-O1), NFR-R4 (99% availability — no owner, becomes part of the operational-envelope finding M1).
- NFR-S5 → stack security note ✓; NFR-R1/R2/R3 → stack/deploy/Deferred ✓; NFR-M1/M2/M3/M4 → AD-1/CI gates/docs dir ✓; NFR-O1 → consistency table ✓; NFR-C1 → FR-24 (minus the mechanism) ✓; NFR-P1/P2/PL1/PL2 → reasonably treated as non-divergent implementation detail.

---

## Rubric Item 6 — The altitude's owned dimension: operational/environmental envelope

**Verdict: PARTIAL → this is the "whole dimension left silent" finding. (→ M1)**

The Structural Seed section *claims* the operational envelope ("the cold-start scaffold and the operational envelope"), and the spine's own frontmatter puts it at feature altitude keeping the epics. Decided: docker-compose multi-service, `deploy/`, backup job container, docs. Deferred: CI/CD provider + workflow files, backup destination.

**Silent — neither decided, deferred, nor listed as an open question:**
- **Deployment environments** (dev/staging/prod split). For a greenfield repo whose CI/CD is deferred, the environment split is the thing the CI/CD will feed; it is never mentioned.
- **Infra/provider strategy** (where this runs: on-prem hardware vs. a VPS/cloud tier) — this cascades into DNS/TLS termination, which is why NFR-S1 (TLS 1.2+, enforce/reject plain HTTP) currently has no owner and sits unmapped.
- **Operations/monitoring beyond structured logs**: NFR-R4's 99% availability has no owner; there is no uptime/metrics/error-tracking story at all (O1 logging is the single observability pin).
- **Secrets management** (NFR-S4): silent in every section.

The rubric's bar is explicit: an altitude that owns a dimension must decide, defer, or open-question it — here roughly half of the envelope is simply absent, and the half that is present is the easy half.

---

## Rubric Item 7 — Paradigm named + mapped to dirs; diagrams valid and structural

**Verdict: PASS with two structure-level notes.**

- Paradigm is named ("Modular monolith — hexagonal (ports & adapters) per module") and mapped to directories: `cmd/server` (composition root), `internal/{user,tools,admin,platform}`, `web/`, `migrations/`, `deploy/`, `docs/`. `internal/platform` is correctly left business-free. ✓
- Diagrams are valid mermaid and convey structure coherently:
  - Fig. 1 (flowchart): HTTP adapter drives all three hexagons; TMM/ADM consume AuthPort; UDA implements it; per-module repository adapters into Postgres. Correct and legible. ✓
  - Fig. 2 (deployment): Frontend / Go API / Postgres containers + backup job to configurable destination. ✓ — but introduces the `GW` (auth gateway) component that neither Fig. 1 nor the paradigm text defines; combined with the AD-2/AD-6 mechanism ambiguity (see Item 2), the diagram is making a structural commitment the rules don't spell out.
  - Fig. 3 (ERD): valid; conveys the "derived status via inspection records" story. ✗ **Missing the mandatory `TOOL_TYPES → QUALIFICATIONS` edge** (FR-8) — the one cross-module reference the spec requires. Under "never reach into storage," its absence is the visible symptom of the ungoverned cross-module-reference question (→ H2).

---

## Findings Summary

### CRITICAL

**C1 — FR-24 erasure/anonymization is a cross-module data mutation with no mechanism and no owner.**
The spine's central law (AD-1) forbids modules from reaching into each other's storage; the PRD forces exactly that: user-module deletion must retain-and-anonymize tools-module inspection history (`"Deleted User"`) and emit an immutable audit entry (NFR-O2). No port/event/outbox/denormalization decision exists — not in any AD and not in Deferred. Epic 1 and Epic 3 will necessarily choose incompatible mechanisms. **discuss** (needs a new AD or an explicit deferral with the invariant stated: "erasure must translate to an anonymizing write against tools-module history through a defined port/event").

### HIGH

**H1 — FR-23 (Admin configures Tool Types/Tools/checklists): the write path has no owner.**
The data being configured is tools-module data (AD-5 owns FR-8/FR-9); Admin must edit it. The spine never decides "Admin writes through a tools-side port" vs. "Admin owns its own repository/copies." That recreates exactly the two-sources-of-truth failure AD-2 was built to prevent, one module over. **autofix** (one ownership line, mirror-styled on AD-2, e.g. extend AD-5: "Admin reaches Tool/Tool-Type/checklist state only through the tools module's port").

**H2 — Cross-module data references in the shared DB are ungoverned.**
The ERD draws `USERS → INSPECTIONS` (tools-module rows referencing user-owned users) and omits the required `TOOL_TYPES → QUALIFICATIONS` reference (FR-8). Nobody has decided: cross-module FK ownership, schema-per-module vs. single public schema, or how one golang-migrate set avoids table-name collisions across modules. Adjacent: the `GW` component drawn in Fig. 2 vs. the AD-2/AD-6 per-capability policy phrasing (central choke-point vs. per-module middleware). **discuss**.

### MEDIUM-LOW

**M1 — Operational envelope is half silent: environments, infra/provider strategy, NFR-R4 availability, NFR-S4 secrets, NFR-S1 TLS owner all absent — not decided, not deferred, not open-questioned.** The spine claims to own the envelope; the environment split and provider strategy are exactly the load-bearing ones the CI/CD deferral will feed. **discuss**.

**M2 — Stack "verified current at authoring" is factually stale on 3 rows.** Go claimed "current stable" = 1.24, but 1.27.0 shipped 2026-08-19 (11 days pre-authoring); chi v5.3.2's "supports Go 1.20+" is wrong (min Go 1.23 since v5.3.0); Vite "latest stable (6.x)" is wrong (8.2 current; 6.x security-only). sqlc v1.31.1, golang-migrate v4.19.1, chi v5.3.2, pgx v5, React 19, TS 5.x all verified current. The pins still function (Go 1.24 ≥ 1.23), but the stated reasons and the "current stable" claims don't. **autofix**.

**M3 — Paper cuts.** AD-4 Rule typo "Complaint with equal force" → "Computed"; AD-1 wrongly binds NFR-M2 (test coverage — belongs to the CI gates); AD-2 lists FR-20 twice; FR-9 CSV transaction-vs-partial-import semantics unprepinned. **autofix** for the typo/binding; **ignore** for FR-20 duplication.

---

## Item-by-item verdict card

| # | Rubric item | Verdict |
|---|---|---|
| 1 | Divergence points fixed & none missed | PARTIAL (2 real misses: C1, H1; H2 mechanical) |
| 2 | AD Rules enforceable & prevent stated divergence | PASS (GW/policy exercise ambiguity + typo) |
| 3 | Deferred section honest | PASS |
| 4 | Tech verified-current, stack consistent | FAIL on 3 stale rows (Go, Vite, chi-Go-min) |
| 5 | Every Epic + FR mapped | PARTIAL (FR-23/FR-24 load-bearing parts ungoverned; NFR-S1/S4/O2/R4 unmapped) |
| 6 | Owned dimension decided/deferred/open-questioned | PARTIAL (envs + infra/provider + NFR-R4/S4 silent) |
| 7 | Paradigm→dirs; diagrams valid & structural | PASS (ToolType→Qualification edge missing; GW ambiguity) |

**Overall: CONCERNS** — a strong spine on the highest-value divergences (derived status, single due-date clock, single auth owner, qualification gating), honest about what it defers, and diagrammatically sound. The costs are concentrated on the two couplings the spec cannot avoid and the spine's isolation law doesn't answer: DSGVO erasure-writing across modules, and Admin writing tools-module data. Both need a decision before the epics start, or the very modularity the spine is betting on will be the thing the epics subvert.
