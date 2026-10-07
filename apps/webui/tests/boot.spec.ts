import { test, expect } from '@playwright/test';

for (const theme of ['light', 'dark'] as const) {
  test(`the ${theme} page background is correct before React loads and during session restoration`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme === 'light' ? 'dark' : 'light' });
    await page.addInitScript(preference => localStorage.setItem('molr-theme', preference), theme);
    await page.route('**/src/main.tsx', route => route.abort());
    await page.goto('/');
    const color = theme === 'light' ? 'rgb(245, 246, 239)' : 'rgb(19, 28, 23)';
    await expect(page.locator('#root')).toBeEmpty();
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
    await expect(page.locator('html')).toHaveCSS('background-color', color);
    await expect(page.locator('body')).toHaveCSS('background-color', color);
    await expect(page.locator('meta[name="theme-color"]')).toHaveAttribute('content', theme === 'light' ? '#f5f6ef' : '#131c17');
    await page.unroute('**/src/main.tsx');
    await page.route('**/api/auth/me', () => {});
    await page.reload();
    await expect(page.locator('.session-page')).toBeVisible();
    await expect(page.locator('.session-page')).toHaveCSS('background-image', 'none');
    await expect(page.locator('body')).toHaveCSS('background-color', color);
    await page.unroute('**/api/auth/me');
    await page.reload();
    await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
    await expect(page.locator('html')).toHaveCSS('background-color', color);
    await expect(page.locator('meta[name="theme-color"]')).toHaveAttribute('content', theme === 'light' ? '#f5f6ef' : '#131c17');
  });
}

test('saved palette is visible before React and while restoring the session', async ({ page }) => {
  await page.addInitScript(() => { localStorage.setItem('molr-theme', 'dark'); localStorage.setItem('molr-webui-palette', 'ocean'); });
  await page.route('**/src/main.tsx', route => route.abort());
  await page.goto('/');
  await expect(page.locator('#root')).toBeEmpty();
  await expect(page.locator('html')).toHaveAttribute('data-palette', 'ocean');
  await expect(page.locator('body')).toHaveCSS('background-color', 'rgb(17, 28, 37)');
  await page.unroute('**/src/main.tsx');
  await page.route('**/api/auth/me', () => {});
  await page.reload();
  await expect(page.locator('.session-page')).toBeVisible();
  await expect(page.locator('body')).toHaveCSS('background-color', 'rgb(17, 28, 37)');
});
