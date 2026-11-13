import { execFile } from 'node:child_process';
import { setTimeout as sleep } from 'node:timers/promises';
import { promisify } from 'node:util';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

// External Actions has no completion subscription in this CLI. See the release
// guide for the polling exception, request budget and test-release-watch.mjs.
const interval = 60_000, duration = 120 * 60_000;
const repositoryPattern = /^[A-Za-z0-9][A-Za-z0-9-]*\/(?!\.{1,2}$)[A-Za-z0-9_.-]+$/;
const runPattern = /^[1-9][0-9]*$/;
export const usage = 'Usage: npm run release:watch -- <run-id|https://github.com/owner/repo/actions/runs/id> [--repo owner/repo]';

export function parseWatchArgs(args, defaultRepository = 'molr-ai/molr') {
  let target, explicitRepository;
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--repo' && !explicitRepository && args[i + 1]) explicitRepository = args[++i];
    else if (!target && !args[i].startsWith('-')) target = args[i];
    else throw new Error(usage);
  }
  const url = target?.match(/^https:\/\/github\.com\/([A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+)\/actions\/runs\/([1-9][0-9]*)\/?$/);
  const repository = explicitRepository || url?.[1] || defaultRepository;
  const run = url?.[2] || target;
  if (!repositoryPattern.test(repository) || !runPattern.test(run || '') || url && url[1] !== repository) throw new Error(usage);
  return { repository, run };
}

export class QueryError extends Error {
  constructor(message, retryable = false) { super(message); this.retryable = retryable; }
}

export function createGitHubAPI(repository, execute = promisify(execFile)) {
  if (!repositoryPattern.test(repository)) throw new Error('Invalid GitHub repository');
  return async (path, { signal, timeout } = {}) => {
    let stdout;
    try {
      ({ stdout } = await execute('gh', ['api', `repos/${repository}/actions/${path}`, '--method', 'GET'], {
        encoding: 'utf8', timeout: Math.min(45_000, timeout ?? 45_000), signal, maxBuffer: 8 << 20,
      }));
    } catch (error) {
      signal?.throwIfAborted();
      if (error.code === 'ENOENT') throw new QueryError('Install GitHub CLI (gh) and authenticate before watching a release.');
      const status = Number(error.stderr?.match(/HTTP (\d{3})/)?.[1]);
      const retryable = error.killed || [408, 429].includes(status) || status >= 500 ||
        /timeout|timed out|TLS handshake|connection|network|EOF|resolve host|rate limit/i.test(error.stderr || '');
      throw new QueryError(`GitHub query failed${status ? ` (HTTP ${status})` : ''}.${retryable ? '' : ' Check gh authentication and repository access.'}`, retryable);
    }
    try { return JSON.parse(stdout); }
    catch { throw new QueryError('GitHub returned invalid JSON.'); }
  };
}

export async function watchRelease({ repository, run, api, signal, now = () => performance.now(),
  pause = (ms, signal) => sleep(ms, undefined, { signal }), log = console.log }) {
  if (!repositoryPattern.test(repository) || !runPattern.test(String(run))) throw new Error('Invalid release run');
  const url = `https://github.com/${repository}/actions/runs/${run}`;
  const deadline = now() + duration;
  let failures = 0, previous = '';
  log(`Watching ${url}`);
  for (let round = 0; round < 120 && now() < deadline; round++) {
    signal?.throwIfAborted();
    let delay = interval;
    try {
      const query = async path => {
        signal?.throwIfAborted();
        const timeout = deadline - now();
        if (timeout <= 0) throw new QueryError('Observation deadline reached.');
        const value = await api(path, { signal, timeout: Math.min(45_000, timeout) });
        signal?.throwIfAborted();
        return value;
      };
      const state = await query(`runs/${run}`);
      if (String(state.id) !== String(run) || state.repository?.full_name?.toLowerCase() !== repository.toLowerCase() ||
          !Number.isSafeInteger(state.run_attempt) || state.run_attempt < 1 || typeof state.status !== 'string' ||
          state.status === 'completed' && typeof state.conclusion !== 'string') throw new QueryError('Invalid workflow run response.');
      const result = await query(`runs/${run}/attempts/${state.run_attempt}/jobs?per_page=100`);
      if (!Array.isArray(result.jobs) || !Number.isSafeInteger(result.total_count) || result.total_count !== result.jobs.length ||
          result.jobs.some(job => !Number.isSafeInteger(job.id) || typeof job.name !== 'string' || typeof job.status !== 'string' ||
            !Array.isArray(job.steps) || job.steps.some(step => typeof step.name !== 'string' || typeof step.status !== 'string'))) {
        throw new QueryError('Incomplete workflow jobs response (maximum 100 jobs); inspect the run link.');
      }
      const jobs = result.jobs;
      const passed = jobs.filter(job => job.conclusion === 'success').length;
      const skipped = jobs.filter(job => job.conclusion === 'skipped').length;
      const pending = jobs.filter(job => job.status !== 'completed');
      const failed = jobs.filter(job => job.status === 'completed' && !['success', 'skipped'].includes(job.conclusion));
      const summary = [
        `Attempt ${state.run_attempt}: ${state.status}${state.conclusion ? ` (${state.conclusion})` : ''}; ${passed} passed, ${skipped} skipped, ${pending.length} pending, ${failed.length} failed.`,
        ...pending.map(job => `  ${job.name}: ${job.steps.find(step => step.status === 'in_progress')?.name || job.status}`),
        ...failed.map(job => `  ${job.name}: ${job.conclusion} — ${url}/job/${job.id}`),
      ].join('\n');
      if (summary !== previous || failures) log(summary);
      previous = summary;
      failures = 0;
      if (state.status === 'completed') return state.conclusion === 'success' ? 0 : 1;
    } catch (error) {
      signal?.throwIfAborted();
      if (now() >= deadline) break;
      if (!(error instanceof QueryError) || !error.retryable) throw error;
      delay = Math.min(300_000, interval * 2 ** Math.min(failures++, 3));
      log(`${error.message} Retrying in ${delay / 1000}s; the remote workflow continues.`);
    }
    const remaining = deadline - now();
    if (remaining <= 0) break;
    await pause(Math.min(delay, remaining), signal);
  }
  log(`Observation timed out; the remote workflow continues. Resume watching ${url}`);
  return 2;
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const controller = new AbortController();
  const interrupt = () => controller.abort(new Error('Observation interrupted; the remote workflow continues.'));
  process.once('SIGINT', interrupt);
  process.once('SIGTERM', interrupt);
  try {
    const options = parseWatchArgs(process.argv.slice(2), process.env.MOLR_RELEASE_REPOSITORY || 'molr-ai/molr');
    process.exitCode = await watchRelease({ ...options, api: createGitHubAPI(options.repository), signal: controller.signal });
  } catch (error) {
    console.error(error.message);
    process.exitCode = controller.signal.aborted ? 130 : 2;
  } finally {
    process.removeListener('SIGINT', interrupt);
    process.removeListener('SIGTERM', interrupt);
  }
}
