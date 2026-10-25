import { expect, test } from '@playwright/test';

// Count only the platform resources shared by the floating lifecycle helpers.
test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('molr-ui-design-locale', 'en');
    const activeObservers = new Set<ResizeObserver>();
    const NativeObserver = window.ResizeObserver;
    window.ResizeObserver = class extends NativeObserver {
      observe(target: Element, options?: ResizeObserverOptions) {
        if (target.matches('.select__popup, .ui-popover')) activeObservers.add(this);
        super.observe(target, options);
      }
      disconnect() { activeObservers.delete(this); super.disconnect(); }
    };
    const listeners = new Map<EventTarget, Map<string, Set<EventListenerOrEventListenerObject>>>();
    const add = EventTarget.prototype.addEventListener, remove = EventTarget.prototype.removeEventListener;
    const tracked = (target: EventTarget, type: string) => (target === window || target === window.visualViewport) && ['resize', 'scroll'].includes(type);
    EventTarget.prototype.addEventListener = function(type, listener, options) {
      if (listener && tracked(this, type)) {
        if (!listeners.has(this)) listeners.set(this, new Map());
        const events = listeners.get(this)!;
        const key = `${type}:${typeof options === 'boolean' ? options : options?.capture ?? false}`;
        if (!events.has(key)) events.set(key, new Set());
        events.get(key)!.add(listener);
      }
      add.call(this, type, listener, options);
    };
    EventTarget.prototype.removeEventListener = function(type, listener, options) {
      if (listener && tracked(this, type)) {
        const key = `${type}:${typeof options === 'boolean' ? options : options?.capture ?? false}`;
        listeners.get(this)?.get(key)?.delete(listener);
      }
      remove.call(this, type, listener, options);
    };
    Object.assign(window, { floatingResources: () => ({
      observers: activeObservers.size,
      listeners: [...listeners.values()].flatMap(events => [...events.values()]).reduce((total, callbacks) => total + callbacks.size, 0),
    }) });
  });
});

for (const kind of ['popover', 'select', 'commands'] as const) {
  test(`${kind} keeps resources while repositioning and releases them on close`, async ({ page }) => {
    if (kind === 'popover') await page.goto('/tests/fixtures/regressions.html');
    else if (kind === 'select') {
      await page.goto('/#components');
      await page.getByRole('tablist', { name: 'Component categories', exact: true }).getByRole('tab', { name: 'Inputs & actions', exact: true }).click();
    } else await page.goto('/?components=chat#command-selector');
    const trigger = kind === 'popover' ? page.getByRole('button', { name: 'Review trigger' })
      : kind === 'select' ? page.getByRole('combobox', { name: 'Choose a resource', exact: true })
      : page.locator('#command-selector').getByRole('button', { name: 'Open selector', exact: true });
    const popup = page.locator(kind === 'popover' ? '.ui-popover:popover-open' : kind === 'select' ? '.select__popup:popover-open' : '.composer-commands:popover-open');
    const resources = () => page.evaluate(() => (window as unknown as { floatingResources: () => { observers: number; listeners: number } }).floatingResources());
    await trigger.scrollIntoViewIfNeeded();
    const baseline = await resources();
    for (let cycle = 0; cycle < 3; cycle++) {
      await trigger.click();
      await expect(popup).toBeVisible();
      const opened = await resources();
      expect(opened.observers).toBe(baseline.observers + 1);
      expect(opened.listeners).toBeGreaterThan(baseline.listeners);
      await page.evaluate(() => {
        window.dispatchEvent(new Event('resize'));
        window.visualViewport?.dispatchEvent(new Event('resize'));
        window.visualViewport?.dispatchEvent(new Event('scroll'));
      });
      if (kind === 'select') {
        const search = page.getByRole('combobox', { name: 'Search Choose a resource', exact: true });
        await search.fill('guide');
        await expect(search).toBeFocused();
      }
      await expect(popup).toBeVisible();
      expect(await resources()).toEqual(opened);
      await page.keyboard.press('Escape');
      await expect(popup).toHaveCount(0);
      await expect.poll(resources).toEqual(baseline);
    }
  });
}
