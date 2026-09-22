---
sidebar_position: 4
---

# System Settings

The configurable system settings live in **Einstellungen → System**. Each
setting is one atomic row; values are type-checked (duration, integer, text)
and bounded. Every setting is saved per-row with inline German feedback.

## What you can configure

- **SMTP** — the transactional email delivery for the forgot-password / reset
  flow (host, port, security, sender, username, password). The password is
  stored **encrypted at rest** and is write-only/masked. A "send test email"
  action verifies the configuration.
- **Backup timeouts** — dial and protocol timeouts for the backup destination
  test-connection.
- **Backup interval** — how often the automated backup job runs (seconds; the
  seed is 86400 = daily). The job also runs shortly after server start.
- **Reset/recovery TTLs** — password-reset token lifetime, admin-recovery
  timeout, forgot-password throttle.
- **Inspection defaults** — the orange window (percentage of the interval) and
  the inventory-width default.
- **Schedule catalog** — named schedules (Zeitpläne) drive inspection due dates.

<figure className="gear-shot gear-shot-frame-desktop">
  <img src="/gear/img/screenshots/screenshot-3-settings-desktop.png" alt="The system settings" loading="lazy" />
  <figcaption>Admin — configurable system settings.</figcaption>
</figure>

## The rules

- The **key catalog is fixed** by the seed — an unknown key is rejected, not
  silently accepted.
- Values are **bounded**: durations ≥ 1s, integers within range, text within
  length, and the two recovery thresholds cannot be ordered incorrectly.
- No secrets are written to VCS — the encryption key and DB password come from
  the environment (`.env`, 0600) at runtime (NFR-S4).