import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import Markdown from 'react-markdown';
import { remarkCallouts } from '../src/docs-callouts.ts';

const render = content => renderToStaticMarkup(createElement(Markdown, { remarkPlugins: [remarkCallouts], children: content }));

test('warning blocks stay separate and preserve formatted content', () => {
  const html = render('> [!WARNING]\n> **Development:** Breaking changes.\n\n<!-- -->\n\n> [!WARNING]\n> **Network:** Use a [trusted network](https://example.com).');
  assert.equal((html.match(/class="docs-warning"/g) ?? []).length, 2);
  assert(!html.includes('[!WARNING]'));
  assert(!html.includes('&lt;!--'));
  assert(html.includes('<strong>Development:</strong>'));
  assert(html.includes('<a href="https://example.com">trusted network</a>'));
});

test('ordinary quotes, inline markers and code examples remain unchanged', () => {
  for (const source of ['> Ordinary quote', '> Mention [!WARNING] inline', '> `[!WARNING]`', '```md\n> [!WARNING]\n```']) {
    assert(!render(source).includes('class="docs-warning"'));
  }
});

test('prompt examples render independently from warnings and ordinary quotes', () => {
  const html = render('> [!PROMPT]\n> First request.\n\n<!-- -->\n\n> [!PROMPT]\n> Second request.\n\n<!-- -->\n\n> [!WARNING]\n> Caution.');
  assert.equal((html.match(/class="docs-prompt"/g) ?? []).length, 2);
  assert.equal((html.match(/class="docs-warning"/g) ?? []).length, 1);
  assert(!html.includes('[!PROMPT]'));
  assert(html.includes('First request.') && html.includes('Second request.'));
});
