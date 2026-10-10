import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';

test.setTimeout(120_000);
const port = process.env.MOLR_TEST_PORT ?? '5193', base = `http://127.0.0.1:${port}`;
async function post(page: Page, path: string, data: object, version?: number) {
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID(), ...(version ? { ExpectedVersion: version } : {}) } } });
  expect(response.status(), await response.text()).toBe(200); return response.json();
}
async function setup(page: Page, suffix: string) {
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill(process.env.MOLR_PROFILE_TEST_PASSWORD ?? 'webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const name = `Profile ${suffix}-${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  const model = models.find((m: any) => m.ResourceID === resource.ID);
  const assistant = await post(page, '/api/assistants', { Name: name, ModelID: model.ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  await page.goto(`/#/assistants/${assistant.ID}`);
  await expect(page.locator('.chat-header h1')).toHaveText(name);
  return assistant;
}
async function send(page: Page, input: object, reply: string) {
  await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).fill(JSON.stringify(input));
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page.locator('.ui-chat-message--assistant').last()).toContainText(reply, { timeout: 25000 });
}

test('profile tool updates presets, file avatars and context without refreshing the page', async ({ page }, info) => {
  const a = await setup(page, info.project.name);
  const get = async () => (await page.request.get(`/api/assistants/${a.ID}`)).json();
  await send(page, { name: '小满', avatar: { type: 'preset', id: 'cat' } }, 'Profile saved: 小满 / preset:cat');
  await expect(page.locator('.chat-header h1')).toHaveText('小满');
  expect((await get()).Avatar).toBe('preset:cat');
  await send(page, { name: '小满的新名字' }, 'Profile saved: 小满的新名字 / preset:cat');
  await expect(page.locator('.chat-header h1')).toHaveText('小满的新名字');
  const upload = await page.evaluate(() => { const canvas = document.createElement('canvas'); canvas.width = 600; canvas.height = 400; const ctx = canvas.getContext('2d')!; ctx.fillStyle = '#6d9e81'; ctx.fillRect(0, 0, 600, 400); return canvas.toDataURL('image/webp').split(',')[1]; });
  await page.locator('.chat-input input[type=file]').setInputFiles({ name: 'avatar.webp', mimeType: 'image/webp', buffer: Buffer.from(upload, 'base64') });
  await expect(page.locator('.composer-attachments')).toContainText('avatar.webp');
  await send(page, { avatar: { type: 'file', path: '/work/profile-upload' } }, 'Profile saved: 小满的新名字 / image');
  expect((await get()).Avatar).toMatch(/^data:image\/jpeg;base64,/);
  await expect(page.locator('.ui-chat-message--assistant .ui-chat-avatar img').first()).toBeVisible();
  await page.screenshot({ path: info.outputPath('profile-avatar.png') });
  await send(page, { avatar: { type: 'clear' } }, 'Profile saved: 小满的新名字 / initial');
  expect((await get()).Avatar).toBe('');
  await page.reload(); await expect(page.locator('.chat-header h1')).toHaveText('小满的新名字');
  await page.locator('.chat-assistant-info').click();
  await expect(page.getByRole('textbox', { name: 'Assistant name', exact: true })).toHaveValue('小满的新名字');
  await page.getByRole('tab', { name: 'Tools', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Update profile', exact: true })).toBeVisible();
});

test('open profile forms sync clean values and retain dirty drafts with their original version', async ({ page }, info) => {
  const a = await setup(page, `form-${info.project.name}`);
  await page.locator('.chat-assistant-info').click();
  const field = page.getByRole('textbox', { name: 'Assistant name', exact: true });
  const publish = (args: object) => post(page, '/api/conversations/messages', { ConversationID: a.ConversationID, Content: { Kind: 'text', Text: JSON.stringify(args) } });
  await publish({ name: '同步的新名字', avatar: { type: 'preset', id: 'fox' } });
  await expect(field).toHaveValue('同步的新名字', { timeout: 25000 });
  await expect(page.getByRole('button', { name: 'Fox', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await field.fill('尚未保存的草稿');
  await publish({ name: '模型再次改名' });
  await expect.poll(async () => (await (await page.request.get(`/api/assistants/${a.ID}`)).json()).Name, { timeout: 30_000 }).toBe('模型再次改名');
  await expect(field).toHaveValue('尚未保存的草稿');
  const response = page.waitForResponse(r => r.url().endsWith('/api/assistants/update'));
  await page.locator('.assistant-info').getByRole('button', { name: 'Save', exact: true }).click();
  expect((await response).status()).toBe(409);
  expect((await (await page.request.get(`/api/assistants/${a.ID}`)).json()).Name).toBe('模型再次改名');
});

test('profile updates also refresh the test theme', async ({ page }, info) => {
  const a = await setup(page, `theme-${info.project.name}`);
  const before = await (await page.request.get('/api/settings')).json();
  try {
    await post(page, '/api/settings', { Settings: { ...before, UITheme: 'molr.test@1.0.0' } }, before.Version);
    await page.reload();
    await send(page, { name: '测试主题的小满', avatar: { type: 'preset', id: 'star' } }, 'Profile saved: 测试主题的小满 / preset:star');
    await expect(page.locator('.chat-header h1')).toHaveText('测试主题的小满');
    expect((await (await page.request.get(`/api/assistants/${a.ID}`)).json()).Avatar).toBe('preset:star');
  } finally {
    const current = await (await page.request.get('/api/settings')).json();
    await post(page, '/api/settings', { Settings: before }, current.Version);
  }
});
