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
    return data === undefined ? api.get(path) : api.post(path, data);
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
    await expect.poll(async () => (await request(member, `/api/jobs?assistant_id=${assistant.ID}`)).Items[0]?.State, { timeout: 25000 }).toBe('done');
    await expect.poll(async () => (await request(member, '/api/account/quotas')).Summary.Active, { timeout: 25000 }).toBe(0);
    await member.goto('/#/account/usage');
    await expect(member.getByRole('heading', { name: 'Current member allowances' })).toBeVisible();
    await expect(member.locator('.usage-resource').filter({ hasText: model.Name })).toContainText('Monthly');
    await expect(member.locator('.usage-resource').filter({ hasText: 'Read conversations' })).toBeVisible();
    const personal = await request(member, '/api/account/quotas');
    const stats = await request(page, '/api/admin/usage'), row = stats.find((u: any) => u.UserID === user.ID);
    expect(row.ModelCalls).toBe(personal.Summary.ModelCalls);
    expect(row.ToolCalls).toBe(personal.Summary.ToolCalls);
    expect(row.Spent).toEqual(personal.Summary.Spent);
    expect(JSON.stringify(stats)).not.toContain('Private usage fixture message');
    expect((await member.request.get('/api/admin/usage')).status()).toBe(403);
    await page.goto('/#/settings/usage');
    await page.getByRole('textbox', { name: 'Search members' }).fill(username);
    await expect(page.locator('.usage-member')).toHaveCount(1);
    await page.locator('.usage-member summary').click();
    await expect(page.locator('.usage-member-details')).toContainText('Model calls');
    await page.getByRole('textbox', { name: 'Search members' }).fill('absent-usage-fixture');
    await expect(page.getByText('No matching members', { exact: true })).toBeVisible();
    for (const current of [page, member]) expect(await current.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  } finally { await context.close(); }
});

test('usage loading failure remains retryable without fabricating zero usage', async ({ page }) => {
  await login(page);
  await page.route('**/api/account/quotas', route => route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { code: 'unavailable', message: 'fixture unavailable' } }) }));
  await page.goto('/#/account/usage');
  await expect(page.locator('.usage-overview')).toHaveCount(0);
  await expect(page.locator('.usage-page').getByRole('alert')).toBeVisible();
  await page.unroute('**/api/account/quotas');
  await page.locator('.usage-toolbar').getByRole('button', { name: 'Refresh' }).click();
  await expect(page.locator('.usage-overview')).toBeVisible();
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
      await expect(next.locator('.usage-overview').first()).toBeVisible();
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
  await mutate(page, '/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: 'Simulate an unknown provider outcome.' } });
  await expect.poll(async () => (await request(page, `/api/jobs?assistant_id=${assistant.ID}`)).Items[0]?.State).toBe('needs_review');
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
  await expect(page.locator('.usage-page [role="alert"]')).toHaveCount(0);
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  expect((await request(page, '/api/account/quotas')).Summary.ModelCalls).toBe(after.ModelCalls);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
