import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import type { Assistant } from '../src/api/types';
const base = `http://127.0.0.1:${process.env.MOLR_TEST_PORT ?? process.env.MOLR_E2E_PORT ?? 5193}`;
const modelOrigin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
async function post(page: Page, path: string, data: object) {
  const csrf = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${new URL(base).port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } } });
  expect(response.status(), await response.text()).toBe(200);
  return response.json();
}

test('chat shares queries, batches sidebar activity and scopes live messages to their conversation', async ({ page }, info) => {
  test.setTimeout(150_000);
  // Authenticate once, seed through real services before mounting the workspace.
  await page.goto('/');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect.poll(async () => (await page.context().cookies()).some(cookie => cookie.name === `molr_dev_session_p${new URL(base).port}`)).toBe(true);
  const provider = await post(page, '/api/resources', { Name: `Sync ${info.project.name}`, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: modelOrigin, AllowedOrigins: [modelOrigin], Models: [{ Name: 'sync', SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0 } }] } });
  const model = (await (await page.request.get('/api/models')).json()).find((row: { ResourceID: string }) => row.ResourceID === provider.ID);
  const assistants: Assistant[] = [];
  for (let i = 0; i < 8; i++) assistants.push(await post(page, '/api/assistants', { Name: `Sync ${info.project.name} ${i}`, ModelID: model.ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } }));
  // All eight born executions must finish, including those queued behind other work.
  // Wait for born publications, then mark them read so opening a chat does
  // not legitimately invalidate the conversation list during measurement.
  await expect.poll(async () => {
    const rows = (await (await page.request.get('/api/conversations?limit=200')).json()).Items;
    return assistants.every(a => rows.some((c: { ID: string; LastSequence: number }) => c.ID === a.ConversationID && c.LastSequence > 0));
  }, { timeout: 60_000 }).toBe(true);
  const conversations = (await (await page.request.get('/api/conversations?limit=200')).json()).Items;
  const csrf = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${new URL(base).port}`)!.value;
  for (const a of assistants) {
    const c = conversations.find((row: { ID: string }) => row.ID === a.ConversationID);
    const response = await page.request.post('/api/conversations/read', { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ConversationID: c.ID, ThroughSequence: c.LastSequence } });
    expect(response.status()).toBe(200);
  }
  // Finish asynchronous born replies before measuring initial reads.
  await expect.poll(async () => {
    const statuses = await Promise.all(assistants.map(a => page.request.get(`/api/assistants/${a.ID}/status`).then(r => r.json())));
    return statuses.every(status => status.State === 'idle');
  }, { timeout: 60_000 }).toBe(true);
  await page.goto('about:blank');
  await page.addInitScript(() => {
    const Original = window.EventSource;
    (window as any).__replayedReads = [];
    window.EventSource = class extends Original {
      constructor(url: string | URL, options?: EventSourceInit) {
        super(url, options);
        this.addEventListener('update', event => {
          const value = JSON.parse((event as MessageEvent).data);
          if (value.Topic === 'conversation.read') (window as any).__replayedReads.push(value.ConversationID);
        });
      }
    };
  });
  let releaseEvents!: () => void;
  const eventsReady = new Promise<void>(resolve => { releaseEvents = resolve; });
  await page.route('**/api/events/stream*', async route => { await eventsReady; await route.continue(); });
  const counts = new Map<string, number>();
  page.on('request', request => {
    if (request.method() !== 'GET') return;
    const url = new URL(request.url());
    if (url.pathname.startsWith('/api/')) counts.set(url.pathname, (counts.get(url.pathname) ?? 0) + 1);
  });
  await page.goto(`/#/assistants/${assistants[0].ID}`);
  await expect(page.getByRole('log')).toBeVisible();
  await expect.poll(() => counts.get('/api/settings')).toBe(1);
  await expect.poll(() => counts.get('/api/conversations')).toBe(1);
  await expect.poll(() => counts.get('/api/models')).toBe(1);
  if (info.project.name === 'desktop') {
    await expect(page.locator(`[data-assistant-id="${assistants[1].ID}"] small`)).not.toBeEmpty();
    expect(counts.get('/api/assistants/activity')).toBe(1);
  }
  for (const a of assistants.slice(1)) {
    expect(counts.get(`/api/assistants/${a.ID}/status`) ?? 0).toBe(0);
    expect(counts.get(`/api/conversations/${a.ConversationID}/messages`) ?? 0).toBe(0);
  }
  releaseEvents();
  await expect.poll(() => page.evaluate(id => (window as any).__replayedReads.includes(id), assistants.at(-1)!.ConversationID)).toBe(true);
  await page.waitForTimeout(750); // Flush the final replay batch before measuring a new event.
  const currentMessages = `/api/conversations/${assistants[0].ConversationID}/messages`;
  const before = counts.get(currentMessages);
  const beforeActivity = counts.get('/api/assistants/activity') ?? 0;
  const marker = `Message in another chat ${info.project.name}`;
  await post(page, '/api/conversations/messages', { ConversationID: assistants[1].ConversationID, Content: { Kind: 'text', Text: marker } });
  await expect.poll(async () => (await (await page.request.get(`/api/conversations/${assistants[1].ConversationID}/messages`)).json()).Items.some((message: { Content: { Text: string } }) => message.Content.Text === marker)).toBe(true);
  // Wait through batching and one status interval; no history read in this chat.
  await page.waitForTimeout(6000);
  expect(counts.get(currentMessages)).toBe(before);
  for (const a of assistants.slice(1)) expect(counts.get(`/api/assistants/${a.ID}/status`) ?? 0).toBe(0);
  if (info.project.name === 'desktop') expect(counts.get('/api/assistants/activity')! - beforeActivity).toBeLessThanOrEqual(4);
  const text = `Visible in this chat ${info.project.name}`;
  await post(page, '/api/conversations/messages', { ConversationID: assistants[0].ConversationID, Content: { Kind: 'text', Text: text } });
  await expect(page.getByRole('log').getByText(text, { exact: true })).toBeVisible();
});
