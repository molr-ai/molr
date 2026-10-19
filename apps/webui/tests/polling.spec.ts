import { test, expect } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.goto('/');
  await page.clock.install();
  await page.clock.pauseAt(new Date());
});

test('shared query polling serializes slow requests, shares the fastest cadence, and pauses hidden/offline', async ({ page }) => {
  await page.evaluate(async () => {
    const path = '/src/query-store.ts'; const { QueryStore } = await import(path);
    const state = window as any; state.calls = 0;
    state.store = new QueryStore();
    state.load = () => { state.calls++; return new Promise(resolve => state.resolve = resolve); };
    state.button = state.store.subscribe('shell', () => {}, 5000);
    state.store.refresh('shell', 0, state.load);
    state.panel = state.store.subscribe('shell', () => {}, 2000);
    state.store.refresh('shell', 0, state.load);
  });
  expect(await page.evaluate(() => (window as any).calls)).toBe(1);
  await page.clock.runFor(10000);
  expect(await page.evaluate(() => (window as any).calls)).toBe(1);
  await page.evaluate(() => (window as any).resolve('running'));
  await page.clock.runFor(1999);
  expect(await page.evaluate(() => (window as any).calls)).toBe(1);
  await page.clock.runFor(1);
  expect(await page.evaluate(() => (window as any).calls)).toBe(2);
  await page.evaluate(() => { const s = window as any; s.resolve('running'); s.panel(); });
  await page.clock.runFor(4999);
  expect(await page.evaluate(() => (window as any).calls)).toBe(2);
  await page.clock.runFor(1);
  expect(await page.evaluate(() => (window as any).calls)).toBe(3);
  await page.evaluate(() => {
    (window as any).resolve('running');
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await page.clock.runFor(60000);
  expect(await page.evaluate(() => (window as any).calls)).toBe(3);
  await page.evaluate(() => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
    Object.defineProperty(navigator, 'onLine', { configurable: true, value: false });
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await page.clock.runFor(60000);
  expect(await page.evaluate(() => (window as any).calls)).toBe(3);
  await page.evaluate(() => {
    Object.defineProperty(navigator, 'onLine', { configurable: true, value: true });
    window.dispatchEvent(new Event('online'));
  });
  expect(await page.evaluate(() => (window as any).calls)).toBe(4);
  await page.evaluate(() => (window as any).store.clear());
});

test('errors back off, events cannot bypass Retry-After, and recovered queries resume their cadence', async ({ page }) => {
  await page.evaluate(async () => {
    const path = '/src/query-store.ts', client = '/src/api/client.ts';
    const { QueryStore } = await import(path), { APIError } = await import(client);
    const s = window as any; s.calls = 0; s.store = new QueryStore();
    s.load = async () => { s.calls++; if (s.calls < 3) throw new APIError('unavailable', '', 503); if (s.calls === 3) throw new APIError('rate_limited', '', 429, Date.now() + 20000); return 'recovered'; };
    s.store.subscribe('item', () => {}, 1000); s.store.refresh('item', 0, s.load);
  });
  await page.clock.runFor(1999); expect(await page.evaluate(() => (window as any).calls)).toBe(1);
  await page.clock.runFor(1); expect(await page.evaluate(() => (window as any).calls)).toBe(2);
  await page.clock.runFor(4000); expect(await page.evaluate(() => (window as any).calls)).toBe(3);
  await page.evaluate(() => { const s = window as any; for (let i = 1; i <= 10; i++) s.store.refresh('item', i, s.load); });
  await page.clock.runFor(19999); expect(await page.evaluate(() => (window as any).calls)).toBe(3);
  await page.clock.runFor(1); expect(await page.evaluate(() => (window as any).calls)).toBe(4);
  await page.clock.runFor(1000); expect(await page.evaluate(() => (window as any).calls)).toBe(5);
  await page.evaluate(() => (window as any).store.clear());
});

test('read-only POSTs do not emit mutations, writes still do', async ({ page }) => {
  await page.goto('/');
  await page.route('**/api/auth/me', route => route.fulfill({ json: {}, headers: { 'X-Molr-CSRF-Cookie': 'molr_dev_csrf' } }));
  await page.route('**/api/assistants/a/shell-tasks', route => route.fulfill({ json: { Output: '', NextCursor: 0 } }));
  const changes = await page.evaluate(async runtimeClientPath => {
    const { createClient } = await import('/@fs' + runtimeClientPath);
    const { api, onMutation } = createClient();
    const changes: string[] = []; const release = onMutation((path: string) => changes.push(path));
    await api.read('/api/assistants/a/shell-tasks', { action: 'read' });
    const afterRead = [...changes];
    await api.post('/api/assistants/a/shell-tasks', { action: 'stop' });
    release(); return { afterRead, afterWrite: changes };
  }, new URL('../../../packages/sdk/src/client.ts', import.meta.url).pathname);
  expect(changes.afterRead).toEqual([]);
  expect(changes.afterWrite).toEqual(['/api/assistants/a/shell-tasks']);
});
