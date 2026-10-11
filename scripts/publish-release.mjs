import { execFileSync } from 'node:child_process';
import { readFileSync, readdirSync } from 'node:fs';
import { resolve } from 'node:path';
import { createHash } from 'node:crypto';
import { validateManifest, compareVersions } from './release-lib.mjs';
const directory = resolve(process.env.MOLR_DIST_DIR || 'dist/releases');
const m = validateManifest(JSON.parse(readFileSync(resolve(directory, 'manifest.json'))));
const tag = `v${m.version}`, repository = process.env.GITHUB_REPOSITORY;
if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository || '')) throw new Error('GitHub repository required');
const gh = (...args) => execFileSync('gh', args, { encoding: 'utf8', maxBuffer: 32 * 1024 * 1024 });
const releases = JSON.parse(gh('api', `repos/${repository}/releases?per_page=100`, '--paginate', '--slurp')).flat();
let release = releases.find(r => r.tag_name === tag);
const files = ['manifest.json', 'install.sh', 'install.sh.sha256', ...m.platforms.flatMap(p => [p.file, `${p.file}.sha256`]), ...readdirSync(directory).filter(n => /^molr_source_[0-9a-f]+\.tar\.gz(?:\.sha256)?$/.test(n))];
if (!release) {
  const args = ['release', 'create', tag, '--repo', repository, '--target', m.commit, '--title', tag, '--draft', '--generate-notes'];
  if (m.channel !== 'stable') args.push('--prerelease');
  if (m.channel !== 'nightly') args.push('--verify-tag');
  gh(...args);
  release = JSON.parse(gh('api', `repos/${repository}/releases?per_page=100`, '--paginate', '--slurp')).flat().find(r => r.tag_name === tag);
}
for (const name of files) {
  const local = readFileSync(resolve(directory, name)), old = release.assets.find(a => a.name === name);
  if (old) {
    const remote = execFileSync('gh', ['api', `repos/${repository}/releases/assets/${old.id}`, '-H', 'Accept: application/octet-stream'], { maxBuffer: 512 * 1024 * 1024 });
    const sha = data => createHash('sha256').update(data).digest('hex');
    if (sha(local) !== sha(remote)) throw new Error(`Existing asset differs: ${name}; use a new version`);
  } else {
    if (!release.draft) throw new Error(`Published release is incomplete: ${name}`);
    gh('release', 'upload', tag, resolve(directory, name), '--repo', repository);
  }
}
if (release.draft) {
  const newerStable = releases.some(r => { try { return !r.draft && !r.prerelease && compareVersions(r.tag_name.slice(1), m.version) > 0; } catch { return false; } });
  gh('release', 'edit', tag, '--repo', repository, '--draft=false', `--latest=${m.channel === 'stable' && !newerStable}`, `--prerelease=${m.channel !== 'stable'}`);
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
