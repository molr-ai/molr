import assert from 'node:assert/strict';
import { checkDocsFeatures } from './docs-features-checks.mjs';
import { checkDocsSearch } from './docs-search-checks.mjs';
import { checkDocs } from './docs-checks.mjs';
import { mkdtemp, mkdir, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { chromium, expect } from '@playwright/test';
import { build, preview } from 'vite';

const root = fileURLToPath(new URL('../', import.meta.url));
const headings = {
  'coming-soon': { zh: '即将上线', en: 'Coming soon' },
  maintenance: { zh: '马上回来', en: 'Be right back' },
  live: { zh: '从好奇出发。', en: 'It starts with curiosity.' },
};

test('website state builds and browser behavior', { timeout: 240_000 }, async t => {
  const temporary = await mkdtemp(join(tmpdir(), 'molr-website-'));
  const screenshots = join(root, 'test-results', 'site-modes');
  await mkdir(screenshots, { recursive: true });
  const originalMode = process.env.VITE_SITE_MODE;
  const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || undefined });
  try {
    for (const [mode, copy] of Object.entries(headings)) {
      process.env.VITE_SITE_MODE = mode;
      const outDir = join(temporary, mode);
      await build({ root, logLevel: 'silent', build: { outDir, emptyOutDir: true } });
      const server = await preview({ root, logLevel: 'silent', build: { outDir }, preview: { host: '0.0.0.0', port: 0, strictPort: false } });
      const address = server.httpServer.address();
      assert(address && typeof address !== 'string');
      const url = `http://127.0.0.1:${address.port}`;
      try {
        await t.test(`${mode}: generated metadata and fallback match the configured page`, async () => {
          const html = await readFile(join(outDir, 'index.html'), 'utf8');
          assert(html.includes(`data-site-mode="${mode}"`));
          assert(!html.includes('__SITE_'));
          const installer = await fetch(`${url}/install.sh`);
          assert.equal(installer.status, 200);
          assert((await installer.text()).startsWith('#!/bin/sh'));
          assert((await readFile(join(outDir, 'install.sh'), 'utf8')).includes('Checksum mismatch'));
          for (const [path, source] of [['agent-install.md', 'docs/admin/deployment/agent-install.md'], ['en/agent-install.md', 'docs/en/admin/deployment/agent-install.md']]) {
            const expected = await readFile(join(root, '../..', source), 'utf8');
            assert.equal(await readFile(join(outDir, path), 'utf8'), expected);
            const response = await fetch(`${url}/${path}`);
            assert.equal(response.status, 200);
            assert.equal(await response.text(), expected);
            const headers = await readFile(join(outDir, '_headers'), 'utf8');
            assert(headers.includes(`/${path}\n  Cache-Control: no-cache\n  Content-Type: text/plain; charset=utf-8`));
          }
          const context = await browser.newContext({ javaScriptEnabled: false });
          try {
            const page = await context.newPage();
            await page.goto(url);
            await expect(page.getByRole('heading', { level: 1 })).toContainText(copy.zh);
            await expect(page.getByRole('heading', { level: 1 })).toContainText(copy.en);
            await expect(page).toHaveTitle(new RegExp(copy.en.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')));
            for (const selector of ['meta[name="description"]', 'meta[property="og:title"]', 'meta[property="og:description"]']) {
              assert((await page.locator(selector).getAttribute('content'))?.includes(copy.en.replace(/\.$/, '')));
            }
          } finally { await context.close(); }
        });

        for (const locale of ['en-US', 'zh-CN']) {
          for (const mobile of [false, true]) {
            const colorScheme = mobile ? 'dark' : 'light';
            await t.test(`${mode}: ${locale}, ${mobile ? 'mobile dark' : 'desktop light'}`, async () => {
              const context = await browser.newContext({ locale, colorScheme, reducedMotion: 'reduce', viewport: mobile ? { width: 320, height: 700 } : { width: 1360, height: 1000 } });
              try {
                const page = await context.newPage();
                const errors = [];
                page.on('pageerror', error => errors.push(error.message));
                await page.goto(url);
                const heading = copy[locale === 'zh-CN' ? 'zh' : 'en'];
                await expect(page.getByRole('heading', { level: 1, name: heading, exact: true })).toBeVisible();
                await expect(page.locator('html')).toHaveAttribute('lang', locale === 'zh-CN' ? 'zh-CN' : 'en');
                await expect(page.locator('html')).toHaveAttribute('data-theme', colorScheme);
                assert((await page.title()).includes(heading.replace(/\.$|。$/, '')));
                for (const selector of ['meta[name="description"]', 'meta[property="og:title"]', 'meta[property="og:description"]']) {
                  assert((await page.locator(selector).getAttribute('content'))?.includes(heading.replace(/\.$|。$/, '')));
                }
                await expect(page.getByRole('navigation')).toHaveCount(mode === 'live' ? 1 : 0);
                await expect(page.getByRole('contentinfo')).toHaveCount(mode === 'live' ? 1 : 0);
                await expect(page.locator('#home-title')).toHaveCount(mode === 'live' ? 1 : 0);
                await expect(page.locator('.launch-phrase')).toHaveCount(mode === 'live' ? 0 : 1);
                if (mode === 'live') {
                  await expect(page.getByText(/场景示意|示例内容|开发中|Sample content|In development|A work in progress/i)).toHaveCount(0);
                }
                if (mode !== 'live') {
                  await expect(page.locator('.launch-phrase')).toHaveText(heading);
                  await expect(page.locator('.launch-phrase')).toHaveCSS('animation-name', 'none');
                }
                assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
                await page.screenshot({ path: join(screenshots, `${mode}-${locale}-${mobile ? 'mobile' : 'desktop'}.png`), fullPage: mode === 'live' });
                assert.deepEqual(errors, []);
              } finally { await context.close(); }
            });
          }
        }

        if (mode === 'live') {
          await t.test('documentation routes, source links, search, anchors and responsive reading', async () => {
            await checkDocs({ browser, url, outDir, screenshots });
          });
          await t.test('documentation search details and prompt copying', async () => {
            await checkDocsFeatures({ browser, url });
          });
          await t.test('documentation search preserves articles and diagrams', async () => {
            await checkDocsSearch({ browser, url });
          });
          await t.test('homepage controls, persisted preferences and honest placeholder destinations', async () => {
            const context = await browser.newContext({ locale: 'en-US', colorScheme: 'dark', viewport: { width: 1440, height: 1000 } });
            try {
              const page = await context.newPage();
              await page.goto(url);
              await expect(page.getByRole('link', { name: 'Skip to content' })).toBeAttached();
              await page.keyboard.press('Tab');
              await expect(page.getByRole('link', { name: 'Skip to content' })).toBeFocused();
              await page.getByRole('button', { name: '切换到中文', exact: true }).click();
              await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN');
              await page.getByRole('button', { name: '切换浅色模式', exact: true }).click();
              await page.reload();
              await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN');
              await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
              await expect(page.locator('meta[name="description"]')).toHaveAttribute('content', /个人 AI 助理/);
              await page.getByRole('button', { name: '安排计划', exact: true }).click();
              await expect(page.getByRole('button', { name: '安排计划', exact: true })).toHaveAttribute('aria-pressed', 'true');
              await expect(page.locator('.home-product-main')).toContainText('近期安排');
              await page.getByRole('button', { name: '连接资源', exact: true }).click();
              await expect(page.locator('.home-product-main')).toContainText('OpenAI-compatible');
              const docsMenu = page.getByRole('button', { name: '文档', exact: true });
              await docsMenu.focus();
              await page.keyboard.press('ArrowDown');
              await expect(page.getByRole('menuitemradio', { name: '用户文档', exact: true })).toBeFocused();
              await page.keyboard.press('ArrowDown');
              await expect(page.getByRole('menuitemradio', { name: '管理员文档', exact: true })).toBeFocused();
              await page.keyboard.press('ArrowDown');
              await expect(page.getByRole('menuitemradio', { name: '开发者文档', exact: true })).toBeFocused();
              await page.keyboard.press('Escape');
              await expect(docsMenu).toBeFocused();
              await expect(page.getByRole('menu', { name: '文档', exact: true })).toBeHidden();
              for (const name of ['GitHub']) {
                const link = page.getByRole('navigation').getByRole('link', { name, exact: false });
                await expect(link).toHaveAttribute('href', 'https://github.com/molr-ai/molr');
                await expect(link).not.toHaveAttribute('aria-disabled', 'true');
              }
              await expect(page.locator('.home-download-actions').getByRole('link')).toHaveCount(0);
              await expect(page.locator('.home-download-actions').getByRole('link', { name: '查看文档', exact: true })).toHaveCount(0);
              await page.locator('.home-hero-download').getByRole('link', { name: '更多安装方式', exact: true }).click();
              await expect(page).toHaveURL(/#install$/);
              await expect(page.getByRole('heading', { name: '给好奇心，一个工作空间。' })).toBeInViewport();
              const installer = page.locator('#install');
              await expect(installer.getByRole('link', { name: '详细安装指南' })).toHaveAttribute('href', '/zh/docs/admin/deployment/installation/');
              await expect(installer.locator('.home-install-advanced')).toHaveCount(0);
              await expect(installer.getByRole('button', { name: /正式版|预发布版|Nightly/ })).toHaveCount(0);
              await expect(installer.getByRole('status')).toContainText('即将推出');
              await expect(installer.getByRole('link', { name: '下载安装包', exact: true })).toHaveCount(0);
              await page.getByRole('button', { name: 'Switch to English', exact: true }).click();
              await page.getByRole('button', { name: 'Switch to dark mode', exact: true }).click();
              await page.reload();
              await expect(page.locator('html')).toHaveAttribute('lang', 'en');
              await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
              for (const width of [390, 768, 1440]) {
                await page.setViewportSize({ width, height: 900 });
                await page.getByRole('button', { name: 'Think together' }).click();
                assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
                await page.screenshot({ path: join(screenshots, `live-en-dark-${width}.png`), fullPage: true });
              }
            } finally { await context.close(); }
          });
          await t.test('homepage language choice works when browser storage is unavailable', async () => {
            const context = await browser.newContext({ locale: 'en-US' });
            try {
              await context.addInitScript(() => {
                Object.defineProperty(window, 'localStorage', { get() { throw new DOMException('Unavailable', 'SecurityError'); } });
              });
              const page = await context.newPage();
              await page.goto(url);
              await page.getByRole('button', { name: '切换到中文', exact: true }).click();
              await page.evaluate(() => window.dispatchEvent(new Event('focus')));
              await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN');
              await page.getByRole('button', { name: '切换暗色模式', exact: true }).click();
              await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
            } finally { await context.close(); }
          });
        }

        if (mode !== 'live') {
          await t.test(`${mode}: multilingual rotation, background pause and reduced motion`, async () => {
            const context = await browser.newContext({ locale: 'en-US', viewport: { width: 320, height: 700 } });
            try {
              const page = await context.newPage();
              await page.clock.install();
              await page.goto(url);
              const phrase = page.locator('.launch-phrase');
              await expect(phrase).toHaveText(copy.en);
              await expect(page.locator('#site-status-title')).toHaveAttribute('aria-label', copy.en);
              await expect(phrase).toHaveAttribute('aria-hidden', 'true');
              for (const language of ['ja', 'fr', 'ko', 'es', 'de', 'it', 'pt', 'ar', 'zh-CN', 'en']) {
                await page.clock.fastForward(5200);
                await expect(phrase).toHaveAttribute('lang', language);
                await expect(phrase).toHaveAttribute('dir', language === 'ar' ? 'rtl' : 'ltr');
                assert(await phrase.evaluate(element => element.scrollWidth <= element.clientWidth));
                assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
                await expect(page.locator('#site-status-title')).toHaveAttribute('aria-label', copy.en);
              }
              await page.evaluate(() => {
                Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
                document.dispatchEvent(new Event('visibilitychange'));
              });
              await page.clock.fastForward(20_000);
              await expect(phrase).toHaveText(copy.en);
              await page.evaluate(() => {
                Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
                document.dispatchEvent(new Event('visibilitychange'));
              });
              await page.clock.fastForward(5200);
              await expect(phrase).toHaveAttribute('lang', 'ja');
              await page.emulateMedia({ reducedMotion: 'reduce' });
              await expect(phrase).toHaveText(copy.en);
              await page.clock.fastForward(20_000);
              await expect(phrase).toHaveText(copy.en);
            } finally { await context.close(); }
          });
        }
      } finally {
        await new Promise((resolve, reject) => server.httpServer.close(error => error ? reject(error) : resolve()));
      }
    }
    await t.test('invalid configuration is rejected instead of silently showing the wrong page', async () => {
      process.env.VITE_SITE_MODE = 'unknown';
      await assert.rejects(build({ root, logLevel: 'silent', build: { outDir: join(temporary, 'invalid'), emptyOutDir: true } }), /Invalid VITE_SITE_MODE/);
    });
  } finally {
    await browser.close();
    if (originalMode === undefined) delete process.env.VITE_SITE_MODE;
    else process.env.VITE_SITE_MODE = originalMode;
    await rm(temporary, { recursive: true, force: true });
  }
});
