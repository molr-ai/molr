import { test, expect, type Page } from '@playwright/test';
import type { DebugRun, SystemNotice } from '@molr/sdk';
const port = process.env.MOLR_TEST_PORT ?? '5193', base = `http://127.0.0.1:${port}`;
const actionLabel = 'View in developer workbench';

async function login(page: Page) {
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
}
async function post(page: Page, path: string, data: object, version?: number) {
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: path === '/api/notifications/update' ? data : { ...data, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: version } } });
  expect(response.ok(), await response.text()).toBe(true);
  return response.json();
}
async function settings(page: Page, patch: object) {
  const current = await (await page.request.get('/api/settings')).json();
  await post(page, '/api/settings', { Settings: { ...current, ...patch } }, current.Version);
}
async function failure(page: Page, suffix: string) {
  const name = `Execution notice ${suffix}-${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1000000 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  const assistant = await post(page, '/api/assistants', { Name: name, ModelID: models.find((m: { ResourceID: string }) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  const notices = async (): Promise<SystemNotice[]> => (await (await page.request.get(`/api/notifications?assistant_id=${assistant.ID}&filter=active`)).json()).Items;
  await expect.poll(async () => (await notices()).some(n => n.Kind === 'assistant.waiting')).toBe(true);
  await post(page, '/api/tasks', { AssistantID: assistant.ID, Goal: name });
  let notice: SystemNotice | undefined;
  await expect.poll(async () => { notice = (await notices()).find(n => n.Kind === 'execution.failed'); return !!notice; }).toBe(true);
  // Keep the failed execution at the front of the real notification queue.
  for (const n of await notices()) if (n.ID !== notice!.ID) await post(page, '/api/notifications/update', { IDs: [n.ID], Action: 'dismiss' });
  return { assistant, notice: notice! };
}

test('background execution notices locate real recorded events across pages in both themes', async ({ page }, info) => {
  await login(page);
  const original = await (await page.request.get('/api/settings')).json();
  await settings(page, { WorkbenchVisible: true });
  try {
    const { assistant, notice } = await failure(page, info.project.name);
    const runs: DebugRun[] = (await (await page.request.get(`/api/debug/runs?assistant_id=${assistant.ID}`)).json()).Items;
    const run = runs.find(r => r.EventID === notice.Source.ID)!;
    expect(run).toBeTruthy();
    expect(run.ExecutionID).not.toBe(notice.Source.ID);
    await page.route('**/api/notifications?*', async route => {
      const response = await route.fetch(), data = await response.json();
      await route.fulfill({ json: { ...data, Items: data.Items.map((n: SystemNotice) => n.ID === notice.ID ? { ...n, Code: 'needs_review:internal' } : n) } });
    });
    let olderRequests = 0;
    await page.route('**/api/debug/trajectory?*', async route => {
      const url = new URL(route.request().url()); url.searchParams.set('limit', '1');
      if (url.searchParams.has('cursor')) olderRequests++;
      await route.fulfill({ response: await route.fetch({ url: url.toString() }) });
    });
    for (const theme of ['molr.official@1.0.0', 'molr.test@1.0.0']) {
      await settings(page, { UITheme: theme });
      await page.goto(`/#/assistants/${assistant.ID}`); await page.reload();
      const banner = page.locator('.assistant-notice');
      await expect(banner).toContainText('Background execution failed');
      await expect(banner).toContainText('The outcome needs review.');
      await banner.getByRole('button', { name: actionLabel, exact: true }).click();
      await expect(page).toHaveURL(`${base}/#/workbench/${assistant.ID}?event=${notice.Source.ID}`);
      const inspector = page.getByRole('region', { name: 'Step details', exact: true });
      await expect(inspector).toContainText(notice.Source.ID);
      await expect(page.getByRole('switch', { name: 'Follow live execution', exact: true })).not.toBeChecked();
      await page.reload(); await expect(inspector).toContainText(notice.Source.ID);
    }
    expect(olderRequests).toBeGreaterThan(0);
    const stored = await (await page.request.get(`/api/notifications/${notice.ID}`)).json();
    expect(stored.ReadAt).toBeTruthy(); expect(stored.DismissedAt).toBeNull();
  } finally { await page.unrouteAll({ behavior: 'wait' }); await settings(page, { WorkbenchVisible: original.WorkbenchVisible, UITheme: original.UITheme }); }
});

