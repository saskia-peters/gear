---
sidebar_position: 10
title: Deploying the App — from a Local Registry to IONOS
description: A plain-language, step-by-step look at how the G.E.A.R. app is built into a single container, stored in a registry, and deployed — and why IONOS is a great fit.
---

# Deploying the App — from a Local Registry to IONOS

**In short: G.E.A.R. is built once into a single, self-contained "container image", stored in a registry (a place that holds images), and the server simply downloads and starts that image. It's the same process on your laptop, on Google Cloud, and on IONOS — which is exactly why it fits IONOS so well.**

This page is written for decision-makers and people who are not deeply technical. It explains:

1. what a container image is and why it makes deployment simple,
2. how we build and store the image in a **local registry** (so we can prove everything works before touching any cloud),
3. how the same image is later pushed to IONOS, and
4. why IONOS is the right home for it.

If you want the exact commands, they are at the bottom.

---

## 1. What is a "container image" anyway?

Think of a container image as a **sealed moving box** that contains the whole app:

- the G.E.A.R. program (the "server"),
- the website that users see (the "frontend"),
- the database engine,
- and every configuration it needs to run.

Because everything is in one box, it behaves **identically everywhere**: on a developer's laptop, on a rented server, or in a cloud. No "it works on my machine" surprises. This is the industry-standard way apps are shipped today.

```mermaid
flowchart LR
    subgraph TheBox["📦 The app as one container image"]
        A[🖥️ G.E.A.R. server] --> C[🌐 Website the users see]
        C --> D[🗄️ Database engine]
    end
```

**Why it matters for the Ortsverband:** once the box is built, we never rebuild it per location. We build it once, store it, and every server just unpacks and runs the same box.

---

## 2. The registry: a garage for container images

A **registry** is simply the place where container images are stored, like a garage or a warehouse for the sealed boxes. You can push an image into it and pull it back out later.

There are public registries and **private** ones. We use a private one — our images are only visible to us, protected by login.

```mermaid
flowchart LR
    B["🧑‍💻 Build the box (once)"] -->|push / store| R["🏭 Registry (image garage)"]
    S1["🖥️ Server 1"] -->|pull / download| R
    S2["🖥️ Server 2"] -->|pull / download| R
```

**The important insight:** Google Cloud's registry, IONOS's registry, and a registry running on your own laptop all speak the **same language** (the "Docker Registry v2" standard). That means a box pushed into our local registry can be pushed to IONOS later with the *same command, just a different address*.

---

## 3. Proving it works locally first (zero cloud cost)

Before we spend a single euro on a cloud provider, we prove the whole process on a normal laptop — for free. We run a small **local registry** (a little box that holds images), build the G.E.A.R. image, and store it there.

```mermaid
flowchart LR
    A["🖥️ Developer laptop"] -->|1. build image| B["📦 G.E.A.R. image"]
    B -->|2. store| C["🏭 Local registry (localhost:5000)"]
    C -->|3. download| D["🖥️ A test server (also on the laptop)"]
    D -->|4. open website| E["✅ App is running"]
```

This proves, end-to-end, that:

- the image builds cleanly,
- it can be stored and retrieved from a registry,
- a server can download and run it, and
- the app (including the login screen and the dashboard) works from the downloaded image.

**If it works here, it will work on IONOS** — because the process is identical.

---

## 4. The same image on IONOS

When we are ready to go live, we do exactly the same thing on IONOS:

```mermaid
flowchart LR
    A["🏭 Local registry (proof)"] -->|same command, new address| B["🏭 IONOS Private Container Registry"]
    B -->|download| C["🖥️ IONOS server (smallest one fits)"]
    C -->|open website| D["✅ G.E.A.R. live on IONOS"]
```

### Why IONOS fits well

- **IONOS has its own private container registry.** It is fully managed (IONOS runs it for us), supports the same standard we already use, and costs only about **$0.05 per GB per month** — for our small app that is roughly **one or two cents a month**.
- **The smallest IONOS server is enough.** Our deployment only *downloads* and runs the image (it never builds it), which is light on memory. The smallest option — **Basic Cube XS: 1 processor, 2 GB RAM, 60 GB disk, about $5.76 per month** — fits a single Ortsverband's workload comfortably. There is a larger option (4 GB RAM) if we ever want more headroom.
- **No lock-in.** Because the app is a standard container image, it is not tied to IONOS. If security or cost ever pointed elsewhere (Google, another provider, or a self-hosted server), the same image runs there with minimal work. This is a deliberate design choice: the app itself knows nothing about the cloud it runs on.
- **Security by default:** the database never exposes itself to the internet, and passwords are stored only on the server itself (not in the cloud or in the code). The same rules apply on any provider.

