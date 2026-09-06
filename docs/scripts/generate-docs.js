/**
 * G.E.A.R. docs build-time generator.
 *
 * Reads the planning epics and the sprint-status file and emits:
 *   - docs/docs/epics/_index.md         (epic overview card grid)
 *   - docs/docs/epics/epic-{n}-*.md      (one page per epic, story cards)
 *   - docs/docs/epics/_category_.json    (sidebar category)
 *   - docs/docs/status.md                (auto implementation status)
 *   - docs/docs/screenshots.md           (screenshot tracker, present/missing)
 *   - docs/src/data/screenshots.json     (manifest consumed by the tracker)
 *
 * The original planning markdown files are never modified — this script only
 * READS them and WRITES new files under docs/docs/epics/, docs/docs/status.md,
 * docs/docs/screenshots.md, and docs/src/data/. It runs on every docs build
 * (npm prebuild/prestart) so the status and epic pages always reflect the
 * current sprint-status.yaml.
 */
const fs = require('fs');
const path = require('path');

const ROOT = path.resolve(__dirname, '../..');
const EPICS_DIR = path.join(ROOT, '_bmad-output/planning-artifacts/epics');
const STATUS_FILE = path.join(ROOT, '_bmad-output/implementation-artifacts/sprint-status.yaml');
const DOCS_DIR = path.join(__dirname, '..');
const EPICS_OUT = path.join(DOCS_DIR, 'docs/epics');
const SCREENSHOT_MANIFEST_SRC = path.join(DOCS_DIR, 'screenshots/screenshots.yaml');
const SCREENSHOT_MANIFEST_OUT = path.join(DOCS_DIR, 'src/data/screenshots.json');
const SCREENSHOT_DIR = path.join(DOCS_DIR, 'static/screenshots');

const STATUS_LABELS = {
  backlog: 'Backlog',
  'ready-for-dev': 'Bereit',
  'in-progress': 'In Arbeit',
  review: 'Review',
  done: 'Erledigt',
};

