---
sidebar_position: 3
---

# Inspections

Every tool type decides how it is inspected:

- **Pass/Fail** — one overall result: pass or fail, with optional notes.
- **Checklist** — a fixed list of items from the tool type; each item gets its
  own pass/fail, and the overall result is derived from the item results.

## Before you can inspect

The system **re-checks your qualification** on submit — never trusting the
client. A tool whose type requires a certificate (e.g. chainsaw) can only be
inspected by someone who holds it. If you do not hold it, the app shows the
reason and blocks the submit.

An **out-of-service** tool cannot be inspected at all.

## Performing an inspection

1. Open the tool (from the dashboard).
2. Choose **start inspection**.
3. Record the result:
   - pass/fail: pick **pass** or **fail**, optionally add notes.
   - checklist: mark **each item** pass/fail.
4. Submit. The record is persisted with your identity, the timestamp, and the
   snapshot of the checklist items.
5. The **status is recomputed** and shown in the confirmation.

A **failed** inspection puts the tool **out of service**. Only a reinstatement
by a Führende or Admin brings it back.

## Reliability

The inspection is **at-most-once**: every submit carries a client-generated
idempotency key, so a retry after a network error can never create a duplicate
record. If the server already committed the first attempt, the retry returns
that same record.