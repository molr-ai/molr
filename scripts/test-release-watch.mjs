import assert from 'node:assert/strict';
import { test } from 'node:test';
import { setTimeout as sleep } from 'node:timers/promises';
import { createGitHubAPI, parseWatchArgs, QueryError, watchRelease } from './watch-release.mjs';

const repository = 'example/molr', run = '123';
const state = (status = 'in_progress', conclusion = null, run_attempt = 1) => ({ id: 123, repository: { full_name: repository }, status, conclusion, run_attempt });
const job = (name, status = 'in_progress', conclusion = null) => ({ id: 456, name, status, conclusion, steps: [] });
const jobs = (...jobs) => ({ total_count: jobs.length, jobs });
function harness(respond) {
  let time = 0, active = 0;
  const calls = [], waits = [], logs = [];
  const options = {
    repository, run, now: () => time, log: line => logs.push(line),
    api: async (path, options) => {
      assert.equal(active++, 0, 'queries must never overlap');
      calls.push({ path, time, options });
      try { return await respond(path, calls.length, () => time, ms => { time += ms; }); }
      finally { active--; }
    },
    pause: async ms => {
      assert.equal(active, 0, 'no pending query while idle');
      waits.push(ms);
      const count = calls.length;
      time += ms - 1;
      await Promise.resolve();
      assert.equal(calls.length, count, 'idle time must not cause extra reads');
      time++;
    },
  };
  return { options, calls, waits, logs, now: () => time };
}

test('selects an exact run ID or URL and rejects ambiguous targets before querying', () => {
  assert.deepEqual(parseWatchArgs(['123']), { repository: 'molr-ai/molr', run: '123' });
  assert.deepEqual(parseWatchArgs(['https://github.com/example/molr/actions/runs/123']), { repository, run });
  assert.deepEqual(parseWatchArgs(['123', '--repo', repository]), { repository, run });
  assert.deepEqual(parseWatchArgs(['123'], repository), { repository, run });
  for (const args of [[], ['0'], ['-1'], ['123', '456'], ['123', '--repo'], ['123', '--repo', '../x'],
    ['https://other.invalid/example/molr/actions/runs/123'], ['https://github.com/example/molr/actions/runs/123', '--repo', 'other/molr']]) {
    assert.throws(() => parseWatchArgs(args), /Usage/);
  }
});

test('idle observation stays within two GETs per minute and 240 GETs in two hours', async () => {
  const h = harness(path => path.includes('/jobs?') ? jobs(job('signing')) : state());
  assert.equal(await watchRelease(h.options), 2);
  assert.equal(h.calls.length, 240);
  assert.equal(h.now(), 7_200_000);
  assert.deepEqual(h.calls.filter((_, i) => i % 2 === 0).map(call => call.time), Array.from({ length: 120 }, (_, i) => i * 60_000));
  assert(h.calls.every(call => call.options.timeout <= 45_000));
  assert.equal(h.logs.filter(line => line.startsWith('Attempt')).length, 1, 'unchanged progress is not repeated');
});

test('waits for the whole workflow including mirror and website after publication', async () => {
  const h = harness((path, call) => path.includes('/jobs?')
    ? jobs(job('publish', 'completed', 'success'), job('website', call < 4 ? 'in_progress' : 'completed', call < 4 ? null : 'success'))
    : call < 3 ? state() : state('completed', 'success'));
  assert.equal(await watchRelease(h.options), 0);
  assert.equal(h.calls.length, 4);
  assert.deepEqual(h.waits, [60_000]);
  assert(h.logs.some(line => line.includes('website: in_progress')));
});

test('completed failure includes job links and stops without another query or timer', async () => {
  const h = harness(path => path.includes('/jobs?') ? jobs(job('notarize', 'completed', 'failure')) : state('completed', 'failure'));
  assert.equal(await watchRelease(h.options), 1);
  assert.equal(h.calls.length, 2);
  assert.deepEqual(h.waits, []);
  assert(h.logs.some(line => line.includes('https://github.com/example/molr/actions/runs/123/job/456')));
});