> **Bottom line for decision-makers:** we prove it locally, then run the identical, already-tested process on IONOS — on the cheapest server, with a registry that costs almost nothing, and no vendor lock-in.

---

## 5. Step by step on a small IONOS server (with the IONOS registry)

Here is exactly how the pieces fit together once we move to IONOS. The story has two halves: **getting the image into the IONOS registry** (done once, from any machine) and **setting up the small server** (done once, on IONOS).

```mermaid
flowchart LR
    subgraph Prepare["Wherever you are"]
        A["🧑‍💻 Build + push to IONOS registry"] --> B["🏭 IONOS Private Container Registry"]
    end
    subgraph IONOS["On IONOS"]
        C["🖥️ Small server (Basic Cube XS)"] -->|first boot: pull image| B
        C -->|start app + database| D["✅ G.E.A.R. live"]
        D --> E["🔐 Database stays private inside the server"]
    end
```

### Step 1 — Create the IONOS registry and a login token

In the IONOS cloud console you create a **Private Container Registry** (a few clicks; it costs about **$0.05 per GB per month**). IONOS gives it an address (a domain name). You then create a **token** (a special password) that lets us log in and push images to it. IONOS can also create a **robot account / one-time token** for automated use — the same mechanism CI/CD pipelines use.

### Step 2 — Push the image to the IONOS registry (once)

On any machine with the image (e.g. the laptop where we proved it), we log in and push the *same* image we already tested locally. Only the address changes — the image and the command are identical to the local proof:

```bash
# 1. Log in to the IONOS registry with the token
podman login <ionos-registry-address>

# 2. Give the image the IONOS registry's address
podman tag gear-app <ionos-registry-address>/gear:latest

# 3. Push (store) it there
podman push <ionos-registry-address>/gear:latest
```

> Note: IONOS's registry is **HTTPS-secured**, so no `--tls-verify=false` here (that flag is only for the local proof registry on your laptop).

### Step 3 — Create the small server

In the IONOS cloud console (or via their API / Data Center Designer) we create a **Basic Cube XS** server: **1 processor, 2 GB RAM, 60 GB disk, about $5.76 per month**. We give it a secure SSH key so only we can log in. The server runs a normal Linux operating system — there is nothing IONOS-specific about what runs inside it.

### Step 4 — First boot: download the app and start it

We copy the small deployment folder (`compose.prod.yaml` + `startup.sh`) onto the server, log the server into the IONOS registry so it may *pull* the image, and run the first-boot script. The script:

1. generates a **secret password** for the database (stored only on the server, permissions set so nobody else can read it),
2. **downloads the app image** from the IONOS registry,
3. starts the app and its private database,
4. waits until the app answers "I am healthy".

```bash
# On the IONOS server (once)
cd /opt/gear
GEAR_IMAGE_REPO=<ionos-registry-address>/gear:latest \
GEAR_APP_ORIGIN=https://gear.example.org \
bash deploy/startup.sh
```

### Step 5 — Everyone opens the website

After the script finishes, the app is reachable at its public address. The database is **not** exposed to the internet — only the app talks to it, and only the app is reachable from outside.

```mermaid
flowchart LR
    U["👥 Users (browser)"] -->|HTTPS via edge/CDN later| A["🖥️ App container"]
    A -->|internal only| D["🗄️ Database container (private)"]
    A -->|pulls image once| R["🏭 IONOS registry"]
```

**What the server does NOT do:** it never builds the app, never compiles code, and needs no development tools — it only downloads and runs a ready-made image. That is why the smallest server is enough.

### Updating the app later

When we release a new version, we simply:

1. build the new image and push it to the IONOS registry (Step 2),
2. on the server: `docker compose -f deploy/compose.prod.yaml pull && docker compose -f deploy/compose.prod.yaml up -d`.

The database stays untouched; only the app is replaced.

### Backup & Restore runbook (Story 7.7, NFR-R3)

G.E.A.R. backs itself up. The app container ships the postgres client
(`pg_dump`), and an in-process **backup job** runs automatically:

- **Schedule:** once a few seconds after boot, then every `backup_interval`
  seconds. The interval is admin-configurable under Einstellungen → System
  (seeded `86400` = daily).
