import { execFileSync } from 'node:child_process';
import { readFileSync, readdirSync } from 'node:fs';
import { resolve } from 'node:path';
import { createHash } from 'node:crypto';
import { validateDesktopRelease, desktopReleaseFiles } from './desktop-release.mjs';
import { validateManifest, compareVersions } from './release-lib.mjs';
const directory = resolve(process.env.MOLR_DIST_DIR || 'dist/releases');
const m = validateManifest(JSON.parse(readFileSync(resolve(directory, 'manifest.json'))));
const tag = `v${m.version}`, repository = process.env.GITHUB_REPOSITORY;
if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository || '')) throw new Error('GitHub repository required');
const desktop = validateDesktopRelease(JSON.parse(readFileSync(resolve(directory, 'desktop-manifest.json'))), repository, m);
const desktopFiles = desktopReleaseFiles(desktop, directory);
const signingNotes = [
  desktop.platforms.some(p => p.os === 'win32' && !p.signed) ? 'Windows 桌面包未签名，系统可能提示未知发布者或阻止运行。 / Windows desktop installers are unsigned; device security policies may warn or block execution.' : '',
  desktop.platforms.some(p => p.os === 'darwin' && !p.signed) ? 'macOS 桌面包仅使用 ad-hoc 签名，未经过 Apple 公证，系统可能阻止首次打开。 / macOS desktop apps are ad-hoc signed without Apple notarization; the first launch may be blocked.' : '',
  desktop.platforms.some(p => p.os === 'darwin' && p.signed && !p.notarized) ? '部分 macOS 桌面包已使用 Developer ID 签名，但尚未经过 Apple 公证，系统可能阻止首次打开。 / Some macOS desktop installers are Developer ID signed but not notarized by Apple; the first launch may be blocked.' : '',
].filter(Boolean).join('\n\n');
const gh = (...args) => execFileSync('gh', args, { encoding: 'utf8', maxBuffer: 32 * 1024 * 1024 });
const mutateRelease = (method, path, body) => JSON.parse(execFileSync('gh', ['api', path, '--method', method, '--input', '-'], { encoding: 'utf8', input: JSON.stringify(body) }));
const releases = JSON.parse(gh('api', `repos/${repository}/releases?per_page=100`, '--paginate', '--slurp')).flat();
let release = releases.find(r => r.tag_name === tag);
const files = ['manifest.json', ...desktopFiles, ...m.platforms.flatMap(p => [p.file, `${p.file}.sha256`]), ...readdirSync(directory).filter(n => /^molr_source_[0-9a-f]+\.tar\.gz(?:\.sha256)?$/.test(n))];
if (!release) {
  if (m.channel !== 'nightly') gh('api', `repos/${repository}/git/ref/tags/${tag}`);
  // Use the creation response: a new draft may not appear in the list yet.
  release = mutateRelease('POST', `repos/${repository}/releases`, {
    tag_name: tag, target_commitish: m.commit, name: tag, draft: true,
    prerelease: m.channel !== 'stable', generate_release_notes: true, make_latest: 'false',
    ...(signingNotes ? { body: signingNotes } : {}),
  });
}
if (!Number.isSafeInteger(release.id) || release.id < 1 || release.tag_name !== tag || !Array.isArray(release.assets)) throw new Error(`Invalid release response: ${tag}`);
for (const name of files) {
  const local = readFileSync(resolve(directory, name)), old = release.assets.find(a => a.name === name);
  if (old) {
    const remote = execFileSync('gh', ['api', `repos/${repository}/releases/assets/${old.id}`, '-H', 'Accept: application/octet-stream'], { maxBuffer: 512 * 1024 * 1024 });
    const sha = data => createHash('sha256').update(data).digest('hex');
    if (sha(local) !== sha(remote)) throw new Error(`Existing asset differs: ${name}; use a new version`);
  } else {
    if (!release.draft) throw new Error(`Published release is incomplete: ${name}`);
    gh('api', `https://uploads.github.com/repos/${repository}/releases/${release.id}/assets?name=${encodeURIComponent(name)}`, '--method', 'POST', '-H', 'Content-Type: application/octet-stream', '--input', resolve(directory, name));
  }
}
if (release.draft) {
  const newerStable = releases.some(r => { try { return !r.draft && !r.prerelease && compareVersions(r.tag_name.slice(1), m.version) > 0; } catch { return false; } });
  mutateRelease('PATCH', `repos/${repository}/releases/${release.id}`, { draft: false, make_latest: String(m.channel === 'stable' && !newerStable), prerelease: m.channel !== 'stable' });
}
for (const name of files) {
  const url = `https://github.com/${repository}/releases/download/${tag}/${name}`;
  let response;
  for (let i = 0; i < 6; i++) {
    response = await fetch(url, { method: 'HEAD', signal: AbortSignal.timeout(30000) });
    if (response.ok) break;
    await new Promise(r => setTimeout(r, 5000));
  }
  if (!response.ok) throw new Error(`Published asset is not downloadable: ${name}`);
}
console.log(`Published ${tag}; all public assets are reachable`);
