import assert from 'node:assert/strict';
import { test } from 'node:test';
import { chromium, expect } from '@playwright/test';
import { createServer } from 'vite';
import { resolve } from 'node:path';
import { readFileSync } from 'node:fs';
import { readCatalog, installCommand } from '../src/release-data.ts';
const platforms = JSON.parse(readFileSync(new URL('../../../release/platforms.json', import.meta.url))).platforms;
function release(version, channel, targets = platforms) {
  const base = `https://github.com/example/molr/releases/download/v${version}`;
  return { schema: 1, version, channel, commit: 'a'.repeat(40), published_at: '2026-10-11T00:00:00Z', notes_url: `https://github.com/example/molr/releases/tag/v${version}`, platforms: targets.map(p => ({ ...p, file: `molr_${version}_${p.os}_${p.arch}.tar.gz`, sha256: 'c'.repeat(64), url: `${base}/molr_${version}_${p.os}_${p.arch}.tar.gz`, checksum_url: `${base}/molr_${version}_${p.os}_${p.arch}.tar.gz.sha256` })) };
}
const catalog = { schema: 1, channels: { stable: release('0.1.0', 'stable'), preview: release('0.2.0-rc.1', 'preview'), nightly: release('0.2.0-0.nightly.20261011.0.gaaaaaaa', 'nightly') } };
test('default commands use latest stable and reject unsafe catalog links', () => {
  assert.equal(readCatalog(catalog), catalog);
  for (const patch of [{ url: "https://example.com/a'bad" }, { file: '$(touch injected).tar.gz' }]) {
    const bad = structuredClone(catalog); Object.assign(bad.channels.stable.platforms[0], patch); assert.throws(() => readCatalog(bad));
  }
  assert.equal(installCommand(), 'curl -fsSL https://molr.ai/install.sh | sh -s -- --mode server');
  assert.equal(installCommand('sandbox'), 'curl -fsSL https://molr.ai/install.sh | sh -s -- --mode sandbox');
});
test('service installation offers command and Agent methods with stable-only, mode, copy and error flows', { timeout: 90000 }, async () => {
  const before = process.env.VITE_SITE_MODE; process.env.VITE_SITE_MODE = 'live';
  const server = await createServer({ root: resolve(import.meta.dirname, '..'), logLevel: 'silent', server: { host: '127.0.0.1', port: 0 } });
  const browser = await chromium.launch({ channel: 'chromium', executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || undefined });
  try {
    await server.listen();
    const page = await browser.newPage({ locale: 'en-US', permissions: ['clipboard-read', 'clipboard-write'] });
    let response = catalog;
    await page.route('**/releases/catalog.json', route => route.fulfill({ json: response }));
    await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/`);
    const section = page.locator('#install');
    await expect(section.getByText('v0.1.0', { exact: true })).toBeVisible();
    await expect(section.getByRole('group', { name: 'Installation entry' }).getByRole('button')).toHaveCount(3);
    await expect(section.getByRole('button', { name: /^(Stable|Preview|Nightly)$/ })).toHaveCount(0);
    await expect(section.locator('pre')).toHaveText(installCommand());
    await expect(section.locator('.home-install-header .home-install-version')).toHaveText('v0.1.0');
    await expect(section.getByRole('radio')).toHaveCount(0);
    await expect(section.getByRole('heading', { level: 3 })).toHaveCount(0);
    await expect(section.locator('.home-install-advanced')).toHaveCount(0);
    await expect(section.locator('pre')).not.toContainText('--channel');
    await expect(section.locator('pre')).not.toContainText('--arch');
    await section.getByRole('button', { name: 'Copy command', exact: true }).click();
    assert.equal(await page.evaluate(() => navigator.clipboard.readText()), installCommand());
    await expect(section.getByRole('tablist', { name: 'Installation method', exact: true }).getByRole('tab')).toHaveText(['Command line', 'Let an Agent install for me']);
    await expect(section.locator('.home-install-role')).toHaveCSS('border-bottom-width', '0px');
    await expect(section.getByRole('link', { name: 'Download package', exact: true })).toHaveCount(0);
    await section.getByRole('tab', { name: 'Command line', exact: true }).focus();
    await page.keyboard.press('End');
    await expect(section.getByRole('tab', { name: 'Let an Agent install for me', exact: true })).toHaveAttribute('aria-selected', 'true');
    await expect(section.locator('.home-install-prompt-text')).toContainText('--mode server');
    await section.getByRole('button', { name: 'Deploy sandbox', exact: true }).click();
    await expect(section.getByRole('tab')).toHaveCount(2);
    await expect(section.locator('.home-install-prompt-text')).toContainText('--mode sandbox');
    await section.getByRole('tab', { name: 'Command line', exact: true }).click();
    await expect(section.locator('pre')).toHaveText(installCommand('sandbox'));
    await expect(section.getByRole('heading', { level: 3 })).toHaveCount(0);
    await expect(section.getByRole('link', { name: 'Installation guide' })).toHaveAttribute('href', '/en/docs/admin/sandbox/remote/');
    await section.getByRole('button', { name: 'Desktop app', exact: true }).click();
    await section.getByRole('button', { name: 'Deploy backend', exact: true }).click();
    await expect(section.locator('pre')).toHaveText(installCommand());
    response = { schema: 1, channels: { stable: null, preview: catalog.channels.preview, nightly: null } };
    await page.reload();
    await expect(section.getByRole('status')).toContainText('Coming soon');
    await expect(section.locator('pre')).toHaveCount(0);
    await expect(section.locator('.home-install-version')).toHaveCount(0);
    response = { schema: 1, channels: { stable: null, preview: null, nightly: null } };
    await page.reload();
    await expect(section.getByRole('status')).toContainText('Coming soon');
    await expect(section.locator('.home-install-advanced')).toHaveCount(0);
    await expect(section.getByRole('link', { name: 'Download package', exact: true })).toHaveCount(0);
    response = {};
    await page.reload();
    await expect(section.getByRole('alert')).toContainText('temporarily unavailable');
    await expect(section.locator('.home-install-advanced')).toHaveCount(0);
    await section.getByRole('tab', { name: 'Let an Agent install for me', exact: true }).click();
    await expect(section.locator('.home-install-status')).toContainText('You can copy the instructions now');
    response = catalog;
    await section.getByRole('button', { name: 'Retry', exact: true }).click();
    await expect(section.getByText('v0.1.0', { exact: true })).toBeVisible();
    await section.getByRole('tab', { name: 'Command line', exact: true }).click();
    await expect(section.locator('pre')).toHaveText(installCommand());
    await section.getByRole('link', { name: 'Installation guide' }).click();
    await expect(page.locator('.docs-prose')).toContainText('tar -xzf');
    await page.locator('.docs-prose').getByRole('link', { name: 'Docker deployment', exact: true }).first().click();
    await expect(page).toHaveURL(/\/en\/docs\/admin\/deployment\/docker\/$/);
    await expect(page.locator('.docs-prose')).toContainText('docker compose');
  } finally {
    await browser.close(); await server.close();
    if (before === undefined) delete process.env.VITE_SITE_MODE; else process.env.VITE_SITE_MODE = before;
  }
});
