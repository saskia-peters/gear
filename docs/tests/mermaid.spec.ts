import {test, expect} from '@playwright/test';

test('mermaid diagram expands to an interactive, responsive overlay', async ({page}) => {
  const consoleErrors: string[] = [];
  page.on('console', (m) => {
    if (m.type() === 'error') {
      consoleErrors.push(m.text());
    }
  });
  page.on('pageerror', (e) => consoleErrors.push(`pageerror: ${e.message}`));

  await page.goto('/gear/docs/planning/architecture-spine');

  // Mermaid renders client-side: wait for the first diagram to appear.
  const container = page.locator('.docusaurus-mermaid-container').first();
  await expect(container.locator('svg')).toBeVisible({timeout: 20_000});

  // Page must remain responsive after render.
  await expect(container).toBeVisible();
  expect(consoleErrors).toEqual([]);

  // Opening the inline diagram must not freeze the page.
  await page.getByRole('button', {name: 'Expand diagram'}).first().click();

  const overlay = page.getByTestId('mermaid-overlay');
  await expect(overlay).toBeVisible({timeout: 10_000});

  // The overlay re-renders with vector labels (htmlLabels:false).
  const transform = page.getByTestId('mermaid-overlay-transform');
  const overlaySvg = transform.locator('svg');
  await expect(overlaySvg).toHaveCount(1, {timeout: 10_000});

  // Diagram should be visibly rendered (has a substantial box, not zero-size).
  const box = await overlaySvg.boundingBox();
  expect(box).not.toBeNull();
  expect(box!.width).toBeGreaterThan(50);
  expect(box!.height).toBeGreaterThan(50);

  // Page still responsive inside the overlay.
  await expect(page.locator('body')).toBeVisible();

  // Zoom in via the toolbar and confirm the fitted box grows (crisp vector re-render).
  const before = await transform.boundingBox();
  await overlay.getByRole('button', {name: 'Zoom in'}).click();
  await expect
    .poll(async () => (await transform.boundingBox())?.width)
    .toBeGreaterThan(before!.width);

  // Close via Escape restores the page.
  await page.keyboard.press('Escape');
  await expect(overlay).not.toBeVisible();
  await expect(page.locator('body')).toBeVisible();
});
