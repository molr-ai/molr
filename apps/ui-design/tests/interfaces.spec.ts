import { test, expect } from '@playwright/test';
import type { Page, Locator } from '@playwright/test';
import { assistantList, selectAssistant } from './interface-helpers';
async function panel(page: Page, name = 'Web') { await page.locator('#interfaces').getByRole('tab', { name, exact: true }).click(); return page.locator('#interfaces').getByRole('tabpanel').first().locator(':scope > div:not([hidden])'); }
async function chat(page: Page, name = 'Web', assistant = 'Molr') { const ui = await panel(page, name); await selectAssistant(ui, assistant); return ui; }
async function say(ui: Locator, text: string) { await ui.getByRole('textbox', { name: 'Message your assistant' }).fill(text); await ui.getByRole('button', { name: 'Send', exact: true }).click(); }
async function tasks(page: Page, ui: Locator, title?: RegExp) { await ui.locator('.conversation-header').getByRole('button', { name: 'Plans', exact: true }).click(); const drawer = page.getByRole('dialog'); if (title) await drawer.getByRole('button', { name: title }).click(); return drawer; }
test.beforeEach(async ({ page }) => { await page.goto('/#interfaces'); await page.evaluate(() => { localStorage.removeItem('molr-assistant-prototype-v1'); localStorage.setItem('molr-ui-design-locale', 'en'); }); await page.reload(); });

test('chat has a single identity header with message copy and feedback controls', async ({ page }) => {
  const ui = await chat(page);
  await expect(ui.locator('.conversation-header').getByRole('heading', { name: 'Molr', exact: true })).toBeVisible();
  await expect(ui.locator('.ip-page-heading')).toHaveCount(0);
  await expect(ui.locator('.ip-toolbar')).toHaveCount(0);
  await expect(ui.locator('.conversation-status')).toContainText('Browsing files');
  await expect(ui.getByRole('button', { name: 'Copy original text', exact: true }).first()).toBeVisible();
  await expect(ui.getByRole('button', { name: 'Like', exact: true }).first()).toBeVisible();
  await expect(ui.getByRole('button', { name: 'Dislike', exact: true }).first()).toBeVisible();
  await expect(ui.locator('.message-task-card button')).toHaveCount(0);
  await say(ui, 'Hello there');
  await expect(ui.locator('.conversation-status')).toContainText('Typing');
  await expect(ui.getByRole('log')).toContainText('I hear you');
  await page.reload(); const restored = await chat(page);
  await expect(restored.getByRole('log')).toContainText('Hello there');
});

test('task button opens a responsive modal drawer and returns focus on close', async ({ page }, info) => {
  const ui = await chat(page);
  const drawer = await tasks(page, ui);
  await expect(drawer).toHaveAttribute('data-side', info.project.name === 'mobile' ? 'bottom' : 'right');
  await expect(drawer.getByRole('button', { name: /Review my last trip/ })).toBeVisible();
  const viewport = page.viewportSize()!;
  await expect.poll(async () => { const bounds = await drawer.boundingBox(); return Math.abs(bounds!.x + bounds!.width - viewport.width); }).toBeLessThan(2);
  await page.keyboard.press('Escape');
  await expect(drawer).not.toBeVisible();
  await expect(ui.locator('.conversation-header').getByRole('button', { name: 'Plans', exact: true })).toBeFocused();
  const mobile = await chat(page, 'Mobile');
  const sheet = await tasks(page, mobile);
  await expect(sheet).toHaveAttribute('data-side', 'bottom');
  await sheet.getByRole('button', { name: 'Close plans' }).click();
  await expect(sheet).not.toBeVisible();
});

