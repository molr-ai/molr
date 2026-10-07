import { test, expect } from '@playwright/test';
import { chooseOption } from './select-helpers';

for (const persistence of ['available', 'blocked']) {
  test(`manual appearance changes keep all logos in sync with storage ${persistence}`, async ({ page }) => {
    await page.addInitScript(() => localStorage.setItem('molr-ui-design-locale', 'en'));
    if (persistence === 'blocked') await page.addInitScript(() => {
      const setItem = Storage.prototype.setItem;
      Storage.prototype.setItem = function (key, value) {
        if (key === 'molr-theme') throw new DOMException('Storage is unavailable', 'SecurityError');
        setItem.call(this, key, value);
      };
    });
    await page.emulateMedia({ reducedMotion: 'no-preference', colorScheme: 'light' });
    await page.goto('/#interfaces');
    const pageLogo = page.locator('.site-header > .logo');
    const appearance = page.getByRole('combobox', { name: 'Appearance', exact: true });
    const marks = page.locator('.ip-nav > .ip-nav-logo, .mobile-assistant-toolbar > .assistant-toolbar-logo');
    const switchPreview = (name: string) => page.locator('#interfaces').getByRole('tab', { name, exact: true }).click();
    await expect(marks).toHaveCount(1); // Mobile is mounted only on first visit.
    async function expectTheme(theme: 'light' | 'dark') {
      const variant = theme === 'dark' ? 'light' : 'dark';
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
      await expect(pageLogo).toHaveClass(new RegExp(`logo--${variant}`));
      const visibleMarks = marks.filter({ visible: true });
      await expect(visibleMarks).toHaveCount(1);
      for (const logo of await visibleMarks.all()) {
        const name = await logo.evaluate(el => el.classList.contains('assistant-toolbar-logo') ? 'wordmark' : 'mark');
        await expect(logo).toHaveClass(new RegExp(`logo--${variant}`));
        await expect(logo.locator('object')).toHaveAttribute('data', new RegExp(`molr-${name}${theme === 'dark' ? '-light' : ''}-(alive|playful)\\.svg`));
        await expect(logo).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
      }
    }
    await expectTheme('light');
    await chooseOption(appearance, 'Dark Dark surfaces with soft text');
    await expectTheme('dark');
    await switchPreview('Mobile');
    await expect(marks).toHaveCount(2);
    await expectTheme('dark'); // First mount uses the latest preference.
    await page.emulateMedia({ colorScheme: 'dark' });
    await chooseOption(appearance, 'Light Light surfaces with clear text');
    await expectTheme('light');
    await switchPreview('Web');
    await expectTheme('light'); // Restored subscribers reconcile on reveal.
    await chooseOption(appearance, 'System Match your device appearance');
    await expectTheme('dark');
    await switchPreview('Mobile');
    await expectTheme('dark');
    await page.emulateMedia({ colorScheme: 'light' });
    await expectTheme('light');
    if (persistence === 'available') {
      await chooseOption(appearance, 'Dark Dark surfaces with soft text');
      await page.reload();
      await expectTheme('dark');
    }
  });
}

for (const version of ['Web', 'Desktop', 'Mobile']) {
  test(`${version} navigation has an interactive daily logo and a reduced-motion fallback`, async ({ page }, info) => {
    await page.addInitScript(() => localStorage.setItem('molr-ui-design-locale', 'en'));
    await page.emulateMedia({ reducedMotion: 'no-preference', colorScheme: 'light' });
    await page.clock.install({ time: new Date('2026-09-23T19:59:58+08:00') });
    await page.clock.pauseAt(new Date('2026-09-23T19:59:59+08:00'));
    await page.goto('/#interfaces');
    await page.locator('#interfaces').getByRole('tab', { name: version, exact: true }).click();
    const shell = page.locator('.interface-preview.ip-shell:visible');
    const logo = shell.locator(version === 'Mobile' ? '.mobile-assistant-toolbar > .assistant-toolbar-logo' : '.ip-nav > .ip-nav-logo');
    const name = version === 'Mobile' ? 'wordmark' : 'mark';
    const width = version === 'Mobile' ? 44 * 183 / 54 : 44;
    const mark = logo.locator('object');
    expect((await logo.boundingBox())!.width).toBeCloseTo(width, 1);
    await expect(logo).toHaveCSS('height', '44px');
    await expect(logo).toHaveCSS('border-radius', '8px');
    await expect(mark).toHaveClass(version === 'Mobile' ? 'logo__wordmark' : 'logo__symbol');
    await expect(mark).toHaveAttribute('data', new RegExp(`molr-${name}-alive\\.svg.*day`));
    await expect(logo).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
    await expect.poll(() => mark.evaluate(el => !!(el as HTMLObjectElement).getSVGDocument()?.documentElement)).toBe(true);
    await mark.evaluate(el => {
      const svg = (el as HTMLObjectElement).getSVGDocument()!.documentElement as unknown as SVGSVGElement;
      svg.setCurrentTime(3); svg.pauseAnimations();
    });
    await page.clock.runFor(200);
    await expect(logo).toHaveAttribute('data-phase', 'waiting');
    await logo.click();
    expect(await mark.evaluate(el => ((el as HTMLObjectElement).getSVGDocument()!.documentElement as unknown as SVGSVGElement).getCurrentTime())).toBeGreaterThan(22);
    await expect(logo).toHaveAttribute('data-phase', 'playing');
    await expect(shell.locator('.conversation-header h3')).toHaveText('Molr');
    await page.clock.runFor(1000);
    await expect(mark).toHaveAttribute('data', new RegExp(`molr-${name}-playful\\.svg.*night`));
    await page.emulateMedia({ colorScheme: 'dark' });
    await expect(mark).toHaveAttribute('data', new RegExp(`molr-${name}-light-playful\\.svg.*night`));
    await expect(logo).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await expect(mark).toHaveCount(0);
    await expect.poll(() => logo.locator('img').evaluate(el => (el as HTMLImageElement).currentSrc)).toContain(`/brand/molr-${name}-light.svg`);
    expect((await logo.boundingBox())!.width).toBeCloseTo(width, 1);
    await expect(logo).toHaveCSS('height', '44px');
    await logo.screenshot({ path: `/tmp/molr-navigation-logo-${version}-${info.project.name}.png` });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  });
}