// ---- sprint status ---------------------------------------------------------
function readSprintStatus() {
  const raw = fs.readFileSync(STATUS_FILE, 'utf8');
  const status = {};
  let inDev = false;
  for (const line of raw.split('\n')) {
    const t = line.trim();
    if (t === 'development_status:') { inDev = true; continue; }
    if (inDev && t === 'action_items:') break;
    if (inDev) {
      const m = t.match(/^([\w.-]+):\s*(.+)$/);
      if (m) status[m[1]] = m[2].replace(/^['"]|['"]$/g, '');
    }
  }
  return status;
}

// ---- epics -----------------------------------------------------------------
function slugify(s) {
  return s
    .toLowerCase()
    .replace(/[’'()]/g, '')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
}

function parseEpics() {
  const files = fs.readdirSync(EPICS_DIR).filter((f) => /^epic-0\d-.*\.md$/.test(f)).sort();
  const epics = [];
  for (const file of files) {
    const src = fs.readFileSync(path.join(EPICS_DIR, file), 'utf8');
    const epicHeader = src.match(/^## Epic (\d+): (.+)$/m);
    if (!epicHeader) continue;
    const epicNum = epicHeader[1];
    const epicTitle = epicHeader[2].trim();
    const intro = src
      .slice(epicHeader.index + epicHeader[0].length)
      .split(/^### Story \d+\.\d+:/m)[0]
      .trim()
      .split('\n')[0];

    const stories = [];
    const storyBlocks = src.split(/^### Story (\d+\.\d+): (.+)$/m);
    for (let i = 1; i < storyBlocks.length; i += 3) {
      const storyId = storyBlocks[i].trim();
      const storyTitle = storyBlocks[i + 1].trim();
      const body = storyBlocks[i + 2] || '';
      const [a, b, c] = storyId.split('.');
      const key = `${a}-${b}-${slugify(storyTitle)}`;
      // Intent = the "As a / I want / So that" lines up to the first bold heading.
      const intent = (body.match(/As a .*?So that .*?(?=\n\*\*|\n###|\n##)/s) || [])[0]?.trim() || '';
      // Acceptance criteria = the **Given/When/Then/And** blocks from
      // "Acceptance Criteria:" up to the next story heading. Capture the whole
      // section (so **And** lines are not truncated), then keep only the
      // Given/When/Then/And lines and format them into grouped blocks:
      //   **Given** <cond>
      //   - **When** <action>
      //   - **Then** <result>
      //   - **And** <extra>
      const acceptSection =
        (body.match(/\*\*Acceptance Criteria:\*\*\n([\s\S]*?)(?=\n### |\n## |\n---)/) || [])[1] ||
        (body.match(/\*\*Acceptance Criteria:\*\*\n([\s\S]*)$/) || [])[1] ||
        '';
      const acceptLines = acceptSection
        .split('\n')
        .map((l) => l.trim())
        .filter((l) => /^\*\*(Given|When|Then|And)\*\*/.test(l));
      const accept = acceptLines
        .map((l, i) => {
          const m = l.match(/^\*\*(Given|When|Then|And)\*\*\s*(.*)$/);
          const kind = m[1];
          const rest = m[2] || '';
          if (kind === 'Given') {
            // A blank line separates Given-blocks (when not the first).
            return (i === 0 ? '' : '\n') + `**Given** ${rest}`;
          }
          return `- **${kind}** ${rest}`;
        })
        .join('\n');
      stories.push({
        num: `${storyId}`,
        key,
        title: storyTitle,
        intent,
        accept,
      });
    }
    epics.push({ num: epicNum, title: epicTitle, intro, file, stories });
  }
  return epics;
}

// ---- screenshots -----------------------------------------------------------
function readScreenshotManifest() {
  if (!fs.existsSync(SCREENSHOT_MANIFEST_SRC)) return [];
  const raw = fs.readFileSync(SCREENSHOT_MANIFEST_SRC, 'utf8');
  const items = [];
  const lines = raw.split('\n');
  let current = null;
  for (const line of lines) {
    const t = line.trim();
    if (!t || t.startsWith('#')) continue;
    const m = t.match(/^-\s+([\w.\-]+\.png)\s*:\s*(.+)$/);
    if (m) {
      current = { file: m[1], label: m[2].trim() };
      items.push(current);
    } else if (current) {
      const kv = t.match(/^(\w+):\s*(.+)$/);
      if (kv) current[kv[1]] = kv[2].trim();
    }
  }
  return items.map((it) => ({
    ...it,
    present: fs.existsSync(path.join(SCREENSHOT_DIR, it.file)),
  }));
}

// ---- emitters --------------------------------------------------------------
function statusBadge(status) {
  const label = STATUS_LABELS[status] || status;
  return `<span className="story-status story-status-${status}">${label}</span>`;
}


function writeEpicIndex(epics) {
  const cards = epics
    .map(
      (e) => `- [Epic ${e.num}: ${e.title}](/docs/epics/epic-${e.num}) — ${e.stories.length} Geschichten`,
    )
    .join('\n');
  const md = `---
sidebar_position: 1
---

# Epics & Stories

Klicke auf ein Epic, um seine Geschichten (Stories) zu sehen. Jede Geschichte ist
eine Karte mit Status, Zielsetzung und Akzeptanzkriterien. Der Status wird beim
nächsten Build automatisch aus \`sprint-status.yaml\` neu berechnet.

${cards}

## Screenshots

Siehe [Screenshots und deren Status](/docs/screenshots) für die fehlenden und
vorhandenen Aufnahmen der Anwendung.
`;
  fs.mkdirSync(EPICS_OUT, { recursive: true });
  fs.writeFileSync(path.join(EPICS_OUT, 'index.md'), md);
}

function writeEpicPage(epic, status) {
  const cards = epic.stories
    .map((s) => {
      const st = status[s.key] || 'backlog';
      const intentLines = s.intent
        ? s.intent.split('\n').map((l) => l.trim()).filter(Boolean).join(' ')
        : s.intent;
      return `### ${s.num} — ${s.title} ${statusBadge(st)}

<div className="story-card">

**Zielsetzung**

${intentLines || '—'}

<details>
<summary>Akzeptanzkriterien</summary>

${s.accept || '—'}

</details>

</div>`;
    })
    .join('\n\n');
  const md = `---
sidebar_position: ${epic.num}
---

# Epic ${epic.num}: ${epic.title}

${epic.intro}

${cards}

---

[← Alle Epics](/docs/epics/)
`;
  fs.writeFileSync(path.join(EPICS_OUT, `epic-${epic.num}.md`), md);
}

function writeCategory() {
  fs.mkdirSync(EPICS_OUT, { recursive: true });
  fs.writeFileSync(
    path.join(EPICS_OUT, '_category_.json'),
    JSON.stringify(
      {
        label: 'Epics & Stories',
        position: 2,
      },
      null,
      2,
    ),
  );
}

function progressBar(pct) {
  return `<progress className="gear-progress" value="${pct}" max="100">${pct}%</progress> <span className="gear-progress-label">${pct}%</span>`;
}

function writeStatusPage(epics, status) {
  const total = epics.reduce((n, e) => n + e.stories.length, 0);
  const done = epics.reduce(
    (n, e) => n + e.stories.filter((s) => status[s.key] === 'done').length,
    0,
  );
  const pct = total ? Math.round((done / total) * 100) : 0;
  const epicRows = epics
    .map((e) => {
      const d = e.stories.filter((s) => status[s.key] === 'done').length;
      const epct = e.stories.length ? Math.round((d / e.stories.length) * 100) : 0;
      return `| [Epic ${e.num}](/docs/epics/epic-${e.num}) | ${e.title} | ${d}/${e.stories.length} | ${progressBar(epct)} |`;
    })
    .join('\n');
  const md = `---
sidebar_position: 1
---

# Implementierungsstatus

> Automatisch aus \`sprint-status.yaml\` beim nächsten Build berechnet.

## Gesamtfortschritt

${done}/${total} Geschichten erledigt

${progressBar(pct)}

## Nach Epic

| Epic | Titel | Erledigt | Fortschritt |
| --- | --- | --- | --- |
${epicRows}

## Legende

- **Backlog** — noch nicht gestartet
- **Bereit** — spezifiziert, bereit zur Umsetzung
- **In Arbeit** — wird gerade umgesetzt
- **Review** — umgesetzt, im Review
- **Erledigt** — abgeschlossen
`;
  fs.writeFileSync(path.join(DOCS_DIR, 'docs/status.md'), md);
}

function writeScreenshotsPage(manifest) {
  const present = manifest.filter((s) => s.present).length;
  const missing = manifest.filter((s) => !s.present).length;
  const rows = manifest
    .map((s) => {
      const cell = s.present
        ? `✅ \`${s.file}\``
        : `❌ **fehlt** — \`${s.file}\``;
      return `| ${s.label} | ${s.device || 'desktop'} | ${cell} |`;
    })
    .join('\n');
  const md = `---
sidebar_position: 3
---

# Screenshots — Übersicht

Platzhalter für Screenshots der laufenden Anwendung (Desktop & mobil). Ein
Screenshot ist **vorhanden**, wenn die Datei unter \`docs/static/screenshots/\`
mit exakt dem angegebenen Namen existiert; die Datei-Namen sind die Konvention
\`screenshot-{epic}-{surface}-{desktop|mobile}.png\`.

**${present} vorhanden · ${missing} fehlend**

| Oberfläche | Gerät | Datei |
| --- | --- | --- |
${rows}

## Namenskonvention

\`screenshot-{epic}-{surface}-{desktop|mobile}.png\`

Beispiele: \`screenshot-1-login-desktop.png\`, \`screenshot-2-admin-mobile.png\`.

Die Liste wird aus \`docs/screenshots/screenshots.yaml\` erzeugt. Neue geplante
Screenshots dort ergänzen, damit sie hier erscheinen.
`;
  fs.writeFileSync(path.join(DOCS_DIR, 'docs/screenshots.md'), md);
  fs.mkdirSync(path.dirname(SCREENSHOT_MANIFEST_OUT), { recursive: true });
  fs.writeFileSync(SCREENSHOT_MANIFEST_OUT, JSON.stringify(manifest, null, 2));
}

// ---- main ------------------------------------------------------------------
function main() {
  const status = readSprintStatus();
  const epics = parseEpics();
  fs.mkdirSync(EPICS_OUT, { recursive: true });

  writeCategory();
  writeEpicIndex(epics);
  for (const e of epics) writeEpicPage(e, status);
  writeStatusPage(epics, status);
  writeScreenshotsPage(readScreenshotManifest());

  console.log(
    `docs generator: ${epics.length} epics, ${epics.reduce((n, e) => n + e.stories.length, 0)} stories, status+epic+screenshots pages written.`,
  );
}

main();