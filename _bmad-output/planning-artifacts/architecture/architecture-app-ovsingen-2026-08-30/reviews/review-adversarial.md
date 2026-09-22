---
title: Adversarial Architecture Review — G.E.A.R. V1
type: architecture-review
subtype: adversarial
reviewer: adversarial-architecture-reviewer
subject: ARCHITECTURE-SPINE.md (2026-08-30)
base: PRD prd-app-ovsingen-2026-08-29 + addendum.md
verdict: GAP
created: '2026-08-30'
---

# Adversarial Review — ARCHITECTURE-SPINE.md

**Method.** Adversarial pairing: for each hypothesized hole, construct two units one level down (two epics, or two features within the epics) that each obey **every** AD "to the letter" under a defensible reading, then show the pair building incompatibly. The spine's real job — per AD-1 "prevents two modules built independently selecting incompatible shapes" — is tested by whether the seven ADs are jointly [sound](observational equivalence) under all readings. They are not: each finding below is a **reaching-reading pair** (two literal readings disagree), not a plausibly-wrong team.

**Surface that protects compatibility today.** The spine depends on four load-bearing silences:

1. The `Service` port (AD-2) is named but its **method set** is never enumerated. AD-7 demands qualification reads "from the auth port," AD-6 demands gateway re-validation, Epics 1/3 demand Admin-write operations (approve, assign quals, deactivate) — none of which appear in a port contract.
2. Every hexagon has "its own repository adapter" into the **same** Postgres (mermaid + structural seed), but no AD states **which module owns which table**. AD-1 forbids *importing* another module's adapter — not writing a second, fresh sqlc package against the same table. That is the classic modular-monolith leak.
3. AD-3 defines a JSONB `attributes` surface but no **key namespace, shape, or versioning**, and no gate requiring *core or gated* attributes (i.e., any attribute referenced by a cross-module invariant) to be first-class.
4. AD-4/AD-5 define derivation formulas but not their **input event universe** (which records feed status? where does a reinstatement event live? what is null-date behavior?) nor year-scales of the interval arithmetic.

---

## Pair 1 — Qualification interface & validator ownership (Epic 1 × Epic 2)

**Units.** Unit A = Epic 1 feature "resolved permission set" (FR-6). Unit B = Epic 2 feature "qualification-gated inspection" (FR-11).

**Both obey.** A obeys AD-2's Rule literally: the port resolves *identity and resolved permission set*; qualifications are owned by A but are **not** named in the port's stated duties. B obeys AD-7 literally: *"Tool Maintenance reads the Tool Type's required Qualification and the caller's granted Qualifications from the auth port and permits initiate/submit only when the caller holds the required one."*

**The incompatibility.**
- A ships `Service.Authenticate(token) → (Identity, PermissionSet)` where `PermissionSet` is a set of group-inherited permission codes (`tools.inspect`, `tools.reinstate`, `admin`, ...). Qualifications exist as table rows but are **not** exposed on the port.
- B, per AD-7, calls a method A never defined — e.g. `Service.QualificationsOf(callerID)` — and expects `tool_types.required_qualification` to reference A's `qualifications` row **by UUID**.
- B cannot compile or cannot get data. Its lawful-looking escape is to write its *own* sqlc query joining qualification rows inside the shared DB (`SELECT name FROM users.qualifications …`). This is AD-1-clean **by the letter**: B imported no adapter, it authored a fresh one — exactly the arrangement the spine's own diagram blesses (every hexagon has a repo adapter into the same DB).
- Result: two codebases read/write one table shape (A: normalized `qualifications` rows; B: whatever its ad-hoc JOIN expects), and the **check site** is contested: AD-7 says TM "permits initiate/submit" (consumer-side app service), AD-6 says every capability re-validates at the gateway. A second, dormant split: gateway re-check vs. TM app-service check — both "server-side," both "from AD-2"; one of them is the single source and NFR-S3's one test passes, the other never runs.

**Deferred/concrete scenario.** A stores `qualifications.name` = `"Chainsaw Certificate"`; B's permission-code gate keys off `permission.qualified.chain-saw`; a user granted the certificate (FR-22, UJ-3 climax) still gets a clear access error at submit because the code and the row never coincide — or B goes around it and FR-11's "can initiate" test passes against a JOIN that A's refactor to first-class columns (AD-3 promotion migration) then silently breaks.

