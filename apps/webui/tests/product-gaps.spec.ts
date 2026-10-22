import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
const base = `http://127.0.0.1:${process.env.MOLR_TEST_PORT ?? 5193}`;
const modelOrigin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
async function login(page: Page) {
  await page.goto('/#/resources');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
}
async function post(page: Page, path: string, data: object, version = 0) {
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${new URL(base).port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: version } } });
  expect(response.status(), await response.text()).toBe(200); return response.json();
}
async function settings(page: Page, patch: object) {
  const current = await (await page.request.get('/api/settings')).json();
  await post(page, '/api/settings', { Settings: { ...current, ...patch } }, current.Version);
}
async function assistant(page: Page, name: string) {
  const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: modelOrigin, AllowedOrigins: [modelOrigin], Models: [{ Name: name, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 1000000, CachedInputMicrosPerMillion: 0 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  return post(page, '/api/assistants', { Name: name, ModelID: models.find((m: { ResourceID: string }) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
}

test('cloud presets test, save, discover and reopen for editing', async ({ page }) => {
  test.setTimeout(90_000);
  await login(page);
  for (const [preset, label] of [['bailian', 'Bailian'], ['zhipu', 'Zhipu'], ['moonshot', 'Moonshot AI']]) {
    await page.locator('.resources-header').getByRole('button', { name: 'More', exact: true }).click();
    await page.getByRole('menuitem', { name: 'Connect provider', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Connect provider', exact: true });
    await dialog.getByRole('button', { name: label, exact: true }).click();
    await dialog.getByLabel('API key', { exact: true }).fill('test-provider-secret');
    await dialog.getByText('More configuration', { exact: true }).click();
    const name = `${label} ${Date.now()}`;
    await dialog.getByRole('textbox', { name: 'Name', exact: true }).fill(name);
    await dialog.getByRole('textbox', { name: 'Service URL', exact: true }).fill(`${modelOrigin}/private/v1`);
    await dialog.getByRole('button', { name: 'Test connection', exact: true }).click();
    await expect(dialog).toContainText('2');
    await dialog.getByRole('button', { name: 'Connect provider', exact: true }).click();
    await expect(dialog).not.toBeVisible();
    const resources = await (await page.request.get('/api/resources')).json();
    const resource = resources.Items.find((r: { Name: string }) => r.Name === name);
    const connection = await (await page.request.get(`/api/resources/${resource.ID}/connection`)).json();
    expect(connection.PricingProvider).toBe(preset);
    const models = await (await page.request.get('/api/models')).json();
    expect(models.filter((m: { ResourceID: string }) => m.ResourceID === resource.ID)).toHaveLength(2);
    const row = page.locator('.resource-row').filter({ has: page.getByRole('button', { name, exact: true }) });
    await row.getByRole('button', { name: 'More', exact: true }).click();
    await page.getByRole('menuitem', { name: 'Edit provider', exact: true }).click();
    const edit = page.getByRole('dialog', { name: 'Edit provider', exact: true });
    await expect(edit.getByRole('textbox', { name: 'Name', exact: true })).toHaveValue(name);
    await expect(edit.getByText('API key configured', { exact: true })).toBeVisible();
    await edit.getByRole('textbox', { name: 'Name', exact: true }).fill(`${name} edited`);
    await edit.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(edit).not.toBeVisible();
    expect((await (await page.request.get(`/api/resources/${resource.ID}/connection`)).json()).PricingProvider).toBe(preset);
  }
});

test('search locates old messages across pages, preserves links and handles missing or foreign targets', async ({ page }, info) => {
  test.setTimeout(120_000);
  await login(page);
  await settings(page, { UITheme: 'molr.official@1.0.0' });
  const a = await assistant(page, `Search ${info.project.name} ${Date.now()}`);
  const needle = `Needle-${crypto.randomUUID()}`;
  const target = await post(page, '/api/conversations/messages', { ConversationID: a.ConversationID, Content: { Kind: 'text', Text: needle } });
  let middle: any;
  for (let i = 1; i <= 110; i++) {
    const value = await post(page, '/api/conversations/messages', { ConversationID: a.ConversationID, Content: { Kind: 'text', Text: `History row ${i}` } });
    if (i === 55) middle = value;
  }
  const readRequests: string[] = [];
  page.on('request', request => { if (request.url().endsWith('/api/conversations/read')) readRequests.push(request.url()); });
  await page.evaluate(() => window.dispatchEvent(new Event('molr:search')));
  await page.getByRole('dialog', { name: 'Search', exact: true }).getByRole('searchbox').fill(needle);
  await page.locator('[data-search-item]').filter({ hasText: needle }).click();
  const targetRow = page.locator(`[data-message-id="${target.ID}"]`);
  await expect(page).toHaveURL(new RegExp(`message=${target.ID}`));
  await expect(targetRow).toHaveClass('chat-message-target');
  await expect(targetRow).toBeInViewport();
  await expect(page.locator('[data-message-id]')).toHaveCount(111);
  expect(readRequests).toHaveLength(0);
  await page.reload();
  await expect(targetRow).toBeInViewport();
  await expect(targetRow).toHaveClass('chat-message-target');
  // A later incoming message must not steal the scroll position.
  await post(page, '/api/conversations/messages', { ConversationID: a.ConversationID, Content: { Kind: 'text', Text: 'New while inspecting history' } });
  await expect(page.locator('[data-message-id]')).toHaveCount(112);
  await expect(targetRow).toBeInViewport();
  expect(readRequests).toHaveLength(0);
  await page.evaluate(({ id, message }) => { location.hash = `/assistants/${id}?message=${message}`; }, { id: a.ID, message: middle.ID });
  await expect(page.locator(`[data-message-id="${middle.ID}"]`)).toBeInViewport();
  await page.goBack();
  await expect(targetRow).toBeInViewport();
  await page.reload();
  await expect(targetRow).toBeInViewport();
  await expect(targetRow).toHaveClass('chat-message-target');
  for (const palette of ['forest', 'ocean', 'iris', 'sand', 'rose', 'monochrome']) {
    for (const mode of ['light', 'dark']) {
      await page.evaluate(({ palette, mode }) => { document.documentElement.dataset.palette = palette; document.documentElement.dataset.theme = mode; }, { palette, mode });
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      await expect(targetRow).toBeInViewport();
      await page.screenshot({ path: info.outputPath(`search-${palette}-${mode}.png`) });
    }
  }
  const other = await assistant(page, `Other ${Date.now()}`);
  await page.goto(`/#/assistants/${other.ID}?message=${target.ID}`);
  await expect(page.getByText('This message does not exist or is no longer accessible.')).toBeVisible();
  await expect(page.getByText(needle, { exact: true })).toHaveCount(0);
  await page.goto(`/#/assistants/${a.ID}?message=missing-message`);
  await expect(page.getByText('This message does not exist or is no longer accessible.')).toBeVisible();
  await page.getByRole('button', { name: 'Back to bottom', exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/assistants/${a.ID}$`));
  await expect(page.getByRole('log').getByText('New while inspecting history', { exact: true })).toBeInViewport();
  await settings(page, { UITheme: 'molr.official@1.0.0' });
});

test('theme error recovery opens the current theme settings route', async ({ page }) => {
  await login(page);
  await page.route('**/src/interfaces/official/features/Plans.tsx*', route => route.fulfill({ contentType: 'application/javascript', body: 'export function Plans() { throw new Error("Synthetic page render failure"); }' }));
  await page.goto('/#/plans');
  await page.reload();
  await expect(page.getByText('This theme could not load', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Theme settings', exact: true }).click();
  await expect(page).toHaveURL(/#\/account\/theme$/);
  await expect(page.getByRole('combobox', { name: 'Theme', exact: true })).toBeVisible();
  await settings(page, { UITheme: 'molr.official@1.0.0' });
});
