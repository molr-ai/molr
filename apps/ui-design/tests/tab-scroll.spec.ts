import { test, expect } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';

async function metrics(tabs: Locator) {
  return tabs.evaluate(el => {
    const items = [...el.querySelectorAll<HTMLElement>(':scope > [role="tab"]')];
    const index = items.findIndex(item => item.getAttribute('aria-selected') === 'true');
    const selected = items[index]!.getBoundingClientRect();
    const left = el.getBoundingClientRect().left + el.clientLeft;
    const right = left + el.clientWidth;
    const visibleWidth = (item?: HTMLElement) => {
      if (!item) return 0;
      const bounds = item.getBoundingClientRect();
      return Math.max(0, Math.min(right, bounds.right) - Math.max(left, bounds.left));
    };
    return {
      scroll: el.scrollLeft, max: el.scrollWidth - el.clientWidth,
      visible: selected.left >= left - 1 && selected.right <= right + 1,
      previous: visibleWidth(items[index - 1]), next: visibleWidth(items[index + 1]),
      scrollbar: getComputedStyle(el).scrollbarWidth,
    };
  });
}

async function settingsTabs(page: Page) {
  await page.goto('/#interfaces');
  await page.locator('#interfaces').getByRole('tab', { name: 'Mobile', exact: true }).click();
  const shell = page.locator('.interface-preview.ip-shell:visible');
  await shell.getByRole('navigation', { name: 'Workspace navigation' }).getByRole('button', { name: 'Settings', exact: true }).click();
  const tabs = shell.getByRole('tablist', { name: 'Settings sections' });
  await tabs.evaluate(el => { el.style.width = '200px'; });
  return tabs;
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('molr-ui-design-locale', 'en'));
});

test('mobile tabs hide scrollbars, reveal adjacent choices, and pin first and last tabs', async ({ page }) => {
  const tabs = await settingsTabs(page);
  const selected = tabs.locator('[aria-selected="true"]');
  await selected.focus();
  await selected.press('Home');
  await expect.poll(async () => (await metrics(tabs)).scroll).toBe(0);
  await expect(tabs).toHaveCSS('scrollbar-width', 'none');
  await selected.press('ArrowRight');
  await expect.poll(async () => (await metrics(tabs)).scroll).toBe(0);
  await selected.press('ArrowRight');
  await expect.poll(async () => (await metrics(tabs)).visible).toBe(true);
  await expect.poll(async () => (await metrics(tabs)).next).toBeCloseTo(12, 0);
  await selected.press('End');
  await expect.poll(async () => { const m = await metrics(tabs); return Math.abs(m.scroll - m.max); }).toBeLessThan(1);
  for (let i = 0; i < 4; i++) await selected.press('ArrowLeft');
  await expect(selected).toHaveText('Preferences');
  await expect.poll(async () => (await metrics(tabs)).visible).toBe(true);
  await expect.poll(async () => (await metrics(tabs)).previous).toBeCloseTo(12, 0);

  // Swiping away and tapping the same selected tab must reveal it again.
  await tabs.evaluate(el => {
    const current = el.querySelector('[aria-selected="true"]')!.getBoundingClientRect();
    el.scrollLeft += current.left - el.getBoundingClientRect().left + 8;
  });
  const box = (await tabs.boundingBox())!;
  await page.mouse.click(box.x + 12, box.y + box.height / 2);
  await expect(selected).toHaveText('Preferences');
  await expect.poll(async () => (await metrics(tabs)).visible).toBe(true);
});

test('custom interface tabs scroll to their selected edge without moving the page horizontally', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/#interfaces');
  const tabs = page.getByRole('tablist', { name: 'Interface versions' });
  await tabs.evaluate(el => { el.style.width = '240px'; });
  await tabs.getByRole('tab', { name: 'Web', exact: true }).focus();
  await page.keyboard.press('End');
  await expect.poll(async () => { const m = await metrics(tabs); return Math.abs(m.scroll - m.max); }).toBeLessThan(1);
  await page.keyboard.press('Home');
  await expect.poll(async () => (await metrics(tabs)).scroll).toBe(0);
  await page.keyboard.press('ArrowRight');
  await page.keyboard.press('ArrowRight');
  await expect.poll(async () => (await metrics(tabs)).visible).toBe(true);
  await expect.poll(async () => (await metrics(tabs)).next).toBeCloseTo(12, 0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('desktop tabs wrap and reveal the current selection when resized to mobile', async ({ page }) => {
  await page.setViewportSize({ width: 1360, height: 1000 });
  await page.goto('/#components');
  const tabs = page.getByRole('tablist', { name: 'Component categories' });
  await tabs.evaluate(el => { el.style.width = '240px'; });
  await expect(tabs).toHaveCSS('flex-wrap', 'wrap');
  await tabs.getByRole('tab').first().focus();
  await page.keyboard.press('End');
  expect((await metrics(tabs)).scroll).toBe(0);
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(tabs).toHaveCSS('flex-wrap', 'nowrap');
  await expect.poll(async () => (await metrics(tabs)).visible).toBe(true);
  await expect.poll(async () => { const m = await metrics(tabs); return Math.abs(m.scroll - m.max); }).toBeLessThan(1);
});

test('automatic positioning slides unless reduced motion is requested', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  const tabs = await settingsTabs(page);
  await tabs.locator('[aria-selected="true"]').focus();
  await page.keyboard.press('Home');
  await expect.poll(async () => (await metrics(tabs)).scroll).toBe(0);

  async function samples(edge: 'first' | 'last') {
    return tabs.evaluate(async (el, edge) => {
      const values = [el.scrollLeft];
      const maximum = el.scrollWidth - el.clientWidth;
      const target = edge === 'first' ? 0 : maximum;
      const items = el.querySelectorAll<HTMLButtonElement>(':scope > [role="tab"]');
      (edge === 'first' ? items[0]! : items[items.length - 1]!).click();
      await new Promise<void>(resolve => {
        const deadline = performance.now() + 2000;
        function sample() {
          values.push(el.scrollLeft);
          if (Math.abs(el.scrollLeft - target) < 1 || performance.now() > deadline) resolve();
          else requestAnimationFrame(sample);
        }
        requestAnimationFrame(sample);
      });
      return { values, maximum, target };
    }, edge);
  }

  const animated = await samples('last');
  expect(animated.values.some(value => value > 1 && value < animated.maximum - 1)).toBe(true);
  expect(animated.values.at(-1)).toBeCloseTo(animated.target, 0);
  await page.emulateMedia({ reducedMotion: 'reduce' });
  const reduced = await samples('first');
  expect(reduced.values.some(value => value > 1 && value < reduced.maximum - 1)).toBe(false);
  expect(reduced.values.at(-1)).toBe(0);
});

test('a new selection stops an outdated slide when the new tab is already visible', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  const tabs = await settingsTabs(page);
  await tabs.evaluate(async el => {
    const items = el.querySelectorAll<HTMLButtonElement>(':scope > [role="tab"]');
    items[items.length - 1]!.click();
    await new Promise<void>(resolve => {
      const deadline = performance.now() + 2000;
      function waitForMotion() {
        if (el.scrollLeft > 0 || performance.now() > deadline) resolve();
        else requestAnimationFrame(waitForMotion);
      }
      requestAnimationFrame(waitForMotion);
    });
    items[1]!.click();
    // Allow the old animation enough time to finish if it was not cancelled.
    await new Promise(resolve => setTimeout(resolve, 700));
  });
  await expect(tabs.locator('[aria-selected="true"]')).toHaveText('Memory');
  expect((await metrics(tabs)).visible).toBe(true);
});
