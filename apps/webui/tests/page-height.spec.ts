import { test, expect, type Page } from '@playwright/test';
import type { Assistant, Model, Resource, Settings } from '@molr/sdk';
import type { PluginPackage } from '@molr/sdk/plugins';
import type { RoutePage } from '../src/navigation';
import { checkPageHeight, expectFitsWebView } from './viewport';

const port = process.env.MOLR_TEST_PORT ?? '5193', base = `http://127.0.0.1:${port}`;
const password = 'webui-e2e-password-2026!';
async function post<T>(page: Page, path: string, data: object, mutation = true): Promise<T> {
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: mutation ? { Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: 0 }, ...data } : data });
  expect(response.ok(), `${path}: ${await response.text()}`).toBe(true);
  return response.json();
}
async function login(page: Page) {
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.locator('.assistant-workspace')).toBeVisible();
}
async function settings(page: Page, patch: Partial<Settings>) {
  const current: Settings = await (await page.request.get('/api/settings')).json();
  await post(page, '/api/settings', { Settings: { ...current, ...patch }, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: current.Version } });
}
async function visit(page: Page, route: string, ready: string) {
  await page.goto(`/#/${route}`);
  await expect(page.locator(ready).first()).toBeVisible();
  await expect(page.locator('.loading:visible')).toHaveCount(0);
  await checkPageHeight(page);
}

test('login, session restoration and invalid login links fit short WebViews', async ({ page }) => {
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/auth/me', async route => { await gate; await route.continue(); });
  try {
    await page.goto('/');
    await expect(page.locator('.session-page')).toBeVisible();
    await checkPageHeight(page);
  } finally { release(); }
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
  await page.unroute('**/api/auth/me');
  await checkPageHeight(page);
  await page.getByLabel('Username', { exact: true }).fill('not-a-user');
  await page.getByLabel('Password', { exact: true }).fill('invalid-password');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.locator('.login-page .ui-alert')).toBeVisible();
  await checkPageHeight(page);
  await page.goto('/#/login?token=invalid');
  await expect(page.getByRole('button', { name: 'Sign in with password', exact: true })).toBeVisible();
  await checkPageHeight(page);
  await page.route('**/api/auth/me', route => route.abort('failed'));
  await page.goto('/');
  await expect(page.locator('.session-page .ui-alert')).toBeVisible();
  await checkPageHeight(page);
  await page.unroute('**/api/auth/me');
});

test('one-time login confirmation fits the WebView and remains usable when short', async ({ page }) => {
  await login(page);
  const proof = await post<{ verification: string }>(page, '/api/auth/reauthenticate', { password, purpose: 'login_link.create' }, false);
  const link = await post<{ value: string }>(page, '/api/auth/login-links', { name: 'Viewport regression', target_path: '/assistants', expires_in: 900, verification: proof.verification }, false);
  await page.goto(`/#/login?token=${link.value}`);
  await expect(page.getByRole('button', { name: 'Confirm sign-in', exact: true })).toBeVisible();
  await checkPageHeight(page);
  await page.setViewportSize({ ...page.viewportSize()!, height: 420 });
  await page.getByRole('button', { name: 'Confirm sign-in', exact: true }).click();
  await expect(page.locator('.assistant-workspace')).toBeVisible();
  await expectFitsWebView(page);
});

