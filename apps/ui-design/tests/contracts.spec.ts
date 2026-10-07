import { test, expect } from '@playwright/test';

test.beforeEach(async ({ page }) => { await page.goto('/tests/fixtures/'); });

test('standalone primitives preserve native form behavior and provider labels', async ({ page }) => {
  await page.getByRole('radio', { name: 'Manual', exact: true }).check();
  await page.getByRole('button', { name: 'Read form' }).click();
  await expect(page.locator('form output')).toHaveText('[["delivery","b"],["files","included"]]');
  await expect(page.getByRole('combobox', { name: 'Inherited placeholder' })).toContainText('Choose an option');
  await expect(page.getByRole('combobox', { name: 'Appearance' })).toBeVisible();
  await page.getByRole('button', { name: 'Switch locale' }).click();
  await expect(page.getByRole('combobox', { name: 'Inherited placeholder' })).toContainText('请选择');
  await expect(page.getByRole('combobox', { name: '外观模式' })).toBeVisible();
  await page.getByRole('button', { name: 'Switch locale' }).click();
  await page.getByRole('button', { name: 'Open modal' }).click();
  const dialog = page.getByRole('dialog', { name: 'Initial focus' });
  await expect(dialog.getByRole('button', { name: 'Cancel modal' })).toBeFocused();
  for (let i = 0; i < 5; i++) {
    await page.keyboard.press('Shift+Tab');
    expect(await dialog.evaluate(element => element.contains(document.activeElement))).toBe(true);
  }
  await page.keyboard.press('Escape');
  await expect(page.getByRole('button', { name: 'Open modal' })).toBeFocused();
  await page.getByRole('tab', { name: 'First', exact: true }).focus();
  await page.keyboard.press('ArrowDown');
  await expect(page.getByRole('tab', { name: 'Second', exact: true })).toBeFocused();
  await expect(page.getByText('Second panel')).toBeVisible();
  await page.getByRole('button', { name: 'Open controlled details' }).click();
  await expect(page.getByRole('dialog', { name: 'Controlled details' })).toBeVisible();
  await page.getByRole('button', { name: 'Apply details' }).click();
  await expect(page.getByRole('dialog', { name: 'Controlled details' })).not.toBeVisible();
});

test('table excludes disabled rows and progress handles boundary values', async ({ page }) => {
  await page.getByRole('checkbox', { name: 'Select all rows on this page' }).check();
  await expect(page.getByRole('status', { name: 'Selected keys' })).toHaveText('other-page,a');
  await expect(page.getByRole('checkbox', { name: 'Select Unavailable' })).toBeDisabled();
  await page.getByRole('checkbox', { name: 'Select all rows on this page' }).uncheck();
  await expect(page.getByRole('status', { name: 'Selected keys' })).toHaveText('other-page');
  await page.getByRole('button', { name: 'Toggle table loading' }).click();
  await expect(page.getByRole('table')).toHaveAttribute('aria-busy', 'true');
  await expect(page.getByRole('checkbox', { name: 'Select all rows on this page' })).toBeDisabled();
  await expect(page.getByRole('progressbar', { name: 'Loading', exact: true })).toBeVisible();
  await expect(page.getByRole('progressbar', { name: 'Below minimum' })).toHaveAttribute('aria-valuenow', '0');
  await expect(page.getByRole('progressbar', { name: 'Above maximum' })).toHaveAttribute('aria-valuenow', '100');
  await expect(page.getByRole('progressbar', { name: 'Unknown total' })).not.toHaveAttribute('aria-valuenow');
});

test('notifications update in place, dismiss by id and pause in background tabs', async ({ page }) => {
  await page.getByRole('button', { name: 'Create persistent notice' }).click();
  await expect(page.locator('.ui-toast')).toHaveCount(1);
  await page.getByRole('button', { name: 'Update notice' }).click();
  await expect(page.locator('.ui-toast')).toContainText('After update');
  await expect(page.locator('.ui-toast')).toHaveCount(1);
  await page.getByRole('button', { name: 'Dismiss notice' }).click();
  await expect(page.locator('.ui-toast')).toHaveCount(0);
  await page.clock.install();
  await page.getByRole('button', { name: 'Create timed notice' }).click();
  await page.mouse.move(1, 1);
  await page.evaluate(() => { Object.defineProperty(document, 'hidden', { configurable: true, value: true }); document.dispatchEvent(new Event('visibilitychange')); });
  await page.clock.fastForward(20_000);
  await expect(page.locator('.ui-toast')).toContainText('Timed notice');
  await page.evaluate(() => { Object.defineProperty(document, 'hidden', { configurable: true, value: false }); document.dispatchEvent(new Event('visibilitychange')); });
  await page.clock.runFor(50);
  await page.clock.fastForward(3100);
  await expect(page.locator('.ui-toast')).toHaveCount(0);
});

test('nested modals close one layer and closed modal cleanup does not move focus', async ({ page }) => {
  const trigger = page.getByRole('button', { name: 'Open modal', exact: true });
  await trigger.click();
  const parent = page.getByRole('dialog', { name: 'Initial focus', exact: true });
  const childTrigger = parent.getByRole('button', { name: 'Open nested modal' });
  await childTrigger.click();
  const nested = page.getByRole('dialog', { name: 'Nested details' });
  await expect(nested).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(nested).not.toBeVisible();
  await expect(parent).toBeVisible();
  await expect(childTrigger).toBeFocused();
  await childTrigger.click();
  await nested.getByRole('button', { name: 'Finish nested modal' }).click();
  await expect(nested).not.toBeVisible();
  await expect(parent).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(trigger).toBeFocused();
  const unmount = page.getByRole('button', { name: 'Unmount closed modal' });
  await unmount.click();
  await expect(unmount).toBeFocused();
});

test('floating triggers preserve stable callback refs and run cleanup on unmount', async ({ page }) => {
  const section = page.getByRole('region', { name: 'Trigger ref lifecycle' });
  const report = section.getByRole('status', { name: 'Ref state' });
  await section.getByRole('button', { name: 'Read ref state' }).click();
  const initial = JSON.parse((await report.textContent())!);
  expect(initial.active).toBe(3);
  expect(initial.legacy).toBe(true);
  await section.getByRole('button', { name: 'Rerender triggers' }).click();
  await section.getByRole('button', { name: 'Read ref state' }).click();
  expect(JSON.parse((await report.textContent())!)).toEqual(initial);
  await section.getByRole('button', { name: 'Unmount triggers' }).click();
  await section.getByRole('button', { name: 'Read ref state' }).click();
  const final = JSON.parse((await report.textContent())!);
  expect(final.active).toBe(0);
  expect(final.legacy).toBe(false);
});

test('tooltips stay while focused and Escape dismisses the tooltip before its modal', async ({ page }) => {
  await page.getByRole('button', { name: 'Open modal', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Initial focus', exact: true });
  const help = dialog.getByRole('button', { name: 'Modal help', exact: true });
  await help.focus();
  await help.hover();
  const tooltip = page.getByRole('tooltip', { name: 'Help inside a modal' });
  await expect(tooltip).toBeVisible();
  await page.mouse.move(1, 1);
  await page.waitForTimeout(200);
  await expect(tooltip).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(tooltip).not.toBeVisible();
  await expect(dialog).toBeVisible();
  await expect(help).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(dialog).not.toBeVisible();
});