test('task revisions remain in the conversation and preserve previous deliverables', async ({ page }) => {
  const ui = await chat(page);
  let drawer = await tasks(page, ui, /Review my last trip/);
  await drawer.getByRole('button', { name: 'Continue in conversation' }).click();
  await expect(drawer).not.toBeVisible();
  await expect(ui.locator('.conversation-reference')).toContainText('#023');
  await say(ui, 'Leave every afternoon free for rest.');
  await expect(ui.getByRole('log')).toContainText('Your revision is attached');
  drawer = await tasks(page, ui, /Review my last trip/);
  await expect(drawer.locator('.ip-detail-meta')).toContainText('In progress');
  await expect(drawer.locator('.assistant-delivery')).toContainText('Leave every afternoon free for rest.');
  await drawer.getByText('Previous deliverables', { exact: false }).click();
  await expect(drawer.getByText('v1', { exact: true })).toBeVisible();
  for (let i = 0; i < 3; i++) await drawer.getByRole('button', { name: 'Advance next step' }).click();
  await drawer.getByRole('button', { name: 'Run verification' }).click();
  await expect(drawer.locator('.ip-detail-meta')).toContainText('Completed');
  await drawer.getByRole('button', { name: 'Close plans' }).click();
  await expect(ui.getByRole('log')).toContainText('result is complete and verified');
});

test('unread counts stay isolated and hidden previews never mark a conversation read', async ({ page }) => {
  const ui = await panel(page);
  const list = await assistantList(ui);
  await expect(list.getByRole('button', { name: 'Molr', exact: true }).locator('.assistant-unread')).toHaveCount(0);
  await expect(list.getByRole('button', { name: '旅行助理', exact: true }).locator('.assistant-unread')).toHaveText('1');
  await chat(page);
  await assistantList(ui);
  await expect(list.getByRole('button', { name: 'Molr', exact: true }).locator('.assistant-unread')).toHaveCount(0);
  await expect(list.getByRole('button', { name: '旅行助理', exact: true }).locator('.assistant-unread')).toHaveText('1');
  const cli = await panel(page, 'CLI');
  await cli.getByRole('textbox', { name: 'Terminal command' }).fill('say Hello while hidden');
  await cli.getByRole('textbox', { name: 'Terminal command' }).press('Enter');
  await expect.poll(() => page.evaluate(() => { const a = JSON.parse(localStorage.getItem('molr-assistant-prototype-v1')!).assistants[0]; return a.messages.filter((m: {role: string}) => m.role === 'assistant').length - a.readMessageCount; })).toBe(1);
  await chat(page);
  await assistantList(ui);
  await expect(list.getByRole('button', { name: 'Molr', exact: true }).locator('.assistant-unread')).toHaveCount(0);
  const mobile = await chat(page, 'Mobile', '旅行助理');
  await expect(mobile.locator('.conversation-status')).toHaveText('Idle');
});

test('Mobile search previews results without marking conversations read until one is opened', async ({ page }) => {
  const ui = await panel(page, 'Mobile');
  const trigger = ui.getByRole('button', { name: 'Search', exact: true });
  const list = ui.getByRole('list', { name: 'Assistant list' });
  await expect(list.getByRole('button', { name: '旅行助理', exact: true }).locator('.assistant-unread')).toHaveText('1');
  await trigger.click();
  const dialog = page.getByRole('dialog', { name: 'Search', exact: true });
  const search = dialog.getByRole('searchbox');
  await expect(search).toBeFocused();
  const before = await page.evaluate(() => localStorage.getItem('molr-assistant-prototype-v1'));
  await search.fill('旅行');
  await expect(dialog.getByRole('region', { name: 'Assistants', exact: true }).getByRole('button')).toContainText('旅行助理');
  await search.fill('  DIGEST  ');
  await expect(dialog.getByRole('region', { name: 'Chats', exact: true })).toContainText('Your trip digest is ready');
  await search.fill('no conversation matches this');
  await expect(dialog.getByRole('heading', { name: 'No matching results', exact: true })).toBeVisible();
  await dialog.getByRole('button', { name: 'Clear search', exact: true }).click();
  await expect(search).toBeFocused();
  await expect(search).toHaveValue('');
  expect(await page.evaluate(() => localStorage.getItem('molr-assistant-prototype-v1'))).toBe(before);
  await page.keyboard.press('Escape');
  await expect(trigger).toBeFocused();
  await expect(list.getByRole('button', { name: '旅行助理', exact: true }).locator('.assistant-unread')).toHaveText('1');
  await trigger.click();
  await search.fill('旅行');
  await dialog.getByRole('region', { name: 'Assistants', exact: true }).getByRole('button').click();
  await expect(dialog).not.toBeVisible();
  await expect(ui.locator('.conversation-header').getByRole('heading', { name: '旅行助理', exact: true })).toBeVisible();
  await assistantList(ui);
  await expect(list.locator('.assistant-unread')).toHaveCount(0);
});

