import assert from 'node:assert/strict';
import { test } from 'node:test';
import postcss from 'postcss';
import breakpointQueries from '../postcss-breakpoints.mjs';
import breakpoints from '../breakpoints.json' with { type: 'json' };

test('named media queries compile from shared tokens, including compound queries', async () => {
  const source = '@media (--molr-mobile) { a { color: red } } @media (pointer: coarse), (--molr-compact) { b { color: blue } }';
  const result = await postcss([breakpointQueries()]).process(source, { from: undefined });
  assert(result.css.includes(`(max-width: ${breakpoints.mobile}px)`));
  assert(result.css.includes(`(pointer: coarse), (max-width: ${breakpoints.compact}px)`));
  assert(!result.css.includes('--molr-'));
  await assert.rejects(postcss([breakpointQueries()]).process('@media (--molr-unknown) {}', { from: undefined }), /Unknown Molr breakpoint/);
});
