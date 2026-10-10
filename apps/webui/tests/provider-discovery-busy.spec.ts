import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';

const model = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
const original = model + '/v1', discovered = model + '/discovered/v1';
async function openProvider(page: Page) {
  await page.route('**/api/resources/discover-local', route => route.fulfill({ json: [
    { Name: 'Discovered fixture', Preset: 'custom', Endpoint: discovered, State: 'ready', Models: ['synthetic-model'], ResourceID: '' },
    { Name: 'Protected fixture', Preset: 'custom', Endpoint: model + '/protected/v1', State: 'authentication_required', Models: [], ResourceID: '' },
    { Name: 'Connected fixture', Preset: 'custom', Endpoint: model + '/connected/v1', State: 'ready', Models: [], ResourceID: 'already-connected' },
  ] }));
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  await page.goto('/#/resources');
  await page.getByRole('button', { name: 'Connect provider', exact: true }).first().click();
  const dialog = page.getByRole('dialog', { name: 'Connect provider', exact: true });
  await dialog.getByRole('button', { name: 'Custom service', exact: true }).click();
  const endpoint = dialog.getByLabel('Service URL', { exact: true }); await endpoint.fill(original);
  await expect(dialog.locator('.local-discovery').getByRole('button', { name: 'Use', exact: true })).toBeEnabled();
  return { dialog, endpoint };
}

test('local discovery cannot replace a provider while its connection test is pending', async ({ page }, info) => {
  let release!: () => void, started!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; }), pending = new Promise<void>(resolve => { started = resolve; });
  let testedEndpoint = '';
  await page.route('**/api/resources/test-connection', async route => {
    testedEndpoint = route.request().postDataJSON().Binding.Endpoint; started(); await gate;
    await route.fulfill({ json: { ModelCount: 2 } });
  });
  try {
    const { dialog, endpoint } = await openProvider(page);
    const choose = dialog.locator('.local-discovery').getByRole('button', { name: 'Use', exact: true });
    await expect(choose).toBeEnabled();
    await dialog.getByRole('button', { name: 'Test connection', exact: true }).click(); await pending;
    await expect(endpoint).toBeDisabled();
    await expect(dialog.getByRole('button', { name: 'Scan again', exact: true })).toBeDisabled();
    await expect(dialog.getByRole('button', { name: 'Configure', exact: true })).toBeDisabled();
    await expect(dialog.getByRole('button', { name: 'Connected', exact: true })).toBeDisabled();
    const enabledWhileTesting = await choose.isEnabled();
    expect.soft(enabledWhileTesting).toBe(false);
    if (enabledWhileTesting) await choose.click();
    release();
    await expect(dialog.getByRole('button', { name: 'Test connection', exact: true })).toBeEnabled();
    const displayedEndpoint = await endpoint.inputValue();
    await info.attach('connection-targets', { body: JSON.stringify({ testedEndpoint, displayedEndpoint, enabledWhileTesting }), contentType: 'application/json' });
    expect(testedEndpoint).toBe(original + '/chat/completions');
    await expect(endpoint).toHaveValue(original);
    await expect(dialog.locator('.ui-alert--positive')).toBeVisible();
    await expect(choose).toBeEnabled();
    await expect(dialog.getByRole('button', { name: 'Scan again', exact: true })).toBeEnabled();
    await choose.click();
    await expect(endpoint).toHaveValue(discovered);
    await expect(dialog.locator('.ui-alert--positive')).toHaveCount(0);
  } finally { release(); }
});

for (const operation of ['test', 'save']) {
  test(`discovery unlocks after a failed provider ${operation} and choosing a new target clears the error`, async ({ page }) => {
    const { dialog, endpoint } = await openProvider(page);
    let release!: () => void, started!: () => void;
    const gate = new Promise<void>(resolve => { release = resolve; }), pending = new Promise<void>(resolve => { started = resolve; });
    await page.route('**/api/resources/' + (operation === 'test' ? 'test-connection' : 'provider'), async route => {
      started(); await gate; await route.fulfill({ status: 503, json: { code: 'internal' } });
    });
    try {
      await dialog.getByRole('button', { name: operation === 'test' ? 'Test connection' : 'Connect provider', exact: true }).click(); await pending;
      for (const name of ['Use', 'Configure', 'Scan again', 'Connected']) await expect(dialog.getByRole('button', { name, exact: true })).toBeDisabled();
      release();
      const error = dialog.locator('.error-notice[data-code="internal"]:visible'); await expect(error).toBeVisible();
      for (const name of ['Use', 'Configure', 'Scan again']) await expect(dialog.getByRole('button', { name, exact: true })).toBeEnabled();
      await expect(dialog.getByRole('button', { name: 'Connected', exact: true })).toBeDisabled();
      await dialog.getByRole('button', { name: 'Configure', exact: true }).click();
      await expect(endpoint).toHaveValue(model + '/protected/v1');
      await expect(dialog.getByLabel('API key', { exact: true })).toBeEnabled();
      await expect(error).toHaveCount(0);
    } finally { release(); }
  });
}

test('discovery remains locked through provider save and model refresh', async ({ page }, info) => {
  const { dialog } = await openProvider(page);
  const name = `Saved provider ${info.project.name}-${Date.now()}`;
  await dialog.getByText('More configuration', { exact: true }).click();
  await dialog.getByLabel('Name', { exact: true }).fill(name);
  let releaseSave!: () => void, releaseRefresh!: () => void, saving!: () => void, refreshing!: () => void;
  const saveGate = new Promise<void>(resolve => { releaseSave = resolve; }), refreshGate = new Promise<void>(resolve => { releaseRefresh = resolve; });
  const saveStarted = new Promise<void>(resolve => { saving = resolve; }), refreshStarted = new Promise<void>(resolve => { refreshing = resolve; });
  await page.route('**/api/resources/provider', async route => { const response = await route.fetch(); expect(response.status()).toBe(200); saving(); await saveGate; await route.fulfill({ response }); });
  await page.route('**/api/resources/refresh-models', async route => { const response = await route.fetch(); expect(response.status()).toBe(200); refreshing(); await refreshGate; await route.fulfill({ response }); });
  try {
    await dialog.getByRole('button', { name: 'Connect provider', exact: true }).click(); await saveStarted;
    for (const label of ['Use', 'Configure', 'Scan again']) await expect(dialog.getByRole('button', { name: label, exact: true })).toBeDisabled();
    releaseSave(); await refreshStarted;
    for (const label of ['Use', 'Configure', 'Scan again']) await expect(dialog.getByRole('button', { name: label, exact: true })).toBeDisabled();
    releaseRefresh(); await expect(dialog).toHaveCount(0);
    await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Connect provider', exact: true }).first().click();
    await expect(dialog.getByRole('button', { name: 'Use', exact: true })).toBeEnabled();
  } finally { releaseSave(); releaseRefresh(); }
});
