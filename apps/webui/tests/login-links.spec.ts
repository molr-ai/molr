import { createHmac } from 'node:crypto';
import { test, expect } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';
const base = `http://127.0.0.1:${process.env.MOLR_TEST_PORT ?? '5193'}`;

async function login(page: Page) {
  await page.goto('/#/account/security');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Login links', exact: true })).toBeVisible();
}
const settingsNavItem = (page: Page, name: string) => page.locator('.settings-nav > button').filter({ hasText: new RegExp(`^${name}$`) });
async function createLink(page: Page, name: string, checkIssued?: (issued: Locator, url: string) => Promise<void>) {
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
  await expect(issued.getByRole('tab', { name: 'Link', exact: true })).toHaveAttribute('aria-selected', 'true');
  const link = await issued.getByRole('group', { name: 'Login links', exact: true }).locator('code').innerText();
  await expect(issued.getByRole('textbox')).toHaveCount(0);
  await expect(issued.getByText('Copy or scan before closing; this link is only shown once.', { exact: true })).toBeVisible();
  expect(new URL(link).search).toBe('');
  const qr = issued.getByRole('img', { name: 'Sign-in QR code', exact: true });
  await expect(qr).toHaveCount(0);
  await issued.getByRole('tab', { name: 'QR code', exact: true }).click();
  await expect(qr).toBeVisible();
  await expect(issued.getByRole('button', { name: 'Copy link', exact: true })).toHaveCount(0);
  await expect(qr.locator('rect')).toHaveCSS('fill', 'rgb(255, 255, 255)');
  await expect(qr.locator('path')).toHaveCSS('fill', 'rgb(0, 0, 0)');
  await expect(issued.getByText('Local address only. Use an address reachable from other devices to create a new link.', { exact: true })).toBeVisible();
  expect(await issued.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
  await issued.getByRole('tab', { name: 'Link', exact: true }).click();
  await expect(qr).toHaveCount(0);
  await expect(issued.getByRole('group', { name: 'Login links', exact: true }).locator('code')).toHaveText(link);
  await checkIssued?.(issued, link);
  // The mobile success toast can cover the footer; use the header close control.
  await issued.getByRole('button', { name: 'Close', exact: true }).first().click();
  await expect(qr).toHaveCount(0);
  return link;
}

test('login link text copies with modern and LAN clipboard support and selects the full URL on failure', async ({ page }) => {
  await login(page);
  await createLink(page, `Copy ${Date.now()}`, async (issued, url) => {
    const address = issued.getByRole('group', { name: 'Login links', exact: true });
    await page.evaluate(() => Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async (text: string) => { (window as unknown as { copiedLink: string }).copiedLink = text; } } }));
    await address.getByRole('button', { name: 'Copy link', exact: true }).click();
    await expect(issued.getByRole('status')).toHaveText('Copied');
    expect(await page.evaluate(() => (window as unknown as { copiedLink: string }).copiedLink)).toBe(url);
    await page.evaluate(() => {
      Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined });
      (window as unknown as { copiedLink: string }).copiedLink = '';
      document.execCommand = command => {
        if (command !== 'copy') return false;
        (window as unknown as { copiedLink: string }).copiedLink = (document.activeElement as HTMLTextAreaElement).value;
        return true;
      };
    });
    await address.getByRole('button', { name: /^(Copy link|Copied)$/ }).click();
    expect(await page.evaluate(() => (window as unknown as { copiedLink: string }).copiedLink)).toBe(url);
    await page.evaluate(() => { document.execCommand = () => false; });
    await address.getByRole('button', { name: /^(Copy link|Copied)$/ }).click();
    await expect(issued.getByRole('status')).toHaveText('The link is selected. Copy it to keep it.');
    await expect(address.locator('code')).toBeFocused();
    expect(await page.evaluate(() => window.getSelection()?.toString())).toBe(url);
  });
});

