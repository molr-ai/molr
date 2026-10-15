import { execFileSync } from 'node:child_process';
import { existsSync, cpSync, mkdirSync, readFileSync, rmSync, writeFileSync, chmodSync, renameSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { platforms, releaseChannel } from './release-lib.mjs';
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
process.chdir(root);
const arch = process.env.MOLR_ARCH || ({ x64: 'amd64', arm64: 'arm64' }[process.arch]);
const os = process.env.MOLR_OS || process.platform;
const platform = platforms.find(p => p.os === os && p.arch === arch);
if (!platform) throw new Error('Unsupported MOLR_OS/MOLR_ARCH');
const version = process.env.MOLR_VERSION || '0.1.0-local';
const local = /^[0-9]+\.[0-9]+\.[0-9]+-local$/.test(version);
if (!local && !platform.channels.includes(releaseChannel(version))) throw new Error('Platform is unavailable in this channel');
if (!local && execFileSync('git', ['status', '--porcelain'], { encoding: 'utf8' }).trim()) throw new Error('Release builds require a clean checkout');
const output = resolve(process.env.MOLR_DIST_DIR || 'dist/releases');
const stage = resolve('.cache/release', `${os}-${arch}`, 'molr');
if (!version.includes('-local') && existsSync(`${output}/molr_${version}_${os}_${arch}.tar.gz`)) throw new Error('Release versions are immutable; choose a new version');
const run = (cmd, args, options = {}) => execFileSync(cmd, args, { stdio: 'inherit', ...options });
run('npm', ['run', 'check:release']);
run('npm', ['run', 'check:licenses']);
run('npm', ['run', 'docs:check']);
const commit = execFileSync('git', ['rev-parse', 'HEAD'], { encoding: 'utf8' }).trim();
run('npm', ['run', 'build:webui'], { env: { ...process.env, MOLR_VERSION: version, MOLR_COMMIT: commit } });
const embedded = resolve('runtime/internal/webassets/dist');
rmSync(embedded, { recursive: true, force: true });
cpSync('apps/webui/dist', embedded, { recursive: true });
rmSync(stage, { recursive: true, force: true });
mkdirSync(`${stage}/bin`, { recursive: true });
for (const name of ['LICENSE', 'THIRD_PARTY_NOTICES.md']) cpSync(`${root}/${name}`, `${stage}/${name}`);
const go = process.env.GO || 'go';
const epoch = execFileSync('git', ['show', '-s', '--format=%ct', commit], { encoding: 'utf8' }).trim();
const buildTime = new Date(Number(epoch) * 1000).toISOString();
for (const name of ['molr', 'molr-setup']) {
  run(go, ['build', '-trimpath', ...(name === 'molr' ? ['-tags', 'webui'] : []), '-ldflags', `-X molr.ai/runtime/internal/buildinfo.Version=${version} -X molr.ai/runtime/internal/buildinfo.Commit=${commit} -X molr.ai/runtime/internal/buildinfo.BuiltAt=${buildTime}`, '-o', `${stage}/bin/${name}`, `./cmd/${name}`], { cwd: resolve('runtime'), env: { ...process.env, CGO_ENABLED: '0', GOOS: os, GOARCH: arch, GOTOOLCHAIN: `go${readFileSync('runtime/.go-version', 'utf8').trim()}`, GOCACHE: process.env.GOCACHE || resolve('.cache/go-build'), GOMODCACHE: process.env.GOMODCACHE || resolve('.cache/go-mod') } });
  const binary = `${stage}/bin/${name}`;
  chmodSync(binary, 0o755);
  const description = execFileSync('file', [binary], { encoding: 'utf8' });
  if (os === 'linux' && !description.includes('statically linked')) throw new Error('Linux Runtime must be statically linked');
  if (os === 'darwin') {
    if (process.platform !== 'darwin' || !description.includes('Mach-O') || !description.includes(arch === 'arm64' ? 'arm64' : 'x86_64')) throw new Error('macOS releases require native Mach-O verification');
    run('/usr/bin/codesign', ['--force', '--sign', '-', binary]);
    run('/usr/bin/codesign', ['--verify', '--strict', binary]);
  }
  const buildInfo = execFileSync(go, ['version', '-m', binary], { encoding: 'utf8' });
  if (!buildInfo.includes('CGO_ENABLED=0') || !buildInfo.includes(`GOARCH=${arch}`) || !buildInfo.includes(`GOOS=${os}`) || !buildInfo.includes(`vcs.revision=${commit}`) || (!local && !buildInfo.includes('vcs.modified=false'))) throw new Error('Invalid Runtime build identity');
}
writeFileSync(`${stage}/release.json`, JSON.stringify({ version, commit, builtAt: buildTime, os, arch }, null, 2));
mkdirSync(output, { recursive: true });
const filename = `molr_${version}_${os}_${arch}.tar.gz`;
const releaseFiles = JSON.parse(readFileSync('scripts/release-policy.json', 'utf8')).releaseFiles;
const temporaryArchive = `${output}/${filename}.tmp`;
try {
  run('python3', ['scripts/package-release.py', stage, temporaryArchive, epoch, ...releaseFiles]);
  run('python3', ['scripts/release_safety.py', 'release', temporaryArchive]);
  renameSync(temporaryArchive, `${output}/${filename}`);
} finally {
  rmSync(temporaryArchive, { force: true });
}
const sha256 = createHash('sha256').update(readFileSync(`${output}/${filename}`)).digest('hex');
writeFileSync(`${output}/${filename}.sha256`, `${sha256}  ${filename}\n`);

console.log(`Release: ${output}/${filename}`);
