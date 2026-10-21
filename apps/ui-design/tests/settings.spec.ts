import { readLayout } from '../../../scripts/test-support/layout.ts';
import { test, expect } from '@playwright/test';
test.beforeEach(async ({ page }) => { await page.goto('/#interfaces'); await page.evaluate(() => { localStorage.removeItem('molr-assistant-prototype-v1'); localStorage.setItem('molr-ui-design-locale', 'en'); }); await page.reload(); });

test('settings owns memory, preferences and usage; section changes preserve drafts', async ({ page }) => {
  const panel = page.locator('#interfaces').getByRole('tabpanel').first().locator(':scope > div:not([hidden])');
  const main = panel.getByRole('navigation', { name: 'Workspace navigation' });
  await expect(main.getByRole('button', { name: 'Memory', exact: true })).toHaveCount(0);
  await main.getByRole('button', { name: 'Settings', exact: true }).click();
  const menu = panel.getByRole('tablist', { name: 'Settings sections' });
  await expect(menu.getByRole('tab')).toHaveText(['Interface', 'Memory', 'Preferences', 'Usage', 'Connections', 'Former assistants', 'Data']);
  await panel.getByRole('textbox', { name: 'What are your preferences?' }).fill('Keep answers concise.');
  await menu.getByRole('tab', { name: 'Memory', exact: true }).click();
  await expect(panel.getByText('No saved memories.', { exact: true })).toBeVisible();
  await panel.getByRole('textbox', { name: 'What should your assistants know?' }).fill('I live in Shanghai.');
  await panel.getByRole('button', { name: 'Save memory', exact: true }).click();
  await menu.getByRole('tab', { name: 'Preferences', exact: true }).click();
  await expect(panel.getByRole('textbox', { name: 'What are your preferences?' })).toHaveValue('Keep answers concise.');
  await expect(panel.getByText('I live in Shanghai.', { exact: true })).not.toBeVisible();
  await panel.getByRole('button', { name: 'Save preference', exact: true }).click();
  await menu.getByRole('tab', { name: 'Usage', exact: true }).click();
  await panel.getByRole('spinbutton', { name: 'Default budget (CNY)' }).fill('4');
  await panel.getByRole('button', { name: 'Save defaults', exact: true }).click();
  await page.reload();
  await main.getByRole('button', { name: 'Settings', exact: true }).click();
  await expect(panel.getByText('Keep answers concise.', { exact: true })).toBeVisible();
  await menu.getByRole('tab', { name: 'Memory', exact: true }).click();
  await expect(panel.getByText('I live in Shanghai.', { exact: true })).toBeVisible();
  await menu.getByRole('tab', { name: 'Usage', exact: true }).click();
  await expect(panel.getByRole('spinbutton', { name: 'Default budget (CNY)' })).toHaveValue('4');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await panel.screenshot({ path: `/tmp/molr-settings-${test.info().project.name}.png` });
});

test('mobile settings exposes connection and data pages', async ({ page }) => {
  await page.locator('#interfaces').getByRole('tab', { name: 'Mobile', exact: true }).click();
  const panel = page.locator('#interfaces').getByRole('tabpanel').first().locator(':scope > div:not([hidden])');
  await panel.getByRole('navigation', { name: 'Workspace navigation' }).getByRole('button', { name: 'Settings', exact: true }).click();
  const menu = panel.getByRole('tablist', { name: 'Settings sections' });
  await menu.getByRole('tab', { name: 'Connections', exact: true }).click();
  await panel.getByRole('button', { name: 'Simulate disconnect', exact: true }).click();
  await expect(panel.getByRole('button', { name: 'Reconnect', exact: true })).toBeVisible();
  await panel.getByRole('button', { name: 'Reconnect', exact: true }).click();
  await menu.getByRole('tab', { name: 'Data', exact: true }).click();
  await panel.getByText('Prototype data and recovery', { exact: true }).click();
  await expect(panel.getByRole('button', { name: 'Reset demo data', exact: true })).toBeVisible();
  expect(await panel.locator('.interface-preview.ip-shell').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
});

test('settings tabs support keyboard selection and keep the selected tab in view', async ({ page }) => {
  await page.locator('#interfaces').getByRole('tab', { name: 'Mobile', exact: true }).click();
  const panel = page.locator('#interfaces').getByRole('tabpanel').first().locator(':scope > div:not([hidden])');
  await panel.getByRole('navigation', { name: 'Workspace navigation' }).getByRole('button', { name: 'Settings', exact: true }).click();
  const tabs = panel.getByRole('tablist', { name: 'Settings sections' });
  await expect(tabs).toHaveAttribute('aria-orientation', 'horizontal');
  const preferences = tabs.getByRole('tab', { name: 'Preferences', exact: true });
  await expect(preferences).toHaveAttribute('aria-selected', 'true');
  await preferences.focus();
  await preferences.press('End');
  const data = tabs.getByRole('tab', { name: 'Data', exact: true });
  await expect(data).toBeFocused();
  await expect(data).toHaveAttribute('aria-selected', 'true');
  await expect(panel.getByRole('tabpanel', { name: 'Data', exact: true })).toBeVisible();
  await expect.poll(async () => {
    const [listBox, dataBox] = await readLayout(tabs, data);
    return dataBox.x >= listBox.x && dataBox.x + dataBox.width <= listBox.x + listBox.width + 1;
  }).toBe(true);
  await data.press('Home');
  const first = tabs.getByRole('tab', { name: 'Interface', exact: true });
  await expect(first).toBeFocused();
  await first.press('ArrowRight');
  await expect(tabs.getByRole('tab', { name: 'Memory', exact: true })).toBeFocused();
  await expect(panel.locator('.settings-tabs').getByRole('tabpanel')).toHaveCount(1);
});
