import { test, expect, type Page } from '@playwright/test';
import type { PluginPackage } from '@molr/sdk/plugins';
const port = process.env.MOLR_TEST_PORT ?? '5193', base = `http://127.0.0.1:${port}`;
async function post(page: Page, path: string, data: object, status = 200) {
 const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
 const r = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data }); expect(r.status(), await r.text()).toBe(status); return r.json();
}
async function login(page: Page) {
 await page.goto('/#/extensions'); await page.getByLabel('Username', { exact: true }).fill('owner'); await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!'); await page.getByRole('button', { name: 'Sign in', exact: true }).click(); await expect(page.getByRole('button', { name: 'Import plugin package' })).toBeVisible();
}
// Minimal in-memory contract fixtures; no installable demo packages are shipped.
async function pkg(name: string): Promise<PluginPackage> {
 const contributes: PluginPackage['manifest']['contributes'] = { themes: [], pages: [], tools: [], skills: [] };
 const files: Record<string, string> = {};
 if (name === 'fixture.page') {
  contributes.themes = [{ id: 'theme', name: 'Fixture theme', pages: { plans: 'ui.html' }, capabilities: ['plans.read'] }];
  contributes.pages = [{ id: 'page', title: 'Fixture page', entry: 'ui.html', capabilities: ['plans.read'] }];
  files['ui.html'] = `<p id="status">waiting</p><script>addEventListener('message',e=>{if(e.source!==parent||e.data?.type!=='molr.plugin.connect')return;const port=e.ports[0];port.onmessage=({data})=>{document.getElementById('status').textContent=data.error?'denied':'connected'};port.postMessage({id:1,method:'plans.list'})});parent.postMessage({type:'molr.plugin.ready',apiVersion:2},'*')</script>`;
 } else {
  contributes.tools = [{ id: 'report', description: 'Write the integration test receipt.', entry: 'report.sh', execution: { type: 'shell' }, capabilities: ['compute.execute', 'workspace.write'], inputSchema: { type: 'object', properties: { label: { type: 'string' } }, required: ['label'], additionalProperties: false } }];
  contributes.skills = [{ id: 'skill', name: 'Fixture workflow', description: 'Integration test workflow.', entry: 'SKILL.md', tools: ['report'] }];
  files['report.sh'] = 'set -eu\nmkdir -p /work/plugin-fixture\nprintf "plugin-fixture-report\\n%s\\n" "$1" > /work/plugin-fixture/report.txt\ncat /work/plugin-fixture/report.txt\n';
  files['SKILL.md'] = 'Call the report tool listed below, then read /work/plugin-fixture/report.txt to verify its output.';
 }
 return { manifest: { manifestVersion: 1, pluginApi: 2, id: name, name: name === 'fixture.page' ? 'Fixture page' : 'Fixture tools', description: 'Integration test fixture', version: '1.0.0', contributes }, files };
}
async function enable(page: Page, name: string, bindings: object[] = []) {
 const value = await pkg(name); const installed = await post(page, '/api/plugins/install', value);
 const grants = [...new Set([...value.manifest.contributes.pages, ...value.manifest.contributes.themes, ...value.manifest.contributes.tools].flatMap((t: { capabilities: string[] }) => t.capabilities))];
 return post(page, '/api/plugins/configure', { ID: name, ExpectedVersion: installed.Version, Enabled: true, Grants: grants, Bindings: bindings });
}
test('package import, isolated real page, theme selection, revocation and immutable releases', async ({ page }, info) => {
 await login(page);
 const imported = page.waitForResponse(r => r.url().endsWith('/api/plugins/install') && r.request().method() === 'POST');
 await page.locator('input[type=file]').setInputFiles({ name: 'fixture.molr-plugin.json', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify(await pkg('fixture.page'))) }); await imported; await page.reload(); await expect(page.getByRole('button', { name: 'Import plugin package' })).toBeVisible();
 const row = page.locator('.plugin-row').filter({ has: page.getByRole('heading', { name: 'Fixture page', exact: true }) });
 await row.getByRole('button', { name: 'More · Fixture page', exact: true }).click(); await page.getByRole('menuitem', { name: 'Permissions and assistants', exact: true }).click();
 const dialog = page.getByRole('dialog', { name: 'Fixture page', exact: true });
 await dialog.getByLabel('Read account plans', { exact: true }).check(); await dialog.getByRole('switch', { name: 'Enable plugin', exact: true }).check();
 await dialog.getByRole('button', { name: 'Save configuration' }).click(); await expect(dialog).not.toBeVisible();
 await row.getByRole('button', { name: 'More · Fixture page', exact: true }).click();
 await page.getByRole('menuitem', { name: 'Open · Fixture page', exact: true }).click();
 const frame = page.frameLocator('iframe'); await expect(frame.getByText('connected', { exact: true })).toBeVisible(); expect(await page.locator('iframe').getAttribute('sandbox')).toBe('allow-scripts');
 await expect(page.locator('.plugin-frame .page-header').getByRole('heading', { name: 'Fixture page', exact: true })).toBeVisible();
 const child = page.frames().find(f => f.url().startsWith('blob:') && !f.url().includes('#'))!;
 expect(await child.evaluate(async () => { try { await fetch('/api/settings'); return false; } catch { return true; } })).toBe(true);
 expect(await child.evaluate(() => { try { void parent.document.body; return false; } catch { return true; } })).toBe(true);
 await page.screenshot({ path: info.outputPath('plugin-page.png') });
 await page.goto('/#/account/theme'); await page.getByRole('combobox', { name: 'Theme', exact: true }).click(); await page.getByRole('option', { name: 'Fixture theme', exact: true }).click(); await page.goto('/#/plans'); await expect(page.frameLocator('iframe').getByText('connected', { exact: true })).toBeVisible();
 await page.goto('/#/extensions'); const installed = (await (await page.request.get('/api/plugins')).json()).find((p: { ID: string }) => p.ID === 'fixture.page');
 const malicious = await pkg('fixture.page'); malicious.files['ui.html'] += 'changed'; await post(page, '/api/plugins/install', malicious, 409);
 await post(page, '/api/plugins/configure', { ID: installed.ID, ExpectedVersion: installed.Version, Enabled: false, Grants: [], Bindings: [] });
 await post(page, '/api/plugins/ui', { ID: installed.ID, Digest: installed.Digest, Kind: 'page', Contribution: 'page', Method: 'plans.list' }, 403);
 await page.reload(); await page.goto('/#/plans'); await expect(page.locator('iframe')).toHaveCount(0);
});
test('real Agent discovers Skill and executes the bound plugin in its sandbox', async ({ page }, info) => {
 test.skip(process.env.MOLR_E2E_SANDBOX_SERVICE !== '1', 'requires delegated sandbox');
 await login(page); const sandbox = await (await page.request.get('/api/sandbox')).json(); expect(sandbox.Enabled).toBe(true); expect(sandbox.Capabilities.Reason).toBe('available');
 const mutate = (path: string, data: object) => post(page, path, { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } });
 const name = `Plugin fixture ${info.project.name} ${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
 const resource = await mutate('/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 65536, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } }] } });
 const models = await (await page.request.get('/api/models')).json(), model = models.find((m: { ResourceID: string }) => m.ResourceID === resource.ID);
 const assistant = await mutate('/api/assistants', { Name: name, ModelID: model.ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
 let compute = (await (await page.request.get('/api/resources')).json()).Items.find((r: { Kind: string }) => r.Kind === 'compute'); if (!compute) compute = await mutate('/api/sandboxes/resource', { SandboxID: 'local', Kind: 'compute', SourceID: 'compute', Name: 'Plugin test compute' });
 await mutate('/api/grants', { AssistantID: assistant.ID, ResourceID: compute.ID, Actions: ['execute'], Scope: {}, Constraints: { compute: { cpu_percent: 100, memory_bytes: 536870912, processes: 64, max_duration_seconds: 60, unlimited_duration: false, temp_bytes: 67108864 } } });
 await enable(page, 'fixture.tools', [{ AssistantID: assistant.ID, ComputeID: compute.ID, Tools: ['report'], Skills: ['skill'] }]);
 await page.goto(`/#/assistants/${assistant.ID}`); await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).fill('Generate a workspace report using the plugin Skill.'); await page.getByRole('button', { name: 'Send', exact: true }).click();
 await expect(page.locator('.chat-history')).toContainText('Plugin test report verified.', { timeout: 30000 });
 const caps = await (await page.request.get(`/api/assistants/${assistant.ID}/capabilities`)).json(); expect(caps.Skills.Items.some((sk: { ID: string; Enabled: boolean }) => sk.ID.startsWith('plugin:fixture.tools') && sk.Enabled)).toBe(true);
 await page.screenshot({ path: info.outputPath('plugin-tool-report.png') });
});

