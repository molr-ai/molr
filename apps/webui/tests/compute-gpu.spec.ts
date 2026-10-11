import { test, expect, type Page } from '@playwright/test';

const port = process.env.MOLR_TEST_PORT ?? '5193';
const base = `http://127.0.0.1:${port}`;
test.skip(!process.env.MOLR_E2E_GPU_DEVICES || process.env.MOLR_E2E_SANDBOX_SERVICE !== '1', 'Requires explicitly configured NVIDIA GPUs and delegated services');
async function post(page: Page, path: string, data: object, origin = base) {
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${new URL(origin).port}`)!.value;
  const response = await page.request.post(origin + path, { headers: { Origin: origin, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } } });
  expect(response.status(), await response.text()).toBe(200);
  return response.json();
}

test('GPU selection belongs to the compute grant and survives editing temporary access', async ({ page }, info) => {
  test.setTimeout(90000);
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  await expect.poll(async()=>{const nodes=await (await page.request.get('/api/sandboxes')).json();return nodes.find((n:any)=>n.ID==='local').Info.GPUs?.length??0;},{timeout:45000}).toBeGreaterThan(0);
  const nodes = await (await page.request.get('/api/sandboxes')).json();
  const gpu = nodes.find((n: any) => n.ID === 'local').Info.GPUs[0];
  expect(gpu.Available).toBe(true);
  const name = `GPU permissions ${info.project.name} ${Date.now()}`;
  const origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const provider = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 65536, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  const assistant = await post(page, '/api/assistants', { Name: name, ModelID: models.find((m: any) => m.ResourceID === provider.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  const resources = await (await page.request.get('/api/resources')).json();
  const compute = (resources.Items ?? resources).find((r: any) => r.Kind === 'compute');
  const limits = { cpu_percent: 100, memory_bytes: 1073741824, processes: 128, temp_bytes: 67108864, max_duration_seconds: 120, unlimited_duration: false };
  const grant = await post(page, '/api/grants', { AssistantID: assistant.ID, ResourceID: compute.ID, Actions: ['execute'], Scope: {}, Constraints: { compute: limits }, Temporary: { limits: { compute: { ...limits, gpu: { device_ids: [gpu.ID] } } }, until: new Date(Date.now() + 1800000).toISOString() } });
  await page.goto(`/#/assistants/${assistant.ID}`);
  await page.locator('.chat-header').getByRole('button', { name: 'More', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Assistant information', exact: true }).click();
  const panel = page.getByRole('dialog', { name: 'Assistant information', exact: true });
  await panel.getByRole('tab', { name: 'Permissions', exact: true }).click();
  const row = panel.locator('.card').filter({ has: page.getByRole('button', { name: 'Local sandbox', exact: true }) });
  await row.getByRole('button', { name: 'Edit', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Edit permission', exact: true });
  const selectors = dialog.getByRole('combobox', { name: 'GPU devices', exact: true });
  await expect(selectors).toHaveCount(2);
  await expect(selectors.last()).toContainText(gpu.Name);
  await selectors.first().click();
  await page.getByRole('option', { name: new RegExp(gpu.Name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')) }).click();
  await page.keyboard.press('Escape');
  const saved = page.waitForResponse(r => r.url().endsWith('/api/grants/update') && r.request().method() === 'POST');
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  const response = await saved;
  expect(response.status(), await response.text()).toBe(200);
  const updated = await response.json();
  expect(updated.ID).toBe(grant.ID);
  expect(updated.Constraints.compute.gpu.device_ids).toEqual([gpu.ID]);
  expect(updated.Temporary.limits.compute.gpu.device_ids).toEqual([gpu.ID]);
  await expect(dialog).not.toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

for (const supported of [true, false]) test(`authorization card ${supported ? 'lists named GPUs separately' : 'hides GPU information for a CPU sandbox'}`, async ({ page }, info) => {
  test.setTimeout(90000);
  const origin = supported ? base : `http://127.0.0.1:${Number(port) + 1}`;
  await page.goto(origin);
  await page.getByLabel('Username', { exact: true }).fill(supported ? 'owner' : 'second-owner');
  await page.getByLabel('Password', { exact: true }).fill(supported ? 'webui-e2e-password-2026!' : 'second-webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const nodes = await (await page.request.get(origin + '/api/sandboxes')).json();
  const devices = nodes.find((node: any) => node.ID === 'local').Info.GPUs ?? [];
  expect(devices.length > 0).toBe(supported);
  const name = `Permission ${supported ? 'gpu' : 'cpu'} display ${info.project.name} ${Date.now()}`;
  const endpoint = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const provider = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: endpoint, AllowedOrigins: [endpoint], Models: [{ Name: name, ContextLimit: 65536, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } }] } }, origin);
  const models = await (await page.request.get(origin + '/api/models')).json();
  const assistant = await post(page, '/api/assistants', { Name: name, ModelID: models.find((model: any) => model.ResourceID === provider.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } }, origin);
  await page.goto(`${origin}/#/assistants/${assistant.ID}`);
  await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).fill('Request compute permission for this test.');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  const card = page.locator('.permission-request-card');
  await expect(card).toBeVisible({ timeout: 60000 });
  await expect(card.locator('.permission-quota-grid')).toContainText('CPU');
  const tempRow = card.locator('.permission-quota-grid > div').filter({ has: page.locator('dt', { hasText: '/tmp' }) });
  await expect(tempRow).toHaveCSS('grid-column', 'auto');
  await expect(tempRow).toHaveCSS('border-top-width', '0px');
  const gpuSection = card.getByRole('group', { name: 'GPU access', exact: true });
  if (supported) {
    await expect(gpuSection).toContainText(devices[0].Name);
    await expect(gpuSection).toContainText('GiB');
    await expect(card).not.toContainText(devices[0].ID);
    await expect(gpuSection).toHaveCSS('grid-column', '1 / -1');
    await expect(gpuSection).toHaveCSS('border-top-style', 'solid');
    await expect(gpuSection).toHaveCSS('border-top-width', '1px');
    expect(await card.evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
  } else {
    await expect(gpuSection).toHaveCount(0);
    await expect(card).not.toContainText('GPU');
  }
  await card.getByRole('button', { name: 'Review permission', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Review permission', exact: true });
  await expect(dialog.getByRole('combobox', { name: 'GPU devices', exact: true })).toHaveCount(supported ? 1 : 0);
  if (!supported) await expect(dialog).not.toContainText('GPU');
  await dialog.getByRole('button', { name: 'Approve permission', exact: true }).click();
  await expect(card).toContainText('Approved');
  if (supported) await expect(gpuSection).toContainText(devices[0].Name);
  else await expect(card).not.toContainText('GPU');
  await page.screenshot({ path: info.outputPath(`gpu-card-${supported ? 'supported' : 'unsupported'}.png`) });
});
