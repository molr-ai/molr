import { build, Platform } from 'electron-builder';
import { execFileSync } from 'node:child_process';
import { readFileSync, existsSync, mkdirSync, readdirSync, writeFileSync, lstatSync } from 'node:fs';
import { resolve, join } from 'node:path';
import { createHash } from 'node:crypto';

const root = resolve(import.meta.dirname, '../../..');
const project = join(root, 'apps/desktop');
const version = process.env.MOLR_VERSION || '0.1.0-local';
if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(version)) throw new Error('Invalid MOLR_VERSION');
const local = version.endsWith('-local');
const commit = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
if (!local && execFileSync('git', ['status', '--porcelain'], { cwd: root, encoding: 'utf8' }).trim()) throw new Error('Release builds require a clean checkout');
const os = process.platform, arch = process.arch;
if (!['linux', 'darwin', 'win32'].includes(os) || !['x64', 'arm64'].includes(arch)) throw new Error('Build on a supported native runner');
const full = os !== 'win32';
const goArch = arch === 'x64' ? 'amd64' : 'arm64';
const run = (command, args) => execFileSync(command, args, { cwd: root, stdio: 'inherit', shell: process.platform === 'win32' });
run('npm', ['run', 'check:release']); run('npm', ['run', 'check:licenses']);
run('npm', ['run', 'build', '--workspace', '@molr/desktop']);
const packageRoot = resolve(root, process.env.MOLR_DESKTOP_RUNTIME_PACKAGE || `.cache/release/${os}-${goArch}/molr`);
if (full) {
  if (!existsSync(join(packageRoot, 'release.json'))) throw new Error('Run npm run build:release first, using the same version and commit');
  const meta = JSON.parse(readFileSync(join(packageRoot, 'release.json')));
  if (meta.os !== os || meta.arch !== goArch || meta.version !== version || meta.commit !== commit) throw new Error('Bundled Runtime identity mismatch');
  for (const name of ['bin/molr', 'bin/molr-setup', 'LICENSE', 'THIRD_PARTY_NOTICES.md', 'release.json']) {
    const stat = lstatSync(join(packageRoot, name));
    if (!stat.isFile() || (name.startsWith('bin/') && (stat.mode & 0o777) !== 0o755)) throw new Error(`Unsafe Runtime resource: ${name}`);
  }
}
if (!local && os === 'darwin' && (!process.env.CSC_LINK || !process.env.APPLE_ID || !process.env.APPLE_APP_SPECIFIC_PASSWORD || !process.env.APPLE_TEAM_ID)) throw new Error('macOS releases require Developer ID signing and notarization credentials');
if (!local && os === 'win32' && !process.env.CSC_LINK) throw new Error('Windows releases require code-signing credentials');
const output = join(root, 'dist/desktop', `${os}-${goArch}`);
mkdirSync(output, { recursive: true });
const electronRoot = resolve(root, 'node_modules/electron/dist');
for (const filename of ['LICENSE', 'LICENSES.chromium.html']) {
  if (!existsSync(join(electronRoot, filename))) throw new Error(`Electron distribution notice missing: ${filename}`);
}
const variant = full ? 'full' : 'client';
const config = {
  appId: 'ai.molr.desktop', productName: 'Molr', electronVersion: '44.8.0',
  electronDist: electronRoot,
  directories: { app: project, output, buildResources: join(root, 'packages/ui/public/brand') },
  extraMetadata: { version, homepage: 'https://molr.ai/', desktopName: 'molr.desktop' },
  files: ['dist/main/main/**', 'dist/main/preload/**', 'dist/main/shared.js', 'dist/renderer/**', ...(full ? ['dist/main/runtime/**'] : []), 'package.json'],
  extraResources: [
    { from: join(root, 'LICENSE'), to: 'LICENSE' },
    { from: join(root, 'THIRD_PARTY_NOTICES.md'), to: 'THIRD_PARTY_NOTICES.md' },
    { from: join(electronRoot, 'LICENSE'), to: 'licenses/Electron-LICENSE' },
    { from: join(electronRoot, 'LICENSES.chromium.html'), to: 'licenses/LICENSES.chromium.html' },
    ...(full ? [{ from: packageRoot, to: 'runtime', filter: ['bin/molr', 'bin/molr-setup', 'release.json', 'LICENSE', 'THIRD_PARTY_NOTICES.md'] }] : []),
  ],
  asar: true, npmRebuild: false,
  forceCodeSigning: !local && os !== 'linux',
  artifactName: `molr-desktop_${version}_${os}-${goArch}_${variant}.\${ext}`,
  linux: { target: ['deb'], executableName: 'molr-desktop', category: 'Utility', maintainer: 'Molr contributors', syncDesktopName: true, icon: join(root, 'packages/ui/public/brand/molr-icon-512.png') },
  deb: { packageName: 'molr-desktop', depends: ['libgtk-3-0', 'libnotify4', 'libnss3', 'libxss1', 'libxtst6', 'xdg-utils', 'libatspi2.0-0', 'libuuid1', 'libsecret-1-0', 'libgbm1', 'libasound2t64 | libasound2', 'libcups2t64 | libcups2', 'bubblewrap', 'busybox-static', 'util-linux', 'ca-certificates', 'systemd (>= 254)'] },
  mac: { target: ['dmg'], category: 'public.app-category.productivity', minimumSystemVersion: '13.0', hardenedRuntime: !local, notarize: !local, binaries: ['Contents/Resources/runtime/bin/molr', 'Contents/Resources/runtime/bin/molr-setup'], identity: local ? '-' : undefined, icon: join(root, 'packages/ui/public/brand/molr-icon-1024.png') },
  win: { target: ['nsis'], executableName: 'Molr', icon: join(root, 'packages/ui/public/brand/molr-icon-256.png') },
  nsis: { oneClick: false, perMachine: false, allowToChangeInstallationDirectory: true, deleteAppDataOnUninstall: false },
};
const nativePlatform = ({ linux: Platform.LINUX, darwin: Platform.MAC, win32: Platform.WINDOWS })[os];
const files = await build({ projectDir: project, config, targets: nativePlatform.createTarget(process.argv.includes('--dir') ? ['dir'] : undefined), publish: 'never' });
const artifacts = [];
for (const file of files.filter(file => /\.(deb|dmg|exe)$/.test(file))) {
  const name = file.split(/[\\/]/).pop();
  const sha256 = createHash('sha256').update(readFileSync(file)).digest('hex');
  writeFileSync(`${file}.sha256`, `${sha256}  ${name}\n`);
  artifacts.push({ file: name, sha256 });
}
writeFileSync(join(output, 'desktop-manifest.json'), JSON.stringify({ schema: 1, version, commit, os, arch: goArch, variant, signed: !local && os !== 'linux', artifacts }, null, 2) + '\n');
// Windows must contain neither the Go binaries nor their privileged adapter.
if (!full) {
  const resources = join(output, 'win-unpacked/resources');
  if (existsSync(join(resources, 'runtime'))) throw new Error('Windows client unexpectedly includes a Runtime');
}
console.log(`Desktop artifacts: ${output} (${readdirSync(output).length} entries)`);
