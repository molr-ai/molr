import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import type { Assistant, Model, DebugRun, TrajectoryEntry } from '@molr/sdk';

const port = process.env.MOLR_TEST_PORT ?? '5193', base = `http://127.0.0.1:${port}`;
async function post<T>(page: Page, path: string, data: object, version?: number): Promise<T> {
  const csrf = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: version } } });
  expect(response.status(), await response.text()).toBe(200); return response.json();
}

async function setup(page: Page, suffix: string) {
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const name = `Timeline time ${suffix}-${Date.now()}`, endpoint = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post<{ ID: string }>(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: endpoint, AllowedOrigins: [endpoint], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1000000, CachedInputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 1000000 } }] } });
  const models: Model[] = await (await page.request.get('/api/models')).json();
  const assistant = await post<Assistant>(page, '/api/assistants', { Name: name, ModelID: models.find(model => model.ResourceID === resource.ID)!.ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  await page.goto(`/#/assistants/${assistant.ID}`);
  await expect(page.getByRole('textbox', { name: 'Message your assistant', exact: true })).toBeEnabled();
  return assistant;
}

declare global { interface Window { trajectoryTimeConstructions: number; trajectoryTimeLocales: string[] } }

test('timeline scrolling reuses formatters and account timezone changes update time labels', async ({ page }, info) => {
  await page.addInitScript(() => {
    window.trajectoryTimeConstructions = 0; window.trajectoryTimeLocales = [];
    Intl.DateTimeFormat = new Proxy(Intl.DateTimeFormat, { construct(target, args, newTarget) {
      const options = args[1];
      if (options?.hour === '2-digit' && options?.minute === '2-digit' && options?.second === '2-digit' && options?.hour12 === false) { window.trajectoryTimeConstructions++; window.trajectoryTimeLocales.push(String(args[0])); }
      return Reflect.construct(target, args, newTarget);
    } });
  });
  const assistant = await setup(page, info.project.name);
  const settings = await (await page.request.get('/api/settings')).json();
  try {
    await post(page, '/api/settings', { Settings: { ...settings, Timezone: 'UTC' } }, settings.Version);
    const runs: DebugRun[] = Array.from({ length: 50 }, (_, index) => ({ ID: 'time-run-' + index, AssistantID: assistant.ID, EventID: 'time-event-' + index, ExecutionID: 'time-execution-' + index, Trigger: 'user_message', Attempt: 1, State: 'succeeded', Reason: '', StartedAt: new Date(Date.UTC(2026, 0, 1, 1, 2 + index, 3)).toISOString(), FinishedAt: new Date(Date.UTC(2026, 0, 1, 1, 2 + index, 4)).toISOString(), Incomplete: false, Steps: null }));
    const entries: TrajectoryEntry[] = runs.flatMap((run, index) => [
      { RunID: run.ID, Step: { Sequence: 1, Revision: index * 2 + 1, Kind: 'model.start', OperationID: 'model-' + index, At: run.StartedAt, State: 'started', DataStatus: 'captured' } },
      { RunID: run.ID, Step: { Sequence: 2, Revision: index * 2 + 2, Kind: 'model.complete', OperationID: 'model-' + index, At: run.FinishedAt!, State: 'succeeded', DataStatus: 'captured' } },
    ]);
    runs[0].State = 'failed'; entries[1].Step.State = 'failed'; entries[1].Step.NextRetryAt = '2026-01-01T01:03:03Z';
    await page.route('**/api/debug/trajectory?*', route => route.fulfill({ json: { Items: new URL(route.request().url()).searchParams.has('after') ? [] : entries, Runs: runs, RunsReset: true, RunsCursor: 'synthetic-runs', NextCursor: '', HeadCursor: 'synthetic-head', ContextResetAt: '2026-01-01T01:01:00Z', Cleanup: null } }));
    await page.goto('/#/workbench/' + assistant.ID); await page.reload();
    const scroll = page.locator('.trajectory-scroll'); await expect(scroll).toBeVisible();
    await scroll.evaluate(element => { element.scrollTop = 0; element.dispatchEvent(new Event('scroll', { bubbles: true })); });
    const first = page.locator('[data-record-id="run:time-run-0"] time');
    await expect(first).toHaveText('01:02:03');
    await expect(page.locator('.trajectory-reset time')).toHaveText('01:01:00');
    await expect(page.locator('[data-record-id="time-run-0:model:model-0"] .trajectory-preview')).toContainText('01:03:03');
    await page.evaluate(() => { window.trajectoryTimeConstructions = 0; });
    await scroll.evaluate(async element => {
      for (const top of [240, 480, 720, 960, 720, 480, 240, 0]) {
        element.scrollTop = top; element.dispatchEvent(new Event('scroll', { bubbles: true }));
        await new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));
      }
    });
    const constructions = await page.evaluate(() => window.trajectoryTimeConstructions);
    await info.attach('formatter-work', { body: JSON.stringify({ loadedRuns: runs.length, scrollUpdates: 8, constructions }), contentType: 'application/json' });
    expect.soft(constructions).toBe(0);
    await expect(first).toHaveText('01:02:03');
    await page.evaluate(async () => {
      const path = '/@id/@molr/sdk', { api } = await import(path);
      const settings = await api.get('/api/settings');
      await api.mutate('/api/settings', { Settings: { ...settings, Timezone: 'Asia/Shanghai' } }, settings.Version);
    });
    await expect(first).toHaveText('09:02:03');
    await expect(page.locator('.trajectory-reset time')).toHaveText('09:01:00');
    await expect(page.locator('[data-record-id="time-run-0:model:model-0"] .trajectory-preview')).toContainText('09:03:03');
    await page.evaluate(async () => {
      const path = '/@id/@molr/sdk', { api } = await import(path);
      const settings = await api.get('/api/settings');
      await api.mutate('/api/settings', { Settings: { ...settings, Language: 'zh' } }, settings.Version);
    });
    await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN');
    await expect.poll(() => page.evaluate(() => window.trajectoryTimeLocales.includes('zh'))).toBe(true);
    await expect(first).toHaveText('09:02:03');
  } finally {
    await page.evaluate(async original => {
      const path = '/@id/@molr/sdk', { api } = await import(path);
      const current = await api.get('/api/settings');
      await api.mutate('/api/settings', { Settings: { ...current, Language: original.Language, Timezone: original.Timezone } }, current.Version);
    }, settings);
  }
});
