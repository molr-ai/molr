import { test, expect, type Page } from '@playwright/test';
import type { AppAppearance, MolrAppAPI } from '@molr/ui-host/app';

async function desktopBridge(page: Page) {
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
        begin: async () => {}, end: async () => {}, setBadge: async () => true, requestPermission: async () => true, show: async () => {},
        close: async () => {}, onClick: () => () => {},
      },
      setAppearance: async appearance => { appearances.push(appearance); },
    } satisfies MolrAppAPI;
  });
}

test('desktop inherits saved account appearance only after login and preserves the system preference', async ({ page }) => {
  await desktopBridge(page);
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

for (const locale of ['zh-CN', 'en-US']) test.describe(`desktop first-time setup in ${locale}`, () => {
  test.use({ locale });
  test('uses initialized settings without applying cached defaults, including during a slow refresh', async ({ page }) => {
    await desktopBridge(page);
    const zh = locale === 'zh-CN';
    let initialized = false, setupPosts = 0, refreshed = false;
    let releaseSettings!: () => void;
    const settingsReady = new Promise<void>(resolve => { releaseSettings = resolve; });
    await page.route(url => url.pathname === '/api/setup', async route => {
      if (route.request().method() === 'POST') {
        const body = route.request().postDataJSON();
        expect(body.Language).toBe(zh ? 'zh' : 'en');
        expect(body.Timezone).toBe('Asia/Shanghai');
        setupPosts++; initialized = true;
      }
      await route.fulfill({ json: { Required: !initialized, AssistantID: '' } });
    });
    await page.route(url => url.pathname === '/api/settings', async route => {
      const response = await route.fetch(), settings = await response.json();
      if (initialized) { refreshed = true; await settingsReady; }
      await route.fulfill({ json: { ...settings, Language: initialized ? (zh ? 'zh' : 'en') : 'en', Timezone: initialized ? 'Asia/Shanghai' : 'UTC', Version: initialized ? 1 : 0 } });
    });
    await page.route(url => url.pathname === '/api/models', route => route.fulfill({ json: [{ ID: 'setup-model', ResourceID: 'setup-provider', Name: 'Setup fixture', SupportsTools: true, ContextLimit: 32768, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0 } }] }));
    try {
      await page.goto('/');
      await page.getByLabel(zh ? '用户名' : 'Username', { exact: true }).fill('owner');
      await page.getByLabel(zh ? '密码' : 'Password', { exact: true }).fill('webui-e2e-password-2026!');
      await page.getByRole('button', { name: zh ? '登录' : 'Sign in', exact: true }).click();
      await expect(page.locator('.setup-page')).toBeVisible();
      await expect.poll(() => page.evaluate(() => (window as unknown as { appearanceFixture: AppAppearance[] }).appearanceFixture.length)).toBeGreaterThan(0);
      await page.evaluate(() => {
        const languages: string[] = [];
        Object.assign(window, { setupLanguages: languages });
        new MutationObserver(() => languages.push(document.documentElement.lang)).observe(document.documentElement, { attributes: true, attributeFilter: ['lang'] });
      });
      await page.getByRole('button', { name: zh ? '开始使用' : 'Start using Molr', exact: true }).click();
      await expect.poll(() => refreshed).toBe(true);
      await expect(page.locator('html')).toHaveAttribute('lang', zh ? 'zh-CN' : 'en');
      releaseSettings();
      await expect(page.locator('.workspace')).toBeVisible();
      await expect(page.locator('html')).toHaveAttribute('lang', zh ? 'zh-CN' : 'en');
      expect(await page.evaluate(() => (window as unknown as { setupLanguages: string[] }).setupLanguages)).not.toContain(zh ? 'en' : 'zh-CN');
      expect((await page.context().cookies()).filter(cookie => cookie.name.startsWith('molr_locale_')).map(cookie => cookie.value)).toEqual([zh ? 'zh' : 'en']);
      await page.reload();
      await expect(page.locator('.workspace')).toBeVisible();
      await expect(page.locator('html')).toHaveAttribute('lang', zh ? 'zh-CN' : 'en');
      expect(setupPosts).toBe(1);
    } finally { releaseSettings(); await page.unrouteAll({ behavior: 'wait' }); }
  });
});