- **What it produces:** a compressed, restorable dump of the whole database
  (`pg_dump -Fc`, custom format), shipped as a **dated artifact**
  `gear-<YYYYMMDDHHMMSS>.dump` to **every configured backup destination**
  (Einstellungen → Backup) whose mechanism supports real transfer in V1:
  - **local** → a dated file in the configured path,
  - **s3** → a dated object via SigV4 PUT,
  - **ftp / sftp** → reachability-tested only (handshake) in V1; the job logs
    them as "configured but not shippable in V1" — never silent, not a failure.
- **Failures are never silent (NFR-R3):** every run and every destination's
  outcome is logged structured and written to the audit trail (`backup.run`).
  One failing destination never stops the others.
- **Restore:** `deploy/restore.sh` restores a dump into a target database with
  `pg_restore --clean --if-exists` (idempotent). The local proof — dump the dev
  DB, restore into a throwaway scratch database, verify the two seeded admins,
  drop the scratch — is one command:

```bash
just backup-restore-proof
```

Manual restore after a disaster (run on a machine with the postgres client
tools, e.g. an operator laptop or the postgres container itself):

```bash
# stop the app so no live session writes during the restore
docker compose -f deploy/compose.prod.yaml stop app
bash deploy/restore.sh /path/to/gear-20260921120000.dump \
  'postgres://gear:...@db:5432/gear?sslmode=disable'
docker compose -f deploy/compose.prod.yaml start app
```

The two seeded admin accounts are part of the dump and are restored as-is
(AD-13). Retention/rotation of old dumps is explicitly out of scope for V1.

---

## 6. Putting it on the internet: bunny.net or Cloudflare (with sassisuperdomain.de)

The small IONOS server gives us the running app, but for a professional, trustworthy website we put a **"front door"** in front of it. A service such as **bunny.net** or **Cloudflare** sits between the visitors and our server and provides three things the app alone does not:

- **TLS / HTTPS** — the padlock in the browser. All traffic between the visitor and the app is encrypted (NFR-S1: TLS 1.2 or higher).
- **Caching** — static parts of the site (the G.E.A.R. website files) are stored "close" to the visitor, so pages load faster and the small server does less work.
- **Protection** — basic DDoS and abuse filtering, so a malicious flood of requests does not knock the app over.

We already own the domain **`sassisuperdomain.de`** — this section explains exactly what is needed to point it at the app through such a service.

```mermaid
flowchart LR
    U["🌍 Visitors"] -->|https://gear.sassisuperdomain.de| E["🛡️ Edge / CDN (bunny.net or Cloudflare)"]
    E -->|HTTPS| A["🖥️ IONOS server — app"]
    A -->|internal only| D["🗄️ Database (private)"]
    E -->|DNS| R["🌐 sassisuperdomain.de DNS"]
```

### What we need regardless of provider

1. **A subdomain** — e.g. `gear.sassisuperdomain.de` — so the app lives at a clean address and the edge/CDN can be pointed at it.
2. **Access to the domain's DNS** — the registrar (or a DNS provider) must allow us to add the records the edge service gives us. Because we own `sassisuperdomain.de`, this is under our control.
3. **The app's public address** — the IONOS server's IP (and port 8080 by default). The edge service forwards traffic to this address.
4. **The app must know its own public address** — so password-reset links point to the real site. We set `GEAR_APP_ORIGIN=https://gear.sassisuperdomain.de` on the server (`deploy/startup.sh` reads it). This is already built into the deployment.

### With bunny.net

bunny.net is a simple, affordable CDN with a built-in "Pull Zone". The steps:

1. **Create an account and a Pull Zone** — in the bunny.net console, create a **Pull Zone** for `gear.sassisuperdomain.de` with the **Origin URL** set to the IONOS server (`http://<ionos-ip>:8080`). This tells bunny.net where to fetch the app.
2. **Get a TLS certificate** — bunny.net issues a free Let's Encrypt certificate automatically for the zone.
3. **Add a CNAME record in DNS** — in `sassisuperdomain.de`'s DNS, add a CNAME from `gear` to the Pull Zone address bunny.net shows (e.g. `gear.b-cdn.net`). bunny.net's console gives you the exact record to create.
4. **Done** — within minutes `https://gear.sassisuperdomain.de` shows the app, encrypted and cached.

### With Cloudflare

Cloudflare is a very widely used, free-forever edge. The steps:

