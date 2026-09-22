---
sidebar_position: 2
---

# The Dashboard

The dashboard is the **start page after login**: every tool with its live
status, newest first.

## The traffic light

| Dot | Status | Meaning |
|---|---|---|
| 🟢 <span className="gear-status-dot gear-green" /> | **Green** | Inspected on time, safe to use |
| 🟠 <span className="gear-status-dot gear-orange" /> | **Orange** | Inspection due within the next two weeks |
| 🔴 <span className="gear-status-dot gear-red" /> | **Red** | Inspection overdue (now or past due) |
| ⬛ <span className="gear-status-dot gear-oos" /> | **Out of service** | Failed inspection, awaiting reinstatement |

The status is **always derived** from the recorded inspections — it can never
go stale or disagree between users.

## What you can do on the dashboard

- **Read the readiness at a glance** — green means "ready", red means "needs
  attention", black means "do not use".
- **Open a tool** to see its history and details (permission-dependent).
- **Start an inspection** from a tool that is due (with the
  `inspection.submit` permission and the tool's required qualification).
- **Reinstate** an out-of-service tool (Führende/Admin only, with a mandatory
  reason).
- **Filter and export** the report as a PDF (Führende/Admin).

<figure className="gear-shot gear-shot-frame-desktop">
  <img src="/gear/screenshots/screenshot-1-dashboard-desktop.png" alt="The G.E.A.R. dashboard with the tool traffic-light list" loading="lazy" />
  <figcaption>The dashboard — every tool with its status.</figcaption>
</figure>

## What a tool's row tells you

Each row shows the tool **name**, its **type**, and its **status color**. The
status is computed from the latest inspection and the tool's schedule — a
reinstated tool gets its clock restarted from the reinstatement date.