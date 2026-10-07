import assert from 'node:assert/strict';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { createServer } from 'vite';

test('JSON code blocks preserve diagnostic data, escape markup and bound highlighting work', async () => {
  const server = await createServer({ configFile: false, root: fileURLToPath(new URL('..', import.meta.url)), server: { middlewareMode: true, hmr: false } });
  try {
    const { highlightJSON } = await server.ssrLoadModule('/src/code-highlight.ts');
    const { CodeBlock } = await server.ssrLoadModule('/src/components/CodeBlock.tsx');
    const code = '{\n "text": "中文 \\"quoted\\" \\u4f60 </code><script>alert(1)</script>", "integer": 9007199254740993, "fraction": -1.25e+3, "values": [true, false, null]\n}';
    const tokens = highlightJSON(code);
    assert(tokens);
    assert.equal(tokens.map(token => token.text).join(''), code);
    assert.equal(tokens.find(token => token.text === '9007199254740993')?.kind, 'number');
    assert.equal(tokens.find(token => token.text === '-1.25e+3')?.kind, 'number');
    assert.deepEqual(new Set(tokens.flatMap(token => token.kind ? [token.kind] : [])), new Set(['property', 'string', 'number', 'boolean', 'null', 'punctuation']));
    const html = renderToStaticMarkup(createElement(CodeBlock, { code, language: 'json', label: 'Diagnostic payload' }));
    assert(!html.includes('<script>'));
    assert(html.includes('&lt;script&gt;'));
    assert(html.includes('9007199254740993'));
    assert(html.includes('role="region"'));
    assert(html.includes('aria-label="Diagnostic payload"'));
    const examples = ['{ "unfinished":', '', '{"x":"unterminated}', JSON.stringify({ value: 'x'.repeat(100_001) }), JSON.stringify(Array(6000).fill(true))];
    for (const value of examples) {
      assert.equal(highlightJSON(value), null);
      const fallback = renderToStaticMarkup(createElement(CodeBlock, { code: value, language: 'json' }));
      assert(fallback.includes('data-highlighted="false"'));
      const escaped = value.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('"', '&quot;').replaceAll("'", '&#x27;');
      assert(fallback.includes(`<code>${escaped}</code>`));
    }
    const plain = renderToStaticMarkup(createElement(CodeBlock, { code: '{"hello":true}', language: 'text', wrap: false }));
    assert(!plain.includes('ui-code-token--'));
    assert(plain.includes('data-wrap="false"'));
    for (const value of ['true', 'false', 'null', '0', '-0', '1e-8', '"hello"', '{}', '[]']) assert.equal(highlightJSON(value).map(token => token.text).join(''), value);
  } finally { await server.close(); }
});
