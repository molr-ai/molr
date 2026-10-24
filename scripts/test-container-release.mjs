import test from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import yaml from 'js-yaml';
import { identity, plan, publish, registry } from './container-release.mjs';

const record = value => { const bytes = Buffer.from(JSON.stringify(value)); return { value, bytes, digest: 'sha256:' + createHash('sha256').update(bytes).digest('hex') }; };
const id = version => identity('example/molr', version, 'a'.repeat(40), '2026-10-24T00:00:00Z');
function fixture() {
  const values = new Map(), writes = [];
  const api = async (path, body) => {
    if (body !== undefined) { writes.push(path); values.set(path, record(JSON.parse(body))); return; }
    if (path.startsWith('tags/list?')) return record({ tags: [...values.keys()].filter(p => p.startsWith('manifests/') && !p.includes('sha256:')).map(p => p.slice(10)).sort() });
    return values.get(path) || null;
  };
  function build(identity) {
    const digests = {};
    for (const arch of ['amd64', 'arm64']) {
      const config = record({ os: 'linux', architecture: arch, config: { Labels: {
        'org.opencontainers.image.version': identity.version, 'org.opencontainers.image.revision': identity.commit,
        'org.opencontainers.image.source': `https://github.com/${identity.repository}`,
      } } });
      const manifest = record({ schemaVersion: 2, mediaType: 'application/vnd.oci.image.manifest.v1+json', config: { digest: config.digest }, layers: [] });
      values.set(`blobs/${config.digest}`, config); values.set(`manifests/${manifest.digest}`, manifest); digests[arch] = { digest: manifest.digest, config_digest: config.digest };
    }
    return digests;
  }
  return { values, writes, api, build };
}
test('stable versions are immutable, latest cannot regress, and retries repair missing aliases', async () => {
  const f = fixture(), newer = id('0.2.0'), older = id('0.1.0');
  const first = await publish(f.api, newer, f.build(newer));
  assert.deepEqual(first.tags, ['0.2.0', 'latest']);
  await publish(f.api, older, f.build(older));
  assert.equal(f.values.get('manifests/latest').digest, first.digest);
  const count = f.writes.length;
  assert.equal((await plan(f.api, newer)).build, false);
  assert.equal((await publish(f.api, newer)).digest, first.digest);
  assert.equal(f.writes.length, count);
  await assert.rejects(plan(f.api, { ...newer, commit: 'b'.repeat(40) }), /identity mismatch/);
  f.values.delete('manifests/latest');
  await publish(f.api, older);
  assert.equal(f.values.get('manifests/latest').digest, first.digest);
});
test('preview never creates latest; nightly publishes only its moving tag and resists old retries', async () => {
  const f = fixture(), preview = id('0.2.0-beta.1');
  await publish(f.api, preview, f.build(preview));
  assert.deepEqual(f.writes, ['manifests/0.2.0-beta.1']);
  const older = id('0.3.0-0.nightly.20261023.0.gaaaaaaa'), newer = id('0.3.0-0.nightly.20261024.0.gaaaaaaa');
  await publish(f.api, older, f.build(older));
  await publish(f.api, newer, f.build(newer));
  const count = f.writes.length;
  assert.equal((await plan(f.api, older)).build, false);
  assert((await publish(f.api, older)).skipped);
  assert.equal(f.writes.length, count);
  assert(f.writes.every(path => !path.includes('0.nightly')));
  assert.equal(f.values.get('manifests/nightly').value.annotations['org.opencontainers.image.version'], newer.version);
});
test('incomplete, substituted and mismatched platform images never receive a public tag', async () => {
  for (const mutate of [
    (f, d) => delete d.arm64,
    (f, d) => d.arm64 = d.amd64,
    (f, d) => d.arm64.config_digest = 'sha256:' + 'e'.repeat(64),
    (f, d) => f.values.get(`manifests/${d.arm64.digest}`).digest = 'sha256:' + 'f'.repeat(64),
    (f, d) => { const m = f.values.get(`manifests/${d.arm64.digest}`); f.values.get(`blobs/${m.value.config.digest}`).value.config.Labels['org.opencontainers.image.revision'] = 'b'.repeat(40); },
  ]) {
    const f = fixture(), build = f.build(id('0.2.0')); mutate(f, build);
    await assert.rejects(publish(f.api, id('0.2.0'), build)); assert.deepEqual(f.writes, []);
  }
});
test('failed alias publication retries without rebuilding or overwriting the fixed version', async () => {
  const f = fixture(), stable = id('0.2.0');
  const fail = async (path, body) => { if (path === 'manifests/latest' && body) throw new Error('HTTP 503'); return f.api(path, body); };
  await assert.rejects(publish(fail, stable, f.build(stable)), /503/);
  const fixed = f.values.get('manifests/0.2.0').digest;
  await publish(f.api, stable);
  assert.equal(f.values.get('manifests/latest').digest, fixed);
  assert.equal(f.writes.filter(p => p === 'manifests/0.2.0').length, 1);
});
test('registry failures do not masquerade as absent tags or print credentials', async () => {
  for (const status of [401, 403, 429, 500]) {
    const api = await registry('example/molr', { username: 'fixture', password: 'artificial', fetcher: async url => url.includes('/token?') ? new Response('{"token":"artificial-pull-token"}') : new Response('', { status }) });
    await assert.rejects(api('manifests/latest'), new RegExp(String(status)));
  }
  const api = await registry('example/molr', { fetcher: async url => url.includes('/token?') ? new Response('{"token":"fixture"}') : new Response('', { status: 404 }) });
  assert.equal(await api('manifests/latest'), null);
});
test('container publication preserves release gates, permissions, native verification and retries', () => {
  const read = name => yaml.load(readFileSync(new URL(`../.github/workflows/${name}.yml`, import.meta.url), 'utf8'));
  for (const name of ['release', 'nightly', 'release-build', 'release-container']) assert.equal(read(name).permissions.packages, 'write');
  const caller = read('release-build').jobs.container;
  assert.deepEqual(caller.needs, ['prepare', 'publish']); assert(caller.if.includes("existing == 'true'"));
  const wf = read('release-container');
  assert.equal(wf.concurrency['cancel-in-progress'], false);
  assert.deepEqual(wf.jobs.build.strategy.matrix.include.map(p => p.arch), ['amd64', 'arm64']);
  const steps = wf.jobs.build.steps;
  const testIndex = steps.findIndex(step => step.run?.includes('test-container.py'));
  assert(testIndex > 0 && testIndex < steps.findIndex(step => step.id === 'push'));
  assert.equal(steps.find(step => step.id === 'push').with.target, 'default');
  assert(steps.find(step => step.id === 'push').with.outputs.includes('push-by-digest=true'));
  assert(wf.jobs.publish.if.includes("needs.build.result == 'success'"));
});
