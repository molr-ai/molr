import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import type { Model, Settings } from '@molr/sdk';

const getSettings = (page: Page): Promise<Settings> => page.request.get('/api/settings').then(response => response.json());
async function post(page: Page, path: string, body: object, version?: number) {
  const origin = new URL(page.url()).origin;
  const token = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${new URL(origin).port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: origin, 'X-CSRF-Token': decodeURIComponent(token) }, data: { ...body, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: version } } });
  expect(response.status(), await response.text()).toBe(200);
  return response.json();
}
async function openModels(page: Page) {
  await page.goto('/#/resources');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.locator('.resources-header')).toBeVisible();
  const origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const name = `Default model ${Date.now()}`;
  const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin + '/v1/chat/completions', AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 262144, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } }] } });
  const models: Model[] = await (await page.request.get('/api/models')).json();
  await page.reload();
  await openDefaultModel(page);
  await expect(page.getByRole('combobox', { name: 'Default model', exact: true })).toBeEnabled();
  return models.find(model => model.ResourceID === resource.ID)!;
}
async function openDefaultModel(page: Page) {
  await expect(page.getByRole('combobox', { name: 'Default model', exact: true })).toHaveCount(0);
  await page.locator('.resources-header').getByRole('button', { name: 'More', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Default model', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Default model', exact: true })).toBeVisible();
}
async function choose(page: Page, name: string) {
  if (!await page.getByRole('dialog', { name: 'Default model', exact: true }).count()) await openDefaultModel(page);
  await page.getByRole('combobox', { name: 'Default model', exact: true }).click();
  await page.getByRole('option', { name, exact: true }).click();
}

test('default model saves on the resources page, survives reload and can be cleared', async ({ page }) => {
  const model = await openModels(page), before = await getSettings(page);
  await choose(page, model.Name);
  await expect.poll(async () => (await getSettings(page)).DefaultModelID).toBe(model.ID);
  for (const field of ['Language', 'Timezone', 'Theme', 'Palette'] as const) expect((await getSettings(page))[field]).toEqual(before[field]);
  await page.reload();
  await openDefaultModel(page);
  await expect(page.getByRole('combobox', { name: 'Default model', exact: true })).toContainText(model.Name);
  await page.goto('/#/account/general');
  await expect(page.locator('.settings-nav > button').filter({ hasText: /^Preferences$/ })).toHaveCount(0);
  expect(await getSettings(page)).not.toHaveProperty('AnswerPreferences');
  await page.goto('/#/assistants');
  await page.getByRole('button', { name: 'Hire new assistant', exact: true }).first().click();
  await expect(page.getByRole('dialog').getByRole('combobox', { name: 'Model', exact: true })).toContainText(model.Name);
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.goto('/#/resources'); await choose(page, 'None');
  await expect.poll(async () => (await getSettings(page)).DefaultModelID).toBe('');
});

test('default model save failure restores the previous selection and can be retried', async ({ page }) => {
  const model = await openModels(page), before = await getSettings(page);
  await page.route('**/api/settings', route => route.request().method() === 'POST' ? route.fulfill({ status: 503, json: { code: 'unavailable' } }) : route.continue());
  await choose(page, model.Name);
  await expect(page.locator('.ui-toast--danger')).toBeVisible();
  expect((await getSettings(page)).DefaultModelID).toBe(before.DefaultModelID);
  await expect(page.getByRole('combobox', { name: 'Default model', exact: true })).not.toContainText(model.Name);
  await page.unroute('**/api/settings'); await choose(page, model.Name);
  await expect.poll(async () => (await getSettings(page)).DefaultModelID).toBe(model.ID);
});

test('default model merges fresh settings and prevents a second selection during save', async ({ page }) => {
  const model = await openModels(page);
  let release!: () => void, started!: () => void, claimed = false;
  const gate = new Promise<void>(resolve => { release = resolve; }), pending = new Promise<void>(resolve => { started = resolve; });
  await page.route('**/api/settings', async route => {
    if (route.request().method() !== 'GET' || claimed) { await route.continue(); return; }
    claimed = true; started(); await gate; await route.continue();
  });
  try {
    await choose(page, model.Name); await pending;
    await expect(page.getByRole('combobox', { name: 'Default model', exact: true })).toBeDisabled();
    const before = await getSettings(page);
    await post(page, '/api/settings', { Settings: { ...before, ReducedMotion: !before.ReducedMotion } }, before.Version);
    release();
    await expect.poll(async () => (await getSettings(page)).DefaultModelID).toBe(model.ID);
    expect((await getSettings(page)).ReducedMotion).toBe(!before.ReducedMotion);
    await expect(page.getByRole('combobox', { name: 'Default model', exact: true })).toBeEnabled();
    const current = await getSettings(page);
    await post(page, '/api/settings', { Settings: { ...current, ReducedMotion: before.ReducedMotion } }, current.Version);
  } finally { release(); }
});