test('icon navigation exposes tooltips for hover and keyboard focus and keeps page actions accessible', async ({ page }) => {
  const ui = await panel(page);
  const nav = ui.getByRole('navigation', { name: 'Workspace navigation' });
  const assistant = nav.getByRole('button', { name: 'Assistant', exact: true });
  await nav.getByRole('button', { name: 'Search', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Search', exact: true }).getByRole('searchbox')).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(assistant).toHaveAttribute('aria-current', 'page');
  // Move on from the restored search trigger before checking the next tooltip.
  await assistant.focus();
  await assistant.hover();
  await expect(page.getByRole('tooltip').filter({ hasText: /^Assistant$/ })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('tooltip').filter({ hasText: /^Assistant$/ })).not.toBeVisible();
  await page.mouse.move(0, 0);

  const settings = nav.getByRole('button', { name: 'Settings', exact: true });
  await settings.focus();
  await expect(page.getByRole('tooltip').filter({ hasText: /^Settings$/ })).toBeVisible();
  await settings.press('Enter');
  await expect(settings).toHaveAttribute('aria-current', 'page');
  await expect(ui.getByRole('tablist', { name: 'Settings sections' })).toBeVisible();

  await nav.getByRole('button', { name: 'Plans', exact: true }).click();
  await expect(ui.getByRole('group', { name: 'Page actions' }).getByRole('button', { name: 'New plan', exact: true })).toBeVisible();
  await nav.getByRole('button', { name: 'Settings', exact: true }).click();
  await expect(ui.getByRole('tablist', { name: 'Settings sections' }).getByRole('tab', { name: 'Preferences', exact: true })).toHaveAttribute('aria-selected', 'true');
});

test('assistant permissions remain isolated through task drawers', async ({ page }) => {
  const ui = await chat(page, 'Mobile', '旅行助理');
  await say(ui, 'Prepare my itinerary');
  await expect(ui.getByRole('log')).toContainText('recorded this as a task');
  const drawer = await tasks(page, ui, /Prepare my itinerary/);
  await expect(drawer.locator('.ip-detail-meta')).toContainText('Awaiting access');
  await expect(drawer.getByRole('button', { name: 'Advance next step' })).toBeDisabled();
  await drawer.getByRole('button', { name: 'Manage resource access' }).click();
  await expect(drawer).not.toBeVisible();
  const grant = ui.locator('.ip-grant').filter({ hasText: 'Travel notes' });
  await grant.getByRole('button', { name: 'Allow access' }).click();
  await chat(page, 'Mobile', '旅行助理');
  const resumed = await tasks(page, ui, /Prepare my itinerary/);
  await resumed.getByRole('button', { name: 'Resume task' }).click();
  await expect(resumed.locator('.ip-detail-meta')).toContainText('In progress');
});

test('chat and drawers stay within bounds and screenshots capture the final layout', async ({ page }, info) => {
  for (const name of ['Web', 'Desktop', 'Mobile']) {
    const ui = await chat(page, name);
    expect(await ui.locator('.interface-preview.ip-shell').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
    await ui.screenshot({ path: `/tmp/molr-conversation-${name}-${info.project.name}.png` });
    const drawer = await tasks(page, ui);
    await drawer.screenshot({ path: `/tmp/molr-tasks-${name}-${info.project.name}.png` });
    await drawer.getByRole('button', { name: 'Close plans' }).click();
  }
});