1. **Add the domain to a Cloudflare account** — enter `sassisuperdomain.de`; Cloudflare scans and imports the existing DNS records.
2. **Switch the domain's name servers** — the registrar for `sassisuperdomain.de` must point the domain's name servers to the two Cloudflare name servers Cloudflare shows (this is the one step that touches the registrar). After that, Cloudflare manages DNS.
3. **Add a DNS record** — an `A` record for `gear` pointing to the IONOS server's IP (proxied = the orange cloud icon on, so traffic goes through Cloudflare).
4. **Enable HTTPS** — Cloudflare's "Flexible/Full" SSL setting issues a free certificate; with **Full** it also encrypts the hop between Cloudflare and the IONOS server.
5. **Done** — `https://gear.sassisuperdomain.de` is served through Cloudflare with TLS, caching, and protection.

### What changes on our side

- The IONOS server keeps listening on its port; the edge service fronts it.
- `GEAR_APP_ORIGIN` on the server is set to `https://gear.sassisuperdomain.de` (already supported by `deploy/startup.sh`).
- The firewall on the IONOS server can be tightened later to only accept traffic from the edge provider's IP ranges — a hardening step we can do after the edge is live.

### Hosting state, data residency, and DSGVO / GDPR

G.E.A.R. stores personal data of the Ortsverband's volunteers (names, email addresses, qualifications, inspection records). The **DSGVO (GDPR)** applies — the Ortsverband is the **data controller** and any service that touches personal data is a **data processor**. Choosing an edge/CDN therefore has a legal dimension: where the data physically stays, which law applies, and what the provider signs.

| Fact | bunny.net | Cloudflare |
|---|---|---|
| **Company / applicable law** | **EU-based** — BunnyWay d.o.o., Ljubljana (Slovenia, EU). Subject to EU/GDPR + Slovenian law. Explicitly avoids U.S. cloud giants and states it does not fall under the U.S. CLOUD Act. | **U.S.-based** — Cloudflare, Inc. (US). US law applies to the company; GDPR is honoured contractually. Relies on EU SCCs + EU-U.S. Data Privacy Framework for transfers to the US. |
| **Where the traffic is processed** | Default is a global CDN; can be restricted to **EU-only** with one click (Routing Filters → 24 EU PoPs). Logs are stored in **Germany** and IP-anonymised by default. | Global network; EU-only requires the **Data Localization Suite** (Regional Services / Geo Key Manager), which is largely an **enterprise/paid** feature — the free tier may inspect traffic outside the EU. |
| **Certifications** | ISO/IEC 27001:2022, GDPR-compliant, DPA signable in the dashboard. | ISO 27001, ISO 27701, ISO 27018, SOC 2 Type II, PCI DSS, European Cloud Code of Conduct, Germany's **C5** (BSI) standard. |
| **DSGVO posture for a German controller** | Strongest fit: EU company, EU-only routing available, EU (DE) log storage, no US jurisdiction exposure. | Good, but US-based: compliance depends on contractual safeguards (SCCs/DPF) and paid EU-localisation features to keep data in the EU. |

**Bottom line for DSGVO/GDPR:** both are GDPR-compliant, but for a German Ortsverband the **practical risk is lower with bunny.net** — an EU company with EU-only routing and EU log storage, no U.S. jurisdiction exposure. With Cloudflare, keeping data in the EU on the free tier is not guaranteed (the EU-localisation features are paid), so a German controller would need the paid tier or accept SCC/DPF-based transfers. This is a **decision-maker question**, not a technical one.

### Is the traffic between the provider and IONOS encrypted?

Yes — **but only if configured correctly.** There are two separate hops, and they can be encrypted independently:

1. **Visitor → provider (browser → edge):** always HTTPS (TLS 1.2+). This is the padlock users see and is the default with either provider.
2. **Provider → IONOS server (edge → origin):** this hop is **only encrypted if the IONOS server itself serves HTTPS**.

Because our server currently listens on **plain HTTP** (port 8080, `GEAR_HTTP_ADDR=:8080`), a naive setup leaves the **provider ↔ IONOS hop unencrypted** — the visitor's browser is safe, but the traffic between the CDN and our server travels in clear text. The data (including login and inspection content) passes through the provider's network, so that hop should be encrypted too.

**What makes it encrypted end-to-end:**

