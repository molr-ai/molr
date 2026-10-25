import { test, expect } from '@playwright/test';
import type { MolrAppAPI } from '@molr/ui-host/app';

async function desktop(page: import('@playwright/test').Page) {
  await page.addInitScript(() => {
    const calls = { permission: 0, shown: 0, closed: 0, fail: false, badge: 0, badges: [] as number[], badgeOK: true, ended: 0 };
    Object.assign(window, { notificationFixture: calls });
    // Desktop must not depend on the browser's denied permission.
    Object.defineProperty(Notification, 'permission', { get: () => 'denied' });
    window.molrApp = {
      updates: { status: async () => ({ comparison: 'current', checking: false }), check: async () => ({ comparison: 'current', checking: false }), openDownloads: async () => {}, upgradeLocal: async () => {}, onChanged: () => () => {} },
      getInfo: async () => ({ apiVersion: 1, platform: 'darwin', build: { version: '0.1.0', commit: '' }, connection: { kind: 'remote', name: 'Fixture', url: location.origin }, window: { maximized: false, fullscreen: false } }),
      window: { minimize: async () => {}, toggleMaximize: async () => {}, close: async () => {} },
      disconnect: async () => {}, onWindowStateChanged: () => () => {},
      oauth: { authorize: async () => ({ status: 'cancelled' }), cancel: async () => {} },
      notifications: {
        begin: async () => {}, end: async () => { calls.ended++; calls.badge = 0; }, setBadge: async (_scope, count) => { calls.badge = count; calls.badges.push(count); return calls.badgeOK; }, requestPermission: async () => { calls.permission++; return true; },
        show: async () => { calls.shown++; if (calls.fail) throw new Error('OS rejected notification'); },
        close: async () => { calls.closed++; }, onClick: () => () => {},
      },
    } satisfies MolrAppAPI;
  });
  await page.goto('/#/account/notifications');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('switch', { name: 'Desktop notifications', exact: true })).toBeVisible();
}
test('desktop switch requests OS initialization without a second authorization control and persists', async ({ page }) => {
  await desktop(page);
  const calls = () => page.evaluate(() => (window as any).notificationFixture);
  expect((await calls()).permission).toBe(0);
  await expect(page.getByText(/blocked by the browser/)).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Allow desktop notifications' })).toHaveCount(0);
  const toggle = page.getByRole('switch', { name: 'Desktop notifications', exact: true });
  await toggle.uncheck(); await toggle.check();
  expect((await calls()).permission).toBe(1);
  await page.getByRole('button', { name: 'Send test notification' }).click();
  expect((await calls()).shown).toBe(1);
  await expect(page.getByText('Test notification requested. Display depends on system notification and Focus settings.')).toBeVisible();
  await toggle.uncheck();
  await page.getByRole('button', { name: 'Send test notification' }).click();
  expect((await calls()).shown).toBe(1);
  await expect(page.getByText('An in-app test alert was displayed.', { exact: true })).toBeVisible();
  await page.reload(); await expect(toggle).not.toBeChecked();
  expect((await calls()).permission).toBe(0);
});
test('desktop errors remain in the matching setting row and allow retry', async ({ page }) => {
  await desktop(page);
  const section = page.getByRole('region', { name: 'Desktop notifications', exact: true });
  await page.evaluate(() => { (window as any).notificationFixture.fail = true; });
  await section.getByRole('button', { name: 'Send test notification' }).click();
  await expect(section.getByText(/could not be sent/)).toBeVisible();
  await expect(section.getByText(/allow Molr in system notification settings/)).toBeVisible();
  await page.getByRole('button', { name: 'Preview sound' }).click();
  await expect(page.getByRole('region', { name: 'Message sound', exact: true }).getByText('Preview sound played.')).toBeVisible();
  await expect(section.getByText(/could not be sent/)).toBeVisible();
  await page.evaluate(() => { (window as any).notificationFixture.fail = false; });
  await section.getByRole('button', { name: 'Send test notification' }).click();
  await expect(section.getByText(/Test notification requested/)).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
test('desktop adapter closes its scope and ignores previous login clicks', async ({ page }) => {
  await page.goto('/');
  const result = await page.evaluate(async () => {
    const path = '/src/notifications/desktop.ts';
    const { createDesktopNotifications } = await import(path);
    const scopes: string[] = [], closed: string[] = [], routes: string[] = [], callbacks: ((scope: string, route: string) => void)[] = [];
    const bridge = { end: async (scope: string) => { closed.push(scope); }, setBadge: async () => true, begin: async (scope: string) => { scopes.push(scope); }, requestPermission: async () => true, show: async () => {}, close: async (scope: string) => { closed.push(scope); }, onClick: (callback: (scope: string, route: string) => void) => { callbacks.push(callback); return () => {}; } };
    const one = createDesktopNotifications(bridge, (route: string) => routes.push(route));
    one.dispose();
    const two = createDesktopNotifications(bridge, (route: string) => routes.push(route));
    callbacks[0](scopes[0], '#/assistants/old'); callbacks[1](scopes[0], '#/assistants/old');
    callbacks[1](scopes[1], '#/assistants/new');
    await two.close(); two.dispose(); await Promise.resolve(); await Promise.resolve();
    return { distinct: scopes[0] !== scopes[1], closed, scopes, routes };
  });
  expect(result.distinct).toBe(true); expect(result.closed).toContain(result.scopes[0]); expect(result.closed).toContain(result.scopes[1]);
  expect(result.routes).toEqual(['#/assistants/new']);
});

test('browser notification switch directly requests permission', async ({ page }) => {
  await page.addInitScript(() => {
    let permission = 'default';
    Object.defineProperty(Notification, 'permission', { get: () => permission });
    Object.defineProperty(Notification, 'requestPermission', { value: async () => { permission = 'granted'; return permission; } });
  });
  await page.goto('/#/account/notifications');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  const toggle = page.getByRole('switch', { name: 'Desktop notifications', exact: true });
  await toggle.uncheck(); await toggle.check();
  await expect(page.getByText('Desktop notifications allowed.', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Allow desktop notifications' })).toHaveCount(0);
  await expect(page.getByRole('switch', { name: 'App icon unread count' })).toHaveCount(0);
});


test('badge tracks real unread messages while hidden, persists and clears independently of alerts', async ({ page }) => {
  await desktop(page);
  const badge = page.getByRole('switch', { name: 'App icon unread count', exact: true });
  const count = () => page.evaluate(() => (window as any).notificationFixture.badge);
  await expect(badge).not.toBeChecked();
  expect(await count()).toBe(0);
  const port = process.env.MOLR_TEST_PORT ?? '5193';
  async function post(path: string, data: object, mutation = true) {
    const csrf = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${port}`)!.value;
    const response = await page.request.post(path, { headers: { Origin: `http://127.0.0.1:${port}`, 'X-CSRF-Token': csrf }, data: mutation ? { ...data, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: 0 } } : data });
    expect(response.ok(), await response.text()).toBe(true); return response.json();
  }
  const chats = await (await page.request.get('/api/conversations')).json();
  for (const chat of chats.Items) await post('/api/conversations/read', { ConversationID: chat.ID, ThroughSequence: chat.LastSequence }, false);
  await page.getByRole('switch', { name: 'Desktop notifications', exact: true }).uncheck();
  await badge.check();
  await page.evaluate(() => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'hidden' });
    document.dispatchEvent(new Event('visibilitychange'));
  });
  const origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? '18882'}`;
  const resource = await post('/api/resources', { Name: `Badge ${Date.now()}`, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: 'badge-fixture', ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  const assistant = await post('/api/assistants', { Name: 'Badge assistant', ModelID: models.find((m: any) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  await expect.poll(count).toBe(1);
  const chat = await (await page.request.get(`/api/conversations/${assistant.ConversationID}`)).json();
  await post('/api/conversations/read', { ConversationID: chat.ID, ThroughSequence: chat.LastSequence }, false);
  await expect.poll(count).toBe(0);
  await post('/api/conversations/messages', { ConversationID: chat.ID, Content: { Kind: 'text', Text: 'Reply for badge verification' } });
  await expect.poll(count).toBe(1);
  await page.evaluate(() => {
    delete (document as any).visibilityState;
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await badge.uncheck(); await expect.poll(count).toBe(0);
  await badge.check(); await expect.poll(count).toBe(1);
  await page.reload(); await expect(badge).toBeChecked(); await expect.poll(count).toBe(1);
  await expect(page.getByRole('switch', { name: 'Notification alerts', exact: true })).not.toBeChecked();
  await expect(page.getByRole('switch', { name: 'Desktop notifications', exact: true })).not.toBeChecked();
  await page.evaluate(() => { (window as any).notificationFixture.badgeOK = false; });
  await badge.uncheck(); await badge.check();
  await expect(page.getByRole('region', { name: 'App icon unread count', exact: true }).getByText(/Could not update/)).toBeVisible();
  await page.evaluate(() => { (window as any).notificationFixture.badgeOK = true; });
  await badge.uncheck(); await badge.check();
  await expect(page.getByText(/Could not update/)).toHaveCount(0);
  await page.evaluate(() => { location.hash = '#/account/security'; });
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
  await expect.poll(count).toBe(0);
  expect(await page.evaluate(() => (window as any).notificationFixture.ended)).toBeGreaterThan(0);
});


test('badge shares foreground queries and hands off without stale counts or duplicate polling', async ({ page }) => {
  const items = (unread: number) => ({ Items: [
    { ID: 'unread', Archived: false, UnreadCount: unread, LastSequence: 10, LastReadSequence: 0 },
    { ID: 'archived', Archived: true, UnreadCount: 99, LastSequence: 99, LastReadSequence: 0 },
    { ID: 'negative', Archived: false, UnreadCount: -1, LastSequence: 0, LastReadSequence: 0 },
  ], NextCursor: '' });
  let requests = 0, hold = false;
  const pending: import('@playwright/test').Route[] = [];
  await page.route(url => url.pathname === '/api/conversations', async route => {
    requests++;
    if (hold) pending.push(route);
    else await route.fulfill({ json: items(2) });
  });
  await desktop(page);
  const badge = page.getByRole('switch', { name: 'App icon unread count', exact: true });
  const count = () => page.evaluate(() => (window as any).notificationFixture.badge);
  await expect(page.locator('#nav-unread-count')).toHaveText('2');
  const beforeEnable = requests;
  await badge.check();
  await expect.poll(count).toBe(2);
  expect(requests).toBe(beforeEnable);
  await page.clock.install();
  await page.clock.pauseAt(new Date());
  hold = true;
  async function visibility(value: 'visible' | 'hidden') {
    await page.evaluate(value => {
      Object.defineProperty(document, 'visibilityState', { configurable: true, value });
      document.dispatchEvent(new Event('visibilitychange'));
    }, value);
  }
  async function next() {
    await expect.poll(() => pending.length).toBe(1);
    return pending.shift()!;
  }
  await visibility('hidden');
  await (await next()).fulfill({ json: items(7) });
  await expect.poll(count).toBe(7);
  await page.clock.runFor(30_000);
  await (await next()).fulfill({ json: items(8) });
  await expect.poll(count).toBe(8);

  const beforeResume = requests;
  await visibility('visible');
  const resumed = await next();
  expect(requests).toBe(beforeResume + 1);
  expect(await count()).toBe(8); // The shared cache still contains 2.
  await resumed.fulfill({ json: items(2) }); // Structurally unchanged shared data still reconciles.
  await expect.poll(count).toBe(2);
  await page.clock.runFor(30_000);
  const foreground = await next();
  expect(requests).toBe(beforeResume + 2);
  await foreground.fulfill({ status: 503, json: { Error: 'unavailable' } });
  expect(await count()).toBe(2);
  await page.clock.runFor(2000);
  await (await next()).fulfill({ json: items(4) });
  await expect.poll(count).toBe(4);
  await expect(page.locator('#nav-unread-count')).toHaveText('4');

  await visibility('hidden');
  const stale = await next();
  await visibility('visible');
  await (await next()).fulfill({ json: items(3) });
  await expect.poll(count).toBe(3);
  await stale.fulfill({ json: items(9) });
  expect(await count()).toBe(3);
  expect(await page.evaluate(() => (window as any).notificationFixture.badges)).not.toContain(9);

  await visibility('hidden');
  const disabled = await next();
  await badge.uncheck();
  await expect.poll(count).toBe(0);
  await disabled.fulfill({ json: items(6) });
  const beforeDisabled = requests;
  await page.clock.runFor(60_000);
  expect(requests).toBe(beforeDisabled);
  expect(await count()).toBe(0);
});
