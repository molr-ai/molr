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

test('desktop signing allows absent credentials but rejects partial configuration', () => {
  for (const os of ['darwin', 'win32']) assert.equal(desktopSigning(os, false, {}), false);
  for (const local of [true, false]) {
    const empty = { CSC_LINK: '', CSC_KEY_PASSWORD: '', APPLE_ID: '', APPLE_APP_SPECIFIC_PASSWORD: '', APPLE_TEAM_ID: '' };
    assert.equal(desktopSigning('darwin', local, empty), false);
    assert.equal(empty.CSC_LINK, undefined, 'electron-builder must not receive an empty certificate path');
    assert.equal(empty.CSC_KEY_PASSWORD, undefined);
  }
  const mac = { CSC_LINK: 'fixture', APPLE_ID: 'fixture', APPLE_APP_SPECIFIC_PASSWORD: 'fixture', APPLE_TEAM_ID: 'fixture' };
  assert.equal(desktopSigning('darwin', false, mac), true);
  for (const key of Object.keys(mac)) { const partial = { ...mac }; delete partial[key]; assert.throws(() => desktopSigning('darwin', false, partial), /Incomplete/); }
  assert.equal(desktopSigning('win32', false, { CSC_LINK: 'fixture' }), true);
  assert.throws(() => desktopSigning('win32', false, { CSC_KEY_PASSWORD: 'fixture' }), /Incomplete/);
  assert.equal(desktopSigning('darwin', true, mac), false);
  assert.equal(desktopSigning('linux', false, mac), false);
});

test('collect native packages requires the entire matching and checksummed matrix and preserves signing status', t => {
  const dir = mkdtempSync(join(tmpdir(), 'molr-desktop-release-')); t.after(() => rmSync(dir, { recursive: true, force: true }));
  const source = join(dir, 'native'), destination = join(dir, 'release'); mkdirSync(source); mkdirSync(destination);
  const runtime = fixture(), m = desktopFixture(runtime);
  for (const p of m.platforms) p.signed = false;
  writeFileSync(join(destination, 'manifest.json'), JSON.stringify(runtime));
  for (const p of m.platforms) {
    const native = join(source, `${p.os}-${p.arch}`); mkdirSync(native);
    const bytes = Buffer.from('artificial installer ' + p.os + p.arch);
    p.sha256 = createHash('sha256').update(bytes).digest('hex');
    writeFileSync(join(native, p.file), bytes); writeFileSync(join(native, p.file + '.sha256'), `${p.sha256}  ${p.file}\n`);
    writeFileSync(join(native, 'desktop-manifest.json'), JSON.stringify({ schema: 1, version: m.version, commit: m.commit, os: p.os, arch: p.arch, variant: p.variant, signed: p.signed, artifacts: [{ file: p.file, sha256: p.sha256 }] }));
  }
  const collected = collectDesktopRelease(source, destination, 'example/molr');
  assert.equal(collected.platforms.length, 5);
  assert.equal(collected.platforms.find(p => p.os === 'win32').variant, 'full');
  assert(collected.platforms.every(p => p.signed === false));
  assert(desktopCatalog([collected]).channels.stable.platforms.every(p => p.signed === false));
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
