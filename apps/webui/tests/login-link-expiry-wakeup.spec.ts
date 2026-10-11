import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';

test('expired login links do not repeatedly format unchanged expiry dates while idle', async ({ page }) => {
  const expiresAt = new Date(Date.now() - 3600_000).toISOString();
  await page.clock.install({ time: new Date() });
  await page.addInitScript(({ expiry }) => {
    const state = window as unknown as { expiryFormats: number };
    state.expiryFormats = 0;
    const original = Date.prototype.toLocaleString;
    Date.prototype.toLocaleString = function (...args: Parameters<typeof original>) {
      if (this.getTime() === expiry) state.expiryFormats++;
      return original.apply(this, args);
    };
  }, { expiry: Date.parse(expiresAt) });
  await page.route(url => url.pathname === '/api/auth/login-links', route => route.fulfill({ json: {
    Items: Array.from({ length: 100 }, (_, index) => ({
      ID: `expired-link-${index}`, Name: `Expired link ${index}`, TargetPath: '/assistants',
      CreatedAt: new Date(Date.parse(expiresAt) - 3600_000).toISOString(), ExpiresAt: expiresAt,
      UsedAt: null, RevokedAt: null,
    })), NextCursor: '',
  } }));
  await page.goto('/#/account/security');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  const region = page.getByRole('region', { name: 'Login links', exact: true });
  await expect(region.locator('.settings-row-item')).toHaveCount(100);
  const initial = await page.evaluate(() => (window as unknown as { expiryFormats: number }).expiryFormats);
  expect(initial).toBeGreaterThanOrEqual(100);
  await page.evaluate(() => { (window as unknown as { expiryFormats: number }).expiryFormats = 0; });
  await page.clock.runFor(5000);
  const idleFormats = await page.evaluate(() => (window as unknown as { expiryFormats: number }).expiryFormats);
  console.log(JSON.stringify({ links: 100, idleMilliseconds: 5000, idleFormats }));
  expect(idleFormats).toBe(0);
});

type LinkFixture = { ID: string; Name: string; TargetPath: string; CreatedAt: string; ExpiresAt: string; UsedAt: string | null; RevokedAt: string | null };
const linkFixture = (id: string, expiry: number, state: Partial<LinkFixture> = {}): LinkFixture => ({ ID: id, Name: id, TargetPath: '/assistants', CreatedAt: new Date(expiry - 60_000).toISOString(), ExpiresAt: new Date(expiry).toISOString(), UsedAt: null, RevokedAt: null, ...state });
async function installClock(page: Page) {
  // Leave time for session restoration before pausing at the fixture clock.
  const now = Date.now() + 60_000;
  await page.clock.install({ time: new Date(now - 60_000) });
  return now;
}
async function openSecurity(page: Page) {
  await page.goto('/#/account/security');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Login links', exact: true })).toBeVisible();
}
const linkRow = (page: Page, name: string) => page.locator('.login-links-settings .settings-row-item').filter({ has: page.getByRole('heading', { name, exact: true, includeHidden: true }) });

test('login link deadlines advance pending and used links without waiting for a polling tick', async ({ page }) => {
  const now = await installClock(page);
  const links = [linkFixture('First', now + 10_000), linkFixture('Second', now + 20_000), linkFixture('Used', now + 15_000, { UsedAt: new Date(now).toISOString() }), linkFixture('Revoked', now + 5000, { RevokedAt: new Date(now).toISOString() })];
  await page.route(url => url.pathname === '/api/auth/login-links', route => route.fulfill({ json: { Items: links, NextCursor: '' } }));
  await openSecurity(page);
  await page.clock.pauseAt(new Date(now));
  const first = linkRow(page, 'First'), second = linkRow(page, 'Second'), used = linkRow(page, 'Used');
  await expect(first).toContainText('Ready to use');
  await expect(used).toContainText('Used');
  await page.locator('.login-link-history > summary').click();
  await expect(used.getByRole('button', { name: 'Revoke link' })).toBeVisible();
  await page.clock.runFor(9999);
  await expect(first).toContainText('Ready to use');
  await page.clock.runFor(1);
  await expect(first).toContainText('Expired');
  await expect(first.getByRole('button', { name: 'Revoke link' })).toHaveCount(0);
  await expect(second).toContainText('Ready to use');
  await page.clock.runFor(5000);
  await expect(used).toContainText('Used');
  await expect(used.getByRole('button', { name: 'Revoke link' })).toHaveCount(0);
  await page.clock.runFor(5000);
  await expect(second).toContainText('Expired');
  await expect(linkRow(page, 'Revoked')).toContainText('Revoked');
});

