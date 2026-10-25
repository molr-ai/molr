import { test, expect } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  // Keep reconnects independent from the invalidation under test.
  await page.addInitScript(() => {
    window.EventSource = class extends EventTarget {
      onopen = null; onerror = null; readyState = 1; close() {}
    } as unknown as typeof EventSource;
  });
  await page.route('**/api/conversations*', route => route.fulfill({ json: { Items: [], NextCursor: '' } }));
});

for (const trigger of ['manual', 'topic', 'all'] as const) test(`data selectors (${trigger}) ignore loading and unrelated changes, but clear on denied access`, async ({ page }) => {
  let count = 2, detail = 'original', denied = false;
  await page.route('**/api/query-fixture*', route => denied
    ? route.fulfill({ status: 403, json: { code: 'forbidden' } })
    : route.fulfill({ json: { Count: count, Detail: detail } }));
  await page.goto('/tests/fixtures/query-selection.html');
  await expect(page.getByLabel('count')).toHaveText('2');
  await expect(page.getByLabel('status')).toHaveText('ready');
  const before = await page.evaluate(() => window.selectionRenders);
  const refresh = async () => {
    const response = page.waitForResponse(response => response.url().includes('/api/query-fixture'));
    await page.evaluate(trigger => trigger === 'manual' ? window.refreshSelection() : window.invalidateSelection(trigger === 'all' ? 'all' : undefined), trigger);
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

test('in-flight topic changes coalesce and disabled queries release their subscriptions', async ({ page }) => {
  let reads = 0, blockedRead = Infinity;
  let release: () => void = () => {};
  const held = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/query-fixture*', async route => {
    const value = ++reads;
    if (value === blockedRead) await held;
    await route.fulfill({ json: { Count: value } });
  });
  await page.goto('/tests/fixtures/query-selection.html');
  await expect(page.getByLabel('status')).toHaveText('ready');
  const initialReads = reads;
  blockedRead = initialReads + 1;
  await page.evaluate(() => window.invalidateSelection());
  await expect.poll(() => reads).toBe(initialReads + 1);
  await page.evaluate(() => { for (let i = 0; i < 10; i++) window.invalidateSelection(); });
  expect(reads).toBe(initialReads + 1);
  release();
  await expect(page.getByLabel('count')).toHaveText(String(initialReads + 2));
  await expect(page.getByLabel('status')).toHaveText('ready');
  expect(reads).toBe(initialReads + 2);
  await page.evaluate(() => window.selectionPath(null));
  await expect(page.getByLabel('count')).toHaveText('0');
  await page.evaluate(() => window.invalidateSelection());
  // A disabled query does not request data, even though revisions keep advancing.
  await page.waitForTimeout(100);
  expect(reads).toBe(initialReads + 2);
  await page.evaluate(() => window.selectionPath('/api/query-fixture'));
  await expect(page.getByLabel('count')).toHaveText(String(initialReads + 3));
  await expect(page.getByLabel('status')).toHaveText('ready');
  expect(reads).toBe(initialReads + 3);
});

test('unread navigation updates preserve the parent and accessible badge semantics', async ({ page }) => {
  let unread = 2;
  await page.route('**/api/query-fixture*', route => route.fulfill({ json: { Count: 1 } }));
  await page.route('**/api/conversations*', route => route.fulfill({ json: {
    Items: [{ ID: 'active', UnreadCount: unread }, { ID: 'archived', Archived: true, UnreadCount: 50 }, { ID: 'negative', UnreadCount: -2 }], NextCursor: '',
  } }));
  await page.goto('/tests/fixtures/query-selection.html');
  const badge = page.locator('#nav-unread-count');
  await expect(badge).toHaveText('2');
  const before = await page.evaluate(() => window.navigationRenders);
  for (const value of [105, 0, 3]) {
    unread = value;
    await page.evaluate(() => window.invalidateSelection('conversations'));
    if (value) {
      await expect(badge).toHaveText(value > 99 ? '99+' : String(value));
      await expect(page.locator('nav button')).toHaveAttribute('aria-describedby', /(?:^| )nav-unread-count(?: |$)/);
      await expect(badge).toHaveAttribute('aria-label', new RegExp(String(value)));
    } else {
      await expect(badge).toHaveCount(0);
      await expect(page.locator('nav button')).not.toHaveAttribute('aria-describedby', /(?:^| )nav-unread-count(?: |$)/);
    }
    expect(await page.evaluate(() => window.navigationRenders)).toBe(before);
  }
});

test('switching query paths releases the old topic and keeps the new query live', async ({ page }) => {
  const reads = { old: 0, next: 0 };
  await page.route('**/api/query-fixture*', route => { reads.old++; return route.fulfill({ json: { Count: 1 } }); });
  await page.route('**/api/query-next*', route => { reads.next++; return route.fulfill({ json: { Count: reads.next + 10 } }); });
  await page.goto('/tests/fixtures/query-selection.html');
  await expect(page.getByLabel('count')).toHaveText('1');
  await expect(page.getByLabel('status')).toHaveText('ready');
  const oldReads = reads.old;
  await page.evaluate(() => window.selectionPath('/api/query-next'));
  await expect(page.getByLabel('count')).toHaveText('11');
  await page.evaluate(() => { window.invalidateSelection('query-fixture'); window.invalidateSelection('query-next'); });
  await expect(page.getByLabel('count')).toHaveText('12');
  await expect(page.getByLabel('status')).toHaveText('ready');
  expect(reads.old).toBe(oldReads);
  expect(reads.next).toBe(2);
});
