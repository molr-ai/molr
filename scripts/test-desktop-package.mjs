// Validate the actual installer payload, launch the packaged app, then exercise
// its bundled Runtime in an isolated instance. No publisher credentials needed.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { existsSync, lstatSync, mkdtempSync, readFileSync, readdirSync, rmSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

export function validatePackageManifest(directory, expected) {
  const manifest = JSON.parse(readFileSync(join(directory, 'desktop-manifest.json')));
  for (const key of ['version', 'commit', 'os', 'arch']) assert.equal(manifest[key], expected[key], `Installer ${key} mismatch`);
  assert.equal(manifest.schema, 1);
  assert.equal(manifest.variant, 'full');
  assert.equal(typeof manifest.signed, 'boolean');
  assert.equal(typeof manifest.notarized, 'boolean');
  assert.equal(manifest.artifacts?.length, 1);
  const artifact = manifest.artifacts[0];
  const extension = { linux: 'deb', darwin: 'dmg', win32: 'exe' }[manifest.os];
  assert(extension, 'Unsupported installer OS');
  const filename = `molr-desktop_${manifest.version}_${manifest.os}-${manifest.arch}_full.${extension}`;
  assert.equal(artifact.file, filename);
  const file = join(directory, filename);
  assert(lstatSync(file).isFile(), 'Installer must be a regular file');
  assert.equal(createHash('sha256').update(readFileSync(file)).digest('hex'), artifact.sha256);
  assert.equal(readFileSync(file + '.sha256', 'utf8'), `${artifact.sha256}  ${filename}\n`);
  return { manifest, file };
}

async function main() {
  const root = resolve(import.meta.dirname, '..'), os = process.platform;
  if (os !== 'linux' && process.env.GITHUB_ACTIONS !== 'true' && process.env.GITEA_ACTIONS !== 'true') {
    throw new Error('Run macOS/Windows installer acceptance on a disposable CI runner');
  }
  const arch = process.arch === 'arm64' ? 'arm64' : 'amd64';
  const directory = resolve(process.argv[2] || `dist/desktop/${os}-${arch}`);
  const version = process.env.MOLR_VERSION || '0.1.0-local';
  const commit = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
  const { manifest, file } = validatePackageManifest(directory, { os, arch, version, commit });
  const temporary = mkdtempSync(join(tmpdir(), 'molr-package-'));
  const run = (command, args, env = process.env) => execFileSync(command, args, { env, stdio: 'inherit', timeout: 180_000 });
  const authenticode = target => run('powershell.exe', ['-NoProfile', '-NonInteractive', '-Command',
    'if ((Get-AuthenticodeSignature -LiteralPath $env:MOLR_VERIFY_FILE).Status -ne "Valid") { throw "Invalid Authenticode signature" }'],
    { ...process.env, MOLR_VERIFY_FILE: target });
  let executable, resources, app, mounted = false, uninstaller;
  const mount = join(temporary, 'mount');
  try {
    if (os === 'linux') {
      const extracted = join(temporary, 'extracted');
      run('dpkg-deb', ['--extract', file, extracted]);
      const application = join(extracted, 'opt', 'Molr');
      executable = join(application, 'molr-desktop');
      resources = join(application, 'resources');
    } else if (os === 'darwin') {
      run('hdiutil', ['verify', file]);
      mkdirSync(mount);
      run('hdiutil', ['attach', '-readonly', '-nobrowse', '-mountpoint', mount, file]);
      mounted = true;
      const bundles = readdirSync(mount).filter(name => name.endsWith('.app'));
      assert.equal(bundles.length, 1);
      const application = join(temporary, 'Molr.app');
      run('ditto', [join(mount, bundles[0]), application]);
      run('hdiutil', ['detach', mount]); mounted = false;
      run('codesign', ['--verify', '--deep', '--strict', application]);
      executable = join(application, 'Contents/MacOS/Molr');
      resources = join(application, 'Contents/Resources');
    } else {
      if (manifest.signed) authenticode(file);
      const application = join(temporary, 'installed');
      run(file, ['/S', '/currentuser', `/D=${application}`]);
      executable = join(application, 'Molr.exe');
      resources = join(application, 'resources');
      uninstaller = join(application, 'Uninstall Molr.exe');
      if (manifest.signed) {
        for (const target of [executable, join(resources, 'runtime/bin/molr.exe'), join(resources, 'runtime/bin/molr-setup.exe')]) authenticode(target);
      }
    }
    assert(existsSync(executable), 'Packaged executable missing');
    for (const name of ['LICENSE', 'THIRD_PARTY_NOTICES.md', 'licenses/Electron-LICENSE', 'licenses/LICENSES.chromium.html']) {
      assert(lstatSync(join(resources, name)).isFile(), `Packaged notice missing: ${name}`);
    }
    const payload = join(resources, 'runtime');
    const identity = JSON.parse(readFileSync(join(payload, 'release.json')));
    assert.equal(identity.version, version); assert.equal(identity.commit, commit);
    assert.equal(identity.arch, arch); assert.equal(identity.os, os === 'win32' ? 'windows' : os);
    const { _electron: electron, expect } = await import('@playwright/test');
    // Packaged apps intentionally ignore development data overrides. Linux
    // honors XDG paths; macOS/Windows use the disposable runner's user profile.
    const env = { ...process.env, XDG_CONFIG_HOME: join(temporary, 'config'),
      XDG_DATA_HOME: join(temporary, 'data'), XDG_CACHE_HOME: join(temporary, 'cache') };
    delete env.ELECTRON_RUN_AS_NODE; delete env.MOLR_DESKTOP_DEV_URL; delete env.MOLR_DESKTOP_RUNTIME;
    const args = process.env.MOLR_DESKTOP_TEST_HEADLESS === '1' ? ['--ozone-platform=headless'] : [];
    app = await electron.launch({ executablePath: executable, args, env, chromiumSandbox: true, timeout: 60_000 });
    assert.equal(await app.evaluate(() => process.argv.includes('--no-sandbox')), false);
    if (os === 'linux') assert((await app.evaluate(({ app }) => app.getPath('userData'))).startsWith(temporary + '/'));
    const page = await app.firstWindow();
    await expect(page.getByRole('heading', { name: '你想在哪里使用 Molr？' })).toBeVisible({ timeout: 30_000 });
    await app.close(); app = undefined;
    run(process.execPath, [join(root, 'apps/desktop/tests/local-runtime.mjs')], { ...process.env, MOLR_DESKTOP_RUNTIME_PACKAGE: payload });
    console.log(`Packaged app and bundled Runtime passed: ${manifest.os}-${arch} ${version}`);
  } finally {
    await app?.close();
    if (mounted) run('hdiutil', ['detach', mount]);
    if (uninstaller && existsSync(uninstaller)) {
      // NSIS _?= avoids the uninstaller copying itself to another temp directory.
      run(uninstaller, ['/S', `_?=${dirname(uninstaller)}`]);
    }
    rmSync(temporary, { recursive: true, force: true, maxRetries: 10, retryDelay: 500 });
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) await main();
