import assert from 'node:assert/strict';
import { test } from 'node:test';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { checkBaseVersion, latestStableVersion, validateBaseVersion } from './check-base-version.mjs';

const metadata = version => [{ version }, { version, packages: { '': { version } } }];
const release = (tag_name, extra = {}) => ({ tag_name, draft: false, prerelease: false, ...extra });

test('the development base must advance beyond stable, allowing patch, minor or major cycles', () => {
  for (const base of ['0.1.1', '0.2.0', '1.0.0']) assert.equal(validateBaseVersion(...metadata(base), '0.1.0'), base);
  for (const base of ['0.0.9', '0.1.0']) assert.throws(() => validateBaseVersion(...metadata(base), '0.1.0'), /Update package.json.*0\.1\.1/);
  assert.equal(validateBaseVersion(...metadata('0.10.0'), '0.9.9'), '0.10.0');
  assert.equal(validateBaseVersion(...metadata('0.1.0')), '0.1.0');
  for (const base of ['v0.1.1', '0.1.1-beta.1', '0.1.1-0.nightly.20261025.0.gabcdef1', '0.1.1-local', '0.01.1', undefined]) {
    assert.throws(() => validateBaseVersion(...metadata(base)));
  }
  for (const field of ['top', 'root']) {
    const [pkg, lock] = metadata('0.1.1');
    if (field === 'top') lock.version = '0.1.0'; else delete lock.packages[''].version;
    assert.throws(() => validateBaseVersion(pkg, lock), /Synchronize/);
  }
});

test('stable selection scans past a full page of nightlies and compares SemVer rather than publication order', async t => {
  const pages = [
    Array.from({ length: 100 }, () => release('v0.11.0-0.nightly.20261025.0.gabcdef1', { prerelease: true })),
    [release('v0.9.0'), release('v0.10.0'), release('v0.8.0'), release('v9.0.0', { draft: true }),
      release('v8.0.0', { prerelease: true }), release('v7.0.0-rc.1'), release('legacy'), release('v06.0.0')],
  ];
  const calls = [];
  t.mock.method(globalThis, 'fetch', async (url, options) => {
    calls.push(url);
    assert.equal(options.headers.Authorization, 'Bearer artificial-test-token');
    assert.equal(options.redirect, 'error');
    assert(options.signal instanceof AbortSignal);
    return Response.json(pages[calls.length - 1]);
  });
  assert.equal(await latestStableVersion('example/molr', 'artificial-test-token'), '0.10.0');
  assert.deepEqual(calls, [1, 2].map(page => `https://api.github.com/repos/example/molr/releases?per_page=100&page=${page}`));
});

test('a complete empty list or only previews permits the first stable development cycle', async t => {
  for (const releases of [[], [release('v0.1.0-beta.1', { prerelease: true }), release('v0.1.0', { draft: true })]]) {
    t.mock.method(globalThis, 'fetch', async (_url, options) => {
      assert.equal(options.headers.Authorization, undefined);
      return Response.json(releases);
    });
    assert.equal(await latestStableVersion('example/molr'), null);
  }
});

test('API errors and invalid responses fail closed, including errors after a full page', async t => {
  for (const status of [401, 403, 404, 429, 500]) {
    t.mock.method(globalThis, 'fetch', async () => new Response(null, { status }));
    await assert.rejects(latestStableVersion('example/molr'), new RegExp(`HTTP ${status}`));
  }
  for (const response of [{}, [null], [{ tag_name: 'v0.1.0' }]]) {
    t.mock.method(globalThis, 'fetch', async () => Response.json(response));
    await assert.rejects(latestStableVersion('example/molr'), /Invalid GitHub release/);
  }
  t.mock.method(globalThis, 'fetch', async () => { throw new Error('network unavailable'); });
  await assert.rejects(latestStableVersion('example/molr'), /network unavailable/);
  let calls = 0;
  t.mock.method(globalThis, 'fetch', async () => ++calls === 1
    ? Response.json(Array.from({ length: 100 }, () => release('v0.1.0')))
    : new Response(null, { status: 503 }));
  await assert.rejects(latestStableVersion('example/molr'), /HTTP 503/);
  assert.equal(calls, 2);
  await assert.rejects(latestStableVersion('../example/molr'), /owner\/repository/);
  assert.equal(calls, 2);
});

test('the check reads real package files, validates them before networking, and never changes files', async t => {
  const root = mkdtempSync(join(tmpdir(), 'molr-base-version-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const save = version => {
    const [pkg, lock] = metadata(version);
    writeFileSync(join(root, 'package.json'), JSON.stringify(pkg));
    writeFileSync(join(root, 'package-lock.json'), JSON.stringify(lock));
  };
  let calls = 0;
  t.mock.method(globalThis, 'fetch', async () => { calls++; return Response.json([release('v0.1.0')]); });
  save('0.1.1');
  const contents = ['package.json', 'package-lock.json'].map(file => readFileSync(join(root, file), 'utf8'));
  assert.deepEqual(await checkBaseVersion({ root, repository: 'example/molr' }), { base: '0.1.1', latest: '0.1.0' });
  assert.deepEqual(['package.json', 'package-lock.json'].map(file => readFileSync(join(root, file), 'utf8')), contents);
  save('0.1.0');
  await assert.rejects(checkBaseVersion({ root, repository: 'example/molr' }), /greater than published stable/);
  save('0.1.1-local');
  await assert.rejects(checkBaseVersion({ root, repository: 'example/molr' }));
  assert.equal(calls, 2);
});
