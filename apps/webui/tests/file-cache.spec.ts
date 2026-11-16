import { test, expect } from '@playwright/test';
import { api, APIError } from '@molr/sdk';
import type { FileInfo } from '@molr/sdk';
import { FileCache } from '../src/file-cache';

const file = (ID: string, MediaType = 'image/png'): FileInfo => ({ ID, Name: ID, MediaType, Size: 4, State: 'ready' });
const flush = async () => { for (let i = 0; i < 8; i++) await Promise.resolve(); };
const original = { get: api.get, download: api.download, create: URL.createObjectURL, revoke: URL.revokeObjectURL };
let gets: string[], downloads: string[], revoked: string[], count: number;
test.beforeEach(() => {
  gets = []; downloads = []; revoked = []; count = 0;
  api.get = (async (path: string) => { gets.push(path); return file(path.split('/').at(-1)!); }) as typeof api.get;
  api.download = async path => { downloads.push(path); return new Blob(['1234']); };
  URL.createObjectURL = () => `blob:test-${++count}`;
  URL.revokeObjectURL = url => { revoked.push(url); };
});
test.afterEach(() => { api.get = original.get; api.download = original.download; URL.createObjectURL = original.create; URL.revokeObjectURL = original.revoke; });

test('shared readers deduplicate metadata and previews, navigation reuses completed data without idle requests', async () => {
  const cache = new FileCache();
  const a = cache.subscribe('image', () => {}), b = cache.subscribe('image', () => {});
  await flush();
  const url = cache.snapshot('image').previewUrl;
  expect(gets).toHaveLength(1); expect(downloads).toHaveLength(1);
  a(); b(); await flush();
  const release = cache.subscribe('image', () => {}); await flush();
  expect(cache.snapshot('image').previewUrl).toBe(url);
  expect(gets).toHaveLength(1); expect(downloads).toHaveLength(1); expect(revoked).toEqual([]);
  release(); cache.clear(); expect(revoked).toEqual([url]);
});

test('non-image metadata is reusable without fetching or retaining its body', async () => {
  const cache = new FileCache(); cache.remember(file('document', 'application/pdf'));
  const release = cache.subscribe('document', () => {}); await flush();
  expect(cache.snapshot('document').file?.Name).toBe('document');
  expect(downloads).toEqual([]); expect(gets).toEqual([]); expect(count).toBe(0);
  release(); cache.clear();
});

test('preview byte budget and metadata count evict inactive LRU entries while pinning visible images', async () => {
  const cache = new FileCache(3, 4);
  const a = cache.subscribe('a', () => {}); await flush();
  const first = cache.snapshot('a').previewUrl;
  const b = cache.subscribe('b', () => {}); await flush(); b(); await flush();
  expect(cache.snapshot('a').previewUrl).toBe(first);
  expect(cache.snapshot('b').file).toBeTruthy(); expect(cache.snapshot('b').previewUrl).toBeUndefined();
  expect(revoked).toEqual(['blob:test-2']);
  cache.remember(file('c', 'text/plain')); cache.remember(file('d', 'text/plain'));
  expect(cache.snapshot('b').file).toBeUndefined(); expect(cache.snapshot('a').file).toBeTruthy();
  a(); cache.clear(); expect(revoked).toContain(first);
});

test('one reader leaving cannot cancel another; late results after the last release or logout are discarded', async () => {
  const cache = new FileCache();
  let finish!: (value: Blob) => void, signal!: AbortSignal;
  api.download = async (_path, next) => { signal = next!; return new Promise(resolve => { finish = resolve; }); };
  const a = cache.subscribe('image', () => {}), b = cache.subscribe('image', () => {});
  await flush(); a(); await flush(); expect(signal.aborted).toBe(false);
  b(); await flush(); expect(signal.aborted).toBe(true);
  cache.clear(); finish(new Blob(['late'])); await flush();
  expect(count).toBe(0); expect(cache.snapshot('image').file).toBeUndefined();
  cache.remember(file('late-upload')); expect(cache.snapshot('late-upload').file).toBeUndefined();
});

test('reconnect validates metadata without re-downloading images; access denial removes previews', async () => {
  const cache = new FileCache(); const release = cache.subscribe('image', () => {}); await flush();
  const url = cache.snapshot('image').previewUrl;
  cache.invalidate(); await flush(); expect(gets).toHaveLength(2); expect(downloads).toHaveLength(1);
  expect(cache.snapshot('image').previewUrl).toBe(url);
  api.get = async () => { throw new APIError('forbidden', '', 403); };
  cache.invalidate(); await flush();
  expect(cache.snapshot('image').file).toBeUndefined(); expect(cache.snapshot('image').previewUrl).toBeUndefined();
  expect(revoked).toEqual([url]); release(); cache.clear();
});

test('failed previews can retry, deletion cancels in-flight content, and old responses cannot replace fresh data', async () => {
  const cache = new FileCache(); let finish!: (blob: Blob) => void;
  api.download = async () => { throw new APIError('unavailable', '', 503); };
  const release = cache.subscribe('image', () => {}); await flush(); expect(cache.snapshot('image').error).toBeTruthy();
  api.download = async () => new Promise(resolve => { finish = resolve; });
  cache.retry(['image']); await flush();
  api.get = async () => { throw new APIError('not_found', '', 404); };
  cache.invalidate('image', true); await flush(); finish(new Blob(['stale'])); await flush();
  expect(cache.snapshot('image').file).toBeUndefined(); expect(count).toBe(0);
  release(); cache.clear();
});


test('StrictMode teardown can reactivate fresh subscriptions without accepting the old request', async () => {
  const cache = new FileCache();
  const old = cache.subscribe('image', () => {}); await flush(); old(); cache.clear();
  const release = cache.subscribe('image', () => {}); await flush();
  expect(gets).toHaveLength(1);
  cache.activate(); await flush();
  expect(gets).toHaveLength(2); expect(cache.snapshot('image').previewUrl).toBe('blob:test-2');
  release(); cache.clear();
});


test('deletion arriving before an upload receipt prevents stale metadata seeding', () => {
  const cache = new FileCache();
  cache.invalidate('deleted', true);
  cache.remember(file('deleted', 'text/plain'));
  expect(cache.snapshot('deleted').file).toBeUndefined();
  cache.clear();
});
