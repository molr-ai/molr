import { test, expect } from '@playwright/test';

test('skeleton gallery shares neutral theme colors and replaces placeholders in place', async ({ page }) => {
  await page.goto('/#components');
  await page.getByRole('tablist', { name: 'Component categories', exact: true }).getByRole('tab', { name: 'Skeletons', exact: true }).click();
  const gallery = page.locator('.skeleton-showcase');
  const list = gallery.locator('.skeleton-showcase__list');
  const card = gallery.locator('.skeleton-showcase__card');
  await expect(list).toHaveAttribute('aria-busy', 'true');
  await expect(list.locator('.ui-skeleton')).toHaveCount(9);
  for (const [theme, base, highlight] of [
    ['light', 'rgba(96, 96, 96, 0.11)', 'rgba(96, 96, 96, 0.18)'],
    ['dark', 'rgba(160, 160, 160, 0.2)', 'rgba(160, 160, 160, 0.28)'],
  ] as const) {
    await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme);
    const background = await gallery.locator('.ui-skeleton').first().evaluate(el => getComputedStyle(el).backgroundImage);
    expect(background).toContain(base);
    expect(background).toContain(highlight);
  }
  const before = await card.boundingBox();
  await gallery.getByRole('switch', { name: 'Preview loading state' }).click();
  await expect(list).toHaveAttribute('aria-busy', 'false');
  await expect(list.locator('.ui-skeleton')).toHaveCount(0);
  await expect(list.getByText('Daily assistant', { exact: true })).toBeVisible();
  await expect(card.getByText('Project resources', { exact: true })).toBeVisible();
  expect((await card.boundingBox())?.height).toBe(before?.height);
  await gallery.getByRole('switch', { name: 'Preview loading state' }).click();
  await expect(list).toHaveAttribute('aria-busy', 'true');
  await page.emulateMedia({ reducedMotion: 'reduce' });
  expect(await gallery.locator('.ui-skeleton').first().evaluate(el => getComputedStyle(el).animationName)).toBe('none');
  await page.setViewportSize({ width: 320, height: 700 });
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
