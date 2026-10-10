import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import type { Settings, Model } from '../src/api/types';

test('saving preferences preserves text entered after the submitted snapshot', async ({ page }) => {
  await page.goto('/#/account/preferences');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  const text = page.getByLabel('Answer preferences', { exact: true });
  const form = page.locator('.settings-form');
  await expect(text).toBeVisible();
  const submitted = 'The preference snapshot that was submitted.', later = 'A newer draft written while the save was pending.';
  let release!: () => void, started!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  const pending = new Promise<void>(resolve => { started = resolve; });
  await page.route(url => url.pathname === '/api/settings', async route => {
    if (route.request().method() !== 'POST') { await route.continue(); return; }
    const response = await route.fetch();
    expect(response.status()).toBe(200);
    started(); await gate; await route.fulfill({ response });
  });
  try {
    await text.fill(submitted);
    await form.getByRole('button', { name: 'Save', exact: true }).click();
    await pending;
    await text.fill(later);
    const returned = page.waitForResponse(response => new URL(response.url()).pathname === '/api/settings' && response.request().method() === 'POST');
    release(); await returned;
    await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeEnabled();
    const stored = await (await page.request.get('/api/settings')).json();
    const displayed = await text.inputValue();
    console.log(JSON.stringify({ submitted, stored: stored.AnswerPreferences, displayed, expectedDraft: later }));
    expect(stored.AnswerPreferences).toBe(submitted);
    expect(displayed).toBe(later);
    await form.getByRole('button', { name: 'Save', exact: true }).click();
    await expect.poll(async () => (await (await page.request.get('/api/settings')).json()).AnswerPreferences).toBe(later);
  } finally { release(); }
});

async function loginPreferences(page: Page) {
  await page.goto('/#/account/preferences');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  const text = page.getByLabel('Answer preferences', { exact: true }), form = page.locator('.settings-form');
  await expect(text).toBeVisible();
  return { text, form };
}
const getSettings = (page: Page): Promise<Settings> => page.request.get('/api/settings').then(response => response.json());
async function post(page: Page, path: string, body: object, version?: number) {
  const origin = new URL(page.url()).origin;
  const token = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${new URL(origin).port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: origin, 'X-CSRF-Token': decodeURIComponent(token) }, data: { ...body, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: version } } });
  expect(response.status(), await response.text()).toBe(200);
  return response.json();
}
async function holdSettings(page: Page, phase: 'GET' | 'POST', failed = false) {
  let release!: () => void, started!: () => void, claimed = false;
  const gate = new Promise<void>(resolve => { release = resolve; }), pending = new Promise<void>(resolve => { started = resolve; });
  const matches = (url: URL) => url.pathname === '/api/settings';
  await page.route(matches, async route => {
    if (route.request().method() !== phase || claimed) { await route.continue(); return; }
    claimed = true;
    const response = phase === 'POST' && !failed ? await route.fetch() : undefined;
    started(); await gate;
    if (failed) await route.fulfill({ status: 503, json: { code: 'unavailable' } });
    else if (response) await route.fulfill({ response });
    else await route.continue();
  });
  return { pending, release, finish: async () => {
    const returned = page.waitForResponse(response => matches(new URL(response.url())) && response.request().method() === phase);
    release(); await returned; await page.unroute(matches);
  } };
}

for (const later of ['', 'Edited before the version read returned.']) {
  test(`edits during the version read preserve ${later ? 'new text' : 'an intentionally empty draft'} and unrelated settings`, async ({ page }) => {
    const { text, form } = await loginPreferences(page);
    const submitted = 'Submitted before reading the latest version.';
    const held = await holdSettings(page, 'GET');
    try {
      await text.fill(submitted); await form.getByRole('button', { name: 'Save', exact: true }).click(); await held.pending;
      await text.fill(later);
      const before = await getSettings(page);
      const reduced = !before.ReducedMotion;
      await post(page, '/api/settings', { Settings: { ...before, ReducedMotion: reduced } }, before.Version);
      await held.finish();
      await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeEnabled();
      await expect(text).toHaveValue(later);
      expect(await getSettings(page)).toMatchObject({ AnswerPreferences: submitted, ReducedMotion: reduced });
      await form.getByRole('button', { name: 'Save', exact: true }).click();
      await expect.poll(async () => (await getSettings(page)).AnswerPreferences).toBe(later);
      expect((await getSettings(page)).ReducedMotion).toBe(reduced);
    } finally { held.release(); }
  });
}

