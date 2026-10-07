import { createHmac } from 'node:crypto';
import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
const base = `http://127.0.0.1:${process.env.MOLR_TEST_PORT ?? '5193'}`;

async function login(page: Page) {
  await page.goto('/#/account/security');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Login links', exact: true })).toBeVisible();
}
const settingsNavItem = (page: Page, name: string) => page.locator('.settings-nav > button').filter({ hasText: new RegExp(`^${name}$`) });
async function createLink(page: Page, name: string) {
  await page.getByRole('button', { name: 'Create login link', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Create login link', exact: true });
  await dialog.getByRole('textbox', { name: 'Name', exact: true }).fill(name);
  await dialog.getByRole('combobox', { name: 'Open after sign-in', exact: true }).click();
  await page.getByRole('option', { name: 'Theme', exact: true }).click();
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  const verification = page.getByRole('dialog', { name: 'Verify identity', exact: true });
  await verification.getByLabel('Runtime sign-in password', { exact: true }).fill('webui-e2e-password-2026!');
  await verification.getByRole('button', { name: 'Verify', exact: true }).click();
  const issued = page.getByRole('dialog', { name: 'Login link created', exact: true });
  const link = await issued.getByRole('textbox', { name: 'Login links', exact: true }).inputValue();
  expect(new URL(link).search).toBe('');
  await issued.getByRole('button', { name: 'Close', exact: true }).filter({ hasText: /^Close$/ }).click();
  return link;
}

test('created login links require confirmation, sign in once, scrub tokens, survive reload and expose usage', async ({ page, browser }) => {
  await login(page);
  const name = `Phone ${Date.now()}`;
  const url = await createLink(page, name);
  const token = new URLSearchParams(new URL(url).hash.split('?')[1]).get('token')!;
  const other = await browser.newContext({ locale: 'en-US' });
  try {
    const next = await other.newPage(); const urls: string[] = []; let exchanges = 0;
    next.on('request', request => { urls.push(request.url()); if (request.url().endsWith('/api/auth/login-links/redeem')) exchanges++; });
    await next.goto(url);
    await expect(next.getByRole('button', { name: 'Confirm sign-in', exact: true })).toBeVisible();
    await expect(next).toHaveURL(/#\/login$/);
    await expect(next.getByText(/This link can only be used once and is valid until/)).toBeVisible();
    expect(exchanges).toBe(0);
    await page.reload();
    await expect(page.locator('.login-links-settings .settings-row-item').filter({ has: page.getByRole('heading', { name, exact: true }) })).toContainText('Ready to use');
    await next.getByRole('button', { name: 'Confirm sign-in', exact: true }).dblclick();
    await expect(settingsNavItem(next, 'Theme')).toHaveAttribute('aria-current', 'page');
    await expect(next).toHaveURL(/#\/account\/theme$/);
    expect(exchanges).toBe(1); expect(urls.some(url => url.includes(token))).toBe(false);
    expect(await next.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))).not.toContain(token);
    await next.reload();
    await expect(settingsNavItem(next, 'Theme')).toHaveAttribute('aria-current', 'page');
    expect(exchanges).toBe(1);
    await page.reload();
    await expect(page.locator('.login-links-settings .settings-row-item').filter({ has: page.getByRole('heading', { name, exact: true }) })).toContainText('Used');
  } finally { await other.close(); }
  const fresh = await browser.newContext({ locale: 'en-US' });
  try { const next = await fresh.newPage(); await next.goto(url); await expect(next.getByRole('status')).toContainText('invalid, expired, used, or revoked'); await next.getByRole('button', { name: 'Sign in with password' }).click(); await expect(next.getByLabel('Password', { exact: true })).toBeVisible(); } finally { await fresh.close(); }
});


test('leaving or refreshing confirmation does not consume the original link', async ({ page, browser }) => {
  await login(page);
  const url = await createLink(page, `Preview ${Date.now()}`);
  const other = await browser.newContext({ locale: 'en-US' });
  try {
    let next = await other.newPage();
    let exchanges = 0;
    other.on('request', request => { if (request.url().endsWith('/api/auth/login-links/redeem')) exchanges++; });
    await next.goto(url);
    await next.getByRole('button', { name: 'Sign in with password', exact: true }).click();
    await expect(next.getByLabel('Password', { exact: true })).toBeVisible();
    await next.goto(url);
    await expect(next.getByRole('button', { name: 'Confirm sign-in', exact: true })).toBeVisible();
    await next.reload();
    await expect(next.getByRole('status')).toContainText('invalid, expired, used, or revoked');
    await next.close();
    expect(exchanges).toBe(0);
    next = await other.newPage();
    await next.goto(url);
    await next.getByRole('button', { name: 'Confirm sign-in', exact: true }).click();
    await expect(settingsNavItem(next, 'Theme')).toHaveAttribute('aria-current', 'page');
    expect(exchanges).toBe(1);
  } finally { await other.close(); }
});

test('revoked links and another Runtime cannot authenticate', async ({ page, browser }) => {
  await login(page);
  const name = `Revoke ${Date.now()}`, url = await createLink(page, name);
  const other = await browser.newContext({ locale: 'en-US' });
  try {
    const next = await other.newPage();
    const second = new URL(url); second.port = String(Number(new URL(base).port) + 1);
    await next.goto(second.href);
    await expect(next.getByRole('status')).toContainText('invalid, expired, used, or revoked');
    await next.goto(url);
    await expect(next.getByRole('button', { name: 'Confirm sign-in', exact: true })).toBeVisible();
    const card = page.locator('.login-links-settings .settings-row-item').filter({ has: page.getByRole('heading', { name, exact: true }) });
    await card.getByRole('button', { name: 'Revoke link', exact: true }).click();
    await page.getByRole('dialog', { name: 'Revoke link', exact: true }).getByRole('button', { name: 'Confirm', exact: true }).click();
    await expect(card).toContainText('Revoked');
    await next.getByRole('button', { name: 'Confirm sign-in', exact: true }).click();
    await expect(next.getByRole('status')).toContainText('invalid, expired, used, or revoked');
  } finally { await other.close(); }
});

test('malformed link remains a recoverable login screen', async ({ page }) => {
  await page.goto('/#/login?token=bad&next=https://evil.example');
  await expect(page).toHaveURL(/#\/login$/);
  await expect(page.getByRole('status')).toContainText('invalid, expired, used, or revoked');
  await page.getByRole('button', { name: 'Sign in with password' }).click();
  await expect(page.getByRole('textbox', { name: 'Username', exact: true })).toBeVisible();
});

test('login links still require MFA and finish at the saved destination', async ({ page, browser }) => {
  await login(page);
  const url = await createLink(page, `MFA ${Date.now()}`);
  async function post(path: string, data: object) {
    const token = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${new URL(base).port}`)!.value;
    const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': token }, data });
    expect(response.ok(), await response.text()).toBe(true);
    return response.json();
  }
  const proof = await post('/api/auth/reauthenticate', { password: 'webui-e2e-password-2026!', purpose: 'mfa.enroll' });
  const enrollment = await post('/api/auth/mfa/enroll', { verification: proof.verification });
  const secret = new URL(enrollment.provisioning_uri).searchParams.get('secret')!;
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'; let bits = '';
  for (const character of secret.replace(/=+$/, '')) bits += alphabet.indexOf(character).toString(2).padStart(5, '0');
  const key = Buffer.from(bits.match(/.{8}/g)!.map(byte => parseInt(byte, 2)));
  const time = Buffer.alloc(8); time.writeBigUInt64BE(BigInt(Math.floor(Date.now() / 30000)));
  const hash = createHmac('sha1', key).update(time).digest(); const offset = hash[hash.length - 1] & 15;
  const code = ((hash.readUInt32BE(offset) & 0x7fffffff) % 1000000).toString().padStart(6, '0');
  const codes = await post('/api/auth/mfa/confirm', { enrollment_id: enrollment.id, code });
  const other = await browser.newContext({ locale: 'en-US' });
  try {
    const next = await other.newPage(); await next.goto(url);
    await next.getByRole('button', { name: 'Confirm sign-in', exact: true }).click();
    await expect(next.getByLabel('Authenticator or recovery code', { exact: true })).toBeVisible();
    await expect(next.getByRole('navigation', { name: 'Molr' })).toHaveCount(0);
    await next.getByLabel('Authenticator or recovery code', { exact: true }).fill(codes.recovery_codes[0]);
    await next.getByRole('button', { name: 'Verify', exact: true }).click();
    await expect(settingsNavItem(next, 'Theme')).toHaveAttribute('aria-current', 'page');
  } finally {
    await other.close();
    const proof = await post('/api/auth/reauthenticate', { password: 'webui-e2e-password-2026!', mfa_code: codes.recovery_codes[1], purpose: 'mfa.disable' });
    await post('/api/auth/mfa/disable', { verification: proof.verification });
  }
});