test('execution action follows visibility and exposes previously hidden recordings', async ({ page }, info) => {
  await login(page);
  const original = await (await page.request.get('/api/settings')).json();
  await settings(page, { WorkbenchVisible: false });
  try {
    const { assistant, notice } = await failure(page, info.project.name);
    await page.goto(`/#/assistants/${assistant.ID}`); await page.reload();
    const banner = page.locator('.assistant-notice');
    await expect(banner).toContainText('Background execution failed');
    await expect(banner.getByRole('button', { name: actionLabel })).toHaveCount(0);
    await expect(banner.getByRole('button', { name: 'View details', exact: true })).toHaveCount(0);
    await expect(banner.getByRole('button', { name: 'Close', exact: true })).toBeVisible();
    await settings(page, { WorkbenchVisible: true }); await page.reload();
    await banner.getByRole('button', { name: actionLabel }).click();
    await expect(page.getByRole('region', { name: 'Step details', exact: true })).toContainText(notice.Source.ID);
    await page.goto(`/#/workbench/${assistant.ID}?event=missing-event`);
    await expect(page.getByText('No workbench recording is available for this turn.', { exact: false })).toBeVisible();
    await page.route('**/api/status', async route => {
      const response = await route.fetch(); await route.fulfill({ json: { ...await response.json(), workbench_available: false } });
    });
    await page.goto(`/#/assistants/${assistant.ID}`); await page.reload();
    await expect(banner).toContainText('Background execution failed');
    await expect(banner.getByRole('button', { name: actionLabel })).toHaveCount(0);
  } finally { await page.unrouteAll({ behavior: 'wait' }); await settings(page, { WorkbenchVisible: original.WorkbenchVisible }); }
});

test('execution toast actions follow workbench settings and navigate to their event', async ({ page }, info) => {
  await login(page);
  const original = await (await page.request.get('/api/settings')).json();
  const principal = await (await page.request.get('/api/auth/me')).json();
  await page.evaluate(owner => localStorage.setItem(`molr-notifications:${owner}`, JSON.stringify({ enabled: true, preview: true, system: false })), principal.OwnerID);
  await settings(page, { WorkbenchVisible: true });
  await page.goto('/#/plans'); await page.reload();
  try {
    const { assistant, notice } = await failure(page, info.project.name);
    const toast = page.locator('.ui-toast').filter({ hasText: 'Background execution failed' });
    await expect(toast.getByRole('button', { name: actionLabel })).toBeVisible();
    await toast.hover();
    // Browser API mutations alone do not invalidate the in-app settings cache.
    const setInApp = async (enabled: boolean) => page.evaluate(async enabled => {
      const modulePath = '/src/api/client.ts'; const { api } = await import(modulePath);
      const current = await api.get('/api/settings');
      await api.mutate('/api/settings', { Settings: { ...current, WorkbenchVisible: enabled } }, current.Version);
    }, enabled);
    await setInApp(false);
    await expect(toast.getByRole('button', { name: actionLabel })).toHaveCount(0);
    await expect(toast.getByRole('button', { name: 'Close', exact: true })).toBeVisible();
    await setInApp(true);
    await toast.getByRole('button', { name: actionLabel }).click();
    await expect(page).toHaveURL(`${base}/#/workbench/${assistant.ID}?event=${notice.Source.ID}`);
    await expect(page.getByRole('region', { name: 'Step details', exact: true })).toContainText(notice.Source.ID);
    const stored = await (await page.request.get(`/api/notifications/${notice.ID}`)).json();
    expect(stored.ReadAt).toBeTruthy(); expect(stored.DismissedAt).toBeNull();
  } finally { await settings(page, { WorkbenchVisible: original.WorkbenchVisible }); await page.evaluate(owner => localStorage.removeItem(`molr-notifications:${owner}`), principal.OwnerID); }
});
