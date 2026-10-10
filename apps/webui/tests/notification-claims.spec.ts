import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import type { NotificationClaims } from '../src/notifications/coordinator';

type Fixture = { claims: NotificationClaims; db: IDBDatabase; now: number; scans: number; visits: number; abortScan: boolean };
declare global { interface Window { claimFixture: Fixture } }

async function setup(page: Page) {
  await page.goto('/');
  await page.evaluate(async () => {
    const path = '/src/notifications/coordinator.ts';
    const { NotificationClaims } = await import(path);
    const db = await new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open('molr-notification-claims', 1);
      request.onupgradeneeded = () => request.result.createObjectStore('claims');
      request.onsuccess = () => resolve(request.result); request.onerror = () => reject(request.error);
    });
    const state = window.claimFixture = { claims: new NotificationClaims(), db, now: 1_800_000_000_000, scans: 0, visits: 0, abortScan: false };
    Date.now = () => state.now;
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction('claims', 'readwrite'), store = tx.objectStore('claims');
      for (let i = 0; i < 1000; i++) store.put({ sequence: 1, at: state.now }, `history:${i}`);
      store.put({ sequence: 1, at: state.now - 7 * 86400_000 - 1 }, 'expired');
      store.put({ at: state.now - 7 * 86400_000 - 1 }, 'sound:expired');
      store.put({ sequence: 1, at: state.now - 7 * 86400_000 + 30_000 }, 'expires-soon');
      tx.oncomplete = () => resolve(); tx.onerror = () => reject(tx.error);
    });
    const original = IDBObjectStore.prototype.openCursor;
    IDBObjectStore.prototype.openCursor = function(...args) {
      state.scans++;
      const request = original.apply(this, args);
      request.addEventListener('success', () => { if (request.result) state.visits++; });
      if (state.abortScan) { state.abortScan = false; this.transaction.abort(); }
      return request;
    };
  });
}
async function stored(page: Page, key: string) {
  return page.evaluate(key => new Promise(resolve => {
    const request = window.claimFixture.db.transaction('claims').objectStore('claims').get(key);
    request.onsuccess = () => resolve(request.result ?? null);
  }), key);
}

test.afterEach(async ({ page }) => {
  await page.evaluate(() => { window.claimFixture?.claims.close(); window.claimFixture?.db.close(); });
});

test('concurrent notification claims share cleanup without rescanning recent history', async ({ page }) => {
  await setup(page);
  const result = await page.evaluate(async () => {
    const state = window.claimFixture;
    const outcomes = await Promise.all(Array.from({ length: 10 }, (_, index) => state.claims.claim('account', 'conversation', index + 1)));
    const duplicates = await Promise.all([state.claims.claim('account', 'conversation', 11), state.claims.claim('account', 'conversation', 11)]);
    return { outcomes, duplicates, scans: state.scans, visits: state.visits };
  });
  expect(result.outcomes).toEqual(Array(10).fill(true));
  expect(result.duplicates.sort()).toEqual([false, true]);
  expect(result.scans).toBe(1);
  expect(result.visits).toBeLessThanOrEqual(1004);
  expect(await stored(page, 'expired')).toBeNull();
  expect(await stored(page, 'sound:expired')).toBeNull();
  expect(await stored(page, 'history:0')).not.toBeNull();
});

test('notification cleanup resumes after its interval and after the clock moves back', async ({ page }) => {
  await setup(page);
  expect(await page.evaluate(() => window.claimFixture.claims.claim('account', 'conversation', 1))).toBe(true);
  expect(await stored(page, 'expires-soon')).not.toBeNull();
  await page.evaluate(async () => { const s = window.claimFixture; s.now += 59_999; await s.claims.claim('account', 'conversation', 2); });
  expect(await stored(page, 'expires-soon')).not.toBeNull();
  expect(await page.evaluate(() => window.claimFixture.scans)).toBe(1);
  await page.evaluate(async () => { const s = window.claimFixture; s.now++; await s.claims.claim('account', 'conversation', 3); });
  expect(await stored(page, 'expires-soon')).toBeNull();
  expect(await page.evaluate(() => window.claimFixture.scans)).toBe(2);
  await page.evaluate(async () => { const s = window.claimFixture; s.now -= 120_000; await s.claims.claim('account', 'conversation', 4); });
  expect(await page.evaluate(() => window.claimFixture.scans)).toBe(3);
  expect(await stored(page, 'history:0')).not.toBeNull();
});

test('aborted notification cleanup fails closed and retries without losing claims', async ({ page }) => {
  await setup(page);
  expect(await page.evaluate(() => { const s = window.claimFixture; s.abortScan = true; return s.claims.claim('account', 'conversation', 1); })).toBe(false);
  expect(await stored(page, 'account:conversation')).toBeNull();
  expect(await stored(page, 'expired')).not.toBeNull();
  expect(await page.evaluate(() => window.claimFixture.claims.claim('account', 'conversation', 1))).toBe(true);
  expect(await stored(page, 'expired')).toBeNull();
  expect(await page.evaluate(() => window.claimFixture.scans)).toBe(2);
  expect(await page.evaluate(() => window.claimFixture.claims.claim('account', 'conversation', 2))).toBe(true);
  expect(await page.evaluate(() => window.claimFixture.scans)).toBe(2);
  expect(await page.evaluate(() => { const s = window.claimFixture; s.claims.close(); return s.claims.claim('account', 'conversation', 3); })).toBe(false);
});
