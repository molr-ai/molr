import { expect, type Page } from '@playwright/test';

/** Check the outer page, while allowing lists, forms and messages to scroll internally. */
export async function expectFitsWebView(page: Page) {
  await expect.poll(() => page.evaluate(() => {
    const height = window.visualViewport?.height ?? innerHeight;
    const top = window.visualViewport?.offsetTop ?? 0;
    const failures: string[] = [];
    const scrolling = document.scrollingElement!;
    if (scrolling.scrollHeight > height + 1) failures.push(`document scrollHeight=${scrolling.scrollHeight}, WebView height=${height}`);
    if (scrollY !== 0) failures.push(`document scrollY=${scrollY}`);
    const selectors = ['#root', '.workspace', '.workspace-main', '.workspace-main > section', '.assistant-workspace', '.login-page', '.session-page', 'dialog[open]'];
    for (const selector of selectors) for (const element of document.querySelectorAll<HTMLElement>(selector)) {
      if (!element.checkVisibility()) continue;
      const rect = element.getBoundingClientRect();
      if (rect.top < top - 1 || rect.bottom > top + height + 1) failures.push(`${selector}: top=${rect.top}, bottom=${rect.bottom}, WebView=${top}..${top + height}`);
    }
    return failures;
  }), { message: `${page.url()} must fit the WebView height` }).toEqual([]);
}

/** Exercise internal scrolling as well as the initial layout and a shorter WebView. */
export async function checkPageHeight(page: Page) {
  const original = page.viewportSize()!;
  for (const height of [original.height, 420]) {
    await page.setViewportSize({ ...original, height });
    await page.evaluate(async () => {
      await document.fonts.ready;
      await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    });
    await expectFitsWebView(page);
    for (const bottom of [true, false]) {
      await page.evaluate(bottom => {
        for (const element of document.querySelectorAll<HTMLElement>('#root *')) {
          if (element.checkVisibility() && /auto|scroll/.test(getComputedStyle(element).overflowY) && element.scrollHeight > element.clientHeight) {
            element.scrollTop = bottom ? element.scrollHeight : 0;
          }
        }
      }, bottom);
      await expectFitsWebView(page);
    }
  }
  await page.setViewportSize(original);
}
