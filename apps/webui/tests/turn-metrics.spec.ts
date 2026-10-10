import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
test.setTimeout(150_000);
const port = process.env.MOLR_TEST_PORT ?? '5193';
const base = `http://127.0.0.1:${port}`;
async function post(page: Page, path: string, data: Record<string, unknown> & { Mutation?: object }) {
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID(), ...data.Mutation } } });
  expect(response.status(), await response.text()).toBe(200); return response.json();
}
async function waitForBorn(page: Page, assistant: { ID: string; ConversationID: string }) {
  let replies: any[] = [];
  await expect.poll(async () => {
    const messages = await (await page.request.get(`/api/conversations/${assistant.ConversationID}/messages`)).json();
    replies = (messages.Items ?? []).filter((message: any) => message.Sender === 'assistant');
    const status = await (await page.request.get(`/api/assistants/${assistant.ID}/status`)).json();
    return replies.length > 0 && status.State === 'idle';
  }, { timeout: 60000 }).toBe(true);
  return replies;
}

test('turn info queries lazily, updates the whole turn and shares finalized cache across bubbles', async ({ page }, info) => {
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const setWorkbench = async (enabled: boolean) => {
    const settings = await (await page.request.get('/api/settings')).json();
    await post(page, '/api/settings', { Settings: { ...settings, WorkbenchVisible: enabled }, Mutation: { ExpectedVersion: settings.Version } });
  };
  await setWorkbench(false);
  const name = `Turn metrics ${info.project.name}-${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 100000, CachedInputMicrosPerMillion: 10000, OutputMicrosPerMillion: 200000 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  const assistant = await post(page, '/api/assistants', { Name: name, ModelID: models.find((m: any) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 1000000 }, Period: 'daily' } });
  const born = await waitForBorn(page, assistant);
  await page.goto(`/#/assistants/${assistant.ID}`);
  let requests = 0; let final: any;
  page.on('request', request => { if (request.url().endsWith('/metrics')) requests++; });
  page.on('response', async response => { if (response.url().endsWith('/metrics') && response.ok()) { const body = await response.json(); if (body.Finalized) final = body; } });
  await page.getByRole('textbox', { name: 'Message your assistant', exact: true }).fill('Exercise turn statistics');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  const icons = page.getByRole('button', { name: 'Turn information', exact: true });
  await expect(icons).toHaveCount(born.length + 2);
  expect(requests).toBe(0);
  await icons.nth(born.length).click();
  const panel = page.getByRole('dialog', { name: 'Turn information', exact: true });
  await expect(panel).toBeVisible();
  await expect(panel).toContainText('Partial statistics');
  await expect.poll(() => final, { timeout: 60000 }).toBeTruthy();
  expect(final.Usage).toEqual({ AudioInputTokens: 0, InputTokens: 90, CachedTokens: 36, OutputTokens: 30 });
  expect(final.ModelCalls).toBe(3); expect(final.ToolCalls).toBe(3);
  expect(final.FirstTokenMs).toBeGreaterThan(0); expect(final.TokensPerSecond).toBeGreaterThan(0);
  expect(final.ElapsedMs).toBeGreaterThan(6500); expect(final.Charges[0].Micros).toBeGreaterThan(0);
  await expect(panel).toContainText('Completed');
  await expect(panel.getByText('Per-call details', { exact: true })).toHaveCount(0);
  await expect(panel.getByRole('button', { name: 'View in developer workbench', exact: true })).toHaveCount(0);
  const box = await panel.boundingBox(); expect(box!.x).toBeGreaterThanOrEqual(0); expect(box!.x + box!.width).toBeLessThanOrEqual(page.viewportSize()!.width + 1);
  const completedRequests = requests;
  await page.keyboard.press('Escape');
  await icons.last().click();
  await expect(panel).toContainText('90');
  await page.waitForTimeout(2300);
  expect(requests).toBe(completedRequests);
  await page.reload();
  await icons.nth(born.length).click();
  await expect.poll(() => requests).toBeGreaterThan(completedRequests);
  await setWorkbench(true);
  try {
    await page.reload(); await icons.nth(born.length).click();
    await expect(panel.getByText('Per-call details', { exact: true })).toHaveCount(0);
    await panel.getByRole('button', { name: 'View in developer workbench', exact: true }).click();
    await expect(page).toHaveURL(`${base}/#/workbench/${assistant.ID}?turn=${final.TurnID}`);
    await expect(page.getByRole('region', { name: 'Step details', exact: true })).toContainText('Exercise turn statistics');
    await expect(page.getByRole('heading', { name: 'Workbench', exact: true })).toBeVisible();
    await expect(page.getByRole('combobox', { name: 'Assistant', exact: true })).toContainText(assistant.Name);
    await expect(panel).not.toBeVisible();
    // Deployment capability must also be present, even when the account shows workbench entries.
    await page.route('**/api/status', async route => { const response = await route.fetch(); await route.fulfill({ json: { ...await response.json(), workbench_available: false } }); });
    await page.goto(`/#/assistants/${assistant.ID}`); await page.reload(); await icons.nth(born.length).click();
    await expect(panel).toBeVisible();
    await expect(panel.getByRole('button', { name: 'View in developer workbench', exact: true })).toHaveCount(0);
  } finally {
    await page.unrouteAll({ behavior: 'wait' });
    await setWorkbench(false);
  }
});


