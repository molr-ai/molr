import assert from 'node:assert/strict';
import { test } from 'node:test';
import { mkdtempSync, rmSync, writeFileSync, mkdirSync, symlinkSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { platforms, releaseChannel, nightlyVersion, compareVersions, selectChannels, validateManifest } from './release-lib.mjs';
import { writeCatalog } from './release-catalog.mjs';
import { readCatalog } from './check-release-catalog.mjs';
import { fixture } from './release-test-fixtures.mjs';
test('stable, beta, rc and nightly have strict, correctly ordered identities', () => {
  const n = nightlyVersion('0.2.0', 'a'.repeat(40), new Date('2026-10-11T03:04:05Z'));
  assert.equal(n, '0.2.0-0.nightly.20261011.30405.gaaaaaaaaaaaa');
  const versions = [n, '0.2.0-beta.2', '0.2.0-beta.10', '0.2.0-rc.1', '0.2.0'];
  for (let i = 1; i < versions.length; i++) assert(compareVersions(versions[i - 1], versions[i]) < 0);
  assert(compareVersions('0.10.0', '0.9.0') > 0);
  for (const v of ['v0.2.0', '0.2.0-dev', '0.2.0-local', '0.2.0-rc.01', '0.2.0-alpha.1', '0.2.0+build', '0.2.0-0.nightly.20261011.030405.gaaaaaaa']) assert.throws(() => releaseChannel(v));
});
test('channels are isolated and selection cannot regress when releases finish out of order', () => {
  const stable = fixture('0.1.0'), rc = fixture('0.2.0-rc.1'), nightly = fixture('0.3.0-0.nightly.20261011.0.gaaaaaaa');
  for (const releases of [[nightly, rc, stable], [stable, rc, nightly]]) {
    const result = selectChannels(releases);
    assert.equal(result.stable.version, '0.1.0'); assert.equal(result.preview.version, rc.version); assert.equal(result.nightly.version, nightly.version);
  }
  assert.equal(selectChannels([rc, fixture('0.2.0')]).preview.version, '0.2.0');
  assert.equal(selectChannels([rc]).stable, null);
});
test('catalog keeps historical platform availability and rejects extra files, links and altered pointers', () => {
  const directory = mkdtempSync(join(tmpdir(), 'molr-catalog-'));
  try {
    const releases = [fixture('0.1.0', [platforms[0]]), fixture('0.2.0-beta.1')];
    writeCatalog(releases, directory);
    const files = readCatalog(directory);
    assert.equal(files['versions/0.1.0.json'].platforms.length, 1);
    assert.equal(files['preview/manifest.json'].platforms.length, 2);
    assert.equal(files['stable/latest-linux-arm64.json'], undefined);
    writeFileSync(join(directory, 'extra.json'), '{}'); assert.throws(() => readCatalog(directory)); rmSync(join(directory, 'extra.json'));
    symlinkSync(join(directory, 'catalog.json'), join(directory, 'link.json')); assert.throws(() => readCatalog(directory)); rmSync(join(directory, 'link.json'));
    writeFileSync(join(directory, 'stable/latest-linux-amd64.json'), '{}'); assert.throws(() => readCatalog(directory));
    assert.throws(() => writeCatalog([releases[0], releases[0]], directory), /Duplicate/);
  } finally { rmSync(directory, { recursive: true, force: true }); }
});
test('manifest rejects dangerous URLs, duplicate targets and mismatched channel/file identities', () => {
  for (const mutate of [m => m.channel = 'nightly', m => m.installer_url = 'http://example.com/install.sh', m => m.platforms.push(m.platforms[0]), m => m.platforms[0].file = '../bad', m => m.platforms[0].sha256 = 'bad', m => m.commit = 'branch']) {
    const m = fixture(); mutate(m); assert.throws(() => validateManifest(m));
  }
});

test('manifest permits extra metadata at every object level, including legacy Python requirements', () => {
  const m = structuredClone(fixture());
  m.description = 'Additional release metadata';
  m.platforms[0].extra = { example: true };
  m.platforms[0].requirements.python = '3.12';
  assert.equal(validateManifest(m), m);
  const directory = mkdtempSync(join(tmpdir(), 'molr-extra-manifest-'));
  try {
    writeCatalog([m], directory);
    assert.deepEqual(readCatalog(directory)['stable/manifest.json'], m);
  } finally { rmSync(directory, { recursive: true, force: true }); }
});
test('manifest still requires every known field and rejects invalid requirement values', () => {
  for (const target of [m => m, m => m.platforms[0], m => m.platforms[0].requirements]) {
    for (const key of Object.keys(target(fixture()))) {
      const m = structuredClone(fixture()); delete target(m)[key];
      assert.throws(() => validateManifest(m), /Missing manifest fields/);
    }
  }
  for (const value of [null, [], 'invalid']) {
    const m = structuredClone(fixture()); m.platforms[0].requirements = value;
    assert.throws(() => validateManifest(m), /Invalid manifest object/);
  }
  for (const changes of [{ systemd: 253 }, { systemd: '254' }, { cgroup: 1 }, { packages: 'bubblewrap' }, { packages: ['../invalid'] }]) {
    const m = structuredClone(fixture()); Object.assign(m.platforms[0].requirements, changes);
    assert.throws(() => validateManifest(m), /Invalid platform requirements/);
  }
});
