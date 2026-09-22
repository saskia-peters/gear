---
sidebar_position: 6
title: Module Roadmap
description: The G.E.A.R. modular ecosystem — Module 1 (tool inspection), the planned Module 2 (global todo list), a future inventory module, and the reserved multilanguage UI feature.
---

# Module Roadmap

G.E.A.R. is a **modular approach**: a shared platform of self-contained modules, each solving one operational need of the Ortsverband. New modules are added over time without rebuilding the platform — they plug into the existing user directory, authentication, roles, and audit foundation.

The **first module is tool inspection** (the current G.E.A.R. application). Other modules will follow.

---

## Module 1 — Tool Inspection (current)

The inspection module keeps the Ortsverband's equipment inspection-ready: qualified volunteers perform pass/fail or checklist inspections on schedule, tools are flagged red / orange / green / out of service, and the dashboard, history, and reports give leadership a live overview.

---

## Module 2 — Global Todo List (planned)

Module 2 is already planned as a **global todo list** to which **Führende** can add todo items:

* Each todo item carries:
  * a **name** and an **explanation**,
  * an **optional** upload of **attachments** (e.g. images, PDFs, …),
  * a **created at** date, and
  * an **optional due-by** date.
* Any **Helfende** can **pick up** a todo item, work on it, and mark it as **resolved** — resolved items are flagged as such.
* **Resolved items must be reviewed by Führende and closed by them** before they leave the active list.
* An **audit log** records the lifecycle of every item.
* **Closed items are archived but not deleted** — so reports can be created and history checked afterwards.
* When resolving, a **Helfende can add a comment and attach files**, giving Führende additional information about what was done on the resolved item.

---

## Future Module — Inventory & Rolling Inventory Check

Another module for the app could be an **inventory**. The organization operates **several trucks**, each equipped with different **equipment and tools** (the tools that are inspected).

* A **defined inventory** per truck describes what should be on that truck.
* Helfende could perform a **"rolling inventory check"**: they mark each item as **"on the truck"**, **"missing"**, or the tool is **OOS** (derived from the G.E.A.R. inspection status).
* A person from **Führende, Schirrmeister, or Admin** could get an **overview with one click**: what is on the truck, what is missing, and what is out of service.
* A rolling inspection avoids the current approach of having to remove everything from a truck at one point in time and check it, only to find out afterwards what is available and what is missing.

---

## Reserved for Future Use — Multilanguage UI

A **multilanguage feature of the UI** is **reserved for future use** — the interface is currently German-only, but the architecture must keep room for localized UI without rework.