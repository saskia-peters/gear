---
sidebar_position: 3
---

# User Stories — G.E.A.R. in Action

Real screenshots from the running app, each showing one everyday story. The
colored dots are **live** — green, orange and red pulse to show the traffic
light at work.

## 🟢 Story 1 — A volunteer inspects a tool

**Lisa (Helfende)** opens the dashboard, sees that the **chainsaw** is due for
its inspection, and performs it — the system checks her qualification first.

<span className="gear-status-dot gear-green" /> **Green** — ready to use
<span style={{marginLeft:'1.5rem'}} className="gear-status-dot gear-orange" /> **Orange** — due soon
<span style={{marginLeft:'1.5rem'}} className="gear-status-dot gear-red" /> **Red** — overdue
<span style={{marginLeft:'1.5rem'}} className="gear-status-dot gear-oos" /> **Out of service**

<div className="gear-story-step">
```mermaid
flowchart LR
  A["🔓 Login"] --> B["📋 Dashboard:<br/>chainsaw is Orange"]
  B --> C{"Has the<br/>chainsaw certificate?"}
  C -- "Yes" --> D["🔧 Perform inspection"]
  C -- "No" --> E["⛔ Blocked —<br/>show missing qualification"]
  D --> F["✅ Record result<br/>(pass/fail or checklist)"]
  F --> G["🟢 Tool is Green again"]
```

</div>

<figure className="gear-shot gear-story-shot gear-shot-frame-desktop">
  <img src="/gear/img/screenshots/screenshot-1-dashboard-desktop.png" alt="The G.E.A.R. dashboard shows the tool traffic-light list" loading="lazy" />
  <figcaption>The dashboard — every tool with its live status. 🟢 🟠 🔴 ⬛</figcaption>
</figure>

---

## 🟠 Story 2 — A caretaker keeps the catalogue ready

**Jonas (Schirrmeister)** keeps the equipment catalogue up to date: tool types
with their inspection checklists, individual tools, and their schedules.

```mermaid
flowchart LR
  A["🛠️ Werkzeuge"] --> B["Tool type<br/>+ checklist"]
  B --> C["Tool<br/>+ schedule"]
  C --> D["Status computed<br/>from the schedule"]
  D --> E["Dashboard stays<br/>current"]
```

<figure className="gear-shot gear-story-shot gear-shot-frame-desktop">
  <img src="/gear/img/screenshots/screenshot-4-3-tools-desktop.png" alt="The admin tool catalogue" loading="lazy" />
  <figcaption>The admin tool catalogue (Werkzeuge).</figcaption>
</figure>

---

## 🔴 Story 3 — A leader spots an overdue tool

**Marie (Führende)** sees that an **aggregate** is overdue (Red). She opens the
tool's history, sees the last inspection and the reason, and can export the
report as a PDF for the leadership meeting.

```mermaid
flowchart LR
  A["📉 Dashboard:<br/>aggregate is Red"] --> B["🔎 Open history"]
  B --> C["📄 Last inspection<br/>+ result"]
  C --> D["📊 PDF report"]
  D --> E["✅ Leadership<br/>informed"]
```

<figure className="gear-shot gear-story-shot gear-shot-frame-desktop">
  <img src="/gear/img/screenshots/screenshot-6-3-history-desktop.png" alt="The per-tool inspection history" loading="lazy" />
  <figcaption>The per-tool history — every inspection and reinstatement.</figcaption>
</figure>

---

## ⬛ Story 4 — An out-of-service tool is reinstated

A tool failed its inspection and is now **Out of Service** (⬛). Only a
Führende or Admin can reinstate it — with a mandatory reason. The clock
restarts from the reinstatement.

```mermaid
stateDiagram-v2
  [*] --> Green: inspected & OK
  Green --> Orange: due within 2 weeks
  Orange --> Red: due date passed
  Red --> Green: inspection passes
  Green --> OutOfService: inspection fails
  Orange --> OutOfService: inspection fails
  Red --> OutOfService: inspection fails
  OutOfService --> Green: reinstated (clock resets)
```

---

## 🛠️ Story 5 — An administrator manages users, roles and settings

**Anna (Admin)** approves new volunteers, assigns qualifications and roles, and
keeps the system settings, backup destinations and DSGVO operations configured.

```mermaid
flowchart LR
  A["👥 Benutzer:<br/>approve + roles"] --> B["🎓 Qualifikationen"]
  B --> C["⚙️ System settings"]
  C --> D["💾 Backup destinations"]
  D --> E["🔒 DSGVO"]
```