test('login link expiry resumes after a hidden page crosses the deadline', async ({ page }) => {
  const now = await installClock(page);
  await page.route(url => url.pathname === '/api/auth/login-links', route => route.fulfill({ json: { Items: [linkFixture('Sleeping', now + 10_000)], NextCursor: '' } }));
  await openSecurity(page);
  await page.clock.pauseAt(new Date(now));
  const row = linkRow(page, 'Sleeping');
  await expect(row).toContainText('Ready to use');
  await page.evaluate(() => { Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' }); document.dispatchEvent(new Event('visibilitychange')); });
  await page.clock.runFor(15_000);
  await expect(row).toContainText('Ready to use');
  await page.evaluate(() => { Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' }); document.dispatchEvent(new Event('visibilitychange')); });
  await expect(row).toContainText('Expired');
  await expect(row.getByRole('button', { name: 'Revoke link' })).toHaveCount(0);
});

test('newly created and revoked links reschedule expiry without remounting the page', async ({ page }) => {
  const now = await installClock(page);
  const links: LinkFixture[] = [];
  await page.route(url => url.pathname === '/api/auth/login-links', async route => {
    if (route.request().method() === 'POST') {
      const name = route.request().postDataJSON().name;
      const link = linkFixture(name, now + 10_000);
      links.push(link);
      await route.fulfill({ json: { link, value: 'synthetic-unused-link-token' } });
    } else await route.fulfill({ json: { Items: links, NextCursor: '' } });
  });
  await page.route(url => url.pathname === '/api/auth/login-links/Revoked-new', async route => {
    links.find(link => link.ID === 'Revoked-new')!.RevokedAt = new Date(now).toISOString();
    await route.fulfill({ json: null });
  });
  await openSecurity(page);
  await page.clock.pauseAt(new Date(now));
  await expect(page.getByText('No active login links', { exact: true })).toBeVisible();
  for (const name of ['Created', 'Revoked-new']) {
    await page.getByRole('button', { name: 'Create login link', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Create login link', exact: true });
    await dialog.getByRole('textbox', { name: 'Name', exact: true }).fill(name);
    await dialog.getByRole('button', { name: 'Save', exact: true }).click();
    const verification = page.getByRole('dialog', { name: 'Verify identity', exact: true });
    await verification.getByLabel('Runtime sign-in password', { exact: true }).fill('webui-e2e-password-2026!');
    await verification.getByRole('button', { name: 'Verify', exact: true }).click();
    const issued = page.getByRole('dialog', { name: 'Login link created', exact: true });
    await issued.getByRole('button', { name: 'Close', exact: true }).first().click();
    await expect(linkRow(page, name)).toContainText('Ready to use');
    // The paused clock keeps success toasts alive; dismiss them before the next form.
    const toastClose = page.locator('.ui-toasts').getByRole('button', { name: 'Close', exact: true });
    while (await toastClose.count()) await toastClose.first().click();
  }
  await linkRow(page, 'Revoked-new').getByRole('button', { name: 'Revoke link', exact: true }).click();
  await page.getByRole('dialog', { name: 'Revoke link', exact: true }).getByRole('button', { name: 'Confirm', exact: true }).click();
  await page.clock.runFor(100);
  await expect(linkRow(page, 'Revoked-new')).toContainText('Revoked');
  await page.clock.runFor(10_000);
  await expect(linkRow(page, 'Created')).toContainText('Expired');
  await expect(linkRow(page, 'Revoked-new')).toContainText('Revoked');
});