- **bunny.net:** configure the Pull Zone's **Origin SSL** to HTTPS and serve HTTPS on the IONOS server (a Let's Encrypt certificate for `gear.sassisuperdomain.de` installed on the IONOS server). bunny.net then fetches the origin over TLS.
- **Cloudflare:** use SSL mode **Full (strict)** — Cloudflare talks HTTPS to the origin and requires a valid origin certificate. (The **Flexible** mode is the trap: it keeps the browser→Cloudflare hop encrypted but talks **plain HTTP** to the origin — the opposite of what we want.)

**What we need to implement for a fully encrypted path** (in the deploy story, not yet built):

- Serve HTTPS **on the IONOS server itself** — either a small TLS reverse proxy in front of the app (e.g. a Caddy/Traefik/nginx container) or have the Go server terminate TLS directly. The app currently listens on plain HTTP only.
- Issue a certificate for `gear.sassisuperdomain.de` (Let's Encrypt via the edge's origin-certificate feature, or on the IONOS server via HTTP-01/DNS-01).
- Keep the app→database hop internal-only (already the case: postgres is never published to the host).

Until that TLS-on-origin step lands, the edge would have to use plain-HTTP origin fetch — acceptable only for a trial, not for real personal data.

```mermaid
flowchart LR
    U["🌍 Visitor"] -->|1. HTTPS (encrypted)| E["🛡️ Edge/CDN"]
    E -->|2. HTTPS (encrypted) IF origin serves TLS| A["🖥️ IONOS app"]
    A -->|3. internal only (encrypted in-compose)| D["🗄️ Database"]
    style E fill:#eef
```

### Which to choose

| | bunny.net | Cloudflare |
|---|---|---|
| **Cost** | Cheap, pay-as-you-go CDN | Generous free tier |
| **Setup** | Pull Zone + one DNS CNAME | Add domain + change name servers (touches the registrar once) |
| **Caching / CDN** | Excellent, purpose-built | Excellent, plus WAF / bot protection |
| **Applicable law / company** | EU (Slovenia) — no US jurisdiction exposure | US (California) — EU compliance via SCCs/DPF |
| **EU data residency** | **EU-only routing + EU (DE) logs by default/one click** | EU-only needs the paid Data Localization Suite |
| **Best for** | Simple, low-cost, EU-sovereign CDN in front of our server | Maximum protection + a "set and forget" free edge |

Both work with the same deployment — this decision does not affect the app, the registry, or the server; it is purely the "front door" configuration. For a **German Ortsverband under DSGVO**, bunny.net is the simpler choice from a data-residency standpoint; Cloudflare is chosen when the free WAF/protection features outweigh the US-jurisdiction consideration.

---

## 7. The exact commands (for the technically minded)

All of this runs with **Podman** (a free, open-source container tool already installed on the dev machines — no Docker license needed).

### Build the image

```bash
just container-build
```

This builds the whole app — frontend + server — into one image named `gear-app`.

### Start a local registry

```bash
podman run -d --name gear-registry -p 5000:5000 \
  -v gear_registry_data:/var/lib/registry docker.io/library/registry:2 \
  || podman start gear-registry
```

This starts a tiny registry on the laptop at `localhost:5000`.

### Tag and store the image in the registry

```bash
podman tag gear-app localhost:5000/gear:local
podman push --tls-verify=false localhost:5000/gear:local
```

The second line "pushes" (stores) the image into the registry.

### Verify it is stored

```bash
curl -s http://localhost:5000/v2/_catalog
# -> {"repositories":["gear"]}
```

### The one-shot convenience recipe

The whole build + store + run flow is bundled into a single command:

```bash
just deploy-local-proof      # build → push to local registry → run the app from it → check it is healthy
just deploy-local-proof-down # stop everything and clean up
```

> The `--tls-verify=false` flag is only needed because the **local** registry speaks plain HTTP. IONOS's registry (and Google's) use secure HTTPS, so the flag is dropped there.

---

## Where this lives in the project

- `Dockerfile` — the recipe that builds the image.
- `deploy/compose.prod.yaml` — the file that says how the downloaded image runs (app + database, database kept private).
- `deploy/startup.sh` — the "first boot" script a server runs: download the image, start it, wait until it is healthy.
- `infra/` — the scripts that create the server on Google Cloud (a separate, optional path).
- `deploy/README.md` and `infra/README.md` — the detailed operator runbooks (in English, for whoever runs the servers).

---

## More reading

- [Management Overview](/docs/management-overview) — what the app does, in plain language.
- [Module Roadmap](/docs/planning/module-roadmap) — the wider product plans.
- [Architecture Spine](/docs/planning/architecture-spine) — the deep technical background.