<div style={{display:'flex', gap:'1rem', flexWrap:'wrap'}}>
  <figure className="gear-shot gear-story-shot gear-shot-frame-desktop" style={{maxWidth:'560px', flex:'1 1 45%'}}>
    <img src="/gear/img/screenshots/screenshot-2-admin-users-desktop.png" alt="The admin users list" loading="lazy" />
    <figcaption>Admin — user directory with roles.</figcaption>
  </figure>
  <figure className="gear-shot gear-story-shot gear-shot-frame-desktop" style={{maxWidth:'560px', flex:'1 1 45%'}}>
    <img src="/gear/img/screenshots/screenshot-2-roles-desktop.png" alt="The roles and permissions editor" loading="lazy" />
    <figcaption>Admin — roles and permissions.</figcaption>
  </figure>
</div>

<div style={{display:'flex', gap:'1rem', flexWrap:'wrap'}}>
  <figure className="gear-shot gear-story-shot gear-shot-frame-desktop" style={{maxWidth:'560px', flex:'1 1 45%'}}>
    <img src="/gear/img/screenshots/screenshot-2-qualifications-desktop.png" alt="The qualifications list" loading="lazy" />
    <figcaption>Admin — qualifications.</figcaption>
  </figure>
  <figure className="gear-shot gear-story-shot gear-shot-frame-desktop" style={{maxWidth:'560px', flex:'1 1 45%'}}>
    <img src="/gear/img/screenshots/screenshot-3-settings-desktop.png" alt="The system settings" loading="lazy" />
    <figcaption>Admin — configurable system settings.</figcaption>
  </figure>
</div>

---

## 🆕 Story 6 — A new volunteer registers and waits for approval

**Tim (Helfende)** wants to help with the equipment. He registers himself — and
his account stays **pending** until an administrator approves it. Only then can
he log in.

<div className="gear-story-step">
```mermaid
flowchart LR
  A["📝 Register<br/>with email + password"] --> B["⏳ Pending approval"]
  B --> C{"Admin approves?"}
  C -- "Yes" --> D["🔓 Can log in"]
  C -- "No" --> E["❌ Registration rejected"]
  D --> F["✅ Roles + qualifications assigned"]
```
</div>

<figure className="gear-shot gear-story-shot gear-shot-frame-desktop">
  <img src="/gear/img/screenshots/screenshot-1-login-desktop.png" alt="The login screen — the entry point after approval" loading="lazy" />
  <figcaption>After approval, Tim logs in with his email and password.</figcaption>
</figure>

---

## 🔐 Story 7 — Logging in with two-factor authentication

**Sara (Schirrmeister)** enabled **OTP** on her account. Every login asks for the
current code from her authenticator app after the password — a stolen password
alone is not enough.

<div className="gear-story-step">
```mermaid
flowchart LR
  A["🔑 Email + password"] --> B["✅ Credentials valid"]
  B --> C{"OTP enabled?"}
  C -- "Yes" --> D["📱 Ask for TOTP code"]
  C -- "No" --> E["🔓 Straight to dashboard"]
  D --> F{"Code correct?"}
  F -- "Yes" --> E
  F -- "No" --> G["⛔ Denied — try again"]
```
</div>

<figure className="gear-shot gear-story-shot gear-shot-frame-desktop">
  <img src="/gear/img/screenshots/screenshot-1-login-desktop.png" alt="The login screen — password first, then the OTP code" loading="lazy" />
  <figcaption>Login: password first, then the current OTP code.</figcaption>
</figure>

---

## 🚫 Story 8 — Locked out? Recover with a reset link

**Ben (Helfende)** mistyped his password a few times and is now **temporarily
locked out**. Instead of waiting, he uses *Forgot password* — the app sends a
secure, single-use reset link to his email.

<div className="gear-story-step">
```mermaid
flowchart LR
  A["🔒 Repeated failed logins"] --> B["⏱️ Progressive lockout"]
  B --> C{"Wait it out<br/>or reset?"}
  C -- "Wait" --> D["🔓 Unlocks after the window"]
  C -- "Reset" --> E["📧 Secure single-use<br/>reset link"]
  E --> F["🔑 Set a new password"]
  F --> G["✅ Logged back in"]
```
</div>

<figure className="gear-shot gear-story-shot gear-shot-frame-desktop">
  <img src="/gear/img/screenshots/screenshot-1-login-desktop.png" alt="The login screen with the forgot-password path" loading="lazy" />
  <figcaption>Forgot password — the recovery entry on the login screen.</figcaption>
</figure>

---

## 🔑 Story 9 — Admin unlocks an account with a one-time password

**Anna (Admin)** needs to force a password change for a user. She issues a
**one-time password** from the admin panel; the user signs in with it and must
immediately set a new password.

<div className="gear-story-step">
```mermaid
flowchart LR
  A["👥 Admin picks the user"] --> B["🔑 Issue one-time password"]
  B --> C["📧 OTP delivered to the user"]
  C --> D["🔐 User logs in with OTP"]
  D --> E["⚠️ Must change password"]
  E --> F["✅ New password set"]
```
</div>

