import { mkdirSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { parseArgs } from 'node:util';
import { channels, selectChannels, validateManifest } from './release-lib.mjs';

export async function publishedReleases(repository) {
  if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository || '')) throw new Error('GitHub owner/repository required');
  const listed = [];
  const headers = { Accept: 'application/vnd.github+json', 'X-GitHub-Api-Version': '2022-11-28', 'User-Agent': 'molr-release-catalog' };
  if (process.env.GH_TOKEN) headers.Authorization = `Bearer ${process.env.GH_TOKEN}`;
  for (let page = 1; ; page++) {
    const response = await fetch(`https://api.github.com/repos/${repository}/releases?per_page=100&page=${page}`, { headers, redirect: 'error', signal: AbortSignal.timeout(30000) });
    if (!response.ok) throw new Error(`GitHub release listing failed: HTTP ${response.status}`);
    const items = await response.json();
    if (!Array.isArray(items)) throw new Error('Invalid GitHub release list');
    listed.push(...items);
    if (items.length < 100) break;
  }
  const releases = [];
  for (const r of listed) {
    if (r.draft) continue;
    const asset = r.assets.find(a => a.name === 'manifest.json');
    if (!asset) continue; // Releases predating this protocol are not advertised as installable.
    const response = await fetch(asset.browser_download_url, { signal: AbortSignal.timeout(30000) });
    if (!response.ok) throw new Error(`Manifest download failed: ${r.tag_name}`);
    const m = validateManifest(await response.json());
    if (r.tag_name !== `v${m.version}` || r.prerelease !== (m.channel !== 'stable')) throw new Error(`Release identity mismatch: ${r.tag_name}`);
    const base = `https://github.com/${repository}/releases/download/${r.tag_name}/`;
    for (const [name, url, digest] of [['install.sh', m.installer_url, m.installer_sha256], ...m.platforms.flatMap(p => [[p.file, p.url, p.sha256], [p.file + '.sha256', p.checksum_url]])]) {
      const asset = r.assets.find(a => a.name === name && a.browser_download_url === url);
      if (url !== base + name || !asset) throw new Error(`Missing release asset: ${name}`);
      if (digest && asset.digest && asset.digest !== `sha256:${digest}`) throw new Error(`Release asset digest mismatch: ${name}`);
    }
    releases.push(m);
  }
  return releases;
}

export async function verifyChannelDownloads(releases) {
  const urls = new Set();
  for (const m of Object.values(selectChannels(releases))) {
    if (!m) continue;
    urls.add(m.installer_url);
    for (const p of m.platforms) { urls.add(p.url); urls.add(p.checksum_url); }
  }
  for (const url of urls) {
    const response = await fetch(url, { method: 'HEAD', signal: AbortSignal.timeout(30000) });
    if (!response.ok) throw new Error(`Channel download is unavailable: ${url}`);
  }
}

function downloadDescriptor(m, p) {
  return ['molr-download-v1', m.version, m.commit, p.id, p.file, p.url, p.sha256, ''].join('\n');
}
export function catalogFiles(releases) {
  const selected = selectChannels(releases), files = {};
  for (const m of releases) {
    const name = `versions/${m.version}.json`;
    if (files[name]) throw new Error(`Duplicate release version: ${m.version}`);
    files[name] = m;
    for (const p of m.platforms) files[`versions/${m.version}/download-${p.id}.txt`] = downloadDescriptor(m, p);
  }
  for (const channel of channels) {
    const m = selected[channel];
    if (!m) continue;
    files[`${channel}/manifest.json`] = m;
    for (const p of m.platforms) files[`${channel}/download-${p.id}.txt`] = downloadDescriptor(m, p);
    for (const p of m.platforms) files[`${channel}/latest-${p.os}-${p.arch}.json`] = {
      version: m.version, commit: m.commit, published_at: m.published_at, notes_url: m.notes_url, file: p.file, sha256: p.sha256,
    };
  }
  files['catalog.json'] = { schema: 1, channels: selected };
  return files;
}

export function writeCatalog(releases, directory) {
  const files = catalogFiles(releases);
  for (const [name, value] of Object.entries(files)) {
    const path = resolve(directory, name);
    mkdirSync(resolve(path, '..'), { recursive: true });
    writeFileSync(path, typeof value === 'string' ? value : JSON.stringify(value, null, 2) + '\n');
  }
  return files;
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const { values } = parseArgs({ options: { repository: { type: 'string' }, directory: { type: 'string', default: '.cache/release-catalog' } } });
  const releases = await publishedReleases(values.repository || process.env.MOLR_RELEASE_REPOSITORY || process.env.GITHUB_REPOSITORY || 'molr-ai/molr');
  await verifyChannelDownloads(releases);
  writeCatalog(releases, values.directory);
  console.log(`Catalog: ${releases.length} published releases`);
}
