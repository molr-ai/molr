import assert from 'node:assert/strict';
import { expect } from '@playwright/test';

export async function checkDocsFeatures({ browser, url }) {
  for (const locale of ['zh', 'en']) {
    const context = await browser.newContext({ viewport: { width: 390, height: 900 }, reducedMotion: 'reduce' });
    try {
      const page = await context.newPage();
      const searchLabel = locale === 'zh' ? '搜索文档' : 'Search documentation';
      const title = locale === 'zh' ? '操作 GitHub' : 'Use GitHub';
      await page.goto(`${url}/${locale}/docs/user/best-practices/github/`);
      const prompts = page.locator('.docs-prompt');
      await expect(prompts).toHaveCount(4);
      await expect(page.locator('.docs-prose')).not.toContainText('[!PROMPT]');
      const text = (await prompts.first().locator('.docs-prompt-content').innerText()).trim();
      await page.evaluate(() => Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async text => { window.copiedPrompt = text; } } }));
      await prompts.first().getByRole('button').click();
      await expect(prompts.first().getByRole('status')).toHaveText(locale === 'zh' ? '已复制提示词' : 'Prompt copied');
      assert.equal(await page.evaluate(() => window.copiedPrompt), text);
      // Exercise the HTTP fallback and copy only the chosen example.
      await page.evaluate(() => {
        Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined });
        document.execCommand = command => { window.copiedPrompt = document.activeElement.value; return command === 'copy'; };
      });
      await prompts.nth(1).getByRole('button').click();
      assert.equal(await page.evaluate(() => window.copiedPrompt), (await prompts.nth(1).locator('.docs-prompt-content').innerText()).trim());
      await expect(prompts.nth(1).getByRole('button')).toBeFocused();
      await page.evaluate(() => Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async () => { throw new Error('Denied'); } } }));
      await prompts.nth(2).getByRole('button').click();
      await expect(prompts.nth(2).getByRole('status')).toContainText(locale === 'zh' ? '复制失败' : 'Copy failed');

      await page.getByRole('button', { name: searchLabel, exact: true }).click();
      const dialog = page.getByRole('dialog');
      const input = dialog.getByRole('searchbox');
      await expect(input).toBeFocused();
      await input.fill(title);
      const first = dialog.locator('.docs-search-result').first();
      await expect(first.locator('.docs-search-match-types')).toContainText(locale === 'zh' ? '标题匹配' : 'Title match');
      await expect(first.locator('mark').first()).toHaveText(title);
      await input.press('Escape');
      await expect(dialog).not.toBeVisible();
      await expect(page.getByRole('button', { name: searchLabel, exact: true })).toBeFocused();
      await page.keyboard.press('ControlOrMeta+k');
      await expect(input).toBeFocused();
      await expect(input).toHaveValue(title);
      await input.fill('GH_CONFIG_DIR');
      await expect(first.locator('.docs-search-match-types')).toContainText(locale === 'zh' ? '正文匹配' : 'Body match');
      await expect(first.locator('.docs-search-snippet')).toContainText('GH_CONFIG_DIR');
      const href = await first.getAttribute('href');
      assert(href.startsWith(`/${locale}/docs/user/`) && href.includes('#'));
      await input.press('ArrowDown');
      await expect(first).toBeFocused();
      await first.press('Enter');
      await expect(dialog).not.toBeVisible();
      await expect(page).toHaveURL(`${url}${href}`);
      const id = decodeURIComponent(href.split('#')[1]);
      await expect(page.locator(`[id="${id}"]`)).toBeInViewport();
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    } finally { await context.close(); }
  }
}
