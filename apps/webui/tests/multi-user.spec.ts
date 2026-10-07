import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import { randomUUID } from 'node:crypto';

const username = process.env.MOLR_MULTIUSER_ADMIN ?? 'owner';
const password = process.env.MOLR_MULTIUSER_PASSWORD ?? 'webui-e2e-password-2026!';
const modelOrigin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
async function login(page: Page, name = username, secret = password) {
  await page.goto('/');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill(name);
  await page.getByLabel('Password', { exact: true }).fill(secret);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
}
async function request(page: Page, path: string, data?: unknown) {
  return page.evaluate(async ({ path, data }) => {
    const clientPath = '/src/api/client.ts', { api } = await import(clientPath);
    return data === undefined ? api.get(path) : api.post(path, data);
  }, { path, data });
}
async function mutate(page: Page, path: string, data: object, version = 0) {
  return request(page, path, { ...data, Mutation: { IdempotencyKey: randomUUID(), ExpectedVersion: version } });
}
async function ordinaryUser(page: Page) {
  const suffix = randomUUID().slice(0, 8), name = `member-${suffix}`, secret = `multi-user-${suffix}-password!`;
  const provider = await mutate(page, '/api/resources', { Kind: 'model', Name: `Multi-user ${suffix}`, Provider: 'openai-compatible', Binding: { Endpoint: `${modelOrigin}/v1`, AllowedOrigins: [modelOrigin], Models: [{ Name: `Multi-user ${suffix}`, SupportsTools: true, ContextLimit: 131072, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 1000000, CachedInputMicrosPerMillion: 0 } }] } });
  const models = await request(page, '/api/models'), model = models.find((m: any) => m.ResourceID === provider.ID);
  const group = await mutate(page, '/api/admin/groups', { Group: { Name: `Group ${suffix}`, Administrator: false, Resources: [{ Kind: 'model', ID: model.ID, Quota: { Budget: { Currency: 'USD', Micros: 10000000 }, Calls: 1000, Concurrent: 2, Period: 'month', Timezone: 'UTC' } }, ...['conversation_read', 'conversation_publish'].map(ID => ({ Kind: 'tool', ID, Quota: {} }))] } });
  const user = await mutate(page, '/api/admin/users', { Username: name, DisplayName: `Member ${suffix}`, Password: secret, GroupID: group.ID });
  return { name, secret, user, group, model };
}

