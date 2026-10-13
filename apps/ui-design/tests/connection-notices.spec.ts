import { test, expect } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => { if (!localStorage.getItem('molr-ui-design-locale')) localStorage.setItem('molr-ui-design-locale', 'en'); });
  await page.goto('/?components=feedback#connection-notices');
});

test('connection notices support preview states, retry and recovery without a server', async ({ page }) => {
  const example = page.locator('#connection-notices'), notice = example.locator('.connection-banner');
  await expect(example.getByRole('heading', { name: 'Connection notices' })).toBeVisible();
  await expect(page.getByRole('tab', { name: 'Feedback', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(notice).toHaveAttribute('data-connection', 'reconnecting');
  await expect(notice.getByRole('button')).toHaveCount(0);
  await example.getByRole('radio', { name: 'Disconnected', exact: true }).check();
  await expect(notice).toHaveAttribute('data-connection', 'disconnected');
  const retry = notice.getByRole('button', { name: 'Retry connection' });
  await expect(retry).toHaveCSS('font-size', '12px');
  await expect(retry).toHaveCSS('color', await notice.evaluate(element => getComputedStyle(element).color));
  await retry.click();
  await expect(notice).toHaveAttribute('data-connection', 'reconnecting');
  await expect(notice).toHaveCount(0);
  await expect(example.getByText('Connected. The notice is hidden.')).toBeVisible();
  await example.getByRole('button', { name: 'Preview failed reconnection' }).click();
  await expect(notice).toHaveAttribute('data-connection', 'failed');
  await example.getByRole('button', { name: 'Preview successful reconnection' }).click();
  await example.getByRole('radio', { name: 'Disconnected', exact: true }).check();
  await page.waitForTimeout(2000);
  await expect(notice).toHaveAttribute('data-connection', 'disconnected');
  expect(await example.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
});

test('connection notices respect reduced motion and translate their labels', async ({ page }) => {
  const example = page.locator('#connection-notices'), icon = example.locator('.connection-banner__icon');
  await expect(icon).not.toHaveCSS('animation-name', 'none');
  await example.getByRole('switch', { name: 'Reduce motion' }).check();
  await expect(icon).toHaveCSS('animation-name', 'none');
  await example.getByRole('switch', { name: 'Reduce motion' }).uncheck();
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await expect(icon).toHaveCSS('animation-name', 'none');
  await page.evaluate(() => localStorage.setItem('molr-ui-design-locale', 'zh'));
  await page.reload();
  await expect(example.getByRole('heading', { name: '连接提示', exact: true })).toBeVisible();
  await example.getByRole('radio', { name: '重连失败', exact: true }).check();
  await expect(example.locator('.connection-banner')).toContainText('重连失败');
  await expect(example.locator('.connection-banner').getByRole('button', { name: '重试连接' })).toBeVisible();
});
