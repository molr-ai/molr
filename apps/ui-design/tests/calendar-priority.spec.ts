import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import { chooseOption } from './select-helpers';
import { assistantList, selectAssistant } from './interface-helpers';
async function setup(page: Page) { await page.goto('/#interfaces'); await page.evaluate(() => { localStorage.removeItem('molr-assistant-prototype-v1'); localStorage.setItem('molr-ui-design-locale', 'en'); }); await page.reload(); return page.locator('#interfaces').getByRole('tabpanel').first().locator(':scope > div:not([hidden])'); }

test('bubbles, red unread badges, and performance information popovers', async ({ page }, info) => {
  const ui = await setup(page);
  const badge = (await assistantList(ui)).getByRole('button', { name: '旅行助理', exact: true }).locator('.assistant-unread');
  expect(await badge.evaluate(el => getComputedStyle(el).backgroundColor)).toBe('rgb(209, 67, 67)');
  expect(await badge.evaluate(el => getComputedStyle(el).color)).toBe('rgb(255, 255, 255)');
  await selectAssistant(ui);
  const content = ui.locator('.assistant-chat__message--assistant .message-content').first();
  expect(await content.evaluate(el => getComputedStyle(el).borderRadius)).toBe('16px');
  expect(await content.evaluate(el => getComputedStyle(el).backgroundColor)).not.toBe('rgba(0, 0, 0, 0)');
  await expect(ui.locator('.message-info-details').first()).not.toBeVisible();
  const trigger = ui.getByRole('button', { name: 'Message information', exact: true }).first();
  await trigger.click();
  const popover = page.getByRole('dialog', { name: 'Message information' });
  await expect(popover).toContainText('Input tokens');
  await expect(popover).toContainText('First-token latency');
  await expect(popover).toContainText('Decode speed');
  await page.keyboard.press('Escape');
  await expect(popover).not.toBeVisible();
  await expect(trigger).toBeFocused();
  await ui.screenshot({ path: `/tmp/molr-bubbles-${info.project.name}.png` });
});

test('todo urgency supports editing, ordering, filtering and persistence', async ({ page }) => {
  let ui = await setup(page);
  await ui.getByRole('navigation', { name: 'Workspace navigation' }).getByRole('button', { name: 'Plans', exact: true }).click();
  for (const [title, urgency] of [['Read later', 'low'], ['Check departure today', 'urgent']] as const) {
    await ui.getByRole('button', { name: 'New plan' }).click();
    const form = ui.getByRole('form', { name: 'Plan editor' });
    await form.getByRole('textbox', { name: 'What needs doing?' }).fill(title);
    await form.getByText('More options', { exact: true }).click();
    await chooseOption(form.getByRole('combobox', { name: 'Urgency', exact: true }), { urgent: 'Urgent', high: 'High', normal: 'Normal', low: 'Low' }[urgency]);
    await form.getByRole('button', { name: 'Save plan' }).click();
  }
  await expect(ui.locator('.plan-row').first()).toContainText('Check departure today');
  await chooseOption(ui.getByRole('combobox', { name: 'Filter urgency' }), 'Low');
  await expect(ui.locator('.plan-row')).toHaveCount(1);
  await expect(ui.locator('.plan-row')).toContainText('Read later');
  await page.reload(); ui = page.locator('#interfaces').getByRole('tabpanel').first().locator(':scope > div:not([hidden])');
  await ui.getByRole('navigation', { name: 'Workspace navigation' }).getByRole('button', { name: 'Plans', exact: true }).click();
  const urgent = ui.locator('.plan-row').filter({ hasText: 'Check departure today' });
  await expect(urgent).toContainText('Urgent');
  await urgent.getByRole('button', { name: 'Edit', exact: true }).click();
  await chooseOption(ui.getByRole('form', { name: 'Plan editor' }).getByRole('combobox', { name: 'Urgency', exact: true }), 'Normal');
  await ui.getByRole('button', { name: 'Save plan' }).click();
  await expect(urgent).toContainText('Normal');
});

test('calendar switches months, shows scheduled days, and supports keyboard selection', async ({ page }, info) => {
  const ui = await setup(page);
  await ui.getByRole('navigation', { name: 'Workspace navigation' }).getByRole('button', { name: 'Plans', exact: true }).click();
  await ui.getByRole('group', { name: 'Plan view' }).getByRole('button', { name: 'Calendar', exact: true }).click();
  await ui.getByLabel('View date').fill('2026-09-30');
  await ui.getByRole('button', { name: 'New plan', exact: true }).click();
  const form = ui.getByRole('form', { name: 'Plan editor' });
  await chooseOption(form.getByRole('combobox', { name: 'Plan type' }), 'Time block');
  await form.getByRole('textbox', { name: 'What needs doing?' }).fill('Meet a friend');
  await form.getByRole('button', { name: 'Save plan' }).click();
  await ui.getByRole('group', { name: 'Plan view' }).getByRole('button', { name: 'Calendar', exact: true }).click();
  await expect(ui.getByRole('heading', { name: /2026/ }).first()).toHaveText('September 2026');
  await expect(ui.locator('.plan-calendar-day')).toHaveCount(126);
  const day = ui.locator('.plan-calendar').first().getByRole('button', { name: /^2026-09-30,/ });
  await day.click();
  await expect(ui.locator('.plan-list').first()).toContainText('Meet a friend');
  await day.press('ArrowRight');
  await expect(ui.getByLabel('View date')).toHaveValue('2026-10-01');
  await expect(ui.getByRole('heading', { name: /2026/ }).first()).toHaveText('October 2026');
  await ui.getByRole('button', { name: 'Previous month' }).click();
  await expect(ui.getByRole('heading', { name: /2026/ }).first()).toHaveText('July 2026');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await ui.screenshot({ path: `/tmp/molr-calendar-${info.project.name}.png` });
  await ui.getByRole('group', { name: 'Plan view' }).getByRole('button', { name: 'List', exact: true }).click();
  await expect(ui.locator('.plan-calendar')).toHaveCount(0);
});
