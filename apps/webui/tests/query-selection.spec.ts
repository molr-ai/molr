import { test, expect } from '@playwright/test';

test('data selectors ignore loading and unrelated changes, but clear on denied access', async ({ page }) => {
  let count = 2, detail = 'original', denied = false;
  await page.route('**/api/events/stream*', route => route.fulfill({ contentType: 'text/event-stream', body: 'event: ready\ndata: {}\n\n' }));
  await page.route('**/api/query-fixture*', route => denied
    ? route.fulfill({ status: 403, json: { code: 'forbidden' } })
    : route.fulfill({ json: { Count: count, Detail: detail } }));
  await page.goto('/tests/fixtures/query-selection.html');
  await expect(page.getByLabel('count')).toHaveText('2');
  await expect(page.getByLabel('status')).toHaveText('ready');
  const before = await page.evaluate(() => window.selectionRenders);
  const refresh = async () => {
    const response = page.waitForResponse(response => response.url().includes('/api/query-fixture'));
    await page.evaluate(() => window.refreshSelection());
    await (await response).finished();
    await expect(page.getByLabel('status')).toHaveText(denied ? 'error' : 'ready');
  };
  for (let i = 0; i < 3; i++) { detail = `changed-${i}`; await refresh(); }
  expect(await page.evaluate(() => window.selectionRenders)).toBe(before);
  count = 3;
  await refresh();
  await expect(page.getByLabel('count')).toHaveText('3');
  expect(await page.evaluate(() => window.selectionRenders)).toBeGreaterThan(before);
  denied = true;
  await refresh();
  await expect(page.getByLabel('count')).toHaveText('0');
});
