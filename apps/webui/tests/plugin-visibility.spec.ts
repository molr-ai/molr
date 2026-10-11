import { expect, test, type Page } from '@playwright/test';
import type { Settings } from '@molr/sdk';
import { showPlugins } from './plugin-visibility-helper';

async function login(page: Page, username = 'owner', password = 'webui-e2e-password-2026!') {
  await page.goto('/#/account/general');
  await page.getByLabel('Username', { exact: true }).fill(username);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('switch', { name: 'Show plugins', exact: true })).toBeVisible();
}
async function post(page: Page, path: string, data: object) {
  const origin = new URL(page.url()).origin;
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${new URL(origin).port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: origin, 'X-CSRF-Token': csrf }, data });
  expect(response.status(), await response.text()).toBe(200);
  return response.json();
}
const settings = async (page: Page): Promise<Settings> => (await page.request.get('/api/settings')).json();
const mutation = (version = 0) => ({ IdempotencyKey: crypto.randomUUID(), ExpectedVersion: version });

test('plugin visibility defaults off per account, syncs across devices and survives failed saves', async ({ page, browser }, info) => {
  await login(page);
  const username = `visibility-${info.project.name}`, password = 'visibility-member-password-2026!';
  await post(page, '/api/admin/users', { Username: username, DisplayName: username, Password: password, GroupID: 'users', Mutation: mutation() });
  const contexts = await Promise.all([browser.newContext({ baseURL: new URL(page.url()).origin }), browser.newContext({ baseURL: new URL(page.url()).origin })]);
  try {
    const [first, second] = await Promise.all(contexts.map(context => context.newPage()));
    await login(first, username, password); await login(second, username, password);
    expect((await settings(first)).PluginsVisible).toBe(false);
    const toggle = first.getByRole('switch', { name: 'Show plugins', exact: true });
    await expect(toggle).not.toBeChecked();
    await expect(first.locator('.workspace-nav').getByRole('button', { name: 'Plugins', exact: true })).toHaveCount(0);
    await showPlugins(first);
    await second.reload();
    await expect(second.getByRole('switch', { name: 'Show plugins', exact: true })).toBeChecked();
    await second.getByRole('switch', { name: 'Show plugins', exact: true }).uncheck();
    await expect.poll(async () => (await settings(second)).PluginsVisible).toBe(false);
    await first.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
    await expect(first).toHaveURL(/#\/account\/general/);
    await expect(toggle).not.toBeChecked();
    await first.route('**/api/settings', route => route.request().method() === 'POST'
      ? route.fulfill({ status: 503, json: { code: 'unavailable' } }) : route.continue());
    await toggle.check();
    await expect(toggle).not.toBeChecked();
    expect((await settings(first)).PluginsVisible).toBe(false);
    await expect(first.locator('.workspace-nav').getByRole('button', { name: 'Plugins', exact: true })).toHaveCount(0);
  } finally { await Promise.all(contexts.map(context => context.close())); }
});

test('hiding plugins removes routes, themes and group controls without changing saved permissions', async ({ page }, info) => {
  await login(page); await showPlugins(page);
  const id = `visibility.theme-${info.project.name}`, name = `Visibility theme ${info.project.name}`;
  const installed = await post(page, '/api/plugins/install', { manifest: { manifestVersion: 1, pluginApi: 3, id, name, description: 'Plugin visibility fixture', version: '1.0.0', contributes: { themes: [{ id: 'theme', name, pages: { plans: 'ui.html' }, capabilities: [] }], pages: [], tools: [] } }, files: { 'ui.html': `<p>Visibility theme active</p><script>parent.postMessage({type:'molr.plugin.ready',apiVersion:2},'*')</script>` } });
  await post(page, '/api/plugins/configure', { ID: id, ExpectedVersion: installed.Version, Enabled: true, Grants: [], Bindings: [] });
  const catalog = await (await page.request.get('/api/admin/catalog')).json();
  const plugin = catalog.find((item: { Kind: string; Name: string }) => item.Kind === 'plugin' && item.Name === name);
  const refs = [{ Kind: 'plugin', ID: plugin.ID, Quota: { Calls: 0, Concurrent: 0, Period: 'month', Timezone: 'UTC' } }];
  const group = await post(page, '/api/admin/groups', { Group: { Name: name, Resources: refs }, Mutation: mutation() });
  await page.goto('/#/account/theme');
  // Fixtures were installed through the API; refresh the browser's plugin snapshot.
  await page.reload();
  await page.getByRole('combobox', { name: 'Theme', exact: true }).click();
  await page.getByRole('option', { name, exact: true }).click();
  await page.goto('/#/plans');
  await expect(page.frameLocator('iframe').getByText('Visibility theme active')).toBeVisible();
  await page.goto('/#/account/general');
  await page.getByRole('switch', { name: 'Show plugins', exact: true }).uncheck();
  await expect.poll(async () => (await settings(page)).PluginsVisible).toBe(false);
  await page.goto('/#/extensions'); await expect(page).toHaveURL(/#\/account\/general/);
  await page.goto('/#/plans'); await expect(page.locator('.plans-workspace')).toBeVisible();
  await expect(page.locator('iframe')).toHaveCount(0);
  await page.goto('/#/account/theme');
  await page.getByRole('combobox', { name: 'Theme', exact: true }).click();
  await expect(page.getByRole('option', { name, exact: true })).toHaveCount(0);
  await page.keyboard.press('Escape');
  await page.goto('/#/settings/groups');
  await page.locator('.settings-row-item').filter({ has: page.getByRole('heading', { name, exact: true }) }).getByRole('button', { name: 'Edit', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Edit group', exact: true });
  await expect(dialog.getByRole('tab', { name: 'Plugins', exact: true })).toHaveCount(0);
  await expect(dialog.getByRole('tab', { name: 'Connections', exact: true })).toHaveCount(0);
  await dialog.getByLabel('Group name', { exact: true }).fill(name + ' renamed');
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  const saved = (await (await page.request.get('/api/admin/groups')).json()).find((item: { ID: string }) => item.ID === group.ID);
  expect(saved.Resources).toEqual(group.Resources);
  expect((await settings(page)).UITheme).toBe(`${id}.theme@1.0.0`);
  await showPlugins(page); await page.goto('/#/plans');
  await expect(page.frameLocator('iframe').getByText('Visibility theme active')).toBeVisible();
  const current = await settings(page);
  await post(page, '/api/settings', { Settings: { ...current, UITheme: 'molr.official@1.0.0', PluginsVisible: false }, Mutation: mutation(current.Version) });
});
