import { expect, test, type Locator } from '@playwright/test';

async function settled(list: Locator) {
  await expect.poll(() => list.evaluate(el => el.getAnimations({ subtree: true }).length)).toBe(0);
}
async function foregrounds(list: Locator) {
  return list.evaluate(el => [...el.querySelectorAll('button, strong, small, span, svg')].map(node => {
    const style = getComputedStyle(node); return [style.color, style.backgroundColor, style.boxShadow];
  }));
}

test.beforeEach(async ({ page }) => {
  await page.setViewportSize({ width: 1000, height: 800 });
  await page.addInitScript(() => {
    const native = window.getComputedStyle;
    Object.assign(window, { highlightReads: 0 });
    window.getComputedStyle = (element, pseudo) => {
      if (pseudo === '::before' && element.matches('.ui-sliding-highlight')) {
        const state = window as typeof window & { highlightReads: number }; state.highlightReads++;
      }
      return native(element, pseudo);
    };
  });
  await page.goto('/tests/fixtures/sliding-highlight.html');
  await expect(page.locator('.assistant-list')).toHaveAttribute('data-highlight', '');
  await settled(page.locator('.assistant-list'));
});

test('hover and focus do not move the highlight; mouse and keyboard selection do', async ({ page }) => {
  const list = page.locator('.assistant-list');
  const transform = () => list.evaluate(el => getComputedStyle(el, '::before').transform);
  const initial = await transform(), fourth = page.locator('[data-assistant-id="four"]');
  await fourth.hover(); expect(await transform()).toBe(initial);
  await fourth.focus(); expect(await transform()).toBe(initial);
  await page.mouse.move(900, 700); expect(await transform()).toBe(initial);
  expect(await list.evaluate(el => el.getAnimations({ subtree: true }).length)).toBe(0);
  await list.evaluate(list => {
    Object.assign(window, { moveDuration: 0 });
    list.addEventListener('transitionrun', event => {
      if ((event as TransitionEvent).pseudoElement !== '::before') return;
      const movement = list.getAnimations({ subtree: true }).find(animation => animation instanceof CSSTransition && animation.transitionProperty === 'transform');
      if (movement) Object.assign(window, { moveDuration: movement.effect!.getTiming().duration });
    });
  });
  await fourth.press('Enter'); await expect(fourth).toHaveAttribute('aria-current', 'page');
  await expect.poll(() => page.evaluate(() => (window as typeof window & { moveDuration: number }).moveDuration)).toBe(180);
  await settled(list); expect(await transform()).not.toBe(initial);
  const selected = await transform();
  await page.locator('[data-assistant-id="two"]').hover(); expect(await transform()).toBe(selected);
  await page.mouse.move(900, 700); expect(await transform()).toBe(selected);
  await page.locator('[data-assistant-id="two"]').click(); await settled(list);
  const changed = await transform(); expect(changed).not.toBe(selected);
  await page.mouse.move(900, 700); expect(await transform()).toBe(changed);
});

test('highlight fades out and in over 100ms and reappears directly at its new position', async ({ page }) => {
  await page.locator('.assistant-list').evaluate(list => {
    const samples: { visible: boolean; duration: number; values: number[]; sliding: boolean }[] = [];
    Object.assign(window, { fadeSamples: samples });
    new MutationObserver(() => {
      const visible = list.hasAttribute('data-highlight');
      if (samples.some(sample => sample.visible === visible)) return;
      getComputedStyle(list, '::before').opacity;
      const animations = list.getAnimations({ subtree: true });
      const animation = animations.find(animation => animation instanceof CSSTransition && animation.transitionProperty === 'opacity')!;
      if (!animation) { samples.push({ visible, duration: 0, values: [], sliding: false }); return; }
      const duration = Number(animation.effect!.getTiming().duration);
      animation.pause();
      const values = [0, 50, 100].map(time => { animation.currentTime = time; return Number(getComputedStyle(list, '::before').opacity); });
      animation.finish();
      samples.push({ visible, duration, values, sliding: animations.some(animation => animation instanceof CSSTransition && animation.transitionProperty === 'transform') });
    }).observe(list, { attributes: true, attributeFilter: ['data-highlight'] });
  });
  await page.getByRole('button', { name: 'Clear selection' }).click();
  await expect(page.locator('.assistant-list')).not.toHaveAttribute('data-highlight');
  await page.locator('[data-assistant-id="four"]').hover();
  await expect(page.locator('.assistant-list')).not.toHaveAttribute('data-highlight');
  await page.locator('[data-assistant-id="four"]').click();
  await expect.poll(() => page.evaluate(() => (window as typeof window & { fadeSamples: unknown[] }).fadeSamples.length)).toBe(2);
  const samples = await page.evaluate(() => (window as typeof window & { fadeSamples: { visible: boolean; duration: number; values: number[]; sliding: boolean }[] }).fadeSamples);
  expect(samples.map(sample => sample.visible)).toEqual([false, true]);
  for (const sample of samples) {
    expect(sample.duration).toBe(100); expect(sample.values[1]).toBeGreaterThan(0); expect(sample.values[1]).toBeLessThan(1);
    expect(sample.values[0]).toBe(sample.visible ? 0 : 1); expect(sample.values[2]).toBe(sample.visible ? 1 : 0);
  }
  expect(samples[1].sliding).toBe(false);
  const aligned = await page.locator('.assistant-list').evaluate(list => {
    const style = getComputedStyle(list, '::before'), rect = list.querySelector('[data-assistant-id="four"]')!.getBoundingClientRect();
    return list.getBoundingClientRect().top + list.clientTop - list.scrollTop + new DOMMatrixReadOnly(style.transform).m42 - rect.top;
  });
  expect(Math.abs(aligned)).toBeLessThan(1);
});


