import assert from 'node:assert/strict';
import { expect } from '@playwright/test';

export async function checkDocsSearch({ browser, url }) {
  for (const width of [1440, 390]) {
    const context = await browser.newContext({ locale: 'zh-CN', colorScheme: 'light', reducedMotion: 'reduce', viewport: { width, height: 900 } });
    try {
      const page = await context.newPage();
      const errors = [];
      page.on('pageerror', error => errors.push(error.message));
      await page.goto(`${url}/docs/concepts/`);
      await expect(page.locator('.docs-mermaid svg')).toHaveCount(3);
      await page.evaluate(() => {
        window.readingNodes = [...document.querySelectorAll('.docs-prose p, .docs-prose pre, .docs-mermaid svg')];
      });
      if (width < 768) await page.getByRole('button', { name: '展开目录 +' }).click();
      const search = page.getByRole('searchbox', { name: '搜索文档', includeHidden: true });
      await search.focus();
      for (const letter of 'RUNTIME') {
        await search.pressSequentially(letter);
        await expect(page.getByRole('navigation', { name: '搜索结果' }).getByRole('link', { name: '基本概念', exact: true })).toBeVisible();
        assert(await page.evaluate(() => window.readingNodes.every(node => node.isConnected)), 'Typing must preserve the current article and rendered diagrams');
      }
      await search.fill('no-such-doc-123');
      await expect(page.getByRole('status')).toContainText('没有找到');
      await search.fill(' runtime ');
      await expect(page.getByRole('navigation', { name: '搜索结果' }).getByRole('link', { name: '基本概念', exact: true })).toBeVisible();
      await search.fill('');
      assert(await page.evaluate(() => window.readingNodes.every(node => node.isConnected)), 'Clearing search must preserve the article');
      if (width < 768) await page.getByRole('button', { name: '收起目录 −' }).click();
      // Theme context must still reach diagrams inside the memoized article.
      const oldIds = await page.locator('.docs-mermaid svg').evaluateAll(nodes => nodes.map(node => node.id));
      await page.getByRole('button', { name: '切换暗色模式', exact: true }).click();
      await expect.poll(() => page.locator('.docs-mermaid svg').evaluateAll(nodes => nodes.map(node => node.id))).not.toEqual(oldIds);
      await expect(page.locator('.docs-mermaid svg')).toHaveCount(3);
      await expect(page.locator('.docs-mermaid-fallback')).toHaveCount(0);
      if (width < 768) await page.getByRole('button', { name: '展开目录 +' }).click();
      await search.fill('Bubblewrap');
      await page.getByRole('navigation', { name: '搜索结果' }).getByRole('link', { name: 'Linux 沙盒部署', exact: true }).click();
      await expect(page.getByRole('heading', { level: 1 })).toHaveText('Linux 沙盒部署');
      await expect(search).toHaveValue('Bubblewrap');
      await expect(page.locator('.docs-prose')).toContainText('CGO_ENABLED=0');
      assert.deepEqual(errors, []);
    } finally { await context.close(); }
  }
}
