import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createHash } from 'node:crypto';
import { fixture, desktopFixture } from './release-test-fixtures.mjs';
import { collectDesktopRelease, validateDesktopRelease, desktopCatalog } from './desktop-release.mjs';
import { publishedDesktopCatalog, writeCatalog } from './release-catalog.mjs';
import { readCatalog } from './check-release-catalog.mjs';
import { desktopSigning } from '../apps/desktop/scripts/signing.mjs';

test('stable, beta and rc macOS builds require publisher signing credentials', () => {
  const mac = { CSC_LINK: 'fixture', APPLE_ID: 'fixture', APPLE_APP_SPECIFIC_PASSWORD: 'fixture', APPLE_TEAM_ID: 'fixture' };
  for (const version of ['0.2.0', '0.2.0-beta.1', '0.2.0-rc.1']) {
    assert.throws(() => desktopSigning('darwin', version, {}), /Incomplete/);
    const empty = { CSC_LINK: '', CSC_KEY_PASSWORD: '' };
    assert.throws(() => desktopSigning('darwin', version, empty), /Incomplete/);
    assert.equal(empty.CSC_LINK, undefined);
    assert.equal(empty.CSC_KEY_PASSWORD, undefined);
    assert.equal(desktopSigning('darwin', version, { ...mac }), true);
    for (const key of Object.keys(mac)) { const partial = { ...mac }; delete partial[key]; assert.throws(() => desktopSigning('darwin', version, partial), /Incomplete/); }
  }
});

test('all Windows release channels permit absent signing credentials but reject partial credentials', () => {
  for (const version of ['0.2.0-0.nightly.20261020.0.gaaaaaaa', '0.2.0-beta.1', '0.2.0-rc.1', '0.2.0']) {
    assert.equal(desktopSigning('win32', version, {}), false);
    const empty = { CSC_LINK: '', CSC_KEY_PASSWORD: '' };
    assert.equal(desktopSigning('win32', version, empty), false);
    assert.equal(empty.CSC_LINK, undefined);
    assert.equal(empty.CSC_KEY_PASSWORD, undefined);
    assert.equal(desktopSigning('win32', version, { CSC_LINK: 'fixture' }), true);
    assert.equal(desktopSigning('win32', version, { CSC_LINK: 'fixture', CSC_KEY_PASSWORD: 'fixture' }), true);
    assert.throws(() => desktopSigning('win32', version, { CSC_KEY_PASSWORD: 'fixture' }), /Incomplete/);
    assert.throws(() => desktopSigning('win32', version, { CSC_LINK: '', CSC_KEY_PASSWORD: 'fixture' }), /Incomplete/);
  }
});

test('macOS nightlies, local and Linux packaging disable certificate inputs even when credentials are present', () => {
  const certificates = { CSC_LINK: 'fixture', CSC_KEY_PASSWORD: 'fixture', WIN_CSC_LINK: 'fixture', WIN_CSC_KEY_PASSWORD: 'fixture' };
  for (const os of ['darwin', 'win32', 'linux']) {
    for (const version of ['0.2.0-local', ...(os !== 'win32' ? ['0.2.0-0.nightly.20261020.0.gaaaaaaa'] : []), ...(os === 'linux' ? ['0.2.0', '0.2.0-beta.1', '0.2.0-rc.1'] : [])]) {
      for (const credentials of [{}, certificates, { CSC_KEY_PASSWORD: 'fixture' }]) {
        const env = { ...credentials };
        assert.equal(desktopSigning(os, version, env), false);
        for (const key of Object.keys(certificates)) assert.equal(env[key], undefined);
      }
    }
  }
  assert.throws(() => desktopSigning('darwin', '0.2.0-unknown', {}), /Unsupported release version/);
});

