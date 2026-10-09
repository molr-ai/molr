import { test, expect } from '@playwright/test';

for (const mode of ['stream', 'json', 'tools', 'empty', 'interrupted']) test(`model response ${mode} defaults to readable output and preserves raw evidence`, async ({ page }, info) => {
  const port = process.env.MOLR_TEST_PORT ?? '5193', base = `http://127.0.0.1:${port}`;
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill(process.env.MOLR_TEST_PASSWORD ?? 'webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const post = async (path: string, data: Record<string, unknown>, version?: number) => {
    const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
    const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: version } } });
    expect(response.status(), await response.text()).toBe(200); return response.json();
  };
  const settings = await (await page.request.get('/api/settings')).json();
  await post('/api/settings', { Settings: { ...settings, WorkbenchVisible: true } }, settings.Version);
  const name = `Response tabs ${mode} ${info.project.name}-${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post('/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  const assistant = await post('/api/assistants', { Name: name, ModelID: models.find((m: { ResourceID: string }) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  await post('/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: '查看模型响应' } });
  await expect.poll(async () => {
    const run = (await (await page.request.get(`/api/debug/runs?assistant_id=${assistant.ID}`)).json()).Items[0];
    return run && run.State !== 'running';
  }).toBe(true);
  await page.goto(`/#/workbench/${assistant.ID}`); await page.reload();
  await page.locator('.trajectory-record--model .trajectory-record-body').first().click();
  const inspector = page.getByRole('region', { name: 'Step details', exact: true });
  await inspector.getByRole('tab', { name: 'Model response', exact: true }).click();
  const readable = inspector.getByRole('tabpanel', { name: 'Response content', exact: true });
  await expect(inspector.getByRole('tab', { name: 'Response content', exact: true })).toHaveAttribute('aria-selected', 'true');
  if (mode === 'stream' || mode === 'json') {
    await expect(readable.locator('.workbench-prose').last()).toHaveText('模型正文第一行。\n第二行保持原始换行。');
    await expect(readable.getByRole('heading', { name: 'thinking', exact: true })).toHaveCount(mode === 'stream' ? 1 : 0);
    const content = readable.locator('details').filter({ has: page.getByRole('heading', { name: 'content', exact: true }) });
    await expect(content.locator('.workbench-prose')).toBeVisible();
    await content.locator('summary').click();
    await expect(content.locator('.workbench-prose')).not.toBeVisible();
    await content.locator('summary').focus();
    await page.keyboard.press('Enter');
    await expect(content.locator('.workbench-prose')).toBeVisible();
    if (mode === 'stream') {
      const thinking = readable.locator('details').filter({ has: page.getByRole('heading', { name: 'thinking', exact: true }) });
      await expect(thinking.locator('.workbench-prose')).not.toBeVisible();
      await thinking.locator('summary').click();
      await expect(thinking.locator('.workbench-prose')).toBeVisible();
      await expect(thinking.locator('.workbench-prose')).toHaveText('先检查输入。\n再给出结果。');
      await expect(content.locator('.workbench-prose')).toBeVisible();
    }
  } else if (mode === 'tools') await expect(readable).toContainText('conversation_publish');
  else await expect(readable).toContainText(mode === 'empty' ? 'The model returned no content' : 'No complete model output is available');
  await expect(readable).not.toContainText('data:');
  // Shared Tabs keyboard behavior must work for the nested response tabs.
  await inspector.getByRole('tab', { name: 'Response content', exact: true }).focus();
  await page.keyboard.press('ArrowRight');
  await expect(inspector.getByRole('tab', { name: 'Raw response', exact: true })).toHaveAttribute('aria-selected', 'true');
  const raw = inspector.getByRole('tabpanel', { name: 'Raw response', exact: true });
  await expect(raw).toContainText(mode === 'stream' || mode === 'interrupted' ? 'data:' : 'choices');
  await inspector.getByRole('tab', { name: 'Response content', exact: true }).click();
  await expect(readable).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
