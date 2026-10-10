import { test, expect, type Page } from '@playwright/test';
import { httpPluginPackage } from './plugin-http-test-model.mjs';

async function post(page: Page, path: string, data: object) {
  const origin = new URL(page.url()).origin, port = new URL(origin).port;
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { data, headers: { Origin: origin, 'X-CSRF-Token': csrf } });
  expect(response.status(), await response.text()).toBe(200);
  return response.json();
}
async function observe(page: Page) {
  await page.evaluate(async () => {
    const path = '/src/api/client.ts'; const { api, onMutation } = await import(path);
    const state = window as any; state.pluginMutations = []; state.pluginReads = [];
    onMutation((path: string) => state.pluginMutations.push(path));
    const client = api as any;
    for (const method of ['post', 'read']) {
      const original = client[method];
      client[method] = async (...args: any[]) => {
        const value = await original.apply(client, args);
        state.pluginReads.push(args[0] === '/api/plugins/ui' ? args[1].Method : args[0]);
        return value;
      };
    }
  });
}
async function readDone(page: Page, path: string) {
  await page.waitForFunction(path => (window as any).pluginReads.includes(path), path);
}
async function mutations(page: Page) {
  return page.evaluate(() => (window as any).pluginMutations);
}
test.beforeEach(async ({ page }) => {
  await page.goto('/#/extensions');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Import plugin package' })).toBeVisible();
});

test('plugin settings, connections and history reads stay silent while writes notify', async ({ page }, info) => {
  const id = `fixture.read-${info.project.name}`;
  const pkg = httpPluginPackage(id, 'https://provider.example.invalid');
  pkg.manifest.name = `Read fixture ${info.project.name}`;
  Object.assign(pkg.manifest, { adminSettings: { label: { type: 'string', title: 'Review label', default: 'Before' } } });
  for (const provider of pkg.manifest.providers!) { provider.auth = { type: 'none' }; delete provider.test; }
  await post(page, '/api/plugins/install', pkg);
  await page.reload();
  await expect(page.getByRole('heading', { name: pkg.manifest.name, exact: true })).toBeVisible();
  await observe(page);
  await page.evaluate(id => { location.hash = `/extensions/${id}/@admin`; }, id);
  await readDone(page, '/api/plugins/connections/list');
  await readDone(page, '/api/plugins/settings/read');
  await expect(page.getByLabel('Review label')).toHaveValue('Before');
  expect(await mutations(page)).toEqual([]);

  await page.getByLabel('Review label').fill('After');
  await page.getByRole('button', { name: 'Save admin settings', exact: true }).click();
  await expect(page.getByText('Saved. Applies to the next call.', { exact: true })).toBeVisible();
  expect(await mutations(page)).toEqual(['/api/plugins/settings/save']);
  await page.getByRole('button', { name: 'Reload settings', exact: true }).click();
  await expect(page.getByLabel('Review label')).toHaveValue('After');

  await page.getByRole('button', { name: 'Add connection', exact: true }).click();
  await page.getByLabel('Connection name', { exact: true }).fill('Read test connection');
  await page.getByRole('button', { name: 'Save connection', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await page.getByRole('button', { name: 'Call history', exact: true }).click();
  await readDone(page, '/api/plugins/connections/history');
  await expect(page.getByRole('dialog', { name: 'Read test connection · Call history', exact: true })).toBeVisible();
  expect(await mutations(page)).toEqual(['/api/plugins/settings/save', '/api/plugins/connections/save']);
});

test('plugin frame reads do not invalidate plugins and settings updates still notify', async ({ page }, info) => {
  const id = `fixture.frame-read-${info.project.name}`;
  const pkg = httpPluginPackage(id, 'https://provider.example.invalid');
  pkg.manifest.name = `Frame read fixture ${info.project.name}`;
  pkg.manifest.settingsPage.capabilities.push('plans.read');
  // Exercise all bridge reads using the real MessageChannel and Runtime.
  pkg.files['settings.html'] = pkg.files['settings.html'].replace("port.onmessage=({data})=>{", "port.onmessage=({data})=>{if(data.id>2)return;").replace("port.postMessage({id:1,method:'settings.get'})", "port.postMessage({id:1,method:'settings.get'});port.postMessage({id:3,method:'connections.list'});port.postMessage({id:4,method:'plans.list'})");
  const installed = await post(page, '/api/plugins/install', pkg);
  await post(page, '/api/plugins/configure', { ID: id, ExpectedVersion: installed.Version, Enabled: true, Grants: [...pkg.manifest.settingsPage.capabilities, 'http.request'], Bindings: [] });
  await page.reload();
  await expect(page.getByRole('heading', { name: pkg.manifest.name, exact: true })).toBeVisible();
  await observe(page);
  await page.evaluate(id => { location.hash = `/extensions/${id}/@settings`; }, id);
  const frame = page.frameLocator('iframe');
  await expect(frame.getByText('Language: en', { exact: true })).toBeVisible();
  for (const method of ['document', 'settings.get', 'connections.list', 'plans.list']) await readDone(page, method);
  expect(await mutations(page)).toEqual([]);
  await frame.getByRole('button', { name: 'Set language to Chinese', exact: true }).click();
  await expect(frame.getByText('Language: zh', { exact: true })).toBeVisible();
  expect(await mutations(page)).toEqual(['/api/plugins/ui']);
});
