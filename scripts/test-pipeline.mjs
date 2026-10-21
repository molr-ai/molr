import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, rmSync, symlinkSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import yaml from 'js-yaml';
import { ciJob, ciStep, ciTitle, ensureCI, matchingCIRuns } from './ensure-ci.mjs';
import { recordBuildAssets, verifyBuildAssets } from './build-assets.mjs';
import { validatePackageManifest } from './test-desktop-package.mjs';
import { platforms, nativeBuildMatrix } from './release-lib.mjs';
import { giteaCIPlan } from './gitea-ci-plan.mjs';

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

test('Gitea validates selected native packages from shared assets and enforces the planned gate', () => {
  const ci = yaml.load(readFileSync(new URL('../.gitea/workflows/ci.yml', import.meta.url), 'utf8'));
  assert(ci.on.pull_request !== undefined);
  assert.equal(ci.on.pull_request?.paths, undefined);
  assert(ci.on.workflow_dispatch !== undefined);
  assert.deepEqual(ci.on.push.branches, ['main']);
  const identity = ci.jobs.source.steps.find(step => step.id === 'identity');
  assert(identity.run.includes('node scripts/gitea-ci-plan.mjs'));
  assert.equal(identity.env.MOLR_CI_PLATFORMS, "${{ github.event.inputs.platforms || vars.MOLR_CI_PLATFORMS || 'all' }}");
  assert.equal(identity.env.MOLR_CI_INSTALLERS, "${{ github.event.inputs.installers || vars.MOLR_CI_INSTALLERS || 'enabled' }}");
  for (const key of ['platforms', 'installers']) assert.equal(ci.on.workflow_dispatch.inputs[key].default, '');
  const plan = giteaCIPlan();
  const { matrix } = plan;
  assert.deepEqual(matrix.include.map(p => `${p.os}-${p.arch}`).sort(), nativeBuildMatrix().include.map(p => `${p.os}-${p.arch}`).sort());
  for (const target of matrix.include) {
    assert.equal(target.runner, `molr-${({ linux: 'linux', darwin: 'macos', win32: 'windows' })[target.os]}-${target.arch}`);
    assert.equal(target.shell, target.os === 'win32' ? 'pwsh' : 'bash');
  }
  assert.equal(ci.jobs.native.strategy.matrix, '${{ fromJSON(needs.source.outputs.matrix) }}');
  assert.equal(ci.jobs.native.env.MOLR_BUILD_ASSETS, '1');
  assert.deepEqual(ci.jobs.native.needs, ['source', 'assets']);
  const nativeSteps = ci.jobs.native.steps;
  const unpack = nativeSteps.findIndex(step => step.run?.includes('build-assets.mjs --verify'));
  const build = nativeSteps.findIndex(step => step.run === 'npm run build:release');
  const packageIndex = nativeSteps.findIndex(step => step.run === 'npm run package:desktop');
  assert(unpack >= 0 && unpack < build && build < packageIndex);
  const evaluations = nativeSteps.filter(step => step.run?.includes('scripts/test-desktop-package.mjs'));
  assert.equal(evaluations.length, 2);
  for (const step of evaluations) assert(nativeSteps.indexOf(step) > packageIndex);
  assert.equal(ci.jobs.browser.strategy.matrix, '${{ fromJSON(needs.source.outputs.browser_matrix) }}');
  assert.deepEqual(plan.browser_matrix.include.map(p => p.runner), ['molr-linux-amd64', 'molr-macos-arm64']);
  assert.deepEqual(ci.jobs.installer.needs, ['source', 'native']);
  assert.equal(ci.jobs.installer.strategy.matrix, '${{ fromJSON(needs.source.outputs.installer_matrix) }}');
  assert.deepEqual(plan.installer_matrix.include.map(p => `${p.os}-${p.arch}`).sort(), platforms.map(p => p.id).sort());
  for (const group of ['browser', 'installer']) {
    assert.equal(ci.jobs[group].if, `\u0024{{ needs.source.outputs.${group}_enabled == 'true' }}`);
  }
  for (const key of Object.keys(plan)) assert.equal(ci.jobs.source.outputs[key], `\u0024{{ steps.identity.outputs.${key} }}`);
  const upload = nativeSteps.find(step => step.uses?.includes('upload-artifact') && step.with.name.startsWith('ci-runtime-'));
  const download = ci.jobs.installer.steps.find(step => step.uses?.includes('download-artifact'));
  assert.equal(upload.with.name, download.with.name);
  const groups = ['source', 'assets', 'native', 'browser', 'installer'];
  assert.deepEqual(ci.jobs.passed.needs, groups);
  assert.equal(ci.jobs.passed.if, '${{ always() }}');
  const gate = ci.jobs.passed.steps[0].run.trim().match(/^node -e '([^']+)'$/)[1];
  const execute = results => {
    const result = spawnSync(process.execPath, ['-e', gate], {
      encoding: 'utf8', env: { ...process.env, RESULTS: JSON.stringify(results) },
    });
    assert.ifError(result.error);
    return result;
  };
  const results = Object.fromEntries(groups.map(name => [name, { result: 'success' }]));
  results.source.outputs = plan;
  assert.equal(execute(results).status, 0);
  for (const name of groups) for (const result of ['failure', 'cancelled', 'skipped']) {
    const failed = execute({ ...results, [name]: { result } });
    assert.equal(failed.status, 1);
    assert(failed.stderr.includes(`${name}: ${result}`));
  }
  for (const selection of [
    { platforms: 'linux-amd64', installers: 'disabled' },
    { platforms: 'windows-amd64' },
    { platforms: 'linux-arm64' },
  ]) {
    const selectedPlan = giteaCIPlan(selection);
    const selectedResults = structuredClone(results);
    selectedResults.source.outputs = selectedPlan;
    for (const name of ['browser', 'installer']) {
      if (selectedPlan[name + '_enabled'] === 'false') selectedResults[name].result = 'skipped';
    }
    assert.equal(execute(selectedResults).status, 0);
    for (const name of groups) for (const result of ['success', 'failure', 'cancelled', 'skipped']) {
      if (result === selectedResults[name].result) continue;
      assert.equal(execute({ ...selectedResults, [name]: { ...selectedResults[name], result } }).status, 1);
    }
    for (const name of groups) {
      const missing = structuredClone(selectedResults);
      delete missing[name];
      assert.equal(execute(missing).status, 1);
    }
  }
  for (const value of [undefined, '', 'unexpected']) {
    const incomplete = structuredClone(results);
    incomplete.source.outputs.installer_enabled = value;
    assert.equal(execute(incomplete).status, 1);
  }
});

