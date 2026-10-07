import { test, expect } from '@playwright/test';

test('continuous refresh events flush within 500ms and preserve every topic', async ({ page }) => {
  await page.goto('/');
  await page.clock.install({ time: new Date('2026-09-28T00:00:00Z') });
  await page.clock.pauseAt(new Date('2026-09-28T00:01:00Z'));
  await page.evaluate(async () => {
    const path = '/src/batch-topics.ts';
    const { batchTopics } = await import(path);
    const state = window as unknown as { batches: string[][]; stop: () => void };
    state.batches = [];
    const batch = batchTopics((topics: string[]) => state.batches.push(topics));
    batch.add(['messages']);
    const timer = setInterval(() => batch.add(['status']), 100);
    state.stop = () => { clearInterval(timer); batch.cancel(); };
  });
  await page.clock.runFor(499);
  expect(await page.evaluate(() => (window as any).batches)).toEqual([]);
  await page.clock.runFor(1);
  expect(await page.evaluate(() => (window as any).batches)).toEqual([['messages', 'status']]);
  await page.clock.runFor(1100);
  expect((await page.evaluate(() => (window as any).batches)).length).toBeGreaterThanOrEqual(3);
  await page.evaluate(() => (window as any).stop());
  const count = await page.evaluate(() => (window as any).batches.length);
  await page.clock.runFor(1000);
  expect(await page.evaluate(() => (window as any).batches.length)).toBe(count);
});