test('reduced motion, mobile, disabled and idle lists retain their behavior', async ({ page }) => {
  const list = page.locator('.assistant-list');
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.locator('[data-assistant-id="four"]').click();
  expect(await list.evaluate(el => getComputedStyle(el, '::before').transitionDuration)).toBe('0s');
  await page.setViewportSize({ width: 390, height: 844 });
  const mobileColors = await list.locator('strong').evaluateAll(elements => elements.map(el => getComputedStyle(el).color));
  expect(new Set(mobileColors).size).toBe(1);
  expect(await list.evaluate(el => getComputedStyle(el, '::before').display)).toBe('none');
  await page.setViewportSize({ width: 1000, height: 800 });
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await settled(list); await page.clock.install();
  const reads = () => page.evaluate(() => (window as typeof window & { highlightReads: number }).highlightReads);
  const idle = await reads(); await page.clock.fastForward(10_000); expect(await reads()).toBe(idle);
  await page.getByRole('button', { name: 'Toggle disabled' }).click();
  await expect(list).not.toHaveAttribute('data-highlight');
  const disabled = await reads(); await page.clock.fastForward(10_000); expect(await reads()).toBe(disabled);
  await page.getByRole('button', { name: 'Toggle mounted' }).click();
  const unmounted = await reads(); await page.clock.fastForward(10_000); expect(await reads()).toBe(unmounted);
});

