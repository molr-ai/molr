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

for (const scoped of [true, false]) {
  test(`metadata changed during an initial page read is reconciled (${scoped ? 'scoped' : 'unknown conversation'})`, async ({ page }) => {
    let release!: () => void;
    const pending = new Promise<void>(resolve => { release = resolve; });
    let initial = false, metadataReads = 0;
    const message = { ID: 'message', ConversationID: 'fixture', Sequence: 1, Feedback: 'none' };
    await page.route('**/api/events/stream*', route => route.fulfill({ contentType: 'text/event-stream', body: 'event: ready\ndata: {}\n\n' }));
    await page.route('**/api/conversations/fixture/messages?*', async route => {
      const query = new URL(route.request().url()).searchParams;
      if (query.has('message_id')) {
        metadataReads++;
        await route.fulfill({ json: { Items: [{ ...message, Feedback: 'like' }], NextCursor: '' } });
      } else if (!initial) {
        initial = true;
        await pending;
        await route.fulfill({ json: { Items: [message], NextCursor: '' } });
      } else await route.fulfill({ json: { Items: [], NextCursor: '' } });
    });
    await page.goto('/tests/fixtures/interactive-updates.html');
    await expect.poll(() => initial).toBe(true);
    await page.evaluate(scoped => window.invalidateHistoryMessage('message', scoped ? 'fixture' : undefined), scoped);
    release();
    await expect(page.getByLabel('history')).toContainText('"Feedback":"like"');
    expect(metadataReads).toBe(1);
  });
}

test('StrictMode reentry ignores a released reader even when its transport delivers a late response', async ({ page }) => {
  await page.addInitScript(() => {
    const fetch = window.fetch.bind(window);
    // Exercise the hook's lifecycle guard even if a transport does not cancel.
    window.fetch = (input, init) => fetch(input, typeof input === 'string' && input.includes('/messages?') ? { ...init, signal: undefined } : init);
  });
  let release!: () => void;
  const pending = new Promise<void>(resolve => { release = resolve; });
  let initialReads = 0, revisiting = false;
  const message = { ID: 'message', ConversationID: 'fixture', Sequence: 1, Feedback: 'none' };
  await page.route('**/api/events/stream*', route => route.fulfill({ contentType: 'text/event-stream', body: 'event: ready\ndata: {}\n\n' }));
  await page.route('**/api/conversations/*/messages?*', async route => {
    const url = new URL(route.request().url());
    if (!url.pathname.includes('/fixture/')) {
      await route.fulfill({ json: { Items: [], NextCursor: '' } });
    } else if (!revisiting) {
      initialReads++;
      await pending;
      await route.fulfill({ json: { Items: [message], NextCursor: '' } });
    } else await route.fulfill({ json: { Items: url.searchParams.has('after_sequence') ? [] : [{ ...message, Feedback: 'like' }], NextCursor: '' } });
  });
  await page.goto('/tests/fixtures/interactive-updates.html?strict');
  await expect.poll(() => initialReads).toBeGreaterThan(0);
  await page.evaluate(() => window.selectHistory('other'));
  await expect(page.getByLabel('history')).toContainText('"conversationID":"other","loaded":true');
  revisiting = true;
  await page.evaluate(() => window.selectHistory('fixture'));
  await expect(page.getByLabel('history')).toContainText('"Feedback":"like"');
  const stale = page.waitForResponse(async response => response.url().includes('/fixture/messages?') && (await response.json()).Items?.[0]?.Feedback === 'none');
  release();
  await (await stale).finished();
  await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
  expect(await page.evaluate(() => window.cachedHistoryFeedback())).toBe('like');
  await expect(page.getByLabel('history')).toContainText('"Feedback":"like"');
});
