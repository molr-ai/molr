import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';

test.setTimeout(120_000);
const port = process.env.MOLR_TEST_PORT ?? '5193', base = `http://127.0.0.1:${port}`;
async function post(page: Page, path: string, data: object) {
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } } });
  expect(response.status(), await response.text()).toBe(200); return response.json();
}

for (const decision of ['approve', 'reject']) test(`permission request ${decision} persists and resumes with the actual decision`, async ({ page }, info) => {
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const name = `Permission ${decision} ${info.project.name} ${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 65536, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  const assistant = await post(page, '/api/assistants', { Name: name, ModelID: models.find((m: any) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  await page.goto(`/#/assistants/${assistant.ID}`);
  await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).fill('Please run the calculation.');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page.locator('.permission-request-card')).toBeVisible({ timeout: 60_000 });
  await expect.poll(async () => (await (await page.request.get(`/api/assistants/${assistant.ID}/status`)).json()).WaitingReason).toBe('permission_pending');
  await page.reload();
  await expect(page.locator('.permission-request-card')).toContainText('Run the calculation in an isolated sandbox.');
  await expect(page.locator('.ui-chat-message--assistant')).toHaveCount(1);
  await expect(page.locator('.chat-history')).not.toContainText('Must not run before approval');
  const pending = await (await page.request.get(`/api/permission-requests?assistant_id=${assistant.ID}`)).json();
  expect(pending[0].Input.resource_id).toBeTruthy(); expect(pending[0].Input.expires_at).toBeTruthy();
  const proposedExpiry = pending[0].Input.expires_at;
  // The permissions tab consumes the paginated resource catalog, including
  // requests that already exist when the panel first mounts.
  await page.locator('.chat-assistant-info').click();
  let permissionsPanel = page.locator('.assistant-info');
  await permissionsPanel.getByRole('tab', { name: 'Permissions', exact: true }).click();
  await expect(permissionsPanel.locator('.permission-requests')).toContainText('Permission requests');
  await expect(permissionsPanel.locator('.permission-requests')).toContainText('Run the calculation in an isolated sandbox.');
  await expect(page.getByText('This theme could not load', { exact: true })).toHaveCount(0);
  await permissionsPanel.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(permissionsPanel).toBeHidden();
  const grantsBefore = await (await page.request.get(`/api/grants?assistant_id=${assistant.ID}`)).json();
  expect(grantsBefore.Items).toHaveLength(1); // only the model chosen at hiring
  if (decision === 'approve') {
    const input = page.getByRole('textbox', { name: 'Message your assistant', exact: true });
    await input.fill('/权限审批 同意');
    await page.getByRole('button', { name: 'Send', exact: true }).click();
    await expect(page.locator('.composer-notice')).toContainText('This request needs a resource or quota.');
    await expect(page.locator('.permission-request-card')).toContainText('Awaiting permission');
    await input.fill('');
    await page.locator('.permission-request-card').getByRole('button', { name: 'Review permission', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Review permission', exact: true });
    await expect(dialog.getByRole('combobox', { name: 'Resource type', exact: true })).toContainText('Sandboxes');
    await expect(dialog.getByRole('combobox', { name: 'Sandbox', exact: true })).toContainText('Local sandbox');
    await expect(dialog.getByRole('combobox', { name: 'Permission type', exact: true })).toContainText('Compute');
    await expect(dialog.getByRole('combobox', { name: 'Allowed actions', exact: true })).toContainText('Run tasks');
    await expect(dialog.getByLabel('Memory limit (MiB)', { exact: true })).toHaveValue('512');
    await expect(dialog).not.toContainText('legacy deployment limits');
    await expect(dialog).not.toContainText('You may narrow');
    await dialog.getByRole('button', { name: '1 day', exact: true }).click();
    await dialog.getByRole('button', { name: 'Approve permission', exact: true }).click();
    await expect(dialog).toBeHidden();
  } else {
    await page.locator('.permission-request-card').getByRole('button', { name: 'Reject', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Reject permission request', exact: true });
    await dialog.getByRole('button', { name: 'Confirm', exact: true }).click();
    await expect(dialog).toBeHidden();
  }
  const requests = await (await page.request.get(`/api/permission-requests?assistant_id=${assistant.ID}`)).json();
  expect(requests).toHaveLength(1); expect(requests[0].State).toBe(decision === 'approve' ? 'approved' : 'rejected');
  if (decision === 'approve') expect(Date.parse(requests[0].Granted.ExpiresAt)).toBeGreaterThan(Date.parse(proposedExpiry));
  const grantsAfter = await (await page.request.get(`/api/grants?assistant_id=${assistant.ID}`)).json();
  expect(grantsAfter.Items).toHaveLength(decision === 'approve' ? 2 : 1);
  if (decision === 'approve') expect(grantsAfter.Items.find((grant: any) => grant.Constraints?.compute)?.Constraints.compute.memory_bytes).toBe(512 * 1048576);
  await page.goto(`/#/assistants/${assistant.ID}`); await page.reload();
  await expect(page.locator('.ui-chat-message--assistant').filter({hasText:decision === 'approve' ? 'Permission approved; current grants checked before continuing.' : 'Permission was declined. No command was executed.'})).toHaveCount(1, { timeout: 60_000 });
  await expect(page.locator('.permission-request-card')).toContainText(decision === 'approve' ? 'Approved' : 'Rejected');
  await expect(page.getByRole('button', { name: 'Review permission', exact: true })).toHaveCount(0);
  await page.locator('.chat-assistant-info').click();
  permissionsPanel = page.locator('.assistant-info');
  await permissionsPanel.getByRole('tab', { name: 'Permissions', exact: true }).click();
  await permissionsPanel.getByText(/Resolved permission requests/).click();
  await expect(permissionsPanel.locator('.permission-requests')).toContainText(decision === 'approve' ? 'Approved scope' : 'Rejected');
  await expect(permissionsPanel.getByRole('button', { name: 'Add permission', exact: true })).toBeEnabled();
  await expect(page.getByText('This theme could not load', { exact: true })).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('task, resource, schedule and permission cards share layout and display real records', async ({ page }, info) => {
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const name = `Permission unified ${info.project.name} ${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 65536, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  const assistant = await post(page, '/api/assistants', { Name: name, ModelID: models.find((m: any) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  const task = await post(page, '/api/tasks', { AssistantID: assistant.ID, Goal: 'Prepare an analysis report' });
  const schedule = await post(page, '/api/schedules', { Kind: 'assistant_plan', AssistantID: assistant.ID, Title: 'Review the report tomorrow', Instructions: 'Review the report', Trigger: { StartAt: new Date(Date.now() + 86400000).toISOString(), Timezone: 'Asia/Shanghai', Recurrence: '', MissedPolicy: 'skip' } });
  const cards = [
    { kind: 'task', target_id: task.ID, title: 'Analysis report', summary: 'Review the task and its deliverables.' },
    { kind: 'resource', target_id: resource.ID, title: 'Analysis model', summary: 'The model selected for this assistant.' },
    { kind: 'schedule', target_id: schedule.ID, title: 'Tomorrow’s review', summary: 'Review the analysis at the scheduled time.' },
  ];
  await post(page, '/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: JSON.stringify({ cards }) } });
  await page.goto(`/#/assistants/${assistant.ID}`);
  const all = page.locator('.ui-chat-card');
  await expect(all).toHaveCount(4, { timeout: 60_000 });
  const taskCard = page.getByRole('article', { name: 'Analysis report', exact: true });
  const resourceCard = page.getByRole('article', { name: 'Analysis model', exact: true });
  const scheduleCard = page.getByRole('article', { name: 'Tomorrow’s review', exact: true });
  await expect(taskCard.locator('dd').first()).toHaveText('0');
  await expect(resourceCard).toContainText(name);
  await expect(resourceCard).toContainText('Available');
  await expect(scheduleCard).toContainText('Asia/Shanghai');
  await expect(page.locator('.permission-request-card')).toContainText('Run tasks');
  const styles = await all.evaluateAll(elements => elements.map(el => {
    const style = getComputedStyle(el);
    return [style.maxWidth, style.padding, style.backgroundColor, style.borderRadius];
  }));
  expect(new Set(styles.map(style => JSON.stringify(style))).size).toBe(1);
  expect(styles[0][0]).toBe('480px');
  expect(await all.evaluateAll(elements => elements.every(el => el.scrollWidth <= el.clientWidth + 1 && el.getBoundingClientRect().right <= innerWidth))).toBe(true);
  await page.screenshot({ path: info.outputPath('unified-message-cards.png') });
  await taskCard.getByRole('button', { name: 'View task', exact: true }).click();
  const taskDetails = page.getByRole('dialog', { name: 'Plan details', exact: true });
  await expect(taskDetails.getByRole('region', { name: `${task.Goal} · Plan details`, exact: true })).toBeVisible();
  await expect(taskDetails.locator('.plan-execution[open] .plan-instructions')).toHaveText(task.Goal);
  await page.keyboard.press('Escape');
  await expect(taskDetails).toBeHidden();
  await resourceCard.getByRole('button', { name: 'View resource', exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/resources/${resource.ID}$`));
  await page.goto(`/#/assistants/${assistant.ID}`);
  await page.getByRole('button', { name: 'View schedule', exact: true }).click();
  await expect(page).toHaveURL(/\/plans/);
});
