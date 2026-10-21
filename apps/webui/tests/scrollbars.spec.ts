import { readLayout } from '../../../scripts/test-support/layout.ts';
import { test, expect } from '@playwright/test';

test('global scrollbars overlay both axes without moving content and remain draggable in a modal', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('html')).toHaveAttribute('data-overlay-scrollbars', '');
  await page.evaluate(() => {
    const dialog = document.createElement('dialog');
    dialog.id = 'scrollbar-fixture';
    dialog.style.cssText = 'width:280px;height:240px;padding:0;border:0;overflow:visible;background:var(--color-surface)';
    const scroller = document.createElement('div');
    scroller.id = 'scrollbar-scroll';
    scroller.tabIndex = 0;
    scroller.style.cssText = 'width:280px;height:240px;overflow:auto';
    const content = document.createElement('div');
    content.style.cssText = 'width:800px;height:1200px';
    content.textContent = 'Scrollable test content';
    scroller.append(content); dialog.append(scroller); document.body.append(dialog); dialog.showModal();
  });
  const scroller = page.locator('#scrollbar-scroll');
  const widths = () => scroller.evaluate(e => ({ client: e.clientWidth, offset: (e as HTMLElement).offsetWidth, height: e.clientHeight }));
  expect(await widths()).toEqual({ client: 280, offset: 280, height: 240 });
  await scroller.focus();
  if (test.info().project.use.isMobile) {
    const box = (await scroller.boundingBox())!, touch = await page.context().newCDPSession(page);
    const x = box.x + 80, y = box.y + 180;
    await touch.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] });
    for (let step = 1; step <= 5; step++) await touch.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x, y: y - step * 24 }] });
    await touch.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
    await touch.detach();
  } else await page.keyboard.press('PageDown');
  await expect.poll(() => scroller.evaluate(e => e.scrollTop)).toBeGreaterThan(0);
  const vertical = page.locator('#scrollbar-fixture .webui-scrollbar-thumb[data-axis="y"]');
  const horizontal = page.locator('#scrollbar-fixture .webui-scrollbar-thumb[data-axis="x"]');
  await expect(vertical).toBeVisible();
  await expect(horizontal).toBeVisible();
  if (!test.info().project.use.isMobile) {
    const previous = await scroller.evaluate(e => e.scrollTop), thumb = (await vertical.boundingBox())!;
    await page.mouse.move(thumb.x + thumb.width / 2, thumb.y + thumb.height / 2);
    await page.mouse.down(); await page.mouse.move(thumb.x + thumb.width / 2, thumb.y + thumb.height / 2 + 30); await page.mouse.up();
    await expect.poll(() => scroller.evaluate(e => e.scrollTop)).toBeGreaterThan(previous);
    const bar = (await horizontal.boundingBox())!;
    await page.mouse.move(bar.x + bar.width / 2, bar.y + bar.height / 2);
    await page.mouse.down(); await page.mouse.move(bar.x + bar.width / 2 + 30, bar.y + bar.height / 2); await page.mouse.up();
    await expect.poll(() => scroller.evaluate(e => e.scrollLeft)).toBeGreaterThan(0);
    await page.mouse.move(0, 0);
  }
  const layer = page.locator('#scrollbar-fixture .webui-scrollbars');
  await expect.poll(() => layer.evaluate(e => { const opacity = Number(getComputedStyle(e).opacity); return opacity > 0 && opacity < 1; }), { intervals: [30] }).toBe(true);
  await scroller.evaluate(e => { e.scrollTop += 20; });
  await expect(layer).toHaveCSS('opacity', '1');
  await expect(page.locator('.webui-scrollbars')).toHaveCount(0);
  expect(await widths()).toEqual({ client: 280, offset: 280, height: 240 });
  await scroller.evaluate(e => { e.scrollTop += 20; });
  await expect(vertical).toBeVisible();
  await page.evaluate(() => document.getElementById('scrollbar-fixture')!.remove());
  await expect(page.locator('.webui-scrollbars')).toHaveCount(0);
});

test('popup scrollbars follow clipping and clean up when the popup closes', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('html')).toHaveAttribute('data-overlay-scrollbars', '');
  await page.evaluate(() => {
    const popup = document.createElement('div');
    popup.id = 'scrollbar-popup'; popup.popover = 'auto';
    popup.style.cssText = 'width:250px;height:180px;padding:0;border:0;overflow:hidden;background:var(--color-surface)';
    const scroll = document.createElement('div');
    scroll.id = 'popup-scroll'; scroll.style.cssText = 'width:250px;height:280px;overflow:auto';
    const content = document.createElement('div'); content.style.height = '1000px'; content.textContent = 'Popup content';
    scroll.append(content); popup.append(scroll); document.body.append(popup); popup.showPopover(); scroll.scrollTop = 300;
  });
  const popup = page.locator('#scrollbar-popup'), thumb = popup.locator('.webui-scrollbar-thumb[data-axis="y"]');
  await expect(thumb).toBeVisible();
  const [bounds, bar] = await readLayout(popup, thumb);
  expect(bar.y + bar.height).toBeLessThanOrEqual(bounds.y + bounds.height);
  await page.evaluate(() => document.getElementById('scrollbar-popup')!.hidePopover());
  await expect(page.locator('.webui-scrollbars')).toHaveCount(0);
});
