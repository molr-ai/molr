import { test, expect, type Page } from '@playwright/test';
import type { PluginPackage } from '@molr/sdk/plugins';
import { pluginArchive } from '../../../scripts/plugin-archive.mjs';
const port = process.env.MOLR_TEST_PORT ?? '5193', origin = `http://127.0.0.1:${port}`;
async function post(page: Page, path: string, data: object, status = 200) {
 const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
 const r = await page.request.post(path, { headers: { Origin: origin, 'X-CSRF-Token': csrf }, data }); expect(r.status(), await r.text()).toBe(status); return r.json();
}
async function login(page: Page) {
 await page.goto('/#/extensions'); await page.getByLabel('Username', { exact: true }).fill('owner'); await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!'); await page.getByRole('button', { name: 'Sign in', exact: true }).click(); await expect(page.getByRole('button', { name: 'Import plugin package' })).toBeVisible();
}
test('ZIP skill installs a downloaded CLI in isolated assistant workspaces, persists status and cleans after removal', async ({ page }, info) => {
 test.skip(process.env.MOLR_E2E_SANDBOX_SERVICE !== '1', 'requires delegated sandbox'); test.setTimeout(120000); await login(page);
 const sandbox = await (await page.request.get('/api/sandbox')).json(); expect(sandbox.Capabilities.Reason).toBe('available');
 const id = `fixture.cli-${info.project.name}`, modelName = `Plugin workspace ${id}`, endpoint = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
 const mutate = (path: string, data: object) => post(page, path, { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } });
 const provider = await mutate('/api/resources', { Name: modelName, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: endpoint, AllowedOrigins: [endpoint], Models: [{ Name: modelName, ContextLimit: 65536, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } }] } });
 const models = await (await page.request.get('/api/models')).json(), model = models.find((m: { ResourceID: string }) => m.ResourceID === provider.ID);
 const assistants: { ID: string; Name: string; ConversationID: string }[] = [];
 for (const suffix of ['A', 'B']) assistants.push(await mutate('/api/assistants', { Name: `CLI ${suffix} ${info.project.name}`, ModelID: model.ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } }));
 const resources = (await (await page.request.get('/api/resources')).json()).Items;
 const compute = resources.find((r: { Kind: string }) => r.Kind === 'compute') ?? await mutate('/api/sandboxes/resource', { SandboxID: 'local', Kind: 'compute', SourceID: 'compute', Name: 'CLI compute' });
 const network = await mutate('/api/sandboxes/resource', { SandboxID: 'local', Kind: 'network', SourceID: 'preset:host', Name: `CLI download ${info.project.name}` });
 const pkg: PluginPackage = { manifest: { manifestVersion: 1, pluginApi: 3, id, name: `Workspace CLI ${info.project.name}`, description: 'Download and install a fixture CLI into each assistant workspace.', version: '1.0.0', requirements: [{ phase: 'install', kind: 'network', description: 'Download the CLI fixture', destinations: ['host.molr.internal'] }], credentials: [{ id: 'token', title: 'CLI token' }], contributes: { themes: [], pages: [], tools: [], skills: [{ id: 'cli', name: 'Workspace CLI', description: 'Install and use the CLI', entry: 'SKILL.md', tools: [] }] } }, files: { 'SKILL.md': 'Read PACKAGE.md, use permission_list, and run prepare.sh through ordinary shell_execute. CLI and dependencies belong in the assistant workspace.', 'scripts/install.sh': `set -eu\nmkdir -p bin\nwget -qO bin/cli http://host.molr.internal:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}/plugin-cli.sh\nchmod 700 bin/cli\n`, 'scripts/check.sh': './bin/cli --version\n' } };
 const imported = page.waitForResponse(r => r.url().endsWith('/api/plugins/install'));
 await page.locator('input[type=file]').setInputFiles({ name: 'cli.zip', mimeType: 'application/zip', buffer: pluginArchive(pkg) }); expect((await imported).status()).toBe(200);
 let installed = (await (await page.request.get('/api/plugins')).json()).find((p: { ID: string }) => p.ID === id);
 installed = await post(page, '/api/plugins/configure', { ID: id, ExpectedVersion: installed.Version, Enabled: true, Grants: [], Bindings: [] });
 for (const assistant of assistants) await post(page, '/api/plugins/instances/save', { ID: id, AssistantID: assistant.ID, Enabled: true });
 const caps = await (await page.request.get(`/api/assistants/${assistants[0].ID}/capabilities`)).json();
 expect(caps.Skills.Items.some((s: { ID: string; Enabled: boolean }) => s.ID.startsWith(`/plugins/${id}/`) && s.Enabled)).toBe(true);
 expect(caps.Tools.filter((t: { Name: string; Available: boolean }) => t.Name.startsWith('plg_') && t.Available)).toHaveLength(0);
 for (const assistant of assistants) {
  await mutate('/api/grants', { AssistantID: assistant.ID, ResourceID: compute.ID, Actions: ['execute'], Scope: {}, Constraints: { compute: { cpu_percent: 100, memory_bytes: 536870912, processes: 64, max_duration_seconds: 60, unlimited_duration: false, temp_bytes: 67108864 } } });
  await mutate('/api/grants', { AssistantID: assistant.ID, ResourceID: network.ID, Actions: ['execute'], Scope: {}, Constraints: { network: { bytes: 10485760, connections: 8, period: 'day', timezone: 'Asia/Shanghai' } } });
 }
 await page.goto(`/#/assistants/${assistants[0].ID}`); await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).fill('Install the CLI into your workspace.'); await page.getByRole('button', { name: 'Send', exact: true }).click();
 await expect(page.locator('.chat-history')).toContainText('Workspace CLI installed and checked.', { timeout: 60000 });
 const first = await post(page, '/api/plugins/instances/read', { ID: id, AssistantID: assistants[0].ID }); expect(first.Environment).toBe('ready');
 const second = await post(page, '/api/plugins/instances/read', { ID: id, AssistantID: assistants[1].ID }); expect(second.Environment).toBe('pending');
 await page.goto('/#/extensions'); await page.reload(); const row = page.locator('.plugin-card').filter({ has: page.getByRole('heading', { name: pkg.manifest.name, exact: true }) });
 await row.getByRole('button', { name: `More · ${pkg.manifest.name}` }).click(); await page.getByRole('menuitem', { name: 'Assistant environments' }).click();
 const dialog = page.getByRole('dialog', { name: 'Assistant environments' }); await dialog.getByRole('combobox', { name: 'Assistant', exact: true }).click(); await page.getByRole('option', { name: assistants[0].Name, exact: true }).click();
 await expect(dialog.getByText('Ready', { exact: true })).toBeVisible(); await expect(dialog.getByLabel('CLI token')).toHaveValue('');
 await page.screenshot({ path: info.outputPath('plugin-workspace.png') });
 await post(page, '/api/plugins/remove', { ID: id, ExpectedVersion: installed.Version });
 const retained = (await (await page.request.get('/api/plugins/instances')).json()).filter((r: { PluginID: string }) => r.PluginID === id); expect(retained).toHaveLength(2); expect(retained.every((r: { Enabled: boolean }) => !r.Enabled)).toBe(true);
 const clean = retained.find((r: { AssistantID: string }) => r.AssistantID === assistants[0].ID);
 await post(page, '/api/plugins/instances/clean', { ID: id, AssistantID: clean.AssistantID, ExpectedVersion: clean.Version });
 const after = (await (await page.request.get('/api/plugins/instances')).json()).filter((r: { PluginID: string }) => r.PluginID === id); expect(after).toHaveLength(1); expect(after[0].AssistantID).toBe(assistants[1].ID);
});
