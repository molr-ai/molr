import { test, expect } from '@playwright/test';

test('fresh SSE positions current snapshots and later business changes stay live', async ({ page }) => {
  await page.addInitScript(() => {
    const Native = window.EventSource;
    (window as any).startupStream = { ready: [], updates: [] };
    window.EventSource = class extends Native {
      constructor(url: string | URL, options?: EventSourceInit) {
        super(url, options);
        if (String(url).startsWith('/api/events/stream')) {
          this.addEventListener('ready', event => {
            const cursor = (event as MessageEvent).lastEventId;
            (window as any).startupStream.ready.push(JSON.parse(atob(cursor.replaceAll('-', '+').replaceAll('_', '/'))).Sequence);
          });
          this.addEventListener('update', event => { (window as any).startupStream.updates.push(JSON.parse((event as MessageEvent).data)); });
        }
      }
    };
  });
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.locator('.assistant-workspace')).toBeVisible();
  await page.reload();
  await expect(page.locator('.assistant-workspace')).toBeVisible();
  await expect.poll(() => page.evaluate(() => (window as any).startupStream.ready.length)).toBe(1);
  const origin = new URL(page.url()).origin, port = new URL(origin).port;
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
  async function create(path: string, data: object) {
    const response = await page.request.post(path, { headers: { Origin: origin, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } } });
    expect(response.ok(), await response.text()).toBe(true);
    return response.json();
  }
  const endpoint = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? '18882'}`;
  const provider = await create('/api/resources', { Name: 'Live SSE fixture', Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: endpoint, AllowedOrigins: [endpoint], Models: [{ Name: 'live-sse', ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  await create('/api/assistants', { Name: 'Live SSE fixture', ModelID: models.find((m: { ResourceID: string }) => m.ResourceID === provider.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  await expect.poll(() => page.evaluate(() => (window as any).startupStream.updates.length)).toBeGreaterThan(0);
  expect(await page.evaluate(() => (window as any).startupStream.updates.every((e: any) => e.Sequence > (window as any).startupStream.ready[0]))).toBe(true);
});

// Pause browser timers after connection so the 30 s session poll cannot make
// this pass: the real Runtime closes SSE and its immediate probe must sign out.
test('revoking a connected session returns to login without periodic polling', async ({ page }) => {
  await page.addInitScript(() => {
    const Native = window.EventSource;
    (window as any).runtimeConnected = false;
    window.EventSource = class extends Native {
      constructor(url: string | URL, options?: EventSourceInit) {
        super(url, options);
        if (String(url).startsWith('/api/events/stream')) this.addEventListener('open', () => { (window as any).runtimeConnected = true; });
      }
    };
  });
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect.poll(() => page.evaluate(() => (window as any).runtimeConnected)).toBe(true);
  await page.clock.install();
  await page.clock.pauseAt(new Date());
  const principal = await (await page.request.get('/api/auth/me')).json();
  const origin = new URL(page.url()).origin;
  const port = new URL(origin).port;
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
  const probe = page.waitForResponse(r => new URL(r.url()).pathname === '/api/auth/me' && r.status() === 401);
  const revoked = await page.request.delete(`/api/auth/sessions/${principal.SessionID}`, { headers: { Origin: origin, 'X-CSRF-Token': csrf } });
  expect(revoked.status()).toBe(200);
  await probe;
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible({ timeout: 3000 });
});
