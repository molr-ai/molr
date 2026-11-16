import { test } from 'node:test';
import assert from 'node:assert/strict';
import { typographyErrors } from './check-typography.mjs';
test('typography gate detects CSS, shorthand, inline strings and JS font sizes', () => {
 for (const source of ['.name { font-size: 13px }', '.name { font: 500 13px/1.5 sans-serif }', 'const style = { fontSize: 13 }', 'const style = { fontSize: "13px" }', 'el.style.cssText = "font-size:13px"']) assert.ok(typographyErrors('apps/webui/src/example.tsx', source).length, source);
 for (const source of ['.name { font-size: var(--text-sm); padding: 12px }', '.name { font: 500 var(--text-sm)/1.5 var(--font-sans) }', 'const style = { fontSize: "var(--text-sm)" }', 'term.options.fontSize = parseFloat(css.fontSize)']) assert.equal(typographyErrors('apps/webui/src/example.tsx', source).length, 0, source);
 assert.equal(typographyErrors('apps/website/src/home.css', '.hero { font-size: clamp(28px, 3vw, 40px) }').length, 0);
 assert.ok(typographyErrors('apps/website/src/home.css', '.label { font-size: 11px }').length);
});

test('typography exceptions use the same paths on Windows', () => {
 assert.equal(typographyErrors('apps\\website\\src\\home.css', '.hero { font-size: 32px }').length, 0);
 assert.equal(typographyErrors('packages\\ui\\src\\components\\ChatCopyButton.tsx', 'input.style.cssText = "font-size:16px"').length, 0);
 assert.ok(typographyErrors('apps\\webui\\src\\page.css', '.label { font-size: 13px }').length);
});
