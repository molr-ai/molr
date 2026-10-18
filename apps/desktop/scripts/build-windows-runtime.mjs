// Build the native Runtime payload used by the Windows desktop installer.
// Windows Runtime is shipped through the desktop channel, not install.sh.
import { execFileSync } from 'node:child_process';
import { cpSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { resolve, join } from 'node:path';
const root = resolve(import.meta.dirname, '../../..');
const version = process.env.MOLR_VERSION || '0.1.0-local';
const arch = process.env.MOLR_ARCH || ({ x64: 'amd64', arm64: 'arm64' }[process.arch]);
if (!/^\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?$/.test(version) || !['amd64', 'arm64'].includes(arch)) throw new Error('Invalid Windows build identity');
const run = (command, args, options = {}) => execFileSync(command, args, { cwd: root, stdio: 'inherit', ...options });
const npm = process.platform === 'win32' ? 'npm.cmd' : 'npm';
const npmRun = (args, env) => run(npm, args, { shell: process.platform === 'win32', env: env || process.env });
const commit = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
if (!version.endsWith('-local') && execFileSync('git', ['status', '--porcelain'], { cwd: root, encoding: 'utf8' }).trim()) throw new Error('Release builds require a clean checkout');
npmRun(['run', 'check:release']); npmRun(['run', 'check:licenses']); npmRun(['run', 'docs:check']);
npmRun(['run', 'build:webui'], { ...process.env, MOLR_VERSION: version, MOLR_COMMIT: commit });
const embedded = join(root, 'runtime/internal/webassets/dist');
rmSync(embedded, { recursive: true, force: true }); cpSync(join(root, 'apps/webui/dist'), embedded, { recursive: true });
const stage = join(root, `.cache/release/win32-${arch}/molr`);
rmSync(stage, { recursive: true, force: true }); mkdirSync(join(stage, 'bin'), { recursive: true });
for (const file of ['LICENSE', 'THIRD_PARTY_NOTICES.md']) cpSync(join(root, file), join(stage, file));
const epoch = execFileSync('git', ['show', '-s', '--format=%ct', commit], { cwd: root, encoding: 'utf8' }).trim();
const builtAt = new Date(Number(epoch) * 1000).toISOString();
const go = process.env.GO || 'go';
for (const name of ['molr', 'molr-setup']) {
  const binary = join(stage, 'bin', `${name}.exe`);
  run(go, ['build', '-trimpath', ...(name === 'molr' ? ['-tags', 'webui'] : []), '-ldflags', `${name === 'molr' ? '-H windowsgui ' : ''}-X molr.ai/runtime/internal/buildinfo.Version=${version} -X molr.ai/runtime/internal/buildinfo.Commit=${commit} -X molr.ai/runtime/internal/buildinfo.BuiltAt=${builtAt}`, '-o', binary, `./cmd/${name}`], { cwd: join(root, 'runtime'), env: { ...process.env, CGO_ENABLED: '0', GOOS: 'windows', GOARCH: arch, GOTOOLCHAIN: `go${readFileSync(join(root, 'runtime/.go-version'), 'utf8').trim()}`, GOCACHE: process.env.GOCACHE || join(root, '.cache/go-build') } });
  const data = readFileSync(binary), pe = data.readUInt32LE(0x3c);
  if (data.toString('ascii', 0, 2) !== 'MZ' || data.toString('ascii', pe, pe + 4) !== 'PE\0\0' || data.readUInt16LE(pe + 4) !== ({ amd64: 0x8664, arm64: 0xaa64 }[arch])) throw new Error('Invalid native Windows PE binary');
  if (name === 'molr' && data.readUInt16LE(pe + 24 + 68) !== 2) throw new Error('Desktop Runtime must use the Windows GUI subsystem to avoid a console window');
  const identity = execFileSync(go, ['version', '-m', binary], { encoding: 'utf8' });
  if (!identity.includes('CGO_ENABLED=0') || !identity.includes('GOOS=windows') || !identity.includes(`GOARCH=${arch}`) || !identity.includes(`vcs.revision=${commit}`) || (!version.endsWith('-local') && !identity.includes('vcs.modified=false'))) throw new Error('Invalid Windows Runtime build identity');
}
writeFileSync(join(stage, 'release.json'), JSON.stringify({ version, commit, builtAt, os: 'windows', arch }, null, 2) + '\n');
console.log(`Windows Runtime payload: ${stage}`);
