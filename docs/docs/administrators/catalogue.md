---
sidebar_position: 3
---

# The Tool Catalogue

The catalogue is what the whole inspection system runs on: tool types with
checklists, individual tools, and the schedules that drive due dates.

## Tool types (Werkzeugtypen)

Each tool type defines:

- a **name**,
- a **default schedule** (how often it must be inspected — "1 Jahr", "1
  Quartal", "1 Monat", "2 Wochen", "3 Tage", …),
- an optional **required qualification**,
- an **inspection mode**: pass/fail or a **checklist** of individual items,
- optional **attributes** (flexible JSON).

## Tools (Werkzeuge)

Each tool belongs to exactly one type and may optionally **override** its
type's schedule. A tool always has a unique **inventory number** (e.g.
`GEAR000001`) and an **attributes** record.

- Tools are **soft-archived** — an archived tool leaves the active list and can
  no longer be edited, but its history stays.
- Creating/editing tools requires `tools.manage` (or the scoped `tool.edit` for
  details + inventory number).

<figure className="gear-shot gear-shot-frame-desktop">
  <img src="/gear/screenshots/screenshot-4-3-tools-desktop.png" alt="The admin tool catalogue" loading="lazy" />
  <figcaption>Admin — the tool catalogue.</figcaption>
</figure>

## Schedules (Zeitpläne)

Named, reusable schedules live in the admin panel. The **next inspection date**
is one shared rule: last successful inspection **plus** the schedule. A
reinstated tool restarts from the reinstatement date — so no tool can drift
into ambiguity.

## Rules that protect the data

- The **type FK is validated active-only** on create *and* update — a live tool
  whose type was archived cannot be silently re-pointed (it answers a clear
  German error).
- The **schedule override FK** is validated against the active catalog — an
  archived schedule is not re-selectable.
- Duplicate tool names and inventory numbers are rejected (case-insensitive,
  with archived reservations respected).