for (const changeText of [false, true]) {
  test(`a pending save preserves a newly selected default model${changeText ? ' and edited text' : ''}`, async ({ page }, info) => {
    const { text, form } = await loginPreferences(page);
    const origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
    const prefix = `Preference ${info.project.name} ${changeText}`;
    const resource = await post(page, '/api/resources', { Name: prefix, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin + '/v1/chat/completions', AllowedOrigins: [origin], Models: ['A', 'B'].map(suffix => ({ Name: prefix + ' ' + suffix, ContextLimit: 262144, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } })) } });
    const models: Model[] = await (await page.request.get('/api/models')).json();
    const a = models.find(model => model.ResourceID === resource.ID && model.Name.endsWith(' A'))!;
    const b = models.find(model => model.ResourceID === resource.ID && model.Name.endsWith(' B'))!;
    await page.reload(); await expect(text).toBeVisible();
    const choose = async (name: string) => { await form.getByRole('combobox', { name: 'Model', exact: true }).click(); await page.getByRole('option', { name, exact: true }).click(); };
    await choose(a.Name); await text.fill('Submitted with model A.');
    const held = await holdSettings(page, 'POST');
    try {
      await form.getByRole('button', { name: 'Save', exact: true }).click(); await held.pending;
      await choose(b.Name);
      if (changeText) await text.fill('New text with model B.');
      await held.finish();
      await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeEnabled();
      await expect(form.getByRole('combobox', { name: 'Model', exact: true })).toContainText(b.Name);
      await expect(text).toHaveValue(changeText ? 'New text with model B.' : 'Submitted with model A.');
      expect(await getSettings(page)).toMatchObject({ DefaultModelID: a.ID, AnswerPreferences: 'Submitted with model A.' });
      await form.getByRole('button', { name: 'Save', exact: true }).click();
      await expect.poll(async () => (await getSettings(page)).DefaultModelID).toBe(b.ID);
    } finally { held.release(); }
  });
}

test('a failed save preserves the latest preference draft and retries it', async ({ page }) => {
  const { text, form } = await loginPreferences(page);
  const before = await getSettings(page);
  const held = await holdSettings(page, 'POST', true);
  try {
    await text.fill('Failed submitted snapshot.'); await form.getByRole('button', { name: 'Save', exact: true }).click(); await held.pending;
    await text.fill('Updated while the failed save was pending.'); await held.finish();
    await expect(form.getByRole('alert')).toContainText('service is unavailable');
    await expect(text).toHaveValue('Updated while the failed save was pending.');
    expect((await getSettings(page)).AnswerPreferences).toBe(before.AnswerPreferences);
    await form.getByRole('button', { name: 'Save', exact: true }).click();
    await expect.poll(async () => (await getSettings(page)).AnswerPreferences).toBe('Updated while the failed save was pending.');
    await expect(form.getByRole('alert')).toHaveCount(0);
  } finally { held.release(); }
});

test('external preference edits still conflict without overwriting either the server or local draft', async ({ page }) => {
  const { text, form } = await loginPreferences(page);
  await text.fill('Keep this local conflicting draft.');
  const before = await getSettings(page);
  await post(page, '/api/settings', { Settings: { ...before, AnswerPreferences: 'Another client saved this value.' } }, before.Version);
  let posted = 0;
  page.on('request', request => { if (new URL(request.url()).pathname === '/api/settings' && request.method() === 'POST') posted++; });
  await form.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(form.locator('.error-notice[data-code="conflict"]')).toBeVisible();
  await expect(text).toHaveValue('Keep this local conflicting draft.');
  expect(posted).toBe(0);
  expect((await getSettings(page)).AnswerPreferences).toBe('Another client saved this value.');
  await page.reload();
  await expect(text).toHaveValue('Another client saved this value.');
  await text.fill('Saved after explicitly refreshing.');
  await form.getByRole('button', { name: 'Save', exact: true }).click();
  await expect.poll(async () => (await getSettings(page)).AnswerPreferences).toBe('Saved after explicitly refreshing.');
});
