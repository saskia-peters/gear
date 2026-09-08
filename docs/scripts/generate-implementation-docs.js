/**
 * G.E.A.R. implementation-docs build-time generator.
 *
 * Reads the Go source under internal/ and emits live, code-derived docs:
 *   - docs/docs/implementation/modules.md      (hexagon module inventory + mermaid)
 *   - docs/docs/implementation/api.md          (endpoint catalog + mermaid route map)
 *   - docs/docs/implementation/flows.md        (mermaid sequence diagrams for key flows)
 *   - docs/openapi/openapi.yaml                (OpenAPI 3.0 spec from the route catalog)
 *
 * It runs on every docs build (npm prebuild/prestart) so the implementation
 * docs always reflect the current code — no manual maintenance. The OpenAPI
 * spec feeds the docusaurus-plugin-openapi-docs API reference (docusaurus
 * openapi:generate).
 *
 * The generator is intentionally dependency-free (Node stdlib only).
 */
const fs = require('fs');
const path = require('path');

const ROOT = path.resolve(__dirname, '../..');
const DOCS_DIR = path.join(__dirname, '..');
const IMPL_OUT = path.join(DOCS_DIR, 'docs/implementation');
const OPENAPI_OUT = path.join(DOCS_DIR, 'openapi');

// ---- hexagon module inventory -------------------------------------------------
// Derived from the internal/ layout. Each module lists its owned concern, the
// in/out ports (inbound Service port + driven repository ports) and adapters.
const MODULES = [
  {
    name: 'user',
    icon: '🪪',
    title: 'User Directory & Auth',
    concern:
      'Owns identity, credentials, sessions, MFA, permissions, qualifications and admin surfaces (AD-2).',
    layers: [
      { layer: 'core', files: 'internal/user/core/*.go', role: 'Domain rules — registration, login, lockout, MFA, reset, permissions, OTP.' },
      { layer: 'ports', files: 'internal/user/ports/*.go', role: 'Inbound Service port + driven Repository port (hexagon boundary, AD-1).' },
      { layer: 'adapters/http', files: 'internal/user/adapters/http/*.go', role: 'REST handlers — auth + admin module (chi).' },
      { layer: 'adapters/postgres', files: 'internal/user/adapters/postgres/*.go', role: 'sqlc-generated PostgreSQL store + repository.' },
    ],
  },
  {
    name: 'admin',
    icon: '🛠️',
    title: 'Admin / Settings',
    concern:
      'Owns SMTP, backup destinations and the schedule catalog; configures other modules through owning-module ports (AD-10/AD-11). Currently a structural seed — concrete interfaces land with Epic 3.',
    layers: [
      { layer: 'core', files: 'internal/admin/core/*.go', role: 'Admin settings domain (structural seed).' },
      { layer: 'ports', files: 'internal/admin/ports/*.go', role: 'Settings ports (SMTP, backup, schedules) — read-only consumers.' },
      { layer: 'adapters', files: 'internal/admin/adapters/*.go', role: 'Adapter seams (structural seed).' },
    ],
  },
  {
    name: 'tools',
    icon: '🧰',
    title: 'Tool Maintenance',
    concern:
      'Equipment catalogue, inspection execution and serviceability. Consumes the User module\'s auth port and the Admin schedule port (AD-2/AD-7/AD-16). Currently a structural seed — Epics 4-5.',
    layers: [
      { layer: 'core', files: 'internal/tools/core/*.go', role: 'Tool/inspection domain (structural seed).' },
      { layer: 'ports', files: 'internal/tools/ports/*.go', role: 'Auth + schedule port consumers (structural seed).' },
      { layer: 'adapters', files: 'internal/tools/adapters/*.go', role: 'Adapter seams (structural seed).' },
    ],
  },
];

const PLATFORM_PKGS = [
  { name: 'auth', role: 'Session validator, permission gateway (RequireAnyPermission), route wrapper.' },
  { name: 'config', role: 'Runtime config from GEAR_* env vars with local-dev defaults.' },
  { name: 'crypto', role: 'Argon2id password hashing, AES-256-GCM secret encryption.' },
  { name: 'health', role: '/healthz probe pinging the pgx pool (200/503 envelope).' },
  { name: 'httpapi', role: 'Uniform JSON error envelope + response helpers.' },
  { name: 'logger', role: 'Shared structured-JSON slog logger (NFR-O1).' },
  { name: 'middleware', role: 'Request logging + panic recovery middleware.' },
  { name: 'router', role: 'chi router assembly: /healthz, auth/protected mounts, JSON 404/405.' },
];

// ---- route catalog (parsed from source, the single source of truth) -----------
// Each entry: { method, path, handler, module, auth }
const ROUTES = [];

