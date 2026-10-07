import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import { chooseOption } from './select-helpers';
import { assistantList } from './interface-helpers';
async function open(page: Page, mobile = false) {
  await page.locator('#interfaces').getByRole('tab', { name: mobile ? 'Mobile' : 'Web', exact: true }).click();
  const panel = page.locator('#interfaces').getByRole('tabpanel').first().locator(':scope > div:not([hidden])');
  await panel.getByRole('navigation').getByRole('button', { name: 'Plans', exact: true }).click(); return panel;
}
test.beforeEach(async ({ page }) => { await page.goto('/#interfaces'); await page.evaluate(() => { localStorage.removeItem('molr-assistant-prototype-v1'); localStorage.setItem('molr-ui-design-locale', 'en'); }); await page.reload(); });

test('personal schedules validate times, recur, persist, and delete as a series', async ({ page }) => {
  let panel = await open(page);
  await panel.getByRole('button', { name: 'New plan', exact: true }).click();
  const form = panel.getByRole('form', { name: 'Plan editor' });
  await chooseOption(form.getByRole('combobox', { name: 'Plan type' }), 'Time block');
  await form.getByRole('textbox', { name: 'What needs doing?' }).fill('Walk with a friend');
  await form.getByLabel('Start date', { exact: true }).fill('2026-09-22');
  await form.getByLabel('Start time', { exact: true }).fill('18:00');
  await form.getByLabel('End time', { exact: true }).fill('17:00');
  await form.getByRole('button', { name: 'Save plan' }).click();
  await expect(form.getByRole('alert')).toContainText('end must be after');
  await form.getByLabel('End time', { exact: true }).fill('19:00');
  await chooseOption(form.getByRole('combobox', { name: 'Repeat' }), 'Every week');
  await form.getByRole('textbox', { name: 'Location', exact: true }).fill('Park');
  await form.getByRole('button', { name: 'Save plan' }).click();
  await panel.getByRole('group', { name: 'Plan view' }).getByRole('button', { name: 'Calendar', exact: true }).click();
  await panel.getByLabel('View date').fill('2026-09-29');
  const event = panel.locator('.plan-row').filter({ hasText: 'Walk with a friend' });
  await expect(event).toBeVisible();
  await event.getByRole('button', { name: 'Edit', exact: true }).click();
  await form.getByRole('textbox', { name: 'What needs doing?' }).fill('Evening walk with a friend');
  await form.getByRole('button', { name: 'Save plan' }).click();
  await page.reload(); panel = await open(page);
  await panel.getByRole('group', { name: 'Plan view' }).getByRole('button', { name: 'Calendar', exact: true }).click();
  await panel.getByLabel('View date').fill('2026-09-29');
  const saved = panel.locator('.plan-row').filter({ hasText: 'Evening walk with a friend' });
  await saved.getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(saved).toContainText('entire series');
  await saved.getByRole('button', { name: 'Confirm delete' }).click();
  await expect(saved).toHaveCount(0);
});

test('assistant schedules assign an owner, pause independently, and survive platform switches', async ({ page }) => {
  const panel = await open(page, true);
  await panel.getByRole('button', { name: 'New plan', exact: true }).click();
  const form = panel.getByRole('form', { name: 'Plan editor' });
  await chooseOption(form.getByRole('combobox', { name: 'Plan type' }), 'Automatic execution');
  await chooseOption(form.getByRole('combobox', { name: 'Owner' }), '旅行助理');
  await form.getByRole('textbox', { name: 'What needs doing?' }).fill('Travel briefing');
  await form.getByLabel('Start date', { exact: true }).fill('2026-09-22');
  await form.getByLabel('Start time', { exact: true }).fill('08:30');
  await chooseOption(form.getByRole('combobox', { name: 'Repeat' }), 'Weekdays');
  await form.getByRole('textbox', { name: 'Instructions' }).fill('Summarize my travel notes.');
  await form.getByRole('button', { name: 'Save plan' }).click();
  const event = panel.locator('.plan-row').filter({ hasText: 'Travel briefing' });
  await expect(event).toContainText('旅行助理');
  await event.getByRole('button', { name: 'Pause automatic execution' }).click();
  await expect(event).toContainText('Paused');
  await panel.getByRole('group', { name: 'Plan view' }).getByRole('button', { name: 'Calendar', exact: true }).click();
  await panel.getByLabel('View date').fill('2026-09-26');
  await expect(event).toHaveCount(0);
  await panel.getByLabel('View date').fill('2026-09-28');
  await expect(event).toContainText('Paused');
  await event.getByRole('button', { name: 'Enable automatic execution' }).click();
  await page.locator('#interfaces').getByRole('tab', { name: 'Web', exact: true }).click();
  const web = await open(page); await web.getByRole('group', { name: 'Plan view' }).getByRole('button', { name: 'Calendar', exact: true }).click(); await web.getByLabel('View date').fill('2026-09-28');
  await expect(web.locator('.plan-row').filter({ hasText: 'Travel briefing' })).toContainText('Enabled');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await web.screenshot({ path: `/tmp/molr-schedule-${test.info().project.name}.png` });
});

