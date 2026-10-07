import assert from 'node:assert/strict';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { renderToString } from 'react-dom/server';
import { createElement } from 'react';
import { createServer } from 'vite';

test('SSR imports and rendering need no browser globals; invalid playback is safe', async () => {
  assert.equal(typeof window, 'undefined'); assert.equal(typeof document, 'undefined');
  const server = await createServer({ configFile: false, root: fileURLToPath(new URL('..', import.meta.url)), server: { middlewareMode: true, hmr: false }, ssr: { noExternal: ['@molr/ui'] } });
  try {
    const module = await server.ssrLoadModule('/tests/ssr-fixture.tsx');
    const html = renderToString(createElement(module.ServerFixture));
    assert(html.replace(/<!--.*?-->/g, '').includes('system:light:0'));
    assert(html.includes('/nested/brand/molr-wordmark-alive.svg'));
    assert(html.includes('data-mobile="false"'));
    assert.equal((html.match(/role="slider"/g) ?? []).length, 2);
    assert(html.includes('aria-valuenow="5"'));
    const valid = module.getLogoTimeline('alive');
    for (const duration of [0, -1, NaN, Infinity]) assert.equal(module.getLogoPlayback({ ...valid, duration }, 1), null);
    assert.equal(module.getLogoPlayback({ duration: 10, steps: [] }, 1), null);
    assert.equal(module.getLogoPlayback(valid, Infinity), null);
    assert.equal(module.getLogoPlayback(valid, NaN), null);
    assert.equal(module.getLogoPlayback(valid, -1).index, 0);
    assert.equal(module.getLogoPlayback(valid, valid.duration).index, 0);
    assert.equal(module.getLogoPlayback({ duration: 100, steps: [{ ...valid.steps[0], end: 1 }] }, 50), null);
    assert(import.meta.resolve('@molr/ui/brand/molr-mark.svg').endsWith('/public/brand/molr-mark.svg'));
  } finally { await server.close(); }
});
