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

test('background execution notices locate real recorded events across pages', async ({ page }, info) => {
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
    await settings(page, { UITheme: 'molr.official@1.0.0' });
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
      const modulePath = '/@id/@molr/sdk'; const { api } = await import(modulePath);
      const current = await api.get('/api/settings');
      await api.mutate('/api/settings', { Settings: { ...current, WorkbenchVisible: enabled } }, { idempotencyKey: crypto.randomUUID(), expectedVersion: current.Version });
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

test('dream timeout explains retained changes and opens the right assistant settings without workbench', async ({ page }, info) => {
  await login(page);
  const original = await (await page.request.get('/api/settings')).json();
  await settings(page, { WorkbenchVisible: false });
  try {
    const { assistant, notice } = await failure(page, `dream-${info.project.name}`);
    // Scheduler integration tests cover the persisted kind. This fixture isolates
    // rendering and navigation from model timing and the session's clock.
    await page.route('**/api/notifications?*', async route => route.fulfill({ json: { Items: [{ ...notice, Kind: 'dream.failed', Code: 'needs_review:deadline_exceeded' }], NextCursor: '', UnreadCount: 1, PendingCount: 1 } }));
    for (const [language, title, retained, action, duration] of [
      ['en', 'Dream consolidation timed out', 'Successfully saved memory changes are retained.', 'View dream settings', 'Maximum dream duration (seconds)'],
      ['zh', '做梦整理超时', '已成功写入的记忆会保留。', '查看做梦设置', '做梦时长上限（秒）'],
    ]) {
      await settings(page, { Language: language });
      await page.goto(`/#/assistants/${assistant.ID}`); await page.reload();
      const banner = page.locator('.assistant-notice');
      await expect(banner).toContainText(title); await expect(banner).toContainText(retained);
      await banner.getByRole('button', { name: action, exact: true }).click();
      await expect(page).toHaveURL(`${base}/#/assistants/${assistant.ID}?panel=info`);
      await expect(page.getByRole('switch', { name: language === 'zh' ? '开启做梦' : 'Enable dreaming' })).toBeChecked();
      await expect(page.getByRole('spinbutton', { name: duration })).toHaveValue('3600');
      await page.getByRole('button', { name: language === 'zh' ? '取消' : 'Cancel', exact: true }).click();
      await expect(page.locator('.assistant-info')).toHaveCount(0);
      await expect(page).toHaveURL(`${base}/#/assistants/${assistant.ID}`);
      for (const close of ['header', 'save']) {
        await banner.getByRole('button', { name: action, exact: true }).click();
        const info = page.locator('.assistant-info');
        await expect(info.getByRole('spinbutton', { name: duration })).toHaveValue('3600');
        if (close === 'header') await info.locator('.ui-dialog__header').getByRole('button', { name: language === 'zh' ? '关闭' : 'Close', exact: true }).click();
        else await info.getByRole('button', { name: language === 'zh' ? '保存' : 'Save', exact: true }).click();
        await expect(info).toHaveCount(0);
        await expect(page).toHaveURL(`${base}/#/assistants/${assistant.ID}`);
      }
    }
    await settings(page, { Language: 'en' });
    await page.goto(`/#/assistants/${assistant.ID}?panel=info`); await page.reload();
    const dreamSettings = page.getByRole('region', { name: 'Dream settings', exact: true });
    await expect(dreamSettings).toBeVisible();
    await expect(page.getByRole('button', { name: 'Dream settings', exact: true })).toHaveCount(0);
    await expect(page.getByRole('textbox', { name: 'Timezone', exact: true })).toHaveCount(0);
    await expect(page).toHaveURL(`${base}/#/assistants/${assistant.ID}?panel=info`);
    const form = page.locator('.assistant-info form'), footer = page.locator('.assistant-info > .ui-dialog__footer');
    await expect(form).toHaveCount(1);
    await expect(footer.getByRole('button', { name: 'Save', exact: true })).toHaveCount(1);
    await expect(footer.getByRole('button', { name: 'Cancel', exact: true })).toHaveCount(1);
    await expect(form.locator('.assistant-settings-section')).toHaveCount(4);
    await expect(page.locator('.assistant-info .assistant-profile-status, .assistant-info .assistant-info-status')).toHaveCount(0);
    const save = footer.getByRole('button', { name: 'Save', exact: true });
    expect(await save.getAttribute('form')).toBe(await form.getAttribute('id'));
    await expect(save).toBeInViewport();
    const footerY = (await footer.boundingBox())!.y;
    await page.locator('.assistant-info > .ui-dialog__body').evaluate(el => { el.scrollTop = el.scrollHeight; });
    await expect(save).toBeInViewport();
    expect((await footer.boundingBox())!.y).toBeCloseTo(footerY, 0);
    await page.getByRole('tab', { name: 'Tools', exact: true }).click();
    await expect(footer).toHaveCount(0);
    await page.getByRole('tab', { name: 'Overview', exact: true }).click();
    await expect(save).toBeInViewport();
    for (const text of ['Saved spending limit', 'Spent this period', 'Reserved spend', 'Next reset', 'Uses the time zone from General settings:']) await expect(form).not.toContainText(text);
    const input = form.getByRole('spinbutton', { name: 'Maximum dream duration (seconds)' });
    const enabled = form.getByRole('switch', { name: 'Enable dreaming' });
    await expect(input).toHaveValue('3600');
    const timing = form.locator('.dream-timing-fields');
    const layout = await timing.evaluate(el => {
      const time = el.querySelector('fieldset')!, duration = el.querySelector('label')!;
      return { time: time.getBoundingClientRect().toJSON(), duration: duration.getBoundingClientRect().toJSON(), button: time.querySelector('button')!.getBoundingClientRect().toJSON(), weight: getComputedStyle(time.querySelector('legend')!).fontWeight };
    });
    expect(layout.time.right).toBeLessThanOrEqual(layout.duration.x);
    expect(Math.abs(layout.time.y - layout.duration.y)).toBeLessThan(2);
    expect(Number(layout.weight)).toBeGreaterThanOrEqual(600);
    expect(layout.time.width).toBeLessThanOrEqual(112);
    expect(layout.button.width).toBeCloseTo(layout.time.width, 0);
    await enabled.uncheck();
    await expect(input).toHaveCount(0);
    await expect(form.getByText('Start time', { exact: true })).toHaveCount(0);
    await enabled.check(); await input.fill('1200');
    const changedName = assistant.Name + ' edited';
    await form.getByRole('textbox', { name: 'Assistant name', exact: true }).fill(changedName);
    let failDreamSave = true, assistantWrites = 0;
    page.on('request', request => { if (request.method() === 'POST' && request.url().endsWith('/api/assistants/update')) assistantWrites++; });
    await page.route('**/api/dreams/config', async route => {
      if (route.request().method() === 'POST' && failDreamSave) { failDreamSave = false; await route.fulfill({ status: 503, json: { error: { code: 'unavailable', message: 'Fixture failure' } } }); }
      else await route.continue();
    });
    await footer.getByRole('button', { name: 'Save', exact: true }).click();
    await expect.poll(async () => (await (await page.request.get(`/api/assistants/${assistant.ID}`)).json()).Name).toBe(changedName);
    await expect(form).toBeVisible(); await expect(input).toHaveValue('1200');
    await page.locator('.ui-toast--danger').getByRole('button', { name: 'Close', exact: true }).click();
    await footer.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(form).toHaveCount(0);
    expect(assistantWrites).toBe(1);
    await expect.poll(async () => (await (await page.request.get(`/api/dreams/config?assistant_id=${assistant.ID}`)).json()).MaxDurationSeconds).toBe(1200);
    await page.locator('.chat-assistant-info').click();
    await enabled.uncheck();
    await footer.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(form).toHaveCount(0);
    expect((await (await page.request.get(`/api/dreams/config?assistant_id=${assistant.ID}`)).json()).Enabled).toBe(false);
    await page.locator('.chat-assistant-info').click();
    await expect(input).toHaveCount(0);
    await enabled.check(); await expect(input).toHaveValue('1200');
    const other = await post(page, '/api/assistants', { Name: 'Independent dream settings', ModelID: assistant.ModelID, Budget: assistant.Budget });
    expect((await (await page.request.get(`/api/dreams/config?assistant_id=${other.ID}`)).json()).MaxDurationSeconds).toBe(3600);
    await page.evaluate(async () => {
      const path = '/@id/@molr/sdk', { api } = await import(path);
      const current = await api.get('/api/settings');
      await api.mutate('/api/settings', { Settings: { ...current, Timezone: 'America/New_York' } }, { idempotencyKey: crypto.randomUUID(), expectedVersion: current.Version });
    });
    await expect(dreamSettings).not.toContainText('America/New_York');
    await expect(input).toHaveValue('1200');
    for (const id of [assistant.ID, other.ID]) expect((await (await page.request.get(`/api/dreams/config?assistant_id=${id}`)).json()).Timezone).toBe('America/New_York');
    // Start a fresh document so pending assistant-form refreshes are excluded.
    await page.goto('about:blank');
    let dreamReads = 0;
    page.on('request', request => { if (request.method() === 'GET' && new URL(request.url()).pathname.startsWith('/api/dreams')) dreamReads++; });
    await page.goto(`/#/account/memory?assistant_id=${assistant.ID}`); await page.reload();
    await expect(page.getByRole('combobox', { name: 'Select assistant' })).toContainText(changedName);
    await expect(page.getByRole('button', { name: 'New file', exact: true })).toBeVisible();
    await expect(page.locator('.dream-panel, .dream-run')).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Consolidate / retry', exact: true })).toHaveCount(0);
    expect(dreamReads).toBe(0);
    const address = page.getByRole('navigation', { name: 'Memory folders' });
    await expect(page.getByRole('heading', { name: 'Nothing here yet', exact: true })).toBeVisible();
    await expect(address).toHaveCount(0);
    await page.getByRole('button', { name: 'New folder', exact: true }).click();
    await page.getByRole('textbox', { name: 'Folder name', exact: true }).fill('Empty directory');
    await page.getByRole('dialog').getByRole('button', { name: 'Save', exact: true }).click();
    await expect(address).toBeVisible();
    await page.locator('.settings-rows').getByRole('button', { name: 'Empty directory', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Nothing here yet', exact: true })).toBeVisible();
    await expect(address.getByRole('button', { name: 'Empty directory', exact: true })).toHaveAttribute('aria-current', 'location');
    await address.getByRole('button', { name: 'Memory', exact: true }).click();
    await expect(page.locator('.settings-rows').getByRole('button', { name: 'Empty directory', exact: true })).toBeVisible();

    await expect(page.getByRole('button', { name: 'Dream settings', exact: true })).toHaveCount(0);
  } finally { await page.unrouteAll({ behavior: 'wait' }); await settings(page, { Language: original.Language, Timezone: original.Timezone, WorkbenchVisible: original.WorkbenchVisible }); }
});
