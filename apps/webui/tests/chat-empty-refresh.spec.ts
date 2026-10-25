import { readLayout } from '../../../scripts/test-support/layout.ts';
import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import type { Assistant, Model } from '@molr/sdk';

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
  const name = `Empty refresh ${suffix}-${Date.now()}`, endpoint = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post<{ ID: string }>(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: endpoint, AllowedOrigins: [endpoint], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1000000, CachedInputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 1000000 } }] } });
  const models: Model[] = await (await page.request.get('/api/models')).json();
  const assistant = await post<Assistant>(page, '/api/assistants', { Name: name, ModelID: models.find(model => model.ResourceID === resource.ID)!.ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  await page.goto(`/#/assistants/${assistant.ID}`);
  await expect(page.getByRole('textbox', { name: 'Message your assistant', exact: true })).toBeEnabled();
  return assistant;
}

declare global { interface Window { historySorts: number; historySortedMessages: number; historyConversation: string } }

test('empty incremental refreshes do not sort the already loaded chat history', async ({ page }, info) => {
  await page.addInitScript(() => {
    window.historySorts = 0; window.historySortedMessages = 0; window.historyConversation = '';
    const sort = Array.prototype.sort;
    Array.prototype.sort = function(compare) {
      if (this.length && this[0]?.ConversationID === window.historyConversation && typeof this[0]?.Sequence === 'number') { window.historySorts++; window.historySortedMessages += this.length; }
      return sort.call(this, compare);
    };
  });
  const assistant = await setup(page, info.project.name), path = `/api/conversations/${assistant.ConversationID}/messages`;
  const messages = Array.from({ length: 500 }, (_, index) => ({ ID: 'empty-history-' + index, ConversationID: assistant.ConversationID, Sequence: index + 1, Sender: 'user', Content: { Kind: 'text', Text: 'Loaded history message ' + (index + 1), AttachmentIDs: [], Card: null }, CreatedAt: new Date(Date.UTC(2026, 0, 1, 0, index)).toISOString(), ReadAt: '2026-01-02T00:00:00Z' }));
  let emptyReads = 0;
  await page.route('**' + path + '?*', route => {
    const query = new URL(route.request().url()).searchParams;
    if (query.has('after_sequence')) { emptyReads++; return route.fulfill({ json: { Items: [], NextCursor: '' } }); }
    const offset = Number(query.get('cursor') ?? 0), ordered = [...messages].reverse();
    return route.fulfill({ json: { Items: ordered.slice(offset, offset + 50), NextCursor: offset + 50 < ordered.length ? String(offset + 50) : '' } });
  });
  await page.reload();
  await page.evaluate(id => { window.historyConversation = id; }, assistant.ConversationID);
  for (let count = 50; count < 500; count += 50) {
    await expect(page.locator('.ui-chat-message')).toHaveCount(count);
    await page.getByRole('button', { name: 'Load earlier messages', exact: true }).click();
  }
  await expect(page.locator('.ui-chat-message')).toHaveCount(500);
  await page.evaluate(() => { window.historySorts = 0; window.historySortedMessages = 0; });
  const before = emptyReads;
  for (let index = 0; index < 5; index++) {
    const response = page.waitForResponse(response => new URL(response.url()).pathname === path && new URL(response.url()).searchParams.has('after_sequence'));
    await page.evaluate(() => {
      Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' }); document.dispatchEvent(new Event('visibilitychange'));
      Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' }); document.dispatchEvent(new Event('visibilitychange'));
    });
    await (await response).finished();
    await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
  }
  const work = await page.evaluate(() => ({ sorts: window.historySorts, sortedMessages: window.historySortedMessages }));
  await info.attach('empty-refresh-work', { body: JSON.stringify({ loadedMessages: 500, emptyReads: emptyReads - before, ...work }), contentType: 'application/json' });
  expect(emptyReads - before).toBeGreaterThanOrEqual(5);
  expect(work.sortedMessages).toBe(0);
  await expect(page.locator('.ui-chat-message')).toHaveCount(500);
});

function sample(assistant: Assistant) {
  return { ID: 'retained-message', ConversationID: assistant.ConversationID, Sequence: 1, Sender: 'user', Content: { Kind: 'text', Text: 'Retained history', AttachmentIDs: [], Card: null }, CreatedAt: '2026-01-01T00:00:00Z', ReadAt: '2026-01-02T00:00:00Z' };
}
async function refresh(page: Page) { await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange'))); }

test('empty older pages exhaust the cursor and an empty recovery clears transient errors without losing history', async ({ page }, info) => {
  const assistant = await setup(page, info.project.name), path = `/api/conversations/${assistant.ConversationID}/messages`;
  let failNext = false, deltas = 0;
  await page.route('**' + path + '?*', route => {
    const query = new URL(route.request().url()).searchParams;
    if (query.has('after_sequence')) {
      deltas++;
      if (failNext) { failNext = false; return route.fulfill({ status: 503, json: { code: 'internal' } }); }
      return route.fulfill({ json: { Items: [], NextCursor: '' } });
    }
    return route.fulfill({ json: query.get('cursor') ? { Items: [], NextCursor: '' } : { Items: [sample(assistant)], NextCursor: 'last-empty-page' } });
  });
  await page.reload(); await expect(page.getByRole('log')).toContainText('Retained history');
  await page.getByRole('button', { name: 'Load earlier messages', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Load earlier messages', exact: true })).toHaveCount(0);
  await expect(page.locator('.ui-chat-message')).toHaveCount(1);
  failNext = true; await refresh(page);
  const error = page.locator('.ui-toast--danger:visible');
  await expect(error).toBeVisible(); await expect(page.getByRole('log')).toContainText('Retained history');
  const failedReads = deltas;
  await expect.poll(() => deltas).toBeGreaterThan(failedReads);
  await expect(error).toHaveCount(0);
  await expect(page.locator('.ui-chat-message')).toHaveCount(1);
  await expect(page.getByRole('button', { name: 'Load earlier messages', exact: true })).toHaveCount(0);
});

test('initial empty history finishes loading and denied history cannot return after empty recovery', async ({ page }, info) => {
  const assistant = await setup(page, info.project.name), path = `/api/conversations/${assistant.ConversationID}/messages`;
  let mode: 'empty' | 'message' | 'denied' = 'empty';
  await page.route('**' + path + '?*', route => mode === 'denied' ? route.fulfill({ status: 403, json: { code: 'forbidden' } }) : route.fulfill({ json: { Items: mode === 'message' ? [sample(assistant)] : [], NextCursor: '' } }));
  await page.reload(); await expect(page.getByRole('heading', { name: 'Start a conversation', exact: true })).toBeVisible();
  mode = 'message'; await refresh(page); await expect(page.getByRole('log')).toContainText('Retained history');
  mode = 'denied'; await refresh(page);
  const error = page.locator('.ui-toast--danger:visible'); await expect(error).toBeVisible();
  await expect(page.locator('.ui-chat-message')).toHaveCount(0);
  mode = 'empty'; await error.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(error).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Start a conversation', exact: true })).toBeVisible();
  await expect(page.locator('.ui-chat-message')).toHaveCount(0);
});

test('return to latest tracks scroll distance and overrides pending history restoration', async ({ page }, info) => {
  const assistant = await setup(page, info.project.name), path = `/api/conversations/${assistant.ConversationID}/messages`;
  const messages = Array.from({ length: 65 }, (_, index) => ({ ...sample(assistant), ID: `scroll-message-${index + 1}`, Sequence: index + 1, Content: { Kind: 'text', Text: `Scroll history ${index + 1}`, AttachmentIDs: [], Card: null } }));
  let release!: () => void;
  const olderGate = new Promise<void>(resolve => { release = resolve; });
  await page.route('**' + path + '?*', async route => {
    const query = new URL(route.request().url()).searchParams;
    if (query.has('after_sequence')) return route.fulfill({ json: { Items: [], NextCursor: '' } });
    if (query.has('cursor')) { await olderGate; return route.fulfill({ json: { Items: messages.slice(0, 15).reverse(), NextCursor: '' } }); }
    return route.fulfill({ json: { Items: messages.slice(15).reverse(), NextCursor: 'older-scroll-messages' } });
  });
  await page.reload();
  const log = page.getByRole('log'), latest = page.locator('.chat-return-latest');
  await expect(page.locator('.ui-chat-message')).toHaveCount(50);
  await expect(latest).toHaveCount(0);
  await log.evaluate(element => { element.scrollTop = element.scrollHeight - element.clientHeight - Math.max(600, element.clientHeight) + 40; });
  await expect(latest).toHaveCount(0);
  await log.evaluate(element => { element.scrollTop = element.scrollHeight - element.clientHeight - Math.max(600, element.clientHeight) - 40; });
  await expect(latest).toBeVisible();
  await log.evaluate(element => { element.scrollTop = 0; });
  await expect(latest).toBeVisible();
  const [bounds, area, composer] = await readLayout(latest, log, page.locator('.chat-input'));
  expect(bounds!.y + bounds!.height).toBeLessThanOrEqual(area!.y + area!.height);
  expect(bounds!.y + bounds!.height).toBeLessThan(composer!.y);
  await latest.focus(); await page.keyboard.press('Enter');
  await expect(latest).toHaveCount(0);
  await expect(log).toBeFocused();
  const distance = () => log.evaluate(element => element.scrollHeight - element.clientHeight - element.scrollTop);
  await expect.poll(distance).toBeLessThan(2);
  await page.getByRole('button', { name: 'Load earlier messages', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Load earlier messages', exact: true })).toBeDisabled();
  await expect(latest).toBeVisible();
  await latest.click();
  release();
  await expect(page.locator('.ui-chat-message')).toHaveCount(65);
  await expect.poll(distance).toBeLessThan(2);
  await expect(latest).toHaveCount(0);
  await page.goto(`/#/assistants/${assistant.ID}?message=scroll-message-20`);
  await expect(page.locator('.chat-message-target')).toBeVisible();
  await expect(latest).toBeVisible();
  await latest.click();
  await expect(page).toHaveURL(new RegExp(`/assistants/${assistant.ID}$`));
  await expect.poll(distance).toBeLessThan(2);
  await expect(page.locator('.chat-search-location')).toHaveCount(0);
});
