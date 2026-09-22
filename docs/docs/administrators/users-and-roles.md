---
sidebar_position: 2
---

# Users, Roles & Permissions

## User directory

- **Benutzer** — the user directory: every account with its state (pending,
  active, …), roles and qualifications.
- **Pending approvals** — new registrations wait here until an admin approves
  them (only then can the user log in).
- **Benutzergruppen** — organize users into groups.

<figure className="gear-shot gear-shot-frame-desktop">
  <img src="/gear/img/screenshots/screenshot-2-admin-users-desktop.png" alt="The admin user directory" loading="lazy" />
  <figcaption>Admin — the user directory.</figcaption>
</figure>

## Roles & permissions

- **Rollen** — create and edit roles, each with a set of **permissions**.
  Base roles ship seeded: `helfende`, `schirrmeister`, `fuehrende`, `admin`.
- The **active permission set** of a user is resolved live from their role
  memberships — there is no stale cache.
- The SPA shows only what the user may see; the **server is the real gate**
  (AD-6) and re-checks on every operation.

<figure className="gear-shot gear-shot-frame-desktop">
  <img src="/gear/img/screenshots/screenshot-2-roles-desktop.png" alt="The roles and permissions editor" loading="lazy" />
  <figcaption>Admin — roles and permissions.</figcaption>
</figure>

## Qualifications

- **Qualifikationen** — the qualification catalogue (e.g. "Kettensägen-Führerschein").
- Users **hold** qualifications; a tool type can **require** one.
- The requirement is enforced on inspection start **and** submit — the system
  blocks an inspection by someone who does not hold the required
  qualification.

<figure className="gear-shot gear-shot-frame-desktop">
  <img src="/gear/img/screenshots/screenshot-2-qualifications-desktop.png" alt="The qualifications list" loading="lazy" />
  <figcaption>Admin — qualifications.</figcaption>
</figure>

## One-time passwords (OTP)

Admins can issue a **one-time password** to unlock a user who must change their
password on next login.