async function trackHTMLURLs(page: Page) {
 await page.addInitScript(() => {
  const create = URL.createObjectURL.bind(URL), revoke = URL.revokeObjectURL.bind(URL);
  const active = new Set<string>(); (window as any).pluginHTMLURLs = active;
  URL.createObjectURL = blob => { const url = create(blob); if (blob instanceof Blob && blob.type.startsWith('text/html')) active.add(url); return url; };
  URL.revokeObjectURL = url => { active.delete(url); revoke(url); };
 });
}

test('plugin HTML blob URLs are released after repeated navigation', async ({ page }, info) => {
 await trackHTMLURLs(page);
 await login(page); await enable(page, 'fixture.page'); await page.reload();
 for (let visit = 0; visit < 3; visit++) {
  await page.evaluate(() => { location.hash = '/extensions/fixture.page/page'; });
  await expect(page.frameLocator('iframe').getByText('connected', { exact: true })).toBeVisible();
  const mounted = await page.evaluate(() => (window as any).pluginHTMLURLs.size);
  await page.evaluate(() => { location.hash = '/extensions'; });
  await expect(page.locator('.plugin-frame iframe')).toHaveCount(0);
  const unmounted = await page.evaluate(() => (window as any).pluginHTMLURLs.size);
  await info.attach(`plugin-urls-${visit}.json`, { body: JSON.stringify({ visit, mounted, unmounted }), contentType: 'application/json' });
  expect(unmounted, 'HTML URLs must not retain plugin documents after unmount').toBe(0);
  expect(mounted, 'Only the active iframe document needs an HTML URL').toBe(1);
 }
});


