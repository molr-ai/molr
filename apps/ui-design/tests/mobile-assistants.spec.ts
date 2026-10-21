import { readLayout } from '../../../scripts/test-support/layout.ts';
import { test, expect } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';

test.use({ hasTouch: true });

async function mobile(page: Page) {
  await page.goto('/#interfaces');
  await page.evaluate(() => { localStorage.removeItem('molr-assistant-prototype-v1'); localStorage.setItem('molr-ui-design-locale', 'en'); });
  await page.reload();
  await page.locator('#interfaces').getByRole('tab', { name: 'Mobile', exact: true }).click();
  return page.locator('#interfaces').getByRole('tabpanel').first().locator(':scope > div:not([hidden])');
}

async function swipe(page: Page, area: Locator, dx: number, dy = 0) {
  await area.scrollIntoViewIfNeeded();
  const box = (await area.boundingBox())!;
  const x = box.x + 28, y = box.y + Math.min(70, box.height / 2);
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] });
  for (let step = 1; step <= 6; step++) await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: x + dx * step / 6, y: y + dy * step / 6 }] });
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
  await cdp.detach();
}

test('Mobile opens the list, keeps unselected messages unread and pushes a chat on selection', async ({ page }) => {
  const ui = await mobile(page);
  const workspace = ui.locator('.conversation-workspace');
  const list = ui.getByRole('list', { name: 'Assistant list' });
  await expect(workspace).toHaveAttribute('data-mobile-page', 'list');
  await expect(ui.getByRole('log')).toHaveCount(0);
  await expect(ui.getByRole('button', { name: 'Hire new assistant', exact: true })).toHaveCount(0);
  await expect(ui.getByRole('button', { name: 'Search', exact: true })).toBeVisible();
  await expect(list.getByRole('button', { name: '旅行助理', exact: true }).locator('.assistant-unread')).toHaveText('1');
  await list.getByRole('button', { name: '旅行助理', exact: true }).click();
  await expect(workspace).toHaveAttribute('data-mobile-page', 'chat');
  await expect.poll(() => ui.locator('.mobile-conversation-track').evaluate(el => Math.abs(new DOMMatrixReadOnly(getComputedStyle(el).transform).m41 + el.getBoundingClientRect().width))).toBeLessThan(1);
  await expect(ui.getByRole('list', { name: 'Assistant list' })).toHaveCount(0);
  await expect(ui.getByRole('navigation', { name: 'Workspace navigation' })).toHaveCount(0);
  await expect(ui.locator('.conversation-header').getByRole('heading', { name: '旅行助理', exact: true })).toBeVisible();
  const back = ui.getByRole('button', { name: 'Back to assistants', exact: true });
  const [backBox, nameBox] = await readLayout(back, ui.locator('.conversation-header').getByRole('heading', { name: '旅行助理', exact: true }));
  expect(backBox.x + backBox.width).toBeLessThanOrEqual(nameBox.x);
  await ui.getByRole('textbox', { name: 'Message your assistant' }).fill('Keep this unsent draft');
  await back.click();
  await expect(workspace).toHaveAttribute('data-mobile-page', 'list');
  await expect(list.getByRole('button', { name: '旅行助理', exact: true }).locator('.assistant-unread')).toHaveCount(0);
  await list.getByRole('button', { name: '旅行助理', exact: true }).click();
  await expect(ui.getByRole('textbox', { name: 'Message your assistant' })).toHaveValue('Keep this unsent draft');
  await ui.locator('.conversation-header').getByRole('button', { name: 'More', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Permissions', exact: true }).click();
  await expect(ui.locator('.ip-notice')).toContainText('旅行助理');
  await ui.getByRole('button', { name: 'Back to conversation', exact: true }).click();
  await expect(workspace).toHaveAttribute('data-mobile-page', 'chat');
});

test('Mobile hire placeholder keeps the list and reduced motion supports navigation', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  const ui = await mobile(page);
  await ui.getByRole('button', { name: 'More', exact: true }).click();
  await page.getByRole('menuitem', { name: 'Hire new assistant', exact: true }).click();
  await expect(ui.getByRole('form', { name: 'Hire new assistant', exact: true })).toHaveCount(0);
  await expect(ui.locator('.conversation-workspace')).toHaveAttribute('data-mobile-page', 'list');
  const list = ui.getByRole('list', { name: 'Assistant list' });
  await expect(list.getByRole('button')).toHaveCount(2);
  await list.getByRole('button', { name: 'Molr', exact: true }).click();
  await expect(ui.locator('.conversation-header').getByRole('heading', { name: 'Molr', exact: true })).toBeVisible();
  await expect(ui.locator('.mobile-conversation-track')).toHaveCSS('transition-property', 'none');
  await ui.getByRole('button', { name: 'Back to assistants', exact: true }).click();
  await expect(list.getByRole('button', { name: 'Molr', exact: true })).toBeVisible();
  await expect(ui.getByRole('navigation', { name: 'Workspace navigation' })).toBeVisible();
});

test('resetting demo badges restores unread messages without resetting conversations or drafts', async ({ page }) => {
  const ui = await mobile(page);
  const list = ui.getByRole('list', { name: 'Assistant list' });
  await list.getByRole('button', { name: '旅行助理', exact: true }).click();
  await ui.getByRole('textbox', { name: 'Message your assistant' }).fill('Keep my travel draft');
  await ui.getByRole('button', { name: 'Back to assistants', exact: true }).click();
  await list.getByRole('button', { name: 'Molr', exact: true }).click();
  await ui.getByRole('textbox', { name: 'Message your assistant' }).fill('Hello again');
  await ui.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(ui.getByRole('log')).toContainText('I hear you');
  await ui.getByRole('textbox', { name: 'Message your assistant' }).fill('Keep this draft too');
  await ui.getByRole('button', { name: 'Back to assistants', exact: true }).click();
  await expect(ui.locator('.assistant-unread')).toHaveCount(0);

  const before = await page.evaluate(() => JSON.parse(localStorage.getItem('molr-assistant-prototype-v1')!));
  const reset = page.getByRole('button', { name: 'Reset unread badges', exact: true });
  await reset.click();
  await expect(list.getByRole('button', { name: 'Molr', exact: true }).locator('.assistant-unread')).toHaveText('3');
  await expect(list.getByRole('button', { name: '旅行助理', exact: true }).locator('.assistant-unread')).toHaveText('1');
  await expect(ui.locator('.ip-mobile-nav .assistant-unread')).toHaveText('4');
  const after = await page.evaluate(() => JSON.parse(localStorage.getItem('molr-assistant-prototype-v1')!));
  expect(after).toEqual({ ...before, assistants: before.assistants.map((assistant: { readMessageCount?: number }) => ({ ...assistant, readMessageCount: 0 })) });
  await reset.click();
  await expect(ui.locator('.ip-mobile-nav .assistant-unread')).toHaveText('4');

  await page.reload();
  await page.locator('#interfaces').getByRole('tab', { name: 'Mobile', exact: true }).click();
  await expect(list.getByRole('button', { name: '旅行助理', exact: true }).locator('.assistant-unread')).toHaveText('1');
  await list.getByRole('button', { name: '旅行助理', exact: true }).click();
  await expect(ui.getByRole('textbox', { name: 'Message your assistant' })).toHaveValue('Keep my travel draft');
  await ui.getByRole('button', { name: 'Back to assistants', exact: true }).click();
  await expect(ui.locator('.assistant-unread')).toHaveCount(0);

  await reset.click();
  await page.locator('#interfaces').getByRole('tab', { name: 'Desktop', exact: true }).click();
  await expect(ui.locator('.ip-nav .assistant-unread')).toHaveText('3');
});

test('a right swipe returns to the list while vertical, short and composer gestures stay in chat', async ({ page }) => {
  const ui = await mobile(page);
  await ui.getByRole('list', { name: 'Assistant list' }).getByRole('button', { name: 'Molr', exact: true }).click();
  const workspace = ui.locator('.conversation-workspace');
  const log = ui.getByRole('log');
  await swipe(page, log, 18, 0);
  await expect(workspace).toHaveAttribute('data-mobile-page', 'chat');
  await swipe(page, log, 20, -55);
  await expect(workspace).toHaveAttribute('data-mobile-page', 'chat');
  await swipe(page, ui.locator('.message-composer'), 110);
  await expect(workspace).toHaveAttribute('data-mobile-page', 'chat');
  await swipe(page, log, 145);
  await expect(workspace).toHaveAttribute('data-mobile-page', 'list');
  await expect(ui.getByRole('list', { name: 'Assistant list' })).toBeVisible();
});
