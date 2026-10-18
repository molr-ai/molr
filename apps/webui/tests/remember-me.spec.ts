import { test, expect } from '@playwright/test';

for (const remember of [false, true]) {
  test(`keep me signed in controls session and CSRF cookie persistence (${remember})`, async ({ page, context }) => {
    await page.goto('/');
    const choice = page.getByRole('checkbox', { name: 'Keep me signed in', exact: true });
    await expect(choice).toBeChecked();
    await choice.setChecked(remember);
    await page.getByLabel('Username', { exact: true }).fill('owner');
    await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
    const submitted = page.waitForRequest(request => request.url().endsWith('/api/auth/login') && request.method() === 'POST');
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    expect((await submitted).postDataJSON().remember_me).toBe(remember);
    await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
    const cookies = (await context.cookies()).filter(cookie => /^molr_dev_(session|csrf)_p/.test(cookie.name));
    expect(cookies).toHaveLength(2);
    for (const cookie of cookies) {
      if (remember) expect(cookie.expires).toBeGreaterThan(Date.now() / 1000 + 29 * 86400);
      else expect(cookie.expires).toBe(-1);
    }
    await page.reload();
    await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  });
}
