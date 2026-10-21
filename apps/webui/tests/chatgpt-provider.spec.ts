import { test, expect } from '@playwright/test';
import type { MolrAppAPI } from '@molr/ui-host/app';

test('OpenAI offers API key and desktop ChatGPT authorization without exposing credentials', async ({ page }) => {
  await page.goto('/#/resources');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  async function open() {
    await page.locator('.resources-header').getByRole('button', { name: 'More', exact: true }).click();
    await page.getByRole('menuitem', { name: 'Connect provider', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Connect provider', exact: true });
    await expect(dialog.getByLabel('API key', { exact: true })).toBeVisible();
    await dialog.getByRole('combobox', { name: 'Authentication', exact: true }).click();
    await page.getByRole('option', { name: 'Continue with ChatGPT', exact: true }).click();
    await expect(dialog.getByLabel('API key', { exact: true })).toHaveCount(0);
    return dialog;
  }
  let dialog = await open();
  await expect(dialog.getByRole('button', { name: 'Continue with ChatGPT', exact: true })).toBeDisabled();
  await expect(dialog.getByText('Authorize in the Molr desktop app', { exact: true })).toBeVisible();
  await expect(dialog.getByRole('link', { name: 'Manage usage' })).toHaveAttribute('href', 'https://chatgpt.com/settings/usage');
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.addInitScript(() => {
    Object.assign(window, { oauthCalls: [] });
    window.molrApp = {
      getInfo: async () => ({ apiVersion: 1, platform: 'linux', build: { version: 'development', commit: '' }, connection: { name: 'Fixture', url: location.origin, kind: 'remote' }, window: { maximized: false, fullscreen: false } }),
      window: { minimize: async () => {}, toggleMaximize: async () => {}, close: async () => {} },
      disconnect: async () => {}, onWindowStateChanged: () => () => {},
      oauth: { authorize: async (provider, options) => { (window as unknown as { oauthCalls: unknown[] }).oauthCalls.push({ provider, options }); return { status: 'cancelled' }; }, cancel: async () => {} },
      updates: { status: async () => ({ comparison: 'uncomparable', checking: false }), check: async () => ({ comparison: 'uncomparable', checking: false }), openDownloads: async () => {}, upgradeLocal: async () => {}, onChanged: () => () => {} },
      notifications: { begin: async () => {}, requestPermission: async () => true, show: async () => {}, close: async () => {}, onClick: () => () => {} },
    } satisfies MolrAppAPI;
  });
  await page.reload(); dialog = await open();
  await dialog.getByLabel('Connection name', { exact: true }).fill('Shared ChatGPT');
  await dialog.getByRole('button', { name: 'Continue with ChatGPT', exact: true }).click();
  await expect.poll(() => page.evaluate(() => (window as unknown as { oauthCalls: unknown[] }).oauthCalls)).toEqual([{ provider: 'chatgpt', options: { name: 'Shared ChatGPT', verification: '' } }]);
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText('Authorize in the Molr desktop app', { exact: true })).toHaveCount(0);
});
