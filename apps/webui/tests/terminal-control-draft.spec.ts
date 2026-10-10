import { test, expect } from '@playwright/test';
import type { Page, Route } from '@playwright/test';

const port = process.env.MOLR_TEST_PORT ?? '5193', base = `http://127.0.0.1:${port}`;
async function post(page: Page, path: string, data: object) {
  const csrf = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } } });
  expect(response.status(), await response.text()).toBe(200);
  return response.json();
}

// Authentication and assistant data use a real Runtime; terminal timing is deterministic.
async function terminal(page: Page, name: string, control?: (route: Route) => Promise<void>) {
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  const assistant = await post(page, '/api/assistants', { Name: name, ModelID: models.find((model: { ResourceID: string }) => model.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  const actions: object[] = [];
  await page.route(`**/api/assistants/${assistant.ID}/shell-tasks`, async route => {
    if (route.request().method() === 'GET') {
      await route.fulfill({ json: { Items: [...['tty-first', 'tty-second'].map(ID => ({ ID, Mode: 'tty', Script: ID, State: 'running', StartedAt: new Date().toISOString() }))] } });
      return;
    }
    const action = route.request().postDataJSON();
    if (action.action === 'read') await route.fulfill({ json: { Output: '', NextCursor: 0, Finished: false, OutputComplete: true } });
    else { actions.push(action); if (control) await control(route); else await route.fulfill({ json: { Accepted: true } }); }
  });
  await page.goto(`/#/assistants/${assistant.ID}`);
  await page.getByRole('button', { name: 'Running tasks (2)', exact: true }).click();
  await page.getByRole('button', { name: /^tty-first/ }).click();
  const input = page.getByRole('textbox', { name: 'Terminal input', exact: true });
  const send = page.locator('.shell-task-input').getByRole('button', { name: 'Send', exact: true });
  await expect(input).toBeVisible();
  return { input, send, actions };
}

for (const key of ['Ctrl-D', 'Ctrl-C']) {
  test(`terminal ${key} preserves the unsent command draft`, async ({ page }, info) => {
    const { input, actions } = await terminal(page, `${key} ${info.project.name}-${Date.now()}`);
    await input.fill('echo unsent-command');
    const button = page.getByRole('button', { name: key, exact: true });
    await button.click();
    await expect.poll(() => actions).toEqual([expect.objectContaining(key === 'Ctrl-D'
      ? { action: 'write', task_id: 'tty-first', data: '\x04' }
      : { action: 'interrupt', task_id: 'tty-first' })]);
    await expect(button).toBeEnabled();
    await expect(input).toHaveValue('echo unsent-command');
  });
}

for (const scenario of ['unchanged', 'edited', 'retyped', 'failed', 'other terminal', 'returned terminal'] as const) {
  test(`terminal send keeps the correct draft: ${scenario}`, async ({ page }, info) => {
    let release!: () => void;
    const held = new Promise<void>(resolve => { release = resolve; });
    const { input, send, actions } = await terminal(page, `Send ${scenario} ${info.project.name}-${Date.now()}`, async route => {
      await held;
      await route.fulfill(scenario === 'failed'
        ? { status: 409, json: { error: { code: 'conflict', message: 'Fixture rejected input' } } }
        : { json: { Accepted: true } });
    });
    try {
      await input.fill('echo first-command');
      await send.click();
      await expect.poll(() => actions).toEqual([expect.objectContaining({ action: 'write', task_id: 'tty-first', data: 'echo first-command\n' })]);
      await expect(send).toBeDisabled();
      if (scenario === 'edited') await input.fill('echo next-command');
      if (scenario === 'retyped') { await input.fill(''); await input.fill('echo first-command'); }
      if (scenario === 'other terminal' || scenario === 'returned terminal') {
        await page.getByRole('button', { name: 'Back to tasks', exact: true }).click();
        await page.getByRole('button', { name: /^tty-second/ }).click();
        if (scenario === 'returned terminal') {
          await page.getByRole('button', { name: 'Back to tasks', exact: true }).click();
          await page.getByRole('button', { name: /^tty-first/ }).click();
        }
        await input.fill('echo first-command');
      }
      release();
      await expect(send).toBeEnabled();
      await expect(input).toHaveValue(scenario === 'unchanged' ? '' : scenario === 'edited' ? 'echo next-command' : 'echo first-command');
      if (scenario === 'edited') {
        await send.click();
        await expect.poll(() => actions).toHaveLength(2);
        expect(actions[1]).toMatchObject({ action: 'write', data: 'echo next-command\n' });
        await expect(input).toHaveValue('');
      }
    } finally { release(); }
  });
}
