import test from 'node:test';
import assert from 'node:assert/strict';
import { chromium, expect } from '@playwright/test';
import { createServer } from 'vite';
import { mkdirSync } from 'node:fs';
import { resolve } from 'node:path';
import { desktopTarget, browserDesktopTarget, preferredDesktopPlatform } from '../src/desktop-target.ts';

const artifact = (os, arch, version = '0.1.0') => {
  const variant = os === 'win32' ? 'client' : 'full';
  const file = `molr-desktop_${version}_${os}-${arch}_${variant}.${({ darwin: 'dmg', win32: 'exe', linux: 'deb' })[os]}`;
  return { os, arch, variant, signed: true, file, sha256: 'a'.repeat(64), url: `https://downloads.example.com/${file}` };
};
const platforms = [artifact('darwin', 'arm64'), artifact('darwin', 'amd64'), artifact('win32', 'amd64'), artifact('linux', 'amd64'), artifact('linux', 'arm64')];
const sample = () => ({ schema: 1, channels: { stable: { version: '0.1.0', platforms }, preview: { version: '0.2.0-rc.1', platforms: [artifact('win32', 'amd64', '0.2.0-rc.1')] }, nightly: null } });

test('desktop detection handles hints, ambiguous Macs, mobile devices and first-item fallback', async () => {
  const mac = { platform: 'MacIntel', userAgent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)', maxTouchPoints: 0 };
  assert.deepEqual(desktopTarget(mac), { os: 'darwin', arch: undefined });
  assert.deepEqual(desktopTarget(mac, { platform: 'macOS', architecture: 'arm', bitness: '64' }), { os: 'darwin', arch: 'arm64' });
  assert.deepEqual(desktopTarget({ ...mac, maxTouchPoints: 5 }), {});
  assert.deepEqual(desktopTarget({ platform: 'Linux aarch64', userAgent: 'Android' }), {});
  assert.deepEqual(desktopTarget({ platform: 'Linux x86_64', userAgent: 'X11; Linux x86_64' }), { os: 'linux', arch: 'amd64' });
  assert.deepEqual(desktopTarget({ platform: 'Win32', userAgent: 'Windows NT 10.0; Win64; x64' }), { os: 'win32', arch: 'amd64' });
  assert.equal(preferredDesktopPlatform(platforms, {}), platforms[0]);
  assert.equal(preferredDesktopPlatform(platforms, { os: 'linux', arch: 'arm64' }), platforms[4]);
  assert.equal(preferredDesktopPlatform([], {}), undefined);
  assert.deepEqual(await browserDesktopTarget({ ...mac, userAgentData: { getHighEntropyValues: async () => { throw new Error('denied'); } } }), desktopTarget(mac));
});