test('collect native packages requires the entire matching and checksummed matrix and preserves signing status', t => {
  const dir = mkdtempSync(join(tmpdir(), 'molr-desktop-release-')); t.after(() => rmSync(dir, { recursive: true, force: true }));
  const source = join(dir, 'native'), destination = join(dir, 'release'); mkdirSync(source); mkdirSync(destination);
  const runtime = fixture(), m = desktopFixture(runtime);
  for (const p of m.platforms) { p.signed = p.os === 'darwin'; p.notarized = p.os === 'darwin' && p.arch === 'arm64'; }
  writeFileSync(join(destination, 'manifest.json'), JSON.stringify(runtime));
  for (const p of m.platforms) {
    const native = join(source, `${p.os}-${p.arch}`); mkdirSync(native);
    const bytes = Buffer.from('artificial installer ' + p.os + p.arch);
    p.sha256 = createHash('sha256').update(bytes).digest('hex');
    writeFileSync(join(native, p.file), bytes); writeFileSync(join(native, p.file + '.sha256'), `${p.sha256}  ${p.file}\n`);
    writeFileSync(join(native, 'desktop-manifest.json'), JSON.stringify({ schema: 1, version: m.version, commit: m.commit, os: p.os, arch: p.arch, variant: p.variant, signed: p.signed, notarized: p.notarized, artifacts: [{ file: p.file, sha256: p.sha256 }] }));
  }
  const collected = collectDesktopRelease(source, destination, 'example/molr');
  assert.equal(collected.platforms.length, 5);
  assert.equal(collected.platforms.find(p => p.os === 'win32').variant, 'full');
  for (const release of [collected, desktopCatalog([collected]).channels.stable]) {
    for (const p of release.platforms) {
      const expected = m.platforms.find(x => x.os === p.os && x.arch === p.arch);
      assert.equal(p.signed, expected.signed); assert.equal(p.notarized, expected.notarized);
    }
  }
  const unknownNotarization = structuredClone(m); delete unknownNotarization.platforms[0].notarized;
  assert.throws(() => validateDesktopRelease(unknownNotarization, 'example/molr', runtime));
  for (const p of m.platforms) assert.equal(createHash('sha256').update(readFileSync(join(destination, p.file))).digest('hex'), p.sha256);
  const check = changed => assert.throws(() => validateDesktopRelease(changed, 'example/molr', runtime));
  check({ ...m, platforms: m.platforms.slice(1) }); check({ ...m, commit: 'f'.repeat(40) });
  const unknown = structuredClone(m); delete unknown.platforms.find(p => p.os === 'win32').signed; check(unknown);
  const clientOnly = structuredClone(m);
  const windows = clientOnly.platforms.find(p => p.os === 'win32');
  windows.variant = 'client';
  for (const field of ['file', 'url', 'checksum_url']) windows[field] = windows[field].replace('_full.exe', '_client.exe');
  check(clientOnly);
  const unsafe = structuredClone(m); unsafe.platforms[0].file = '../private'; check(unsafe);
  writeFileSync(join(source, 'linux-amd64', m.platforms[0].file), 'corrupt');
  assert.throws(() => collectDesktopRelease(source, destination, 'example/molr'), /checksum/);
});

test('desktop channels select SemVer, allow stable in preview and keep nightly separate', () => {
  const versions = ['0.9.0', '0.10.0', '0.10.0-rc.9', '0.11.0-0.nightly.20261016.0.gaaaaaaa'];
  const c = desktopCatalog(versions.map(v => desktopFixture(fixture(v))));
  assert.equal(c.channels.stable.version, '0.10.0'); assert.equal(c.channels.preview.version, '0.10.0');
  assert.equal(c.channels.nightly.version, versions[3]);
});

test('website reads selected GitHub desktop assets, verifies their identity and emits the combined catalog', async t => {
  const runtime = fixture(), m = desktopFixture(runtime); let kind = 'valid'; const calls = [];
  t.mock.method(globalThis, 'fetch', async (url, options = {}) => {
    calls.push({ url: String(url), options });
    if (String(url).startsWith('https://api.github.com/')) {
      const assets = [{ name: 'desktop-manifest.json', browser_download_url: `https://github.com/example/molr/releases/download/v${m.version}/desktop-manifest.json` }, ...m.platforms.flatMap(p => [{ name: p.file, browser_download_url: p.url, digest: `sha256:${kind === 'digest' ? '0'.repeat(64) : p.sha256}` }, { name: p.file + '.sha256', browser_download_url: p.checksum_url }])];
      return Response.json({ tag_name: `v${m.version}`, draft: false, prerelease: false, assets: kind === 'legacy' ? [] : kind === 'missing' ? assets.slice(0, -1) : assets });
    }
    assert.equal(options.headers, undefined, 'Never forward API credentials to asset hosts');
    if (options.method === 'HEAD') return new Response(null, { status: kind === 'unavailable' ? 404 : 200 });
    assert(String(url).endsWith('/desktop-manifest.json'), 'Never download installer bodies');
    return Response.json(kind === 'identity' ? { ...m, commit: '0'.repeat(40) } : m);
  });
  const catalog = await publishedDesktopCatalog('example/molr', [runtime]);
  assert.equal(catalog.channels.stable.platforms.length, 5); assert.equal(calls.filter(c => c.options.method === 'HEAD').length, 10);
  const dir = mkdtempSync(join(tmpdir(), 'molr-combined-catalog-')); t.after(() => rmSync(dir, { recursive: true, force: true }));
  writeCatalog([runtime], dir, catalog); assert.deepEqual(readCatalog(dir)['desktop/catalog.json'], catalog);
  for (kind of ['missing', 'digest', 'identity', 'unavailable']) await assert.rejects(publishedDesktopCatalog('example/molr', [runtime]));
  kind = 'legacy'; assert.equal((await publishedDesktopCatalog('example/molr', [runtime])).channels.stable, null);
});
