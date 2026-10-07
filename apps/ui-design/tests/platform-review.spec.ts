import { test, expect } from '@playwright/test';

test.beforeEach(async ({ page }) => { await page.goto('/tests/fixtures/platform-review.html'); });

test('theme readers share one live subscription, one DOM update and teardown', async ({ page }) => {
  await page.addInitScript(() => {
    const metrics = { active: 0, peak: 0, writes: 0 };
    (window as any).themeMetrics = metrics;
    const native = window.matchMedia.bind(window);
    window.matchMedia = query => {
      const result = native(query);
      if (query !== '(prefers-color-scheme: dark)') return result;
      const add = result.addEventListener.bind(result), remove = result.removeEventListener.bind(result);
      result.addEventListener = ((type: string, listener: EventListener) => { if (type === 'change') { metrics.active++; metrics.peak = Math.max(metrics.peak, metrics.active); } add(type, listener); }) as typeof result.addEventListener;
      result.removeEventListener = ((type: string, listener: EventListener) => { if (type === 'change') metrics.active--; remove(type, listener); }) as typeof result.removeEventListener;
      return result;
    };
    const observeRoot = () => new MutationObserver(records => { metrics.writes += records.filter(record => record.attributeName === 'data-theme').length; }).observe(document.documentElement, { attributes: true });
    if (document.documentElement) observeRoot(); else document.addEventListener('DOMContentLoaded', observeRoot, { once: true });
  });
  await page.reload();
  await expect.poll(() => page.evaluate(() => (window as any).themeMetrics.active)).toBe(1);
  expect(await page.evaluate(() => (window as any).themeMetrics.peak)).toBe(1);
  await page.getByRole('button', { name: 'Light A', exact: true }).click();
  const before = await page.evaluate(() => (window as any).themeMetrics.writes);
  await page.getByRole('button', { name: 'Dark B', exact: true }).click();
  for (const reader of ['A', 'B', 'C']) await expect(page.getByRole('status', { name: `Theme ${reader}` })).toHaveText('dark:dark');
  expect(await page.evaluate(() => (window as any).themeMetrics.writes)).toBe(before + 1);
  await page.emulateMedia({ colorScheme: 'light' });
  await page.getByRole('button', { name: 'System C', exact: true }).click();
  await expect(page.getByRole('status', { name: 'Theme A' })).toHaveText('system:light');
  await page.emulateMedia({ colorScheme: 'dark' });
  await expect(page.getByRole('status', { name: 'Theme B' })).toHaveText('system:dark');
  await page.evaluate(() => { localStorage.setItem('molr-theme', 'light'); window.dispatchEvent(new StorageEvent('storage', { key: 'molr-theme' })); });
  await expect(page.getByRole('status', { name: 'Theme C' })).toHaveText('light:light');
  await page.getByRole('button', { name: 'Toggle theme readers' }).click();
  await expect.poll(() => page.evaluate(() => (window as any).themeMetrics.active)).toBe(0);
  await page.getByRole('button', { name: 'Toggle theme readers' }).click();
  await expect.poll(() => page.evaluate(() => (window as any).themeMetrics.active)).toBe(1);
});

test('Select keeps APG first-option ArrowUp while menu opens on its last action', async ({ page }) => {
  const select = page.getByRole('combobox', { name: 'Review selection' });
  await select.focus(); await page.keyboard.press('ArrowUp');
  await expect(page.getByRole('option', { name: 'Alpha', exact: true })).toHaveAttribute('data-active', 'true');
  await page.keyboard.press('Escape');
  const menu = page.getByRole('button', { name: 'Open review menu' });
  await menu.focus(); await page.keyboard.press('ArrowUp');
  await expect(page.getByRole('menuitem', { name: 'Omega' })).toBeFocused();
});

test.describe('locale-independent typeahead', () => {
  test.use({ locale: 'tr-TR' });
  test('English labels match i in a Turkish browser for Select and Menu', async ({ page }) => {
    // Chromium's CDP locale override does not affect this legacy String method
    // on every build; model a Turkish host default with the native tr-TR mapping.
    await page.addInitScript(() => {
      const lower = String.prototype.toLocaleLowerCase;
      String.prototype.toLocaleLowerCase = function (locales?: string | string[]) { return lower.call(this, locales ?? 'tr-TR'); };
    });
    await page.reload();
    expect(await page.evaluate(() => 'I'.toLocaleLowerCase())).toBe('ı');
    const select = page.getByRole('combobox', { name: 'Review selection' });
    await select.focus(); await page.keyboard.press('i');
    await expect(page.getByRole('option', { name: 'India', exact: true })).toHaveAttribute('data-active', 'true');
    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: 'Open review menu' }).click();
    await page.keyboard.press('i');
    await expect(page.getByRole('menuitem', { name: 'India' })).toBeFocused();
  });
});

test('dialog includes native media, summary and contenteditable in its focus cycle', async ({ page }) => {
  await page.getByRole('button', { name: 'Open media dialog' }).click();
  const stops = await page.evaluate(() => (window as any).tabStops());
  for (const id of ['first-field', 'audio', 'video', 'summary', 'editor']) expect(stops).toContain(id);
  for (const id of ['hidden-field', 'not-editable', 'closed-button']) expect(stops).not.toContain(id);
  const dialog = page.getByRole('dialog', { name: 'Media dialog' });
  await page.keyboard.press('Shift+Tab'); await expect(page.locator('#editor')).toBeFocused();
  await page.keyboard.press('Tab'); await expect(dialog.getByRole('button', { name: 'Close', exact: true })).toBeFocused();
  await page.locator('#summary').focus(); await page.keyboard.press('Space');
  expect(await page.evaluate(() => (window as any).tabStops())).toContain('closed-button');
});

test('Logo honors provider paths, local overrides and exported package assets', async ({ page }) => {
  await page.route('**/deployment/brand/**', async route => {
    const response = await page.request.get(route.request().url().replace('/deployment/brand/', '/brand/'));
    await route.fulfill({ response });
  });
  await page.emulateMedia({ reducedMotion: 'no-preference' }); await page.reload();
  await expect(page.locator('#based-logo object')).toHaveAttribute('data', '/deployment/brand/molr-mark-alive.svg');
  await expect(page.locator('#override-logo img')).toHaveAttribute('src', /#\/brand\/molr-wordmark\.svg$/);
  await expect.poll(() => page.getByRole('img', { name: 'Exported package logo' }).evaluate(image => (image as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await expect(page.locator('#based-logo source')).toHaveAttribute('srcset', '/deployment/brand/molr-mark.svg');
  await expect.poll(() => page.locator('#based-logo img').evaluate(image => (image as HTMLImageElement).currentSrc)).toContain('/deployment/brand/molr-mark.svg');
});

test('public overlay helpers allow a consumer-defined contained popup', async ({ page }) => {
  await page.getByRole('button', { name: 'Custom overlay', exact: true }).click();
  const panel = page.getByTestId('custom-panel'); await expect(panel).toBeVisible(); await expect(panel).toHaveText('Contained');
  const outer = await page.locator('#custom-scope').boundingBox(); const inner = await panel.boundingBox();
  expect(inner!.x).toBeGreaterThanOrEqual(outer!.x); expect(inner!.x + inner!.width).toBeLessThanOrEqual(outer!.x + outer!.width);
});
