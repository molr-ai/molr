import { expect, test } from '@playwright/test';
import { NoticeQueue } from '../src/notifications/notice-queue';

test('notice lookups coalesce duplicates and reconcile a newer version after an in-flight read', async () => {
  const reads: { id: string; resolve: () => void }[] = [], invalidated: string[] = [];
  const queue = new NoticeQueue(id => new Promise<void>(resolve => reads.push({ id, resolve })), id => invalidated.push(id));
  const first = queue.request('notice', 1);
  await queue.request('notice', 1);
  await queue.request('notice', 2);
  await queue.request('notice', 3);
  await queue.request('notice', 2);
  expect(reads).toHaveLength(1);
  expect(invalidated).toEqual(['notice', 'notice']);
  reads[0].resolve();
  await expect.poll(() => reads.length).toBe(2);
  reads[1].resolve(); await first;
  const next = queue.request('notice', 4);
  expect(reads).toHaveLength(3);
  reads[2].resolve(); await next;
});

test('independent notices can load together and disposal prevents queued reads', async () => {
  const reads: (() => void)[] = [];
  const queue = new NoticeQueue(() => new Promise<void>(resolve => reads.push(resolve)), () => {});
  const first = queue.request('one', 1), second = queue.request('two', 1);
  expect(reads).toHaveLength(2);
  await queue.request('one', 2);
  queue.stop();
  reads.forEach(resolve => resolve());
  await Promise.all([first, second]);
  await queue.request('three', 1);
  expect(reads).toHaveLength(2);
});
