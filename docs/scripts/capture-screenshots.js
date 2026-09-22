// Capture real G.E.A.R. app screenshots for the docs user-stories.
// Usage: node scripts/capture-screenshots.js
// Requires the app running at http://localhost:8080 (serves web/dist + API).
const { chromium } = require('playwright');
const fs = require('fs');
const path = require('path');

const APP = 'http://localhost:8080';
const OUT = path.join(__dirname, '../static/screenshots');
const ADMIN_EMAIL = 'admin.1@gear.local';
const ADMIN_PASSWORD = 'AdminShot!2026';

fs.mkdirSync(OUT, { recursive: true });

async function shot(page, name, opts = {}) {
  const file = path.join(OUT, name);
  await page.waitForTimeout(opts.wait ?? 600);
  await page.screenshot({ path: file, fullPage: opts.fullPage ?? false });
  console.log('captured', name);
}

(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });

  // --- Login page (unauthenticated) ---
  await page.goto(`${APP}/login`);
  await shot(page, 'screenshot-1-login-desktop.png');

  // --- Login as admin.1 ---
  await page.fill('input[name="email"], input[type="email"], input[placeholder*="E-Mail"]', ADMIN_EMAIL);
  await page.fill('input[name="password"], input[type="password"]', ADMIN_PASSWORD);
  await page.click('button[type="submit"], button:has-text("Anmelden"), button:has-text("Einloggen")');
  await page.waitForURL('**/dashboard', { timeout: 8000 }).catch(() => {});
  await page.waitForTimeout(800);

  // --- Dashboard / Übersicht (traffic-light list) ---
  await shot(page, 'screenshot-1-dashboard-desktop.png');

  // --- Tool details / history ---
  await page.goto(`${APP}/tools/01a08062-6cf2-79b1-9e71-ae6140791d0c`).catch(() => {});
  // navigate to the first tool via the dashboard list
  await page.goto(`${APP}/`);
  await page.waitForTimeout(800);
  const firstRow = page.locator('a[href^="/tools/"], tr a, li a').first();
  if (await firstRow.count()) {
    await firstRow.click().catch(() => {});
    await page.waitForTimeout(800);
    await shot(page, 'screenshot-6-3-history-desktop.png', { wait: 400 });
  }

  // --- Inspection page (pass/fail) ---
  await page.goto(`${APP}/`);
  await page.waitForTimeout(600);
  const inspectLink = page.locator('a[href^="/inspection/"]').first();
  if (await inspectLink.count()) {
    await inspectLink.click().catch(() => {});
    await page.waitForTimeout(800);
    await shot(page, 'screenshot-5-4-inspection-desktop.png');
  }

  // --- Admin: users & roles ---
  await page.goto(`${APP}/admin/benutzer`);
  await page.waitForTimeout(900);
  await shot(page, 'screenshot-2-admin-users-desktop.png');

  // --- Admin: rollen (roles/permissions) ---
  await page.goto(`${APP}/admin/rollen`);
  await page.waitForTimeout(900);
  await shot(page, 'screenshot-2-roles-desktop.png');

  // --- Admin: qualifikationen ---
  await page.goto(`${APP}/admin/qualifikationen`);
  await page.waitForTimeout(900);
  await shot(page, 'screenshot-2-qualifications-desktop.png');

  // --- Admin: werkzeuge (tool catalogue) ---
  await page.goto(`${APP}/admin/werkzeuge`);
  await page.waitForTimeout(900);
  await shot(page, 'screenshot-4-3-tools-desktop.png');

  // --- Admin: einstellungen (system settings) ---
  await page.goto(`${APP}/admin/einstellungen`);
  await page.waitForTimeout(900);
  await shot(page, 'screenshot-3-settings-desktop.png');

  // --- Admin: backup destinations ---
  await page.goto(`${APP}/admin/einstellungen`);
  await page.waitForTimeout(400);
  const backupTab = page.locator('a,button,div[role="tab"]', { hasText: /Backup|Sicherung/ }).first();
  if (await backupTab.count()) {
    await backupTab.click().catch(() => {});
    await page.waitForTimeout(700);
    await shot(page, 'screenshot-3-backup-desktop.png');
  }

  await browser.close();
  console.log('done');
})();