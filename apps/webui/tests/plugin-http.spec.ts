import { test, expect, type Page } from '@playwright/test';
import type { PluginPackage } from '@molr/sdk/plugins';
import { httpPluginPackage } from './plugin-http-test-model.mjs';
const port = process.env.MOLR_TEST_PORT ?? '5193', base = `http://127.0.0.1:${port}`;
async function post(page: Page, path: string, data: object, status = 200) {
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data });
  expect(response.status(), await response.text()).toBe(status); return response.json();
}
async function login(page: Page, username = 'owner', password = 'webui-e2e-password-2026!') {
  await page.goto('/#/extensions'); await page.getByLabel('Username', { exact: true }).fill(username); await page.getByLabel('Password', { exact: true }).fill(password); await page.getByRole('button', { name: 'Sign in', exact: true }).click(); await expect(page.getByRole('button', { name: 'Sign in', exact: true })).not.toBeVisible();
}
test('HTTP connections, personal provider switching, isolated settings and real Agent execution', async ({ page, browser }, info) => {
  test.setTimeout(90000);
  test.skip(process.env.MOLR_E2E_PLUGIN_HTTP !== '1', 'requires the local TLS provider fixture');
  await login(page);
  const id = `fixture.http-${info.project.name}`, pkg: PluginPackage = httpPluginPackage(id, `https://127.0.0.1:${process.env.MOLR_E2E_PLUGIN_PORT ?? 18886}`);
  const customPage = pkg.manifest.settingsPage!; pkg.manifest.contributes.pages = [customPage]; delete pkg.manifest.settingsPage;
  const pluginName = `HTTP fixture ${info.project.name}`; pkg.manifest.name = pluginName;
  const installed = await post(page, '/api/plugins/install', pkg);
  const mutate = (path: string, data: object) => post(page, path, { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } });
  const modelName = `Plugin HTTP fixture ${info.project.name}`, modelOrigin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await mutate('/api/resources', { Name: modelName, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: modelOrigin, AllowedOrigins: [modelOrigin], Models: [{ Name: modelName, ContextLimit: 65536, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } }] } });
  const models = await (await page.request.get('/api/models')).json(), model = models.find((m: { ResourceID: string }) => m.ResourceID === resource.ID);
  const assistant = await mutate('/api/assistants', { Name: modelName, ModelID: model.ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  await post(page, '/api/plugins/configure', { ID: id, ExpectedVersion: installed.Version, Enabled: true, Grants: ['http.request', ...customPage.capabilities], Bindings: [{ AssistantID: assistant.ID, Tools: ['lookup'] }] });
  await page.goto('/#/extensions'); await page.reload();
  const row = page.locator('.plugin-row').filter({ has: page.getByRole('heading', { name: pluginName, exact: true }) });
  await row.getByRole('button', { name: `More · ${pluginName}`, exact: true }).click(); await page.getByRole('menuitem', { name: 'Admin settings', exact: true }).click();
  const dialog = page.locator('.plugin-detail-settings');
  for (const [provider, key] of [['A', 'fixture-key-a'], ['B', 'fixture-key-b']]) {
    await dialog.getByRole('button', { name: 'Add connection', exact: true }).click();
    const editor = page.getByRole('dialog', { name: 'Service connection', exact: true });
    await editor.getByLabel('Connection name', { exact: true }).fill(`Connection ${provider} ${info.project.name}`);
    await editor.getByRole('combobox', { name: 'Provider', exact: true }).click(); await page.getByRole('option', { name: `Provider ${provider}`, exact: true }).click();
    await editor.getByLabel('API Key', { exact: true }).fill(key); await editor.getByRole('button', { name: 'Save connection', exact: true }).click(); await expect(editor).not.toBeVisible();
  }
  const connections = await post(page, '/api/plugins/connections/list', { PluginID: id, Digest: installed.Digest });
  expect(connections).toHaveLength(2); expect(JSON.stringify(connections)).not.toContain('fixture-key');
  const a = connections.find((c: { ProviderID: string }) => c.ProviderID === 'a'), b = connections.find((c: { ProviderID: string }) => c.ProviderID === 'b');
  await dialog.locator('.plugin-connection-row').filter({ hasText: a.Name }).getByRole('button', { name: 'Test connection', exact: true }).click(); await expect(dialog.getByRole('status').filter({ hasText: 'Connection test passed' })).toBeVisible();
  await page.screenshot({ path: info.outputPath('plugin-connections.png') });

  await page.goto('/#/extensions'); await page.reload();
  await page.goto(`/#/extensions/${id}/@settings`);
  async function select(name: string) {
    await page.getByRole('combobox', { name: 'Search service', exact: true }).click(); await page.getByRole('option', { name: `${name} · Provider ${name.includes(' A ') ? 'A' : 'B'}`, exact: true }).click();
    await page.getByRole('button', { name: 'Save personal settings', exact: true }).click(); await expect(page.getByText('Saved. Applies to the next call.', { exact: true })).toBeVisible();
  }
  await select(a.Name); await page.reload(); await page.goto(`/#/extensions/${id}/@settings`); await expect(page.getByRole('combobox', { name: 'Search service', exact: true })).toContainText(a.Name);
  const first = await post(page, '/api/plugins/settings/read', { ID: id, Digest: installed.Digest });
  await post(page, '/api/plugins/settings/save', { ID: id, Digest: installed.Digest, ExpectedVersion: first.Version - 1, Values: first.Values }, 409);
  await page.goto(`/#/extensions/${id}/${customPage.id}`);
  await expect(page.frameLocator('iframe').getByText('Language: en', { exact: true })).toBeVisible();
  await page.frameLocator('iframe').getByRole('button', { name: 'Set language to Chinese', exact: true }).click(); await expect(page.frameLocator('iframe').getByText('Language: zh', { exact: true })).toBeVisible();
  await page.goto(`/#/assistants/${assistant.ID}`); await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).fill('Use the HTTP fixture.'); await page.getByRole('button', { name: 'Send', exact: true }).click(); await expect(page.locator('.chat-history')).toContainText('A: fixture', { timeout: 30000 });
  const caps = await (await page.request.get(`/api/assistants/${assistant.ID}/capabilities`)).json(); expect(caps.Tools.some((t: { Name: string; Available: boolean }) => t.Name.startsWith('plg_') && t.Available)).toBe(true);
  await page.goto('/#/extensions'); await page.goto(`/#/extensions/${id}/@settings`); await select(b.Name);
  await page.goto(`/#/assistants/${assistant.ID}`); await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).fill('Use the selected provider again.'); await page.getByRole('button', { name: 'Send', exact: true }).click(); await expect(page.locator('.chat-history')).toContainText('B: fixture', { timeout: 30000 });
  await page.screenshot({ path: info.outputPath('plugin-http-agent.png') });
  const group = await mutate('/api/admin/groups', { Group: { Name: `HTTP group ${info.project.name}`, Resources: [{ Kind: 'plugin', ID: (await (await page.request.get('/api/plugins')).json()).find((p: { ID: string }) => p.ID === id).CatalogID, Quota: {} }, { Kind: 'connection', ID: a.ID, Quota: {} }] } });
  const username = `http-${info.project.name}`; await mutate('/api/admin/users', { Username: username, DisplayName: username, Password: 'plugin-member-password-2026!', GroupID: group.ID });
  const context = await browser.newContext({ baseURL: base }); const member = await context.newPage(); await login(member, username, 'plugin-member-password-2026!');
  const visible = await post(member, '/api/plugins/connections/list', { PluginID: id, Digest: installed.Digest }); expect(visible.map((c: { ID: string }) => c.ID)).toEqual([a.ID]);
  await post(member, '/api/plugins/settings/save', { ID: id, Digest: installed.Digest, ExpectedVersion: 0, Values: { backend: a.ID, language: 'en' } });
  await post(member, '/api/plugins/settings/save', { ID: id, Digest: installed.Digest, ExpectedVersion: 1, Values: { backend: b.ID } }, 403);
  const ownerSettings = await post(page, '/api/plugins/settings/read', { ID: id, Digest: installed.Digest }); expect(ownerSettings.Values.backend).toBe(b.ID);
  await context.close();
});
