import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync, readdirSync } from 'node:fs';
import { dirname, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));
test('desktop package includes every relative main-process module dependency', () => {
  const source = readFileSync(resolve(root, 'scripts/package.mjs'), 'utf8');
  const patterns = JSON.parse(source.match(/files:\s*(\[[^\]]+\])/)[1].replaceAll("'", '"'));
  const included = path => patterns.some(pattern => pattern.endsWith('/**') ? path.startsWith(pattern.slice(0, -2)) : path === pattern);
  const files = readdirSync(resolve(root, 'dist/main'), { recursive: true }).filter(file => /\.(?:c?js)$/.test(file));
  assert(files.length > 0);
  for (const file of files) {
    const path = resolve(root, 'dist/main', file), name = relative(root, path).replaceAll('\\', '/');
    if (!included(name)) continue;
    const code = readFileSync(path, 'utf8');
    for (const match of code.matchAll(/\b(?:from\s*|import\s*\(|require\s*\()(['"])(\.[^'"]+)\1/g)) {
      const dependency = relative(root, resolve(dirname(path), match[2])).replaceAll('\\', '/');
      assert(included(dependency), `${name} imports unpackaged module ${dependency}`);
      assert(files.includes(relative(resolve(root, 'dist/main'), resolve(root, dependency))), `${dependency} was not compiled`);
    }
  }
});
