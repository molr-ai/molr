import { test, expect } from '@playwright/test';
import type { MolrAppAPI } from '@molr/ui-host/app';

async function desktop(page: import('@playwright/test').Page) {
  await page.addInitScript(() => {
    const calls = { permission: 0, shown: 0, closed: 0, fail: false };
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
        begin: async () => {}, requestPermission: async () => { calls.permission++; return true; },
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
    const bridge = { begin: async (scope: string) => { scopes.push(scope); }, requestPermission: async () => true, show: async () => {}, close: async (scope: string) => { closed.push(scope); }, onClick: (callback: (scope: string, route: string) => void) => { callbacks.push(callback); return () => {}; } };
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
});
