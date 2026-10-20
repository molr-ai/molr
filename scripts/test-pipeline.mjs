import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, rmSync, symlinkSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createHash } from 'node:crypto';
import yaml from 'js-yaml';
import { ciJob, ciStep, ciTitle, ensureCI, matchingCIRuns } from './ensure-ci.mjs';
import { recordBuildAssets, verifyBuildAssets } from './build-assets.mjs';
import { validatePackageManifest } from './test-desktop-package.mjs';
import { platforms, nativeBuildMatrix } from './release-lib.mjs';

const repository = 'example/molr', commit = 'a'.repeat(40);
const run = (extra = {}) => ({
  id: 10, run_attempt: 2, display_title: ciTitle(commit), head_sha: commit,
  head_repository: { full_name: repository }, head_branch: 'main', event: 'push',
  status: 'completed', conclusion: 'success', html_url: 'https://github.com/example/molr/actions/runs/10', ...extra,
});
const passedJob = (sha = commit) => ({ name: ciJob(), status: 'completed', conclusion: 'success',
  steps: [{ name: ciStep(sha), status: 'completed', conclusion: 'success' }] });
function fakeAPI(runs, options = {}) {
  const calls = [];
  const api = async (path, request) => {
    calls.push({ path, request });
    if (request?.method === 'POST') { options.dispatch?.(); return {}; }
    if (path.includes('/jobs?')) return { jobs: options.jobs || [passedJob()] };
    return { workflow_runs: runs.filter(r => path.includes('event=push') ? r.event === 'push' : r.event === 'workflow_dispatch') };
  };
  return { api, calls };
}

test('release reuses exact complete CI and inspects the latest attempt without dispatching', async () => {
  const { api, calls } = fakeAPI([run()]);
  const result = await ensureCI({ repository, commit, api, log() {} });
  assert.equal(result.run_id, 10); assert.equal(result.run_attempt, 2);
  assert(calls.some(c => c.path.includes('/runs/10/attempts/2/jobs')));
  assert(!calls.some(c => c.request));
});
test('a newer failure or cancellation cannot be hidden by an older successful run', async () => {
  for (const conclusion of ['failure', 'cancelled', 'timed_out', 'skipped']) {
    const { api, calls } = fakeAPI([run(), run({ id: 11, conclusion })]);
    await assert.rejects(ensureCI({ repository, commit, api }), /did not pass/);
    assert(!calls.some(c => c.request));
  }
});
test('repository, branch, event, source SHA and validation policy distinguish reusable runs', () => {
  for (const extra of [
    { head_repository: { full_name: 'fork/molr' } }, { head_branch: 'feature' },
    { event: 'pull_request' }, { head_sha: 'b'.repeat(40) }, { display_title: 'CI v1 ' + commit },
  ]) assert.equal(matchingCIRuns([run(extra)], repository, commit).length, 0);
});
test('dispatched validation uses the reported checked-out SHA rather than the workflow head SHA', async () => {
  const { api } = fakeAPI([run({ event: 'workflow_dispatch', head_sha: 'b'.repeat(40) })]);
  assert.equal((await ensureCI({ repository, commit, api, log() {} })).run_id, 10);
  const wrong = fakeAPI([run({ event: 'workflow_dispatch' })], { jobs: [passedJob('b'.repeat(40))] });
  await assert.rejects(ensureCI({ repository, commit, api: wrong.api }), /lacks complete/);
});
test('absent CI is dispatched once; in-progress CI is awaited and not duplicated', async () => {
  for (const missing of [false, true]) {
    let elapsed = 0;
    const runs = missing ? [] : [run({ status: 'in_progress', conclusion: null })];
    const { api, calls } = fakeAPI(runs, { dispatch: () => runs.push(run({ event: 'workflow_dispatch', status: 'queued', conclusion: null })) });
    await ensureCI({ repository, commit, api, log() {}, now: () => elapsed, pollMs: 1, pause: async ms => {
      elapsed += ms; runs[0].status = 'completed'; runs[0].conclusion = 'success';
    } });
    assert.equal(calls.filter(c => c.request).length, Number(missing));
    if (missing) assert.deepEqual(calls.find(c => c.request).request.body, { ref: 'main', inputs: { commit } });
  }
});
test('API failure, incomplete validation and timeout never authorize release', async () => {
  await assert.rejects(ensureCI({ repository, commit, api: async () => { throw new Error('HTTP 403'); } }), /403/);
  for (const jobs of [[], [{ name: ciJob(commit), status: 'completed', conclusion: 'skipped' }]]) {
    await assert.rejects(ensureCI({ repository, commit, api: fakeAPI([run()], { jobs }).api }), /lacks complete/);
  }
  const { api, calls } = fakeAPI([run({ status: 'in_progress' })]);
  await assert.rejects(ensureCI({ repository, commit, api, maxWaitMs: 0, log() {} }), /Timed out/);
  assert(!calls.some(c => c.request));
});

