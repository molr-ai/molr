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
  const name = `Composer rendering ${suffix}-${Date.now()}`, endpoint = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post<{ ID: string }>(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: endpoint, AllowedOrigins: [endpoint], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1000000, CachedInputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 1000000 } }] } });
  const models: Model[] = await (await page.request.get('/api/models')).json();
  const assistant = await post<Assistant>(page, '/api/assistants', { Name: name, ModelID: models.find(model => model.ResourceID === resource.ID)!.ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  await page.goto(`/#/assistants/${assistant.ID}`);
  await expect(page.getByRole('textbox', { name: 'Message your assistant', exact: true })).toBeEnabled();
  return assistant;
}

// Observe committed renders in the actual application, including the message
// components. A DOM-mutation count would miss redundant React render work.
declare global { interface Window { chatRenders: Record<string, number> } }
test('typing and draft attachments do not render the chat history', async ({ page }, info) => {
  await page.addInitScript(() => {
    window.chatRenders = {};
    // Keep the SSE connection open without reconnect timers or live events.
    // Fulfilling an event-stream response would close it and trigger refreshes.
    class FixtureEventSource extends EventTarget {
      readyState = 1;
      constructor() {
        super();
        setTimeout(() => this.dispatchEvent(new MessageEvent('ready', { data: '{}' })), 0);
      }
      close() { this.readyState = 2; }
    }
    window.EventSource = FixtureEventSource as unknown as typeof EventSource;
    type Fiber = { type?: { name?: string }; flags: number; child: Fiber | null; sibling: Fiber | null; alternate?: Fiber };
    const renderers = new Map();
    Object.assign(window, { __REACT_DEVTOOLS_GLOBAL_HOOK__: {
      supportsFiber: true,
      renderers,
      inject(renderer: unknown) { const id = renderers.size + 1; renderers.set(id, renderer); return id; },
      onCommitFiberRoot(_id: number, root: { current: Fiber }) {
        function visit(fiber: Fiber | null) {
          if (!fiber) return;
          const name = fiber.type?.name;
          // React's PerformedWork flag marks function components that rendered.
          if (name && ['Chat', 'ChatComposerContainer', 'ChatMessage'].includes(name) && (fiber.flags & 1)) {
            window.chatRenders[name] = (window.chatRenders[name] ?? 0) + 1;
          }
          // A bailed-out subtree retains its previous fibers and their flags.
          if (fiber.child !== fiber.alternate?.child) visit(fiber.child);
          visit(fiber.sibling);
        }
        visit(root.current);
      },
      onCommitFiberUnmount() {},
    } });
  });
  const assistant = await setup(page, info.project.name);
  // Hold the server snapshot steady to distinguish input work from live updates.
  await page.route(`**/api/assistants/${assistant.ID}/status*`, route => route.fulfill({ json: { State: 'idle' } }));
  const messages = Array.from({ length: 100 }, (_, i) => ({
    ID: `composer-message-${i}`, ConversationID: assistant.ConversationID,
    Sequence: i + 1, Sender: i % 2 ? 'assistant' : 'user',
    CreatedAt: '2026-01-01T00:00:00Z', ReadAt: '2026-01-01T00:01:00Z',
    Content: { Kind: 'text', Text: `History message ${i}` },
  }));
  await page.route(`**/api/conversations/${assistant.ConversationID}/messages?*`, route => route.fulfill({ json: { Items: messages, NextCursor: '' } }));
  await page.route('**/api/conversations/read', route => route.fulfill({ json: {} }));
  // Keep an upload local too: the real upload/navigation paths have separate E2E coverage.
  await page.route('**/api/files', route => route.fulfill({ json: { ID: 'composer-file', Name: 'draft.txt', MediaType: 'text/plain', Size: 5 } }));
  await page.reload();
  const input = page.getByRole('textbox', { name: 'Message your assistant', exact: true });
  await expect(input).toBeEnabled();
  await expect(page.locator('.ui-chat-message')).toHaveCount(100);
  // Settle the initial SSE refresh and layout before measuring input-only work.
  await page.waitForTimeout(750);
  const before = await page.evaluate(() => ({ ...window.chatRenders }));
  expect(before.Chat).toBeGreaterThan(0);
  expect(before.ChatMessage).toBeGreaterThanOrEqual(100);
  expect(before.ChatComposerContainer).toBeGreaterThan(0);
  const text = 'A draft that should only render the composer';
  await input.pressSequentially(text);
  await expect(input).toHaveValue(text);
  await page.locator('.chat-input input[type=file]').setInputFiles({ name: 'draft.txt', mimeType: 'text/plain', buffer: Buffer.from('draft') });
  const remove = page.getByRole('button', { name: 'Remove attachment draft.txt', exact: true });
  await expect(remove).toBeVisible();
  await remove.click();
  await expect(remove).toHaveCount(0);
  await input.fill('/com');
  await expect(page.getByRole('listbox', { name: 'Commands' })).toBeVisible();
  await input.press('Enter');
  await expect(input).toHaveValue('/compress');
  const after = await page.evaluate(() => ({ ...window.chatRenders }));
  expect(after.ChatComposerContainer).toBeGreaterThan(before.ChatComposerContainer);
  expect(after.Chat).toBe(before.Chat);
  expect(after.ChatMessage).toBe(before.ChatMessage);
  await info.attach('chat-render-counts', { body: JSON.stringify({ before, after }), contentType: 'application/json' });
});
