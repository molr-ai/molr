import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
const port = process.env.MOLR_TEST_PORT ?? '5193', base = `http://127.0.0.1:${port}`;
async function post(page: Page, path: string, data: object) {
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } } });
  expect(response.status(), await response.text()).toBe(200); return response.json();
}

// Auth and the chat use a real Runtime; only shell process timing is a fixture.
// Manager persistence, output finality and lifecycle events are covered in Go.
test('shell output reads stay local, pause in background and stop after terminal EOF', async ({ page }, info) => {
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const name = `Polling ${info.project.name}-${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  const assistant = await post(page, '/api/assistants', { Name: name, ModelID: models.find((m: any) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  let finished = false, reads = 0, lists = 0;
  await page.route(`**/api/assistants/${assistant.ID}/shell-tasks`, async route => {
    if (route.request().method() === 'GET') {
      lists++;
      await route.fulfill({ json: { Items: [{ ID: 'poll-task', Mode: 'background', Script: 'echo polling-fixture', State: finished ? 'completed' : 'running', StartedAt: new Date().toISOString() }] } });
    } else {
      reads++;
      await route.fulfill({ json: { Output: reads === 1 ? 'fixture output' : '', NextCursor: 14, Finished: finished, OutputComplete: false } });
    }
  });
  await page.goto(`/#/assistants/${assistant.ID}`);
  await page.getByRole('button', { name: 'Running tasks (1)', exact: true }).click();
  await page.getByRole('button', { name: /echo polling-fixture/ }).click();
  await expect(page.locator('.shell-task-output')).toContainText('fixture output');
  // Finish the real born execution before measuring shell-only polling.
  await expect.poll(async () => (await (await page.request.get(`/api/assistants/${assistant.ID}/status`)).json()).WaitingReason).toBeTruthy();
  const now = new Date();
  await page.clock.install({ time: now }); await page.clock.pauseAt(new Date(now.getTime() + 1000));
  await page.clock.runFor(1000); // Flush existing event batches before observing new reads.
  const unrelated: string[] = [];
  page.on('request', request => { const path = new URL(request.url()).pathname; if (['/api/status', '/api/assistants', '/api/conversations', '/api/grants', '/api/schedules'].includes(path)) unrelated.push(path); });
  const before = reads;
  await page.clock.runFor(1500); await expect.poll(() => reads).toBeGreaterThan(before);
  await page.clock.runFor(500);
  expect(unrelated).toEqual([]);
  await page.evaluate(() => { Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' }); document.dispatchEvent(new Event('visibilitychange')); });
  const paused = { reads, lists };
  await page.clock.runFor(30000); expect({ reads, lists }).toEqual(paused);
  finished = true;
  await page.evaluate(() => { Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' }); document.dispatchEvent(new Event('visibilitychange')); });
  await expect.poll(() => reads).toBeGreaterThan(paused.reads);
  await expect(page.locator('.shell-tasks-content')).toContainText('Completed');
  await page.clock.runFor(1500);
  const stopped = reads;
  await page.clock.runFor(10000);
  expect(reads).toBe(stopped);
});
