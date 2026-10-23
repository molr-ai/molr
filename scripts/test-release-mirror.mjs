import assert from 'node:assert/strict';
import { test } from 'node:test';
import { mkdtempSync, readFileSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createHash } from 'node:crypto';
import yaml from 'js-yaml';
import { fixture, desktopFixture } from './release-test-fixtures.mjs';
import { publishMirror } from './publish-release-mirror.mjs';

function setup(t, version = '0.2.0') {
  const directory = mkdtempSync(join(tmpdir(), 'molr-mirror-test-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const m = fixture(version), desktop = desktopFixture(m), puts = [], verified = [], objects = new Map();
  const bytes = Buffer.from('artificial release bytes');
  const sha = createHash('sha256').update(bytes).digest('hex');
  for (const p of [...m.platforms, ...desktop.platforms]) {
    p.sha256 = sha;
    writeFileSync(join(directory, p.file), bytes);
    writeFileSync(join(directory, p.file + '.sha256'), `${sha}  ${p.file}\n`);
  }
  const source = `molr_source_${m.commit.slice(0, 12)}.tar.gz`;
  writeFileSync(join(directory, source), bytes);
  writeFileSync(join(directory, source + '.sha256'), `${sha}  ${source}\n`);
  writeFileSync(join(directory, 'manifest.json'), JSON.stringify(m));
  writeFileSync(join(directory, 'desktop-manifest.json'), JSON.stringify(desktop));
  writeFileSync(join(directory, 'private.txt'), 'must not be uploaded');
  const store = {
    async read(key) { return objects.get(key) ?? null; },
    async put(key, path, cache) { puts.push({ key, cache }); objects.set(key, readFileSync(path)); },
    async verify(key) { verified.push(key); },
  };
  const run = () => publishMirror({ directory, repository: 'example/molr', version, store });
  return { m, directory, puts, verified, objects, store, run };
}

test('stable mirror uploads allowlisted assets before pointers and supports retries and versioned downloads', async t => {
  const f = setup(t);
  await f.run();
  assert(f.verified.length > 10);
  assert(f.puts.every(p => !p.key.includes('private') && !p.key.includes('/preview/') && !p.key.includes('/nightly/')));
  const prefix = 'releases/download/v0.2.0/';
  const firstMetadata = f.puts.findIndex(p => !p.key.startsWith(prefix));
  assert.equal(firstMetadata, f.verified.length);
  assert.equal(f.puts.at(-1).key, 'releases/stable/manifest.json');
  assert.equal(f.puts.at(-1).cache, 'no-cache');
  const m = JSON.parse(f.objects.get('releases/stable/manifest.json'));
  assert(m.platforms.every(p => p.url === `https://cdn.molr.ai/${prefix}${p.file}`));
  const descriptor = f.objects.get('releases/versions/0.2.0/download-linux-amd64.txt').toString();
  assert(descriptor.includes(m.platforms[0].sha256));
  assert(descriptor.includes('https://cdn.molr.ai/'));
  assert(f.objects.has('install.sh'));
  const before = new Map(f.objects);
  await f.run();
  assert.deepEqual(f.objects, before);
});

test('mirror refuses prereleases and corrupt assets before any remote writes', async t => {
  for (const version of ['0.2.0-rc.1', '0.2.0-0.nightly.20261011.0.gaaaaaaa', '0.2.0']) {
    const f = setup(t, version);
    if (version === '0.2.0') writeFileSync(join(f.directory, f.m.platforms[0].file), 'corrupt');
    await assert.rejects(f.run);
    assert.equal(f.puts.length, 0);
  }
});

test('failed CDN verification cannot publish pointers; an older retry cannot regress latest', async t => {
  const f = setup(t);
  f.store.verify = async () => { throw new Error('CDN unavailable'); };
  await assert.rejects(f.run, /CDN unavailable/);
  assert(f.puts.every(p => p.key.startsWith('releases/download/')));
  f.store.verify = async () => {};
  const newer = Buffer.from(JSON.stringify(fixture('0.3.0')));
  f.objects.set('releases/stable/manifest.json', newer);
  f.puts.length = 0;
  await f.run();
  assert(f.puts.every(p => p.key.startsWith('releases/download/') || p.key.startsWith('releases/versions/')));
  assert.deepEqual(f.objects.get('releases/stable/manifest.json'), newer);
});

test('release workflow mirrors stable only, includes existing releases on retry, and keeps credentials in environment', () => {
  const workflow = yaml.load(readFileSync(new URL('../.github/workflows/release-build.yml', import.meta.url), 'utf8'));
  const job = workflow.jobs.mirror;
  assert.deepEqual(job.needs, ['prepare', 'publish']);
  assert.match(job.if, /channel == 'stable'/);
  assert.match(job.if, /existing == 'true'/);
  assert.match(job.if, /needs.publish.result == 'success'/);
  assert.equal(job.environment, 'release');
  assert.equal(job.concurrency.group, 'release-mirror');
  assert(job.steps.some(s => s.run?.includes('sha256sum --check')));
  const upload = job.steps.find(s => s.run?.includes('publish-release-mirror.mjs'));
  assert.equal(upload.env.OSS_ACCESS_KEY_SECRET, '${{ secrets.MOLR_OSS_ACCESS_KEY_SECRET }}');
  assert(!upload.run.includes('secrets.'));
});
