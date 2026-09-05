# gear
G.E.A.R. — Geräte-Einsatz-Assistenz & Readiness

## Local development

Start the full dev stack (PostgreSQL + API + SPA):

```bash
just dev
```

`just dev` also generates a local `GEAR_ENCRYPTION_KEY` into `.env` (gitignored)
if none exists — this is required for the MFA / TOTP features. To regenerate or
inspect it:

```bash
just dev-key
```

- SPA: http://localhost:5173
- API: http://localhost:8080/healthz

### Opening the dev SPA from a phone / other device on the LAN

`just dev` binds both the API (`:8080`) and Vite (`:5173`, `--host`) to all
interfaces, so the app can be loaded from another device on the same network.

**WSL2 (Option A — port proxy):** the WSL IP (e.g. `172.22.x.x`) is a NAT
virtual address and is not reachable from the LAN directly. Windows must forward
the two ports to WSL. Print the ready-to-paste commands (current WSL IP is
filled in automatically):

```bash
just wsl-portproxy-setup
```

Run the printed `netsh` + firewall commands in an **administrator PowerShell** on
Windows, then open `http://<windows-lan-ip>:5173` from the device. To remove the
forwarding and firewall rules later:

```bash
just wsl-portproxy-teardown
```

> **Alternative (WSL2 mirrored networking, Windows 11 22H2+):** set
> `networkingMode=mirrored` in `%USERPROFILE%\.wslconfig`, run `wsl --shutdown`,
> then restart. WSL then shares the Windows network interfaces, so the dev SPA is
> reachable directly at the Windows LAN IP without a port proxy. Note this
> removes the NAT isolation, so anything binding `0.0.0.0` becomes LAN-visible.