function addRoutes(file, base, lines, routerVar) {
  // Match `<routerVar>.Get("/path", h.Handler)` for the given sub-router var
  // (r | users | userGroups | qualifications | groups).
  const re = new RegExp(`\\b${routerVar}\\.(Get|Post|Put|Delete|Patch)\\("([^"]+)",\\s*h\\.([A-Za-z0-9_]+)\\)`, 'g');
  let m;
  while ((m = re.exec(lines)) !== null) {
    const [, method, path, handler] = m;
    ROUTES.push({ file, method: method.toUpperCase(), path: base + path, handler, module: 'user' });
  }
}

// Auth routes (handler.go).
addRoutes('internal/user/adapters/http/handler.go', '/api/v1/auth', fs.readFileSync(path.join(ROOT, 'internal/user/adapters/http/handler.go'), 'utf8'), 'r');

// Admin routes (admin.go) — each sub-router parsed once against its own mount base.
const adminSrc = fs.readFileSync(path.join(ROOT, 'internal/user/adapters/http/admin.go'), 'utf8');
addRoutes('internal/user/adapters/http/admin.go', '/api/v1/admin', adminSrc, 'r'); // recovery + adminStatus
addRoutes('internal/user/adapters/http/admin.go', '/api/v1/admin/users', adminSrc, 'users');
addRoutes('internal/user/adapters/http/admin.go', '/api/v1/admin/user-groups', adminSrc, 'userGroups');
addRoutes('internal/user/adapters/http/admin.go', '/api/v1/admin/qualifications', adminSrc, 'qualifications');
addRoutes('internal/user/adapters/http/admin.go', '/api/v1/admin/groups', adminSrc, 'groups');

// Manual entries not captured by regex (protected demo route + healthz).
ROUTES.push(
  { file: 'cmd/server/main.go', method: 'GET', path: '/api/v1/protected/me', handler: 'ProtectedMe', module: 'user' },
  { file: 'internal/platform/router/router.go', method: 'GET', path: '/healthz', handler: 'Health', module: 'platform' },
);

// ---- helpers ------------------------------------------------------------------
function hexagonMermaid(module) {
  const id = module.name;
  const layerIds = module.layers.map((l) => `${id}-${l.layer.split('/').join('')}`);
  const lines = [];
  lines.push(`flowchart LR`);
  lines.push(`  subgraph ${id}["${module.icon} ${module.title} hexagon"]`);
  module.layers.forEach((l, i) => {
    lines.push(`    ${layerIds[i]}["${l.layer}"]`);
  });
  for (let i = 0; i < layerIds.length - 1; i++) {
    lines.push(`    ${layerIds[i]} --> ${layerIds[i + 1]}`);
  }
  lines.push(`  end`);
  lines.push(`  Style${id}[${module.icon}] -->|"consumes port"| ${id}`);
  return lines.join('\n');
}

function routeGroupMermaid() {
  const lines = ['flowchart LR'];
  const groups = {};
  for (const r of ROUTES) {
    const prefix = r.path.split('/').slice(0, 4).join('/'); // /api/v1/{area}
    (groups[prefix] ??= []).push(r);
  }
  for (const [prefix, rs] of Object.entries(groups)) {
    const gid = 'g' + prefix.replace(/[^a-zA-Z0-9]/g, '');
    lines.push(`  subgraph ${gid}["${prefix}"]`);
    for (const r of rs) {
      const nid = 'n' + r.method + r.path.replace(/[^a-zA-Z0-9]/g, '');
      lines.push(`    ${nid}["${r.method} ${r.path}"]`);
    }
    lines.push(`  end`);
  }
  return lines.join('\n');
}

// ---- markdown emitters ----------------------------------------------------------
function modulesMarkdown() {
  const md = `---
sidebar_position: 1
---

# Hexagon Modules

> Auto-generated from \`internal/\` on every docs build — no manual maintenance.

G.E.A.R. is a **modular monolith with hexagonal architecture** (AD-1): every
feature module is an isolated Go package exposing only port interfaces across
its boundary. The platform layer provides cross-cutting infrastructure; it
never contains business logic.

## Module overview

\`\`\`mermaid
flowchart LR
  U["🪪 User Directory & Auth hexagon"]
  A["🛠️ Admin / Settings hexagon"]
  T["🧰 Tool Maintenance hexagon"]
  P["🧱 Platform (cross-cutting infrastructure)"]
  U <-->|"auth port"| P
  T -->|"auth port"| P
  A -->|"settings ports"| P
  T -->|"schedule port"| A
\`\`\`

## Modules

${MODULES.map((mod) => {
  return `### ${mod.icon} ${mod.title} (\`internal/${mod.name}\`)

${mod.concern}

\`\`\`mermaid
${hexagonMermaid(mod)}
\`\`\`

| Layer | Path | Role |
| --- | --- | --- |
${mod.layers.map((l) => `| \`${l.layer}\` | \`${l.files}\` | ${l.role} |`).join('\n')}