test('separate settings entries and user-group resource reference editor persist', async ({ page }) => {
  await login(page);
  const nav = page.getByRole('navigation', { name: 'Molr' });
  await expect(nav.getByRole('button', { name: 'User settings', exact: true })).toBeVisible();
  await nav.getByRole('button', { name: 'User settings', exact: true }).click();
  await expect(page).toHaveURL(/#\/account/);
  await expect(page.locator('.settings-nav')).not.toContainText('Plugins');
  await nav.getByRole('button', { name: 'Admin settings', exact: true }).click();
  await page.locator('.settings-nav > button').filter({ hasText: /^User groups$/ }).click();
  await expect(page.locator('.settings-panel')).toContainText('All resources');
  await page.getByRole('button', { name: 'Add group', exact: true }).click();
  const name = `UI group ${randomUUID().slice(0, 8)}`;
  const dialog = page.getByRole('dialog', { name: 'Add group', exact: true });
  await dialog.getByRole('textbox', { name: 'Group name', exact: true }).fill(name);
  await dialog.getByRole('tab', { name: 'Tools', exact: true }).click();
  await dialog.getByRole('checkbox', { name: 'conversation_read', exact: true }).check();
  await dialog.getByRole('button', { name: 'Quotas', exact: true }).click();
  const quota = page.getByRole('dialog', { name: 'conversation_read · Quotas', exact: true });
  await quota.getByLabel('Call limit (0: unlimited)', { exact: true }).fill('20');
  await quota.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(quota).not.toBeVisible();
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await page.reload();
  await expect(page.locator('.settings-panel')).toContainText(name);
  const groups = await request(page, '/api/admin/groups'), saved = groups.find((g: any) => g.Name === name);
  expect(saved.Resources).toHaveLength(1);
  expect(saved.Resources[0].Quota.Calls).toBe(20);
  await page.getByRole('navigation', { name: 'Molr' }).getByRole('button', { name: 'User settings', exact: true }).click();
  await expect(page.locator('.settings-nav')).toContainText('Security');
});

test('ordinary user creates and edits own assistant but cannot configure shared resources', async ({ page, browser }) => {
  await login(page); const u = await ordinaryUser(page);
  const context = await browser.newContext({ baseURL: new URL(page.url()).origin, locale: 'en-US' });
  try {
    const member = await context.newPage(); await login(member, u.name, u.secret);
    const nav = member.getByRole('navigation', { name: 'Molr' });
    await expect(nav.getByRole('button', { name: 'Admin settings', exact: true })).toHaveCount(0);
    await expect(nav.getByRole('button', { name: 'Resources', exact: true })).toHaveCount(0);
    const a = await mutate(member, '/api/assistants', { Name: 'My independent assistant', ModelID: u.model.ID, Budget: { Limit: { Currency: 'USD', Micros: 1000000 }, Period: 'daily' } });
    await member.goto(`/#/assistants/${a.ID}`);
    await expect(member.locator('.workspace-main')).toContainText('My independent assistant');
    await mutate(member, '/api/assistants/update', { ID: a.ID, Name: 'My renamed assistant', ModelID: u.model.ID, Budget: a.Budget }, a.Version);
    await member.reload(); await expect(member.locator('.workspace-main')).toContainText('My renamed assistant');
    await mutate(member, '/api/conversations/messages', { ConversationID: a.ConversationID, Content: { Kind: 'text', Text: 'Private multi-user acceptance message' } });
    await expect.poll(async () => (await request(member, `/api/conversations/${a.ConversationID}/messages`)).Items.some((m: any) => m.Sender === 'assistant'), { timeout: 25000 }).toBe(true);
    for (const path of ['/api/admin/users', '/api/admin/groups', '/api/admin/catalog', '/api/admin/usage']) expect((await member.request.get(path)).status()).toBe(403);
    const denied = await member.evaluate(async () => { const path = '/src/api/client.ts', { api } = await import(path); try { await api.post('/api/plugins/install', {}); return 200; } catch (e: any) { return e.status; } });
    expect(denied).toBe(403);
    expect([403, 404]).toContain((await page.request.get(`/api/assistants/${a.ID}`)).status());
    expect([403, 404]).toContain((await page.request.get(`/api/conversations/${a.ConversationID}/messages`)).status());
    await member.goto('/#/settings'); await expect(member.locator('.workspace-main')).toContainText('Administrator access required');
    await nav.getByRole('button', { name: 'User settings', exact: true }).click();
    await expect(member.locator('.settings-nav')).not.toContainText('Plugins');
    const stats = await request(page, '/api/admin/usage');
    expect(JSON.stringify(stats)).not.toContain('Private multi-user acceptance message');
    expect(stats.find((s: any) => s.UserID === u.user.ID).ModelCalls).toBeGreaterThan(0);
  } finally { await context.close(); }
});

test('revoking a group resource removes an existing assistant model after refresh', async ({ page, browser }) => {
  await login(page); const u = await ordinaryUser(page);
  const context = await browser.newContext({ baseURL: new URL(page.url()).origin, locale: 'en-US' });
  try {
    const member = await context.newPage(); await login(member, u.name, u.secret);
    await mutate(member, '/api/assistants', { Name: 'Resource revocation assistant', ModelID: u.model.ID, Budget: { Limit: { Currency: 'USD', Micros: 1000000 }, Period: 'daily' } });
    const models = await request(member, '/api/models'); expect(models.map((m: any) => m.ID)).toEqual([u.model.ID]);
    await mutate(page, '/api/admin/groups', { Group: { ...u.group, Resources: u.group.Resources.filter((r: any) => r.Kind !== 'model') } }, u.group.Version);
    await member.reload(); expect(await request(member, '/api/models')).toEqual([]);
    expect((await request(member, '/api/assistants')).Items).toHaveLength(1);
  } finally { await context.close(); }
});

test('account changes in another tab discard the previous identity and navigation', async ({ page, context }) => {
  await login(page); const u = await ordinaryUser(page);
  const second = await context.newPage(); await second.goto('/');
  await expect(second.getByRole('navigation', { name: 'Molr' }).getByRole('button', { name: 'Admin settings', exact: true })).toBeVisible();
  await page.goto('/#/account/security');
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(second.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
  await login(page, u.name, u.secret);
  await expect(second.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  await expect(second.getByRole('navigation', { name: 'Molr' }).getByRole('button', { name: 'Admin settings', exact: true })).toHaveCount(0);
  const me = await request(second, '/api/auth/me'); expect(me.OwnerID).toBe(u.user.ID);
});
