import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import { randomBytes } from 'node:crypto';

const base = `http://127.0.0.1:${process.env.MOLR_TEST_PORT ?? '5193'}`;
async function signIn(page: Page) {
  await page.goto('/#/account/security');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Signed-in devices', exact: true })).toBeVisible();
}
async function current(page: Page) {
  const result = await page.request.get('/api/auth/sessions');
  expect(result.ok()).toBe(true);
  return (await result.json()).Items.find((s: { Current: boolean }) => s.Current);
}
async function browserLogin(page: Page) {
  return page.evaluate(async () => {
    const modulePath = '/src/auth-device.ts';
    const { BrowserLogin } = await import(/* @vite-ignore */ modulePath);
    return new BrowserLogin().start('/api/auth/login', { username: 'owner', password: 'webui-e2e-password-2026!' });
  });
}

test('one browser keeps one device through repeated login, logout and other tabs', async ({ page, context }) => {
  const identifiers: string[] = [];
  context.on('request', request => { if (request.url().endsWith('/api/auth/login')) identifiers.push(request.postDataJSON().device_id); });
  await signIn(page);
  const first = await current(page);
  expect(first.ClientType).toBe('web');
  await expect(page.getByRole('region', { name: 'Signed-in devices' })).toContainText(' · Web');
  const oldCookie = (await context.cookies()).find(c => c.name === `molr_dev_session_p${new URL(base).port}`)!;
  const secondTab = await context.newPage();
  await secondTab.goto('/#/account/security');
  await expect(secondTab.getByRole('region', { name: 'Signed-in devices' })).toBeVisible();
  await browserLogin(page);
  await page.reload();
  const second = await current(page);
  expect(second.ID).not.toBe(first.ID);
  expect(second.CreatedAt).toBe(first.CreatedAt);
  expect((await page.request.get('/api/auth/me', { headers: { Cookie: `${oldCookie.name}=${oldCookie.value}` } })).status()).toBe(401);
  await secondTab.reload();
  await expect(secondTab.getByRole('region', { name: 'Signed-in devices' })).toBeVisible();
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(secondTab.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
  await signIn(page);
  const third = await current(page);
  expect(third.CreatedAt).toBe(first.CreatedAt);
  expect(new Set(identifiers).size).toBe(1);
  expect(identifiers[0]).toMatch(/^[a-f0-9]{48}$/);
});

test('native device labels and bulk sign-out preserve the current Web device', async ({ page, playwright }) => {
  await signIn(page);
  const first = await current(page);
  const native = await playwright.request.newContext({ baseURL: base, extraHTTPHeaders: { Origin: base } });
  try {
    const proof = await (await native.get('/api/auth/csrf')).json();
    const login = await native.post('/api/auth/login', {
      headers: { 'X-CSRF-Token': proof.csrf_token },
      data: { username: 'owner', password: 'webui-e2e-password-2026!', device_id: randomBytes(24).toString('hex'), client_type: 'ios', device_name: 'Test iPhone' },
    });
    expect(login.ok(), await login.text()).toBe(true);
    await page.reload();
    await expect(page.getByRole('heading', { name: 'Test iPhone · iOS', exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Sign out all other devices', exact: true }).click();
    await page.getByRole('dialog', { name: 'Sign out all other devices', exact: true }).getByRole('button', { name: 'Confirm', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Test iPhone · iOS', exact: true })).toHaveCount(0);
    expect((await native.get('/api/auth/me')).status()).toBe(401);
    expect((await current(page)).ID).toBe(first.ID);
  } finally { await native.dispose(); }
});

test('first-use device allocation and login leases are shared between tabs on HTTP', async ({ page, context }) => {
  await page.goto('/');
  const other = await context.newPage();
  await other.goto('/');
  const acquire = (page: Page) => page.evaluate(async () => {
    const modulePath = '/src/auth-device.ts';
    const { BrowserLogin } = await import(/* @vite-ignore */ modulePath);
    const login = new BrowserLogin();
    (window as any).deviceTestLogin = login;
    try { await (login as any).acquire(); return 'acquired'; } catch (error) { return (error as any).code; }
  });
  const results = await Promise.all([acquire(page), acquire(other)]);
  expect(results.sort()).toEqual(['acquired', 'login_in_progress']);
  await page.evaluate(() => (window as any).deviceTestLogin.close());
  await other.evaluate(() => (window as any).deviceTestLogin.close());
  await signIn(page);
  const first = await current(page);
  await browserLogin(other);
  expect((await current(other)).CreatedAt).toBe(first.CreatedAt);
});
