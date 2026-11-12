import { test as base } from '@playwright/test';

export { expect } from '@playwright/test';
export type { Locator } from '@playwright/test';

export const test = base.extend({
  context: async ({ context }, use) => {
    // Native focus scrolling can move click targets between pointer down/up.
    // Only stabilize the document: component animations and explicit scrolling remain testable.
    await context.addInitScript(() => {
      if (window !== window.top) return;
      document.addEventListener('DOMContentLoaded', () => {
        const style = document.createElement('style');
        style.textContent = 'html { scroll-behavior: auto !important; }';
        document.head.append(style);
      }, { once: true });
    });
    await use(context);
  },
});
