import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import { defaultConfiguration, parseManifest, readConfiguration, requestPermission, resolveUI } from '@molr/ui-host';
import { officialPackage } from '../src/interfaces/official/manifest';
import { testPackage } from '../src/interfaces/test/manifest';
import { expectFitsWebView } from './viewport';
const uiPort = Number(process.env.MOLR_TEST_PORT ?? '5193');
async function login(page: Page) {
  await page.goto('/#/account/theme');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('combobox', { name: 'Theme', exact: true })).toBeVisible();
}
async function chooseTheme(page: Page, label: string) {
  await page.getByRole('combobox', { name: 'Theme', exact: true }).click();
  await page.getByRole('option', { name: label, exact: true }).click();
  await expect(page.getByRole('combobox', { name: 'Theme', exact: true })).toContainText(label);
  await expect(page.getByRole('combobox', { name: 'Theme', exact: true })).toBeEnabled();
}
async function post(page: Page, path: string, data: object) {
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${uiPort}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: `http://127.0.0.1:${uiPort}`, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: 0 } } });
  expect(response.status(), await response.text()).toBe(200); return response.json();
}
async function assistant(page: Page) {
  const name = `Theme ${Date.now()}`, endpoint = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: endpoint, AllowedOrigins: [endpoint], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  return post(page, '/api/assistants', { Name: name, ModelID: models.find((m: { ResourceID: string }) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
}

test('one selected theme determines every page; missing pages use the default theme', () => {
  const packages = [officialPackage, testPackage], configuration = { schema: 2 as const, selected: { id: 'molr.test', version: '1.0.0' } };
  expect(resolveUI(configuration, packages, 'assistants')).toBe(testPackage);
  for (const page of ['plans', 'resources', 'workbench', 'settings'] as const) expect(resolveUI(configuration, packages, page)).toBe(officialPackage);
  expect(readConfiguration(JSON.stringify({ ...configuration, routes: { assistants: { id: 'evil.custom', version: '1.0.0' } }, installed: [{ manifest: { id: 'evil.custom' } }] }), packages)).toEqual(configuration);
  expect(readConfiguration(JSON.stringify({ schema: 1, routes: { assistants: { id: 'evil.custom' } }, installed: [] }), packages)).toEqual(defaultConfiguration());
  expect(readConfiguration(JSON.stringify({ schema: 2, selected: { id: 'evil.custom', version: '1.0.0' } }), packages)).toEqual(defaultConfiguration());
  expect(() => parseManifest({ ...testPackage.manifest, apiVersion: 2 })).toThrow('incompatible_ui_api');
  expect(requestPermission('GET', '/api/conversations/one/messages?limit=200')).toBe('chat.read');
  expect(requestPermission('POST', '/api/auth/login-links')).toBeNull();
});

test('theme settings have one theme selector, persists its choice and exposes no package management', async ({ page }, info) => {
  await login(page);
  const nav = page.getByRole('navigation', { name: 'Molr' });
  await expect(nav.getByRole('button', { name: 'Page layouts', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Manage page layouts', exact: true })).toHaveCount(0);
  await page.getByRole('combobox', { name: 'Theme', exact: true }).click();
  await expect(page.getByRole('option', { name: 'Molr default theme', exact: true })).toBeVisible();
  await expect(page.getByRole('option', { name: 'Molr test theme', exact: true })).toBeVisible();
  await page.getByRole('option', { name: 'Molr test theme', exact: true }).click();
  await expect(page.getByRole('combobox', { name: 'Theme', exact: true })).toContainText('Molr test theme');
  await page.reload();
  await expect(page.getByRole('combobox', { name: 'Theme', exact: true })).toContainText('Molr test theme');
  await expect(page.getByRole('textbox', { name: /manifest/i })).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Theme colors', exact: true })).toBeVisible();
  await expect(page.getByRole('combobox', { name: 'Display mode', exact: true })).toBeVisible();
  await page.screenshot({ path: info.outputPath('appearance-theme-selector.png') });
  await page.setViewportSize({ width: 320, height: 720 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  for (const retired of ['/#/ui', '/#/account/appearance']) {
    await page.goto(retired);
    await expect(page.getByRole('combobox', { name: 'Theme', exact: true })).toHaveCount(0);
  }
  await expect(page.locator('.ui-manager')).toHaveCount(0);
});

test('test theme replaces chat only and shares drafts and real messages with the default theme', async ({ page }, info) => {
  const errors: string[] = []; page.on('pageerror', error => errors.push(error.message));
  await login(page); await chooseTheme(page, 'Molr default theme'); const selected = await assistant(page);
  await page.goto(`/#/assistants/${selected.ID}`);
  await page.locator('.chat-input textarea').fill('A shared draft');
  await page.goto('/#/account/theme'); await chooseTheme(page, 'Molr test theme');
  await page.goto(`/#/assistants/${selected.ID}`);
  await expect(page.locator('.assistant-workspace--focus')).toBeVisible();
  await expect(page.getByRole('combobox', { name: 'Assistant list', exact: true })).toContainText(selected.Name);
  await expect(page.locator('.chat-input textarea')).toHaveValue('A shared draft');
  await page.locator('.chat-input textarea').press('Enter');
  await expect(page.locator('.chat-history')).toContainText('A shared draft');
  await page.locator('.chat-input textarea').fill('Back to default');
  if (info.project.name === 'mobile') await page.getByRole('button', { name: 'Back to assistants', exact: true }).click();
  await page.getByRole('navigation', { name: 'Molr' }).getByRole('button', { name: 'Resources', exact: true }).click();
  await expect(page.locator('.resources-workspace')).toBeVisible();
  await page.goto('/#/account/theme');
  await expect(page.getByRole('combobox', { name: 'Theme', exact: true })).toContainText('Molr test theme');
  await chooseTheme(page, 'Molr default theme');
  await page.goto(`/#/assistants/${selected.ID}`);
  await expect(page.locator('.assistant-workspace--focus')).toHaveCount(0);
  await expect(page.locator('.chat-input textarea')).toHaveValue('Back to default');
  await expect(page.locator('.chat-history')).toContainText('A shared draft');
  expect(errors).toEqual([]);
});

for (const theme of ['Molr default theme', 'Molr test theme']) {
  test(`${theme}: long conversations scroll inside chat without extending the document`, async ({ page }) => {
    await login(page); await chooseTheme(page, theme);
    const selected = await assistant(page);
    for (let index = 0; index < 2; index++) {
      await post(page, '/api/conversations/messages', { ConversationID: selected.ConversationID, Content: { Kind: 'text', Text: `Long message ${index}\n\n` + 'A paragraph in a long conversation.\n\n'.repeat(60) } });
    }
    await page.goto('/#/assistants');
    await expect(page.locator('.assistant-workspace')).toBeVisible();
    if (theme === 'Molr default theme') await page.locator(`[data-assistant-id="${selected.ID}"]`).click();
    else {
      await page.getByRole('combobox', { name: 'Assistant list', exact: true }).click();
      await page.getByRole('option', { name: selected.Name, exact: true }).click();
    }
    const history = page.locator('.chat-history');
    await expect(history.locator('.ui-chat-message--user')).toHaveCount(2);
    const fitsViewport = () => page.evaluate(() => document.documentElement.scrollHeight <= innerHeight && document.documentElement.scrollWidth <= innerWidth);
    await expect.poll(fitsViewport).toBe(true);
    await expectFitsWebView(page);
    expect(await history.evaluate(el => el.scrollHeight > el.clientHeight)).toBe(true);
    for (const bottom of [false, true]) {
      await history.evaluate((el, bottom) => { el.scrollTop = bottom ? el.scrollHeight : 0; }, bottom);
      await expect.poll(fitsViewport).toBe(true);
      await expect(page.locator('.chat-header')).toBeInViewport();
      await expect(page.locator('.chat-input textarea')).toBeInViewport();
    }
    // The copy acknowledgement remains accessible without creating page overflow.
    await page.evaluate(() => Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined }));
    const last = history.locator('.ui-chat-message--user').last();
    await last.getByRole('button', { name: 'Copy original text', exact: true }).click();
    await expect(last.getByRole('status')).toHaveText('Copied');
    await expect.poll(fitsViewport).toBe(true);
    await page.reload();
    await expect(history.locator('.ui-chat-message--user')).toHaveCount(2);
    await expect.poll(fitsViewport).toBe(true);
  });

  test(`${theme}: assistant selection follows the URL including the unselected entry`, async ({ page }, info) => {
    await login(page); await chooseTheme(page, theme);
    const first = await assistant(page), second = await assistant(page);
    const focus = theme === 'Molr test theme';
    const nav = page.getByRole('navigation', { name: 'Molr' });
    const mobile = info.project.name === 'mobile';
    const selector = page.getByRole('combobox', { name: 'Assistant list', exact: true });
    async function expectUnselected() {
      await expect(page).toHaveURL(/#\/assistants$/);
      await expect(nav).toBeVisible();
      if (focus) {
        await expect(selector).toBeVisible();
        await expect(selector).not.toContainText(first.Name);
        await expect(selector).not.toContainText(second.Name);
        await selector.click();
        await expect(page.getByRole('option', { selected: true })).toHaveCount(0);
        await page.keyboard.press('Escape');
      } else {
        await expect(page.locator(`[data-assistant-id="${first.ID}"]`)).toBeVisible();
        await expect(page.locator('.assistant-list [aria-current="page"]')).toHaveCount(0);
      }
      await expect(page.locator('.chat-input textarea')).toHaveCount(0);
      await expect(page.locator('.chat-history')).toHaveCount(0);
    }
    async function select(target: typeof first) {
      if (focus) {
        await selector.click();
        await page.getByRole('option', { name: target.Name, exact: true }).click();
      } else {
        await page.locator(`[data-assistant-id="${target.ID}"]`).click();
      }
      await expect(page).toHaveURL(new RegExp(`/assistants/${target.ID}$`));
      await expect(page.locator('.chat-input textarea')).toBeVisible();
      if (mobile) await expect(nav).toHaveCount(0);
      else await expect(nav).toBeVisible();
    }
    // API fixture writes bypass the open page's query invalidation. Start a fresh document.
    await page.goto('/#/assistants');
    await page.reload();
    await expectUnselected();
    await page.reload();
    await expectUnselected();
    await select(first);
    await page.locator('.chat-input textarea').fill('Route-owned draft');
    await page.goBack();
    await expectUnselected();
    await page.goForward();
    await expect(page).toHaveURL(new RegExp(`/assistants/${first.ID}$`));
    await expect(page.locator('.chat-input textarea')).toHaveValue('Route-owned draft');
    await page.reload();
    await expect(page.locator('.chat-input textarea')).toHaveValue('Route-owned draft');
    if (mobile) {
      await expect(nav).toHaveCount(0);
      await page.getByRole('button', { name: 'Back to assistants', exact: true }).click();
    } else await nav.getByRole('button', { name: 'Assistants', exact: true }).click();
    await expectUnselected();
    await select(second);
    await expect(page.locator('.chat-input textarea')).toHaveValue('');
  });
}

test('theme saves preserve preferences, ignore local storage and keep the saved choice on failure', async ({ page }) => {
  await login(page); await chooseTheme(page, 'Molr default theme');
  const before = await (await page.request.get('/api/settings')).json();
  await page.evaluate(ownerID => localStorage.setItem(`molr-ui-packages:v1:${ownerID}`, '{broken'), before.OwnerID);
  await page.reload();
  await expect(page.getByRole('combobox', { name: 'Theme', exact: true })).toContainText('Molr default theme');
  await page.route('**/api/settings', route => route.request().method() === 'POST' ? route.fulfill({ status: 503, contentType: 'application/json', body: '{"code":"unavailable"}' }) : route.continue());
  await page.getByRole('combobox', { name: 'Theme', exact: true }).click();
  await page.getByRole('option', { name: 'Molr test theme', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Could not save your theme');
  await expect(page.getByRole('combobox', { name: 'Theme', exact: true })).toContainText('Molr default theme');
  expect((await (await page.request.get('/api/settings')).json()).Version).toBe(before.Version);
  await page.unroute('**/api/settings');
  await page.evaluate(() => { Storage.prototype.setItem = () => { throw new DOMException('Full', 'QuotaExceededError'); }; });
  await chooseTheme(page, 'Molr test theme');
  const after = await (await page.request.get('/api/settings')).json();
  expect(after.UITheme).toBe('molr.test@1.0.0');
  for (const field of ['Theme', 'Palette', 'Language', 'Currency', 'Timezone', 'ReducedMotion', 'AnswerPreferences', 'DefaultModelID']) expect(after[field]).toEqual(before[field]);
  await expect(page.getByRole('alert')).toHaveCount(0);
});

test('theme, palette and display mode sync to a separate browser; remote theme changes preserve drafts', async ({ page, browser }) => {
  await login(page); const selected = await assistant(page);
  await chooseTheme(page, 'Molr test theme');
  await page.getByRole('group', { name: 'Color palettes' }).getByRole('button', { name: 'Ocean', exact: true }).click();
  await expect.poll(async () => (await (await page.request.get('/api/settings')).json()).Palette).toBe('ocean');
  await page.getByRole('combobox', { name: 'Display mode', exact: true }).click();
  await page.getByRole('option', { name: /^Dark / }).click();
  await expect.poll(async () => (await (await page.request.get('/api/settings')).json()).Theme).toBe('dark');
  expect((await (await page.request.get('/api/settings')).json()).UITheme).toBe('molr.test@1.0.0');
  const other = await browser.newContext({ locale: 'en-US' });
  try {
    const next = await other.newPage(); await login(next);
    await expect(next.getByRole('combobox', { name: 'Theme', exact: true })).toContainText('Molr test theme');
    await expect(next.locator('html')).toHaveAttribute('data-palette', 'ocean');
    await expect(next.locator('html')).toHaveAttribute('data-theme', 'dark');
    await next.goto(`/#/assistants/${selected.ID}`);
    await expect(next.locator('.assistant-workspace--focus')).toBeVisible();
    await next.locator('.chat-input textarea').fill('Keep this draft when another device changes theme');
    await chooseTheme(page, 'Molr default theme');
    // Returning to a visible tab refreshes the real account settings (as do normal snapshots).
    await next.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
    await expect(next.locator('.assistant-workspace--focus')).toHaveCount(0);
    await expect(next.locator('.chat-input textarea')).toHaveValue('Keep this draft when another device changes theme');
    await next.reload();
    await expect(next.locator('.assistant-workspace--focus')).toHaveCount(0);
    await next.goto('/#/account/theme');
    await expect(next.getByRole('combobox', { name: 'Theme', exact: true })).toContainText('Molr default theme');
  } finally { await other.close(); }
});

test('official hire waits for the model catalog instead of navigating to resources', async ({ page }, info) => {
  await login(page); await assistant(page);
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/models', async route => { await gate; await route.continue(); });
  try {
    await page.goto('/#/assistants'); await page.reload();
    const hire = page.getByRole('button', { name: 'Hire new assistant', exact: true }).first();
    await expect(hire).toBeDisabled();
    if (info.project.name === 'mobile') {
      await expect(hire).toHaveClass(/button--ghost/);
      await expect(hire).toHaveText('');
      const search = page.getByRole('button', { name: 'Search', exact: true });
      const searchBox = (await search.boundingBox())!, hireBox = (await hire.boundingBox())!;
      expect(hireBox.x).toBeGreaterThanOrEqual(searchBox.x + searchBox.width);
      expect(hireBox.y).toBe(searchBox.y);
      await expect(page.locator('.sidebar-footer')).toHaveCount(0);
    }
    release();
    await expect(hire).toBeEnabled(); await hire.click();
    await expect(page.getByRole('dialog', { name: 'Hire new assistant', exact: true })).toBeVisible();
  } finally { release(); }
});

test('settings rows only activate their controls and keep theme controls together before palettes', async ({ page }) => {
  await login(page);
  const panel = page.locator('.settings-theme'), rows = panel.locator('.settings-list');
  await expect(rows).toHaveCount(1);
  await expect(rows.getByRole('combobox')).toHaveCount(2);
  await expect(rows.getByRole('switch', { name: 'Reduce motion', exact: true })).toBeVisible();
  expect(await panel.evaluate(element => {
    const controls = element.querySelector('.settings-list')!, palettes = element.querySelector('.settings-theme__palettes')!;
    return !!(controls.compareDocumentPosition(palettes) & Node.DOCUMENT_POSITION_FOLLOWING);
  })).toBe(true);
  for (const label of ['Theme', 'Display mode']) {
    const select = rows.getByRole('combobox', { name: label, exact: true });
    const row = select.locator('..');
    await row.locator('.select__label').click();
    await expect(select).toHaveAttribute('aria-expanded', 'false');
    await row.click({ position: { x: 4, y: 4 } });
    await expect(select).toHaveAttribute('aria-expanded', 'false');
    await select.click(); await expect(select).toHaveAttribute('aria-expanded', 'true'); await page.keyboard.press('Escape');
  }
  const toggle = rows.getByRole('switch', { name: 'Reduce motion', exact: true }), checked = await toggle.isChecked();
  const row = toggle.locator('..').locator('..');
  await row.locator('.settings-label > span').click();
  await row.click({ position: { x: 4, y: 4 } });
  await expect(toggle).toBeChecked({ checked });
  await row.locator('.switch-control').click();
  await expect.poll(async () => (await (await page.request.get('/api/settings')).json()).ReducedMotion).toBe(!checked);
  await expect(toggle).toBeEnabled(); await toggle.focus(); await page.keyboard.press('Space');
  await expect.poll(async () => (await (await page.request.get('/api/settings')).json()).ReducedMotion).toBe(checked);
  await page.goto('/#/account/general');
  for (const label of ['Language', 'Timezone', 'Currency']) {
    const select = page.getByRole('combobox', { name: label, exact: true });
    await select.locator('..').locator('.select__label').click();
    await expect(select).toHaveAttribute('aria-expanded', 'false');
  }
  const workbench = page.getByRole('switch', { name: 'Show developer workbench', exact: true });
  const before = await workbench.isChecked();
  await page.locator('.settings-row').filter({ has: workbench }).locator('.settings-label > span').click();
  await expect(workbench).toBeChecked({ checked: before });
});
