import { mkdirSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { parseArgs } from 'node:util';
import { channels, compareVersions, releaseChannel, selectChannels, validateManifest } from './release-lib.mjs';

async function githubReleaseRequest(repository, path) {
  if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository || '')) throw new Error('GitHub owner/repository required');
  const headers = { Accept: 'application/vnd.github+json', 'X-GitHub-Api-Version': '2022-11-28', 'User-Agent': 'molr-release-catalog' };
  if (process.env.GH_TOKEN) headers.Authorization = `Bearer ${process.env.GH_TOKEN}`;
  return fetch(`https://api.github.com/repos/${repository}/releases${path}`, { headers, redirect: 'error', signal: AbortSignal.timeout(30000) });
}

export async function publishedReleases(repository, { limit = Number(process.env.MOLR_RELEASE_SCAN_LIMIT || 100) } = {}) {
  if (!Number.isSafeInteger(limit) || limit < 1) throw new Error('MOLR_RELEASE_SCAN_LIMIT must be a positive safe integer');
  const selected = Object.fromEntries(channels.map(c => [c, null]));
  const pageSize = Math.min(limit, 100);
  for (let page = 1, scanned = 0; scanned < limit; page++) {
    const response = await githubReleaseRequest(repository, `?per_page=${pageSize}&page=${page}`);
    if (!response.ok) throw new Error(`GitHub release listing failed: HTTP ${response.status}`);
    const items = await response.json();
    if (!Array.isArray(items)) throw new Error('Invalid GitHub release list');
    for (const r of items.slice(0, limit - scanned)) {
      if (r.draft || !r.assets?.some(a => a.name === 'manifest.json')) continue;
      let channel;
      try { channel = releaseChannel(r.tag_name?.startsWith('v') ? r.tag_name.slice(1) : ''); }
      catch { continue; } // Releases outside our version protocol are not installable.
      const previous = selected[channel];
      if (!previous || compareVersions(r.tag_name.slice(1), previous.tag_name.slice(1)) > 0) selected[channel] = r;
    }
    scanned += items.length;
    if (items.length < pageSize) break;
  }
  const releases = [];
  for (const r of Object.values(selected)) if (r) releases.push(await releaseManifest(repository, r));
  return releases;
}

// Publication retries must still find an exact tag outside the website's scan window.
export async function publishedRelease(repository, version) {
  releaseChannel(version);
  const response = await githubReleaseRequest(repository, `/tags/v${version}`);
  if (response.status === 404) return null;
  if (!response.ok) throw new Error(`GitHub release lookup failed: v${version}, HTTP ${response.status}`);
  const r = await response.json();
  if (r.draft) return null;
  if (r.tag_name !== `v${version}`) throw new Error(`Release identity mismatch: v${version}`);
  return releaseManifest(repository, r);
}

async function releaseManifest(repository, r) {
  try {
    const asset = r.assets.find(a => a.name === 'manifest.json');
    if (!asset) throw new Error('Missing release asset: manifest.json');
    const response = await fetch(asset.browser_download_url, { signal: AbortSignal.timeout(30000) });
    if (!response.ok) throw new Error(`Manifest download failed: ${r.tag_name}`);
    const m = validateManifest(await response.json());
    if (r.tag_name !== `v${m.version}` || r.prerelease !== (m.channel !== 'stable')) throw new Error(`Release identity mismatch: ${r.tag_name}`);
    const base = `https://github.com/${repository}/releases/download/${r.tag_name}/`;
    for (const [name, url, digest] of m.platforms.flatMap(p => [[p.file, p.url, p.sha256], [p.file + '.sha256', p.checksum_url]])) {
      const asset = r.assets.find(a => a.name === name && a.browser_download_url === url);
      if (url !== base + name || !asset) throw new Error(`Missing release asset: ${name}`);
      if (digest && asset.digest && asset.digest !== `sha256:${digest}`) throw new Error(`Release asset digest mismatch: ${name}`);
    }
    return m;
  } catch (error) {
    throw new Error(`Release ${r.tag_name}: ${error.message}`, { cause: error });
  }
}

export async function verifyChannelDownloads(releases) {
  const urls = new Set();
  for (const m of Object.values(selectChannels(releases))) {
    if (!m) continue;
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
