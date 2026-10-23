import { test, expect } from '@playwright/test';

test('typing isolates draft subscribers and empty background refreshes do not render history', async ({ page }, info) => {
  let reads = 0;
  await page.route('**/api/events/stream*', route => route.fulfill({ contentType: 'text/event-stream', body: 'event: ready\ndata: {}\n\n' }));
  await page.route('**/api/conversations/fixture/messages?*', route => {
    reads++;
    return route.fulfill({ json: { Items: [], NextCursor: '' } });
  });
  await page.goto('/tests/fixtures/interactive-updates.html');
  await expect(page.getByLabel('history')).toContainText('"loading":false');
  const settled = () => page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
  await settled();
  const before = await page.evaluate(() => ({ ...window.renders }));
  await page.getByRole('textbox', { name: 'first', exact: true }).pressSequentially('draft stays local');
  const typed = await page.evaluate(() => ({ ...window.renders }));
  expect(typed.first).toBeGreaterThan(before.first);
  expect(typed.second).toBe(before.second);
  expect(typed.host).toBe(before.host);
  expect(typed.history).toBe(before.history);
  for (let i = 0; i < 5; i++) {
    const completed = page.waitForResponse(response => response.url().includes('/conversations/fixture/messages?'));
    await page.evaluate(() => window.refreshHistory());
    await (await completed).finished(); await settled();
  }
  const after = await page.evaluate(() => ({ ...window.renders }));
  expect(after.history).toBe(typed.history);
  await info.attach('render-counts', { body: JSON.stringify({ before, typed, after, reads }), contentType: 'application/json' });
});
