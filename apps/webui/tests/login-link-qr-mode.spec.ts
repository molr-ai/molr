import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';

async function openPreview(page: Page) {
  const now = Date.now() + 60_000;
  let previews = 0, redemptions = 0;
  await page.clock.install({ time: new Date(now - 60_000) });
  await page.addInitScript(() => {
    const timers = new Set<number>();
    (window as unknown as { loginExpiryTimers: Set<number> }).loginExpiryTimers = timers;
    const schedule = window.setTimeout.bind(window), cancel = window.clearTimeout.bind(window);
    Object.defineProperty(window, 'setTimeout', { configurable: true, value: (handler: TimerHandler, timeout?: number, ...args: unknown[]) => {
      const id = schedule(handler, timeout, ...args);
      if (typeof handler === 'function' && handler.name === 'syncExpiry') timers.add(id);
      return id;
    } });
    Object.defineProperty(window, 'clearTimeout', { configurable: true, value: (id?: number) => { if (id !== undefined) timers.delete(id); cancel(id); } });
  });
  await page.route('**/api/auth/login-links/preview', route => {
    previews++;
    return route.fulfill({ json: { expires_at: new Date(now + 30_000).toISOString() } });
  });
  await page.route('**/api/auth/login-links/redeem', route => {
    redemptions++;
    return route.fulfill({ status: 401, json: { code: 'unauthenticated' } });
  });
  await page.goto(`/#/login?token=${'q'.repeat(48)}`);
  await expect(page.getByRole('tab', { name: 'This device', exact: true })).toBeVisible();
  await page.clock.pauseAt(new Date(now));
  await page.getByRole('tab', { name: 'QR code', exact: true }).click();
  return { counts: () => ({ previews, redemptions }) };
}
const timerCount = (page: Page) => page.evaluate(() => (window as unknown as { loginExpiryTimers: Set<number> }).loginExpiryTimers.size);

test('QR confirmation uses one deadline timer with no polling and expires without redeeming', async ({ page }) => {
  const state = await openPreview(page);
  expect(await timerCount(page)).toBe(1);
  await page.clock.runFor(29_999);
  await expect(page.getByRole('img', { name: 'Sign-in QR code', exact: true })).toBeVisible();
  expect(state.counts()).toEqual({ previews: 1, redemptions: 0 });
  await page.clock.runFor(1);
  await expect(page.getByRole('img', { name: 'Sign-in QR code', exact: true })).toHaveCount(0);
  await expect(page.getByRole('status')).toContainText('invalid, expired, used, or revoked');
  await expect(page.getByRole('button', { name: 'Sign in with password', exact: true })).toBeVisible();
  expect(await timerCount(page)).toBe(0);
  await page.clock.fastForward(3_600_000);
  expect(state.counts()).toEqual({ previews: 1, redemptions: 0 });
});

test('hidden QR confirmation pauses its timer and rechecks expiry on visibility', async ({ page }) => {
  const state = await openPreview(page);
  await page.evaluate(() => { Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' }); document.dispatchEvent(new Event('visibilitychange')); });
  expect(await timerCount(page)).toBe(0);
  await page.clock.runFor(30_001);
  expect(state.counts()).toEqual({ previews: 1, redemptions: 0 });
  await page.evaluate(() => { Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' }); document.dispatchEvent(new Event('visibilitychange')); });
  await expect(page.getByRole('img', { name: 'Sign-in QR code', exact: true })).toHaveCount(0);
  await expect(page.getByRole('status')).toContainText('invalid, expired, used, or revoked');
  expect(await timerCount(page)).toBe(0);
});

test('leaving QR confirmation removes the credential display and deadline timer', async ({ page }) => {
  const state = await openPreview(page);
  await expect(page.getByRole('button', { name: 'Sign in with password', exact: true })).toHaveCount(0);
  await page.getByRole('tab', { name: 'This device', exact: true }).click();
  await page.getByRole('button', { name: 'Sign in with password', exact: true }).click();
  await expect(page.getByLabel('Password', { exact: true })).toBeVisible();
  await expect(page.getByRole('img', { name: 'Sign-in QR code', exact: true })).toHaveCount(0);
  expect(await timerCount(page)).toBe(0);
  await page.clock.fastForward(3_600_000);
  expect(state.counts()).toEqual({ previews: 1, redemptions: 0 });
  await expect(page.getByLabel('Password', { exact: true })).toBeVisible();
});