test('turn info locates an older recorded turn across pages and survives reload', async ({ page }, info) => {
  test.setTimeout(90_000);
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const settings = await (await page.request.get('/api/settings')).json();
  await post(page, '/api/settings', { Settings: { ...settings, WorkbenchVisible: true }, Mutation: { ExpectedVersion: settings.Version } });
  try {
    const name = `Turn navigation ${info.project.name}-${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
    const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } }] } });
    const models = await (await page.request.get('/api/models')).json();
    const assistant = await post(page, '/api/assistants', { Name: name, ModelID: models.find((m: any) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
    await waitForBorn(page, assistant);
    let runs: any[] = [];
    for (let i = 1; i <= 3; i++) {
      await post(page, '/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: `Locate turn ${i}` } });
      await expect.poll(async () => { runs = (await (await page.request.get(`/api/debug/runs?assistant_id=${assistant.ID}`)).json()).Items.filter((run: any) => run.Source?.Text?.startsWith('Locate turn ')); return runs.length === i && runs.every(run => run.State === 'succeeded'); }, { timeout: 30_000 }).toBe(true);
    }
    const first = runs.find(run => run.Source?.Text === 'Locate turn 1');
    expect(first).toBeTruthy();
    const history = await (await page.request.get(`/api/conversations/${assistant.ConversationID}/messages`)).json();
    const target = history.Items.find((message: any) => message.Sender === 'assistant' && message.Source?.ExecutionID === first.ExecutionID);
    expect(target).toBeTruthy();
    let olderRequests = 0;
    // Keep real API data while forcing the target outside the initial page.
    await page.route('**/api/debug/trajectory?*', async route => {
      const url = new URL(route.request().url()); url.searchParams.set('limit', '20');
      if (url.searchParams.has('cursor')) olderRequests++;
      const response = await route.fetch({ url: url.toString() }); await route.fulfill({ response });
    });
    await page.goto(`/#/assistants/${assistant.ID}`); await page.reload();
    await page.locator(`[data-message-id="${target.ID}"]`).getByRole('button', { name: 'Turn information', exact: true }).click();
    await page.getByRole('button', { name: 'View in developer workbench', exact: true }).click();
    await expect(page).toHaveURL(`${base}/#/workbench/${assistant.ID}?turn=${first.ExecutionID}`);
    const inspector = page.getByRole('region', { name: 'Step details', exact: true });
    await expect(inspector).toContainText('Locate turn 1');
    expect(olderRequests).toBeGreaterThan(0);
    await expect(page.getByRole('switch', { name: 'Follow live execution', exact: true })).not.toBeChecked();
    await inspector.getByRole('button', { name: 'Close', exact: true }).click();
    await expect(page.locator('.trajectory-run[aria-pressed="true"]')).toHaveCount(0);
    await page.reload();
    await expect(inspector).toContainText('Locate turn 1');
    if (info.project.name === 'desktop') await expect(page.locator('.trajectory-run[aria-pressed="true"]')).toContainText('Locate turn 1');
    await inspector.getByRole('button', { name: 'Close', exact: true }).click();
    await expect(page.locator('.trajectory-trigger-copy').filter({ hasText: 'Locate turn 1' })).toBeVisible();
  } finally {
    await page.unrouteAll({ behavior: 'wait' });
    const latest = await (await page.request.get('/api/settings')).json();
    await post(page, '/api/settings', { Settings: { ...latest, WorkbenchVisible: settings.WorkbenchVisible }, Mutation: { ExpectedVersion: latest.Version } });
  }
});