test('hero downloads detected platform directly, supports manual selection and has no channel switch', { timeout: 90000 }, async () => {
  const before = process.env.VITE_SITE_MODE; process.env.VITE_SITE_MODE = 'live';
  const server = await createServer({ root: resolve(import.meta.dirname, '..'), logLevel: 'silent', server: { host: '127.0.0.1', port: 0 } });
  const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || undefined });
  try {
    await server.listen();
    const page = await browser.newPage({ locale: 'en-US', reducedMotion: 'reduce', viewport: { width: 1440, height: 1000 } });
    await page.addInitScript(() => {
      Object.defineProperty(navigator, 'userAgentData', { configurable: true, value: { platform: 'Windows', mobile: false, getHighEntropyValues: async () => ({ platform: 'Windows', architecture: 'x86', bitness: '64' }) } });
    });
    let catalog = sample(), failure = false;
    await page.route('**/releases/desktop/catalog.json', route => failure ? route.abort() : route.fulfill({ json: catalog }));
    await page.route('https://downloads.example.com/**', route => route.fulfill({ body: 'artificial installer fixture', headers: { 'Content-Disposition': `attachment; filename="${platforms[2].file}"` } }));
    await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/en/`);
    const hero = page.locator('.home-hero-download');
    const more = hero.getByRole('button', { name: 'More download options' });
    const panel = page.getByRole('dialog', { name: 'Choose a download' });
    await expect(hero.getByRole('link', { name: 'Download Molr Desktop (Windows · AMD64)' })).toHaveAttribute('href', platforms[2].url);
    await expect(hero.getByRole('link', { name: 'View docs', exact: true })).toHaveAttribute('href', '/en/docs/user/');
    await expect(hero.getByRole('link', { name: 'More versions and installation methods', exact: true })).toHaveAttribute('href', '#install');
    await expect(hero.locator('.home-download-actions').getByRole('link')).toHaveCount(2);
    await expect(hero.locator('.home-download-label > span')).toHaveText('Download Molr Desktop');
    await expect(hero.locator('.home-download-label > small')).toHaveText('Windows · AMD64');
    await expect(hero.locator('.home-download-label > small')).toHaveCSS('opacity', '0.7');
    const originalWidth = (await hero.locator('.home-download-primary').boundingBox()).width;
    await expect(hero.locator('.home-download-split')).toHaveCSS('width', '280px');
    const titleBox = await hero.locator('.home-download-label > span').boundingBox();
    const detailBox = await hero.locator('.home-download-label > small').boundingBox();
    assert(detailBox.y >= titleBox.y + titleBox.height);
    assert(await hero.locator('.home-download-label').evaluate(el => parseFloat(getComputedStyle(el.querySelector('small')).fontSize) < parseFloat(getComputedStyle(el.querySelector('span')).fontSize)));
    await expect(hero.locator('pre')).toHaveCount(0);
    await expect(hero.locator('.home-download-meta, .home-download-edition')).toHaveCount(0);
    await expect(page.getByRole('button', { name: /^(Stable|Preview|Nightly)$/ })).toHaveCount(0);
    const download = page.waitForEvent('download');
    await hero.getByRole('link', { name: 'Download Molr Desktop (Windows · AMD64)' }).click();
    assert.equal((await download).suggestedFilename(), platforms[2].file);
    await more.click();
    await expect(panel).toBeVisible();
    await expect(panel.getByText('This device', { exact: true })).toHaveCount(1);
    await expect(panel.getByRole('button', { name: 'Windows · AMD64 This device', exact: true })).toHaveAttribute('aria-pressed', 'true');
    await expect(panel.locator('.home-download-option-label > svg')).toHaveCount(5);
    await panel.getByRole('button', { name: 'macOS · Intel' }).click();
    await expect(panel).not.toBeVisible();
    await expect(more).toBeFocused();
    assert.equal((await hero.locator('.home-download-primary').boundingBox()).width, originalWidth);
    await expect(hero.getByRole('link', { name: 'Download Molr Desktop (macOS · Intel)' })).toHaveAttribute('href', platforms[1].url);
    await more.click();
    await expect(panel.getByRole('button', { name: 'Windows · AMD64 This device', exact: true })).toHaveAttribute('aria-pressed', 'false');
    await expect(panel.getByRole('button', { name: 'macOS · Intel', exact: true })).toHaveAttribute('aria-pressed', 'true');
    await page.keyboard.press('Escape'); await expect(panel).not.toBeVisible();
    const screenshots = resolve(import.meta.dirname, '../test-results/hero-download'); mkdirSync(screenshots, { recursive: true });
    for (const theme of ['light', 'dark']) for (const palette of ['forest', 'ocean', 'iris', 'sand', 'rose', 'monochrome']) for (const width of [320, 1440]) {
      await page.setViewportSize({ width, height: 1000 });
      const toggle = page.getByRole('button', { name: theme === 'dark' ? 'Switch to dark mode' : 'Switch to light mode', exact: true });
      if (await toggle.count()) await toggle.click();
      await page.locator('html').evaluate((el, value) => { el.dataset.palette = value; }, palette);
      await hero.scrollIntoViewIfNeeded();
      const downloadBox = await hero.locator('.home-download-split').boundingBox();
      const docsBox = await hero.locator('.home-download-docs').boundingBox();
      assert.equal(downloadBox.height, docsBox.height);
      assert(docsBox.width < downloadBox.width);
      if (width === 1440) assert.equal(downloadBox.y, docsBox.y);
      await more.click();
      await expect.poll(async () => { const b = await panel.boundingBox(); return b && b.x >= 0 && b.x + b.width <= width; }).toBeTruthy();
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      await expect(panel.getByRole('button', { name: 'macOS · Intel', exact: true })).toHaveCSS('white-space', 'nowrap');
      await page.screenshot({ path: `${screenshots}/${theme}-${palette}-${width}.png` });
      await page.keyboard.press('Escape');
    }
    await page.getByRole('button', { name: '切换到中文', exact: true }).click();
    await expect(hero.getByRole('link', { name: '下载 Molr 桌面版 (macOS · Intel)' })).toHaveAttribute('href', platforms[1].url);
    await expect(page.getByRole('button', { name: /^(正式版|预发布版|Nightly)$/ })).toHaveCount(0);
    // Unsupported detection uses the first item in the same list, without hiding alternatives.
    await page.addInitScript(() => {
      Object.defineProperty(navigator, 'userAgentData', { configurable: true, value: undefined });
      Object.defineProperty(navigator, 'userAgent', { configurable: true, value: 'unknown' });
      Object.defineProperty(navigator, 'platform', { configurable: true, value: 'unknown' });
    });
    await page.reload();
    await expect(hero.getByRole('link', { name: '下载 Molr 桌面版 (macOS · Apple Silicon)' })).toHaveAttribute('href', platforms[0].url);
    await hero.getByRole('button', { name: '更多下载选项', exact: true }).click();
    await expect(page.getByRole('dialog').getByText('当前设备', { exact: true })).toHaveCount(0);
    await page.keyboard.press('Escape');
    catalog = { schema: 1, channels: { stable: null, preview: sample().channels.preview, nightly: null } };
    await page.reload();
    await expect(hero.getByRole('link', { name: '下载 Molr 桌面版 (Windows · AMD64)' })).toHaveAttribute('href', catalog.channels.preview.platforms[0].url);
    catalog = { schema: 1, channels: { stable: null, preview: null, nightly: null } };
    await page.reload();
    await expect(hero.locator('.home-download-primary')).toHaveCount(0);
    await expect(hero.getByRole('button', { name: '更多下载选项' })).toHaveCount(0);
    await expect(hero.getByRole('button', { name: '即将推出', exact: true })).toBeDisabled();
    await expect(hero.getByRole('button', { name: '即将推出', exact: true })).toHaveCSS('opacity', '1');
    await expect(hero.getByRole('button', { name: '即将推出', exact: true }).locator('svg')).toHaveCount(1);
    await expect(hero.getByRole('button', { name: '即将推出', exact: true })).toHaveCSS('border-top-style', 'dashed');
    await expect(hero.getByRole('status')).toHaveText('即将推出');
    await expect(hero.getByRole('link', { name: '查看文档', exact: true })).toHaveAttribute('href', '/zh/docs/user/');
    await expect(hero.getByRole('link', { name: '更多版本和安装方式', exact: true })).toHaveAttribute('href', '#install');
    for (const width of [320, 1440]) {
      await page.setViewportSize({ width, height: 1000 });
      const pendingBox = await hero.locator('.home-download-pending').boundingBox();
      const docsBox = await hero.locator('.home-download-docs').boundingBox();
      assert.equal(pendingBox.height, docsBox.height);
      if (width === 1440) assert.equal(pendingBox.y, docsBox.y);
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    }
    failure = true; await page.reload();
    await expect(hero.getByRole('status')).toContainText('暂时无法');
    failure = false; catalog = sample();
    await hero.getByRole('button', { name: '重试', exact: true }).click();
    await expect(hero.getByRole('link', { name: '下载 Molr 桌面版 (macOS · Apple Silicon)' })).toHaveAttribute('href', platforms[0].url);
  } finally {
    await browser.close(); await server.close();
    if (before === undefined) delete process.env.VITE_SITE_MODE; else process.env.VITE_SITE_MODE = before;
  }
});
