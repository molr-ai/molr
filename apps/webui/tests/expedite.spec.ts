import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
const port = process.env.MOLR_TEST_PORT ?? '5193';
const base = `http://127.0.0.1:${port}`;
async function post(page: Page, path: string, data: object) {
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } } });
  expect(response.status(), await response.text()).toBe(200); return response.json();
}
test('expedite injects unread messages into the running turn, updates both tabs and never replies twice', async ({ page, context }, info) => {
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const name = `Expedite ${info.project.name}-${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  const assistant = await post(page, '/api/assistants', { Name: name, ModelID: models.find((m: any) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  await page.goto(`/#/assistants/${assistant.ID}`);
  const send = async (text: string) => {
    await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).fill(text);
    await page.getByRole('button', { name: 'Send', exact: true }).click();
  };
  // Observe every rendered frame, including short-lived buttons before ReadAt.
  await page.evaluate(() => {
    const seen: string[] = [];
    (window as any).expediteFrames = seen;
    const observer = new MutationObserver(() => {
      document.querySelectorAll('.ui-chat-message--user:has(.message-expedite-button)').forEach(row => seen.push(row.textContent ?? ''));
    });
    observer.observe(document.body, { childList: true, subtree: true, attributes: true });
  });
  await send('Work on the original task');
  const first = page.locator('.ui-chat-message--user').filter({ hasText: 'Work on the original task' });
  await expect(first.locator('[data-read-status="unread"]')).toBeVisible();
  await expect.poll(async () => (await (await page.request.get(`/api/assistants/${assistant.ID}/status`)).json()).TriggerMessageID).toBeTruthy();
  await expect(first.locator('[data-read-status="read"]')).toBeVisible();
  expect(await page.evaluate(() => (window as any).expediteFrames.some((text: string) => text.includes('Work on the original task')))).toBe(false);
  const observer = await context.newPage(); await observer.goto(`/#/assistants/${assistant.ID}`);
  await send('First correction'); await send('Second correction');
  const latest = page.locator('.ui-chat-message--user').filter({ hasText: 'Second correction' });
  await expect(latest.locator('[data-read-status="unread"]')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Expedite', exact: true })).toHaveCount(1);
  await latest.getByRole('button', { name: 'Expedite', exact: true }).click();
  await expect(latest.locator('[data-read-status="read"]')).toBeVisible({ timeout: 10_000 });
  const reply = 'Expedited messages received: First correction / Second correction';
  await expect(page.locator('.ui-chat-message--assistant').filter({ hasText: reply })).toHaveCount(1);
  await expect(observer.locator('.ui-chat-message--user').filter({ hasText: 'Second correction' }).locator('[data-read-status="read"]')).toBeVisible();
  await expect.poll(async () => (await (await page.request.get(`/api/assistants/${assistant.ID}/status`)).json()).State).toBe('idle');
  await page.reload();
  await expect(page.locator('.ui-chat-message--assistant')).toHaveCount(1);
  await expect(page.getByRole('button', { name: 'Expedite', exact: true })).toHaveCount(0);
  const jobs = await (await page.request.get(`/api/jobs?assistant_id=${assistant.ID}`)).json();
  expect(jobs.Items).toHaveLength(3);
  expect(jobs.Items.every((job: any) => job.State === 'done')).toBe(true);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  // Stop must end this execution without permanently disabling future input.
  await send('Task before stop');
  await expect(page.locator('.ui-chat-message--user').filter({ hasText: 'Task before stop' }).locator('[data-read-status="read"]')).toBeVisible();
  const processing = page.getByRole('button', { name: 'Pause current execution', exact: true });
  await expect(processing.locator('.composer-processing__working')).toBeVisible();
  await expect(processing.locator('.composer-processing__stop')).toBeHidden();
  await send('Queued before stop');
  await expect(page.locator('.ui-chat-message--user').filter({ hasText: 'Queued before stop' }).locator('[data-read-status="unread"]')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeVisible();
  await page.mouse.move(0, 0);
  await expect(processing.locator('.ui-spinner')).toHaveCSS('opacity', '1');
  await expect(processing.locator('svg')).toHaveCSS('opacity', '0');
  if (info.project.name === 'desktop') {
    const width = (await processing.boundingBox())!.width;
    await processing.hover();
    await expect(processing.locator('.composer-processing__working')).toBeHidden();
    await expect(processing.locator('.composer-processing__stop')).toBeVisible();
    await expect(processing.locator('.composer-processing__stop')).toHaveText('Stop');
    expect((await processing.boundingBox())!.width).toBe(width);
    await expect(processing.locator('.ui-spinner')).toHaveCSS('opacity', '0');
    await expect(processing.locator('svg')).toHaveCSS('opacity', '1');
  }
  await processing.click();
  await expect.poll(async () => (await (await page.request.get(`/api/assistants/${assistant.ID}/status`)).json()).State).toBe('idle');
  await expect.poll(async () => {
    const jobs = await (await page.request.get(`/api/jobs?assistant_id=${assistant.ID}`)).json();
    return jobs.Items.filter((job: any) => ['running', 'queued', 'retry'].includes(job.State)).length;
  }).toBe(0);
  await page.reload();
  await expect(page.locator('.chat-header [data-status="idle"]')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Expedite', exact: true })).toHaveCount(0);
  await send('New task after stopping');
  await expect(page.getByRole('button', { name: 'Pause current execution', exact: true })).toBeVisible();
  await expect(page.locator('.ui-chat-message--user').filter({ hasText: 'New task after stopping' }).locator('[data-read-status="read"]')).toBeVisible();
  await send('Urgent after restart');
  await page.getByRole('button', { name: 'Expedite', exact: true }).click();
  await expect(page.locator('.ui-chat-message--assistant').filter({ hasText: 'Expedited messages received: Urgent after restart' })).toHaveCount(1);
  await expect.poll(async () => (await (await page.request.get(`/api/assistants/${assistant.ID}/status`)).json()).State).toBe('idle');
  await observer.close();
});

test('rapid messages can be expedited while their unread trigger stays on its original turn', async ({ page }, info) => {
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const name = `Expedite burst ${info.project.name}-${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  const assistant = await post(page, '/api/assistants', { Name: name, ModelID: models.find((m: any) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  await page.goto(`/#/assistants/${assistant.ID}`);
  const trigger = await post(page, '/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: 'Burst trigger' } });
  await expect.poll(async () => (await (await page.request.get(`/api/assistants/${assistant.ID}/status`)).json()).TriggerMessageID).toBe(trigger.ID);
  const first = page.locator('.ui-chat-message--user').filter({ hasText: 'Burst trigger' });
  await expect(first.locator('[data-read-status="unread"]')).toBeVisible();
  // A stale client may still request expedition of the trigger directly.
  const result = await post(page, `/api/assistants/${assistant.ID}/expedite`, { ConversationID: assistant.ConversationID, ThroughSequence: trigger.Sequence });
  expect(result.State).toBe('not_needed');
  await post(page, '/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: 'Burst B' } });
  await post(page, '/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: 'Burst C' } });
  const latest = page.locator('.ui-chat-message--user').filter({ hasText: 'Burst C' });
  await expect(first.locator('.message-expedite-button')).toHaveCount(0);
  await expect(first.locator('[data-read-status="unread"]')).toBeVisible();
  await latest.getByRole('button', { name: 'Expedite', exact: true }).click();
  await expect(page.locator('.ui-chat-message--assistant').filter({ hasText: 'Expedited messages received: Burst B / Burst C' })).toHaveCount(1);
  await expect.poll(async () => (await (await page.request.get(`/api/assistants/${assistant.ID}/status`)).json()).State).toBe('idle');
  await expect(page.locator('.message-expedite-button')).toHaveCount(0);
});
