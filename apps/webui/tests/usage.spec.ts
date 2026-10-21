import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import { randomUUID } from 'node:crypto';

async function login(page: Page, username = 'owner', password = 'webui-e2e-password-2026!') {
  await page.goto('/');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill(username);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
}
async function request(page: Page, path: string, data?: unknown): Promise<any> {
  return page.evaluate(async ({ path, data }) => {
    const module = '/src/api/client.ts', { api } = await import(module);
    return data === undefined ? api.get(path + (path.includes('?') ? '&' : '?') + 'refresh=true') : api.post(path, data);
  }, { path, data });
}
async function mutate(page: Page, path: string, data: object) {
  return request(page, path, { ...data, Mutation: { IdempotencyKey: randomUUID() } });
}
test('member usage and admin statistics share actual call totals', async ({ page, browser }) => {
  await login(page);
  const suffix = randomUUID().slice(0, 8), origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const provider = await mutate(page, '/api/resources', { Kind: 'model', Name: `Usage ${suffix}`, Provider: 'openai-compatible', Binding: { Endpoint: `${origin}/v1`, AllowedOrigins: [origin], Models: [{ Name: `Usage model ${suffix}`, SupportsTools: true, ContextLimit: 131072, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 1000000, CachedInputMicrosPerMillion: 0 } }] } });
  const model = (await request(page, '/api/models')).find((m: any) => m.ResourceID === provider.ID);
  const group = await mutate(page, '/api/admin/groups', { Group: { Name: `Usage group ${suffix}`, Resources: [{ Kind: 'model', ID: model.ID, Quota: { Budget: { Currency: 'USD', Micros: 10000000 }, Calls: 100, Concurrent: 2, Period: 'month', Timezone: 'UTC' } }, ...['conversation_read', 'conversation_publish'].map(ID => ({ Kind: 'tool', ID, Quota: { Calls: 100, Concurrent: 2, Period: 'month', Timezone: 'UTC' } }))] } });
  const username = `usage-${suffix}`, password = `usage-password-${suffix}!`;
  const user = await mutate(page, '/api/admin/users', { Username: username, DisplayName: `Usage member ${suffix}`, Password: password, GroupID: group.ID });
  const context = await browser.newContext({ baseURL: new URL(page.url()).origin, viewport: page.viewportSize() ?? undefined, locale: 'en-US' });
  try {
    const member = await context.newPage(); await login(member, username, password);
    const assistant = await mutate(member, '/api/assistants', { Name: 'Usage assistant', ModelID: model.ID, Budget: { Limit: { Currency: 'USD', Micros: 10000000 }, Period: 'daily' } });
    await mutate(member, '/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: 'Private usage fixture message' } });
    await expect.poll(async () => (await request(member, '/api/account/quotas')).Summary.ModelCalls, { timeout: 25000 }).toBeGreaterThan(0);
    await expect.poll(async () => (await request(member, `/api/jobs?assistant_id=${assistant.ID}`)).Items.find((job: any) => job.Event.Kind === 'user_message')?.State, { timeout: 25000 }).toBe('done');
    await expect.poll(async () => (await request(member, '/api/account/quotas')).Summary.Active, { timeout: 25000 }).toBe(0);
    await member.goto('/#/account/usage');
    await expect(member.locator('.usage-analytics-overview')).toBeVisible();
    await expect(member.locator('.usage-current')).toHaveAttribute('open', '');
    await expect(member.locator('.usage-current > summary')).toHaveText('Lifetime usage and allowances');
    await expect(member.getByRole('heading', { name: 'Current member allowances' })).toBeVisible();
    await expect(member.locator('.usage-resource').filter({ hasText: model.Name })).toContainText('Monthly');
    await expect(member.locator('.usage-resource').filter({ hasText: 'Read conversations' })).toBeVisible();
    const personal = await request(member, '/api/account/quotas');
    const stats = await request(page, '/api/admin/usage'), row = stats.Users.find((u: any) => u.UserID === user.ID);
    expect(personal.ContextStorage.Bytes).toBeGreaterThan(0);
    expect(personal.ContextStorage.Messages).toBeGreaterThan(0);
    expect(stats.ContextStorage.Bytes).toBeGreaterThanOrEqual(personal.ContextStorage.Bytes);
    await expect(member.getByRole('region', { name: 'Account context storage', exact: true })).toContainText('Unique message nodes');
    expect(row.ModelCalls).toBe(personal.Summary.ModelCalls);
    expect(row.ToolCalls).toBe(personal.Summary.ToolCalls);
    expect(row.Spent).toEqual(personal.Summary.Spent);
    expect(JSON.stringify(stats)).not.toContain('Private usage fixture message');
    expect((await member.request.get('/api/admin/usage')).status()).toBe(403);
    const dates = '?from=' + new Date().getUTCFullYear() + '-01-01&until=' + (new Date().getUTCFullYear() + 1) + '-01-01&timezone=Asia%2FShanghai';
    const analytics = await request(member, '/api/account/usage' + dates);
    expect(analytics.Summary.TotalTokens).toBe(analytics.Summary.ModelCalls * 40);
    expect(analytics.Summary.Spent).toEqual(personal.Summary.Spent);
    expect(analytics.Rows.find((r: any) => r.ID === assistant.ID).TotalTokens).toBeGreaterThan(0);
    const adminAnalytics = await request(page, '/api/admin/usage/analytics' + dates + '&user_id=' + user.ID);
    expect(adminAnalytics.Summary).toEqual(analytics.Summary);
    expect(JSON.stringify(adminAnalytics)).not.toContain(assistant.ID);
    expect(JSON.stringify(adminAnalytics)).not.toContain('Private usage fixture message');
    expect((await member.request.get('/api/admin/usage/analytics' + dates)).status()).toBe(403);
    const populated = analytics.Daily.find((d: any) => d.ModelCalls > 0);
    await member.locator(`button[data-date="${populated.Date}"]`).click();
    await expect(member.getByRole('heading', { name: populated.Date + ' · Daily breakdown' })).toBeVisible();
    await member.getByRole('button', { name: 'Show full range' }).click();
    await member.getByRole('radio', { name: 'By model', exact: true }).click();
    await expect(member.locator('.usage-table')).toContainText(model.Name);
    await page.goto('/#/settings/usage');
    await expect(page.locator('.usage-current')).toHaveAttribute('open', '');
    await expect(page.locator('.usage-current > summary')).toHaveText('Lifetime usage and resources');
    const storage = page.getByRole('region', { name: 'Global context storage', exact: true });
    await expect(storage).toContainText('Unique message nodes');
    const globalText = await storage.textContent();
    await page.getByRole('searchbox', { name: 'Search members' }).fill(username);
    await expect(page.locator('.usage-table tbody tr')).toHaveCount(1);
    await expect(page.locator('.usage-table')).toContainText(username);
    await page.getByRole('searchbox', { name: 'Search members' }).fill('absent-usage-fixture');
    await expect(page.getByText('No usage recorded', { exact: true })).toBeVisible();
    await expect(storage).toHaveText(globalText!);
    for (const current of [page, member]) expect(await current.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  } finally { await context.close(); }
});

test('usage loading failure remains retryable without fabricating zero usage', async ({ page }) => {
  await login(page);
  await page.route('**/api/account/usage?*', route => route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { code: 'unavailable', message: 'fixture unavailable' } }) }));
  await page.goto('/#/account/usage');
  await expect(page.locator('.usage-analytics-overview')).toHaveCount(0);
  await expect(page.getByText('Usage is temporarily unavailable', { exact: true })).toBeVisible();
  await page.unroute('**/api/account/usage?*');
  await page.locator('.usage-analytics .usage-toolbar').getByRole('button', { name: 'Refresh' }).click();
  await expect(page.locator('.usage-analytics-overview')).toBeVisible();
});

