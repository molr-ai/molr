import { test, expect } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await page.goto('/tests/fixtures/regressions.html');
});

test('toasts remain actionable through modal nesting, reparenting and later popovers', async ({ page }) => {
  await page.getByRole('button', { name: 'Notify', exact: true }).click();
  const notice = page.locator('.ui-toast').filter({ hasText: 'Global notice' });
  await page.getByRole('button', { name: 'Open review modal', exact: true }).click();
  const modal = page.getByRole('dialog', { name: 'Review modal', exact: true });
  await expect(modal.locator('.ui-toasts')).toHaveAttribute('popover', 'manual');
  await notice.getByRole('button', { name: 'Acknowledge notice' }).click();
  await expect(page.locator('html')).toHaveAttribute('data-ack', 'yes');
  await page.getByRole('button', { name: 'Notify inside modal', exact: true }).click();
  await page.getByRole('button', { name: 'Open review nested', exact: true }).click();
  const nested = page.getByRole('dialog', { name: 'Review nested', exact: true });
  await expect(nested.locator('.ui-toast')).toHaveCount(1);
  await notice.getByRole('button', { name: 'Close', exact: true }).focus();
  await page.keyboard.press('Tab');
  expect(await nested.evaluate(element => element.contains(document.activeElement))).toBe(true);
  await notice.getByRole('button', { name: 'Close', exact: true }).focus();
  await page.keyboard.press('Escape');
  await expect(nested).not.toBeVisible();
  await expect(modal.locator('.ui-toast')).toHaveCount(1);
  await modal.getByRole('button', { name: 'Close', exact: true }).first().click();
  await expect(page.locator('body > .ui-toasts .ui-toast')).toHaveCount(1);
  await page.getByRole('button', { name: 'Review trigger', exact: true }).click();
  // Use a native overlay so a floating-position observer cannot move it away
  // and accidentally make this stacking assertion pass without the fix.
  await page.evaluate(() => {
    const cover = document.createElement('div');
    cover.id = 'later-cover'; cover.setAttribute('popover', 'manual');
    Object.assign(cover.style, { position: 'fixed', inset: '0', margin: '0', width: '100vw', height: '100vh', background: 'gray' });
    document.body.append(cover); cover.showPopover();
  });
  await expect(page.locator('#later-cover')).toBeVisible();
  await expect.poll(() => notice.evaluate(element => {
    const button = element.querySelector('button')!; const rect = button.getBoundingClientRect();
    return element.contains(document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2));
  })).toBe(true);
});

test('toast state does not broadcast to consumers or reset elapsed time across modal changes', async ({ page }) => {
  const initial = await page.evaluate(() => window.regression.renders);
  await page.clock.install();
  await page.evaluate(() => window.regression.notify(4000));
  await page.mouse.move(0, 0);
  await page.clock.runFor(1600);
  await page.getByRole('button', { name: 'Open review modal', exact: true }).click();
  await page.clock.runFor(1600);
  await page.getByRole('dialog', { name: 'Review modal', exact: true }).getByRole('button', { name: 'Close', exact: true }).first().click();
  await page.clock.runFor(1000);
  await expect(page.locator('.ui-toast')).toHaveCount(0);
  expect(await page.evaluate(() => window.regression.renders)).toBe(initial);
});

