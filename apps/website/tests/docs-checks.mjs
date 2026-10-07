import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { expect } from '@playwright/test';

export async function checkDocs({ browser, url, outDir, screenshots }) {
  const pages = [
    ['/docs/', 'Molr 产品文档'],
    ['/docs/getting-started/', '快速开始'],
    ['/docs/architecture/', '架构总览'],
    ['/docs/assistants-and-plans/', '助理、聊天与计划'],
    ['/docs/resources-and-permissions/', '资源、权限与预算'],
    ['/docs/settings-and-troubleshooting/', '设置、安全与常见问题'],
    ['/docs/deployment/', '部署与维护'],
    ['/docs/deployment/sandbox/', '自动命令执行环境'],
    ['/docs/deployment/remote-sandbox/', '独立 Linux Sandbox 服务'],
    ['/dev-docs/', 'Molr 开发文档'],
    ['/dev-docs/product-scope/', 'Molr 产品范围'],
    ['/dev-docs/runtime/architecture/', 'Molr Runtime 架构设计'],
    ['/dev-docs/runtime/security/', 'Runtime 公网部署与安全边界'],
  ];
  const context = await browser.newContext({ locale: 'zh-CN', colorScheme: 'light', viewport: { width: 1440, height: 1000 } });
  try {
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.goto(url);
    await page.getByRole('navigation').getByRole('link', { name: '用户文档', exact: true }).click();
    await expect(page).toHaveURL(`${url}/docs/`);
    for (const [path, title] of pages) {
      const html = await readFile(join(outDir, path, 'index.html'), 'utf8');
      assert(html.includes(`<title>${title} · Molr Docs</title>`));
      assert(html.includes(`href="https://molr.ai${path}"`));
      await page.goto(`${url}${path}`);
      await expect(page.getByRole('heading', { level: 1, name: title, exact: true })).toBeVisible();
      await expect(page).toHaveTitle(`${title} · Molr Docs`);
      await expect(page.getByRole('navigation', { name: '文档目录' }).locator('[aria-current="page"]')).toHaveCount(1);
      for (const href of await page.locator('.docs-toc a').evaluateAll(links => links.map(link => link.getAttribute('href')))) {
        assert(await page.evaluate(hash => document.getElementById(decodeURIComponent(hash.slice(1))) !== null, href));
      }
      assert(await page.locator('.docs-prose a').evaluateAll(links => links.every(link => !new URL(link.href).pathname.endsWith('.md'))));
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    }
    for (const removed of ['/dev-docs/product/prd/', '/dev-docs/research/']) {
      await page.goto(`${url}${removed}`);
      await expect(page.getByRole('heading', { level: 1 })).toHaveText('未找到文档');
    }
    await page.goto(`${url}/dev-docs/`);
    await page.getByRole('searchbox', { name: '搜索文档' }).fill('候选试点');
    await expect(page.getByRole('status')).toContainText('没有找到');
    await page.getByRole('searchbox', { name: '搜索文档' }).fill('');
    await checkClientNavigation(page, url);
    await page.goto(`${url}/docs/getting-started/`);
    await page.getByRole('link', { name: '部署说明', exact: true }).last().click();
    await expect(page).toHaveURL(/\/docs\/deployment\/#/);
    await expect(page.locator('#模型网络与价格配置')).toBeInViewport();
    await page.reload();
    await expect(page.locator('#模型网络与价格配置')).toBeInViewport();
    await page.goBack();
    await expect(page.getByRole('heading', { level: 1, name: '快速开始', exact: true })).toBeVisible();
    await page.getByRole('searchbox', { name: '搜索文档' }).fill('Bubblewrap');
    await expect(page.getByRole('navigation', { name: '搜索结果' }).getByRole('link')).toHaveCount(3);
    await page.getByRole('searchbox', { name: '搜索文档' }).fill('no-such-doc-123');
    await expect(page.getByRole('status')).toContainText('没有找到');
    await page.getByRole('searchbox', { name: '搜索文档' }).fill('');
    await page.getByRole('button', { name: 'Switch to English', exact: true }).click();
    await expect(page).toHaveTitle('快速开始 · Molr Docs');
    await expect(page.locator('.docs-language-note')).toContainText('available in Chinese');
    await page.getByRole('button', { name: 'Switch to dark mode', exact: true }).click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    await page.goto(`${url}/docs/deployment/`);
    for (const width of [1440, 768, 390, 320]) {
      await page.setViewportSize({ width, height: 900 });
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      await page.screenshot({ path: join(screenshots, `docs-deployment-dark-${width}.png`), fullPage: true });
    }
    await expect(page.getByRole('navigation', { name: '文档目录' })).toBeHidden();
    await page.getByRole('button', { name: '展开目录 +' }).click();
    await page.getByRole('navigation', { name: '文档目录' }).getByRole('link', { name: '设置、安全与常见问题' }).click();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('设置、安全与常见问题');
    await expect(page.getByRole('button', { name: '展开目录 +' })).toHaveAttribute('aria-expanded', 'false');
    await expect(page.getByRole('navigation', { name: '文档目录' })).toBeHidden();
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    await page.screenshot({ path: join(screenshots, 'docs-settings-mobile.png'), fullPage: true });
    await page.goto(`${url}/docs/missing/`);
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('未找到文档');
    await page.getByRole('link', { name: '返回文档概览 →' }).click();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Molr 产品文档');
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.getByRole('button', { name: 'Switch to light mode', exact: true }).click();
    await page.screenshot({ path: join(screenshots, 'docs-overview-desktop.png'), fullPage: true });
    assert.deepEqual(errors, []);
  } finally { await context.close(); }
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
    await page.getByRole('navigation').getByRole('link', { name: '用户文档', exact: true }).click();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Molr 产品文档');
    await page.evaluate(() => { document.querySelector('.docs-sidebar').navigationMarker = true; });
    await page.getByRole('navigation', { name: '文档目录' }).getByRole('link', { name: '快速开始', exact: true }).click();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('快速开始');
    assert(await page.locator('.docs-sidebar').evaluate(element => element.navigationMarker));
    await expect(page.locator('#main')).toBeFocused();
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(0);

    const search = page.getByRole('searchbox', { name: '搜索文档' });
    await search.fill('Bubblewrap');
    await page.getByRole('navigation', { name: '搜索结果' }).getByRole('link', { name: '部署与维护', exact: true }).click();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('部署与维护');
    await expect(search).toHaveValue('Bubblewrap');
    await expect(page).toHaveTitle('部署与维护 · Molr Docs');
    await expect(page.locator('link[rel="canonical"]')).toHaveAttribute('href', 'https://molr.ai/docs/deployment/');
    await search.fill('');

    const sandboxLink = page.getByRole('link', { name: '沙盒检查', exact: true });
    await sandboxLink.scrollIntoViewIfNeeded();
    const originalScroll = await page.evaluate(() => window.scrollY);
    assert(originalScroll > 300);
    await sandboxLink.click();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('自动命令执行环境');
    await expect(page.locator('#检查与验证')).toBeInViewport();
    const sandboxScroll = await page.evaluate(() => window.scrollY);
    await page.goBack();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('部署与维护');
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBeCloseTo(originalScroll, 0);
    await page.goForward();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('自动命令执行环境');
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBeCloseTo(sandboxScroll, 0);

    await page.locator('.docs-toc').getByRole('link', { name: '用户如何使用', exact: true }).click();
    await expect(page.locator('#用户如何使用')).toBeInViewport();
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
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Molr 产品文档');
    await page.getByRole('navigation', { name: '前后篇文档' }).getByRole('link').click();
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('快速开始');
    assert.equal(await page.evaluate(() => window.docsNavigationMarker), 'same-document');
    assert.equal(documentRequests, 0, 'Internal navigation must not request another HTML document');

    // Modified clicks retain native new-tab behavior.
    const popupPromise = page.context().waitForEvent('page');
    await page.getByRole('navigation', { name: '文档目录' }).getByRole('link', { name: '文档概览', exact: true }).click({ modifiers: ['ControlOrMeta'] });
    const popup = await popupPromise;
    await popup.waitForLoadState();
    await expect(popup.getByRole('heading', { level: 1 })).toHaveText('Molr 产品文档');
    await popup.close();
    await expect(page).toHaveURL(`${url}/docs/getting-started/`);
  } finally { page.off('request', countDocumentRequests); }
}
