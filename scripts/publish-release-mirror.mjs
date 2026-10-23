// Publish already released, verified files to the official OSS mirror.
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { lstatSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { resolve, join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { catalogFiles } from './release-catalog.mjs';
import { validateDesktopRelease, desktopReleaseFiles } from './desktop-release.mjs';
import { validateManifest, compareVersions } from './release-lib.mjs';

const origin = 'https://cdn.molr.ai';
const digest = data => createHash('sha256').update(data).digest('hex');

export async function publishMirror({ directory, repository, version, store, installer = resolve('scripts/install.sh') }) {
  const m = validateManifest(JSON.parse(readFileSync(join(directory, 'manifest.json'))));
  if (m.channel !== 'stable' || m.version !== version) throw new Error('Only the requested stable release may be mirrored');
  const desktop = validateDesktopRelease(JSON.parse(readFileSync(join(directory, 'desktop-manifest.json'))), repository, m);
  const files = ['manifest.json', ...desktopReleaseFiles(desktop, directory)];
  for (const p of m.platforms) {
    if (p.url !== `https://github.com/${repository}/releases/download/v${m.version}/${p.file}`) throw new Error('Release asset URL mismatch');
    if (digest(readFileSync(join(directory, p.file))) !== p.sha256 || readFileSync(join(directory, p.file + '.sha256'), 'utf8') !== `${p.sha256}  ${p.file}\n`) throw new Error(`Runtime checksum mismatch: ${p.file}`);
    files.push(p.file, p.file + '.sha256');
  }
  const source = `molr_source_${m.commit.slice(0, 12)}.tar.gz`;
  const sourceDigest = digest(readFileSync(join(directory, source)));
  if (readFileSync(join(directory, source + '.sha256'), 'utf8') !== `${sourceDigest}  ${source}\n`) throw new Error('Source checksum mismatch');
  files.push(source, source + '.sha256');
  // Validate the entire allowlist before the first remote write; never upload a directory recursively.
  for (const name of files) if (!lstatSync(join(directory, name)).isFile()) throw new Error(`Not a regular release file: ${name}`);
  if (!lstatSync(installer).isFile()) throw new Error('Not a regular installer');
  const previousBytes = await store.read('releases/stable/manifest.json');
  const previous = previousBytes === null ? null : validateManifest(JSON.parse(previousBytes));
  if (previous && previous.channel !== 'stable') throw new Error('Invalid mirror stable pointer');
  if (previous?.version === m.version && previous.commit !== m.commit) throw new Error('Mirror release identity mismatch');
  for (const name of files) {
    const key = `releases/download/v${m.version}/${name}`;
    await store.put(key, join(directory, name), 'public, max-age=31536000, immutable');
    await store.verify(key, lstatSync(join(directory, name)).size);
  }
  const mirror = structuredClone(m);
  for (const p of mirror.platforms) {
    p.url = `${origin}/releases/download/v${m.version}/${p.file}`;
    p.checksum_url = p.url + '.sha256';
  }
  const temporary = mkdtempSync(join(tmpdir(), 'molr-mirror-'));
  try {
    const generated = catalogFiles([mirror]);
    const publishLatest = !previous || compareVersions(m.version, previous.version) >= 0;
    for (const [name, value] of Object.entries(generated)) {
      // OSS holds stable and versioned metadata only; preview/nightly remain on GitHub.
      if (!name.startsWith('versions/') && !(publishLatest && name.startsWith('stable/'))) continue;
      if (name === 'stable/manifest.json') continue; // Commit the stable pointer last.
      const path = join(temporary, 'metadata');
      writeFileSync(path, typeof value === 'string' ? value : JSON.stringify(value, null, 2) + '\n');
      await store.put(`releases/${name}`, path, name.startsWith('versions/') ? 'public, max-age=31536000, immutable' : 'no-cache');
    }
    if (publishLatest) {
      await store.put('install.sh', installer, 'no-cache');
      const path = join(temporary, 'manifest.json');
      writeFileSync(path, JSON.stringify(mirror, null, 2) + '\n');
      await store.put('releases/stable/manifest.json', path, 'no-cache');
    }
  } finally { rmSync(temporary, { recursive: true, force: true }); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  for (const key of ['MOLR_OSS_BUCKET', 'OSS_REGION', 'OSS_ACCESS_KEY_ID', 'OSS_ACCESS_KEY_SECRET', 'MOLR_VERSION', 'GITHUB_REPOSITORY']) {
    if (!process.env[key]) throw new Error(`Missing mirror configuration: ${key}`);
  }
  const bucket = process.env.MOLR_OSS_BUCKET;
  if (!/^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$/.test(bucket)) throw new Error('Invalid OSS bucket');
  const oss = (...args) => execFileSync('ossutil', args, { encoding: 'utf8', maxBuffer: 2 << 20, timeout: 30 * 60 * 1000 });
  await publishMirror({ directory: resolve(process.env.MOLR_DIST_DIR || 'dist/releases'), repository: process.env.GITHUB_REPOSITORY, version: process.env.MOLR_VERSION, store: {
    read(key) {
      try { return oss('cat', `oss://${bucket}/${key}`); }
      catch (error) {
        if (/\bNoSuchKey\b/.test(String(error.stderr))) return null;
        throw new Error(`Cannot read OSS object: ${key}`);
      }
    },
    put(key, path, cache) {
      oss('api', 'put-object', '--bucket', bucket, '--key', key, '--body', pathToFileURL(path).href, '--cache-control', cache);
      console.log(`Mirrored ${key}`);
    },
    async verify(key, size) {
      for (let attempt = 0; attempt < 6; attempt++) {
        try {
          const response = await fetch(`${origin}/${key}`, { method: 'HEAD', redirect: 'error', signal: AbortSignal.timeout(15000) });
          if (response.ok && Number(response.headers.get('content-length')) === size) return;
        } catch { /* Retry CDN propagation and transient network failures. */ }
        if (attempt < 5) await new Promise(resolve => setTimeout(resolve, 5000));
      }
      throw new Error(`Official mirror is not reachable or has wrong size: ${key}`);
    },
  } });
}