<figure className="gear-shot gear-story-shot gear-shot-frame-desktop">
  <img src="/gear/img/screenshots/screenshot-2-admin-users-desktop.png" alt="The admin user directory — where OTPs are issued" loading="lazy" />
  <figcaption>Admin — the user directory where a one-time password is issued.</figcaption>
</figure>

---

## 💾 Story 10 — Backups run automatically and can be restored

**Anna (Admin)** configures backup destinations (local disk + an S3 bucket).
The job runs daily on its own, ships a dated dump to every reachable
destination, and a failed destination never aborts the others. The restore
procedure is tested end-to-end.

<div className="gear-story-step">
```mermaid
flowchart LR
  A["⚙️ Configure destinations"] --> B["🔁 Automated job<br/>daily + on start"]
  B --> C["🗄️ Custom-format dump"]
  C --> D["☁️ Ship to local + S3"]
  D --> E{"Destination OK?"}
  E -- "Yes" --> F["✅ Audited + logged"]
  E -- "No" --> G["⚠️ Logged, others continue"]
  F --> H["♻️ Restore procedure proven"]
```
</div>

<figure className="gear-shot gear-story-shot gear-shot-frame-desktop">
  <img src="/gear/img/screenshots/screenshot-3-backup-desktop.png" alt="The backup destination settings" loading="lazy" />
  <figcaption>Admin — backup destinations with the connection test.</figcaption>
</figure>

---

## 📋 Story 11 — Onboarding the fleet with a CSV import

**Jonas (Schirrmeister)** inherits a long equipment list in a spreadsheet.
Instead of typing each tool, he uploads a **CSV** — the app creates the tools,
auto-assigns inventory numbers, and reports every row that cannot be imported
(e.g. a duplicate inventory number).

<div className="gear-story-step">
```mermaid
flowchart LR
  A["📄 CSV from the old list"] --> B["⬆️ Upload"]
  B --> C["🔍 Validate each row"]
  C --> D["✔️ Tools created<br/>+ inventory numbers"]
  C --> E["⚠️ Problem rows reported"]
  D --> F["📊 Catalogue is ready"]
```
</div>

<figure className="gear-shot gear-story-shot gear-shot-frame-desktop">
  <img src="/gear/img/screenshots/screenshot-4-3-tools-desktop.png" alt="The tool catalogue — the result of a bulk import" loading="lazy" />
  <figcaption>The catalogue after onboarding — tools with inventory numbers.</figcaption>
</figure>

---

## 🕊️ Story 12 — DSGVO: access report and clean deletion

**Anna (Admin)** handles a volunteer's data-protection request: she produces an
**access report** of every personal reference, or **deletes** the account —
rewriting the person's references to "Deleted User" while keeping the equipment
history intact.

<div className="gear-story-step">
```mermaid
flowchart LR
  A["📋 Data-protection request"] --> B{"Auskunft or<br/>Löschen?"}
  B -- "Auskunft" --> C["📄 Access report<br/>of all references"]
  B -- "Löschen" --> D["🗑️ Rewrite references<br/>to 'Deleted User'"]
  D --> E["✅ History preserved"]
  C --> F["🔒 Audited, high severity"]
  E --> F
```
</div>

---

## 🤝 Story 13 — Dual-admin recovery

An admin loses access to their account. Because G.E.A.R. starts with **two
admin accounts**, recovery needs the **other admin's approval** — no single
account can reset the other's credentials alone.

<div className="gear-story-step">
```mermaid
flowchart LR
  A["🔐 Admin A locked out"] --> B["🙋 Request recovery"]
  B --> C{"Admin B approves?"}
  C -- "Yes" --> D["🔑 Credentials recovered"]
  C -- "No" --> E["⛔ Recovery denied"]
  D --> F["✅ System stays protected"]
```
</div>

---

## The inspection cycle at a glance

```mermaid
flowchart TD
  A["Tool enters the catalogue"] --> B["Next inspection date is computed"]
  B --> C{"Is a qualified<br/>person available?"}
  C -- "No" --> D["⚠️ Flag for attention"]
  C -- "Yes" --> E["Qualified volunteer inspects"]
  E --> F{"All checks pass?"}
  F -- "Yes" --> G["🟢 Green — next due date set"]
  F -- "No" --> H["⬛ Out of Service"]
  H --> I["Reinstated by Führende/Admin<br/>after repair or check"]
  I --> B
```

---

## Where the screenshots come from

The screenshots on this page are captured from the **running application** by a
scripted Playwright session
(`docs/scripts/capture-screenshots.js`). They show the real UI, not a design
mock-up. The full screenshot inventory (desktop + mobile, present or missing)
is tracked on the [screenshots page](/docs/screenshots).