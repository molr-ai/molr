import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';

const port = process.env.MOLR_TEST_PORT ?? '5193';
const base = `http://127.0.0.1:${port}`;
async function post(page: Page, path: string, data: object) {
  const csrf = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } } });
  expect(response.status(), await response.text()).toBe(200);
  return response.json();
}

test('assistant previews show history, live user and assistant messages, and empty fallback', async ({ page }, info) => {
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill(process.env.MOLR_PREVIEW_TEST_PASSWORD ?? 'webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const name = `Preview ${info.project.name}-${Date.now()}`;
  const origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [
    { Name: `${name} paid`, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1000000, CachedInputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 1000000 } },
    { Name: `${name} free`, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } },
  ] } });
  const models = await (await page.request.get('/api/models')).json();
  const hire = (suffix: string, free = false) => post(page, '/api/assistants', { Name: `${name} ${suffix}`, ModelID: models.find((model: any) => model.ResourceID === resource.ID && model.Name.endsWith(free ? 'free' : 'paid')).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  const empty = await hire('empty'), history = await hire('history'), replying = await hire('reply', true);
  const send = (assistant: any, text: string) => post(page, '/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: text } });
  await send(history, 'Older message');
  await send(history, 'Latest historical message\nwith another line');
  await page.goto('/#/assistants');
  await page.reload();
  const preview = (assistant: any) => page.locator(`.assistant-list button[data-assistant-id="${assistant.ID}"] small`);
  await expect(preview(empty)).toHaveText('Say hello or ask for help');
  await expect(preview(history)).toHaveText('Latest historical message with another line');
  const long = '新消息🙂'.repeat(30);
  await send(history, long);
  await expect(preview(history)).toHaveText(Array.from(long).slice(0, 80).join('') + '…');
  await expect(preview(empty)).toHaveText('Say hello or ask for help');
  await page.reload();
  await expect(preview(history)).toHaveText(Array.from(long).slice(0, 80).join('') + '…');
  await expect(preview(history)).toHaveCSS('text-overflow', 'ellipsis');
  await send(replying, 'Hello');
  await expect(preview(replying)).toHaveText('Runtime delivered this reply through conversation_publish.', { timeout: 20000 });
  await page.reload();
  await expect(preview(replying)).toHaveText('Runtime delivered this reply through conversation_publish.');
  // Attachment-only messages are still messages, never the empty-chat prompt.
  await page.goto(`/#/assistants/${history.ID}`);
  await page.locator('input[type=file]').first().setInputFiles({ name: 'preview.txt', mimeType: 'text/plain', buffer: Buffer.from('preview attachment') });
  await expect(page.locator('.composer-attachments')).toContainText('preview.txt');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  if (info.project.name === 'mobile') await page.getByRole('button', { name: 'Back to assistants', exact: true }).click();
  await expect(preview(history)).toHaveText('[Attachment]');
});