test('hire new assistant is a placeholder and More renames the active assistant', async ({ page }) => {
  const panel = page.locator('#interfaces').getByRole('tabpanel').first().locator(':scope > div:not([hidden])');
  await expect(panel.locator('.conversation-header').getByRole('heading', { name: 'Molr', exact: true })).toBeVisible();
  await expect(panel.getByRole('heading', { name: 'My assistant team' })).toHaveCount(0);
  const list = await assistantList(panel);
  await expect(panel.getByRole('navigation', { name: 'Workspace navigation' }).getByRole('list', { name: 'Assistant list' })).toHaveCount(0);
  await panel.getByRole('button', { name: 'Hire new assistant', exact: true }).click();
  await expect(panel.getByRole('form', { name: 'Hire new assistant', exact: true })).toHaveCount(0);
  await expect(list.getByRole('button')).toHaveCount(2);
  await list.getByRole('button', { name: 'Molr', exact: true }).click();
  await panel.getByRole('button', { name: 'More', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Rename assistant', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Rename assistant', exact: true });
  await dialog.getByRole('textbox', { name: 'Assistant name' }).fill('My daily helper');
  await dialog.getByRole('button', { name: 'Save name', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect(panel.locator('.conversation-header').getByRole('heading', { name: 'My daily helper', exact: true })).toBeVisible();
  await panel.locator('.conversation-header').getByRole('button', { name: 'More', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Permissions', exact: true }).click();
  await expect(panel.locator('.ip-notice')).toContainText('My daily helper');
  await panel.getByRole('button', { name: 'Back to conversation', exact: true }).click();
  await assistantList(panel);
  await expect(list.getByRole('button', { name: 'My daily helper', exact: true })).toHaveAttribute('aria-current', 'page');
  await page.reload();
  await expect(panel.locator('.conversation-header').getByRole('heading', { name: 'My daily helper', exact: true })).toBeVisible();
  await panel.getByRole('button', { name: 'More', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Plans', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Plans', exact: true })).toBeVisible();
});

test('resource categories expose appropriate details and assistant-scoped access', async ({ page }) => {
  const panel = page.locator('#interfaces').getByRole('tabpanel').first().locator(':scope > div:not([hidden])');
  await panel.getByRole('navigation').getByRole('button', { name: 'Resources', exact: true }).click();
  const categories = panel.getByRole('group', { name: 'Resource categories' });
  await expect(categories.getByRole('button')).toHaveCount(4);
  await expect(panel.getByRole('button', { name: /Travel notes/ })).toBeVisible();
  for (const [category, resource, detail] of [['Compute', 'Local compute', 'Concurrency 1'], ['Network', 'Web access', 'public HTTPS'], ['Tools', 'Document reader', 'authorized storage']]) {
    await categories.getByRole('button', { name: new RegExp(category) }).click();
    await panel.getByRole('button', { name: new RegExp(resource) }).click();
    await expect(panel.locator('.resource-capability')).toContainText(detail);
    await expect(panel.getByRole('heading', { name: 'File preview' })).toHaveCount(0);
    await panel.getByRole('navigation').getByRole('button', { name: 'Resources', exact: true }).click();
  }
  await panel.getByRole('group', { name: 'Resource views' }).getByRole('button', { name: 'Access' }).click();
  const grant = panel.locator('.ip-grant').filter({ hasText: 'Local compute' });
  await grant.getByRole('button', { name: 'Allow access' }).click();
  await expect(grant.getByText('Allowed', { exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
