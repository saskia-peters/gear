import {test, expect} from '@playwright/test';

test('OpenAPI reference page renders without console errors', async ({page}) => {
  const consoleErrors: string[] = [];
  page.on('console', (m) => {
    if (m.type() === 'error') {
      consoleErrors.push(m.text());
    }
  });
  page.on('pageerror', (e) => consoleErrors.push(`pageerror: ${e.message}`));

  await page.goto('/gear/docs/api/g-e-a-r-api');

  // The API page renders (title + at least one operation visible).
  await expect(page.getByRole('heading', {name: /G\.E\.A\.R\. API/i})).toBeVisible({timeout: 20_000});

  expect(consoleErrors).toEqual([]);
});

test('an OpenAPI operation page renders its method + path without console errors', async ({page}) => {
  const consoleErrors: string[] = [];
  page.on('console', (m) => {
    if (m.type() === 'error') {
      consoleErrors.push(m.text());
    }
  });
  page.on('pageerror', (e) => consoleErrors.push(`pageerror: ${e.message}`));

  // A known generated operation page (auth login).
  await page.goto('/gear/docs/api/post-login');

  await expect(page.locator('body')).toBeVisible({timeout: 20_000});

  expect(consoleErrors).toEqual([]);
});