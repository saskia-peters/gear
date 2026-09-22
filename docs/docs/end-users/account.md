---
sidebar_position: 5
---

# Your Account

Everything about logging in, your password, and what happens when you are
locked out.

## Login

Log in with your **email** and **password**. If two-factor authentication
(OTP) is enabled on your account, you are asked for the current code after the
password.

## Passwords

- You can **change your own password** at any time (Profile → password).
- **Forgot your password?** The *forgot password* flow sends a secure,
  single-use reset link by email — only for the transactional reset email; the
  app does not send notification emails.
- Passwords are stored **hashed** (Argon2id) — never in plaintext.

## Registration & approval

New volunteers register themselves. Their account is **pending approval** until
an administrator approves it — only then can they log in.

## Lockout

The app progressively locks out after repeated failed logins. After a lockout:

- You must **wait** for the lockout to expire, or
- **Recover via email** (the reset link works for the lockout too), or
- For **admins**: the dual-admin recovery flow — the *other* admin must approve
  the recovery; no single admin can reset the other alone.

## Profile

Your profile holds your **display name** (how others see you), your **email**,
and your **first/last name**. The display name is what appears in inspection
history and reports.

## If your account was deleted (DSGVO)

The admin DSGVO operation anonymizes or deletes your personal references while
preserving the equipment history. After deletion your references read as
"Deleted User".