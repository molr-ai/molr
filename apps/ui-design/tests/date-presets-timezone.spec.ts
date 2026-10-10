import { test, expect } from '@playwright/test';

test.use({ timezoneId: 'UTC' });

for (const scenario of [
  { name: 'spring day', now: '2026-03-07T17:00:45.123Z', zone: 'America/New_York', preset: '1 day', expected: '2026-03-08T16:00:45.123Z' },
  { name: 'autumn day', now: '2026-10-31T16:00:00Z', zone: 'America/New_York', preset: '1 day', expected: '2026-11-01T17:00:00.000Z' },
  { name: 'spring week', now: '2026-03-01T17:00:00Z', zone: 'America/New_York', preset: '7 days', expected: '2026-03-08T16:00:00.000Z' },
  { name: 'autumn week', now: '2026-10-25T16:00:00Z', zone: 'America/New_York', preset: '7 days', expected: '2026-11-01T17:00:00.000Z' },
  { name: 'month end in target zone', now: '2026-01-30T16:30:00Z', zone: 'Asia/Tokyo', preset: '1 month', expected: '2026-02-27T16:30:00.000Z' },
  { name: 'leap month in target zone', now: '2028-01-30T16:30:00Z', zone: 'Asia/Tokyo', preset: '1 month', expected: '2028-02-28T16:30:00.000Z' },
  { name: 'year rollover', now: '2026-12-30T16:30:00Z', zone: 'Asia/Tokyo', preset: '1 month', expected: '2027-01-30T16:30:00.000Z' },
]) {
  test(`date preset uses specified timezone: ${scenario.name}`, async ({ page }) => {
    await page.clock.setFixedTime(new Date(scenario.now));
    await page.goto(`/tests/fixtures/date-presets.html?zone=${scenario.zone}`);
    await page.getByRole('button', { name: scenario.preset, exact: true }).click();
    await expect(page.getByLabel('Selected instant', { exact: true })).toHaveText(scenario.expected);
    await expect(page.getByLabel('Changes', { exact: true })).toHaveText('1');
    await expect(page.getByRole('button', { name: scenario.preset, exact: true })).toHaveAttribute('aria-pressed', 'true');
  });
}

test('nonexistent preset time keeps the saved value and recovers with another choice', async ({ page }) => {
  await page.clock.setFixedTime(new Date('2026-03-07T07:30:00Z'));
  const saved = '2026-04-01T12:00:00.000Z';
  await page.goto(`/tests/fixtures/date-presets.html?zone=America/New_York&value=${encodeURIComponent(saved)}`);
  await page.getByRole('button', { name: '1 day', exact: true }).click();
  await expect(page.getByRole('alert')).toHaveText('This time does not exist in the selected timezone');
  await expect(page.getByRole('group', { name: 'Expiry', exact: true })).toHaveAttribute('aria-invalid', 'true');
  await expect(page.getByLabel('Selected instant', { exact: true })).toHaveText(saved);
  await expect(page.getByLabel('Changes', { exact: true })).toHaveText('0');
  await page.getByRole('button', { name: '7 days', exact: true }).click();
  await expect(page.getByLabel('Selected instant', { exact: true })).toHaveText('2026-03-14T06:30:00.000Z');
  await expect(page.getByRole('alert')).toHaveCount(0);
  await page.getByRole('button', { name: 'Forever', exact: true }).click();
  await expect(page.getByLabel('Selected instant', { exact: true })).toHaveText('none');
  await expect(page.getByLabel('Changes', { exact: true })).toHaveText('2');
});

test('changing timezone clears a stale shortcut error and uses the new zone', async ({ page }) => {
  await page.clock.setFixedTime(new Date('2026-03-07T07:30:00Z'));
  await page.goto('/tests/fixtures/date-presets.html?zone=America/New_York');
  await page.getByRole('button', { name: '1 day', exact: true }).click();
  await expect(page.getByRole('alert')).toBeVisible();
  await page.getByRole('combobox', { name: 'Timezone', exact: true }).click();
  await page.getByRole('option', { name: 'UTC', exact: true }).click();
  await expect(page.getByRole('alert')).toHaveCount(0);
  await page.getByRole('button', { name: '1 day', exact: true }).click();
  await expect(page.getByLabel('Selected instant', { exact: true })).toHaveText('2026-03-08T07:30:00.000Z');
});

test.describe('implicit browser timezone', () => {
  test.use({ timezoneId: 'America/New_York' });
  test('calendar shortcuts still follow the browser when timeZone is omitted', async ({ page }) => {
    await page.clock.setFixedTime(new Date('2026-03-07T17:00:00Z'));
    await page.goto('/tests/fixtures/date-presets.html?zone=local');
    await page.getByRole('button', { name: '1 day', exact: true }).click();
    await expect(page.getByLabel('Selected instant', { exact: true })).toHaveText('2026-03-08T16:00:00.000Z');
  });
});
