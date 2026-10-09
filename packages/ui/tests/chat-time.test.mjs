import assert from 'node:assert/strict';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { createServer } from 'vite';

test('chat time reuses bounded explicit-zone formatters while dates and implicit zones stay live', async () => {
  const server = await createServer({ configFile: false, root: fileURLToPath(new URL('..', import.meta.url)), server: { middlewareMode: true, hmr: false } });
  const native = Intl.DateTimeFormat, originalZone = process.env.TZ;
  let constructions = 0;
  Intl.DateTimeFormat = new Proxy(native, { construct(target, args, newTarget) { constructions++; return Reflect.construct(target, args, newTarget); } });
  try {
    const { formatChatTime } = await server.ssrLoadModule('/src/chat-time.ts');
    const now = new Date('2026-10-09T04:00:00Z'), at = '2026-10-08T16:05:00Z';
    assert.deepEqual(formatChatTime(at, 'zh', 'Asia/Shanghai', now), { text: '00:05', full: '2026年 10月9日 00:05' });
    const initial = constructions;
    for (let i = 0; i < 500; i++) formatChatTime(new Date(Date.parse(at) - i * 60000).toISOString(), 'zh', 'Asia/Shanghai', now);
    assert.equal(constructions, initial, 'same account language/zone should reuse its formatter');
    assert.deepEqual(formatChatTime(at, 'en', 'UTC', now), { text: 'October 8, 16:05', full: 'October 8, 2026, 16:05' });
    assert.equal(formatChatTime('2025-12-31T23:30:00Z', 'en', 'UTC', now).text, 'December 31, 2025, 23:30');
    assert.equal(formatChatTime('2026-10-08T23:59:00Z', 'en', 'UTC', new Date('2026-10-08T23:59:30Z')).text, '23:59');
    assert.equal(formatChatTime('2026-10-08T23:59:00Z', 'en', 'UTC', new Date('2026-10-09T00:01:00Z')).text, 'October 8, 23:59');
    assert.equal(formatChatTime('2026-12-31T16:05:00Z', 'en', 'Asia/Shanghai', new Date('2026-12-31T18:00:00Z')).full, 'January 1, 2027, 00:05');
    const spring = new Date('2026-03-08T12:00:00Z');
    assert.equal(formatChatTime('2026-03-08T06:59:00Z', 'en', 'America/New_York', spring).text, '01:59');
    assert.equal(formatChatTime('2026-03-08T07:01:00Z', 'en', 'America/New_York', spring).text, '03:01');
    const autumn = new Date('2026-11-01T12:00:00Z');
    assert.equal(formatChatTime('2026-11-01T05:30:00Z', 'en', 'America/New_York', autumn).text, '01:30');
    assert.equal(formatChatTime('2026-11-01T06:30:00Z', 'en', 'America/New_York', autumn).text, '01:30');
    assert.equal(formatChatTime('invalid', 'zh', 'invalid/zone', now), undefined);
    assert.throws(() => formatChatTime(at, 'en', 'invalid/zone', now), RangeError);
    process.env.TZ = 'UTC';
    assert.equal(formatChatTime(at, 'en', undefined, now).text, 'October 8, 16:05');
    process.env.TZ = 'Pacific/Honolulu';
    assert.equal(formatChatTime(at, 'en', undefined, now).text, '06:05');
    assert.equal(formatChatTime(at, 'en', 'UTC', now).text, 'October 8, 16:05');
    const zones = Intl.supportedValuesOf('timeZone').slice(0, 40);
    for (const zone of zones) formatChatTime(at, 'zh', zone, now);
    const before = constructions;
    formatChatTime(at, 'zh', zones[0], now);
    assert.equal(constructions, before + 1, 'old zones must be evicted rather than retained indefinitely');
    formatChatTime(at, 'zh', zones.at(-1), now);
    assert.equal(constructions, before + 1, 'recent zone should remain cached');
  } finally {
    Intl.DateTimeFormat = native;
    if (originalZone === undefined) delete process.env.TZ; else process.env.TZ = originalZone;
    await server.close();
  }
});
