// Validate and collect native desktop artifacts before the GitHub draft is created.
import { createHash } from 'node:crypto';
import { cpSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { releaseChannel, compareVersions } from './release-lib.mjs';
import { readDesktopCatalog } from '../apps/website/src/desktop-releases.ts';
export const desktopPlatforms = ['darwin-amd64', 'darwin-arm64', 'linux-amd64', 'linux-arm64', 'win32-amd64'];
export function validateDesktopRelease(m, repository, runtime) {
  if (!m || m.schema !== 1 || !/^[a-f0-9]{40}$/.test(m.commit) || !Array.isArray(m.platforms)) throw new Error('Invalid desktop release manifest');
  releaseChannel(m.version);
  if (runtime && (m.version !== runtime.version || m.commit !== runtime.commit)) throw new Error('Desktop/Runtime identity mismatch');
  if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository || '')) throw new Error('GitHub repository required');
  const base = `https://github.com/${repository}/releases/download/v${m.version}/`;
  const channel = releaseChannel(m.version);
  readDesktopCatalog({ schema: 1, channels: { stable: null, preview: null, nightly: null, [channel]: m } });
  const ids = m.platforms.map(p => `${p.os}-${p.arch}`).sort();
  if (JSON.stringify(ids) !== JSON.stringify(desktopPlatforms)) throw new Error('Incomplete desktop platform matrix');
  for (const p of m.platforms) {
    if (p.url !== base + p.file || p.checksum_url !== base + p.file + '.sha256') throw new Error('Desktop asset URL mismatch');
    if (typeof p.signed !== 'boolean') throw new Error('Desktop signing status required');
  }
  return m;
}
export function desktopCatalog(releases) {
  const channels = { stable: null, preview: null, nightly: null };
  for (const m of releases) {
    const kind = releaseChannel(m.version);
    for (const channel of kind === 'stable' ? ['stable', 'preview'] : [kind]) {
      if (!channels[channel] || compareVersions(m.version, channels[channel].version) > 0) channels[channel] = m;
    }
  }
  return readDesktopCatalog({ schema: 1, channels });
}
export function desktopReleaseFiles(m, directory) {
  const names = ['desktop-manifest.json'];
  for (const p of m.platforms) {
    const digest = createHash('sha256').update(readFileSync(resolve(directory, p.file))).digest('hex');
    if (digest !== p.sha256 || readFileSync(resolve(directory, p.file + '.sha256'), 'utf8') !== `${digest}  ${p.file}\n`) throw new Error(`Desktop artifact checksum mismatch: ${p.file}`);
    names.push(p.file, p.file + '.sha256');
  }
  return names;
}
export function collectDesktopRelease(source, destination, repository) {
  const runtime = JSON.parse(readFileSync(resolve(destination, 'manifest.json')));
  const manifests = [];
  function walk(path) {
    for (const entry of readdirSync(path, { withFileTypes: true })) {
      const file = resolve(path, entry.name);
      if (entry.isDirectory()) walk(file);
      else if (entry.isFile() && entry.name === 'desktop-manifest.json') manifests.push({ directory: path, m: JSON.parse(readFileSync(file)) });
    }
  }
  walk(source);
  const platforms = manifests.map(({ m }) => {
    if (m.schema !== 1 || m.version !== runtime.version || m.commit !== runtime.commit || m.artifacts?.length !== 1) throw new Error('Native desktop build identity mismatch');
    const a = m.artifacts[0];
    const url = `https://github.com/${repository}/releases/download/v${m.version}/${a.file}`;
    return { os: m.os, arch: m.arch, variant: m.variant, signed: m.signed, file: a.file, sha256: a.sha256, url, checksum_url: url + '.sha256' };
  }).sort((a, b) => `${a.os}-${a.arch}`.localeCompare(`${b.os}-${b.arch}`));
  const m = validateDesktopRelease({ schema: 1, version: runtime.version, commit: runtime.commit, platforms }, repository, runtime);
  for (let i = 0; i < manifests.length; i++) {
    const { directory, m: native } = manifests[i];
    const p = platforms.find(p => p.os === native.os && p.arch === native.arch);
    const files = desktopReleaseFiles({ platforms: [p] }, directory).slice(1);
    for (const file of files) cpSync(resolve(directory, file), resolve(destination, file));
  }
  writeFileSync(resolve(destination, 'desktop-manifest.json'), JSON.stringify(m, null, 2) + '\n');
  return m;
}
if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  collectDesktopRelease(resolve('dist/desktop-release'), resolve('dist/releases'), process.env.GITHUB_REPOSITORY);
  console.log('Desktop release: all five native packages and checksums verified');
}
