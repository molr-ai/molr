import { expect, type Page } from '@playwright/test';

export async function showPlugins(page: Page) {
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).not.toBeVisible();
  if (!page.url().endsWith('#/account/general')) await page.goto('/#/account/general');
  const toggle = page.getByRole('switch', { name: 'Show plugins', exact: true });
  await expect(toggle).toBeEnabled();
  await toggle.check();
  await expect.poll(async () => (await (await page.request.get('/api/settings')).json()).PluginsVisible).toBe(true);
  await expect(page.locator('.workspace-nav').getByRole('button', { name: 'Plugins', exact: true })).toBeVisible();
  await page.goto('/#/extensions');
  await expect(page.getByRole('heading', { name: 'Plugins', exact: true })).toBeVisible();
}
