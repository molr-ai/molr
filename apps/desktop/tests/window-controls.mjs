import { expect } from '@playwright/test';

// Compare the rendered titlebar against Electron's native safe area, rather
// than assuming a button size or unscaled CSS dimensions.
export async function expectMacTitlebar(page, toolbar, brand) {
  await expect.poll(() => page.evaluate(({ toolbar, brand }) => {
    const overlay = navigator.windowControlsOverlay;
    if (!overlay?.visible) return false;
    const safe = overlay.getTitlebarAreaRect();
    const bar = document.querySelector(toolbar)?.getBoundingClientRect();
    const logo = document.querySelector(brand)?.getBoundingClientRect();
    if (!bar || !logo || safe.height <= 0) return false;
    const buttons = [...document.querySelectorAll(`${toolbar} button`)].map(el => el.getBoundingClientRect());
    return Math.abs(bar.height - 48) < 0.1
      && Math.abs(bar.height - safe.height) < 1
      && Math.abs(logo.left - safe.x) < 1
      && Math.abs(logo.top + logo.height / 2 - (bar.top + bar.height / 2)) < 1
      && buttons.every(button => button.top >= bar.top - 1 && button.bottom <= bar.bottom + 1
        && Math.abs(button.top + button.height / 2 - (bar.top + bar.height / 2)) < 1);
  }, { toolbar, brand })).toBe(true);
}

export async function expectFullscreenTitlebar(page, toolbar, brand) {
  await expect(page.locator(toolbar)).toHaveCSS('height', '48px');
  await expect(page.locator(toolbar)).toHaveCSS('padding-left', '16px');
  await expect.poll(() => page.evaluate(({ toolbar, brand }) => {
    const bar = document.querySelector(toolbar).getBoundingClientRect();
    const logo = document.querySelector(brand).getBoundingClientRect();
    return Math.abs(logo.left - 16) < 1 && [...document.querySelectorAll(`${toolbar} button`)].every(el => {
      const button = el.getBoundingClientRect();
      return Math.abs(button.top + button.height / 2 - (bar.top + bar.height / 2)) < 1;
    });
  }, { toolbar, brand })).toBe(true);
}