test('copy feedback resets after its deadline and releases timers on tab, visibility and dialog changes', async ({ page }) => {
  const now = Date.now() + 60_000;
  await page.clock.install({ time: new Date(now - 60_000) });
  await page.addInitScript(() => {
    const timers = new Set<number>();
    (window as unknown as { copyFeedbackTimers: Set<number> }).copyFeedbackTimers = timers;
    const schedule = window.setTimeout.bind(window), cancel = window.clearTimeout.bind(window);
    Object.defineProperty(window, 'setTimeout', { configurable: true, value: (handler: TimerHandler, timeout?: number, ...args: unknown[]) => {
      const id = schedule(handler, timeout, ...args);
      if (typeof handler === 'function' && handler.name === 'resetCopied') timers.add(id);
      return id;
    } });
    Object.defineProperty(window, 'clearTimeout', { configurable: true, value: (id?: number) => { if (id !== undefined) timers.delete(id); cancel(id); } });
  });
  const timers = () => page.evaluate(() => (window as unknown as { copyFeedbackTimers: Set<number> }).copyFeedbackTimers.size);
  let requests = 0;
  page.on('request', request => { if (new URL(request.url()).pathname.startsWith('/api/auth/login-links')) requests++; });
  await login(page);
  const name = `Copy timer ${Date.now()}`;
  await createLink(page, name, async issued => {
    await expect(page.locator('.login-links-settings .settings-row-item').filter({ hasText: name })).toHaveCount(1);
    await page.clock.pauseAt(new Date(now));
    await page.evaluate(() => Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async () => {} } }));
    const before = requests;
    const copy = issued.getByRole('button', { name: 'Copy link', exact: true });
    const copied = issued.getByRole('button', { name: 'Copied', exact: true });
    expect(await timers()).toBe(0);
    await page.clock.runFor(5000); expect(requests).toBe(before);
    await copy.click(); await expect(copied).toBeVisible(); expect(await timers()).toBe(1);
    await page.clock.runFor(2499); await expect(copied).toBeVisible();
    await page.clock.runFor(1); await expect(copy).toBeVisible(); expect(await timers()).toBe(0);
    await copy.click(); await page.clock.runFor(1000); await copied.click();
    expect(await timers()).toBe(1);
    await page.clock.runFor(2499); await expect(copied).toBeVisible();
    await page.clock.runFor(1); await expect(copy).toBeVisible();
    await copy.click(); await issued.getByRole('tab', { name: 'QR code', exact: true }).click();
    expect(await timers()).toBe(0);
    await issued.getByRole('tab', { name: 'Link', exact: true }).click(); await expect(copy).toBeVisible();
    expect(requests).toBe(before);
    await copy.click();
    await page.evaluate(() => { Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' }); document.dispatchEvent(new Event('visibilitychange')); });
    await expect(copy).toBeVisible(); expect(await timers()).toBe(0);
    await page.evaluate(() => { Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' }); document.dispatchEvent(new Event('visibilitychange')); });
    await copy.click(); expect(await timers()).toBe(1);
  });
  await page.clock.runFor(500);
  await expect(page.getByRole('dialog', { name: 'Login link created', exact: true })).toHaveCount(0);
  expect(await timers()).toBe(0);
});

test('created login links require confirmation, sign in once, scrub tokens, survive reload and expose usage', async ({ page, browser }) => {
  await login(page);
  const name = `Phone ${Date.now()}`;
  const url = await createLink(page, name);
  const token = new URLSearchParams(new URL(url).hash.split('?')[1]).get('token')!;
  const other = await browser.newContext({ locale: 'en-US' });
  try {
    const next = await other.newPage(); const urls: string[] = []; let exchanges = 0;
    next.on('request', request => { urls.push(request.url()); if (request.url().endsWith('/api/auth/login-links/redeem')) exchanges++; });
    await next.goto(url + '&auto=1');
    await expect(next.getByRole('button', { name: 'Confirm sign-in', exact: true })).toBeVisible();
    await expect(next).toHaveURL(/#\/login$/);
    await expect(next.getByText(/This link can only be used once and is valid until/)).toBeVisible();
    expect(exchanges).toBe(0);
    await page.reload();
    await expect(page.locator('.login-links-settings .settings-row-item').filter({ has: page.getByRole('heading', { name, exact: true, includeHidden: true }) })).toContainText('Ready to use');
    await next.getByRole('button', { name: 'Confirm sign-in', exact: true }).dblclick();
    await expect(settingsNavItem(next, 'Theme')).toHaveAttribute('aria-current', 'page');
    await expect(next).toHaveURL(/#\/account\/theme$/);
    expect(exchanges).toBe(1); expect(urls.some(url => url.includes(token))).toBe(false);
    expect(await next.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))).not.toContain(token);
    await next.reload();
    await expect(settingsNavItem(next, 'Theme')).toHaveAttribute('aria-current', 'page');
    expect(exchanges).toBe(1);
    await page.reload();
    await expect(page.locator('.login-links-settings .settings-row-item').filter({ has: page.getByRole('heading', { name, exact: true, includeHidden: true }) })).toContainText('Used');
  } finally { await other.close(); }
  const fresh = await browser.newContext({ locale: 'en-US' });
  try { const next = await fresh.newPage(); await next.goto(url); await expect(next.getByRole('status')).toContainText('invalid, expired, used, or revoked'); await next.getByRole('button', { name: 'Sign in with password' }).click(); await expect(next.getByLabel('Password', { exact: true })).toBeVisible(); } finally { await fresh.close(); }
});

test('confirmation QR mode leaves redemption to the phone and shares the same single-use link', async ({ page, browser }) => {
  await login(page);
  const url = await createLink(page, `QR transfer ${Date.now()}`);
  const desktop = await browser.newContext({ locale: 'en-US' }), phone = await browser.newContext({ locale: 'en-US', viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });
  try {
    const source = await desktop.newPage(); let previews = 0, redemptions = 0;
    source.on('request', request => { if (request.url().endsWith('/api/auth/login-links/preview')) previews++; if (request.url().endsWith('/api/auth/login-links/redeem')) redemptions++; });
    await source.goto(url);
    await source.getByRole('tab', { name: 'QR code', exact: true }).click();
    await expect(source.getByRole('img', { name: 'Sign-in QR code', exact: true })).toBeVisible();
    await expect(source.getByRole('button', { name: 'Sign in with password', exact: true })).toHaveCount(0);
    await expect(source.getByRole('button', { name: 'Confirm sign-in', exact: true })).toBeHidden();
    await source.getByRole('tab', { name: 'This device', exact: true }).click();
    await expect(source.getByRole('button', { name: 'Sign in with password', exact: true })).toBeVisible();
    await expect(source.getByRole('img', { name: 'Sign-in QR code', exact: true })).toHaveCount(0);
    await source.getByRole('tab', { name: 'QR code', exact: true }).click();
    expect(previews).toBe(1); expect(redemptions).toBe(0);
    expect(await source.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))).not.toContain(new URL(url).hash.split('token=')[1]);
    const target = await phone.newPage(); await target.goto(url);
    await target.getByRole('button', { name: 'Confirm sign-in', exact: true }).click();
    await expect(target).toHaveURL(/#\/account\/theme$/);
    expect(redemptions).toBe(0);
    await source.getByRole('tab', { name: 'This device', exact: true }).click();
    await source.getByRole('button', { name: 'Confirm sign-in', exact: true }).click();
    await expect(source.getByRole('status')).toContainText('invalid, expired, used, or revoked');
    await expect(source.getByRole('tablist')).toHaveCount(0);
  } finally { await desktop.close(); await phone.close(); }
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
    const card = page.locator('.login-links-settings .settings-row-item').filter({ has: page.getByRole('heading', { name, exact: true, includeHidden: true }) });
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
    await next.getByLabel('Authenticator or recovery code', { exact: true }).fill('invalid-recovery-code');
    await next.getByRole('button', { name: 'Verify', exact: true }).click();
    await expect(next.getByText('Invalid authenticator or recovery code. Please try again.', { exact: true })).toBeVisible();
    await next.getByLabel('Authenticator or recovery code', { exact: true }).fill(codes.recovery_codes[0]);
    await next.getByRole('button', { name: 'Verify', exact: true }).click();
    await expect(settingsNavItem(next, 'Theme')).toHaveAttribute('aria-current', 'page');
    for (const remember of [false, true]) {
      const passwordContext = await browser.newContext({ locale: 'en-US' });
      try {
        const passwordPage = await passwordContext.newPage();
        await passwordPage.goto(base);
        await passwordPage.getByLabel('Username', { exact: true }).fill('owner');
        await passwordPage.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
        await passwordPage.getByRole('checkbox', { name: 'Keep me signed in', exact: true }).setChecked(remember);
        await passwordPage.getByRole('button', { name: 'Sign in', exact: true }).click();
        await passwordPage.getByLabel('Authenticator or recovery code', { exact: true }).fill('invalid-recovery-code');
        await passwordPage.getByRole('button', { name: 'Verify', exact: true }).click();
        await expect(passwordPage.getByText('Invalid authenticator or recovery code. Please try again.', { exact: true })).toBeVisible();
        await passwordPage.getByLabel('Authenticator or recovery code', { exact: true }).fill(codes.recovery_codes[remember ? 3 : 2]);
        await passwordPage.getByRole('button', { name: 'Verify', exact: true }).click();
        await expect(passwordPage.getByRole('navigation', { name: 'Molr' })).toBeVisible();
        const cookies = (await passwordContext.cookies()).filter(cookie => /^molr_dev_(session|csrf)_p/.test(cookie.name));
        expect(cookies).toHaveLength(2);
        for (const cookie of cookies) expect(cookie.expires > 0).toBe(remember);
      } finally { await passwordContext.close(); }
    }
  } finally {
    await other.close();
    const proof = await post('/api/auth/reauthenticate', { password: 'webui-e2e-password-2026!', mfa_code: codes.recovery_codes[1], purpose: 'mfa.disable' });
    await post('/api/auth/mfa/disable', { verification: proof.verification });
  }
});
