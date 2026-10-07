import { test, expect } from '@playwright/test';
import type { Locator } from '@playwright/test';

async function freezeMotion(drawer: Locator, progress = 0.5) {
  return drawer.evaluate((element, fraction) => {
    const animations = element.getAnimations({ subtree: true }).filter(animation => animation.id.startsWith('molr-drawer-'));
    const slide = animations.find(animation => animation.id === 'molr-drawer-slide');
    if (!slide) throw new Error('Drawer slide animation is missing');
    for (const animation of animations) {
      animation.pause();
      animation.currentTime = Number(animation.effect!.getTiming().duration) * fraction;
    }
    return {
      x: new DOMMatrixReadOnly(getComputedStyle(element).transform).m41,
      y: new DOMMatrixReadOnly(getComputedStyle(element).transform).m42,
      width: element.getBoundingClientRect().width,
      opacity: Number(getComputedStyle(element, '::backdrop').opacity),
    };
  }, progress);
}

async function finishMotion(drawer: Locator) {
  await drawer.evaluate(element => {
    for (const animation of element.getAnimations({ subtree: true })) {
      if (animation.id.startsWith('molr-drawer-')) animation.finish();
    }
  });
}

test.beforeEach(async ({ page }) => { await page.goto('/tests/fixtures/'); });

test('drawer has square edges and slides both ways before releasing the modal', async ({ page }) => {
  const trigger = page.getByRole('button', { name: 'Open motion drawer' });
  const drawer = page.locator('.ui-drawer');
  await trigger.click();
  const entering = await freezeMotion(drawer);
  expect(entering.x).toBeGreaterThan(0);
  expect(entering.x).toBeLessThan(entering.width);
  expect(entering.y).toBe(0);
  expect(entering.opacity).toBeGreaterThan(0);
  expect(entering.opacity).toBeLessThan(1);
  expect(await drawer.evaluate(element => {
    const style = getComputedStyle(element);
    return [style.borderTopLeftRadius, style.borderTopRightRadius, style.borderBottomLeftRadius, style.borderBottomRightRadius];
  })).toEqual(['0px', '0px', '0px', '0px']);
  await finishMotion(drawer);
  await expect(drawer).toHaveCSS('transform', 'none');
  const bounds = await drawer.boundingBox();
  const viewport = page.viewportSize()!;
  expect(bounds!.y).toBe(0);
  expect(bounds!.x + bounds!.width).toBeCloseTo(viewport.width, 0);
  expect(bounds!.height).toBeCloseTo(viewport.height, 0);

  await drawer.getByRole('button', { name: 'Close', exact: true }).click();
  const leaving = await freezeMotion(drawer);
  expect(leaving.x).toBeGreaterThan(0);
  expect(leaving.x).toBeLessThan(leaving.width);
  expect(leaving.opacity).toBeGreaterThan(0);
  expect(leaving.opacity).toBeLessThan(1);
  expect(await drawer.evaluate(element => element.matches(':modal'))).toBe(true);
  await expect(page.locator('html')).toHaveCSS('overflow', 'hidden');
  await expect(drawer).toHaveAttribute('open');
  await finishMotion(drawer);
  await expect(drawer).not.toBeVisible();
  await expect(trigger).toBeFocused();
  await expect(page.locator('html')).not.toHaveCSS('overflow', 'hidden');
});

test('drawer reverses from its current position and cancels motion on unmount', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.clock.install();
  const trigger = page.getByRole('button', { name: 'Open motion drawer' });
  const drawer = page.locator('.ui-drawer');
  await trigger.click();
  await freezeMotion(drawer);
  await finishMotion(drawer);
  await drawer.getByRole('button', { name: 'Close and reopen' }).click();
  const interrupted = await freezeMotion(drawer);
  await page.clock.runFor(70);
  await expect(drawer).toHaveAttribute('data-state', 'open');
  const start = await drawer.evaluate(element => {
    const animation = element.getAnimations().find(animation => animation.id === 'molr-drawer-slide')!;
    const frame = (animation.effect as KeyframeEffect).getKeyframes()[0];
    return new DOMMatrixReadOnly(String(frame.transform)).m41;
  });
  expect(start).toBeCloseTo(interrupted.x, 1);
  await freezeMotion(drawer);
  await expect(drawer).toHaveAttribute('open');
  await drawer.getByRole('button', { name: 'Unmount drawer' }).click();
  await expect(drawer).toHaveCount(0);
  await expect(trigger).toBeFocused();
  await page.clock.runFor(1000);
  await trigger.click();
  await freezeMotion(drawer);
  await finishMotion(drawer);
  await expect(drawer).toHaveAttribute('open');
  expect(errors).toEqual([]);
});

test('drawer skips motion when requested and settles an in-flight exit', async ({ page }) => {
  const trigger = page.getByRole('button', { name: 'Open motion drawer' });
  const drawer = page.locator('.ui-drawer');
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await trigger.click();
  expect(await drawer.evaluate(element => element.getAnimations({ subtree: true }).filter(animation => animation.id.startsWith('molr-drawer-')).length)).toBe(0);
  await expect(drawer).toHaveCSS('transform', 'none');
  await page.keyboard.press('Escape');
  await expect(drawer).not.toBeVisible();
  await expect(trigger).toBeFocused();

  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await trigger.click();
  await freezeMotion(drawer);
  await finishMotion(drawer);
  await page.keyboard.press('Escape');
  await freezeMotion(drawer);
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await expect(drawer).not.toBeVisible();
  await expect(trigger).toBeFocused();
  await expect(page.locator('html')).not.toHaveCSS('overflow', 'hidden');
});
