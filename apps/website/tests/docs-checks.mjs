import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { expect } from '@playwright/test';
import { allDocRoutes } from '../src/docs-routes.ts';
import { documentReadyTimeout, withBrowserDiagnostics } from './browser-support.mjs';

export async function checkDocs({ browser, url, outDir, screenshots, signal }) {
  const pages = await Promise.all(allDocRoutes.map(async route => {
    const source = await readFile(new URL(`../../../${route.root}/${route.file}`, import.meta.url), 'utf8');
    return [route.path, /^# (.+)$/m.exec(source)[1], route.locale];
  }));
  const context = await browser.newContext({ locale: 'zh-CN', colorScheme: 'light', viewport: { width: 1440, height: 1000 } });
  await withBrowserDiagnostics(context, 'documentation', async () => {
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.goto(url);
    await page.getByRole('button', { name: '文档', exact: true }).click();
    await page.getByRole('menuitemradio', { name: '用户文档', exact: true }).click();
    await expect(page).toHaveURL(`${url}/zh/docs/user/`);
    for (const [path, title, locale] of pages) {
      const html = await readFile(join(outDir, path, 'index.html'), 'utf8');
      const escapedTitle = title.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('"', '&quot;').replaceAll("'", '&#39;');
      assert(html.includes(`<title>${escapedTitle} · Molr Docs</title>`));
      assert(html.includes(`href="https://molr.ai${path}"`));
      await page.goto(`${url}${path}`);
      await expect(page.getByRole('heading', { level: 1, name: title, exact: true })).toBeVisible();
      await expect(page).toHaveTitle(`${title} · Molr Docs`);
      await expect(page.locator('html')).toHaveAttribute('lang', locale === 'zh' ? 'zh-CN' : 'en');
      assert(html.includes(`<html lang="${locale === 'zh' ? 'zh-CN' : 'en'}"`));
      assert(html.includes('hreflang="zh-CN"') && html.includes('hreflang="en"'));
      assert(await page.locator('.docs-prose a').evaluateAll((links, language) => links.every(link => link.origin !== location.origin || link.pathname.startsWith(`/${language}/`) || link.pathname === '/'), locale));
      await expect(page.getByRole('navigation', { name: locale === 'zh' ? '文档目录' : 'Documentation navigation' }).locator('[aria-current="page"]'), path).toHaveCount(1);
      const missingAnchors = await page.locator('.docs-toc a').evaluateAll(links => links
        .map(link => link.getAttribute('href'))
        .filter(hash => !document.getElementById(decodeURIComponent(hash.slice(1)))));
      assert.deepEqual(missingAnchors, [], `Missing table-of-contents anchors in ${path}`);
      assert(await page.locator('.docs-prose a').evaluateAll(links => links.every(link => {
        const target = new URL(link.href);
        return target.origin !== location.origin || !target.pathname.endsWith('.md');
      })), `Unresolved local Markdown link in ${path}`);
      const sectionPath = path.match(/^\/(zh|en)\/docs\/(user|admin|dev)\//)?.[0] ?? `/${locale}/docs/`;
      assert(await page.locator('.docs-nav-group-links a, .docs-pagination a').evaluateAll((links, prefix) => links.every(link => link.pathname.startsWith(prefix)), sectionPath), `Cross-guide navigation: ${path}`);
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    }
    for (const removed of ['/zh/docs/dev/product/prd/', '/zh/docs/dev/research/', '/zh/docs/admin/deployment/multi-user-upgrade/', '/zh/docs/dev/product-scope/', '/zh/docs/assistants-and-plans/']) {
      await page.goto(`${url}${removed}`);
      await expect(page.getByRole('heading', { level: 1 })).toHaveText('未找到文档');
    }
    await page.goto(`${url}/zh/docs/dev/`);
    await page.getByRole('button', { name: '搜索文档', exact: true }).click();
    await page.getByRole('searchbox', { name: '搜索文档' }).fill('候选试点');
    await expect(page.getByRole('navigation', { name: '搜索结果' }).getByRole('status')).toContainText('没有找到');
    await page.getByRole('searchbox', { name: '搜索文档' }).fill('');
    await page.keyboard.press('Escape');
    await checkClientNavigation(page, url);
    await checkLanguageNavigation(page, url);
    await page.goto(`${url}/zh/docs/admin/getting-started/`);
    await expect(page.locator('.docs-prose .docs-warning')).toHaveCount(2);
    await expect(page.locator('.docs-prose .docs-warning').first()).toContainText('破坏性变更');
    await expect(page.locator('.docs-prose .docs-warning').last()).toContainText('直接暴露到公网');
    await expect(page.locator('.docs-prose')).not.toContainText('[!WARNING]');
    await expect(page.locator('.docs-prose')).not.toContainText('<!-- -->');
    await page.getByRole('link', { name: '配置与访问', exact: true }).last().click();
    await page.locator('.docs-toc').getByRole('link', { name: '连接模型服务', exact: true }).click();
    await expect(page.locator('#连接模型服务')).toBeInViewport();
    await page.reload();
    await expect(page.locator('#连接模型服务')).toBeInViewport();
    await page.goto(`${url}/zh/docs/admin/getting-started/`);
    await page.getByRole('button', { name: '搜索文档', exact: true }).click();
    await page.getByRole('searchbox', { name: '搜索文档' }).fill('Bubblewrap');
    assert(await page.getByRole('navigation', { name: '搜索结果' }).getByRole('link').count() > 0);
    await page.getByRole('searchbox', { name: '搜索文档' }).fill('no-such-doc-123');
    await expect(page.getByRole('navigation', { name: '搜索结果' }).getByRole('status')).toContainText('没有找到');
    await page.getByRole('searchbox', { name: '搜索文档' }).fill('');
    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: 'Switch to English', exact: true }).click();
    await expect(page).toHaveURL(`${url}/en/docs/admin/getting-started/`);
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
    await expect(page.getByRole('button', { name: 'Search documentation', exact: true })).toBeVisible();
    await expect(page.locator('.docs-language-note')).toHaveCount(0);
    await page.getByRole('button', { name: 'Switch to dark mode', exact: true }).click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    await page.goto(`${url}/zh/docs/user/`);
    for (const width of [1440, 768, 390, 320]) {
      await page.setViewportSize({ width, height: 900 });
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      await page.screenshot({ path: join(screenshots, `docs-deployment-dark-${width}.png`), fullPage: true });
    }
    await expect(page.getByRole('navigation', { name: '文档目录' })).toBeHidden();
    await page.getByRole('button', { name: '展开目录 +' }).click();
    await page.getByRole('navigation', { name: '文档目录' }).getByRole('link', { name: '个人设置' }).click();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('个人设置');
    await expect(page.getByRole('button', { name: '展开目录 +' })).toHaveAttribute('aria-expanded', 'false');
    await expect(page.getByRole('navigation', { name: '文档目录' })).toBeHidden();
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    await page.screenshot({ path: join(screenshots, 'docs-settings-mobile.png'), fullPage: true });
    await page.goto(`${url}/zh/docs/missing/`);
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('未找到文档');
    await page.getByRole('link', { name: '返回文档概览 →' }).click();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Molr 文档');
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.getByRole('button', { name: '切换浅色模式', exact: true }).click();
    await page.screenshot({ path: join(screenshots, 'docs-overview-desktop.png'), fullPage: true });
    assert.deepEqual(errors, []);
  }, signal);
}

