import { test, expect } from '@playwright/test';
import type { AppAppearance, MolrAppAPI } from '@molr/ui-host/app';

test('desktop inherits saved account appearance only after login and preserves the system preference', async ({ page }) => {
  await page.addInitScript(() => {
    const appearances: AppAppearance[] = [];
    Object.assign(window, { appearanceFixture: appearances });
    window.molrApp = {
      getInfo: async () => ({ apiVersion: 1, platform: 'darwin', build: { version: 'development', commit: '' }, connection: { name: 'Fixture', url: location.origin, kind: 'remote' }, window: { maximized: false, fullscreen: false } }),
      window: { minimize: async () => {}, toggleMaximize: async () => {}, close: async () => {} },
      disconnect: async () => {}, onWindowStateChanged: () => () => {},
      oauth: { authorize: async () => ({ status: 'cancelled' }), cancel: async () => {} },
      updates: { status: async () => ({ comparison: 'uncomparable', checking: false }), check: async () => ({ comparison: 'uncomparable', checking: false }), openDownloads: async () => {}, upgradeLocal: async () => {}, onChanged: () => () => {} },
      notifications: {
        begin: async () => {}, requestPermission: async () => true, show: async () => {},
        close: async () => {}, onClick: () => () => {},
      },
      setAppearance: async appearance => { appearances.push(appearance); },
    } satisfies MolrAppAPI;
  });
  const calls = () => page.evaluate(() => (window as unknown as { appearanceFixture: AppAppearance[] }).appearanceFixture);
  const latest = async () => (await calls()).at(-1);
  await page.goto('/#/account/theme');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  expect(await calls()).toEqual([]);
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  const palettes = page.getByRole('group', { name: 'Color palettes', exact: true });
  await expect(palettes).toBeVisible();
  if (process.env.MOLR_E2E_SANDBOX_SERVICE === '1' && process.platform === 'linux') {
    const sandbox = await (await page.request.get('/api/sandbox')).json();
    expect(sandbox.Enabled).toBe(true);
    expect(sandbox.Capabilities).toMatchObject({ Reason: 'available', Shell: true, FileIsolation: true, ProcessIsolation: true, NetworkIsolation: true, ResourceLimits: true });
  }
  const saved = await (await page.request.get('/api/settings')).json();
  await expect.poll(latest).toEqual({ theme: saved.Theme, palette: saved.Palette });
  await palettes.getByRole('button', { name: 'Ocean', exact: true }).click();
  await expect.poll(async () => (await latest())?.palette).toBe('ocean');
  await page.getByRole('combobox', { name: 'Display mode', exact: true }).click();
  await page.getByRole('option', { name: /^Dark / }).click();
  await expect.poll(latest).toEqual({ theme: 'dark', palette: 'ocean' });

  await page.route('**/api/settings', route => route.request().method() === 'POST'
    ? route.fulfill({ status: 503, contentType: 'application/json', body: '{"code":"unavailable"}' })
    : route.continue());
  const beforeFailure = (await calls()).length;
  await palettes.getByRole('button', { name: 'Rose', exact: true }).click();
  await expect(page.locator('.ui-toast').getByRole('alert')).toContainText('service is unavailable');
  expect((await calls()).slice(beforeFailure)).not.toContainEqual({ theme: 'dark', palette: 'rose' });
  expect(await latest()).toEqual({ theme: 'dark', palette: 'ocean' });
  await page.unroute('**/api/settings');
  await page.getByRole('combobox', { name: 'Display mode', exact: true }).click();
  await page.getByRole('option', { name: /^System / }).click();
  await expect.poll(latest).toEqual({ theme: 'system', palette: 'ocean' });
  for (const colorScheme of ['dark', 'light'] as const) {
    await page.emulateMedia({ colorScheme });
    await expect(page.locator('html')).toHaveAttribute('data-theme', colorScheme);
    expect(await latest()).toEqual({ theme: 'system', palette: 'ocean' });
  }
  await page.reload();
  await expect.poll(latest).toEqual({ theme: 'system', palette: 'ocean' });
});
