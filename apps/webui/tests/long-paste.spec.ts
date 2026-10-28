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
  const name = `Long paste ${suffix}-${Date.now()}`, endpoint = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post<{ ID: string }>(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: endpoint, AllowedOrigins: [endpoint], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1000000, CachedInputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 1000000 } }] } });
  const models: Model[] = await (await page.request.get('/api/models')).json();
  const assistant = await post<Assistant>(page, '/api/assistants', { Name: name, ModelID: models.find(model => model.ResourceID === resource.ID)!.ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  await page.goto(`/#/assistants/${assistant.ID}`);
  await expect(page.getByRole('textbox', { name: 'Message your assistant', exact: true })).toBeEnabled();
  return assistant;
}


async function paste(page: Page, text: string) {
  await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).evaluate((el, text) => {
    const data = new DataTransfer(); data.setData('text/plain', text);
    el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }));
  }, text);
}

test('long and cumulative pastes upload exact text and send attachment references', async ({ page }, info) => {
  const assistant = await setup(page, info.project.name);
  const input = page.getByRole('textbox', { name: 'Message your assistant', exact: true });
  const sources = ['材料\t保持原文\r\n' + '中文资料'.repeat(1100), Array.from({ length: 20 }, (_, i) => `line ${i}`).join('\n'), 'A short addition beyond the body limit'];
  const sent: { Content: { Text: string; AttachmentIDs: string[] } }[] = [];
  page.on('request', request => {
    if (request.method() !== 'POST') return;
    if (new URL(request.url()).pathname === '/api/conversations/messages') sent.push(request.postDataJSON());
  });
  await input.fill('Read the attachment as needed.');
  for (let i = 0; i < sources.length; i++) {
    if (i === 2) await input.fill('x'.repeat(4000));
    await paste(page, sources[i]);
    await expect(page.locator('.composer-attachments .ui-chat-attachments li')).toHaveCount(i + 1);
    await expect(input).toBeEnabled();
    await expect(input).toHaveValue(i === 2 ? 'x'.repeat(4000) : 'Read the attachment as needed.');
  }
  await input.fill('Only this instruction belongs in the message body.');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(input).toHaveValue('');
  expect(sent).toHaveLength(1);
  expect(sent[0].Content.Text).toBe('Only this instruction belongs in the message body.');
  expect(sent[0].Content.AttachmentIDs).toHaveLength(3);
  for (let i = 0; i < 3; i++) {
    const response = await page.request.get(`/api/files/${sent[0].Content.AttachmentIDs[i]}/content`);
    expect(response.ok()).toBeTruthy(); expect((await response.body()).toString('utf8')).toBe(sources[i]);
  }
  const messages = await (await page.request.get(`/api/conversations/${assistant.ConversationID}/messages`)).json();
  expect(messages.Items.find((m: { Sender: string }) => m.Sender === 'user').Content.Text).toBe(sent[0].Content.Text);
});

test('failed text upload retains original across navigation and retries with the same key', async ({ page }, info) => {
  const assistant = await setup(page, info.project.name);
  const input = page.getByRole('textbox', { name: 'Message your assistant', exact: true });
  const text = '原文😀'.repeat(1500), keys: string[] = [];
  let fail = true;
  await page.route('**/api/files', async route => {
    keys.push(route.request().headers()['idempotency-key']);
    expect(route.request().postDataBuffer()!.toString('utf8')).toBe(text);
    if (fail) await route.fulfill({ status: 503, json: { code: 'unavailable' } }); else await route.continue();
  });
  await input.fill('Keep this draft'); await paste(page, text);
  const retry = page.getByRole('button', { name: 'Retry text uploads' });
  await expect(retry).toBeVisible();
  await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeDisabled();
  await expect(input).toHaveValue('Keep this draft');
  await page.goto('/#/resources'); await page.goto(`/#/assistants/${assistant.ID}`);
  await expect(retry).toBeVisible();
  await expect(input).toHaveValue('Keep this draft');
  fail = false; await retry.click();
  await expect(page.locator('.composer-attachments .ui-chat-attachments li')).toHaveCount(1);
  await expect(retry).toHaveCount(0);
  expect(keys.length).toBeGreaterThanOrEqual(2);
  expect(new Set(keys).size).toBe(1);
});

test('selection replacement stays within the body limit and text attachments can be removed', async ({ page }, info) => {
  await setup(page, info.project.name);
  const input = page.getByRole('textbox', { name: 'Message your assistant', exact: true });
  await input.fill('x'.repeat(4000));
  await input.evaluate(el => (el as HTMLTextAreaElement).select());
  await paste(page, 'Replacement');
  await expect(page.getByRole('group', { name: 'Pending text attachments' })).toHaveCount(0);
  await expect(page.locator('.composer-attachments .ui-chat-attachments li')).toHaveCount(0);
  await input.fill('Instructions'); await paste(page, 'z'.repeat(4001));
  const attachment = page.locator('.composer-attachments .ui-chat-attachments li');
  await expect(attachment).toHaveCount(1);
  await attachment.getByRole('button').click(); await expect(attachment).toHaveCount(0);
  await expect(input).toHaveValue('Instructions');
});
