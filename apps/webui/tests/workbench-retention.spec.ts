import { test, expect } from '@playwright/test';

test('admin retention UI persists, protects recent executions and rejects member changes', async ({ page, browser }, info) => {
  const port = process.env.MOLR_TEST_PORT ?? '5193', base = `http://127.0.0.1:${port}`;
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const post = async (path: string, data: object, version?: number) => {
    const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
    return page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: version } } });
  };
  const mutate = async (path: string, data: object, version?: number) => { const response = await post(path, data, version); expect(response.status(), await response.text()).toBe(200); return response.json(); };
  expect((await (await page.request.get('/api/sandbox')).json()).Capabilities.Reason).toBe('available');
  const initial = await (await page.request.get('/api/admin/debug/retention')).json();
  try {
    await page.goto('/#/settings/workbench');
    for (const [label, placeholder] of [['Retention days', '7'], ['Maximum executions', '1000'], ['Total detail size (MiB)', '64'], ['Minimum recent completed executions', '10']]) {
      await expect(page.getByLabel(label, { exact: true })).toHaveValue('');
      await expect(page.getByLabel(label, { exact: true })).toHaveAttribute('placeholder', placeholder);
    }
    await page.getByLabel('Retention days', { exact: true }).fill('14');
    await page.getByLabel('Maximum executions', { exact: true }).fill('2');
    await page.getByLabel('Total detail size (MiB)', { exact: true }).fill('8');
    await page.getByLabel('Minimum recent completed executions', { exact: true }).fill('2');
    await page.getByRole('button', { name: 'Save retention policy', exact: true }).click();
    await expect(page.getByRole('status').filter({ hasText: 'Retention policy saved' })).toBeVisible();
    await page.reload();
    await expect(page.getByLabel('Retention days', { exact: true })).toHaveValue('14');
    await expect(page.getByLabel('Minimum recent completed executions', { exact: true })).toHaveValue('2');
    await page.getByLabel('Retention days', { exact: true }).fill('');
    await page.getByLabel('Total detail size (MiB)', { exact: true }).fill('');
    await page.getByLabel('Minimum recent completed executions', { exact: true }).fill('');
    await page.getByRole('button', { name: 'Save retention policy', exact: true }).click();
    await expect.poll(async () => (await (await page.request.get('/api/admin/debug/retention')).json()).Configured).toEqual({ RetentionDays: null, MaxRuns: 2, MaxBytes: null, MinRuns: null });
    await page.reload();
    await expect(page.getByLabel('Retention days', { exact: true })).toHaveValue('');
    await expect(page.getByLabel('Minimum recent completed executions', { exact: true })).toHaveAttribute('placeholder', '2');
    const inherited = await (await page.request.get('/api/admin/debug/retention')).json();
    expect(inherited.RetentionDays).toBe(7); expect(inherited.MaxBytes).toBe(64 * 1048576); expect(inherited.MinRuns).toBe(2);
    await page.getByLabel('Retention days', { exact: true }).fill('14');
    await page.getByLabel('Total detail size (MiB)', { exact: true }).fill('8');
    await page.getByLabel('Minimum recent completed executions', { exact: true }).fill('2');
    await page.getByRole('button', { name: 'Save retention policy', exact: true }).click();
    await expect.poll(async () => (await (await page.request.get('/api/admin/debug/retention')).json()).RetentionDays).toBe(14);
    expect((await post('/api/admin/debug/retention', { Policy: initial.Configured }, initial.Version)).status()).toBe(409);
    const settings = await (await page.request.get('/api/settings')).json();
    await mutate('/api/settings', { Settings: { ...settings, WorkbenchEnabled: true } }, settings.Version);
    const name = `Retention ${info.project.name}-${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
    const resource = await mutate('/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } }] } });
    const models = await (await page.request.get('/api/models')).json();
    const assistant = await mutate('/api/assistants', { Name: name, ModelID: models.find((m: { ResourceID: string }) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
    await page.goto(`/#/workbench/${assistant.ID}`);
    await page.reload();
    await expect(page.locator('.trajectory-overview')).toBeVisible();
    await expect(page.locator('.workbench-header + .trajectory-overview')).toBeVisible();
    await expect(page.locator('.trajectory-overview ~ .trajectory-controls')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Duration axis', exact: true })).toBeVisible();
    await expect(page.locator('.trajectory-table-heading, .trajectory-time-labels, .workbench-retention, .trajectory-cleanup')).toHaveCount(0);
    await expect(page.locator('.trajectory-boundary[data-boundary="start"]')).toHaveText('No earlier events');
    await expect(page.locator('.workbench-view-heading, .trajectory-history')).toHaveCount(0);
    const seen: string[] = [];
    for (let i = 0; i < 3; i++) {
      await mutate('/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: `Retention event ${i}` } });
      await expect.poll(async () => {
        const rows = (await (await page.request.get(`/api/debug/runs?assistant_id=${assistant.ID}`)).json()).Items;
        const fresh = rows.find((r: { ID: string; State: string }) => !seen.includes(r.ID) && r.State === 'succeeded');
        if (fresh) { seen.push(fresh.ID); return true; } return false;
      }).toBe(true);
    }
    const runs = (await (await page.request.get(`/api/debug/runs?assistant_id=${assistant.ID}`)).json()).Items;
    expect(runs.map((r: { ID: string }) => r.ID)).toEqual(seen.slice(-2).reverse());
    expect((await page.request.get(`/api/debug/runs/${seen[0]}`)).status()).toBe(404);
    const latest = await (await page.request.get(`/api/debug/runs/${seen[2]}`)).json();
    const saved = latest.Steps.find((s: { Kind: string }) => s.Kind === 'checkpoint.saved');
    const detail = await (await page.request.get(`/api/debug/runs/${latest.ID}/steps/${saved.Sequence}`)).json();
    expect(detail.Data.ContextOmitted).toBe(true); expect(detail.Data.Checkpoint).toBeUndefined();
    const checkpoint = await (await page.request.get(`/api/debug/assistants/${assistant.ID}/checkpoint`)).json();
    expect(checkpoint.Data.Context.Inputs.length).toBeGreaterThan(0);
    await page.goto(`/#/workbench/${assistant.ID}`);
    await page.reload();
    const scroll = page.locator('.trajectory-scroll');
    await scroll.focus(); await page.keyboard.press('Home');
    const cleanup = page.locator('.trajectory-boundary[data-boundary="cleanup"]');
    await expect(cleanup.getByRole('button')).toHaveText('Earlier events have been cleaned up');
    await expect(page.locator('.trajectory-virtual-row').first()).toHaveAttribute('data-record-id', 'boundary');
    await expect(page.locator('.trajectory-boundary[data-boundary="start"]')).toHaveCount(0);
    await expect(cleanup.locator('.badge, .trajectory-run')).toHaveCount(0);
    await expect(page.locator('.trajectory-run-shell .badge').first()).toHaveText('Run 1');
    if (info.project.name === 'mobile') await cleanup.getByRole('button').tap();
    else await cleanup.getByRole('button').hover();
    const tip = page.getByRole('tooltip');
    await expect(tip).toBeVisible();
    await expect(tip).toContainText('14 days / 2 executions / 8 MiB');
    await expect(tip).toContainText('latest 2 completed executions');
    await expect(page.getByRole('region', { name: 'Step details', exact: true })).toHaveCount(0);
    await page.keyboard.press('Escape');
    await expect(tip).not.toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    // A normal member may inspect the common policy but cannot change it.
    const username = `retention-member-${info.project.name}-${Date.now()}`;
    await mutate('/api/admin/users', { Username: username, Password: 'retention-test-password-2026!', GroupID: 'users' });
    const memberContext = await browser.newContext(); const member = await memberContext.newPage();
    try {
      await member.goto(base); await member.getByLabel('Username', { exact: true }).fill(username); await member.getByLabel('Password', { exact: true }).fill('retention-test-password-2026!'); await member.getByRole('button', { name: 'Sign in', exact: true }).click();
      await expect(member.getByRole('navigation', { name: 'Molr' })).toBeVisible();
      expect((await member.request.get(`${base}/api/debug/retention`)).status()).toBe(200);
      expect((await member.request.get(`${base}/api/admin/debug/retention`)).status()).toBe(403);
      const csrf = (await memberContext.cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
      expect((await member.request.post(`${base}/api/admin/debug/retention`, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { Policy: initial.Configured, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: initial.Version + 1 } } })).status()).toBe(403);
    } finally { await memberContext.close(); }
  } finally {
    const current = await (await page.request.get('/api/admin/debug/retention')).json();
    await mutate('/api/admin/debug/retention', { Policy: initial.Configured }, current.Version);
    await page.goto('/#/settings/workbench'); await page.reload();
    await expect(page.getByLabel('Maximum executions', { exact: true })).toHaveValue('');
    await expect(page.getByLabel('Maximum executions', { exact: true })).toHaveAttribute('placeholder', '1000');
  }
});