test('usage login links land on the selected usage page', async ({ page, browser }) => {
  await login(page);
  for (const target of ['/account/usage', '/settings/usage']) {
    const proof = await request(page, '/api/auth/reauthenticate', { password: 'webui-e2e-password-2026!', purpose: 'login_link.create', mfa_code: '' });
    const issued = await request(page, '/api/auth/login-links', { name: 'Usage destination', target_path: target, expires_in: 300, verification: proof.verification });
    const context = await browser.newContext({ baseURL: new URL(page.url()).origin, viewport: page.viewportSize() ?? undefined });
    try {
      const next = await context.newPage();
      await next.goto('/#/login?' + new URLSearchParams({ token: issued.value }));
      await next.getByRole('button', { name: 'Confirm sign-in', exact: true }).click();
      await expect(next).toHaveURL(new RegExp(`#${target}$`));
      await expect(next.locator('.usage-analytics-overview')).toBeVisible();
    } finally { await context.close(); }
  }
});


test('unknown model costs end holds automatically and remain visible as history', async ({ page }) => {
  await login(page);
  const before = (await request(page, '/api/account/quotas')).Summary;
  const suffix = randomUUID().slice(0, 8), origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const provider = await mutate(page, '/api/resources', { Kind: 'model', Name: `Unknown usage ${suffix}`, Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: `Retry unknown ${suffix}`, SupportsTools: true, ContextLimit: 32768, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 1000000, CachedInputMicrosPerMillion: 0 } }] } });
  const model = (await request(page, '/api/models')).find((m: any) => m.ResourceID === provider.ID);
  const assistant = await mutate(page, '/api/assistants', { Name: `Unknown usage ${suffix}`, ModelID: model.ID, Budget: { Limit: { Currency: 'USD', Micros: 1000000 }, Period: 'daily' } });
  // Hiring already starts a model call. Sending immediately can cancel that
  // birth execution and adds a second unknown outcome to this accounting test.
  await expect.poll(async () => (await request(page, `/api/jobs?assistant_id=${assistant.ID}`)).Items.some((job: any) => job.State === 'needs_review')).toBe(true);
  const after = (await request(page, '/api/account/quotas')).Summary;
  expect(after.Unknown).toBe(before.Unknown + 1);
  expect(after.ModelCalls).toBe(before.ModelCalls + 1);
  expect(after.Active).toBe(0);
  expect(after.Reserved).toEqual([]);
  expect(after.Spent).toEqual(before.Spent);
  const usage = await request(page, `/api/usage?assistant_id=${assistant.ID}&current_period=true`);
  expect(usage.Reserved.Micros).toBe(0);
  expect(usage.Spent.Micros).toBe(0);
  expect(usage.UnknownOperations).toHaveLength(1);
  await page.goto('/#/account/usage');
  await expect(page.locator('.usage-page')).not.toContainText('no manual action is needed');
  await expect(page.getByRole('heading', { name: 'Current member allowances' })).toHaveCount(0);
  await expect(page.locator('.usage-analytics-overview')).toBeVisible();
  await expect(page.locator('.usage-page').getByRole('alert')).toBeHidden();
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  expect((await request(page, '/api/account/quotas')).Summary.ModelCalls).toBe(after.ModelCalls);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('usage omits empty model choices and labels unassigned history', async ({ page }) => {
  await login(page);
  await page.route('**/api/account/usage?*', async route => {
    const response = await route.fetch();
    const data = await response.json();
    data.Options.Models = [{ ID: '', Name: '' }, { ID: 'fixture-model', Name: 'Fixture model' }];
    data.Rows = [{ ...data.Summary, ID: '', Name: '', Secondary: '', State: '', ModelCalls: 1, TotalTokens: 20 }];
    data.TotalRows = 1;
    await route.fulfill({ response, json: data });
  });
  await page.goto('/#/account/usage');
  await page.getByRole('radio', { name: 'By model', exact: true }).click();
  await expect(page.locator('.usage-table')).toContainText('Unknown model');
  await expect(page.locator('.usage-table').getByRole('button', { name: 'Unknown model', exact: true })).toBeDisabled();
  await page.getByRole('combobox', { name: 'Model', exact: true }).click();
  await expect(page.getByRole('option')).toHaveText(['All models', 'Fixture model']);
});

