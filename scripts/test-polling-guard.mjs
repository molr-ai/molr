import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { scanTypeScript, signature, checkPolicy, collectSites, repository } from './check-polling.mjs';

function policy(sites) { return { version: 1, files: [{ file: 'apps/demo/src/run.ts', reason: 'External protocol requires a heartbeat.', budget: 'One tick per second while connected; cancel on close.', sites: sites.map(signature) }] }; }
const path = 'apps/demo/src/run.ts';
test('recognizes direct, qualified, aliased, recursive, and promise timer entry points', () => {
  for (const source of [
    'setInterval(check, 100)', 'window.setInterval(check, 100)', 'globalThis["setInterval"](check, 100)',
    'const later = setTimeout; later(check, 100)',
    'import { setTimeout as wait } from "node:timers/promises"; await wait(100)',
    'function loop(){setTimeout(loop, 100)}', 'function loop(){setTimeout(loop)}',
    'const {setTimeout: later}=window;later(check, 100)',
  ]) assert(scanTypeScript(path, source).length > 0, source);
  assert.equal(scanTypeScript(path, 'const [timeout,setTimeout]=useState(20);setTimeout(30)').length, 0);
  assert.equal(scanTypeScript(path, '// setInterval(check, 100)\nconst text="setTimeout"').length, 0);
});
test('poll wrappers, cadence callbacks, and query overrides are reviewed', () => {
  for (const source of [
    'import { poll as watch } from "./polling";watch(async()=>100)',
    'import { useQuery as query } from "./state";query("/api/tasks",false,100)',
    'import { useQueryValue } from "./state";useQueryValue("/api/tasks",select,false,()=>100)',
    'queries.subscribe(key, listener, 100)', 'loop.reschedule(100)',
  ]) assert(scanTypeScript(path, source).length > 0, source);
  assert.equal(scanTypeScript(path, 'import { useQuery } from "./state";useQuery("/api/tasks",false,0)').length, 0);
});
test('faster intervals, added sites, removed sites, and expired allowances fail closed', () => {
  const original = scanTypeScript(path, 'setInterval(check,1000)');
  const reviewed = policy(original);
  assert.deepEqual(checkPolicy(original, reviewed), []);
  for (const source of ['setInterval(check,100)', 'setInterval(check,1000);setTimeout(check,100)', '']) {
    assert(checkPolicy(scanTypeScript(path, source), reviewed).length > 0, source);
  }
  assert(checkPolicy(original, { ...reviewed, files: [...reviewed.files, ...reviewed.files] }).some(x => x.includes('Duplicate')));
  assert(checkPolicy(original, { ...reviewed, files: [{ ...reviewed.files[0], budget: '' }] }).length > 0);
});
test('renaming a local cadence does not hide its value and whitespace does not change the policy', () => {
  const original = scanTypeScript(path, 'import {useQuery} from "./state";function Panel(){const delay=1000;useQuery(path,false,delay)}');
  assert.equal(original[0].delay, '1000');
  assert.deepEqual(original.map(signature), scanTypeScript(path, 'import { useQuery } from "./state"; function Panel() { const wait = 1000; useQuery(path, false, wait); }').map(signature));
  assert.notDeepEqual(original.map(signature), scanTypeScript(path, 'import {useQuery} from "./state";function Panel(){const delay=100;useQuery(path,false,delay)}').map(signature));
});
test('recursive scan includes new untracked production files and excludes test fixtures', () => {
  const root = mkdtempSync(join(tmpdir(), 'molr-polling-'));
  try {
    for (const dir of ['runtime/internal/new', 'apps/new/src/nested']) mkdirSync(join(root, dir), { recursive: true });
    writeFileSync(join(root, 'runtime/internal/new/poll.go'), 'package p;import t "time";func run(){for{t.Sleep(100*t.Millisecond)}}');
    writeFileSync(join(root, 'runtime/internal/new/poll_test.go'), 'package p;import "time";func TestFixture(){time.Sleep(time.Second)}');
    writeFileSync(join(root, 'apps/new/src/nested/poll.ts'), 'setInterval(check,100)');
    writeFileSync(join(root, 'apps/new/src/nested/poll.spec.ts'), 'setInterval(check,100)');
    const sites = collectSites(root);
    assert.equal(sites.length, 2);
    assert.equal(checkPolicy(sites, { version: 1, files: [] }, root).length, 2);
  } finally { rmSync(root, { recursive: true, force: true }); }
});
test('both CI providers enforce the guard and idle behavior suite', () => {
  const pkg = JSON.parse(readFileSync(join(repository, 'package.json')));
  assert(pkg.scripts.check.includes('check:polling'));
  for (const provider of ['.github', '.gitea']) {
    const workflow = readFileSync(join(repository, provider, 'workflows/ci.yml'), 'utf8');
    assert(workflow.includes('npm run check'));
    assert(workflow.includes('npm run test:polling'));
  }
});

test('global constants, promise timer delays, and retry policy changes remain visible', () => {
  assert.equal(scanTypeScript(path, 'const cadence=100;function run(){setInterval(check,cadence)}')[0].delay, '100');
  assert.equal(scanTypeScript(path, 'import {setTimeout as wait} from "node:timers/promises";await wait(100)')[0].delay, '100');
  assert.equal(scanTypeScript(path, 'import * as timers from "node:timers/promises";await timers.setTimeout(50)')[0].delay, '50');
  assert.notDeepEqual(scanTypeScript('apps/webui/src/polling.ts', 'export function retryDelay(){return 2000}').map(signature), scanTypeScript('apps/webui/src/polling.ts', 'export function retryDelay(){return 100}').map(signature));
});

test('an omitted interval is visible as a zero-delay timer', () => {
  assert.equal(scanTypeScript(path, 'function loop(){setTimeout(loop)}')[0].delay, '0');
});

test('production event capabilities cannot be waived by adding an inventory entry', () => {
  const site = { file: path, scope: 'run', api: 'wiring.realtime.NewFeed', delay: 'missing:WatchAuthorization', loops: 0, line: 1 };
  assert(checkPolicy([site], policy([site])).some(message => message.includes('Production event capability required')));
});