**Remedy.** Tightened AD-2 + AD-7 (or a new AD): enumerate the `Service` port method set (identity, permission set, and *granted-qualification set* as a first-class query); pin `required_qualification` as a first-class FK column on the tools module referencing UD&A's qualification catalog **never a JSONB key and never a raw code string**; and declare the single check site — TM's application service — plus the rule that module-side SQL touches only that module's owned tables (carry into the Pair-2 ownership AD). Include an explicit cache rule: resolved sets resolve **per request** so FR-22's "immediately on the very next check" cannot be defeated by a session-cached claims set.

---

## Pair 2 — Two owners of `tools` / `tool_types` (Epic 2 × Epic 3)

**Units.** Unit A = Epic 2 tool management (FR-8, FR-9: Tool Type default interval, per-tool override, CSV import). Unit B = Epic 3 admin configuration (FR-23: "the admin module exposes full Tool Type and Tool management (FR-8 and FR-9), including checklist item definition and CSV bulk import").

**Both obey.** FR-8/FR-9 are bound to Epic 2 by AD-2/AD-3/AD-4/AD-5, and FR-23 *re-states the same consequences* inside Epic 3. AD-1's Rule blocks a module from importing "another module's adapter, handler, or repository implementation" — it says nothing about which module's repository **owns the tables**. The spine's DB diagram explicitly re-creates an `ADM --> DB` repository adapter into the same Postgres.

