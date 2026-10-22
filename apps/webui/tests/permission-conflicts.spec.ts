import { test, expect } from '@playwright/test';

const port = process.env.MOLR_TEST_PORT ?? '5193', base = `http://127.0.0.1:${port}`;
test('permission conflicts explain duplicate grants and resolved requests', async ({ page }) => {
  test.setTimeout(120_000);
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill(process.env.MOLR_E2E_USERNAME ?? 'owner');
  await page.getByLabel('Password', { exact: true }).fill(process.env.MOLR_E2E_PASSWORD ?? 'webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  async function post(path: string, body: object) {
    const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
    const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { Mutation: { IdempotencyKey: crypto.randomUUID() }, ...body } });
    expect(response.status(), await response.text()).toBe(200); return response.json();
  }
  const name = `Permission duplicate ${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post('/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 65536, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  const assistant = await post('/api/assistants', { Name: name, ModelID: models.find((m: any) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  await page.goto(`/#/assistants/${assistant.ID}`);
  await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).fill('Please run the calculation.');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  const card = page.locator('.permission-request-card');
  await expect(card).toBeVisible({ timeout: 60_000 });
  const [request] = await (await page.request.get(`/api/permission-requests?assistant_id=${assistant.ID}`)).json();
  await card.getByRole('button', { name: 'Review permission', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Review permission', exact: true });
  const grant = await post('/api/grants', { AssistantID: assistant.ID, ResourceID: request.Input.resource_id, Actions: ['execute'], Scope: {}, Constraints: { compute: { cpu_percent: 100, memory_bytes: 536870912, processes: 64, temp_bytes: 67108864, max_duration_seconds: 300, unlimited_duration: false } } });
  await dialog.getByRole('button', { name: 'Approve permission', exact: true }).click();
  await expect(page.getByText('The assistant already has an active grant for this resource. Edit it in the assistant’s permissions.', { exact: true })).toBeVisible();
  const pending = await (await page.request.get(`/api/permission-requests/${request.ID}`)).json();
  expect(pending.State).toBe('pending');
  const grants = await (await page.request.get(`/api/grants?assistant_id=${assistant.ID}`)).json();
  expect(grants.Items.filter((g: any) => g.ResourceID === grant.ResourceID)).toHaveLength(1);
  await post('/api/permission-requests/resolve', { RequestID: request.ID, Decision: 'reject', Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: request.Version } });
  await dialog.getByRole('button', { name: 'Approve permission', exact: true }).click();
  await expect(page.getByText('This permission request has been resolved or changed. Close the dialog to check its latest state.', { exact: true })).toBeVisible();
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  const duplicateAssistant = await post('/api/assistants', { Name: 'Already authorized', ModelID: assistant.ModelID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  await post('/api/grants', { AssistantID: duplicateAssistant.ID, ResourceID: grant.ResourceID, Actions: ['execute'], Scope: {}, Constraints: grant.Constraints });
  await page.goto(`/#/assistants/${duplicateAssistant.ID}`);
  await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).fill('Please run the calculation.');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page.getByText('Existing permission covers this request. No authorization card is needed.', { exact: true })).toBeVisible({ timeout: 60_000 });
  await expect(page.locator('.permission-request-card')).toHaveCount(0);
  const requests = await (await page.request.get(`/api/permission-requests?assistant_id=${duplicateAssistant.ID}`)).json();
  expect(requests).toHaveLength(0);
});
