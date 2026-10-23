import { test, expect } from '@playwright/test';
import { TopicStore } from '../src/topic-store';

test('topic snapshots retain unobserved changes and release old subscriptions', () => {
  const store = new TopicStore();
  store.invalidate(['messages:a']);
  expect(store.version('messages:a')).toBe(1);
  let calls = 0;
  const release = store.subscribe('messages:a', () => { calls++; });
  store.invalidate(['messages:b']); expect(calls).toBe(0);
  store.invalidate(['messages:a', 'messages', 'messages']);
  expect(calls).toBe(1); expect(store.version('messages:a')).toBe(3);
  store.invalidate(['all']); expect(calls).toBe(2);
  release(); store.invalidate(['all', 'messages:a']); expect(calls).toBe(2);
  expect(new TopicStore().version('messages:a')).toBe(0);
});

test('scoped changes render and refresh only their subscribers, while reconnect and account changes still reconcile', async ({ page }) => {
  // Drive the SSE boundary explicitly so background reconnection cannot hide extra renders.
  await page.addInitScript(() => {
    window.EventSource = class extends EventTarget {
      onopen = null; onerror = null; readyState = 1;
      relay = () => this.dispatchEvent(new MessageEvent('ready', { data: '{}' }));
      constructor() { super(); window.addEventListener('fixture:ready', this.relay); }
      close() { window.removeEventListener('fixture:ready', this.relay); }
    } as unknown as typeof EventSource;
  });
  const reads: Record<string, number> = { a: 0, b: 0 };
  await page.route('**/api/assistants/*/status', route => {
    const id = new URL(route.request().url()).pathname.split('/')[3];
    return route.fulfill({ json: { revision: ++reads[id] } });
  });
  await page.goto('/tests/fixtures/topic-subscriptions.html');
  for (const id of ['a', 'b']) await expect(page.getByTestId(`query:${id}`)).toHaveAttribute('data-loading', 'false');
  const untouched = ['messages:b', 'messages', 'plans', 'debug', 'controls', 'connection', 'query:a', 'query:b'];
  const before = await Promise.all(untouched.map(id => page.getByTestId(id).getAttribute('data-renders')));
  await page.getByRole('button', { name: 'Conversation A', exact: true }).click();
  await expect(page.getByTestId('messages:a')).toHaveText('1');
  for (const [index, id] of untouched.entries()) await expect(page.getByTestId(id)).toHaveAttribute('data-renders', before[index]!);
  await page.getByRole('button', { name: 'All conversations', exact: true }).click();
  await expect(page.getByTestId('messages:a')).toHaveText('2');
  await expect(page.getByTestId('messages:b')).toHaveText('1');
  await expect(page.getByTestId('messages')).toHaveText('1');
  const prior = { ...reads };
  await page.getByRole('button', { name: 'Assistant A', exact: true }).click();
  await expect(page.getByTestId('query:a')).toHaveText(String(prior.a + 1));
  expect(reads.b).toBe(prior.b);
  await expect(page.getByTestId('controls')).toHaveAttribute('data-renders', before[4]!);
  const queryRenders = await page.getByTestId('query:b').getAttribute('data-renders');
  await page.evaluate(() => {
    Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => false });
    window.dispatchEvent(new Event('offline'));
  });
  await expect(page.getByTestId('connection')).toHaveText('disconnected');
  await expect(page.getByTestId('query:b')).toHaveAttribute('data-renders', queryRenders!);
  await page.evaluate(() => { Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => true }); });
  // Explicit global invalidation refreshes all snapshots, including the quiet query.
  await page.getByRole('button', { name: 'All data', exact: true }).click();
  await expect(page.getByTestId('query:b')).toHaveText(String(prior.b + 1));
  await expect(page.getByTestId('plans')).toHaveText('1');
  await page.getByRole('button', { name: 'Switch account', exact: true }).click();
  await expect(page.getByTestId('messages:a')).toHaveText('0');
  await expect(page.getByTestId('plans')).toHaveText('0');
  await expect(page.getByTestId('query:b')).toHaveAttribute('data-loading', 'false');
  const current = reads.b;
  await page.evaluate(() => window.dispatchEvent(new Event('fixture:ready')));
  await expect(page.getByTestId('connection')).toHaveText('connected');
  await expect(page.getByTestId('query:b')).toHaveText(String(current + 1));
  await expect(page.getByTestId('plans')).toHaveText('1');
});
