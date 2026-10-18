import { test, expect } from '@playwright/test';

for (const locale of ['en-US', 'zh-CN']) {
  test(`incorrect credentials can be corrected without a session-expiry message (${locale})`, async ({ page }) => {
    await page.addInitScript(locale => { Object.defineProperty(navigator, 'language', { configurable: true, value: locale }); }, locale);
    await page.goto('/');
    const chinese = locale === 'zh-CN';
    const username = page.getByLabel(chinese ? '用户名' : 'Username', { exact: true });
    const password = page.getByLabel(chinese ? '密码' : 'Password', { exact: true });
    const submit = page.getByRole('button', { name: chinese ? '登录' : 'Sign in', exact: true });
    const message = chinese ? '用户名或密码不正确，请重新输入。' : 'Incorrect username or password. Please try again.';
    for (const account of ['nonexistent-login-error-user', 'owner']) {
      await username.fill(account);
      await password.fill('incorrect-password');
      await submit.click();
      await expect(page.getByText(message, { exact: true })).toBeVisible();
      await expect(username).toHaveValue(account);
      await expect(page.getByText(chinese ? '登录信息已失效，请重新登录。' : 'Your session has expired. Sign in again.', { exact: true })).toHaveCount(0);
    }
    await password.fill('webui-e2e-password-2026!');
    await submit.click();
    await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
    await expect(page.getByText(message, { exact: true })).toHaveCount(0);
  });
}

for (const entry of ['password', 'password-mfa', 'link-mfa']) {
  for (const code of ['rate_limited', 'unavailable']) {
    test(`${entry} shows ${code} again after dismissal and expiry`, async ({ page }) => {
      await page.clock.install();
      const response = (value: object) => ({ contentType: 'application/json', body: JSON.stringify(value) });
      const mfa = entry !== 'password';
      if (entry === 'link-mfa') {
        await page.route('**/api/auth/login-links/preview', route => route.fulfill(response({ expires_at: new Date(Date.now() + 10800000).toISOString() })));
        await page.route('**/api/auth/login-links/redeem', route => route.fulfill(response({ challenge: 'fixture-challenge', target_path: '/assistants' })));
      } else if (mfa) {
        await page.route('**/api/auth/login', route => route.fulfill(response({ challenge: 'fixture-challenge' })));
      }
      let requests = 0;
      await page.route(mfa ? '**/api/auth/mfa/complete' : '**/api/auth/login', async route => {
        requests++;
        await route.fulfill({ ...response({ code, request_id: `attempt-${requests}` }), status: code === 'rate_limited' ? 429 : 503 });
      });
      await page.goto(entry === 'link-mfa' ? `/#/login?token=${'a'.repeat(48)}` : '/');
      if (entry === 'link-mfa') {
        await page.getByRole('button', { name: 'Confirm sign-in', exact: true }).click();
      } else {
        await page.getByLabel('Username', { exact: true }).fill('owner');
        await page.getByLabel('Password', { exact: true }).fill('fixture-password');
        if (mfa) await page.getByRole('button', { name: 'Sign in', exact: true }).click();
      }
      const submit = page.getByRole('button', { name: mfa ? 'Verify' : 'Sign in', exact: true });
      const toast = page.locator(code === 'rate_limited' ? '.ui-toast--warning' : '.ui-toast--danger');
      const message = code === 'rate_limited' ? 'Too many requests. Try again shortly.' : 'The service is unavailable. Try again shortly.';
      for (let attempt = 1; attempt <= 3; attempt++) {
        if (mfa) await page.getByLabel('Authenticator or recovery code', { exact: true }).fill('fixture-code');
        await submit.click();
        await expect.poll(() => requests).toBe(attempt);
        await expect(toast).toHaveCount(1);
        await expect(toast).toContainText(message);
        if (attempt === 1) await toast.getByRole('button', { name: 'Close', exact: true }).click();
        else await page.clock.runFor(5100);
        await expect(toast).toHaveCount(0);
      }
    });
  }
}
