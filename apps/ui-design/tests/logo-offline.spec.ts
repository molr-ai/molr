import { test, expect } from '@playwright/test';

test('first loading, replay and recovery animate without a reachable asset server', async ({ page, context }) => {
  await page.addInitScript(() => localStorage.setItem('molr-ui-design-locale', 'en'));
  // No HTTP artwork can enter the browser cache, even during initial render.
  await page.route('**/brand/**', route => route.abort('connectionrefused'));
  await page.goto('/#logo-connection');
  const section = page.locator('#logo-connection');
  const controls = section.getByRole('group', { name: 'Logo connection state' });
  const previews = section.locator('#logo-connection-previews');
  const motion = previews.locator('.connection-logo__motion img');
  await expect.poll(() => motion.first().evaluate(el => (el as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
  await context.setOffline(true);
  for (const reduced of [false, true]) {
    await page.emulateMedia({ reducedMotion: reduced ? 'reduce' : 'no-preference' });
    await controls.getByRole('button', { name: 'Reconnecting', exact: true }).click();
    for (const image of await motion.all()) {
      await expect(image).toHaveAttribute('src', /^data:image\/svg\+xml,.*#\/brand\/.*-loading\.svg/);
      await expect.poll(() => image.evaluate(el => (el as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
      await expect(image).toBeVisible();
    }
    const firstFrame = await motion.first().screenshot();
    await expect.poll(async () => (await motion.first().screenshot()).equals(firstFrame)).toBe(false);
    const beforeReplay = await motion.first().getAttribute('src');
    await section.getByRole('button', { name: 'Replay current state', exact: true }).click();
    await expect(motion.first()).not.toHaveAttribute('src', beforeReplay!);
    await expect.poll(() => motion.first().evaluate(el => (el as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
    await controls.getByRole('button', { name: 'Disconnected', exact: true }).click();
    for (const still of await previews.locator('.connection-logo__still img').all()) {
      await expect(still).toBeVisible();
      expect(await still.evaluate(el => (el as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
    }
  }
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await controls.getByRole('button', { name: 'Connection restored', exact: true }).click();
  await expect(motion.first()).toHaveAttribute('src', /-greet\.svg/);
  await expect.poll(() => motion.first().evaluate(el => (el as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
  await expect(previews).toHaveAttribute('data-connection', 'connected');
  await expect(motion.first()).toHaveAttribute('src', /-(alive|playful)\.svg/);
  await expect.poll(() => motion.first().evaluate(el => (el as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
});

test('offline day and night changes preserve interactive SVG playback and theme changes', async ({ page, context }) => {
  await page.addInitScript(() => localStorage.setItem('molr-ui-design-locale', 'en'));
  await page.clock.install({ time: new Date('2026-09-23T19:59:58+08:00') });
  await page.clock.pauseAt(new Date('2026-09-23T19:59:59+08:00'));
  await page.route('**/brand/**', route => route.abort('connectionrefused'));
  await page.goto('/#interfaces');
  const logo = page.locator('.site-header > .logo');
  const object = logo.locator('object');
  await expect.poll(() => object.evaluate(el => !!(el as HTMLObjectElement).getSVGDocument()?.documentElement)).toBe(true);
  await context.setOffline(true);
  await page.clock.runFor(1200);
  await expect(object).toHaveAttribute('data', /^blob:.*-playful\.svg.*night/);
  await expect.poll(() => object.evaluate(el => !!(el as HTMLObjectElement).getSVGDocument()?.documentElement)).toBe(true);
  await object.evaluate(el => {
    const svg = (el as HTMLObjectElement).getSVGDocument()!.documentElement as unknown as SVGSVGElement;
    svg.setCurrentTime(3); svg.pauseAnimations();
  });
  await page.clock.runFor(200);
  await expect(logo).toHaveAttribute('data-phase', 'waiting');
  await logo.click();
  await expect(logo).toHaveAttribute('data-phase', 'playing');
  await page.getByRole('group', { name: 'Color palettes' }).getByRole('button', { name: 'Ocean', exact: true }).click();
  await expect(object).toHaveAttribute('data', /#\/brand\/themes\/ocean\//);
  const dark = await page.locator('html').getAttribute('data-theme') === 'dark';
  await expect.poll(() => object.evaluate(el => (el as HTMLObjectElement).getSVGDocument()?.querySelector('[stroke]')?.getAttribute('stroke'))).toBe(dark ? '#eaf3f9' : '#244b66');
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await expect(object).toHaveCount(0);
  await expect.poll(() => logo.locator('img').evaluate(el => (el as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await expect.poll(() => object.evaluate(el => !!(el as HTMLObjectElement).getSVGDocument()?.documentElement)).toBe(true);
});

test('unreachable custom artwork falls back to a bundled static logo', async ({ page }) => {
  await page.route('**/deployment/brand/**', route => route.abort('connectionrefused'));
  await page.goto('/tests/fixtures/platform-review.html');
  const image = page.locator('#based-logo img');
  await expect(image).toHaveAttribute('src', /^data:image\/svg\+xml,.*#\/deployment\/brand\/molr-mark\.svg$/);
  await expect.poll(() => image.evaluate(el => (el as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
  await expect(image).toBeVisible();
});
