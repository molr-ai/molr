import { test, expect } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('molr-ui-design-locale', 'en'));
  await page.goto('/?components=chat#command-selector');
});

test('command selector completes without sending, handles filtering and simulates compression', async ({ page }) => {
  const example = page.locator('#command-selector'), input = example.getByRole('textbox', { name: 'Message your assistant' });
  await example.getByRole('button', { name: 'Open selector', exact: true }).click();
  const menu = page.getByRole('listbox', { name: 'Commands', exact: true });
  await expect(menu).toBeVisible(); await expect(input).toBeFocused();
  await expect(menu.getByRole('option')).toHaveCount(1);
  await input.dispatchEvent('keydown', { key: 'Enter', code: 'Enter', isComposing: true });
  await expect(input).toHaveValue('/');
  await input.press('Escape'); await expect(menu).toBeHidden(); await expect(input).toHaveValue('/');
  await example.getByRole('radio', { name: 'No matches', exact: true }).click();
  await expect(menu).toContainText('No matching commands');
  await example.getByRole('radio', { name: 'Filter', exact: true }).click();
  await expect(input).toHaveValue('/com');
  await menu.getByRole('option').click(); await expect(input).toHaveValue('/compress');
  await expect(menu).toBeHidden(); await expect(input).toBeFocused();
  await input.press('Enter'); await expect(example.locator('.ui-chat-status')).toContainText('Compressing context');
  await expect(example.locator('.composer-notice')).toContainText('Simulated compression complete');
  await expect(example.locator('.ui-chat-status')).toContainText('Idle');
  await expect(input).toHaveValue('');
});

test('floating selector stays in viewport and closes on outside interaction', async ({ page }, info) => {
  const example = page.locator('#command-selector');
  await example.getByRole('button', { name: 'Open selector', exact: true }).click();
  const popup = page.locator('.composer-commands:popover-open');
  await expect(popup).toBeVisible();
  const box = (await popup.boundingBox())!, viewport = page.viewportSize()!;
  expect(box.x).toBeGreaterThanOrEqual(0); expect(box.y).toBeGreaterThanOrEqual(0);
  expect(box.x + box.width).toBeLessThanOrEqual(viewport.width); expect(box.y + box.height).toBeLessThanOrEqual(viewport.height);
  expect(box.width).toBeLessThanOrEqual(360);
  if (info.project.name === 'mobile') await expect(popup.locator('.composer-commands__footer')).toBeHidden();
  await example.getByRole('heading', { name: 'Command selector', exact: true }).click();
  await expect(popup).toBeHidden();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