test('usage and sandbox GET responses cache snapshots and explicit refresh replaces them', async ({ page }) => {
  await login(page);
  const year = new Date().getUTCFullYear();
  const dates = `?from=${year}-01-01&until=${year + 1}-01-01`;
  const paths = ['/api/account/usage' + dates, '/api/account/quotas', '/api/admin/usage/analytics' + dates, '/api/admin/usage', '/api/sandbox', '/api/sandboxes', '/api/sandboxes/local/resources'];
  for (const path of paths) {
    const fresh = await page.request.get(path + (path.includes('?') ? '&' : '?') + 'refresh=true');
    expect(fresh.status()).toBe(200);
    expect(fresh.headers()['x-cache']).toBe('REFRESH');
    expect(Date.parse(fresh.headers()['x-cache-written-at'])).toBeGreaterThan(0);
    const hit = await page.request.get(path);
    expect(hit.headers()['x-cache']).toBe('HIT');
    expect(hit.headers()['x-cache-written-at']).toBe(fresh.headers()['x-cache-written-at']);
    expect(await hit.json()).toEqual(await fresh.json());
  }
  for (const path of ['/api/models', '/api/resources', '/api/grants', '/api/assistants']) {
    expect((await page.request.get(path)).headers()['x-cache']).toBeUndefined();
  }
  await page.goto('/#/account/usage');
  await expect(page.locator('.usage-analytics-overview')).toBeVisible();
  const refreshed = page.waitForResponse(response => new URL(response.url()).pathname === '/api/account/quotas' && new URL(response.url()).searchParams.get('refresh') === 'true');
  await page.locator('.usage-toolbar').getByRole('button', { name: 'Refresh', exact: true }).click();
  expect((await refreshed).headers()['x-cache']).toBe('REFRESH');
});