async function checkClientNavigation(page, url) {
  await page.goto(url);
  await page.getByRole('heading', { level: 1 }).waitFor();
  await page.evaluate(() => { window.docsNavigationMarker = 'same-document'; });
  let documentRequests = 0;
  const countDocumentRequests = request => {
    if (request.isNavigationRequest() && request.frame() === page.mainFrame()) documentRequests++;
  };
  page.on('request', countDocumentRequests);
  try {
    await page.getByRole('button', { name: '文档', exact: true }).click();
    await page.getByRole('menuitemradio', { name: '用户文档', exact: true }).click();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Molr 使用指南');
    await page.evaluate(() => { document.querySelector('.docs-sidebar').navigationMarker = true; });
    await page.getByRole('navigation', { name: '文档目录' }).getByRole('link', { name: '快速开始', exact: true }).click();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('快速开始');
    assert(await page.locator('.docs-sidebar').evaluate(element => element.navigationMarker));
    await expect(page.locator('#main')).toBeFocused();
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(0);

    await page.getByRole('button', { name: '搜索文档', exact: true }).click();
    const search = page.getByRole('searchbox', { name: '搜索文档' });
    await search.fill('Bubblewrap');
    await expect(page.getByRole('navigation', { name: '搜索结果' }).getByRole('status')).toContainText('没有找到');
    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: '文档', exact: true }).click();
    await page.getByRole('menuitemradio', { name: '管理员文档', exact: true }).click();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Molr 管理员指南');
    await page.getByRole('button', { name: '搜索文档', exact: true }).click();
    await search.fill('Bubblewrap');
    await page.getByRole('navigation', { name: '搜索结果' }).getByRole('link', { name: '安装与服务管理', exact: true }).click();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('安装与服务管理');
    await expect(page.getByRole('dialog')).not.toBeVisible();
    await page.getByRole('button', { name: '搜索文档', exact: true }).click();
    await expect(search).toHaveValue('Bubblewrap');
    await expect(page).toHaveTitle('安装与服务管理 · Molr Docs');
    await expect(page.locator('link[rel="canonical"]')).toHaveAttribute('href', 'https://molr.ai/zh/docs/admin/deployment/installation/');
    await search.fill('');
    await page.keyboard.press('Escape');

    // Record reading positions only after the installation architecture diagram settles.
    await expect(page.locator('.docs-mermaid svg')).toHaveCount(1);
    const sandboxLink = page.getByRole('link', { name: '沙盒检查', exact: true });
    await sandboxLink.scrollIntoViewIfNeeded();
    const originalScroll = await page.evaluate(() => window.scrollY);
    assert(originalScroll > 300);
    await sandboxLink.click();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Linux 沙盒部署');
    await expect(page.locator('#检查与验证')).toBeInViewport();
    const sandboxScroll = await page.evaluate(() => window.scrollY);
    await page.goBack();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('安装与服务管理');
    await expect(page.locator('.docs-mermaid svg')).toHaveCount(1);
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBeCloseTo(originalScroll, 0);
    await page.goForward();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Linux 沙盒部署');
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBeCloseTo(sandboxScroll, 0);

    await page.locator('.docs-toc').getByRole('link', { name: '安装和启动', exact: true }).click();
    await expect(page.locator('#安装和启动')).toBeInViewport();
    await page.goBack();
    await expect(page.locator('#检查与验证')).toBeInViewport();
    await page.getByRole('link', { name: '返回官网', exact: true }).click();
    await expect(page.locator('#home-title')).toHaveText('从好奇出发。');
    await expect(page).toHaveTitle(/从好奇出发/);
    await expect(page.locator('link[rel="canonical"]')).toHaveAttribute('href', 'https://molr.ai/');
    await expect(page.locator('meta[name="description"]')).toHaveAttribute('content', /个人 AI 助理/);
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(0);
    // Footer destinations use the same navigation behavior as the header.
    await page.getByRole('contentinfo').getByRole('link', { name: '用户文档', exact: true }).click();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Molr 使用指南');
    await page.getByRole('navigation', { name: '前后篇文档' }).getByRole('link').click();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('快速开始');
    assert.equal(await page.evaluate(() => window.docsNavigationMarker), 'same-document');
    assert.equal(documentRequests, 0, 'Internal navigation must not request another HTML document');

    // Modified clicks retain native new-tab behavior.
    const [popup] = await Promise.all([
      // Native modifier-click tabs may have no opener and no page-level popup event.
      page.context().waitForEvent('page', { timeout: documentReadyTimeout }),
      page.getByRole('navigation', { name: '文档目录' }).getByRole('link', { name: '用户文档概览', exact: true }).click({ modifiers: ['ControlOrMeta'], timeout: documentReadyTimeout }),
    ]);
    // Reading happens after switching to the native tab. A load event alone
    // does not wait for React's lazy document bundle to render.
    await popup.bringToFront();
    await expect(popup).toHaveURL(`${url}/zh/docs/user/`, { timeout: documentReadyTimeout });
    await expect(popup.getByRole('heading', { level: 1 })).toHaveText('Molr 使用指南', { timeout: documentReadyTimeout });
    await popup.close();
    await page.bringToFront();
    await expect(page).toHaveURL(`${url}/zh/docs/user/getting-started/`);
  } finally { page.off('request', countDocumentRequests); }
}

