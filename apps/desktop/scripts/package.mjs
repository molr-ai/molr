import { build, Platform } from 'electron-builder';
import { execFileSync } from 'node:child_process';
import { readFileSync, existsSync, mkdirSync, readdirSync, lstatSync } from 'node:fs';
import { resolve, join } from 'node:path';
import { createHash } from 'node:crypto';
import { desktopSigning } from './signing.mjs';
import { prepareNotarization, finishNotarization, writeManifest } from './notarization.mjs';
import { useBuildAssets } from '../../../scripts/build-assets.mjs';

const root = resolve(import.meta.dirname, '../../..');
const project = join(root, 'apps/desktop');
const version = process.env.MOLR_VERSION || '0.1.0-local';
if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(version)) throw new Error('Invalid MOLR_VERSION');
const local = version.endsWith('-local');
const commit = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
if (!local && execFileSync('git', ['status', '--porcelain'], { cwd: root, encoding: 'utf8' }).trim()) throw new Error('Release builds require a clean checkout');
const os = process.platform, arch = process.arch;
const deferNotarization = process.argv.includes('--defer-notarization');
if (deferNotarization && (os !== 'darwin' || process.argv.includes('--dir'))) throw new Error('Deferred notarization requires a native macOS DMG build');
if (!['linux', 'darwin', 'win32'].includes(os) || !['x64', 'arm64'].includes(arch)) throw new Error('Build on a supported native runner');
const runtimeOS = os === 'win32' ? 'windows' : os;
const runtimeFiles = os === 'win32' ? ['bin/molr.exe', 'bin/molr-setup.exe'] : ['bin/molr', 'bin/molr-setup'];
const goArch = arch === 'x64' ? 'amd64' : 'arm64';
const run = (command, args) => execFileSync(command, args, { cwd: root, stdio: 'inherit', shell: process.platform === 'win32' });
run('npm', ['run', 'check:release']); run('npm', ['run', 'check:licenses']);
if (!useBuildAssets(root, version, commit)) run('npm', ['run', 'build', '--workspace', '@molr/desktop']);
const packageRoot = resolve(root, process.env.MOLR_DESKTOP_RUNTIME_PACKAGE || `.cache/release/${os}-${goArch}/molr`);
{
  if (!existsSync(join(packageRoot, 'release.json'))) throw new Error('Build the matching native Runtime payload first (build:release on Unix, build-windows-runtime.mjs on Windows)');
  const meta = JSON.parse(readFileSync(join(packageRoot, 'release.json')));
  if (meta.os !== runtimeOS || meta.arch !== goArch || meta.version !== version || meta.commit !== commit) throw new Error('Bundled Runtime identity mismatch');
  for (const name of [...runtimeFiles, 'LICENSE', 'THIRD_PARTY_NOTICES.md', 'release.json']) {
    const stat = lstatSync(join(packageRoot, name));
    if (!stat.isFile() || (os !== 'win32' && name.startsWith('bin/') && (stat.mode & 0o777) !== 0o755)) throw new Error(`Unsafe Runtime resource: ${name}`);
  }
}
const signed = desktopSigning(os, version);
if (!signed && os !== 'linux') console.log(os === 'darwin' ? 'Packaging without Developer ID or notarization (ad-hoc signing).' : 'Packaging without Windows code signing.');
const output = join(root, 'dist/desktop', `${os}-${goArch}`);
mkdirSync(output, { recursive: true });
const electronRoot = resolve(root, 'node_modules/electron/dist');
for (const filename of ['LICENSE', 'LICENSES.chromium.html']) {
  if (!existsSync(join(electronRoot, filename))) throw new Error(`Electron distribution notice missing: ${filename}`);
}
const variant = 'full';
const config = {
  appId: 'ai.molr.desktop', productName: 'Molr', electronVersion: '44.8.0',
  electronDist: electronRoot,
  directories: { app: project, output, buildResources: join(root, 'packages/ui/public/brand') },
  extraMetadata: { version, molrCommit: commit, homepage: 'https://molr.ai/', desktopName: 'molr.desktop' },
  files: ['dist/main/main/**', 'dist/main/preload/**', 'dist/main/shared.js', 'dist/main/version.js', 'dist/main/runtime-version.js', 'dist/renderer/**', 'dist/main/runtime/**', 'package.json'],
  extraResources: [
    { from: join(root, 'LICENSE'), to: 'LICENSE' },
    { from: join(root, 'THIRD_PARTY_NOTICES.md'), to: 'THIRD_PARTY_NOTICES.md' },
    { from: join(electronRoot, 'LICENSE'), to: 'licenses/Electron-LICENSE' },
    { from: join(electronRoot, 'LICENSES.chromium.html'), to: 'licenses/LICENSES.chromium.html' },
    { from: packageRoot, to: 'runtime', filter: [...runtimeFiles, 'release.json', 'LICENSE', 'THIRD_PARTY_NOTICES.md'] },
  ],
  asar: true, npmRebuild: false,
  forceCodeSigning: signed,
  artifactName: `molr-desktop_${version}_${os}-${goArch}_${variant}.\${ext}`,
  linux: { target: ['deb'], executableName: 'molr-desktop', category: 'Utility', maintainer: 'Molr contributors', syncDesktopName: true, icon: join(root, 'packages/ui/public/brand/molr-icon-512.png') },
  deb: { packageName: 'molr-desktop', depends: ['libgtk-3-0', 'libnotify4', 'libnss3', 'libxss1', 'libxtst6', 'xdg-utils', 'libatspi2.0-0', 'libuuid1', 'libsecret-1-0', 'libgbm1', 'libasound2t64 | libasound2', 'libcups2t64 | libcups2', 'bubblewrap', 'busybox-static', 'util-linux', 'ca-certificates', 'systemd (>= 254)'] },
  mac: { extendInfo: { CFBundleLocalizations: ['en', 'zh_CN', 'zh_TW'] }, target: ['dmg'], category: 'public.app-category.productivity', minimumSystemVersion: '13.0', hardenedRuntime: signed, notarize: false, binaries: ['Contents/Resources/runtime/bin/molr', 'Contents/Resources/runtime/bin/molr-setup'], identity: signed ? undefined : '-', icon: join(root, 'packages/ui/public/brand/molr-icon-1024.png') },
  dmg: { sign: signed },
  win: { target: ['nsis'], executableName: 'Molr', signExecutable: signed, icon: join(root, 'packages/ui/public/brand/molr-icon-256.png') },
  nsis: { oneClick: false, perMachine: false, allowToChangeInstallationDirectory: true, deleteAppDataOnUninstall: false },
};
const nativePlatform = ({ linux: Platform.LINUX, darwin: Platform.MAC, win32: Platform.WINDOWS })[os];
const files = await build({ projectDir: project, config, targets: nativePlatform.createTarget(process.argv.includes('--dir') ? ['dir'] : undefined), publish: 'never' });
const artifacts = [];
for (const file of files.filter(file => /\.(deb|dmg|exe)$/.test(file))) {
  const name = file.split(/[\\/]/).pop();
  const sha256 = createHash('sha256').update(readFileSync(file)).digest('hex');
  artifacts.push({ file: name, sha256 });
}
const manifest = { schema: 1, version, commit, os, arch: goArch, variant, signed, notarized: false, artifacts };
if (os === 'darwin' && signed && !process.argv.includes('--dir')) {
  prepareNotarization(output, manifest);
  if (!deferNotarization) await finishNotarization(output, manifest);
}
else writeManifest(output, manifest);
console.log(`Desktop artifacts: ${output} (${readdirSync(output).length} entries)`);
