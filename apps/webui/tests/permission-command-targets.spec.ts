import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import type { PermissionRequest } from '../src/api/types';

const port = process.env.MOLR_TEST_PORT ?? '5193', base = `http://127.0.0.1:${port}`;
async function post(page: Page, path: string, data: object) {
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } } });
  expect(response.status(), await response.text()).toBe(200); return response.json();
}
async function fixture(page: Page) {
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const name = `Permission cpu targets ${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 65536, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  const assistant = await post(page, '/api/assistants', { Name: name, ModelID: models.find((m: any) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  await page.goto(`/#/assistants/${assistant.ID}`);
  const input = page.getByRole('textbox', { name: 'Message your assistant', exact: true }), send = page.getByRole('button', { name: 'Send', exact: true });
  await input.fill('Please request compute permission.'); await send.click();
  await expect(page.locator('.permission-request-card')).toBeVisible();
  const path = `/api/permission-requests?assistant_id=${assistant.ID}`;
  const [proposal]: PermissionRequest[] = await (await page.request.get(path)).json();
  await expect(page.locator('.permission-request-card code')).toHaveText(proposal.ID);
  await expect(page.getByRole('button', { name: 'Copy request ID', exact: true })).toBeVisible();
  // Model/Runtime creation is real; controlled list/resolve responses exercise
  // concurrent arrivals and partial failures without timing-dependent scheduling.
  let rows: PermissionRequest[] = [], calls: any[] = [], failAt = 0, arrive = false;
  const proposals = () => ['newest', 'middle', 'oldest'].map((id, index) => ({ ...proposal, ID: id, Version: index + 1, CreatedAt: new Date(Date.now() - index * 1000).toISOString() }));
  await page.route(`**${path}`, route => route.fulfill({ json: rows }));
  await page.route('**/api/permission-requests/resolve', async route => {
    const body = route.request().postDataJSON(); calls.push(body);
    if (calls.length === failAt) { await route.fulfill({ status: 409, json: { code: 'conflict', message: 'Request changed.' } }); return; }
    const row = rows.find(row => row.ID === body.RequestID)!;
    expect(body.Mutation.ExpectedVersion).toBe(row.Version);
    row.State = body.Decision === 'approve' ? 'approved' : 'rejected'; row.Version++;
    if (arrive && calls.length === 1) rows.unshift({ ...proposal, ID: 'arrived-after-submit' });
    await route.fulfill({ json: row });
  });
  async function reset(options: { failAt?: number; arrive?: boolean; incomplete?: boolean } = {}) {
    rows = proposals(); calls = []; failAt = options.failAt ?? 0; arrive = options.arrive ?? false;
    if (options.incomplete) rows[1] = { ...rows[1], Input: { ...rows[1].Input, constraints: undefined } };
    // A newer resolved record must not be selected by a default command.
    rows.unshift({ ...proposal, ID: 'resolved', State: 'approved' });
    await page.reload(); await input.fill('/'); await expect(page.getByRole('option')).toHaveCount(3);
  }
  async function command(value: string) { await input.fill(value); await send.click(); }
  return { reset, command, input, calls: () => calls, rows: () => rows };
}

test('default selects newest pending; exact IDs select older requests and never fall back', async ({ page }) => {
  const f = await fixture(page); await f.reset();
  await f.command('/permission approve');
  await expect(f.input).toHaveValue(''); expect(f.calls().map(call => call.RequestID)).toEqual(['newest']);
  await f.command('/权限审批 拒绝 oldest');
  await expect(f.input).toHaveValue(''); expect(f.calls().map(call => call.RequestID)).toEqual(['newest', 'oldest']);
  for (const id of ['old', 'unknown-or-other-assistant', 'resolved', 'newest']) {
    await f.command(`/permission approve ${id}`);
    await expect(page.locator('.composer-notice')).toContainText('No pending request with this ID');
    expect(f.calls()).toHaveLength(2);
  }
  await f.command('/permission approve middle extra');
  await expect(page.locator('.composer-notice')).toContainText('Usage:'); expect(f.calls()).toHaveLength(2);
  await f.command('/permission reject');
  await expect(f.input).toHaveValue(''); expect(f.calls().at(-1).RequestID).toBe('middle');
  await f.command('/permission approve all');
  await expect(page.locator('.composer-notice')).toContainText('There is no pending permission request.');
  await expect(page.locator('.ui-chat-message--user')).toHaveCount(1);
});

test('all and 全部 resolve a fixed batch with either decision, excluding new arrivals', async ({ page }) => {
  const f = await fixture(page);
  for (const command of ['/permission approve all', '/权限审批 同意 全部', '/permission reject all', '/权限审批 拒绝 全部']) {
    await f.reset({ arrive: true }); await f.command(command);
    await expect(f.input).toHaveValue('');
    await expect(page.locator('.composer-notice')).toContainText('3 permission request(s)');
    expect(f.calls().map(call => call.RequestID)).toEqual(['newest', 'middle', 'oldest']);
    const approve = command.includes('approve') || command.includes('同意');
    for (const call of f.calls()) {
      expect(call.Decision).toBe(approve ? 'approve' : 'reject');
      if (approve) expect(call.Constraints.compute.memory_bytes).toBe(536870912);
      else expect(call).not.toHaveProperty('ResourceID');
    }
    expect(f.rows().find(row => row.ID === 'arrived-after-submit')?.State).toBe('pending');
  }
});

test('batch preflight prevents incomplete grants; later failure reports partial progress and stops', async ({ page }) => {
  const f = await fixture(page); await f.reset({ incomplete: true });
  await f.command('/permission approve all');
  await expect(page.locator('.composer-notice')).toContainText('This request needs a resource or quota.');
  expect(f.calls()).toHaveLength(0);
  await f.reset({ failAt: 2 }); await f.command('/permission approve all');
  await expect(page.locator('.composer-notice')).toContainText('Processed 1/3 requests, then stopped.');
  expect(f.calls().map(call => call.RequestID)).toEqual(['newest', 'middle']);
  expect(f.rows().find(row => row.ID === 'newest')?.State).toBe('approved');
  expect(f.rows().find(row => row.ID === 'oldest')?.State).toBe('pending');
  await expect(f.input).toHaveValue('/permission approve all');
});
