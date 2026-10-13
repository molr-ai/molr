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
    const nav = document.querySelector<HTMLElement>('.workspace--mobile-nav .workspace-nav');
    const main = document.querySelector<HTMLElement>('.workspace--mobile .workspace-main');
    if (main) {
      const content = main.getBoundingClientRect(), style = getComputedStyle(main);
      if (Math.abs(content.left - 8) > 1 || Math.abs(innerWidth - content.right - 8) > 1) failures.push('mobile content card must keep 8px side margins');
      if (!nav && Math.abs(top + height - content.bottom - 8) > 1) failures.push('mobile content must reclaim navigation space when navigation is hidden');
      if (!document.querySelector('.connection-banner') && Math.abs(content.top - top - 8) > 1) failures.push('mobile content card must keep an 8px top margin without a connection notice');
      if (style.borderRadius !== '16px' || style.boxShadow === 'none') failures.push('mobile content must retain its rounded card and shadow');
    }
    if (nav && main) {
      const menu = nav.getBoundingClientRect(), content = main.getBoundingClientRect();
      if (Math.abs(menu.top - content.bottom - 8) > 1) failures.push('navigation must be a separate card 8px below content, without overlap');
      if (Math.abs(menu.left - content.left) > 1 || Math.abs(menu.right - content.right) > 1) failures.push('navigation and content card edges must align');
      if (Math.abs(top + height - menu.bottom - 8) > 1) failures.push('navigation must keep an 8px bottom gap in viewports without a safe area');
      const menuStyle = getComputedStyle(nav), contentStyle = getComputedStyle(main);
      for (const property of ['backgroundColor', 'borderRadius', 'boxShadow'] as const) if (menuStyle[property] !== contentStyle[property]) failures.push(`navigation and content must share ${property}`);
      if (menuStyle.position === 'absolute' || menuStyle.position === 'fixed') failures.push('navigation must participate in normal layout');
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
      if (bottom) await expect.poll(() => page.evaluate(() => {
        const nav = document.querySelector<HTMLElement>('.workspace--mobile-nav .workspace-nav');
        if (!nav) return [];
        // Queries can finish after the initial scroll; measure the end of the current content.
        for (const el of document.querySelectorAll<HTMLElement>('.workspace-main *')) {
          if (el.checkVisibility() && /auto|scroll/.test(getComputedStyle(el).overflowY)) el.scrollTop = el.scrollHeight;
        }
        const bottom = document.querySelector('.workspace-main')!.getBoundingClientRect().bottom;
        const selectors = ['.assistant-list > li:last-child', '.settings-body > :last-child', '.resources-master > :last-child', '.plans-master > :last-child', '.plans-month-grid', '.trajectory-scroll > :last-child', '.plugin-market > :last-child', '.page-body > :last-child'];
        return selectors.flatMap(selector => Array.from(document.querySelectorAll<HTMLElement>(`.workspace-main ${selector}`)).filter(el => el.checkVisibility() && el.getBoundingClientRect().bottom > bottom + 1).map(() => `${selector} is obscured at the end of scrolling`));
      }), { message: 'Scrolling to the end must reveal the last content inside its card' }).toEqual([]);
    }
  }
  await page.setViewportSize(original);
}
