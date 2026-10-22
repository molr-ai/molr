import { expect } from '@playwright/test';
import assert from 'node:assert/strict';

// firstWindow() also returns initially hidden windows. Pointer actionability
// needs animation frames, so wait for the application's own show() first.
export async function waitForVisibleWindow(app, page) {
  const window = await app.browserWindow(page);
  try {
    await expect.poll(() => window.evaluate(win => !win.isDestroyed() && win.isVisible()), {
      message: 'Electron window must be shown before interacting with its page', timeout: 30000,
    }).toBe(true);
    await page.bringToFront();
    return page;
  } finally { await window.dispose(); }
}

// Sample during resizing: a settled screenshot misses macOS WCO's transient x.
export async function expectStableMacTitlebar(app, page, toolbar, brand) {
  await expectMacTitlebar(page, toolbar, brand);
  await page.evaluate(brand => {
    const logo = document.querySelector(brand);
    const start = logo.getBoundingClientRect();
    const probe = { samples: 0, maxX: 0, maxY: 0, frame: 0 };
    const sample = () => {
      const rect = logo.getBoundingClientRect();
      probe.samples++;
      probe.maxX = Math.max(probe.maxX, Math.abs(rect.x - start.x));
      probe.maxY = Math.max(probe.maxY, Math.abs(rect.y - start.y));
    };
    const tick = () => { sample(); probe.frame = requestAnimationFrame(tick); };
    window.addEventListener('resize', sample);
    navigator.windowControlsOverlay.addEventListener('geometrychange', sample);
    window.stopTitlebarProbe = () => {
      cancelAnimationFrame(probe.frame);
      window.removeEventListener('resize', sample);
      navigator.windowControlsOverlay.removeEventListener('geometrychange', sample);
      delete window.stopTitlebarProbe;
      return probe;
    };
    tick();
  }, brand);
  let samples;
  try {
    await app.evaluate(async ({ BrowserWindow }) => {
      const win = BrowserWindow.getAllWindows()[0];
      const original = win.getBounds();
      try {
        for (let step = 0; step < 60; step++) {
          const delta = step < 30 ? step * 4 : (59 - step) * 4;
          win.setSize(original.width - delta, original.height - Math.round(delta / 2));
          await new Promise(resolve => setTimeout(resolve, 16));
        }
      } finally { win.setBounds(original); }
    });
  } finally { samples = await page.evaluate(() => window.stopTitlebarProbe()); }
  assert(samples.samples > 20, 'Observe multiple frames/events during native resizing');
  assert(samples.maxX < 0.1 && samples.maxY < 0.1, `Logo moved during resize: ${JSON.stringify(samples)}`);
  await expectMacTitlebar(page, toolbar, brand);
  return samples;
}

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
