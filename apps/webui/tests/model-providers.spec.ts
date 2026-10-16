import { test, expect } from '@playwright/test';
const modelOrigin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
const providers = [
  ['anthropic', 'Anthropic', 'https://api.anthropic.com/v1'],
  ['gemini', 'Google Gemini', 'https://generativelanguage.googleapis.com/v1beta/openai'],
  ['minimax', 'MiniMax', 'https://api.minimax.cn/v1'],
  ['volcengine', 'Volcengine · 火山方舟', 'https://ark.cn-beijing.volces.com/api/v3'],
  ['siliconflow', 'SiliconFlow · 硅基流动', 'https://api.siliconflow.cn/v1'],
  ['lmstudio', 'LM Studio', 'http://127.0.0.1:1234/v1'],
  ['zai', 'Z.AI', 'https://api.z.ai/api/paas/v4'],
];
test('new providers preserve protocol endpoints and credentials through save and edit', async ({ page }) => {
  test.setTimeout(150_000);
  await page.goto('/#/resources');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  for (const [preset, label, address] of providers) {
    await page.locator('.resources-header').getByRole('button', { name: 'More', exact: true }).click();
    await page.getByRole('menuitem', { name: 'Connect provider', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Connect provider', exact: true });
    await dialog.getByRole('searchbox').fill(preset === 'volcengine' ? '豆包' : preset === 'siliconflow' ? '硅基流动' : label);
    await dialog.getByRole('button', { name: label, exact: true }).click();
    await dialog.getByText('More configuration', { exact: true }).click();
    const endpoint = dialog.getByLabel('Service URL', { exact: true });
    await expect(endpoint).toHaveValue(address);
    if (preset !== 'lmstudio') await dialog.getByLabel('API key', { exact: true }).fill('test-provider-secret');
    const base = `${modelOrigin}/${preset === 'anthropic' ? 'anthropic/v1' : preset === 'lmstudio' ? 'v1' : 'private/v1'}`;
    await endpoint.fill(base);
    const name = `${preset} ${Date.now()}`;
    await dialog.getByLabel('Name', { exact: true }).fill(name);
    await dialog.getByRole('button', { name: 'Test connection', exact: true }).click();
    await expect(dialog.locator('.ui-alert--positive')).toBeVisible();
    await dialog.getByRole('button', { name: 'Connect provider', exact: true }).click();
    await expect(dialog).toHaveCount(0);
    const resources = await (await page.request.get('/api/resources')).json();
    const resource = resources.Items.find((r: { Name: string }) => r.Name === name);
    const connection = await (await page.request.get(`/api/resources/${resource.ID}/connection`)).json();
    expect(connection.PricingProvider).toBe(preset);
    expect(connection.Endpoint).toBe(base + (preset === 'anthropic' ? '/messages' : '/chat/completions'));
    const models = await (await page.request.get('/api/models')).json();
    expect(models.filter((m: { ResourceID: string }) => m.ResourceID === resource.ID)).toHaveLength(preset === 'anthropic' ? 1 : 2);
    const row = page.locator('.resource-row').filter({ has: page.getByRole('button', { name, exact: true }) });
    await row.getByRole('button', { name: 'More', exact: true }).click();
    await page.getByRole('menuitem', { name: 'Edit provider', exact: true }).click();
    const edit = page.getByRole('dialog', { name: 'Edit provider', exact: true });
    await expect(edit.getByLabel('Service URL', { exact: true })).toHaveValue(base);
    if (preset !== 'lmstudio') await expect(edit.getByText('API key configured', { exact: true })).toBeVisible();
    await edit.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(edit).toHaveCount(0);
  }
});
