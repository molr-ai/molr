import assert from 'node:assert/strict';
import { readFileSync, readdirSync, lstatSync } from 'node:fs';
import { resolve, relative } from 'node:path';
import { pathToFileURL } from 'node:url';
import { catalogFiles } from './release-catalog.mjs';
import { validateManifest } from './release-lib.mjs';
import { readDesktopCatalog } from '../apps/website/src/desktop-releases.ts';
export function readCatalog(directory) {
  const files = {};
  function walk(path) {
    if (lstatSync(path).isSymbolicLink()) throw new Error('Catalog symlinks are forbidden');
    if (lstatSync(path).isDirectory()) { for (const name of readdirSync(path)) walk(resolve(path, name)); return; }
    if (!lstatSync(path).isFile()) throw new Error('Catalog must contain regular files');
    files[relative(directory, path)] = path.endsWith('.txt') ? readFileSync(path, 'utf8') : JSON.parse(readFileSync(path, 'utf8'));
  }
  walk(directory);
  const releases = Object.entries(files).filter(([name]) => name.startsWith('versions/') && name.endsWith('.json')).map(([, m]) => validateManifest(m));
  const expected = catalogFiles(releases);
  if (Object.hasOwn(files, 'desktop/catalog.json')) expected['desktop/catalog.json'] = readDesktopCatalog(files['desktop/catalog.json']);
  assert.deepEqual(files, expected, 'Catalog files do not match published versions and channel selection');
  return files;
}
if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) readCatalog(resolve(process.argv[2]));
