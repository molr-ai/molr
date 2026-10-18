import { test, expect } from '@playwright/test';

const password = process.env.MOLR_CSRF_TEST_PASSWORD ?? 'webui-e2e-password-2026!';

test('password verification ignores stale transport cookies before and after reload', async ({ page, context, baseURL }) => {
  const port = new URL(baseURL!).port;
  // Non-current port and unscoped cookies must never select another identity.
  await context.addCookies([
    { name: 'molr_dev_csrf', value: 'synthetic-old-unscoped', url: baseURL! },
    { name: 'molr_lan_csrf_p1', value: 'synthetic-other-port', url: baseURL! },
  ]);
  await page.goto('/#/account/security');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Change password', exact: true })).toBeVisible();
  const me = await page.request.get('/api/auth/me');
  const cookieName = me.headers()['x-molr-csrf-cookie'];
  expect(cookieName).toMatch(new RegExp(`_p${port}$`));
  const staleName = cookieName.startsWith('molr_lan_') ? `molr_dev_csrf_p${port}` : `molr_lan_csrf_p${port}`;
  await context.addCookies([{ name: staleName, value: 'synthetic-old-transport', url: baseURL! }]);
  await page.reload();
  await expect(page.getByRole('button', { name: 'Change password', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Change password', exact: true }).click();
  const editor = page.getByRole('dialog', { name: 'Change password', exact: true });
  // Reapplying the fixture password still exercises persistence and revocation,
  // while leaving the shared test account usable by later tests.
  await editor.getByLabel('New password', { exact: true }).fill(password);
  await editor.getByRole('button', { name: 'Save', exact: true }).click();
  const verification = page.getByRole('dialog').filter({ has: page.getByLabel('Runtime sign-in password', { exact: true }) });
  await verification.getByLabel('Runtime sign-in password', { exact: true }).fill(password);
  const verified = page.waitForResponse(response => response.url().endsWith('/api/auth/reauthenticate') && response.request().method() === 'POST');
  const changed = page.waitForResponse(response => response.url().endsWith('/api/auth/password') && response.request().method() === 'POST');
  await verification.getByRole('button', { name: 'Verify', exact: true }).click();
  expect((await verified).status()).toBe(200);
  expect((await changed).status()).toBe(200);
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
  expect((await page.request.get('/api/auth/me')).status()).toBe(401);
  expect((await context.cookies()).some(cookie => cookie.name === staleName)).toBe(true);
});