**The incompatibility.**
- A models `tools.schedule_override` as a typed column (the ER diagram's `date schedule_override`), stores custom attrs under `attributes["…"]` in A's key namespace, and enforces interval/month validation in its app service (which CSV import routes through, per FR-9's per-row error requirement).
- B, to expose the same surface in its isolated hexagon, generates its *own* sqlc package for `tools` / `tool_types` (lawful under AD-1), writes its config UI reads/writes from a **different** JSONB key (`attributes["schedule"]`) or its own model of the `schedule_override` column, and bulk-inserts CSV rows **bypassing A's service** — so A's validation and AD-5's interval rules never run on B-imported tools.
- AD-3's promotion path ("promote to a real column … and backfill") is a single migration, but only whichever module regenerated sqlc against the new column sees it; the other module's stale model compiles fine and reads NULL.
- Result: the same `tools` row renders a different `next_due` in the dashboard (A's column) than in Admin's edit form (B's JSONB); CSV-imported tools violate FR-9/FR-20-style per-row reporting; AD-5's "single shared function" computes over inputs only A wrote.

**Concrete scenario.** Admin imports `generator-09.csv` with a `schedule=6` column. B writes `attributes["schedule"]=6`. A's `schedule_override` column stays NULL; A's dashboards and the shared due-date function fall back to the Tool Type default (12 months, FR-9 consequence "custom interval … not the Tool Type default" is false for that row). No AD is violated at any point.

**Remedy.** **New AD** — *single-owner table registration*: every table is owned by exactly one module's adapter; any cross-module business mutation (including Admin's FR-23 config) flows through the **owner module's inbound application port** — which requires declaring the currently-absent Tool-Config port (`internal/tools` inbound surface consumed by `internal/admin`). CSV import is one owner-side feature; Admin merely drives its port. Also carry AD-3's JSONB-key-namespace/versioning rule here.

---

## Pair 3 — OOS: derived-pure vs. reinstatement-episode (two features inside Epic 2)

**Units.** Unit A = status-derivation feature (FR-14, FR-16: "Out of Service" is a *pure* read-time derivation). Unit B = reinstatement feature (FR-15: manual Fuehrung/Admin reinstatement with mandatory reason, clock reset).

**Both obey.** A reads AD-4 literally: *"Out-of-Service is itself derived: a tool is OOS iff its latest inspection or any of its latest checklist items failed **and it has not since been reinstated**"* → under a purist reading, when a *passing* inspection follows the failing one, the latest inspection no longer failed, so the tool is not OOS. B reads FR-15 + AD-5 literally: OOS is an *episode* exited **only** by a recorded reinstatement event (the mandatory reason note is an audit invariant; a passing inspection writes no reason note and so cannot be a reinstatement), and AD-5's `next_due = reinstatement_date + schedule_interval` branch only exists if reinstatement is the exclusive exit.

**The incompatibility.** A pass-after-fail diverges:
- Dashboard (A): latest inspection passed ⇒ Green/Orange per normal math; automation is satisfied.
- Tool detail, report export, and filter (B): still Red/Out-of-Service because no reinstatement event exists; FR-14/FR-15 history shows a documented open failure episode.

Both are AD-4/AD-5-faithful; this is exactly the "two independent consumers disagreeing on a tool's color" drift AD-4 names as its *prevention*, now reproduced by the two teams on the two reads. Note also the spillovers: the derivation's input universe is unspecified (does AD-4 read `inspections` only, or `inspections ∪ reinstatement events`?); AD-4 says "latest inspection … failed" yet also "not since been reinstated" — two predicates that need the event set to be consistent; and a tool with **no** inspection ever (fresh CSV import) has no `last_successful_inspection_date` at all — AD-5's formula returns null, and *no* AD assigns that tool a color. A's test suite demands one color, B's demands another.

**Remedy.** Tighten AD-4/AD-5: (i) declare the derivation's input universe = inspection records **plus** reinstatement events, both append-only; (ii) a passing inspection **never** reinstates — OOS exits only via a recorded reinstatement event carrying a reason; (iii) define null-date behavior for never-inspected tools (recommend: treat as due-now ⇒ Red, so brand-new inventory is visible to Fuehrung's readiness view, UJ-2); (iv) define interval arithmetic at year scale. This is not a new AD — it is making one rule unambiguous.

---

## Pair 4 — DSGVO anonymization with no Admin→Tool port (Epic 3 × Epic 2)

**Units.** Unit A = Epic 3 DSGVO account deletion (FR-24). Unit B = Epic 2 inspection history (FR-18) and its `inspector` join.

**Both obey.** FR-24 is bound in the spine only to AD-2 and AD-6 (no AD binds it to any Tool-side port). The spine's port diagram contains **no** Admin→Tool edge and no Tool outbound port — only `TMM → AuthPort` and `ADM → AuthPort`. A must still satisfy the PRD: *"Inspection history records linked to a deleted user are retained but anonymized: the inspector reference is replaced with the string `"Deleted User"`."* B models `inspections.inspector_id` as an FK UUID → `users`, which is precisely how FR-18/FR-17 render "name of last inspector."

**The incompatibility.**
- A, having no port to call, writes its own repository statement against the shared Postgres (`UPDATE inspections SET inspector_id = …`), targeting the string `"Deleted User"` (the PRD's literal requirement).
- B's model types `inspector_id` as `uuid`; the join either drops the rows (so deleted-user inspections vanish, violating "retained") or errors on a malformed cast; A may also hit/enforce FK constraints B installed, wedging the deletion mid-transaction (and NFR-O2's immutable audit trail sits in the same wedge).
- Whatever survives, the shape is clashing: A's anonymized marker is text, B's read model is UUID — two owners of the `inspections` write surface, and the append-only invariant AD-4's derivation depends on (Pair 3) now has a third writer with no service in front of it.

**Remedy.** **New AD** — cross-module mutation ports: declare inbound ports on **each** owning hexagon for every foreign write the PRD requires in a different epic (here: `AnonymizeInspector(userID)` on the tools hexagon, consumed by Admin), and restate the FR-24 anonymization shape as a **typed sentinel** (`inspector_id` set to NULL + `inspector_display = 'Deleted User'` or an explicit anonymized-reference type) so B's read model and A's write model share one shape. This is the single highest-cost hole: it couples a data-integrity operation to an undeclared port and lets two sqlc packages own one table across a deletion that must be atomic.

---

## Pair 5 — Gateway policy vocabulary and admin isolation (Epic 1 × Epic 3)

**Units.** Unit A = Epic 1 permission catalog (FR-6: `Helfer*in`, `Fuehrung`, `Admin` groups; resolved permission set). Unit B = Epic 3 admin isolation (FR-19) built in the composition-root gateway per AD-2/AD-6.

**Both obey.** AD-2 gives A the right to *define* permissions while owning users/groups; AD-6 gives B a gateway that "re-validates … against a server-side policy derived from AD-2" and returns 403 for non-Admins, hiding the module's existence.

**The incompatibility.**
- A labels the catalog with its own codes (e.g. `permission.administrator`, group name `Admin`), and — defensibly — caches the *resolved permission set* in the session at login (AD-2 says the port "resolves," not "re-resolves").
- B keys the admin route gate off the string `admin` (or off the group name), and re-checks inside the Admin hexagon by calling the AuthPort **again per request** (its FR-19/403 tests). The two permission vocabularies match only by luck; when A and B disagree on the code, admin routes 403 for real Admins or — worse — accepted for a revoked user for the rest of the cached session (FR-21 "revokes … immediately," FR-22 "immediately on the very next check" are both defeated by A's cache while B's re-check appears to pass its own tests).
- The gateway is drawn as the single funnel (mermaid: `SPA → GW → UD/TM/AD`), but the spine never says whether AD-6's re-validation is (a) gateway middleware only, or (b) a per-handler port call inside each hexagon. Under reading (a), a future route mounted inside the Admin hexagon but outside the gateway middleware is 403-blind; under (b), the *policy mapping* (which permission gates which route) has no named owner.

**Concrete scenario.** B's 403 test suite runs green. A deploys `permission.administrator`. A revoked admin's stale cached claims still pass the gateway for up to the session idle window (8h, NFR-S2) because AD-6's re-validation never says the resolved set must be re-derived per request.

**Remedy.** Tighten AD-2/AD-6: (i) the permission vocabulary is a named, machine-readable set owned by UD&A (one canonical action list: `tools.inspect`, `tools.reinstate`, `reports.export`, `admin.*`, …); (ii) gateway middleware is the single re-validation point with a single policy mapping, and hexagon handlers may consume the AuthPort for data but not for a second, independent policy; (iii) resolved permission sets are re-derived per request — no cross-request caching of claims — so revocations and deactivations (FR-20/21/22) bite immediately. The 403/existence-hiding consequence stays an AD-6 test of the gateway, now with one vocabulary and one clock.

---

## Deferred (non-issue for build substrate)

**Month-arithmetic unit (`schedule_interval`).** AD-5 computes `next_due` as date + interval "in months" (FR-8/FR-9) while thresholds are "calendar days." Because the spine mandates *one shared function all consumers call* (AD-5's final sentence), the two epics cannot actually diverge at runtime no matter which calendar/month convention they pick — only their *test-fixture expectations* could disagree, and only until one build owner pins the convention. Recommend one sentence in AD-5 ("intervals are calendar months, clone-with-clamp on the day; empty/never-inspected ⇒ due-now") and move on. Real-but-not-binding today.

---

## Prioritized closure list

| # | Hole | Pair | Severity | Fix |
| --- | --- | --- | --- | --- |
| 1 | Notification: no Admin→Tool port for FR-24 anonymization; two sqlc owners of `inspections`; text-vs-UUID inspector marker | 4 | **Critical** (data-integrity, atomic deletion) | New AD: cross-module mutation ports + one-owner-per-table + typed anonymization sentinel |
| 2 | `tools`/`tool_types` have two legitimate writers (FR-9 vs FR-23); JSONB keyspace and schema-promotion drift | 2 | **High** (silent wrong due-dates, AD-5 fed bad inputs) | New AD: single-owner table registry + Admin drives tools' inbound config port |
| 3 | Qualification port surface & check site unspecified (`Service` method set, required-qualification reference shape, per-request resolve) | 1 | **High** (FR-11/FR-22 compile- and runtime failure; AD-7's promise unfulfillable) | Tighten AD-2 + AD-7: enumerate port methods; FK not JSONB/code; one check site; no claim caching |
| 4 | OOS pass-after-fail contradiction; unnamed derivation input universe; null-date color | 3 | **Medium-High** (dashboard vs tool-detail color drift — the exact drift AD-4 bans) | Tighten AD-4/AD-5: inputs = inspections ∪ reinstatement events; passing never reinstates; null-date rule |
| 5 | Permission vocabulary, single gateway policy, and revocation immediacy | 5 | **Medium** (403 semantics, stale-claims revocation window) | Tighten AD-2/AD-6: named machine-readable action set; gateway = single policy point; per-request resolve |
| 6 | Month-arithmetic convention | — | Non-issue | One sentence in AD-5; decide at build |

**Verdict: GAP.** The spine is coherent as a *diagram* and silent where it must be a *contract*. Every pair above obeys all seven ADs to the letter under a competing literal reading; each closes only when the spine starts enumerating port methods, table ownership, derivation input universes, and vocabularies. Highest-priority: Pair 4 (undeclared port + two owners of inspection rows) and Pair 2 (two owners of tool rows + ungoverned JSONB keyspace).

---

*Evidence references: ARCHITECTURE-SPINE.md AD-1 (Rule, l.48), AD-2 (Rule, l.54), AD-3 (Rule, l.60), AD-4 (Rule, l.66), AD-5 (Rule, l.72), AD-6 (Rule, l.78), AD-7 (Rule, l.84); mermaid port/DB diagram (l.26-42, l.136-159); ER diagram (l.163-184); consistency conventions (l.88-98); PrD FR-1..FR-24 incl. FR-15 (l.176-183), FR-17 (l.193-198), FR-23 (l.237-241), FR-24 (l.243-249); Non-Goals (l.287-293).*
