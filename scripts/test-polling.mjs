import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { goEnvironment, repository } from './check-polling.mjs';
const go = process.env.GO || 'go';
function run(command, args, cwd = repository, capture = false) {
  const result = spawnSync(command, args, { cwd, env: goEnvironment(), encoding: 'utf8', stdio: capture ? 'pipe' : 'inherit', maxBuffer: 16 << 20 });
  if (result.error || result.status !== 0) {
    if (capture) console.error(result.stdout, result.stderr);
    throw new Error(result.error?.message ?? `${command} exited ${result.status}`);
  }
  return result;
}
// Exact contracts, not a name substring that could silently run zero tests.
const contracts = [
  ['internal/platform/sandbox/authorization_test.go', ['TestAuthorizationObserverIdleChangesExpiryAndFailures', 'TestAuthorizationObserverReschedulesExtendedExpiry']],
  ['internal/agent/scheduler/tick_wakeup_test.go', ['TestScheduleTickSleepsUntilCommittedChangeOrDeadline']],
  ['internal/agent/delivery_wakeup_test.go', ['TestIdleInterruptReadCount', 'TestInterruptCommitDuringReadIsNotLost', 'TestInterruptFutureDeadlineWithoutAnotherCommit', 'TestInterruptRollbackDoesNotWakeAndCommitDoes']],
  ['internal/api/stream_idle_test.go', ['TestStreamIdleDoesNotPollHTTPIdentity']],
  ['internal/api/debug_stream_idle_test.go', ['TestDebugStreamIdleDoesNotPoll', 'TestDebugStreamClosesAtCredentialExpiry']],
  ['internal/im/realtime/auth_watch_test.go', ['TestAuthorizationWatchIdleConnections', 'TestAuthorizationWatchExpiryRechecksWithoutReplay', 'TestAuthorizationCommitDuringCheckIsNotLost']],
  ...['dingtalk', 'feishu', 'slack', 'telegram', 'weixin'].map(channel => [`internal/im/${channel}/idle_test.go`, ['TestIdleChannelDoesNotAccessDatabase', 'TestActiveChannelSleepsAndWakesCommittedDeliveryAtDeadline']]),
];
try {
  run(go, ['test', '-count=1', 'scripts/polling-go/main.go', 'scripts/polling-go/main_test.go']);
  run(process.execPath, ['--test', 'scripts/test-polling-guard.mjs', 'scripts/test-polling-behavior.mjs']);
  const expected = [], packages = new Set(), names = new Set();
  for (const [file, tests] of contracts) {
    const source = readFileSync(join(repository, 'runtime', file), 'utf8');
    const pkg = file.slice(0, file.lastIndexOf('/')); packages.add(`./${pkg}`);
    for (const name of tests) {
      if (!new RegExp(`func\\s+${name}\\s*\\(`).test(source)) throw new Error(`Idle contract missing: ${file}:${name}`);
      expected.push(`molr.ai/runtime/${pkg}:${name}`); names.add(name);
    }
  }
  const result = run(go, ['test', '-json', '-count=1', '-timeout=2m', '-run', `^(${[...names].join('|')})$`, ...packages], join(repository, 'runtime'), true);
  const passed = new Set(result.stdout.split('\n').filter(Boolean).map(line => JSON.parse(line)).filter(row => row.Action === 'pass' && row.Test).map(row => `${row.Package}:${row.Test}`));
  for (const name of expected) if (!passed.has(name)) throw new Error(`Idle contract did not execute successfully: ${name}`);
  console.log(`Polling behavior gate passed: ${expected.length} Runtime idle/change/deadline contracts plus frontend virtual-clock budgets.`);
} catch (error) { console.error(error.message); process.exitCode = 1; }
