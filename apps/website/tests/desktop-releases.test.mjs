import test from 'node:test';
import assert from 'node:assert/strict';
import { readDesktopCatalog } from '../src/desktop-releases.ts';
import { createServer } from 'vite';
import { chromium, expect } from '@playwright/test';
import { resolve } from 'node:path';
import { withBrowserDiagnostics } from './browser-support.mjs';
const file = 'molr-desktop_0.1.0_win32-amd64_full.exe';
const platform = { os: 'win32', arch: 'amd64', variant: 'full', signed: false, notarized: false, file, sha256: 'a'.repeat(64), url: `https://downloads.example.com/${file}` };
const sample = () => ({ schema: 1, channels: { stable: { version: '0.1.0', platforms: [{ ...platform }] }, preview: null, nightly: null } });
test('desktop catalog enforces bundled Runtime edition and HTTPS artifact identity', () => {
  assert.deepEqual(readDesktopCatalog(sample()), sample());
  for (const change of [{ notarized: undefined }, { notarized: 'false' }, { notarized: true }, { signed: undefined }, { signed: 'false' }, { variant: 'client' }, { url: 'javascript:alert(1)' }, { url: `http://downloads.example.com/${file}` }, { file: '../private' }, { sha256: 'bad' }]) {
    const c = sample(); Object.assign(c.channels.stable.platforms[0], change); assert.throws(() => readDesktopCatalog(c));
  }
  const duplicate = sample(); duplicate.channels.stable.platforms.push(platform); assert.throws(() => readDesktopCatalog(duplicate));
});
test('desktop catalogs never publish unknown metadata or local builds', () => {
  const c = sample(); c.private = 'fixture'; c.channels.stable.private = 'fixture'; c.channels.stable.platforms[0].private = 'fixture';
  assert(!JSON.stringify(readDesktopCatalog(c)).includes('fixture'));
  c.channels.stable.version = '0.1.0-local'; assert.throws(() => readDesktopCatalog(c));
});
// Includes cold Vite startup and seven page loads across signing/channel states.
test('desktop download UI shows edition and does not invent missing releases', { timeout: 120000 }, async t => {
  const before = process.env.VITE_SITE_MODE; process.env.VITE_SITE_MODE = 'live';
  const server = await createServer({ root: resolve(import.meta.dirname, '..'), logLevel: 'silent', server: { host: '127.0.0.1', port: 0 } });
  const browser = await chromium.launch({ channel: 'chromium', executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || undefined });
  try {
    await server.listen();
    const context = await browser.newContext({ locale: 'en-US' });
    await withBrowserDiagnostics(context, 'desktop-releases', async () => {
      const page = await context.newPage();
      let catalog = sample();
      await page.route('**/releases/desktop/catalog.json', route => route.fulfill({ json: catalog }));
      await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/en/`);
      const section = page.locator('#install');
      await expect(section.getByRole('button', { name: 'Desktop app', exact: true })).toHaveAttribute('aria-pressed', 'true');
      await expect(section.getByText('Full · includes Runtime')).toBeVisible();
      await expect(section.getByRole('heading', { level: 3 })).toHaveCount(0);
      await expect(section.getByRole('link', { name: 'Installation guide' })).toHaveAttribute('href', '/en/docs/admin/deployment/installation/');
      await expect(section.getByRole('link', { name: 'Download installer' })).toHaveAttribute('href', platform.url);
      await expect(section.getByText(/Unsigned test build/)).toBeVisible();
      const macFile = 'molr-desktop_0.1.0_darwin-arm64_full.dmg';
      catalog.channels.stable.platforms = [{ ...platform, os: 'darwin', arch: 'arm64', variant: 'full', file: macFile, url: `https://downloads.example.com/${macFile}` }];
      await page.reload();
      await expect(section.getByRole('button', { name: 'Desktop app', exact: true })).toHaveAttribute('aria-pressed', 'true');
      await expect(section.getByText(/Unnotarized test build/)).toBeVisible();
      catalog.channels.stable.platforms[0].signed = true;
      await page.reload();
      await expect(section.getByRole('button', { name: 'Desktop app', exact: true })).toHaveAttribute('aria-pressed', 'true');
      await expect(section.getByText(/Signed, not notarized/)).toBeVisible();
      await expect(page.locator('.home-download-signing')).toContainText('Not notarized by Apple');
      catalog.channels.stable.platforms[0].notarized = true;
      await page.reload();
      await expect(section.getByRole('button', { name: 'Desktop app', exact: true })).toHaveAttribute('aria-pressed', 'true');
      await expect(section.getByRole('link', { name: 'Download installer' })).toBeVisible();
      await expect(section.getByText(/Unnotarized test build|Signed, not notarized/)).toHaveCount(0);
      await expect(page.locator('.home-download-signing')).toHaveCount(0);
      for (const [channel, version] of [['preview', '0.2.0-rc.6'], ['nightly', '0.2.0-0.nightly.20261011.0.gaaaaaaa']]) {
        const prereleaseFile = `molr-desktop_${version}_win32-amd64_full.exe`;
        catalog = { schema: 1, channels: { stable: null, preview: null, nightly: null,
          [channel]: { version, platforms: [{ ...platform, file: prereleaseFile, url: `https://downloads.example.com/${prereleaseFile}` }] } } };
        await page.reload();
        await expect(section.getByRole('button', { name: 'Desktop app', exact: true })).toHaveAttribute('aria-pressed', 'true');
        await expect(section.getByText('Coming soon')).toBeVisible();
        await expect(section.getByRole('link', { name: 'Download installer' })).toHaveCount(0);
        await expect(page.locator('.home-hero-download .home-download-primary')).toHaveCount(0);
      }
      catalog = { schema: 1, channels: { stable: null, preview: null, nightly: null } };
      await page.reload();
      await expect(section.getByRole('button', { name: 'Desktop app', exact: true })).toHaveAttribute('aria-pressed', 'true');
      await expect(section.getByText('Coming soon')).toBeVisible();
      await expect(section.getByRole('link', { name: 'Download installer' })).toHaveCount(0);
    }, t.signal);
  } finally { await browser.close(); await server.close(); if (before === undefined) delete process.env.VITE_SITE_MODE; else process.env.VITE_SITE_MODE = before; }
});