test('uses the current attempt and follows a rerun instead of mixing earlier jobs', async () => {
  const h = harness((path, call) => path.includes('/jobs?') ? jobs() : call < 3 ? state() : state('completed', 'success', 2));
  assert.equal(await watchRelease(h.options), 0);
  assert(h.calls[1].path.includes('/attempts/1/jobs?'));
  assert(h.calls[3].path.includes('/attempts/2/jobs?'));
});

test('transient failures back off and recover without starting or retrying remote jobs', async () => {
  const h = harness((path, call) => {
    if (call <= 4) throw new QueryError('Temporary failure', true);
    return path.includes('/jobs?') ? jobs() : state('completed', 'success');
  });
  assert.equal(await watchRelease(h.options), 0);
  assert.deepEqual(h.waits, [60_000, 120_000, 240_000, 300_000]);
  assert.equal(h.calls.length, 6);
});

test('authorization and malformed responses fail closed without repeated requests', async () => {
  for (const respond of [() => { throw new QueryError('HTTP 403'); }, () => ({ ...state(), id: 999 }),
    path => path.includes('/jobs?') ? { total_count: 101, jobs: [] } : state(),
    path => path.includes('/jobs?') ? jobs({ ...job('bad'), steps: null }) : state()]) {
    const h = harness(respond);
    await assert.rejects(watchRelease(h.options));
    assert.deepEqual(h.waits, []);
    assert(h.calls.length <= 2);
  }
});

test('deadline bounds a slow request and prevents the next GET', async () => {
  const h = harness((path, call, now, advance) => {
    if (now() >= 7_140_000) advance(60_000);
    return path.includes('/jobs?') ? jobs() : state();
  });
  assert.equal(await watchRelease(h.options), 2);
  assert.equal(h.calls.length, 239);
  assert.equal(h.now(), 7_200_000);
});

test('cancellation during the idle timer releases it and leaves no further reads', async () => {
  const controller = new AbortController();
  let idle;
  const waiting = new Promise(resolve => { idle = resolve; });
  const h = harness(path => path.includes('/jobs?') ? jobs() : state());
  const watching = watchRelease({ ...h.options, signal: controller.signal, pause: (ms, signal) => {
    const timer = sleep(ms, undefined, { signal });
    idle();
    return timer;
  } });
  await waiting;
  controller.abort();
  await assert.rejects(watching, /abort/i);
  assert.equal(h.calls.length, 2);
});

test('cancellation reaches an active request and does not retry it', async () => {
  const controller = new AbortController();
  let calls = 0;
  await assert.rejects(watchRelease({ repository, run, signal: controller.signal, log: () => {}, api: async (_, { signal }) => {
    calls++;
    assert.equal(signal, controller.signal);
    controller.abort();
    signal.throwIfAborted();
  } }), /abort/i);
  assert.equal(calls, 1);
});

test('GitHub transport uses read-only CLI calls, bounded process lifetime and sanitized errors', async () => {
  const controller = new AbortController();
  const api = createGitHubAPI(repository, async (command, args, options) => {
    assert.equal(command, 'gh');
    assert.deepEqual(args, ['api', 'repos/example/molr/actions/runs/123', '--method', 'GET']);
    assert.equal(options.timeout, 1000);
    assert.equal(options.signal, controller.signal);
    assert.equal(options.shell, undefined);
    return { stdout: '{"id":123}' };
  });
  assert.deepEqual(await api('runs/123', { timeout: 1000, signal: controller.signal }), { id: 123 });
  for (const [stderr, retryable] of [['gh: HTTP 503 artificial-private-detail', true], ['TLS handshake timeout', true], ['gh: HTTP 403 private-detail', false]]) {
    const failing = createGitHubAPI(repository, async () => { throw { stderr }; });
    await assert.rejects(failing('runs/123'), error => error instanceof QueryError && error.retryable === retryable && !error.message.includes('private-detail'));
  }
});
