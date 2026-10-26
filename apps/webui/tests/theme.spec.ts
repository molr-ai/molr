import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';

const settingsPane = (page: Page) => page.locator('.settings-pane:visible');
const settingsNavItem = (page: Page, name: string) => page.locator('.settings-nav > button').filter({ hasText: new RegExp(`^${name}$`) });
async function openSettingsSection(page: Page, name: string) {
  const item = settingsNavItem(page, name);
  await item.waitFor({ state: 'attached' });
  if (!(await item.isVisible())) await page.locator('.settings-header').getByRole('button', { name: 'Back', exact: true }).click();
  await item.click();
}
async function login(page: Page) {
  await page.goto('/#/account/theme');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(settingsPane(page)).toBeVisible();
}
async function settings(page: Page) { return (await page.request.get('/api/settings')).json(); }
async function chooseMode(page: Page, mode: string) {
  await page.getByRole('group', { name: 'Display mode', exact: true }).getByRole('radio', { name: mode, exact: true }).check();
  await expect.poll(async () => (await settings(page)).Theme).toBe(mode.toLowerCase());
}

test('theme settings reuse palette cards, saves all themes and restores them in a new browser', async ({ page, browser }, info) => {
  const errors: string[] = []; page.on('pageerror', error => errors.push(error.message));
  await login(page);
  await expect(settingsNavItem(page, 'Theme')).toHaveAttribute('aria-current', 'page');
  const palettes = page.getByRole('group', { name: 'Color palettes', exact: true });
  await expect(palettes.locator('.theme-option__preview')).toHaveCount(6);
  await chooseMode(page, 'Light');
  const colors = new Set<string>();
  for (const [id, label] of [['forest', 'Forest'], ['ocean', 'Ocean'], ['iris', 'Iris'], ['sand', 'Sand'], ['rose', 'Rose'], ['monochrome', 'Monochrome']]) {
    await palettes.getByRole('button', { name: label, exact: true }).click();
    await expect.poll(async () => (await settings(page)).Palette).toBe(id);
    await expect(page.locator('html')).toHaveAttribute('data-palette', id);
    await expect(palettes.getByRole('button', { name: label, exact: true })).toHaveAttribute('aria-pressed', 'true');
    const background = await page.evaluate(() => {
      const hex = getComputedStyle(document.documentElement).getPropertyValue('--color-background').trim();
      return `rgb(${hex.slice(1).match(/../g)!.map(value => parseInt(value, 16)).join(', ')})`;
    });
    await expect(page.locator('body')).toHaveCSS('background-color', background);
    colors.add(background);
  }
  expect(colors.size).toBe(6);
  await chooseMode(page, 'Dark');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-palette', 'monochrome');
  await expect(settingsPane(page)).toBeVisible();
  await expect(page.locator('link[rel="icon"]')).toHaveAttribute('href', '/brand/themes/monochrome/molr-icon.svg');
  await page.screenshot({ path: info.outputPath('appearance.png') });
  await page.setViewportSize({ width: 320, height: 720 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect(await page.locator('.settings-theme').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
  const other = await browser.newContext({ locale: 'en-US' });
  try {
    const next = await other.newPage();
    await login(next);
    await expect(next.locator('html')).toHaveAttribute('data-palette', 'monochrome');
    await expect(next.locator('html')).toHaveAttribute('data-theme', 'dark');
  } finally { await other.close(); }
  await chooseMode(page, 'System');
  await page.emulateMedia({ colorScheme: 'light' });
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await page.emulateMedia({ colorScheme: 'dark' });
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  expect(errors).toEqual([]);
});

test('failed palette saves restore the current choice and successful saves preserve unrelated settings', async ({ page }) => {
  await login(page);
  const before = await settings(page);
  const target = before.Palette === 'ocean' ? ['iris', 'Iris'] : ['ocean', 'Ocean'];
  await page.route('**/api/settings', route => route.request().method() === 'POST' ? route.fulfill({ status: 503, contentType: 'application/json', body: '{"code":"unavailable"}' }) : route.continue());
  await page.getByRole('group', { name: 'Color palettes' }).getByRole('button', { name: target[1], exact: true }).click();
  const panel = settingsPane(page);
  await expect(page.locator('.ui-toast').getByRole('alert')).toContainText('service is unavailable');
  await expect(page.locator('html')).toHaveAttribute('data-palette', before.Palette);
  await expect(panel.locator('.theme-option[aria-pressed="true"] .theme-option__preview')).toHaveAttribute('data-palette', before.Palette);
  expect((await settings(page)).Version).toBe(before.Version);
  await page.unroute('**/api/settings');
  await page.getByRole('group', { name: 'Color palettes' }).getByRole('button', { name: target[1], exact: true }).click();
  await expect.poll(async () => (await settings(page)).Palette).toBe(target[0]);
  const after = await settings(page);
  for (const field of ['Language', 'Timezone', 'Currency', 'Theme', 'ReducedMotion', 'DefaultModelID']) expect(after[field]).toEqual(before[field]);
  await expect(page.locator('.ui-toast').getByRole('alert')).toHaveCount(0);
  await openSettingsSection(page, 'General');
  await expect(page.getByRole('group', { name: 'Display mode', exact: true })).toHaveCount(0);
  await expect(page.getByRole('switch', { name: 'Reduce motion', exact: true })).toHaveCount(0);
});