test('menus use primary, list backgrounds use the theme canvas, and hover uses active text colors without moving the background in all palettes and modes', async ({ page }) => {
  await page.goto('/tests/fixtures/sliding-highlight.html?colors');
  await page.emulateMedia({ reducedMotion: 'reduce' });
  const assistant = page.locator('.assistant-list'), settings = page.locator('.settings-nav'), sandbox = page.locator('.sandbox-list');
  for (const palette of ['forest', 'ocean', 'iris', 'sand', 'rose', 'monochrome']) for (const theme of ['light', 'dark']) {
    await page.evaluate(({ palette, theme }) => { document.documentElement.dataset.palette = palette; document.documentElement.dataset.theme = theme; }, { palette, theme });
    await settled(assistant); await settled(settings);
    const beforeAssistant = await foregrounds(assistant), beforeSettings = await foregrounds(settings);
    const hierarchy = () => page.evaluate(() => {
      const checks: boolean[] = [];
      for (const [selector, title] of [['.assistant-list button[data-assistant-id]', 'strong'], ['.settings-nav > button', 'span'], ['.sandbox-item', 'strong']]) {
        for (const item of document.querySelectorAll(selector)) {
          const label = item.querySelector(title)!;
          const probe = document.createElement('span'); item.append(probe);
          probe.style.color = item.hasAttribute('aria-current') || item.getAttribute('aria-pressed') === 'true' || item.matches(':hover') ? 'var(--color-text)' : 'var(--color-text-muted)';
          checks.push(getComputedStyle(label).color === getComputedStyle(probe).color && getComputedStyle(item).opacity === '1'); probe.remove();
        }
      }
      return checks.every(Boolean);
    });
    expect(await hierarchy(), `${palette}/${theme} text hierarchy`).toBe(true);
    const colors = await page.evaluate(() => {
      const nav = getComputedStyle(document.querySelector('.nav-button[aria-current]')!);
      const assistant = document.querySelector('.assistant-list')!, settings = document.querySelector('.settings-nav')!;
      const probe = document.createElement('span'); assistant.append(probe);
      probe.style.color = 'var(--color-primary)'; const primary = getComputedStyle(probe).color;
      probe.style.color = 'var(--color-on-primary)'; const onPrimary = getComputedStyle(probe).color;
      probe.style.backgroundColor = 'var(--color-background)'; const canvasColor = getComputedStyle(probe).backgroundColor;
      const canvas = document.createElement('canvas'); canvas.width = canvas.height = 1;
      const context = canvas.getContext('2d')!; context.fillStyle = getComputedStyle(assistant, '::before').backgroundColor; context.fillRect(0, 0, 1, 1);
      const alpha = context.getImageData(0, 0, 1, 1).data[3]; probe.remove();
      return { menu: nav.backgroundColor, foreground: nav.color, primary, onPrimary, canvasColor, alpha, assistant: getComputedStyle(assistant, '::before').backgroundColor, settings: getComputedStyle(settings, '::before').backgroundColor, sandbox: getComputedStyle(document.querySelector('.sandbox-list')!, '::before').backgroundColor };
    });
    expect(colors.menu, `${palette}/${theme}`).toBe(colors.primary); expect(colors.foreground).toBe(colors.onPrimary);
    expect(colors.assistant).toBe(colors.canvasColor); expect(colors.settings).toBe(colors.canvasColor); expect(colors.sandbox).toBe(colors.canvasColor); expect(colors.alpha).toBe(255);
    await page.locator('[data-assistant-id="four"]').hover(); await settled(assistant);
    expect(await hierarchy()).toBe(true);
    await settings.locator('[data-sliding-item="profile"]').hover(); await settled(settings);
    expect(await hierarchy()).toBe(true);
    const iconColors = await settings.evaluate(el => {
      const icons = [...el.querySelectorAll('.settings-nav-icon')]; return icons.map(icon => getComputedStyle(icon).color);
    });
    expect(new Set(iconColors).size).toBe(1);
    const statusBefore = await sandbox.locator('.resource-sidebar-state').evaluateAll(elements => elements.map(el => getComputedStyle(el).color));
    await sandbox.locator('[data-sliding-item="remote"]').hover(); expect(await hierarchy()).toBe(true);
    const sandboxIcons = await sandbox.locator('.sandbox-item > svg').evaluateAll(elements => elements.map(el => getComputedStyle(el).color));
    expect(new Set(sandboxIcons).size).toBe(1);
    await sandbox.locator('[data-sliding-item="remote"]').click(); await settled(sandbox); expect(await hierarchy()).toBe(true);
    expect(await sandbox.locator('.resource-sidebar-state').evaluateAll(elements => elements.map(el => getComputedStyle(el).color))).toEqual(statusBefore);
    await sandbox.locator('[data-sliding-item="local"]').click(); await settled(sandbox);
    await page.mouse.move(900, 700); await settled(assistant); await settled(settings);
    expect(await foregrounds(assistant)).toEqual(beforeAssistant); expect(await foregrounds(settings)).toEqual(beforeSettings);
    const details = () => assistant.evaluate(el => [...el.querySelectorAll('small, .ui-assistant-avatar, .assistant-unread, .assistant-list__pin')].map(node => {
      const style = getComputedStyle(node); return [style.color, style.backgroundColor, style.opacity];
    }));
    const beforeDetails = await details();
    await page.locator('[data-assistant-id="four"]').click(); await settled(assistant);
    await settings.locator('[data-sliding-item="profile"]').click(); await settled(settings);
    expect(await hierarchy()).toBe(true); expect(await details()).toEqual(beforeDetails);
    await page.locator('[data-assistant-id="one"]').click();
    await settings.locator('[data-sliding-item="general"]').click();
    await page.mouse.move(900, 700); await settled(assistant); await settled(settings);
  }
});


test('settings highlight follows selection and ignores hover', async ({ page }) => {
  await page.goto('/tests/fixtures/sliding-highlight.html?colors');
  const list = page.locator('.settings-nav'); await settled(list);
  const transform = () => list.evaluate(el => getComputedStyle(el, '::before').transform);
  const initial = await transform(), profile = list.locator('[data-sliding-item="profile"]');
  await profile.hover(); expect(await transform()).toBe(initial);
  await profile.click(); await expect(profile).toHaveAttribute('aria-current', 'page'); await settled(list);
  const selected = await transform(); expect(selected).not.toBe(initial);
  await list.locator('[data-sliding-item="general"]').hover(); expect(await transform()).toBe(selected);
  await page.mouse.move(900, 700); expect(await transform()).toBe(selected);
});


test('sandbox highlight follows selection while hover preserves position and connection status', async ({ page }) => {
  await page.goto('/tests/fixtures/sliding-highlight.html?colors');
  const list = page.locator('.sandbox-list'), remote = list.locator('[data-sliding-item="remote"]'); await settled(list);
  const position = () => list.evaluate(el => getComputedStyle(el, '::before').transform);
  const statuses = () => list.locator('.resource-sidebar-state').evaluateAll(elements => elements.map(el => getComputedStyle(el).color));
  const initial = await position(), before = await statuses();
  await remote.hover(); expect(await position()).toBe(initial); expect(await statuses()).toEqual(before);
  await remote.click(); await expect(remote).toHaveAttribute('aria-pressed', 'true'); await settled(list);
  const selected = await position(); expect(selected).not.toBe(initial); expect(await statuses()).toEqual(before);
  await list.locator('[data-sliding-item="local"]').hover(); expect(await position()).toBe(selected);
  await page.mouse.move(900, 700); expect(await position()).toBe(selected);
});
