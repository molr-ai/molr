import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';

const firstPort = Number(process.env.MOLR_TEST_PORT ?? process.env.MOLR_E2E_PORT ?? 5193);
const secondPort = Number(process.env.MOLR_TEST_SECOND_PORT ?? firstPort + 1);
const firstOrigin = `http://127.0.0.1:${firstPort}`;
const secondOrigin = `http://127.0.0.1:${secondPort}`;

async function signIn(page: Page, username: string, password: string) {
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill(username);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
}

test('same browser keeps independent Runtime sessions through login, writes, reload and logout', async ({ context, page }) => {
  // Legacy cookies must neither authenticate this proxy nor be cleared by it.
  await context.addCookies([
    { name: 'molr_dev_session', value: 'legacy-session', url: firstOrigin, httpOnly: true },
    { name: 'molr_dev_csrf', value: 'legacy-proof', url: firstOrigin },
  ]);
  await expect.poll(async () => (await page.request.get(`${secondOrigin}/api/auth/me`)).status()).toBe(401);
  const second = await context.newPage();
  await page.goto(firstOrigin);
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
  const proof = await (await page.request.get(`${firstOrigin}/api/auth/csrf`)).json();
  const preLogin = (await context.cookies()).find(cookie => cookie.name === `molr_dev_login_p${firstPort}`)?.value;
  expect(preLogin).toBeTruthy();
  await second.goto(secondOrigin);
  await signIn(second, 'second-owner', 'second-webui-e2e-password-2026!');
  expect((await context.cookies()).find(cookie => cookie.name === `molr_dev_login_p${firstPort}`)?.value).toBe(preLogin);
  const login = await page.request.post(`${firstOrigin}/api/auth/login`, {
    headers: { Origin: firstOrigin, 'X-CSRF-Token': proof.csrf_token },
    data: { username: 'owner', password: 'webui-e2e-password-2026!', device_name: 'Isolation test' },
  });
  expect(login.status(), await login.text()).toBe(200);

  for (const tab of [page, second]) {
    await tab.reload();
    await expect(tab.getByRole('navigation', { name: 'Molr' })).toBeVisible();
    // Exercise the actual browser client and its choice of CSRF proof with
    // both instances' cookies and stale legacy cookies in the same cookie jar.
    const status = await tab.evaluate(async firstPort => {
      const clientPath = '/@id/@molr/sdk';
      const { api } = await import(clientPath);
      return api.post('/api/auth/reauthenticate', {
        password: location.port === String(firstPort) ? 'webui-e2e-password-2026!' : 'second-webui-e2e-password-2026!',
        purpose: 'change_password',
      });
    }, firstPort);
    expect(status).toBeTruthy();
  }
  const sameInstance = await context.newPage();
  await sameInstance.goto(firstOrigin);
  await expect(sameInstance.getByRole('navigation', { name: 'Molr' })).toBeVisible();

  await second.getByRole('button', { name: /^User settings(?: ·|$)/ }).click();
  await second.locator('.settings-nav > button').filter({ hasText: /^Security$/ }).click();
  await second.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(second.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
  expect((await second.request.get(`${secondOrigin}/api/auth/me`)).status()).toBe(401);
  for (const tab of [page, sameInstance]) {
    await tab.reload();
    await expect(tab.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  }
  const cookies = await context.cookies();
  expect(cookies.find(cookie => cookie.name === `molr_dev_session_p${firstPort}`)?.httpOnly).toBe(true);
  expect(cookies.some(cookie => cookie.name === `molr_dev_session_p${secondPort}`)).toBe(false);
  expect(cookies.find(cookie => cookie.name === 'molr_dev_session')?.value).toBe('legacy-session');
});