test('plugin document URLs follow reference changes, theme updates and retries', async ({ page }, info) => {
 await trackHTMLURLs(page); await login(page);
 const value = await pkg('fixture.page');
 value.manifest.id = `fixture.lifecycle-${info.project.name}`;
 value.manifest.name = `Lifecycle ${info.project.name}`;
 value.manifest.contributes.themes = [];
 value.manifest.contributes.pages.push({ id: 'second', title: 'Second page', entry: 'second.html', capabilities: ['plans.read'] });
 value.files['ui.html'] = value.files['ui.html'].replace('<script>', '<p id="theme"></p><script>').replace('const port=e.ports[0];', 'document.getElementById("theme").textContent=e.data.context.theme;const port=e.ports[0];');
 value.files['second.html'] = value.files['ui.html'].replace("'connected'", "'second connected'");
 const installed = await post(page, '/api/plugins/install', value);
 await post(page, '/api/plugins/configure', { ID: value.manifest.id, ExpectedVersion: installed.Version, Enabled: true, Grants: ['plans.read'], Bindings: [] });
 await page.reload();
 const go = (entry: string) => page.evaluate(path => { location.hash = path; }, `/extensions/${value.manifest.id}/${entry}`);
 const urls = () => page.evaluate(() => (window as any).pluginHTMLURLs.size);
 await go('page');
 await expect(page.frameLocator('iframe').getByText('connected', { exact: true })).toBeVisible();
 const originalURL = await page.locator('iframe').getAttribute('src');
 const theme = await page.frameLocator('iframe').locator('#theme').textContent();
 const nextTheme = theme === 'dark' ? 'light' : 'dark';
 await page.emulateMedia({ colorScheme: nextTheme });
 await expect(page.frameLocator('iframe').locator('#theme')).toHaveText(nextTheme);
 await expect(page.locator('iframe')).toHaveAttribute('src', originalURL!);
 expect(await urls()).toBe(1);
 let release!: () => void;
 const pending = new Promise<void>(resolve => { release = resolve; });
 await page.route('**/api/plugins/ui', async route => {
  const request = route.request().postDataJSON();
  if (request.Method === 'document' && request.Contribution === 'second') await pending;
  await route.continue();
 });
 try {
  await go('second');
  await expect(page.locator('iframe')).toHaveCount(0);
  await expect.poll(urls).toBe(0);
 } finally { release(); }
 await expect(page.frameLocator('iframe').getByText('second connected', { exact: true })).toBeVisible();
 expect(await urls()).toBe(1);
 await page.unroute('**/api/plugins/ui');
 let failed = false;
 await page.route('**/api/plugins/ui', route => {
  if (!failed && route.request().postDataJSON().Method === 'document') { failed = true; return route.fulfill({ status: 503, json: { code: 'unavailable' } }); }
  return route.continue();
 });
 await go('page');
 await expect(page.getByRole('heading', { name: 'Extension page unavailable', exact: true })).toBeVisible();
 await expect.poll(urls).toBe(0);
 await page.getByRole('button', { name: 'Retry', exact: true }).click();
 await expect(page.frameLocator('iframe').getByText('connected', { exact: true })).toBeVisible();
 expect(await urls()).toBe(1);
 await page.unroute('**/api/plugins/ui');
 await page.clock.install();
 await page.route('**/api/plugins/ui', route => route.request().postDataJSON().Method === 'document' ? route.fulfill({ json: { HTML: '<p>No handshake</p>' } }) : route.continue());
 await go('second');
 await expect(page.locator('iframe')).toHaveCount(1);
 await page.clock.fastForward(11000);
 await expect(page.getByRole('heading', { name: 'Extension page unavailable', exact: true })).toBeVisible();
 await expect(page.locator('iframe')).toHaveCount(0);
 await expect.poll(urls).toBe(0);
 await page.unroute('**/api/plugins/ui');
 await page.getByRole('button', { name: 'Retry', exact: true }).click();
 await expect(page.frameLocator('iframe').getByText('second connected', { exact: true })).toBeVisible();
 expect(await urls()).toBe(1);
});