test('scoped toast stays within the preview and remains interactive inside its drawer', async ({ page }) => {
  const preview = page.locator('#preview');
  await page.getByRole('button', { name: 'Notify in preview', exact: true }).click();
  const notice = page.locator('.ui-toast').filter({ hasText: 'Scoped notice' });
  const fits = async () => {
    const inner = await notice.boundingBox(); const outer = await preview.boundingBox();
    expect(inner!.x).toBeGreaterThanOrEqual(outer!.x); expect(inner!.y).toBeGreaterThanOrEqual(outer!.y);
    expect(inner!.x + inner!.width).toBeLessThanOrEqual(outer!.x + outer!.width + 1);
    expect(inner!.y + inner!.height).toBeLessThanOrEqual(outer!.y + outer!.height + 1);
  };
  await fits();
  await page.getByRole('button', { name: 'Open preview modal', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Preview modal' }).locator('.ui-toast')).toHaveCount(1);
  await fits();
  await notice.getByRole('button', { name: 'Scoped action' }).click();
  await expect(notice).toHaveCount(0);
});

test('mixed checkboxes use a single state for DOM, icons and controlled changes', async ({ page }) => {
  for (const label of ['Uncontrolled mixed', 'Controlled mixed']) {
    const input = page.getByRole('checkbox', { name: label, exact: true });
    await expect(input).toBeChecked({ indeterminate: true });
    await input.click();
    await expect(input).toBeChecked();
    expect(await input.evaluate(element => (element as HTMLInputElement).indeterminate)).toBe(false);
    const parent = input.locator('..');
    await expect(parent.locator('.check-field__checked-icon')).toBeVisible();
    await expect(parent.locator('.check-field__mixed-icon')).not.toBeVisible();
  }
  const retained = page.getByRole('checkbox', { name: 'Retained mixed' });
  await retained.click();
  await expect(retained).toBeChecked({ indeterminate: true });
  expect(await retained.evaluate(element => (element as HTMLInputElement).checked)).toBe(false);
});

test('default logo labels describe playing and waiting in both languages', async ({ page }) => {
  const logo = page.locator('#logo button');
  const seek = async (time: number) => {
    await expect.poll(() => logo.locator('object').evaluate(element => !!(element as HTMLObjectElement).getSVGDocument()?.documentElement)).toBe(true);
    await logo.locator('object').evaluate((element, time) => { const svg = (element as HTMLObjectElement).getSVGDocument()!.documentElement as unknown as SVGSVGElement; svg.setCurrentTime(time); svg.pauseAnimations(); }, time);
  };
  await seek(1);
  await expect(logo).toHaveAttribute('aria-label', /playing greeting/);
  await expect(logo).toHaveAttribute('aria-disabled', 'true');
  await seek(3);
  await expect(logo).toHaveAttribute('aria-label', /skip the wait and play looking around/);
  await page.getByRole('button', { name: 'Toggle locale' }).click();
  await expect(logo).toHaveAttribute('aria-label', /跳过等待，播放张望/);
  await seek(1);
  await expect(logo).toHaveAttribute('aria-label', /正在招呼/);
  await expect(page.locator('#custom-logo button')).toHaveAttribute('aria-label', 'Custom identity');
});

test('typing and open popover rerenders keep observers while callbacks stay fresh', async ({ page }) => {
  await page.addInitScript(() => {
    const NativeObserver = window.ResizeObserver;
    (window as any).observerCounts = { composer: 0, popover: 0 };
    window.ResizeObserver = class extends NativeObserver {
      observe(target: Element, options?: ResizeObserverOptions) {
        if (target.matches('#composer textarea')) (window as any).observerCounts.composer++;
        if (target.matches('.ui-popover')) (window as any).observerCounts.popover++;
        super.observe(target, options);
      }
    };
  });
  await page.reload();
  const composer = page.locator('#composer textarea');
  const initial = await page.evaluate(() => (window as any).observerCounts.composer);
  await composer.pressSequentially('Several updates');
  expect(await page.evaluate(() => (window as any).observerCounts.composer)).toBe(initial);
  await composer.fill('words '.repeat(20));
  const originalHeight = await composer.evaluate(element => element.clientHeight);
  await page.locator('#composer').evaluate(element => { element.style.width = '160px'; });
  await expect.poll(() => composer.evaluate(element => element.clientHeight)).toBeGreaterThan(originalHeight);
  expect(await page.evaluate(() => (window as any).observerCounts.composer)).toBe(initial);
  await composer.fill('Long message '.repeat(100));
  await expect(composer).toHaveCSS('height', '180px');
  await composer.fill(''); await expect(composer).toHaveCSS('height', '28px');
  await page.getByRole('button', { name: 'Review trigger' }).click();
  await expect(page.getByRole('dialog', { name: 'Review popover' })).toBeVisible();
  const observers = await page.evaluate(() => (window as any).observerCounts.popover);
  await page.evaluate(() => window.regression.rerender());
  await page.evaluate(() => window.regression.rerender());
  expect(await page.evaluate(() => (window as any).observerCounts.popover)).toBe(observers);
  await page.getByRole('button', { name: 'Review trigger' }).evaluate(element => { element.style.transform = 'translateY(-5000px)'; window.dispatchEvent(new Event('resize')); });
  await expect(page.getByRole('dialog', { name: 'Review popover' })).not.toBeVisible();
  await expect(page.getByRole('status', { name: 'Callback revision' })).toHaveText('2');
});
