import { readLayout } from '../../../scripts/test-support/layout.ts';
import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';

async function category(page: Page, name: string) {
  await page.getByRole('tablist', { name: 'Component categories', exact: true }).getByRole('tab', { name, exact: true }).click();
}
async function choose(page: Page, name: string, option: string) {
  await page.getByRole('combobox', { name, exact: true }).click();
  await page.getByRole('listbox', { name, exact: true }).getByRole('option', { name: option, exact: true }).click();
}
function example(page: Page, name: string) {
  return page.locator('.component-example').filter({ has: page.getByRole('heading', { name, exact: true }) });
}
const runtimeErrors = new WeakMap<Page, string[]>();

test.beforeEach(async ({ page }) => {
  const errors: string[] = [];
  runtimeErrors.set(page, errors);
  page.on('pageerror', error => errors.push(error.message));
  await page.goto('/#components');
  await expect(page.getByRole('tablist', { name: 'Component categories', exact: true })).toBeVisible();
});
test.afterEach(async ({ page }) => { expect(runtimeErrors.get(page)).toEqual([]); });

test('date time picker supports calendar shortcuts, forever and custom input', async ({ page }, info) => {
  await page.clock.setFixedTime(new Date('2028-01-31T04:05:00.000Z'));
  await category(page, 'Inputs & actions');
  const demo = example(page, 'Date and time picker');
  const picker = demo.getByRole('group', { name: 'Expiry', exact: true });
  const result = demo.getByTestId('expiry-value');
  await expect(picker.getByRole('button', { name: 'Forever', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await picker.getByRole('button', { name: '1 day', exact: true }).click();
  await expect(result).toHaveText('2028-02-01T04:05:00.000Z');
  await picker.getByRole('button', { name: '7 days', exact: true }).focus();
  await page.keyboard.press('Enter');
  await expect(result).toHaveText('2028-02-07T04:05:00.000Z');
  await picker.getByRole('button', { name: '1 month', exact: true }).click();
  await expect(result).toHaveText('2028-02-29T04:05:00.000Z');
  await picker.getByRole('button', { name: 'Custom', exact: true }).click();
  const calendar = page.getByRole('dialog', { name: 'Date and time', exact: true });
  await expect(calendar.getByRole('grid', { name: 'Calendar', exact: true })).toBeVisible();
  const [dateColumn, timeColumn] = await readLayout(calendar.locator('.ui-date-calendar__date'), calendar.locator('.ui-date-calendar__time'));
  expect(timeColumn.x).toBeGreaterThanOrEqual(dateColumn.x + dateColumn.width);
  expect(Math.abs(timeColumn.y - dateColumn.y)).toBeLessThan(2);
  expect(await calendar.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
  await expect(calendar.locator('input[type="datetime-local"]')).toHaveCount(0);
  await calendar.getByRole('button', { name: 'Next month', exact: true }).click();
  await calendar.getByRole('button', { name: 'Sunday, March 5, 2028', exact: true }).click();
  const hours = calendar.getByRole('listbox', { name: 'Hour', exact: true });
  const minutes = calendar.getByRole('listbox', { name: 'Minute', exact: true });
  await expect.poll(() => hours.evaluate(element => Math.round(element.scrollTop / 36))).toBe(12);
  await expect.poll(() => minutes.evaluate(element => Math.round(element.scrollTop / 36))).toBe(5);
  await hours.evaluate(element => { element.scrollTop = 14 * 36; });
  await expect(hours.getByRole('option', { name: '14', exact: true })).toHaveAttribute('aria-selected', 'true');
  await minutes.evaluate(element => { element.scrollTop = 29 * 36; });
  await expect(minutes.getByRole('option', { name: '29', exact: true })).toHaveAttribute('aria-selected', 'true');
  await minutes.focus(); await page.keyboard.press('ArrowDown');
  await expect(minutes.getByRole('option', { name: '30', exact: true })).toHaveAttribute('aria-selected', 'true');
  await calendar.screenshot({ path: `/tmp/molr-date-calendar-${info.project.name}.png` });
  await calendar.getByRole('button', { name: 'Apply', exact: true }).click();
  await expect(result).toHaveText('2028-03-05T06:30:00.000Z');
  await expect(calendar).not.toBeVisible();
  await expect(picker.getByRole('button', { name: 'Custom', exact: true })).toContainText('3/5');
  await picker.getByRole('button', { name: 'Custom', exact: true }).click();
  await calendar.getByRole('button', { name: 'Sunday, March 5, 2028', exact: true }).focus();
  await page.keyboard.press('ArrowRight');
  await expect(calendar.getByRole('button', { name: 'Monday, March 6, 2028', exact: true })).toBeFocused();
  await page.keyboard.press('Enter');
  await calendar.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(result).toHaveText('2028-03-05T06:30:00.000Z');
  await picker.getByRole('button', { name: 'Forever', exact: true }).click();
  await expect(result).toHaveText('No expiry');
  await expect(picker.getByRole('button', { name: 'Custom', exact: true })).toHaveText('Custom');
  for (const [label, size, height] of [['Small expiry', 'sm', 32], ['Expiry', 'md', 40], ['Large expiry', 'lg', 48]] as const) {
    const button = demo.getByRole('group', { name: label, exact: true }).getByRole('button', { name: '1 day', exact: true });
    await expect(button).toHaveClass(new RegExp(`button--${size}`));
    await expect(button).toHaveCSS('font-weight', '400');
    expect((await button.boundingBox())!.height).toBeGreaterThanOrEqual(height);
  }
  for (const button of await demo.getByRole('group', { name: 'Disabled expiry', exact: true }).getByRole('button').all()) await expect(button).toBeDisabled();
  await demo.screenshot({ path: `/tmp/molr-date-time-${info.project.name}.png` });
  expect(await demo.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
});

test('inputs validate, retain values, and expose native selection states', async ({ page }) => {
  await category(page, 'Inputs & actions');
  const form = example(page, 'Form fields');
  await form.getByRole('button', { name: 'Save settings' }).click();
  await expect(form.getByRole('textbox', { name: 'Project name', exact: true })).toHaveAttribute('aria-invalid', 'true');
  await expect(form.getByText('Please enter a project name.')).toBeVisible();
  await form.getByRole('textbox', { name: 'Project name', exact: true }).fill('Curious project');
  await form.getByRole('textbox', { name: 'Project description', exact: true }).fill('A little room to explore.');
  await form.getByRole('button', { name: 'Save settings' }).click();
  await expect(form.getByRole('button', { name: 'Saving', exact: true })).toBeDisabled();
  await expect(page.locator('.ui-toast')).toContainText('Settings saved');
  const all = page.getByRole('checkbox', { name: 'Select all files', exact: true });
  await expect(all).toHaveJSProperty('indeterminate', true);
  await all.check();
  await expect(page.getByRole('checkbox', { name: 'Logo source', exact: true })).toBeChecked();
  await expect(all).toHaveJSProperty('indeterminate', false);
  await page.getByRole('checkbox', { name: 'Brand guide', exact: true }).uncheck();
  await expect(all).toHaveJSProperty('indeterminate', true);
  await expect(page.getByRole('checkbox', { name: 'Read-only option' })).toBeDisabled();
  const daily = page.getByRole('radio', { name: 'Daily digest', exact: true });
  await daily.focus(); await page.keyboard.press('ArrowUp');
  await expect(page.getByRole('radio', { name: 'Immediately', exact: true })).toBeChecked();
  await expect(page.getByRole('radio', { name: /Weekly digest/ })).toBeDisabled();
  await page.getByRole('radio', { name: 'Grid', exact: true }).check();
  await choose(page, 'Default view', 'Board');
  await expect(page.getByRole('combobox', { name: 'Default view' })).not.toHaveAttribute('aria-invalid');
  await expect(page.getByRole('combobox', { name: 'Member access' })).toBeDisabled();
  await category(page, 'Navigation'); await category(page, 'Inputs & actions');
  await expect(form.getByRole('textbox', { name: 'Project name', exact: true })).toHaveValue('Curious project');
  await expect(page.getByRole('radio', { name: 'Grid', exact: true })).toBeChecked();
});

test('tabs use one tab stop, skip disabled panels, and pagination respects boundaries', async ({ page }) => {
  await category(page, 'Navigation');
  const tabs = page.getByRole('tablist', { name: 'Project details', exact: true });
  await tabs.getByRole('tab', { name: 'Overview' }).focus();
  await page.keyboard.press('ArrowRight');
  await expect(tabs.getByRole('tab', { name: 'Activity' })).toBeFocused();
  await expect(tabs.getByRole('tab', { name: 'Activity' })).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press('End');
  await expect(tabs.getByRole('tab', { name: 'Settings' })).toBeFocused();
  await expect(tabs.getByRole('tab', { name: 'Planned' })).toBeDisabled();
  await page.keyboard.press('ArrowRight');
  await expect(tabs.getByRole('tab', { name: 'Overview' })).toBeFocused();
  await expect(tabs.locator('[tabindex="0"]')).toHaveCount(1);
  await expect(page.getByRole('navigation', { name: 'Breadcrumb' }).locator('[aria-current="page"]')).toHaveText('Brand exploration');
  const pagination = page.getByRole('navigation', { name: 'Pagination' });
  await expect(pagination.getByRole('button', { name: 'Previous page' })).toBeDisabled();
  await pagination.getByRole('button', { name: 'Next page' }).click();
  await expect(page.getByText('Showing content from page 2.')).toBeVisible();
  for (let i = 0; i < 10; i++) await pagination.getByRole('button', { name: 'Next page' }).click();
  await expect(pagination.getByRole('button', { name: 'Next page' })).toBeDisabled();
});

test('dialog handles validation, keyboard containment, Escape and focus restoration', async ({ page }) => {
  await category(page, 'Overlays');
  const trigger = page.getByRole('button', { name: 'Edit project', exact: true });
  await trigger.click();
  const dialog = page.getByRole('dialog', { name: 'Edit project', exact: true });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('heading')).toBeFocused();
  for (let i = 0; i < 8; i++) {
    await page.keyboard.press('Tab');
    expect(await dialog.evaluate(el => el.contains(document.activeElement))).toBe(true);
  }
  for (let i = 0; i < 8; i++) {
    await page.keyboard.press('Shift+Tab');
    expect(await dialog.evaluate(el => el.contains(document.activeElement))).toBe(true);
  }
  await dialog.getByRole('textbox', { name: 'Project name', exact: true }).fill('');
  await dialog.getByRole('button', { name: 'Save changes' }).click();
  await expect(dialog.getByText('Please enter a project name.')).toBeVisible();
  await dialog.getByRole('textbox', { name: 'Project name', exact: true }).fill('Edited project');
  await dialog.getByRole('button', { name: 'Save changes' }).click();
  await expect(dialog.getByRole('button', { name: 'Saving', exact: true })).toBeDisabled();
  await page.keyboard.press('Escape');
  await expect(dialog).toBeVisible();
  await expect(dialog).not.toBeVisible();
  await expect(trigger).toBeFocused();
  await trigger.click(); await page.keyboard.press('Escape');
  await expect(dialog).not.toBeVisible(); await expect(trigger).toBeFocused();
  await trigger.click();
  await page.mouse.click(3, 3);
  await expect(dialog).not.toBeVisible();
  await category(page, 'Inputs & actions');
  await expect(example(page, 'Form fields').getByRole('textbox', { name: 'Project name', exact: true })).toHaveValue('Edited project');
});

test('drawer supports a menu in the top layer and restores its trigger', async ({ page }) => {
  await category(page, 'Overlays');
  const trigger = page.getByRole('button', { name: 'View details', exact: true });
  await trigger.click();
  const drawer = page.getByRole('dialog', { name: 'Project details', exact: true });
  await expect(drawer).toBeVisible();
  await drawer.getByRole('button', { name: 'More actions' }).click();
  await expect(page.getByRole('menu')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('menu')).not.toBeVisible();
  await expect(drawer).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(drawer).not.toBeVisible(); await expect(trigger).toBeFocused();
});

test('menu navigates enabled actions, opens dialogs and supports undo', async ({ page }) => {
  await category(page, 'Overlays');
  const trigger = example(page, 'Action menus').getByRole('button', { name: 'More actions' });
  await trigger.focus(); await page.keyboard.press('ArrowDown');
  const menu = page.getByRole('menu', { name: 'Project actions' });
  await expect(menu.getByRole('menuitem', { name: /Rename/ })).toBeFocused();
  await page.keyboard.press('ArrowDown'); await page.keyboard.press('ArrowDown');
  await expect(menu.getByRole('menuitem', { name: 'Archive project' })).toBeFocused();
  await page.keyboard.press('Escape'); await expect(trigger).toBeFocused();
  await trigger.click(); await page.keyboard.press('d');
  await expect(menu.getByRole('menuitem', { name: 'Duplicate' })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.locator('.ui-toast')).toContainText('Copy created');
  await trigger.click(); await page.keyboard.press('End'); await page.keyboard.press('Enter');
  const dialog = page.getByRole('dialog', { name: 'Archive this project?' });
  await dialog.getByRole('button', { name: 'Confirm archive' }).click();
  const notification = page.locator('.ui-toast').filter({ hasText: 'Project archived' });
  await expect(notification).toBeVisible();
  await notification.getByRole('button', { name: 'Undo' }).click();
  await expect(notification).not.toBeVisible();
  await expect(page.locator('.ui-toast').filter({ hasText: 'Archive undone' })).toBeVisible();
  await trigger.click(); await page.keyboard.press('Tab');
  await expect(menu).not.toBeVisible();
  expect(await page.evaluate(() => document.activeElement?.tagName)).not.toBe('BODY');
  await trigger.focus(); await page.keyboard.press('F2');
  await expect(page.getByRole('dialog', { name: 'Edit project', exact: true })).toBeVisible();
});

test('popover and tooltip expose content and return keyboard focus', async ({ page }) => {
  await category(page, 'Overlays');
  const trigger = page.getByRole('button', { name: 'Sharing settings' });
  await trigger.click();
  const popover = page.getByRole('dialog', { name: 'Who can view this?' });
  await expect(popover).toBeVisible();
  await popover.getByRole('radio', { name: 'Workspace members' }).check();
  await page.keyboard.press('Escape');
  await expect(popover).not.toBeVisible(); await expect(trigger).toBeFocused();
  await trigger.click(); await expect(popover.getByRole('radio', { name: 'Workspace members' })).toBeChecked();
  await popover.getByRole('button', { name: 'Close' }).click();
  const help = example(page, 'Helpful details').getByRole('button', { name: 'Show help' });
  await help.focus();
  await expect(page.getByRole('tooltip')).toContainText('View the actions available for this project');
  await page.keyboard.press('Escape'); await expect(page.getByRole('tooltip')).not.toBeVisible();
  await expect(help).toBeFocused();
});

test('feedback supports dismissal, retry, loading and measurable progress', async ({ page }) => {
  await category(page, 'Feedback');
  const alerts = example(page, 'Inline messages');
  await alerts.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(alerts.getByText('Connection interrupted', { exact: true })).not.toBeVisible();
  await alerts.getByRole('button', { name: 'Restore message' }).click();
  await alerts.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(alerts.getByRole('button', { name: 'Retry', exact: true })).toBeDisabled();
  await expect(alerts.getByText('Sync incomplete', { exact: true })).not.toBeVisible();
  const loading = example(page, 'Loading & progress');
  await expect(loading.locator('.skeleton-preview')).toHaveAttribute('aria-busy', 'true');
  await loading.getByRole('switch', { name: 'Preview loading state' }).uncheck();
  await expect(loading.locator('.ui-skeleton')).toHaveCount(0);
  await loading.getByRole('button', { name: 'Start demo' }).click();
  const progress = loading.getByRole('progressbar', { name: 'Organizing resources' });
  await expect(progress).not.toHaveAttribute('aria-valuenow', '0');
  await loading.getByRole('button', { name: 'Pause' }).click();
  const paused = await progress.getAttribute('aria-valuenow');
  await page.waitForTimeout(750); await expect(progress).toHaveAttribute('aria-valuenow', paused!);
  await loading.getByRole('button', { name: 'Resume' }).click();
  await expect(progress).not.toHaveAttribute('aria-valuenow', paused!);
  await expect(loading.getByRole('progressbar', { name: 'Looking for new content' })).not.toHaveAttribute('aria-valuenow');
});

test('actionable notifications persist and ordinary timers pause on focus', async ({ page }) => {
  await category(page, 'Feedback');
  await page.clock.install();
  const notifications = example(page, 'Notifications');
  await notifications.getByRole('button', { name: 'Notification with undo' }).click();
  await page.clock.fastForward(60_000);
  const archived = page.locator('.ui-toast').filter({ hasText: 'Project archived' });
  await expect(archived).toBeVisible();
  await archived.getByRole('button', { name: 'Close' }).click();
  await notifications.getByRole('button', { name: 'Success notification' }).click();
  const success = page.locator('.ui-toast').filter({ hasText: 'Settings saved' });
  await success.getByRole('button', { name: 'Close' }).focus();
  await page.clock.fastForward(10_000); await expect(success).toBeVisible();
  await notifications.getByRole('button', { name: 'Success notification' }).focus();
  await page.mouse.move(1, 1);
  await page.clock.fastForward(5100); await expect(success).not.toBeVisible();
});

test('table filters, sorts, selects across pages and opens the selected resource', async ({ page }) => {
  await category(page, 'Data display');
  const table = page.getByRole('table', { name: 'Project resources' });
  await expect(table.locator('tbody tr')).toHaveCount(4);
  await page.getByRole('button', { name: 'Sort by Size' }).click();
  await expect(table.locator('tbody tr').first()).toContainText('64 KB');
  await page.getByRole('checkbox', { name: 'Select all rows on this page', exact: true }).check();
  await expect(page.getByText('4 selected', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Next page' }).click();
  await expect(table.locator('tbody input:checked')).toHaveCount(0);
  await table.locator('tbody').getByRole('checkbox').first().check();
  await expect(page.getByText('5 selected', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Previous page' }).click();
  await expect(table.locator('tbody input:checked')).toHaveCount(4);
  const search = page.getByRole('searchbox', { name: 'Search resources' });
  await search.fill('no-such-resource');
  await expect(table.getByText('No results found')).toBeVisible();
  await page.getByRole('button', { name: 'Clear search', exact: true }).click();
  await expect(search).toBeFocused(); await expect(table.locator('tbody tr')).toHaveCount(4);
  await search.fill('Logo'); await expect(table.locator('tbody tr')).toHaveCount(1);
  await table.getByRole('button', { name: 'View details' }).click();
  await expect(page.getByRole('dialog', { name: 'Logo source' })).toBeVisible();
  await page.keyboard.press('Escape');
  const task = example(page, 'Task cards');
  await task.getByRole('button', { name: 'Start demo' }).click();
  await expect(task.getByRole('progressbar')).not.toHaveAttribute('aria-valuenow', '0');
  await task.getByRole('button', { name: 'Pause' }).click();
  await expect(task.getByText('Paused', { exact: true })).toBeVisible();
});

test('empty states lead to recovery or creation and tasks reach completion', async ({ page }) => {
  await category(page, 'Feedback');
  const empty = example(page, 'Empty states');
  await empty.getByRole('radio', { name: 'No results found', exact: true }).check();
  await empty.getByRole('button', { name: 'Clear filters' }).click();
  await expect(empty.getByRole('heading', { name: 'No projects yet' })).toBeVisible();
  await empty.getByRole('radio', { name: 'Unable to connect', exact: true }).check();
  await empty.getByRole('button', { name: 'Reconnect' }).click();
  await expect(page.locator('.ui-toast')).toContainText('Connection restored');
  await empty.getByRole('button', { name: 'New project' }).click();
  const dialog = page.getByRole('dialog', { name: 'New project', exact: true });
  await expect(dialog.getByRole('textbox', { name: 'Project name', exact: true })).toHaveValue('');
  await dialog.getByRole('textbox', { name: 'Project name', exact: true }).fill('A new exploration');
  await dialog.getByRole('button', { name: 'Save changes' }).click();
  await expect(dialog).not.toBeVisible();
  await category(page, 'Data display');
  await page.clock.install();
  const task = example(page, 'Task cards');
  await task.getByRole('button', { name: 'Start demo' }).click();
  await page.clock.runFor(6500);
  await expect(task.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '100');
  await expect(task.getByText('Complete', { exact: true })).toBeVisible();
  await task.getByRole('button', { name: 'Start again' }).click();
  await expect(task.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '0');
  await task.getByRole('button', { name: 'Pause' }).click();
  await example(page, 'Resource cards').getByRole('button', { name: 'Browse resources' }).click();
  await expect(page.getByRole('table', { name: 'Project resources' })).toBeVisible();
});

test('all categories adapt to language, theme, mobile width and reduced motion', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await choose(page, 'Appearance', 'Dark Dark surfaces with soft text');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  for (const name of ['Foundations', 'Inputs & actions', 'Chat', 'Navigation', 'Feedback', 'Overlays', 'Data display']) {
    await category(page, name);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  }
  await choose(page, 'Language', '简体中文');
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN');
  await expect(page.getByRole('tab', { name: '数据展示', exact: true })).toHaveAttribute('aria-selected', 'true');
  await page.reload();
  await expect(page.getByRole('tablist', { name: '组件分类', exact: true })).toBeVisible();
  await page.setViewportSize({ width: 320, height: 700 });
  for (const name of ['设计基础', '输入与操作', '聊天', '导航', '状态反馈', '弹层', '数据展示']) {
    await page.getByRole('tablist', { name: '组件分类', exact: true }).getByRole('tab', { name, exact: true }).click();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  }
  await page.getByRole('tab', { name: '状态反馈', exact: true }).click();
  await page.emulateMedia({ reducedMotion: 'reduce' });
  expect(await page.locator('.ui-skeleton').first().evaluate(el => getComputedStyle(el).animationName)).toBe('none');
  expect(await page.locator('.ui-progress__track--indeterminate > span').evaluate(el => getComputedStyle(el).animationName)).toBe('none');
  await choose(page, '语言', 'English');
  await expect(page.getByRole('button', { name: 'Success notification' })).toBeVisible();
  expect(errors).toEqual([]);
});

test('range slider shares one track and keeps keyboard bounds accessible when thumbs overlap', async ({ page }) => {
  await category(page, 'Inputs & actions');
  const demo = example(page, 'Range slider');
  const control = demo.getByRole('group', { name: 'Value range', exact: true });
  const lower = control.getByRole('slider', { name: 'Range start', exact: true });
  const upper = control.getByRole('slider', { name: 'Range end', exact: true });
  await expect(control.locator('.ui-range-slider__rail')).toHaveCount(1);
  await expect(lower).toHaveAttribute('aria-valuenow', '20');
  await lower.focus(); await page.keyboard.press('ArrowRight');
  await expect(lower).toHaveAttribute('aria-valuenow', '25');
  await expect(lower).toHaveAttribute('aria-valuetext', '25%');
  await page.keyboard.press('End');
  await expect(lower).toHaveAttribute('aria-valuenow', '80');
  await page.keyboard.press('Tab'); await expect(upper).toBeFocused();
  await page.keyboard.press('ArrowRight'); await expect(upper).toHaveAttribute('aria-valuenow', '85');
  await page.keyboard.press('Home'); await expect(upper).toHaveAttribute('aria-valuenow', '80');
  await page.keyboard.press('End'); await expect(upper).toHaveAttribute('aria-valuenow', '100');
  await lower.focus(); await page.keyboard.press('Home'); await expect(lower).toHaveAttribute('aria-valuenow', '0');
  await demo.getByRole('button', { name: 'Collapse range', exact: true }).click();
  await expect(lower).toHaveAttribute('aria-valuenow', '50'); await expect(upper).toHaveAttribute('aria-valuenow', '50');
  await lower.focus(); await page.keyboard.press('ArrowLeft'); await expect(lower).toHaveAttribute('aria-valuenow', '45');
  await upper.focus(); await page.keyboard.press('ArrowRight'); await expect(upper).toHaveAttribute('aria-valuenow', '55');
  const disabled = demo.getByRole('group', { name: 'Disabled range', exact: true });
  for (const thumb of await disabled.getByRole('slider').all()) await expect(thumb).toBeDisabled();
  const small = demo.getByRole('group', { name: 'Event range · Small', exact: true });
  await small.getByRole('slider', { name: 'First event' }).focus(); await page.keyboard.press('ArrowRight');
  await expect(small.getByRole('slider', { name: 'First event' })).toHaveAttribute('aria-valuenow', '4');
});

test('range slider supports track taps and pointer dragging without crossing endpoints', async ({ page }, info) => {
  await category(page, 'Inputs & actions');
  const demo = example(page, 'Range slider');
  const control = demo.getByRole('group', { name: 'Value range', exact: true });
  const lower = control.getByRole('slider', { name: 'Range start', exact: true });
  const upper = control.getByRole('slider', { name: 'Range end', exact: true });
  await control.scrollIntoViewIfNeeded();
  const [rail, handle] = await readLayout(control.locator('.ui-range-slider__rail'), lower);
  await page.mouse.move(handle.x + handle.width / 2, handle.y + handle.height / 2); await page.mouse.down();
  await page.mouse.move(rail.x + rail.width, rail.y + rail.height / 2, { steps: 8 }); await page.mouse.up();
  await expect(lower).toHaveAttribute('aria-valuenow', '80'); await expect(upper).toHaveAttribute('aria-valuenow', '80');
  await demo.getByRole('button', { name: 'Start again', exact: true }).click();
  await control.scrollIntoViewIfNeeded();
  const currentRail = (await control.locator('.ui-range-slider__rail').boundingBox())!;
  const tap = { x: currentRail.x + currentRail.width * .95, y: currentRail.y + currentRail.height / 2 };
  if (info.project.name === 'mobile') await page.touchscreen.tap(tap.x, tap.y); else await page.mouse.click(tap.x, tap.y);
  await expect(upper).toHaveAttribute('aria-valuenow', '95');
  await expect(lower).toHaveAttribute('aria-valuenow', '20');
  if (process.env.MOLR_CAPTURE_RANGE) await demo.screenshot({ path: `/tmp/molr-range-slider-${info.project.name}.png` });
});

test('code highlighting preserves source, adapts to theme and scrolls without widening the page', async ({ page }) => {
  await category(page, 'Foundations');
  const demo = page.locator('#code-block');
  const block = demo.locator('.ui-code-block');
  const region = demo.getByRole('region', { name: 'Code highlighting example', exact: true });
  await expect(block).toHaveAttribute('data-highlighted', 'true');
  for (const kind of ['property', 'string', 'number', 'boolean', 'null', 'punctuation']) expect(await block.locator(`.ui-code-token--${kind}`).count()).toBeGreaterThan(0);
  const key = block.locator('.ui-code-token--property').first();
  const propertyColor = await key.evaluate(el => getComputedStyle(el).color);
  const stringColor = await block.locator('.ui-code-token--string').first().evaluate(el => getComputedStyle(el).color);
  expect(propertyColor).not.toBe(stringColor);
  await region.focus(); await page.keyboard.press('End');
  await expect.poll(() => region.evaluate(el => el.scrollTop)).toBeGreaterThan(0);
  const currentTheme = await page.locator('html').getAttribute('data-theme');
  await page.getByRole('combobox', { name: 'Appearance', exact: true }).click();
  await page.getByRole('option', { name: currentTheme === 'dark' ? /^Light / : /^Dark / }).click();
  expect(await key.evaluate(el => getComputedStyle(el).color)).not.toBe(propertyColor);
  await demo.getByText('Edit sample content', { exact: true }).click();
  const input = demo.getByRole('textbox', { name: 'Code source', exact: true });
  const unsafe = JSON.stringify({ text: '</code><img src=x onerror="window.codeExecuted=true">', number: 42, nil: null }, null, 2);
  await input.fill(unsafe);
  await expect.poll(() => block.locator('code').textContent()).toBe(unsafe);
  await expect(block.locator('img, script')).toHaveCount(0);
  expect(await page.evaluate(() => (window as typeof window & { codeExecuted?: boolean }).codeExecuted)).toBeUndefined();
  const invalid = '{ "unfinished":';
  await input.fill(invalid);
  await expect(block).toHaveAttribute('data-highlighted', 'false');
  expect(await block.locator('code').textContent()).toBe(invalid);
  const long = JSON.stringify({ message: 'Long diagnostic line '.repeat(200) });
  await input.fill(long);
  await expect(block).toHaveAttribute('data-highlighted', 'true');
  expect(await region.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
  await demo.getByRole('switch', { name: 'Wrap lines', exact: true }).click();
  await expect.poll(() => region.evaluate(el => el.scrollWidth > el.clientWidth)).toBe(true);
  await demo.getByRole('radio', { name: 'Plain text', exact: true }).check();
  await expect(block).toHaveAttribute('data-highlighted', 'false');
  expect(await block.locator('code').textContent()).toBe(long);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
