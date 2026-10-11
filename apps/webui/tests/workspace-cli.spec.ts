import { test, expect, type Page } from '@playwright/test';
const port = process.env.MOLR_TEST_PORT ?? '5193', origin = `http://127.0.0.1:${port}`;
async function post(page: Page, path: string, data: object, status = 200) {
 const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
 const r = await page.request.post(path, { headers: { Origin: origin, 'X-CSRF-Token': csrf }, data }); expect(r.status(), await r.text()).toBe(status); return r.json();
}
async function login(page: Page) {
 await page.goto('/#/extensions'); await page.getByLabel('Username', { exact: true }).fill('owner'); await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!'); await page.getByRole('button', { name: 'Sign in', exact: true }).click(); await expect(page.getByRole('button', { name: 'Install plugin' })).toBeVisible();
}
test('assistant installs CLI and Skill with ordinary tools, persists files and isolates workspaces', async ({ page }, info) => {
 test.skip(process.env.MOLR_E2E_SANDBOX_SERVICE !== '1', 'requires delegated sandbox'); test.setTimeout(120000); await login(page);
 const sandbox = await (await page.request.get('/api/sandbox')).json(); expect(sandbox.Capabilities.Reason).toBe('available');
 const id = `fixture.cli-${info.project.name}`, modelName = `Workspace CLI ${id}`, endpoint = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
 const mutate = (path: string, data: object) => post(page, path, { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } });
 const provider = await mutate('/api/resources', { Name: modelName, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: endpoint, AllowedOrigins: [endpoint], Models: [{ Name: modelName, ContextLimit: 65536, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } }] } });
 const models = await (await page.request.get('/api/models')).json(), model = models.find((m: { ResourceID: string }) => m.ResourceID === provider.ID);
 const assistants: { ID: string; Name: string; ConversationID: string }[] = [];
 for (const suffix of ['A', 'B']) assistants.push(await mutate('/api/assistants', { Name: `CLI ${suffix} ${info.project.name}`, ModelID: model.ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } }));
 const resources = (await (await page.request.get('/api/resources')).json()).Items;
 const compute = resources.find((r: { Kind: string }) => r.Kind === 'compute') ?? await mutate('/api/sandboxes/resource', { SandboxID: 'local', Kind: 'compute', SourceID: 'compute', Name: 'CLI compute' });
 const network = await mutate('/api/sandboxes/resource', { SandboxID: 'local', Kind: 'network', SourceID: 'preset:host', Name: `CLI download ${info.project.name}` });
 const before = (await (await page.request.get('/api/plugins')).json()).map((p: { ID: string }) => p.ID);
 for (const assistant of assistants) {
  await mutate('/api/grants', { AssistantID: assistant.ID, ResourceID: compute.ID, Actions: ['execute'], Scope: {}, Constraints: { compute: { cpu_percent: 100, memory_bytes: 536870912, processes: 64, max_duration_seconds: 60, unlimited_duration: false, temp_bytes: 67108864 } } });
  await mutate('/api/grants', { AssistantID: assistant.ID, ResourceID: network.ID, Actions: ['execute'], Scope: {}, Constraints: { network: { bytes: 10485760, connections: 8, period: 'day', timezone: 'Asia/Shanghai' } } });
 }
 await page.goto(`/#/assistants/${assistants[0].ID}`); await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).fill('Install the CLI into your workspace.'); await page.getByRole('button', { name: 'Send', exact: true }).click();
 await expect(page.locator('.chat-history')).toContainText('Workspace CLI installed and checked.', { timeout: 60000 });
 const caps = await (await page.request.get(`/api/assistants/${assistants[0].ID}/capabilities`)).json();
 expect(caps.Skills.Items.some((s: { ID: string; Enabled: boolean }) => s.ID === '/work/.agents/skills/workspace-cli/SKILL.md' && s.Enabled)).toBe(true);
 const other = await (await page.request.get(`/api/assistants/${assistants[1].ID}/capabilities`)).json();
 expect(other.Skills.Items.some((s: { ID: string }) => s.ID.includes('workspace-cli'))).toBe(false);
 await page.reload();
 await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).fill('Check the installed CLI without downloading it again.'); await page.getByRole('button', { name: 'Send', exact: true }).click();
 await expect(page.locator('.chat-history')).toContainText('Workspace CLI persisted and checked.', { timeout: 60000 });
 await page.goto(`/#/assistants/${assistants[1].ID}`);
 await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).fill('Check workspace isolation.'); await page.getByRole('button', { name: 'Send', exact: true }).click();
 await expect(page.locator('.chat-history')).toContainText('Workspace is isolated.', { timeout: 60000 });
 expect((await (await page.request.get('/api/plugins')).json()).map((p: { ID: string }) => p.ID)).toEqual(before);
 await page.goto('/#/extensions');
 await expect(page.getByRole('button', { name: 'Assistant installation records' })).toHaveCount(0);
 await page.screenshot({ path: info.outputPath('workspace-cli.png') });
});
