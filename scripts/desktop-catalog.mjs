// Assemble a reviewed native desktop release locally. Does not upload or publish.
import { readFileSync, readdirSync, mkdirSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { parseArgs } from 'node:util';
import { releaseChannel } from './release-lib.mjs';
import { readDesktopCatalog } from '../apps/website/src/desktop-releases.ts';
const { values } = parseArgs({ options: { manifests: { type: 'string' }, 'base-url': { type: 'string' }, directory: { type: 'string', default: '.cache/desktop-catalog' }, existing: { type: 'string' } } });
if (!values.manifests || !values['base-url']) throw new Error('--manifests DIRECTORY and --base-url HTTPS_URL required');
const base = new URL(values['base-url'].replace(/\/$/, '') + '/');
if (base.protocol !== 'https:' || base.username || base.password || base.search || base.hash) throw new Error('HTTPS release asset base URL required');
const manifests = readdirSync(values.manifests).filter(n => n.endsWith('.json')).map(n => JSON.parse(readFileSync(resolve(values.manifests, n))));
if (!manifests.length) throw new Error('No desktop manifests');
const { version, commit } = manifests[0];
if (version.endsWith('-local') || !/^[a-f0-9]{40}$/.test(commit)) throw new Error('Only release builds can enter a published catalog');
const channel = releaseChannel(version);
const platforms = [], seen = new Set();
for (const m of manifests) {
  if (m.schema !== 1 || m.version !== version || m.commit !== commit || !['linux', 'darwin', 'win32'].includes(m.os) || !['amd64', 'arm64'].includes(m.arch) || m.variant !== (m.os === 'win32' ? 'client' : 'full') || (m.os !== 'linux' && m.signed !== true) || !Array.isArray(m.artifacts) || m.artifacts.length !== 1) throw new Error('Inconsistent or unsigned desktop manifest');
  const id = `${m.os}-${m.arch}`; if (seen.has(id)) throw new Error('Duplicate platform'); seen.add(id);
  const a = m.artifacts[0], ext = { linux: 'deb', darwin: 'dmg', win32: 'exe' }[m.os];
  if (a.file !== `molr-desktop_${version}_${id}_${m.variant}.${ext}` || !/^[a-f0-9]{64}$/.test(a.sha256)) throw new Error('Invalid desktop artifact');
  platforms.push({ os: m.os, arch: m.arch, variant: m.variant, ...a, url: new URL(a.file, base).href });
}
const catalog = values.existing ? JSON.parse(readFileSync(values.existing)) : { schema: 1, channels: { stable: null, preview: null, nightly: null } };
if (catalog.schema !== 1 || !catalog.channels) throw new Error('Invalid existing desktop catalog');
catalog.channels[channel] = { version, commit, platforms };
mkdirSync(values.directory, { recursive: true });
writeFileSync(resolve(values.directory, 'catalog.json'), JSON.stringify(readDesktopCatalog(catalog), null, 2) + '\n');
console.log(`Desktop catalog prepared: ${resolve(values.directory, 'catalog.json')}`);
