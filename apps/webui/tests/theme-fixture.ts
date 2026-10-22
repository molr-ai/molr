import { expect, type Page } from '@playwright/test';
import type { PluginPackage } from '@molr/sdk/plugins';

export const fixtureTheme = 'fixture.theme.theme@1.0.0';
export async function installThemeFixture(page: Page) {
  const origin = new URL(page.url()).origin;
  const csrf = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${new URL(origin).port}`)!.value;
  async function post(path: string, data: object) {
    const response = await page.request.post(path, { headers: { Origin: origin, 'X-CSRF-Token': csrf }, data });
    expect(response.status(), await response.text()).toBe(200);
    return response.json();
  }
  const settings = await (await page.request.get('/api/settings')).json();
  await post('/api/settings', { Settings: { ...settings, PluginsVisible: true }, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: settings.Version } });
  const pkg: PluginPackage = {
    manifest: { manifestVersion: 1, pluginApi: 3, id: 'fixture.theme', name: 'Theme fixture', description: 'Theme integration test fixture', version: '1.0.0', contributes: {
      themes: [{ id: 'theme', name: 'Fixture theme', pages: { plans: 'page.html' }, capabilities: [] }], pages: [], tools: [],
    } },
    files: { 'page.html': `<p>Fixture plans</p><script>parent.postMessage({type:'molr.plugin.ready',apiVersion:2},'*')</script>` },
  };
  const installed = await post('/api/plugins/install', pkg);
  await post('/api/plugins/configure', { ID: pkg.manifest.id, ExpectedVersion: installed.Version, Enabled: true, Grants: [], Bindings: [] });
  await page.reload();
}