for (const theme of ['molr.official', 'molr.test']) {
  test(`${theme}: every route and settings section stays within the WebView`, async ({ page }, info) => {
    test.setTimeout(180_000);
    await login(page);
    const before: Settings = await (await page.request.get('/api/settings')).json();
    const suffix = `${info.project.name}-${Date.now()}`;
    try {
      await settings(page, { UITheme: `${theme}@1.0.0`, WorkbenchVisible: true });
      const endpoint = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
      const resource = await post<Resource>(page, '/api/resources', { Name: `Layout ${suffix}`, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: endpoint, AllowedOrigins: [endpoint], Models: Array.from({ length: 20 }, (_, i) => ({ Name: `Layout model ${suffix} ${i}`, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0 } })) } });
      const models: Model[] = await (await page.request.get('/api/models')).json();
      const model = models.find(m => m.ResourceID === resource.ID)!;
      const assistant = await post<Assistant>(page, '/api/assistants', { Name: `Layout assistant ${suffix}`, ModelID: model.ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
      await post(page, '/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: 'Layout conversation\n\n' + 'A long message must scroll inside the conversation.\n\n'.repeat(80) } });
      const title = `Layout plan ${suffix}`;
      await post(page, '/api/schedules', { Title: title, Kind: 'personal', Trigger: { StartAt: new Date(Date.now() + 3600_000).toISOString(), Timezone: 'Asia/Shanghai', Recurrence: 'FREQ=DAILY;COUNT=60', MissedPolicy: 'coalesce' } });
      const pluginID = `layout.${suffix}`;
      const fields = Object.fromEntries(Array.from({ length: 16 }, (_, i) => [`field${i}`, { type: 'string' as const, title: `Layout field ${i}`, default: `Value ${i}` }]));
      const pkg: PluginPackage = { manifest: { manifestVersion: 1, pluginApi: 2, id: pluginID, name: `Layout plugin ${suffix}`, description: 'Long content for WebView layout regression.', version: '1.0.0', settings: fields, adminSettings: fields, contributes: { themes: [], tools: [], skills: [], pages: [{ id: 'page', title: 'Layout plugin page', entry: 'page.html', capabilities: [] }] } }, files: { 'page.html': `<h1>Long plugin page</h1>${'<p>Scrollable plugin content</p>'.repeat(100)}<script>parent.postMessage({type:'molr.plugin.ready',apiVersion:2},'*')</script>` } };
      const installed = await post<{ Version: number }>(page, '/api/plugins/install', pkg, false);
      await post(page, '/api/plugins/configure', { ID: pluginID, ExpectedVersion: installed.Version, Enabled: true, Grants: [], Bindings: [] }, false);
      await page.reload();
      await expect(page.locator('.assistant-workspace')).toBeVisible();
      await expect(page.locator('.assistant-workspace--focus')).toHaveCount(theme === 'molr.test' ? 1 : 0);

      // A newly added RoutePage must be included here (checked by TypeScript).
      const routes: Record<RoutePage, string> = {
        assistants: '.assistant-workspace', plans: '.plans-workspace', todos: '.plans-workspace', schedules: '.plans-workspace',
        resources: '.resources-workspace', extensions: '.extensions-workspace', workbench: '.workbench',
        account: '.settings-workspace', settings: '.settings-workspace',
      };
      for (const [route, ready] of Object.entries(routes)) await test.step(route, async () => {
        await visit(page, route, ready);
      });

      await test.step('selected chat, assistant information and plans', async () => {
        await visit(page, `assistants/${assistant.ID}`, '.chat-history .ui-chat-message--assistant');
        await page.locator('.chat-assistant-info').click();
        const panel = page.getByRole('dialog');
        await expect(panel).toBeVisible();
        for (const tab of await panel.getByRole('tab').all()) {
          await tab.click(); await expect(tab).toHaveAttribute('aria-selected', 'true');
          await checkPageHeight(page);
        }
        await page.keyboard.press('Escape');
        await page.locator('.chat-header').getByRole('button', { name: 'Plans', exact: true }).click();
        await expect(page.getByRole('dialog', { name: 'Plans', exact: true })).toBeVisible();
        await checkPageHeight(page);
      });
      await test.step('plan list and calendar', async () => {
        await visit(page, 'plans', '.plan-row');
        await expect(page.locator('.plan-row').filter({ hasText: title }).first()).toBeVisible();
        await page.getByRole('button', { name: 'Calendar', exact: true }).click();
        await expect(page.locator('.plans-month-calendar')).toBeVisible();
        await checkPageHeight(page);
      });
      await test.step('resource categories and sandbox sections', async () => {
        await visit(page, `resources/${resource.ID}`, '.resource-models-list');
        const categories = page.locator('.resources-categories > [role="tablist"]').getByRole('tab');
        await expect(categories).toHaveCount(3);
        for (const tab of await categories.all()) {
          await tab.click(); await expect(tab).toHaveAttribute('aria-selected', 'true');
          await checkPageHeight(page);
        }
        await expect(page.locator('.sandbox-detail')).toBeVisible();
        for (const tab of await page.locator('.sandbox-detail').getByRole('tab').all()) {
          await tab.click(); await expect(tab).toHaveAttribute('aria-selected', 'true');
          await checkPageHeight(page);
        }
      });
      // Discover section buttons so future settings sections are covered automatically.
      for (const route of ['account', 'settings']) await test.step(`${route}: every section`, async () => {
        await page.goto(`/#/${route}`);
        await expect(page.locator('.settings-nav')).toBeVisible();
        const names = await page.locator('.settings-nav > button').allTextContents();
        expect(names.length).toBeGreaterThan(0);
        for (const name of names) await test.step(name, async () => {
          await page.goto(`/#/${route}`);
          await page.locator('.settings-nav').getByRole('button', { name: name.trim(), exact: true }).click();
          await expect(page.locator('.settings-pane:visible')).toBeVisible();
          await expect(page.locator('.loading:visible')).toHaveCount(0);
          await checkPageHeight(page);
        });
      });
      for (const entry of ['@settings', '@admin', 'page']) await test.step(`plugin ${entry}`, async () => {
        await visit(page, `extensions/${pluginID}/${entry}`, entry === 'page' ? '.plugin-frame iframe' : '.plugin-settings-form');
        if (entry === 'page') {
          const frame = page.frameLocator('iframe');
          await expect(frame.getByRole('heading', { name: 'Long plugin page' })).toBeVisible();
          const iframe = page.locator('iframe');
          expect(await iframe.evaluate(el => el.getBoundingClientRect().bottom <= innerHeight + 1)).toBe(true);
          await checkPageHeight(page);
        }
      });
      await visit(page, `workbench/${assistant.ID}`, '.trajectory-run');
      await page.reload();
      await expect(page.locator('.trajectory-run').first()).toBeVisible();
      await expectFitsWebView(page);
    } finally {
      await settings(page, { UITheme: before.UITheme, WorkbenchVisible: before.WorkbenchVisible });
    }
  });
}
