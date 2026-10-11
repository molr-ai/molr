import { test, expect, type Page } from '@playwright/test';
import type { UserInfo } from '@molr/sdk';

async function login(page: Page) {
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.locator('.workspace')).toBeVisible();
}
async function account(page: Page): Promise<UserInfo> {
  return (await page.request.get('/api/account')).json();
}
async function save(page: Page, avatar: string | RegExp) {
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect.poll(async () => (await account(page)).Avatar).toMatch(avatar);
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeEnabled();
}

test('account avatars persist presets, uploads and clearing across navigation and sessions', async ({ page, browser }, info) => {
  await login(page);
  await page.goto('/#/account/profile');
  await page.getByRole('button', { name: 'Cat', exact: true }).click();
  await save(page, /^preset:cat$/);
  await page.reload();
  await expect(page.getByRole('button', { name: 'Cat', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await page.goto('/#/assistants');
  const nav = info.project.name === 'mobile' ? '.mobile-account-button' : '.nav-account-actions';
  await expect(page.locator(`${nav} .user-avatar svg`)).toBeVisible();
  const other = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  try {
    await login(other);
    await expect(other.locator('.nav-account-actions .user-avatar svg')).toBeVisible();
  } finally { await other.close(); }
  await page.goto('/#/account/profile');
  const png = await page.evaluate(() => {
    const canvas = document.createElement('canvas'); canvas.width = 400; canvas.height = 240;
    const context = canvas.getContext('2d')!;
    context.fillStyle = getComputedStyle(document.documentElement).getPropertyValue('--color-primary');
    context.fillRect(0, 0, 400, 240); return canvas.toDataURL('image/png').split(',')[1];
  });
  await page.locator('input[type="file"]').setInputFiles({ name: 'avatar.png', mimeType: 'image/png', buffer: Buffer.from(png, 'base64') });
  await expect(page.getByRole('img', { name: 'Avatar preview' }).locator('img')).toBeVisible();
  await save(page, /^data:image\/jpeg;base64,/);
  await page.goto('/#/assistants');
  await expect(page.locator(`${nav} .user-avatar img`)).toBeVisible();
  expect(await page.locator(`${nav} .user-avatar img`).evaluate((image: HTMLImageElement) => [image.naturalWidth, image.naturalHeight])).toEqual([256, 256]);
  await page.goto('/#/account/profile');
  await page.locator('input[type="file"]').setInputFiles({ name: 'invalid.png', mimeType: 'image/png', buffer: Buffer.from('invalid image') });
  await expect(page.getByRole('alert').filter({ hasText: 'Unable to read' })).toBeVisible();
  await expect(page.getByRole('img', { name: 'Avatar preview' }).locator('img')).toBeVisible();
  // A failed save retains the selected draft and leaves the persisted avatar intact.
  await page.getByRole('button', { name: 'Fox', exact: true }).click();
  await page.route('**/api/account/profile', route => route.abort('failed'), { times: 1 });
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.locator('.settings-form .error-notice:visible')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Fox', exact: true })).toHaveAttribute('aria-pressed', 'true');
  expect((await account(page)).Avatar).toMatch(/^data:image\/jpeg;base64,/);
  await save(page, /^preset:fox$/);
  await page.getByRole('button', { name: 'Clear', exact: true }).click();
  await save(page, /^$/);
  await page.reload();
  await expect(page.getByRole('img', { name: 'Avatar preview' })).toHaveText('O');
});

test('saving an avatar updates both tabs without remounting their workspaces', async ({ page, context }, info) => {
  await login(page);
  await page.goto('/#/account/profile');
  const other = await context.newPage();
  await other.goto('/#/assistants');
  await expect(other.locator('.workspace')).toBeVisible();
  const currentWorkspace = await page.locator('.workspace').elementHandle();
  const currentForm = await page.locator('.settings-form').elementHandle();
  const otherWorkspace = await other.locator('.workspace').elementHandle();
  let release!: () => void;
  const pending = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/auth/me', async route => { await pending; await route.continue(); });
  try {
    await page.getByRole('button', { name: 'Cat', exact: true }).click();
    const refreshing = page.waitForRequest(request => new URL(request.url()).pathname === '/api/auth/me');
    const otherUpdated = other.waitForResponse(async response => new URL(response.url()).pathname === '/api/auth/me' && response.ok() && (await response.json()).User?.Avatar === 'preset:cat');
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await refreshing;
    expect(await currentWorkspace!.evaluate(node => node.isConnected)).toBe(true);
    release();
    await otherUpdated;
    const nav = info.project.name === 'mobile' ? '.mobile-account-button' : '.nav-account-actions';
    await expect(other.locator(`${nav} .user-avatar svg`)).toBeVisible();
    await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeEnabled();
    expect(await currentWorkspace!.evaluate(node => node.isConnected)).toBe(true);
    expect(await otherWorkspace!.evaluate(node => node.isConnected)).toBe(true);
    expect(await currentForm!.evaluate(node => node.isConnected)).toBe(true);
    await expect(page).toHaveURL(/#\/account\/profile$/);
  } finally { release(); await other.close(); }
});

test('about logo switches its full animation at the local day and night boundaries', async ({ page }) => {
  await page.clock.install({ time: new Date('2026-10-08T19:59:50+08:00') });
  await login(page);
  await page.goto('/#/account/about');
  const logo = page.locator('.settings-about-brand .logo--interactive');
  await expect(logo).toHaveAttribute('data-ready', 'true');
  await expect(page.locator('.settings-about-brand img')).toHaveAttribute('src', /molr-wordmark(?:-light)?-alive\.svg/);
  await page.clock.fastForward(11_000);
  await expect(page.locator('.settings-about-brand img')).toHaveAttribute('src', /molr-wordmark(?:-light)?-playful\.svg/);
  await expect(logo).toHaveAttribute('data-ready', 'true');
  await page.clock.fastForward(12 * 60 * 60 * 1000);
  await expect(page.locator('.settings-about-brand img')).toHaveAttribute('src', /molr-wordmark(?:-light)?-alive\.svg/);
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await expect(page.locator('.settings-about-brand object')).toHaveCount(0);
  expect(await page.locator('.settings-about-brand img').evaluate((image: HTMLImageElement) => image.currentSrc)).toMatch(/#\/brand\/.*molr-wordmark(?:-light)?\.svg$/);
});
