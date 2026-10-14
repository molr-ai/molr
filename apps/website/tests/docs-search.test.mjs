import assert from 'node:assert/strict';
import { test } from 'node:test';
import { indexDocument, searchDocuments } from '../src/docs-search.ts';

function document(title, content, headings = []) {
  return { title, label: title, group: 'Guide', path: `/${title}/`, searchIndex: indexDocument(content, title, title, headings) };
}

test('search distinguishes title, heading and body matches and ranks them', () => {
  const body = document('Body', '# Body\n\n## Setup\nRun Bubblewrap.', [{ title: 'Setup', id: 'setup' }]);
  const heading = document('Heading', '# Heading\n\n## Bubblewrap\nInstall the tool.', [{ title: 'Bubblewrap', id: 'bubblewrap' }]);
  const title = document('Bubblewrap guide', '# Bubblewrap guide\nInstall the tool.');
  const results = searchDocuments([body, heading, title], ' BUBBLEWRAP ');
  assert.deepEqual(results.map(result => result.kinds), [['title'], ['heading'], ['body']]);
  assert.deepEqual(results.map(result => result.href), ['/Bubblewrap guide/', '/Heading/#bubblewrap', '/Body/#setup']);
  assert.equal(results[2].snippet, 'Run Bubblewrap.');
  assert.deepEqual(searchDocuments([body], '  '), []);
});

test('search ignores markup and link targets, preserves code identifiers and prompt text', () => {
  const doc = document('Guide', '# Guide\n<!-- hidden-comment -->\n[Visible label](https://example.com/hidden-target)\n> [!PROMPT]\n> Please summarize.\n\n> [!INFO]\n> Browser information.\n\n```sh\n# not a title\nCGO_ENABLED=0\n--help\n*.txt\n```\n\n## End\nA concluding phrase.', [{ title: 'End', id: 'end' }]);
  for (const query of ['hidden-comment', 'hidden-target', '[!PROMPT]', '[!INFO]']) assert.equal(searchDocuments([doc], query).length, 0);
  for (const query of ['Visible label', 'Browser information', 'Please summarize', 'CGO_ENABLED=0', '--help', '*.txt', 'not a title']) assert.equal(searchDocuments([doc], query).length, 1);
  assert.equal(searchDocuments([doc], 'concluding')[0].href, '/Guide/#end');
});

test('body excerpt includes late matches and duplicate headings retain their anchors', () => {
  const doc = document('Guide', '# Guide\n## Setup\nNothing here.\n## Setup\n' + 'context '.repeat(100) + 'needle ending', [{ title: 'Setup', id: 'setup' }, { title: 'Setup', id: 'setup-1' }]);
  const result = searchDocuments([doc], 'needle')[0];
  assert.equal(result.href, '/Guide/#setup-1');
  assert(result.snippet.includes('needle'));
  assert(result.snippet.startsWith('…'));
  assert(result.snippet.length <= 182);
});
