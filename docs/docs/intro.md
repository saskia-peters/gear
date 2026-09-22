---
sidebar_position: 1
---

# G.E.A.R. — Documentation

**G.E.A.R.** (Geräte-Einsatz-Assistenz & Readiness) modernizes and centralizes
the operational equipment management of the Ortsverband Singen: a modularized
web application for mobile and desktop devices.

This documentation is organized **by audience** — pick the section that matches
your role.

## For everyone

- [What is G.E.A.R.?](/docs/overview/what-is-gear) — a few sentences on what it
  is and what it is good for (non-technical)
- [Management Overview](/docs/management-overview) — the plain-language
  deep-dive for decision-makers
- [User Stories](/docs/overview/user-stories) — see it in action with real
  screenshots and diagrams

## By role

| Section | For | What you find |
|---|---|---|
| [For End Users](/docs/end-users) | Helfende, Schirrmeister, Führende | Dashboard, inspections, history, your account |
| [For Administrators](/docs/administrators) | Admin | Users & roles, catalogue, settings, backups, DSGVO |
| [For Developers](/docs/developers) | Engineers | Modules, API, flows, status, epics |
| [For Architects](/docs/architects) | Designers & reviewers | PRD, architecture spine, addendum, roadmap, deployment |

## API reference

The interactive **OpenAPI reference** is available at
[API Reference](/docs/api/g-e-a-r-api) (generated from the Go route
registrations + the spec).

## Stack

| Layer | Technology |
|---|---|
| Backend | Go (chi, sqlc/pgx, golang-migrate) |
| Frontend | React + Vite + TypeScript SPA |
| Database | PostgreSQL |
| Docs | Docusaurus (this site) |
| IaC | OpenTofu (Google Cloud Run testing/staging) |