import { fixtureTheme, installThemeFixture } from './theme-fixture';
import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

// Exercise the production graph: the development server does not tree-shake.
test.skip(process.env.MOLR_E2E_PREVIEW !== '1', 'Requires a production WebUI build and MOLR_E2E_PREVIEW=1');
const port = process.env.MOLR_TEST_PORT ?? '5193', base = `http://127.0.0.1:${port}`;

test('the assistant list defers secondary pages and panels until navigation', async ({ page }) => {
  const scripts: string[] = [];
  page.on('request', request => { if (request.resourceType() === 'script') scripts.push(new URL(request.url()).pathname); });
  await page.goto('/#/assistants'); await signIn(page);
  await expect(page.locator('.assistant-workspace')).toBeVisible();
  expect(scripts.filter(path => /\/(Plans|Resources|Settings|Workbench|Browser|Terminals|ShellTasks|WorkspaceFiles|AssistantInfo|TerminalSession|WeixinChannel)-/.test(path))).toEqual([]);
  await page.evaluate(() => { location.hash = '#/resources'; });
  await expect.poll(() => scripts.some(path => /\/Resources-/.test(path))).toBe(true);
  await expect(page.getByRole('navigation', { name: 'Molr', exact: true })).toBeVisible();
  await page.evaluate(() => { location.hash = '#/plans'; });
  await expect(page.getByRole('heading', { name: 'Plans', exact: true })).toBeVisible();
  expect(scripts.some(path => /\/Plans-/.test(path))).toBe(true);
});
async function signIn(page: Page) {
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr', exact: true })).toBeVisible();
}
async function post(page: Page, path: string, data: object, version?: number) {
  const csrf = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: version } } });
  expect(response.status(), await response.text()).toBe(200);
  return response.json();
}

async function setTheme(page: Page, theme: string) {
  const settings = await (await page.request.get('/api/settings')).json();
  if (settings.UITheme === theme) return;
  return post(page, '/api/settings', { Settings: { ...settings, UITheme: theme } }, settings.Version);
}

test('login defers business code and a slow page keeps the workspace navigation available', async ({ page }) => {
  const scripts: string[] = [];
  page.on('request', request => { if (request.resourceType() === 'script') scripts.push(new URL(request.url()).pathname); });
  await page.goto('/');
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
  expect(scripts.some(path => /\/(OfficialPage|AdminSettings|Plugins|PluginFrame)-/.test(path))).toBe(false);
  let release!: () => void;
  const ready = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/assets/OfficialPage-*.js', async route => { await ready; await route.continue(); });
  await signIn(page);
  await expect(page.getByRole('navigation', { name: 'Molr', exact: true })).toBeVisible();
  await expect(page.getByRole('status', { name: 'Loading…', exact: true })).toBeVisible();
  release();
  await expect(page.locator('.assistant-workspace')).toBeVisible();
  expect(scripts.some(path => /\/AdminSettings-/.test(path))).toBe(false);
  await page.evaluate(() => { location.hash = '#/settings/users'; });
  await expect(page.locator('.settings-workspace')).toBeVisible();
  expect(scripts.some(path => /\/AdminSettings-/.test(path))).toBe(true);
});

