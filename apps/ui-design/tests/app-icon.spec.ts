import { readFile } from 'node:fs/promises';
import { test, expect } from '@playwright/test';

for (const shape of ['Rounded', 'Square'] as const) {
test(`${shape} app icons download real PNGs at every advertised size`, async ({ page }, info) => {
  const asset = `molr-icon${shape === 'Square' ? '-square' : ''}`;
  await page.goto('/#app-icon');
  const section = page.getByRole('region', { name: 'App icon', exact: true });
  await expect(section).toBeVisible();
  await section.getByRole('radio', { name: shape, exact: true }).check();
  await expect(section.getByRole('img', { name: 'Molr app icon', exact: true })).toHaveAttribute('src', `/brand/${asset}.svg`);
  await expect(page.locator('#identity').getByRole('img', { name: 'Molr app icon' })).toHaveCount(0);
  const sizes = [16, 32, 48, 64, 128, 180, 192, 256, 512, 1024];
  await expect(section.getByRole('link', { name: /px PNG$/ })).toHaveCount(sizes.length);

  for (const size of sizes) {
    const downloadEvent = page.waitForEvent('download');
    await section.getByRole('link', { name: `Download the Molr app icon (${shape}) ${size} × ${size} px PNG`, exact: true }).click();
    const download = await downloadEvent;
    expect(download.suggestedFilename()).toBe(`${asset}-${size}.png`);
    const png = await readFile((await download.path())!);
    expect(png.subarray(0, 8).toString('hex')).toBe('89504e470d0a1a0a');
    expect([png.readUInt32BE(16), png.readUInt32BE(20)]).toEqual([size, size]);
  }

  const pixels = await page.evaluate(async (src) => {
    const icon = new Image();
    icon.src = src;
    await icon.decode();
    const canvas = document.createElement('canvas');
    canvas.width = canvas.height = 1024;
    const context = canvas.getContext('2d')!;
    context.drawImage(icon, 0, 0);
    return [[0, 0], [1023, 0], [0, 1023], [1023, 1023], [512, 870], [243, 410]].map(([x, y]) => Array.from(context.getImageData(x, y, 1, 1).data));
  }, `/brand/${asset}-1024.png`);
  const corner = shape === 'Square' ? [213, 244, 120, 255] : [0, 0, 0, 0];
  expect(pixels).toEqual([corner, corner, corner, corner, [213, 244, 120, 255], [38, 59, 49, 255]]);

  await section.getByRole('heading', { name: 'App icon', exact: true }).click();
  await section.screenshot({ path: info.outputPath('app-icon.png') });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
}

test('app icon navigation and downloads follow the selected language', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('combobox', { name: 'Language', exact: true }).click();
  await page.getByRole('option', { name: '简体中文', exact: true }).click();
  await page.getByRole('navigation', { name: '页面导航' }).getByRole('link', { name: '应用图标', exact: true }).click();
  await expect(page).toHaveURL(/#app-icon$/);
  const section = page.getByRole('region', { name: '应用图标', exact: true });
  await expect(section.getByRole('heading', { name: 'PNG 下载', exact: true })).toBeVisible();
  for (const [shape, filename, radius] of [['圆角', 'molr-icon', 22], ['直角', 'molr-icon-square', 0]] as const) {
    await section.getByRole('radio', { name: shape, exact: true }).check();
    await expect(section.getByRole('link', { name: `下载 Molr ${shape}应用图标 1024 × 1024 px PNG`, exact: true })).toHaveAttribute('href', `/brand/${filename}-1024.png`);
    const svgEvent = page.waitForEvent('download');
    await section.getByRole('link', { name: `下载 Molr ${shape}应用图标 SVG`, exact: true }).click();
    const svg = await svgEvent;
    expect(svg.suggestedFilename()).toBe(`${filename}.svg`);
    expect(await readFile((await svg.path())!, 'utf8')).toContain(`rx="${radius}"`);
  }
  await section.getByRole('radio', { name: '直角', exact: true }).focus();
  await page.keyboard.press('ArrowLeft');
  await expect(section.getByRole('radio', { name: '圆角', exact: true })).toBeChecked();
  await expect(section.getByRole('img', { name: 'Molr 应用图标', exact: true })).toHaveAttribute('src', '/brand/molr-icon.svg');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
