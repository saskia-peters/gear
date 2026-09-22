---
sidebar_position: 1
---

# For Administrators

This section is for the **Admin** who keeps G.E.A.R. running: users, roles,
the catalogue, settings, backups and DSGVO.

import Card from '@site/src/components/Card';
import CardGrid from '@site/src/components/CardGrid';

<CardGrid>
  <Card to="/docs/administrators/users-and-roles" icon="👥" title="Users, Roles & Permissions" description="Approve users, assign roles and qualifications." />
  <Card to="/docs/administrators/catalogue" icon="🧰" title="The Tool Catalogue" description="Tool types, checklists, tools, schedules." />
  <Card to="/docs/administrators/settings" icon="⚙️" title="System Settings" description="Configurable settings, schedules, SMTP." />
  <Card to="/docs/administrators/backups" icon="💾" title="Backups & Restore" description="Destinations and the restore procedure." />
  <Card to="/docs/administrators/dsgvo" icon="🔒" title="DSGVO" description="Access reports and account deletion." />
</CardGrid>

## Access

The whole admin module is gated by the `admin.settings.backup` /
`admin` permission family (AD-6) and is **isolated from the rest of the app** —
only authorized holders reach it. Defense-in-depth means the core re-checks the
permission on every operation, not just at the route.

## Dual-admin rule

The system starts with **two admin accounts**. Neither can reset the other's
credentials alone — recovery requires the other admin's approval. This is the
protection against a single compromised account taking over the system.