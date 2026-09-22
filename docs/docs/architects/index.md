---
sidebar_position: 1
---

# For Architects

The design documents: what G.E.A.R. must do, how it is built, and the
operational plan.

import Card from '@site/src/components/Card';
import CardGrid from '@site/src/components/CardGrid';

<CardGrid>
  <Card to="/docs/planning/prd" icon="📘" title="PRD" description="Functional requirements, epics, non-functional guidelines." />
  <Card to="/docs/planning/architecture-spine" icon="🏛️" title="Architecture Spine" description="Technical invariants, ADs, database artifacts, diagrams." />
  <Card to="/docs/planning/addendum" icon="➕" title="Architecture Addendum" description="Technology stack decisions and deferred options." />
  <Card to="/docs/planning/module-roadmap" icon="🗺️" title="Module Roadmap" description="The modular ecosystem and planned modules." />
  <Card to="/docs/planning/ux-design" icon="🎨" title="UX Design" description="Interaction patterns and design constraints." />
  <Card to="/docs/planning/deployment-ionos" icon="🚀" title="Deployment" description="From a local registry to a small self-hosted server (IONOS)." />
</CardGrid>

## The design spine in one sentence

G.E.A.R. is a **modular monolith with hexagonal architecture** — three
self-contained feature modules (User, Admin, Tools) behind port interfaces on a
cross-cutting platform layer, a PostgreSQL database, and a Go + React
frontend/backend. The spine fixes the invariants; the host/provider is a
deployment decision.

## Non-functional commitments

- **NFR-M2/M3** — automated tests + lint gate CI; DB-backed suites run for real.
- **NFR-R1–R4** — containerized deploy, migrations, backup/restore, 99% monthly
  availability.
- **NFR-S1/S4** — TLS 1.2+, secrets via env/secret-manager, never in VCS.
- **NFR-O1/O2** — structured logging + immutable audit on every sensitive
  operation.