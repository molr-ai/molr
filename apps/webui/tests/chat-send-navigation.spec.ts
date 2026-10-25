import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import type { Assistant, Message, Model } from '@molr/sdk';

const port = process.env.MOLR_TEST_PORT ?? '5193', base = `http://127.0.0.1:${port}`;
async function post<T>(page: Page, path: string, data: object): Promise<T> {
  const csrf = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } } });
  expect(response.status(), await response.text()).toBe(200); return response.json();
}

async function setup(page: Page, suffix: string) {
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const name = `Send navigation ${suffix}-${Date.now()}`, endpoint = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post<{ ID: string }>(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: endpoint, AllowedOrigins: [endpoint], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1000000, CachedInputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 1000000 } }] } });
  const models: Model[] = await (await page.request.get('/api/models')).json();
  const assistant = await post<Assistant>(page, '/api/assistants', { Name: name, ModelID: models.find(model => model.ResourceID === resource.ID)!.ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  await page.goto(`/#/assistants/${assistant.ID}`);
  await expect(page.getByRole('textbox', { name: 'Message your assistant', exact: true })).toBeEnabled();
  return assistant;
}

declare global { interface Window { completedSends: number } }
async function delaySend(page: Page) {
  let release!: () => void, committed!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; }), ready = new Promise<void>(resolve => { committed = resolve; });
  await page.evaluate(async () => {
    window.completedSends = 0;
    const path = '/@id/@molr/sdk', { onMutation } = await import(path);
    onMutation((path: string) => { if (path === '/api/conversations/messages') window.completedSends++; });
  });
  await page.route('**/api/conversations/messages', async route => {
    const response = await route.fetch(); committed(); await gate; await route.fulfill({ response });
  });
  return { ready, release, async finish() {
    release(); await expect.poll(() => page.evaluate(() => window.completedSends)).toBe(1);
    await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
  } };
}
async function draftText(page: Page, id: string) {
  return page.evaluate(id => {
    const key = Object.keys(sessionStorage).find(key => key.startsWith('molr-chat-draft:') && key.endsWith(':' + id));
    return key ? JSON.parse(sessionStorage.getItem(key) ?? '{}').text : null;
  }, id);
}

for (const destination of ['/assistants', '/account/theme']) {
  test('completing a delayed send preserves navigation to ' + destination, async ({ page }, info) => {
    const assistant = await setup(page, info.project.name);
    const pending = await delaySend(page);
    try {
      const marker = 'Message sent while navigating away';
      await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).fill(marker);
      await page.getByRole('button', { name: 'Send', exact: true }).click();
      await pending.ready;
      if (destination === '/assistants' && info.project.name === 'mobile') {
        await page.getByRole('button', { name: 'Back to assistants', exact: true }).click();
      } else await page.evaluate(path => { location.hash = path; }, destination);
      await expect(page).toHaveURL(base + '/#' + destination);
      await pending.finish();
      await expect.poll(() => draftText(page, assistant.ID)).toBe('');
      await expect(page).toHaveURL(base + '/#' + destination);
      const messages = await (await page.request.get(`/api/conversations/${assistant.ConversationID}/messages`)).json();
      expect(messages.Items.filter((message: { Content: { Text: string } }) => message.Content.Text === marker)).toHaveLength(1);
    } finally { pending.release(); }
  });
}

for (const changedDraft of [false, true]) {
  test(`returning before send completion preserves the current chat and ${changedDraft ? 'new draft' : 'clears the sent draft'}`, async ({ page }, info) => {
    const assistant = await setup(page, info.project.name), pending = await delaySend(page);
    const input = page.getByRole('textbox', { name: 'Message your assistant', exact: true });
    try {
      await input.fill('Original sent draft'); await page.getByRole('button', { name: 'Send', exact: true }).click(); await pending.ready;
      await page.evaluate(() => { location.hash = '/account/theme'; });
      await expect(page.getByRole('heading', { name: 'Theme', exact: true })).toBeVisible();
      await page.evaluate(id => { location.hash = '/assistants/' + id; }, assistant.ID);
      await expect(input).toBeEnabled();
      if (changedDraft) await input.fill('New draft after returning');
      await pending.finish();
      await expect(page).toHaveURL(base + '/#/assistants/' + assistant.ID);
      await expect(input).toHaveValue(changedDraft ? 'New draft after returning' : '');
      expect(await draftText(page, assistant.ID)).toBe(changedDraft ? 'New draft after returning' : '');
    } finally { pending.release(); }
  });
}

for (const changeTarget of [false, true]) {
  test(`send completion ${changeTarget ? 'preserves a newer search target' : 'returns the unchanged search target to latest messages'}`, async ({ page }, info) => {
    const assistant = await setup(page, info.project.name);
    const targets: Message[] = [];
    for (const text of ['First search target', 'Second search target']) targets.push(await post<Message>(page, '/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: text } }));
    const route = (id: string) => `/assistants/${assistant.ID}?message=${id}`;
    await page.evaluate(path => { location.hash = path; }, route(targets[0].ID));
    await expect(page.locator('.chat-message-target')).toHaveAttribute('data-message-id', targets[0].ID);
    const pending = await delaySend(page);
    try {
      await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).fill('Message from located history');
      await page.getByRole('button', { name: 'Send', exact: true }).click(); await pending.ready;
      if (changeTarget) {
        await page.evaluate(path => { location.hash = path; }, route(targets[1].ID));
        await expect(page.locator('.chat-message-target')).toHaveAttribute('data-message-id', targets[1].ID);
      }
      await pending.finish();
      await expect(page).toHaveURL(base + '/#' + (changeTarget ? route(targets[1].ID) : `/assistants/${assistant.ID}`));
      await expect(page.getByRole('textbox', { name: 'Message your assistant', exact: true })).toHaveValue('');
      if (changeTarget) await expect(page.locator('.chat-message-target')).toHaveAttribute('data-message-id', targets[1].ID);
      else await expect(page.locator('.chat-message-target')).toHaveCount(0);
    } finally { pending.release(); }
  });
}
