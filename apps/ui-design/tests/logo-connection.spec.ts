import { test, expect } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('molr-ui-design-locale', 'en'));
  await page.goto('/#logo-connection');
});

test('connection previews reuse loading, fade offline, and greet once on return', async ({ page }, info) => {
  const section = page.locator('#logo-connection'), previews = section.locator('#logo-connection-previews');
  const controls = section.getByRole('group', { name: 'Logo connection state' });
  await controls.getByRole('button', { name: 'Reconnecting', exact: true }).click();
  await expect(previews).toHaveAttribute('data-connection', 'reconnecting');
  for (const img of await previews.locator('.connection-logo__motion img').all()) {
    await expect(img).toHaveAttribute('src', /-loading\.svg/);
    await expect.poll(() => img.evaluate(el => (el as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
  }
  for (const target of await previews.locator('.logo-connection__target--navigation').all()) {
    await expect(target).toHaveCSS('width', '44px'); await expect(target).toHaveCSS('height', '44px');
  }
  await section.screenshot({ path: `/tmp/molr-logo-connection-loading-${info.project.name}.png` });
  await controls.getByRole('button', { name: 'Disconnected', exact: true }).click();
  for (const img of await previews.locator('.connection-logo__still img').all()) await expect(img).toHaveAttribute('src', /molr-(mark|wordmark)(-light)?\.svg$/);
  await expect(previews.locator('.connection-logo__art').first()).toHaveCSS('filter', 'none');
  await expect(previews.locator('.connection-logo__art').first()).toHaveCSS('opacity', '0.24');
  await previews.locator('.connection-logo__trigger').first().focus();
  await expect(section.getByRole('tooltip', { name: 'Disconnected', exact: true }).first()).toBeVisible();
  await page.keyboard.press('Escape');
  await section.screenshot({ path: `/tmp/molr-logo-connection-offline-${info.project.name}.png` });
  await controls.getByRole('button', { name: 'Connection restored', exact: true }).click();
  await expect(previews.locator('.connection-logo__motion img').first()).toHaveAttribute('src', /-greet\.svg/);
  await expect(previews).toHaveAttribute('data-connection', 'connected');
  await expect(previews.locator('.connection-logo__motion img').first()).toHaveAttribute('src', /-(alive|playful)\.svg/);
  expect(await section.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
});

test('manual changes cancel the sequence and reduced motion preserves only loading', async ({ page }) => {
  const section = page.locator('#logo-connection'), previews = section.locator('#logo-connection-previews');
  const controls = section.getByRole('group', { name: 'Logo connection state' });
  await section.getByRole('button', { name: 'Play connection sequence', exact: true }).click();
  await expect(previews).toHaveAttribute('data-connection', 'disconnected');
  await expect(previews).toHaveAttribute('data-connection', 'reconnecting');
  await controls.getByRole('button', { name: 'Disconnected', exact: true }).click();
  await expect(section.getByRole('button', { name: 'Stop preview', exact: true })).toHaveCount(0);
  await section.getByRole('switch', { name: 'Reduce motion', exact: true }).check();
  await controls.getByRole('button', { name: 'Reconnecting', exact: true }).click();
  await expect(section.getByRole('status')).toContainText('Reconnecting');
  await expect(previews.locator('.connection-logo__motion img').first()).toHaveAttribute('src', /molr-mark-loading\.svg/);
  await expect(section.getByRole('button', { name: 'Replay current state', exact: true })).toBeEnabled();
  await page.waitForTimeout(3400);
  await expect(previews).toHaveAttribute('data-connection', 'reconnecting');
  await controls.getByRole('button', { name: 'Connected', exact: true }).click();
  await expect(previews.locator('.connection-logo__still img').first()).toHaveAttribute('src', /molr-mark\.svg$/);
  await expect(section.getByRole('button', { name: 'Replay current state', exact: true })).toBeDisabled();
  await section.getByRole('switch', { name: 'Reduce motion', exact: true }).uncheck();
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await controls.getByRole('button', { name: 'Reconnecting', exact: true }).click();
  await expect.poll(() => previews.locator('.connection-logo__motion img').first().evaluate(el => (el as HTMLImageElement).currentSrc)).toContain('/brand/molr-mark-loading.svg');
  await expect(section.getByRole('status')).toContainText('Reconnecting');
  await controls.getByRole('button', { name: 'Connected', exact: true }).click();
  await expect(previews.locator('.connection-logo__motion')).toHaveCount(0);
  await expect.poll(() => previews.locator('.connection-logo__still img').first().evaluate(el => (el as HTMLImageElement).currentSrc)).toContain('/brand/molr-mark.svg');
});

test('downloaded loading SVGs remain visibly animated under reduced motion', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  for (const name of ['molr-mark', 'molr-mark-light', 'molr-wordmark', 'molr-wordmark-light']) {
    await page.goto(`/brand/${name}-loading.svg`);
    const result = await page.evaluate(() => {
      const svg = document.documentElement as unknown as SVGSVGElement;
      const artwork = svg.querySelector('[data-motion-art]')!, trace = svg.querySelector('[data-loading-trace]')!;
      svg.pauseAnimations(); svg.setCurrentTime(.2);
      const before = getComputedStyle(trace).strokeDashoffset;
      svg.setCurrentTime(.8);
      return { display: getComputedStyle(artwork).display, before, after: getComputedStyle(trace).strokeDashoffset, opacity: getComputedStyle(svg.querySelector('[data-loading-track]')!).opacity };
    });
    expect(result.display).not.toBe('none'); expect(result.before).not.toBe(result.after); expect(result.opacity).toBe('0.24');
  }
});

test('failed reconnection stops loading and supports retry under reduced motion', async ({ page }, info) => {
  const section = page.locator('#logo-connection'), previews = section.locator('#logo-connection-previews');
  await section.getByRole('button', { name: 'Preview reconnection failure', exact: true }).click();
  await expect(previews).toHaveAttribute('data-connection', 'disconnected');
  await expect(previews).toHaveAttribute('data-connection', 'reconnecting');
  await expect(previews).toHaveAttribute('data-connection', 'failed');
  await expect(section.getByRole('status')).toContainText('Reconnection failed');
  await expect(section.getByRole('button', { name: 'Stop preview', exact: true })).toHaveCount(0);
  const artwork = previews.locator('.connection-logo__art').first();
  await expect(artwork).toHaveCSS('opacity', '0.24');
  await expect(artwork).toHaveCSS('animation-iteration-count', '1');
  await expect(artwork).toHaveCSS('transform', 'none');
  await expect(previews.locator('.connection-logo__still img').first()).toHaveAttribute('src', /molr-mark\.svg$/);
  await section.screenshot({ path: `/tmp/molr-logo-connection-failed-${info.project.name}.png` });
  await section.getByRole('switch', { name: 'Reduce motion', exact: true }).check();
  await expect(artwork).toHaveCSS('animation-name', 'none');
  await expect(previews).toHaveAttribute('data-connection', 'failed');
  await section.getByRole('button', { name: 'Retry connection', exact: true }).click();
  await expect(previews).toHaveAttribute('data-connection', 'reconnecting');
  await expect(previews.locator('.connection-logo__motion img').first()).toHaveAttribute('src', /molr-mark-loading\.svg/);
  await expect(previews).toHaveAttribute('data-connection', 'recovered');
  await expect(previews).toHaveAttribute('data-connection', 'connected');
  await expect(section.getByRole('button', { name: 'Retry connection', exact: true })).toHaveCount(0);
  expect(await section.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
});