test('a failed page download preserves navigation and retry reloads a working page', async ({ page }) => {
  await page.goto('/');
  await page.route('**/assets/OfficialPage-*.js', route => route.abort('failed'), { times: 1 });
  await signIn(page);
  await expect(page.getByRole('heading', { name: 'This theme could not load', exact: true })).toBeVisible();
  await expect(page.getByRole('navigation', { name: 'Molr', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(page.locator('.assistant-workspace')).toBeVisible();
});

test('restoring the default theme recovers a transient default-page download failure', async ({ page }) => {
  await page.goto('/');
  await page.route('**/assets/OfficialPage-*.js', route => route.abort('failed'), { times: 1 });
  await signIn(page);
  await expect(page.getByRole('heading', { name: 'This theme could not load', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Restore default theme', exact: true }).click();
  await expect(page.locator('.assistant-workspace')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'This theme could not load', exact: true })).toHaveCount(0);
});

for (const saveFails of [false, true]) {
  test(`default recovery ${saveFails ? 'stays in-session if saving fails' : 'persists before reloading a different theme'}`, async ({ page }) => {
    await page.goto('/'); await signIn(page);
    await expect(page.locator('.assistant-workspace')).toBeVisible();
    await installThemeFixture(page);
    await setTheme(page, fixtureTheme);
    try {
      await page.route('**/assets/PluginFrame-*.js', route => route.fulfill({ contentType: 'application/javascript', body: 'export function PluginFrame() { throw new Error("Synthetic theme render failure"); }' }));
      await page.goto('/#/plans'); await page.reload();
      await expect(page.getByRole('heading', { name: 'This theme could not load', exact: true })).toBeVisible();
      await page.evaluate(() => { (window as any).recoveryMarker = true; });
      if (saveFails) await page.route('**/api/settings', route => route.request().method() === 'POST' ? route.fulfill({ status: 503, json: { code: 'unavailable' } }) : route.continue());
      await page.getByRole('button', { name: 'Restore default theme', exact: true }).click();
      await expect(page.locator('.plans-workspace')).toBeVisible();
      expect(await page.evaluate(() => (window as any).recoveryMarker)).toBe(saveFails ? true : undefined);
      const saved = await (await page.request.get('/api/settings')).json();
      expect(saved.UITheme).toBe(saveFails ? fixtureTheme : 'molr.official@1.0.0');
    } finally { await page.unroute('**/api/settings'); await setTheme(page, 'molr.official@1.0.0'); }
  });
}

test('persistent page download failure remains recoverable without a reload loop', async ({ page }) => {
  await page.goto('/');
  await page.route('**/assets/OfficialPage-*.js', route => route.abort('failed'));
  await signIn(page);
  await expect(page.getByRole('heading', { name: 'This theme could not load', exact: true })).toBeVisible();
  let documents = 0;
  page.on('request', request => { if (request.isNavigationRequest() && request.frame() === page.mainFrame()) documents++; });
  await page.getByRole('button', { name: 'Restore default theme', exact: true }).click();
  await expect.poll(() => documents).toBe(1);
  await expect(page.getByRole('heading', { name: 'This theme could not load', exact: true })).toBeVisible();
  // A persistent asset failure must not start an automatic refresh loop.
  await page.waitForTimeout(500);
  expect(documents).toBe(1);
  await page.unroute('**/assets/OfficialPage-*.js');
  await page.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(page.locator('.assistant-workspace')).toBeVisible();
});

test('restoring after an admin page download failure retains the chat draft', async ({ page }, info) => {
  await page.goto('/'); await signIn(page);
  const origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? '18882'}`;
  const name = `Draft recovery ${info.project.name}`;
  const provider = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } }] } });
  const model = (await (await page.request.get('/api/models')).json()).find((item: { ResourceID: string }) => item.ResourceID === provider.ID);
  const assistant = await post(page, '/api/assistants', { Name: name, ModelID: model.ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  await page.evaluate(id => { location.hash = `#/assistants/${id}`; }, assistant.ID);
  const composer = page.getByRole('textbox', { name: 'Message your assistant', exact: true });
  await composer.fill('Keep this unsent draft through recovery.');
  await page.route('**/assets/AdminSettings-*.js', route => route.abort('failed'), { times: 1 });
  await page.evaluate(() => { location.hash = '#/settings/users'; });
  await expect(page.getByRole('heading', { name: 'This theme could not load', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Restore default theme', exact: true }).click();
  await expect(page.locator('.settings-workspace')).toBeVisible();
  await page.evaluate(id => { location.hash = `#/assistants/${id}`; }, assistant.ID);
  await expect(composer).toHaveValue('Keep this unsent draft through recovery.');
});

test('deferred official chat still loads Markdown, formulas and their styles', async ({ page }, info) => {
  await page.goto('/'); await signIn(page);
  const origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? '18882'}`;
  const name = `Rich text deferred ${info.project.name}`;
  const provider = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0 } }] } });
  const model = (await (await page.request.get('/api/models')).json()).find((item: { ResourceID: string }) => item.ResourceID === provider.ID);
  const assistant = await post(page, '/api/assistants', { Name: name, ModelID: model.ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  await page.evaluate(id => { location.hash = `#/assistants/${id}`; }, assistant.ID);
  await expect(page.getByRole('log').getByRole('heading', { name: 'Runtime rich text reply', exact: true })).toBeVisible({ timeout: 20000 });
  await expect(page.getByRole('log').locator('.katex').first()).toBeVisible();
  await expect(page.getByRole('log').locator('.katex').first()).toHaveCSS('font-family', /KaTeX_Main/);
  await expect(page.getByRole('log').getByRole('table')).toBeVisible();
  await expect(page.getByRole('log').locator('pre')).toHaveCSS('overflow-x', 'auto');
});

for (const route of ['plans', 'account/general', 'workbench']) {
  test(`${route} defers assistant code and queries until entering assistants`, async ({ page }) => {
    const scripts: string[] = [], queries: string[] = [];
    page.on('request', request => {
      const path = new URL(request.url()).pathname;
      if (request.resourceType() === 'script') scripts.push(path);
      if (path.startsWith('/api/')) queries.push(path);
    });
    await page.goto(`/#/${route}`); await signIn(page);
    const chunk = route === 'plans' ? 'Plans' : route === 'workbench' ? 'Workbench' : 'Settings';
    await expect.poll(() => scripts.some(path => path.includes(`/${chunk}-`))).toBe(true);
    await expect(page.locator(route === 'plans' ? '.plans-workspace' : route === 'workbench' ? '.workspace-main section.page' : '.settings-workspace')).toBeVisible();
    expect(scripts.filter(path => /\/(AssistantsPage|WeixinChannel)-/.test(path))).toEqual([]);
    expect(queries.filter(path => path === '/api/models' || path === '/api/assistants/activity')).toEqual([]);
    await page.evaluate(() => { location.hash = '#/assistants'; });
    await expect(page.locator('.assistant-workspace')).toBeVisible();
    expect(scripts.some(path => /\/AssistantsPage-/.test(path))).toBe(true);
    await expect.poll(() => queries.includes('/api/models')).toBe(true);
    await expect.poll(() => queries.includes('/api/assistants/activity')).toBe(true);
    expect(scripts.some(path => /\/WeixinChannel-/.test(path))).toBe(false);
    const filter = page.getByRole('searchbox', { name: 'Search assistants', exact: true });
    await filter.fill('Remember my filter');
    await page.evaluate(() => { location.hash = '#/plans'; });
    await expect(page.locator('.assistant-workspace')).toHaveCount(0);
    await page.evaluate(() => { location.hash = '#/assistants'; });
    await expect(filter).toHaveValue('Remember my filter');
  });
}

test('search opens task details across a delayed assistant page and the link survives reload', async ({ page }, info) => {
  await page.goto('/#/plans'); await signIn(page);
  const origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? '18882'}`;
  const name = `Search lazy ${info.project.name}`;
  const provider = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } }] } });
  const model = (await (await page.request.get('/api/models')).json()).find((item: { ResourceID: string }) => item.ResourceID === provider.ID);
  const assistant = await post(page, '/api/assistants', { Name: name, ModelID: model.ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  const goal = `Search lazy task ${info.project.name}`;
  const task = await post(page, '/api/tasks', { AssistantID: assistant.ID, Goal: goal });
  await expect(page.locator('.plans-workspace')).toBeVisible();
  await page.evaluate(() => window.dispatchEvent(new Event('molr:search')));
  const search = page.getByRole('dialog', { name: 'Search', exact: true });
  await search.getByRole('searchbox').fill(goal);
  let release!: () => void;
  const ready = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/assets/AssistantsPage-*.js', async route => { await ready; await route.continue(); });
  try {
    await search.getByText(goal, { exact: true }).first().click();
    await expect(page).toHaveURL(new RegExp(`/assistants/${assistant.ID}\\?panel=tasks&task=${task.ID}$`));
    await expect(search).not.toBeVisible();
    if (info.project.name === 'desktop') await expect(page.getByRole('navigation', { name: 'Molr', exact: true })).toBeVisible();
    await expect(page.getByRole('status', { name: 'Loading…', exact: true })).toBeVisible();
  } finally { release(); }
  const detail = page.locator('.plans-panel-detail');
  await expect(detail).toContainText(goal);
  await page.reload();
  await expect(detail).toContainText(goal);
  await page.evaluate(id => { location.hash = `#/assistants/${id}?panel=channel`; }, assistant.ID);
  await expect(page.locator('.chat-channel-drawer')).toBeVisible();
});
