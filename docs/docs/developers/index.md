---
sidebar_position: 1
---

# For Developers

Everything a developer needs: how the code is organized, the API, the flows,
and the implementation status.

import Card from '@site/src/components/Card';
import CardGrid from '@site/src/components/CardGrid';

<CardGrid>
  <Card to="/docs/implementation/modules" icon="🧩" title="Hexagon Modules" description="The modular monolith: user, admin, tools, platform." />
  <Card to="/docs/implementation/api" icon="🔌" title="API Endpoints" description="The route catalog as a map + the interactive OpenAPI reference." />
  <Card to="/docs/implementation/flows" icon="🔄" title="Flows" description="Sequence diagrams: registration, login, OTP, qualifications." />
  <Card to="/docs/status" icon="📊" title="Implementation Status" description="Stories per epic, auto-generated from the sprint status." />
  <Card to="/docs/epics" icon="🗂️" title="Epics & Stories" description="Every story with its acceptance criteria." />
  <Card to="/docs/screenshots" icon="📷" title="Screenshots" description="The screenshot inventory tracker." />
</CardGrid>

## Quick facts

| Area | Technology |
|---|---|
| Backend | Go (chi, sqlc/pgx, golang-migrate) |
| Frontend | React + Vite + TypeScript SPA |
| Database | PostgreSQL |
| Tests | Go `testing` + Vitest, Playwright for docs |
| IaC | OpenTofu |

## The one big idea

G.E.A.R. is a **modular monolith with hexagonal architecture** (AD-1): every
feature module (`internal/user`, `internal/admin`, `internal/tools`) is an
isolated Go package exposing only **port interfaces** across its boundary. The
`internal/platform` layer provides cross-cutting infrastructure and never
contains business logic. The [Hexagon Modules](/docs/implementation/modules)
page shows it with diagrams.