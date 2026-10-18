import { test, expect } from '@playwright/test';

for (const locale of ['zh-CN', 'en-US', 'fr-FR']) {
  test.describe(locale, () => {
    test.use({ locale });
    test('login follows browser language without a saved preference', async ({ page }) => {
      await page.goto('/');
      await expect(page.getByRole('button', { name: locale.startsWith('zh') ? '登录' : 'Sign in', exact: true })).toBeVisible();
      await expect(page.locator('.login-card').getByRole('combobox')).toHaveCount(0);
      await expect(page.getByText(/使用服务器初始化|Use the account created during/)).toHaveCount(0);
    });
  });
}

test('saved language cookie takes priority and persists after sign-out', async ({ page, context, baseURL }) => {
  const name = `molr_locale_p${new URL(baseURL!).port}`;
  await context.addCookies([{ name, value: 'zh', url: baseURL! }]);
  await page.goto('/#/account/general');
  await expect(page.getByRole('button', { name: '登录', exact: true })).toBeVisible();
  await page.getByLabel('用户名', { exact: true }).fill('owner');
  await page.getByLabel('密码', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await page.getByRole('combobox', { name: 'Language', exact: true }).click();
  await page.getByRole('option', { name: '简体中文', exact: true }).click();
  await expect.poll(async () => (await context.cookies()).find(cookie => cookie.name === name)?.value).toBe('zh');
  const cookie = (await context.cookies()).find(cookie => cookie.name === name)!;
  expect(cookie.expires).toBeGreaterThan(Date.now() / 1000 + 86400);
  expect(cookie.sameSite).toBe('Lax');
  await page.goto('/#/account/security');
  await page.getByRole('button', { name: '退出登录', exact: true }).click();
  await page.reload();
  await expect(page.getByRole('button', { name: '登录', exact: true })).toBeVisible();
  // Restore the fixture account's language for the remaining tests.
  await page.getByLabel('用户名', { exact: true }).fill('owner');
  await page.getByLabel('密码', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await page.goto('/#/account/general');
  await page.getByRole('combobox', { name: '语言', exact: true }).click();
  await page.getByRole('option', { name: 'English', exact: true }).click();
  await expect.poll(async () => (await context.cookies()).find(cookie => cookie.name === name)?.value).toBe('en');
});

for (const [seconds, initial, next] of [[3660, '1小时1分钟0秒', '1小时0分钟59秒'], [60, '1分钟0秒', '59秒'], [1, '1秒', '']] as const) {
  test(`rate-limit countdown formats ${seconds} seconds and updates`, async ({ page }) => {
    await page.addInitScript(() => Object.defineProperty(navigator, 'language', { value: 'zh-CN' }));
    await page.clock.install({ time: new Date('2026-01-01T00:00:00Z') });
    await page.clock.pauseAt(new Date('2026-01-01T00:00:01Z'));
    await page.route('**/api/auth/login', route => route.fulfill({ status: 429, headers: { 'Retry-After': String(seconds) }, json: { code: 'rate_limited' } }));
    await page.goto('/');
    await page.getByLabel('用户名', { exact: true }).fill('fixture-user');
    await page.getByLabel('密码', { exact: true }).fill('fixture-password');
    await page.getByRole('button', { name: '登录', exact: true }).click();
    const toast = page.locator('.ui-toast--warning');
    await expect(toast).toContainText(`操作过于频繁，请在${initial}后重试。`);
    await page.clock.runFor(1000);
    await expect(toast).toContainText(next ? `操作过于频繁，请在${next}后重试。` : '等待时间已结束，可以重试。');
    await toast.getByRole('button', { name: '关闭', exact: true }).click();
    await page.clock.runFor(10000);
    await expect(toast).toHaveCount(0);
  });
}
