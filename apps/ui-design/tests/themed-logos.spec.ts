import { readFile } from 'node:fs/promises';
import { test, expect } from '@playwright/test';
import { designBrandColors } from '../src/design-brand';
import { chooseOption } from './select-helpers';

test('SVG artwork and downloads follow every palette while PNG app icons stay original', async ({ page, request }) => {
  await page.goto('/#colors');
  await chooseOption(page.getByRole('combobox', { name: 'Appearance', exact: true }), /^Light /);
  const png = await readFile(new URL('../../../packages/ui/public/brand/molr-icon-48.png', import.meta.url));
  for (const [palette, label] of [['forest', 'Forest'], ['ocean', 'Ocean'], ['iris', 'Iris'], ['sand', 'Sand'], ['rose', 'Rose'], ['monochrome', 'Monochrome']] as const) {
    await page.getByRole('group', { name: 'Color palettes' }).getByRole('button', { name: label, exact: true }).click();
    const base = palette === 'forest' ? '/brand' : `/brand/themes/${palette}`;
    const colors = designBrandColors[palette];
    const header = page.locator('.site-header > .logo object');
    await expect(header).toHaveAttribute('data', new RegExp(`#${base}/molr-wordmark-(alive|playful)\\.svg`));
    await expect.poll(() => header.evaluate(el => (el as HTMLObjectElement).getSVGDocument()?.querySelector('[stroke]')?.getAttribute('stroke'))).toBe(colors.ink);
    const image = page.locator('.app-icon-preview__hero');
    await expect(image).toHaveAttribute('src', `${base}/molr-icon.svg`);
    await expect(page.locator('link[rel="icon"]')).toHaveAttribute('href', `${base}/molr-icon.svg`);
    for (const filename of ['molr-wordmark.svg', 'molr-wordmark-light.svg', 'molr-mark-loading.svg', 'molr-icon.svg']) {
      const response = await request.get(`${base}/${filename}`);
      expect(response.ok()).toBe(true);
      expect(response.headers()['content-type']).toContain('image/svg+xml');
      const svg = await response.text();
      expect(svg).toContain(filename.includes('-light') ? colors.paper : colors.ink);
      if (filename.includes('-light') || filename.includes('icon')) expect(svg).toContain(colors.accent);
    }
    await expect(page.locator('.app-icon-preview__sizes img').last()).toHaveAttribute('src', '/brand/molr-icon-48.png');
    await expect(page.locator('.app-icon-downloads__list a').first()).toHaveAttribute('href', '/brand/molr-icon-16.png');
    expect(await (await request.get('/brand/molr-icon-48.png')).body()).toEqual(png);
  }
  const downloaded = page.waitForEvent('download');
  await page.locator('.app-icon-svg').click();
  const svg = await readFile((await (await downloaded).path())!, 'utf8');
  expect(svg).toContain(designBrandColors.monochrome.ink);
  expect(svg).toContain(designBrandColors.monochrome.accent);
  await expect(page.locator('.logo-panel--light a')).toHaveAttribute('href', /\/brand\/themes\/monochrome\/molr-wordmark/);
  await expect(page.locator('.logo-downloads a').first()).toHaveAttribute('href', /\/brand\/themes\/monochrome\/molr-mark/);
});

test('themed SVGs keep dark variants, connection states, motion and reduced-motion fallbacks', async ({ page }) => {
  await page.goto('/#colors');
  await page.getByRole('group', { name: 'Color palettes' }).getByRole('button', { name: 'Ocean', exact: true }).click();
  await chooseOption(page.getByRole('combobox', { name: 'Appearance', exact: true }), /^Dark /);
  const header = page.locator('.site-header > .logo object');
  await expect(header).toHaveAttribute('data', /\/themes\/ocean\/molr-wordmark-light-(alive|playful)\.svg/);
  await expect.poll(() => header.evaluate(el => (el as HTMLObjectElement).getSVGDocument()?.querySelector('[stroke]')?.getAttribute('stroke'))).toBe(designBrandColors.ocean.paper);
  await expect.poll(() => header.evaluate(el => !!(el as HTMLObjectElement).getSVGDocument()?.querySelector('animate, animateTransform'))).toBe(true);
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await expect(page.locator('.site-header > .logo img')).toHaveAttribute('src', /\/themes\/ocean\/molr-wordmark-light-/);
  await expect.poll(() => page.locator('.site-header > .logo img').evaluate(el => (el as HTMLImageElement).currentSrc)).toContain('/themes/ocean/molr-wordmark-light.svg');
  await expect(page.locator('.connection-logo__still img').first()).toHaveAttribute('src', /#\/brand\/themes\/ocean\/molr-mark\.svg$/);
  await page.reload();
  await expect(page.locator('.site-header > .logo img')).toHaveAttribute('src', /\/themes\/ocean\/molr-wordmark-light-/);
});