function assets(t) {
  const root = mkdtempSync(join(tmpdir(), 'molr-assets-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  for (const path of ['apps/webui/dist/index.html', 'apps/desktop/dist/renderer/index.html', 'apps/desktop/dist/main/main/index.js', 'package-lock.json']) {
    mkdirSync(join(root, path, '..'), { recursive: true }); writeFileSync(join(root, path), 'artificial ' + path);
  }
  recordBuildAssets(root, '0.2.0', commit);
  return root;
}
test('shared assets require identical version, source, lockfile and complete output inventory', t => {
  const root = assets(t);
  assert.equal(verifyBuildAssets(root, '0.2.0', commit).schema, 1);
  assert.throws(() => verifyBuildAssets(root, '0.2.0-rc.1', commit), /identity/);
  assert.throws(() => verifyBuildAssets(root, '0.2.0', 'b'.repeat(40)), /identity/);
  const extra = join(root, 'apps/webui/dist/stale.js'); writeFileSync(extra, 'stale');
  assert.throws(() => verifyBuildAssets(root, '0.2.0', commit), /inventory/); rmSync(extra);
  writeFileSync(join(root, 'apps/webui/dist/index.html'), 'corrupt');
  assert.throws(() => verifyBuildAssets(root, '0.2.0', commit), /checksum/);
  writeFileSync(join(root, 'package-lock.json'), '{}');
  assert.throws(() => verifyBuildAssets(root, '0.2.0', commit), /identity/);
});
test('shared assets reject incomplete builds and symlinks', t => {
  const root = assets(t), entry = join(root, 'apps/webui/dist/index.html');
  rmSync(entry); assert.throws(() => recordBuildAssets(root, '0.2.0', commit), /Incomplete/);
  symlinkSync(join(root, 'package-lock.json'), entry);
  assert.throws(() => recordBuildAssets(root, '0.2.0', commit), /regular files/);
});
test('installer acceptance rejects wrong identities, paths and altered final bytes before extraction', t => {
  const root = mkdtempSync(join(tmpdir(), 'molr-installer-')); t.after(() => rmSync(root, { recursive: true, force: true }));
  const expected = { version: '0.2.0', commit, os: 'linux', arch: 'amd64' };
  const file = 'molr-desktop_0.2.0_linux-amd64_full.deb', bytes = Buffer.from('artificial installer');
  const sha256 = createHash('sha256').update(bytes).digest('hex');
  const manifest = { schema: 1, ...expected, variant: 'full', signed: false, notarized: false, artifacts: [{ file, sha256 }] };
  const save = () => writeFileSync(join(root, 'desktop-manifest.json'), JSON.stringify(manifest));
  save(); writeFileSync(join(root, file), bytes); writeFileSync(join(root, file + '.sha256'), `${sha256}  ${file}\n`);
  validatePackageManifest(root, expected);
  assert.throws(() => validatePackageManifest(root, { ...expected, version: '0.3.0' }), /mismatch/);
  manifest.artifacts[0].file = '../bad'; save(); assert.throws(() => validatePackageManifest(root, expected));
  manifest.artifacts[0].file = file; save(); writeFileSync(join(root, file), 'corrupt');
  assert.throws(() => validatePackageManifest(root, expected));
});
test('CI includes every native target, both browser platforms, and a non-skippable final gate', () => {
  const read = name => yaml.load(readFileSync(new URL(`../.github/workflows/${name}.yml`, import.meta.url), 'utf8'));
  const ci = read('ci'), native = read('native-build'), release = read('release-build'), desktop = read('release-desktop');
  assert(ci.on.pull_request !== undefined); assert(ci.on.workflow_dispatch !== undefined);
  assert.deepEqual(ci.on.push.branches, ['main']);
  assert.equal(ci.on.pull_request?.paths, undefined);
  assert.deepEqual(ci.jobs.browser.strategy.matrix.runner, ['ubuntu-24.04', 'macos-15']);
  for (const wf of [native, desktop]) {
    assert.equal(wf.jobs[wf === native ? 'native' : 'desktop'].strategy.matrix, '${{ fromJSON(inputs.matrix) }}');
    assert.deepEqual(nativeBuildMatrix().include.map(p => `${p.os}-${p.arch}`).sort(),
      [...platforms.map(p => p.id), 'win32-amd64'].sort());
  }
  assert.equal(ci.jobs.native.with.matrix, '${{ needs.prepare.outputs.matrix }}');
  assert.equal(release.jobs.native.with.matrix, release.jobs.desktop.with.matrix);
  assert.deepEqual(ci.jobs.passed.needs.sort(), ['prepare', 'source', 'assets', 'native', 'browser', 'installer'].sort());
  assert.equal(ci.jobs.passed.name, ciJob());
  assert.equal(ci.jobs.passed.steps[0].name, ciStep('${{ needs.prepare.outputs.commit }}'));
  assert(release.jobs.assets.needs.includes('ci'));
  assert(release.jobs.publish.needs.includes('ci'));
  assert(read('release').jobs.website.if.includes("'stable'"));
  assert.equal(read('nightly').jobs.website, undefined);
});