`;
}).join('')}

## Platform packages

\`internal/platform\` is cross-cutting infrastructure — never business logic.

| Package | Purpose |
| --- | --- |
${PLATFORM_PKGS.map((p) => `| \`${p.name}\` | ${p.role} |`).join('\n')}
`;
  return md;
}

function apiMarkdown() {
  const rows = ROUTES
    .map((r) => `| \`${r.method}\` | \`${r.path}\` | \`${r.handler}\` |`)
    .sort((a, b) => a.localeCompare(b));
  const md = `---
sidebar_position: 2
---

# API Endpoints

> Auto-generated from the route registrations on every docs build.

Every route below is registered in the chi router (\`internal/user/adapters/http\`
+ \`internal/platform/router\`). The auth module mounts at \`/api/v1/auth\`, the
admin module at \`/api/v1/admin\`, and \`/healthz\` probes the database. Full
request/response schemas are in the [OpenAPI reference](/docs/api/g-e-a-r-api).

## Route map

\`\`\`mermaid
${routeGroupMermaid()}
\`\`\`

## Endpoint catalog

| Method | Path | Handler |
| --- | --- | --- |
${rows.join('\n')}
`;
  return md;
}

function flowsMarkdown() {
  const md = `---
sidebar_position: 3
---

# Flows (sequence diagrams)

> Auto-generated on every docs build.

## Registration & approval

\`\`\`mermaid
sequenceDiagram
  autonumber
  participant V as Volunteer (SPA)
  participant A as Auth API /api/v1/auth
  participant C as User core
  participant D as PostgreSQL
  participant P as Pending approvals page
  V->>A: POST /register
  A->>C: Register(input)
  C->>D: INSERT user (state=pending_approval)
  C-->>A: anti-enumeration confirmation
  A-->>V: 201 confirmation
  P->>A: GET /users/pending (users.approve)
  A->>C: ListPendingUsers()
  P->>A: POST /users/{id}/approve
  A->>C: ApproveUser()
  C->>D: UPDATE state=active + seed helfende
  C-->>P: German confirmation
\`\`\`

## Login (with forced password change fallback)

\`\`\`mermaid
sequenceDiagram
  autonumber
  participant V as Volunteer (SPA)
  participant A as Auth API /api/v1/auth
  participant C as User core
  participant D as PostgreSQL
  V->>A: POST /login {email, password[, totp_code]}
  A->>C: Login(input)
  C->>D: GetUserByEmail + lockout check
  alt MFA enabled
    C-->>A: {mfa_required:true}
    A-->>V: 200 MFA challenge
    V->>A: POST /login {email, password, totp_code}
    C->>C: verify TOTP
  end
  alt must_change_password OR one-time-password
    C->>D: mint single-use reset token
    C-->>A: {must_change_password:true, reset_token}
    A-->>V: 200 forced-change flow
  else normal login
    C->>D: INSERT session
    C-->>A: {token, user}
    A-->>V: 200 session token
  end
\`\`\`

## Admin one-time-password (OTP) issuance

\`\`\`mermaid
sequenceDiagram
  autonumber
  participant Adm as Admin (SPA)
  participant API as Admin API /api/v1/admin
  participant C as User core
  participant D as PostgreSQL
  Adm->>API: POST /users/{id}/otp {confirmed:true}
  API->>C: IssueOneTimePassword(actor, id)
  C->>D: GetUserByID (active-only)
  C->>C: generate + Argon2id hash
  C->>D: UPDATE one_time_password_hash + must_change_password
  C-->>API: {one_time_password (once), expires_at}
  API-->>Adm: 200 plaintext OTP (out-of-band handover)
\`\`\`

## Qualification assignment (user detail)

\`\`\`mermaid
sequenceDiagram
  autonumber
  participant U as User (SPA)
  participant API as Admin API /api/v1/admin
  participant C as User core
  participant D as PostgreSQL
  U->>API: POST /users/{id}/qualifications/{qualId}
  API->>C: AssignUserQualification(actor, id, qualId, expiresAt)
  C->>D: GetQualificationExpiryKindByID
  alt fixed validity
    C->>D: INSERT user_qualifications (expires_at required)
  else unlimited
    C->>D: INSERT user_qualifications (no expires_at)
  end
  C-->>API: German confirmation
  API-->>U: 200 message
\`\`\`
`;
  return md;
}

