import { expect, test } from '@playwright/test';

for (const browser of [false, true]) {
  test(`${browser ? 'browser' : 'App'} navigation remains clickable while a tooltip is open`, async ({ page }, info) => {
    await page.goto(`/tests/fixtures/app-tooltip.html${browser ? '?browser' : ''}`);
    const desktopApp = !browser && info.project.name === 'desktop';
    const nav = page.getByRole('navigation');
    await expect(nav).toHaveCSS('app-region', desktopApp ? 'drag' : 'none');
    for (const name of ['Plans', 'Settings', 'Assistants']) {
      const button = nav.getByRole('button', { name, exact: true });
      await button.hover();
      const tooltip = page.getByRole('tooltip', { name, exact: true });
      await expect(tooltip).toBeVisible();
      // Browser clicks bypass native window hit testing. Check the regions that
      // Electron uses too: an inherited drag backdrop covers the entire window.
      await expect(tooltip).toHaveCSS('app-region', browser ? 'none' : 'no-drag');
      await expect.poll(() => tooltip.evaluate(el => getComputedStyle(el, '::backdrop').getPropertyValue('app-region'))).toBe('none');
      await button.click();
      await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
      await expect(tooltip).not.toBeVisible();
    }
    if (!browser) await expect(page.locator('.app-titlebar')).toHaveCSS('app-region', 'drag');
    await expect(nav).toHaveCSS('app-region', desktopApp ? 'drag' : 'none');
  });
}

test('keyboard navigation and hovering tooltip content preserve App navigation', async ({ page }) => {
  await page.goto('/tests/fixtures/app-tooltip.html');
  await page.getByRole('button', { name: 'Assistants', exact: true }).focus();
  await page.keyboard.press('Tab');
  const plans = page.getByRole('button', { name: 'Plans', exact: true });
  await expect(plans).toBeFocused();
  const tooltip = page.getByRole('tooltip', { name: 'Plans', exact: true });
  await expect(tooltip).toBeVisible();
  await tooltip.hover();
  await expect(tooltip).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(tooltip).not.toBeVisible();
  await expect(plans).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('heading', { name: 'Plans', exact: true })).toBeVisible();
});