async function checkLanguageNavigation(page, url) {
  await page.goto(`${url}/zh/docs/admin/getting-started/?source=language-test`);
  const headings = page.locator('.docs-toc a');
  await headings.nth(1).click();
  const chineseURL = page.url();
  await page.evaluate(() => { window.languageNavigationMarker = true; });
  await page.getByRole('button', { name: 'Switch to English', exact: true }).click();
  await expect(page).toHaveURL(/\/en\/docs\/admin\/getting-started\/\?source=language-test#/);
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  assert(await page.evaluate(() => window.languageNavigationMarker));
  const englishHash = await headings.nth(1).getAttribute('href');
  assert.equal(decodeURIComponent(new URL(page.url()).hash), englishHash);
  await expect(page.locator(englishHash)).toBeInViewport();
  await expect(page.locator('link[rel="canonical"]')).toHaveAttribute('href', 'https://molr.ai/en/docs/admin/getting-started/');
  await expect(page.locator('link[hreflang="zh-CN"]')).toHaveAttribute('href', 'https://molr.ai/zh/docs/admin/getting-started/');
  const englishTitle = await page.title();
  await page.reload();
  await expect(page).toHaveTitle(englishTitle);
  await page.goBack();
  await expect(page).toHaveURL(chineseURL);
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN');
  await page.goForward();
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await page.getByRole('button', { name: 'Search documentation', exact: true }).click();
  await page.getByRole('searchbox', { name: 'Search documentation' }).fill('Bubblewrap');
  const results = page.getByRole('navigation', { name: 'Search results' }).getByRole('link');
  assert(await results.count() > 0);
  assert(await results.evaluateAll(links => links.every(link => link.pathname.startsWith('/en/docs/admin/'))));
  await results.first().click();
  await expect(page.getByRole('dialog')).not.toBeVisible();
  await page.getByRole('button', { name: 'Search documentation', exact: true }).click();
  await expect(page.getByRole('searchbox', { name: 'Search documentation' })).toHaveValue('Bubblewrap');
  await page.getByRole('searchbox', { name: 'Search documentation' }).fill('');
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: '切换到中文', exact: true }).click();
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN');

  // Explicit links override a conflicting browser/storage preference, even
  // when storage is disabled. Both locales remain usable on narrow screens.
  const blockedContext = await page.context().browser().newContext({ locale: 'zh-CN' });
  try {
    const isolatedPage = await blockedContext.newPage();
    await blockedContext.addInitScript(() => {
      Storage.prototype.getItem = () => { throw new Error('Storage blocked'); };
      Storage.prototype.setItem = () => { throw new Error('Storage blocked'); };
    });
    await isolatedPage.goto(`${url}/en/docs/dev/architecture/overview/`);
    await expect(isolatedPage.locator('html')).toHaveAttribute('lang', 'en');
    await isolatedPage.setViewportSize({ width: 320, height: 900 });
    await isolatedPage.getByRole('button', { name: 'Open menu +' }).click();
    await isolatedPage.getByRole('navigation', { name: 'Documentation navigation' }).getByRole('link', { name: 'Frontend architecture', exact: true }).click();
    await expect(isolatedPage).toHaveURL(`${url}/en/docs/dev/frontend/architecture/`);
    await expect(isolatedPage.getByRole('button', { name: 'Open menu +' })).toHaveAttribute('aria-expanded', 'false');
    assert(await isolatedPage.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    await isolatedPage.getByRole('button', { name: '切换到中文', exact: true }).click();
    await expect(isolatedPage).toHaveURL(`${url}/zh/docs/dev/frontend/architecture/`);
    await isolatedPage.setViewportSize({ width: 1440, height: 1000 });
  } finally { await blockedContext.close(); }

}