test('Gitea selection filters every platform combination and never creates extra selected jobs', () => {
  const ids = ['linux-amd64', 'linux-arm64', 'macos-amd64', 'macos-arm64', 'windows-amd64'];
  for (let mask = 1; mask < 1 << ids.length; mask++) {
    const selected = ids.filter((_, i) => mask & (1 << i));
    for (const installers of ['enabled', 'disabled']) {
      const plan = giteaCIPlan({ platforms: selected.join(', '), installers });
      assert.deepEqual(plan.matrix.include.map(p => p.runner).sort(), selected.map(id => 'molr-' + id).sort());
      const browser = selected.filter(id => ['linux-amd64', 'macos-arm64'].includes(id));
      const install = installers === 'enabled' ? selected.filter(id => id !== 'windows-amd64') : [];
      assert.equal(plan.browser_enabled, String(browser.length > 0));
      assert.equal(plan.installer_enabled, String(install.length > 0));
      if (browser.length) assert.deepEqual(plan.browser_matrix.include.map(p => p.runner).sort(), browser.map(id => 'molr-' + id).sort());
      if (install.length) assert.deepEqual(plan.installer_matrix.include.map(p => p.runner).sort(), install.map(id => 'molr-install-' + id).sort());
      assert(plan.browser_matrix.include.length > 0);
      assert(plan.installer_matrix.include.length > 0);
    }
  }
  const linux = giteaCIPlan({ platforms: 'linux-amd64', installers: 'disabled' });
  assert.equal(linux.scope, 'native=molr-linux-amd64; browser=molr-linux-amd64; installer=none');
  for (const platforms of ['linux', 'all,linux-amd64', 'linux-amd64,', 'linux-amd64,linux-amd64', 'linux-amd64\nwindows-amd64', 'unknown']) {
    assert.throws(() => giteaCIPlan({ platforms }), /Invalid Gitea CI platforms/);
  }
  assert.throws(() => giteaCIPlan({ installers: 'false' }), /enabled or disabled/);
});

test('Gitea planner writes outputs for a single Linux runner and rejects invalid input before writing', t => {
  const dir = mkdtempSync(join(tmpdir(), 'molr-gitea-plan-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const output = join(dir, 'output');
  const env = { ...process.env, GITHUB_OUTPUT: output, MOLR_CI_INSTALLERS: 'disabled' };
  delete env.NODE_TEST_CONTEXT;
  const execute = platforms => spawnSync(process.execPath, ['scripts/gitea-ci-plan.mjs'], {
    cwd: fileURLToPath(new URL('../', import.meta.url)), encoding: 'utf8',
    env: { ...env, MOLR_CI_PLATFORMS: platforms },
  });
  const result = execute('linux-amd64');
  assert.ifError(result.error);
  assert.equal(result.status, 0, result.stderr);
  const contents = readFileSync(output, 'utf8');
  const outputs = Object.fromEntries(contents.trim().split('\n').map(line => [line.slice(0, line.indexOf('=')), line.slice(line.indexOf('=') + 1)]));
  assert.deepEqual(JSON.parse(outputs.matrix).include.map(p => p.runner), ['molr-linux-amd64']);
  assert.equal(outputs.installer_enabled, 'false');
  assert(result.stdout.includes('installer=none'));
  assert.notEqual(execute('linux-typo').status, 0);
  assert.equal(readFileSync(output, 'utf8'), contents);
});
