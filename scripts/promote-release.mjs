// Local catalog preparation; remote publication is owned by the release workflows.
import { readFileSync, readdirSync } from 'node:fs';
import { resolve } from 'node:path';
import { parseArgs } from 'node:util';
import { writeCatalog } from './release-catalog.mjs';
import { validateManifest } from './release-lib.mjs';
const { values } = parseArgs({ options: { manifests: { type: 'string' }, directory: { type: 'string', default: '.cache/release-catalog' } } });
if (!values.manifests) throw new Error('--manifests DIRECTORY containing verified release manifests is required');
const releases = readdirSync(values.manifests).filter(n => n.endsWith('.json')).map(n => validateManifest(JSON.parse(readFileSync(resolve(values.manifests, n)))));
writeCatalog(releases, values.directory);
console.log(`Channel catalog: ${resolve(values.directory)}`);
