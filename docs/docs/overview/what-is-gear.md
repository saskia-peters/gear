---
sidebar_position: 1
---

# What is G.E.A.R.?

**G.E.A.R.** (Geräte-Einsatz-Assistenz & Readiness) is the equipment-management
app of the **Ortsverband Singen** — the place where every tool lives, gets its
inspection, and shows its readiness at a glance.

import Card from '@site/src/components/Card';
import CardGrid from '@site/src/components/CardGrid';

**In a few sentences:** G.E.A.R. replaces the paper lists and spreadsheets that
used to track the association's equipment. Every chainsaw, generator and rescue
tool now has one shared digital record: a clear **traffic-light status**
(green / orange / red / out of service), a **qualified** person performs every
inspection, and the whole history is recorded and reproducible. The dashboard
tells you in one glance what is ready to use, what needs attention, and what
must not be used until it is checked again.

<CardGrid>
  <Card to="/docs/management-overview" icon="👥" title="For decision-makers" description="Plain-language overview — the problem, the roles, the inspection cycle." />
  <Card to="/docs/overview/user-stories" icon="🎬" title="See it in action" description="User stories with real screenshots and diagrams." />
  <Card to="/docs/end-users" icon="🧑‍🔧" title="For end users" description="How volunteers, caretakers and leaders use the app day to day." />
  <Card to="/docs/administrators" icon="🛠️" title="For administrators" description="Users, roles, the catalogue, settings, backups and DSGVO." />
</CardGrid>

## What makes it trustworthy

- **One source of truth.** Status and due dates are always *derived* from the
  recorded inspections — there is no separate "status" that can go stale.
- **Qualifications are enforced.** A chainsaw can only be inspected by someone
  who holds the chainsaw certificate — checked by the system, not by memory.
- **A clear traffic light.** 🟢 Green, 🟠 Orange (due soon), 🔴 Red (overdue),
  ⬛ Out of service — every tool has exactly one, always current.
- **Full history.** Every inspection and reinstatement is recorded; questions
  and audits are answered in seconds.
- **Secure by design.** Two admin accounts with dual control, encrypted
  credentials, DSGVO access reports and deletion.

## Who uses it

| Role | Typical person | What they get |
|---|---|---|
| **Helfende** | Volunteers who work with the equipment | Inspect tools, see what's due, record results |
| **Schirrmeister** | The equipment caretaker | Maintain the tool catalogue, check readiness |
| **Führende** | Unit leaders | Overview, history, reports and PDF export |
| **Admin** | One trusted administrator | Manage users, roles, settings, backups and DSGVO |

## What it is not (yet)

Tool reservations/booking, repair tracking, automated notifications and
external integrations are **not** in version 1 — they are candidates for later
versions, and the architecture is built so they can be added without reworking
what exists.

---

**[Read the management overview](/docs/management-overview)** for the
full plain-language explanation, or **[see it in action](/docs/overview/user-stories)**
with real screenshots.