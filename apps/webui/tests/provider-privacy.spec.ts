import { test, expect } from '@playwright/test';
import { selectProvider } from './provider-selection';

test('provider privacy notice is first and dismissal survives provider changes, reopening and reload', async ({ page }) => {
  await page.goto('/#/resources');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  const open = async () => {
    await page.locator('.resources-header').getByRole('button', { name: 'More', exact: true }).click();
    await page.getByRole('menuitem', { name: 'Connect provider', exact: true }).click();
    return page.getByRole('dialog', { name: 'Connect provider', exact: true });
  };
  let dialog = await open();
  const notice = dialog.locator('.provider-privacy-notice');
  await expect(notice).toBeVisible();
  expect(await notice.evaluate(e => e.parentElement?.firstElementChild === e)).toBe(true);
  await notice.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(notice).toHaveCount(0);
  await selectProvider(dialog, 'DeepSeek · 深度求索');
  await expect(dialog.locator('.provider-privacy-notice')).toHaveCount(0);
  await selectProvider(dialog, 'Ollama');
  await selectProvider(dialog, 'OpenAI');
  await expect(dialog.locator('.provider-privacy-notice')).toHaveCount(0);
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  dialog = await open();
  await expect(dialog.locator('.provider-privacy-notice')).toHaveCount(0);
  await page.reload();
  dialog = await open();
  await expect(dialog.locator('.provider-privacy-notice')).toHaveCount(0);
});
