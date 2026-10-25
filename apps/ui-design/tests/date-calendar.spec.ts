import { test, expect } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.goto('/tests/fixtures/date-calendar.html');
});

test('time edits reuse calendar formatting and day eligibility work', async ({ page }) => {
  await page.evaluate(() => {
    const counts = { constructors: 0, labels: 0, wall: 0 };
    Object.assign(window, { calendarFormats: counts });
    const prototype = Intl.DateTimeFormat.prototype;
    const format = Object.getOwnPropertyDescriptor(prototype, 'format')!.get!;
    Object.defineProperty(prototype, 'format', { configurable: true, get() {
      const bound = format.call(this);
      return (date: Date | number) => { counts.labels++; return bound(date); };
    } });
    const formatToParts = prototype.formatToParts;
    prototype.formatToParts = function (...args) { counts.wall++; return formatToParts.apply(this, args); };
    Intl.DateTimeFormat = new Proxy(Intl.DateTimeFormat, {
      construct(target, args) { counts.constructors++; return Reflect.construct(target, args); },
    });
  });
  const readCounts = () => page.evaluate(() => (window as unknown as { calendarFormats: { constructors: number; labels: number; wall: number } }).calendarFormats);
  const grid = page.getByRole('grid');
  await expect(grid.getByRole('button', { name: 'Monday, March 9, 2026', exact: true })).toBeDisabled();
  for (const [name, expected] of [['Hour', '13'], ['Minute', '31']]) {
    const before = await readCounts();
    const wheel = page.getByRole('listbox', { name, exact: true });
    await wheel.press('ArrowDown');
    await expect(wheel.getByRole('option', { name: expected, exact: true })).toHaveAttribute('aria-selected', 'true');
    const after = await readCounts();
    expect(after.labels - before.labels).toBe(0);
    expect(after.constructors - before.constructors).toBeLessThan(10);
    expect(after.wall - before.wall).toBeLessThan(20);
  }
  await page.getByRole('button', { name: 'Apply', exact: true }).click();
  await expect(page.getByLabel('Selected instant')).toHaveText('2026-03-15T13:31:00.000Z');
});

test('minimum and timezone changes refresh eligibility; locale refreshes labels', async ({ page }) => {
  const day = (date: string) => page.locator(`[data-date="${date}"]`);
  await expect(day('2026-2-9')).toBeDisabled();
  await page.getByLabel('Timezone', { exact: true }).selectOption('America/New_York');
  await expect(day('2026-2-9')).toBeEnabled();
  await page.getByLabel('Minimum', { exact: true }).fill('2026-03-11T04:00:00Z');
  await expect(day('2026-2-10')).toBeDisabled();
  await expect(day('2026-2-11')).toBeEnabled();
  await page.getByLabel('Minimum', { exact: true }).fill('');
  await expect(day('2026-2-9')).toBeEnabled();
  await page.getByRole('button', { name: 'Toggle language' }).click();
  await expect(day('2026-2-15')).toHaveAttribute('aria-label', '2026年3月15日星期日');
  await expect(page.getByRole('columnheader').first()).toHaveText('一');
  await expect(page.getByRole('combobox', { name: '月份', exact: true })).toContainText('3月');
});

test('keyboard navigation checks dates beyond the visible grid and refreshes the month', async ({ page }) => {
  const selected = page.locator('[data-date="2026-2-15"]');
  await selected.focus();
  await selected.press('PageUp');
  await expect(selected).toBeFocused(); // February 15 is before the minimum.
  await selected.press('PageDown');
  const next = page.locator('[data-date="2026-3-15"]');
  await expect(next).toBeFocused();
  await expect(next).toHaveAttribute('aria-label', 'Wednesday, April 15, 2026');
  await next.press('Enter');
  await page.getByRole('button', { name: 'Apply', exact: true }).click();
  await expect(page.getByLabel('Selected instant')).toHaveText('2026-04-15T12:30:00.000Z');
});

test('time edits still validate the minimum and nonexistent DST times', async ({ page }) => {
  const apply = page.getByRole('button', { name: 'Apply', exact: true });
  const hours = page.getByRole('listbox', { name: 'Hour', exact: true });
  await page.getByLabel('Minimum', { exact: true }).fill('2026-03-15T13:30:00Z');
  await expect(apply).toBeDisabled();
  await hours.press('ArrowDown');
  await expect(apply).toBeEnabled();
  await page.getByLabel('Minimum', { exact: true }).fill('');
  await page.getByLabel('Timezone', { exact: true }).selectOption('America/New_York');
  await page.locator('[data-date="2026-2-8"]').click();
  await hours.press('Home');
  await hours.press('ArrowDown');
  await hours.press('ArrowDown');
  await expect(page.getByRole('alert')).toHaveText('This time does not exist in the selected timezone');
  await expect(apply).toBeDisabled();
  await hours.press('ArrowDown');
  await expect(apply).toBeEnabled();
  await apply.click();
  await expect(page.getByLabel('Selected instant')).toHaveText('2026-03-08T07:30:00.000Z');
});

for (const mode of ['time', 'month']) {
  test(`${mode} mode skips timezone checks for the hidden day grid`, async ({ page }) => {
    await page.addInitScript(() => {
      const counts = { wall: 0 };
      Object.assign(window, { calendarFormats: counts });
      const formatToParts = Intl.DateTimeFormat.prototype.formatToParts;
      Intl.DateTimeFormat.prototype.formatToParts = function (...args) { counts.wall++; return formatToParts.apply(this, args); };
    });
    await page.goto(`/tests/fixtures/date-calendar.html?mode=${mode}`);
    await expect(page.getByRole('button', { name: 'Apply', exact: true })).toBeVisible();
    await expect(page.getByRole('grid')).toHaveCount(0);
    expect(await page.evaluate(() => (window as unknown as { calendarFormats: { wall: number } }).calendarFormats.wall)).toBeLessThan(10);
  });
}
