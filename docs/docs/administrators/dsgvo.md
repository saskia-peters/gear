---
sidebar_position: 6
---

# DSGVO

The German data-protection (DSGVO/GDPR) operations live in the admin panel.

## Access report (Auskunft)

Produce a **DSGVO access report** for one user: every personal data reference
the system holds about them — their account, plus every inspection they
performed (as inspector) and every reinstatement (as actor), with the items.

## Account deletion (Löschen)

Delete a user's account and personal references in a **DSGVO-compliant** way:

- The user's personal references (inspector / actor) are rewritten to the
  canonical **"Deleted User"** sentinel.
- Every timestamp, result, item and status stays intact — the equipment history
  is preserved even though the person's identity is removed.
- The operation is **idempotent** — a user with no references (or an already
  anonymized one) is a no-op, and it is audited.

## Why the history survives

Tool inspection history is operationally important, not just personal. DSGVO
deletion **anonymizes** the personal identity while keeping the equipment trail
intact — so the dashboard, history and reports keep working with a neutral
"Deleted User" label.

## Audit

Every DSGVO operation is an **immutable, high-severity audit event** (NFR-O2),
logged structured (NFR-O1). Nothing about these operations is silent.