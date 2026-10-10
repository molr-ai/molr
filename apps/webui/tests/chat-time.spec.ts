import { test, expect } from '@playwright/test';

test('long chat typing reuses timestamp formatters and changing account timezone updates labels', async ({ page }, info) => {
  test.setTimeout(90_000);
  await page.addInitScript(() => {
    const native = Intl.DateTimeFormat;
    (window as any).chatTimeConstructions = 0;
    Intl.DateTimeFormat = new Proxy(native, { construct(target, args, newTarget) {
      const options = args[1];
      if (options?.hourCycle === 'h23' && options?.year === 'numeric' && options?.month === 'long' && options?.day === 'numeric') (window as any).chatTimeConstructions++;
      return Reflect.construct(target, args, newTarget);
    } });
  });
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const port = process.env.MOLR_TEST_PORT ?? '5193', base = `http://127.0.0.1:${port}`;
  const post = async (path: string, data: object, version?: number) => {
    const csrf = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${port}`)!.value;
    const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: version } } });
    expect(response.status(), await response.text()).toBe(200); return response.json();
  };
  const settings = await (await page.request.get('/api/settings')).json();
  await post('/api/settings', { Settings: { ...settings, Timezone: 'UTC' } }, settings.Version);
  const name = `Chat time ${info.project.name}-${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post('/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, SupportsTools: true, ContextLimit: 32768, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  const assistant = await post('/api/assistants', { Name: name, ModelID: models.find((model: any) => model.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  await expect.poll(async () => {
    const messages = await (await page.request.get(`/api/conversations/${assistant.ConversationID}/messages`)).json();
    const status = await (await page.request.get(`/api/assistants/${assistant.ID}/status`)).json();
    return messages.Items.some((message: any) => message.Sender === 'assistant') && status.State === 'idle';
  }, { timeout: 60_000 }).toBe(true);
  // Synthetic, paginated history isolates browser rendering from model execution.
  const messages = Array.from({ length: 500 }, (_, index) => ({ ID: `time-message-${index}`, ConversationID: assistant.ConversationID, Sequence: index + 1, Sender: index % 2 ? 'assistant' : 'user', Content: { Kind: 'text', Text: `History line ${index + 1}`, AttachmentIDs: null, Card: null }, CreatedAt: new Date(Date.UTC(2026, 0, 1, 0, index)).toISOString(), AcceptedAt: null, ReadAt: '2026-01-02T00:00:00Z' }));
  await page.route(`**/api/conversations/${assistant.ConversationID}/messages?*`, route => {
    const query = new URL(route.request().url()).searchParams;
    if (query.get('order') !== 'desc') return route.fulfill({ json: { Items: [], NextCursor: '' } });
    const offset = Number(query.get('cursor') ?? 0), ordered = [...messages].reverse();
    return route.fulfill({ json: { Items: ordered.slice(offset, offset + 50), NextCursor: offset + 50 < messages.length ? String(offset + 50) : '' } });
  });
  await page.route('**/api/conversations/read', route => route.fulfill({ json: {} }));
  await page.goto(`/#/assistants/${assistant.ID}`); await page.reload();
  for (let count = 50; count < 500; count += 50) {
    await expect(page.locator('.ui-chat-message')).toHaveCount(count);
    await page.getByRole('button', { name: 'Load earlier messages', exact: true }).click();
  }
  await expect(page.locator('.ui-chat-message')).toHaveCount(500);
  await expect(page.locator('.message-time').last()).toHaveAttribute('title', 'January 1, 2026, 08:19');
  const input = page.getByRole('textbox', { name: 'Message your assistant', exact: true });
  await input.focus();
  await page.evaluate(() => { (window as any).chatTimeConstructions = 0; });
  const started = Date.now();
  await input.pressSequentially('typing stays responsive', { delay: 30 });
  await expect(input).toHaveValue('typing stays responsive');
  const constructions = await page.evaluate(() => (window as any).chatTimeConstructions);
  expect(constructions).toBe(0);
  await info.attach('time-format-work.json', { body: JSON.stringify({ messages: 500, typedCharacters: 23, constructions, interactionMs: Date.now() - started }), contentType: 'application/json' });
  const latest = await (await page.request.get('/api/settings')).json();
  await post('/api/settings', { Settings: { ...latest, Timezone: 'Asia/Shanghai' } }, latest.Version);
  await page.reload();
  await expect(page.locator('.message-time').last()).toHaveAttribute('title', 'January 1, 2026, 16:19');
  await post('/api/settings', { Settings: { ...latest, Timezone: settings.Timezone } }, latest.Version + 1);
});
