import { test, expect } from '@playwright/test';

test('closed tooltips share document input work and clean up on remount', async ({ page }) => {
  await page.addInitScript(() => {
    const add = EventTarget.prototype.addEventListener, remove = EventTarget.prototype.removeEventListener;
    const wrappers = new WeakMap<object, EventListener>();
    const state = window as any;
    state.tooltipCallbacks = { keydown: 0, pointerdown: 0 };
    EventTarget.prototype.addEventListener = function(type, listener, options) {
      if (this === document && ['keydown', 'pointerdown'].includes(type) && listener) {
        let wrapped = wrappers.get(listener);
        if (!wrapped) {
          wrapped = function(this: EventTarget, event) {
            state.tooltipCallbacks[event.type]++;
            if (typeof listener === 'function') listener.call(this, event);
            else listener.handleEvent(event);
          };
          wrappers.set(listener, wrapped);
        }
        return add.call(this, type, wrapped, options);
      }
      return add.call(this, type, listener, options);
    };
    EventTarget.prototype.removeEventListener = function(type, listener, options) {
      return remove.call(this, type, this === document && ['keydown', 'pointerdown'].includes(type) && listener ? wrappers.get(listener) ?? listener : listener, options);
    };
  });
  await page.goto('/tests/fixtures/tooltip-scale.html');
  const input = page.getByRole('textbox', { name: 'Type here' });
  for (let cycle = 0; cycle < 2; cycle++) {
    await expect(page.getByRole('button', { name: /^Item / })).toHaveCount(500);
    await page.evaluate(() => { (window as any).tooltipCallbacks = { keydown: 0, pointerdown: 0 }; });
    await input.click();
    await page.keyboard.press('a');
    const callbacks = await page.evaluate(() => (window as any).tooltipCallbacks);
    await test.info().attach(`input-work-${cycle}.json`, { body: JSON.stringify({ tooltips: 500, callbacks }), contentType: 'application/json' });
    expect(callbacks.keydown).toBeLessThanOrEqual(2);
    expect(callbacks.pointerdown).toBeLessThanOrEqual(2);
    await page.getByRole('button', { name: 'Toggle tooltips' }).click();
    await expect(page.getByRole('button', { name: /^Item / })).toHaveCount(0);
    await page.evaluate(() => { (window as any).tooltipCallbacks = { keydown: 0, pointerdown: 0 }; });
    await input.click();
    await page.keyboard.press('b');
    expect(await page.evaluate(() => (window as any).tooltipCallbacks)).toEqual({ keydown: 0, pointerdown: 0 });
    if (!cycle) await page.getByRole('button', { name: 'Toggle tooltips' }).click();
  }
});


test('click-pinned tooltips recover after suspension and dismiss outside', async ({ page }) => {
  await page.goto('/tests/fixtures/tooltip-scale.html');
  const button = page.getByRole('button', { name: 'Item 0', exact: true });
  const tip = page.getByRole('tooltip', { name: 'Help 0', exact: true });
  await button.click();
  await expect(tip).toBeVisible();
  await page.mouse.move(0, 0);
  await expect(tip).toBeVisible();
  await page.evaluate(() => { window.dispatchEvent(new Event('blur')); window.dispatchEvent(new Event('focus')); });
  await expect(tip).not.toBeVisible();
  await button.click();
  await expect(tip).toBeVisible();
  await page.getByRole('textbox', { name: 'Type here' }).click();
  await expect(tip).not.toBeVisible();
  await button.click();
  await expect(tip).toBeVisible();
  await button.click();
  await expect(tip).not.toBeVisible();
});