// ---- OpenAPI 3.0 emitter ---------------------------------------------------------
function openapiSpec() {
  const byArea = {};
  for (const r of ROUTES) {
    const area = r.path.startsWith('/api/v1/admin')
      ? 'admin'
      : r.path.startsWith('/api/v1/auth')
        ? 'auth'
        : 'platform';
    (byArea[area] ??= []).push(r);
  }
  const tags = [
    { name: 'auth', description: 'Authentication & user directory (User hexagon).' },
    { name: 'admin', description: 'Administration module (users, groups, roles, qualifications, recovery, OTP).' },
    { name: 'platform', description: 'Infrastructure probes (healthz).' },
  ];
  const paths = {};
  for (const [area, rs] of Object.entries(byArea)) {
    for (const r of rs) {
      const op = {
        tags: [area],
        summary: humanizeHandler(r.handler),
        operationId: `${r.method.toLowerCase()}${r.handler}`,
        responses: {
          200: { description: 'Success' },
          400: { description: 'Invalid request' },
          401: { description: 'Unauthenticated' },
          403: { description: 'Forbidden (missing permission)' },
          404: { description: 'Not found' },
        },
      };
      if (r.path.includes('{') && (r.method === 'POST' || r.method === 'PUT' || r.method === 'PATCH' || r.method === 'DELETE')) {
        op.parameters = (r.path.match(/\{([^}]+)\}/g) || []).map((p) => ({
          name: p.slice(1, -1),
          in: 'path',
          required: true,
          schema: { type: 'string' },
        }));
      }
      paths[r.path] = { ...(paths[r.path] ?? {}), [r.method.toLowerCase()]: op };
    }
  }
  return {
    openapi: '3.0.3',
    info: {
      title: 'G.E.A.R. API',
      version: '1.0.0',
      description:
        'Auto-generated OpenAPI spec from the route catalog (docs/scripts/generate-implementation-docs.js).',
    },
    servers: [{ url: 'http://localhost:8080' }],
    tags,
    paths,
  };
}

function humanizeHandler(h) {
  return h
    .replace(/(Handler|Admin|User|Group|Qualification|Role|Recovery|Approval|Password|Profile|MFA|Totp)/g, ' $1')
    .replace(/([a-z])([A-Z])/g, '$1 $2')
    .replace(/^ /, '')
    .replace(/\b\w/g, (c) => c.toUpperCase());
}

// ---- main ----------------------------------------------------------------------
function main() {
  fs.mkdirSync(IMPL_OUT, { recursive: true });
  fs.mkdirSync(OPENAPI_OUT, { recursive: true });

  fs.writeFileSync(path.join(IMPL_OUT, 'modules.md'), modulesMarkdown());
  fs.writeFileSync(path.join(IMPL_OUT, 'api.md'), apiMarkdown());
  fs.writeFileSync(path.join(IMPL_OUT, 'flows.md'), flowsMarkdown());
  fs.writeFileSync(path.join(IMPL_OUT, '_category_.json'), JSON.stringify({ label: 'Implementation', position: 2 }, null, 2));
  fs.writeFileSync(path.join(OPENAPI_OUT, 'openapi.yaml'), yamlDump(openapiSpec()));

  console.log(`implementation docs: ${MODULES.length} modules, ${ROUTES.length} routes, ${Object.keys(openapiSpec().paths).length} paths, openapi.yaml written.`);
}

// Minimal YAML emitter (stdlib only) — sufficient for the OpenAPI 3.0 structure.
function yamlDump(obj, indent = 0) {
  const pad = '  '.repeat(indent);
  const lines = [];
  for (const [k, v] of Object.entries(obj)) {
    if (Array.isArray(v)) {
      if (v.length === 0) { lines.push(`${pad}${k}: []`); continue; }
      lines.push(`${pad}${k}:`);
      for (const item of v) {
        if (item && typeof item === 'object' && !Array.isArray(item)) {
          lines.push(`${pad}  - ${Object.keys(item)[0]}: ${JSON.stringify(Object.values(item)[0])}`);
          // nested objects (rare here) fall back to inline
        } else {
          lines.push(`${pad}  - ${yamlScalar(item)}`);
        }
      }
    } else if (v && typeof v === 'object') {
      lines.push(`${pad}${k}:`);
      lines.push(yamlDump(v, indent + 1).replace(/\n$/, ''));
    } else {
      lines.push(`${pad}${k}: ${yamlScalar(v)}`);
    }
  }
  return lines.join('\n') + '\n';
}

function yamlScalar(v) {
  if (typeof v === 'boolean') return v ? 'true' : 'false';
  if (typeof v === 'number') return String(v);
  const s = String(v);
  if (/[:#\n]|^\s|\s$|^['"]/.test(s) || s === '' || s !== s.trim()) {
    return `'${s.replace(/'/g, "''")}'`;
  }
  return s;
}

main();