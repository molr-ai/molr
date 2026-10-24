import assert from 'node:assert/strict';
import { test } from 'node:test';
import { chromium, expect } from '@playwright/test';
import { createServer } from 'vite';
import { mkdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { documentReadyTimeout } from './browser-support.mjs';

const root = resolve(import.meta.dirname, '..');
const repository = resolve(root, '../..');

test('agent installation instructions work across modes, locales, catalog failures and narrow screens', { timeout: 120000 }, async () => {
  const before = process.env.VITE_SITE_MODE;
  process.env.VITE_SITE_MODE = 'live';
  const server = await createServer({ root, logLevel: 'silent', server: { host: '127.0.0.1', port: 0 } });
  let browser;
  try {
    browser = await chromium.launch({ channel: 'chromium', executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || undefined });
    await server.listen();
    const url = `http://127.0.0.1:${server.httpServer.address().port}`;
    for (const [path, source] of [['/agent-install.md', 'docs/admin/deployment/agent-install.md'], ['/en/agent-install.md', 'docs/en/admin/deployment/agent-install.md']]) {
      const response = await fetch(`${url}${path}`);
      assert.equal(response.status, 200);
      assert.match(response.headers.get('content-type'), /^text\/plain; charset=utf-8$/);
      assert.equal(response.headers.get('cache-control'), 'no-cache');
      assert.equal(await response.text(), readFileSync(resolve(repository, source), 'utf8'));
    }
    const context = await browser.newContext({ locale: 'en-US', reducedMotion: 'reduce', permissions: ['clipboard-read', 'clipboard-write'] });
    const page = await context.newPage();
    let failure = false;
    let response = { schema: 1, channels: { stable: null, preview: null, nightly: null } };
    await page.route('**/releases/catalog.json', route => failure ? route.abort() : route.fulfill({ json: response }));
    await page.goto(url);
    const section = page.locator('#install');
    await section.getByRole('button', { name: 'Deploy backend', exact: true }).click();
    await section.getByRole('tab', { name: 'Let an Agent install for me', exact: true }).click();
    await expect(section.getByRole('group', { name: 'Release channel', exact: true })).toHaveCount(0);
    for (const name of ['Operating system', 'Architecture', 'Download tool']) await expect(section.getByRole('group', { name, exact: true })).toHaveCount(0);
    await expect(section.locator('pre')).toHaveCount(0);
    await expect(section.getByRole('heading', { level: 3 })).toHaveCount(0);
    const prompt = section.locator('.home-install-prompt-text');
    await expect(prompt).not.toContainText('channel');
    await expect(prompt).toContainText('https://molr.ai/en/agent-install.md');
    await expect(section.locator('.home-install-status')).toContainText('coming soon');
    await section.getByRole('button', { name: 'Copy installation instructions', exact: true }).click();
    assert.equal(await page.evaluate(() => navigator.clipboard.readText()), await prompt.innerText());
    await expect(prompt).toContainText('--mode server');
    await expect(prompt).toContainText('one-time login link');
    await section.getByRole('button', { name: 'Deploy sandbox', exact: true }).click();
    await expect(prompt).toContainText('--mode sandbox');
    await expect(prompt).toContainText('node address');
    await expect(prompt).not.toContainText('one-time login link');
    const screenshots = resolve(root, 'test-results/agent-install');
    mkdirSync(screenshots, { recursive: true });
    for (const width of [320, 1440]) {
      await page.setViewportSize({ width, height: 1000 });
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      await section.screenshot({ path: `${screenshots}/en-${width}.png` });
    }
    await page.getByRole('button', { name: '切换到中文', exact: true }).click();
    await expect(prompt).toContainText('正式渠道最新版 Molr');
    await expect(prompt).toContainText('https://molr.ai/agent-install.md');
    await section.getByRole('button', { name: '复制安装指令', exact: true }).click();
    assert.equal(await page.evaluate(() => navigator.clipboard.readText()), await prompt.innerText());
    await expect(section.getByRole('link', { name: '查看 Agent 安装说明' })).toHaveAttribute('href', '/zh/docs/admin/deployment/agent-install/');
    for (const theme of ['light', 'dark']) for (const palette of ['forest', 'ocean', 'iris', 'sand', 'rose', 'monochrome']) for (const width of [320, 1440]) {
      await page.setViewportSize({ width, height: 1000 });
      await page.locator('html').evaluate((el, { theme, palette }) => { el.dataset.theme = theme; el.dataset.palette = palette; }, { theme, palette });
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      await section.screenshot({ path: `${screenshots}/${theme}-${palette}-${width}.png` });
    }
    await section.getByRole('link', { name: '查看 Agent 安装说明' }).click();
    await expect(page).toHaveURL(`${url}/zh/docs/admin/deployment/agent-install/`);
    // The first docs visit loads and transforms the lazy module in the dev server.
    // React keeps the homepage visible during that transition; URL changes alone
    // do not mean the document is ready, especially on slower CI runners.
    await expect(page.getByRole('heading', { level: 1, name: '让 Agent 为我安装', exact: true })).toBeVisible({ timeout: documentReadyTimeout });
    await page.goto(url);
    failure = true;
    await page.reload();
    await page.locator('#install').getByRole('button', { name: '部署后端', exact: true }).click();
    await page.locator('#install').getByRole('tab', { name: '让 Agent 为我安装', exact: true }).click();
    await expect(section.locator('.home-install-status')).toContainText('可先复制指令');
    await section.getByRole('button', { name: '复制安装指令', exact: true }).click();
    await expect(section.getByRole('button', { name: '已复制', exact: true })).toBeVisible();
    // A successful retry updates availability while retaining the agent flow.
    failure = false;
    const platform = JSON.parse(readFileSync(resolve(repository, 'release/platforms.json'), 'utf8')).platforms[0];
    const file = `molr_0.1.0_${platform.os}_${platform.arch}.tar.gz`;
    response.channels.stable = { schema: 1, version: '0.1.0', channel: 'stable', commit: 'a'.repeat(40), published_at: '2026-10-15T00:00:00Z', notes_url: 'https://example.com/notes', platforms: [{ ...platform, file, sha256: 'b'.repeat(64), url: `https://example.com/${file}`, checksum_url: `https://example.com/${file}.sha256` }] };
    await section.getByRole('button', { name: '重试', exact: true }).click();
    await expect(section.locator('.home-install-header .home-install-version')).toHaveText('v0.1.0');
    await expect(prompt).toContainText('正式渠道最新版 Molr');
    await expect(prompt).not.toContainText('v0.1.0');
    // Exercise the LAN HTTP clipboard fallback and its manual-copy error message.
    await page.evaluate(() => {
      Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined });
      document.execCommand = (command) => { window.copiedFallback = Array.from(document.querySelectorAll('textarea')).at(-1)?.value; return command === 'copy'; };
    });
    await expect(section.getByRole('button', { name: '复制安装指令', exact: true })).toBeVisible();
    await section.getByRole('button', { name: '复制安装指令', exact: true }).click();
    assert.equal(await page.evaluate(() => window.copiedFallback), await prompt.innerText());
    await page.evaluate(() => { document.execCommand = () => false; });
    await section.getByRole('button', { name: '已复制', exact: true }).click();
    await expect(section.getByText('复制失败，请选中文字手动复制。', { exact: true })).toBeVisible();
    await expect(prompt).toBeVisible();
  } finally {
    await browser?.close(); await server.close();
    if (before === undefined) delete process.env.VITE_SITE_MODE; else process.env.VITE_SITE_MODE = before;
  }
});
