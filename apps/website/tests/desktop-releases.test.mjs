import test from 'node:test';
import assert from 'node:assert/strict';
import { readDesktopCatalog } from '../src/desktop-releases.ts';
import { createServer } from 'vite';
import { chromium, expect } from '@playwright/test';
import { resolve } from 'node:path';
const file = 'molr-desktop_0.1.0_win32-amd64_client.exe';
const platform = { os: 'win32', arch: 'amd64', variant: 'client', signed: false, file, sha256: 'a'.repeat(64), url: `https://downloads.example.com/${file}` };
const sample = () => ({ schema: 1, channels: { stable: { version: '0.1.0', platforms: [{ ...platform }] }, preview: null, nightly: null } });
test('desktop catalog enforces Windows client edition and HTTPS artifact identity', () => {
  assert.deepEqual(readDesktopCatalog(sample()), sample());
  for (const change of [{ signed: undefined }, { signed: 'false' }, { variant: 'full' }, { url: 'javascript:alert(1)' }, { url: `http://downloads.example.com/${file}` }, { file: '../private' }, { sha256: 'bad' }]) {
    const c = sample(); Object.assign(c.channels.stable.platforms[0], change); assert.throws(() => readDesktopCatalog(c));
  }
  const duplicate = sample(); duplicate.channels.stable.platforms.push(platform); assert.throws(() => readDesktopCatalog(duplicate));
});
test('desktop catalogs never publish unknown metadata or local builds', () => {
  const c = sample(); c.private = 'fixture'; c.channels.stable.private = 'fixture'; c.channels.stable.platforms[0].private = 'fixture';
  assert(!JSON.stringify(readDesktopCatalog(c)).includes('fixture'));
  c.channels.stable.version = '0.1.0-local'; assert.throws(() => readDesktopCatalog(c));
});
test('desktop download UI shows edition and does not invent missing releases', { timeout: 60000 }, async () => {
  const before = process.env.VITE_SITE_MODE; process.env.VITE_SITE_MODE = 'live';
  const server = await createServer({ root: resolve(import.meta.dirname, '..'), logLevel: 'silent', server: { host: '127.0.0.1', port: 0 } });
  const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || undefined });
  try {
    await server.listen();
    const page = await browser.newPage({ locale: 'en-US' });
    let catalog = sample();
    await page.route('**/releases/desktop/catalog.json', route => route.fulfill({ json: catalog }));
    await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/en/`);
    const section = page.locator('#install');
    await section.getByRole('button', { name: 'Desktop app', exact: true }).click();
    await expect(section.getByText('Client · remote connections only')).toBeVisible();
    await expect(section.getByRole('link', { name: 'Installation guide' })).toHaveAttribute('href', '/en/docs/admin/deployment/installation/');
    await expect(section.getByRole('link', { name: 'Download installer' })).toHaveAttribute('href', platform.url);
    await expect(section.getByText(/Unsigned test build/)).toBeVisible();
    const macFile = 'molr-desktop_0.1.0_darwin-arm64_full.dmg';
    catalog.channels.stable.platforms = [{ ...platform, os: 'darwin', arch: 'arm64', variant: 'full', file: macFile, url: `https://downloads.example.com/${macFile}` }];
    await page.reload();
    await section.getByRole('button', { name: 'Desktop app', exact: true }).click();
    await expect(section.getByText(/Unnotarized test build/)).toBeVisible();
    catalog.channels.stable.platforms[0].signed = true;
    await page.reload();
    await section.getByRole('button', { name: 'Desktop app', exact: true }).click();
    await expect(section.getByRole('link', { name: 'Download installer' })).toBeVisible();
    await expect(section.getByText(/Unnotarized test build/)).toHaveCount(0);
    await section.locator('.home-install-advanced > summary').click();
    await section.getByRole('group', { name: 'Desktop release channel' }).getByRole('button', { name: 'Preview', exact: true }).click();
    await expect(section.getByText('No desktop installers have been published in this channel.')).toBeVisible();
    await expect(section.getByRole('link', { name: 'Download installer' })).toHaveCount(0);
  } finally { await browser.close(); await server.close(); if (before === undefined) delete process.env.VITE_SITE_MODE; else process.env.VITE_SITE_MODE = before; }
});
