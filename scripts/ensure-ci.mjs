// Reuse a complete, trusted CI run for the exact source revision before release.
import { appendFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

export const ciPolicy = 'v2';
export const ciTitle = commit => `CI ${ciPolicy} ${commit}`;
export const ciJob = () => `CI passed ${ciPolicy}`;
export const ciStep = commit => `Validated ${commit}`;

export function matchingCIRuns(runs, repository, commit) {
  return runs.filter(run => run.head_repository?.full_name === repository && run.head_branch === 'main' &&
    run.display_title === ciTitle(commit) &&
    ((run.event === 'push' && run.head_sha === commit) || run.event === 'workflow_dispatch'))
    .sort((a, b) => b.id - a.id);
}

export async function ensureCI({
  repository, commit, api, pause = ms => new Promise(r => setTimeout(r, ms)), now = Date.now,
  log = console.log, maxWaitMs = 120 * 60_000, pollMs = 30_000,
}) {
  if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository) || !/^[a-f0-9]{40}$/.test(commit)) throw new Error('Invalid CI source identity');
  const base = `repos/${repository}/actions`;
  const deadline = now() + maxWaitMs;
  let dispatched = false;
  async function list(query, key) {
    const values = [];
    for (let page = 1; page <= 10; page++) {
      const response = await api(`${query}${query.includes('?') ? '&' : '?'}per_page=100&page=${page}`);
      if (!Array.isArray(response[key])) throw new Error('Invalid CI API response');
      values.push(...response[key]);
      if (response[key].length < 100) return values;
    }
    throw new Error('CI search exceeded 1000 entries; narrow or inspect the workflow history');
  }
  while (true) {
    const workflow = `${base}/workflows/ci.yml/runs`;
    const runs = [
      ...await list(`${workflow}?event=push&branch=main&head_sha=${commit}`, 'workflow_runs'),
      ...await list(`${workflow}?event=workflow_dispatch&branch=main`, 'workflow_runs'),
    ];
    const run = matchingCIRuns(runs, repository, commit)[0];
    if (run) {
      if (run.status === 'completed') {
        if (run.conclusion !== 'success') throw new Error(`CI did not pass (${run.conclusion}): ${run.html_url}`);
        // Use jobs from the latest attempt, never an older successful attempt.
        const jobs = await list(`${base}/runs/${run.id}/attempts/${run.run_attempt}/jobs`, 'jobs');
        const completed = jobs.filter(job => job.name === ciJob());
        if (completed.length !== 1 || completed[0].status !== 'completed' || completed[0].conclusion !== 'success') {
          throw new Error(`CI lacks complete ${ciPolicy} validation for ${commit}: ${run.html_url}`);
        }
        const steps = completed[0].steps?.filter(step => step.name === ciStep(commit)) || [];
        if (steps.length !== 1 || steps[0].status !== 'completed' || steps[0].conclusion !== 'success') {
          throw new Error(`CI lacks complete validation for the checked-out SHA ${commit}: ${run.html_url}`);
        }
        log(`Reusing complete CI: ${run.html_url} (attempt ${run.run_attempt})`);
        return { run_id: run.id, run_attempt: run.run_attempt, url: run.html_url };
      }
      log(`Waiting for CI: ${run.html_url}`);
    } else if (!dispatched) {
      // The workflow runs trusted main definitions but explicitly checks out and
      // reports this SHA. A dispatch run's head_sha is NOT the tested revision.
      await api(`${base}/workflows/ci.yml/dispatches`, { method: 'POST', body: { ref: 'main', inputs: { commit } } });
      dispatched = true;
      log(`Requested complete CI for ${commit}`);
    }
    if (now() >= deadline) throw new Error('Timed out waiting for CI; retry release after the existing CI finishes');
    await pause(Math.min(pollMs, deadline - now()));
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const token = process.env.GH_TOKEN;
  if (!token) throw new Error('GH_TOKEN with Actions read/write permission is required');
  const result = await ensureCI({
    repository: process.env.GITHUB_REPOSITORY, commit: process.env.MOLR_COMMIT,
    api: async (path, { method = 'GET', body } = {}) => {
      const response = await fetch(`https://api.github.com/${path}`, {
        method, headers: { Authorization: `Bearer ${token}`, Accept: 'application/vnd.github+json', 'Content-Type': 'application/json' },
        body: body ? JSON.stringify(body) : undefined, signal: AbortSignal.timeout(30_000),
      });
      if (!response.ok) throw new Error(`CI API ${method} failed: HTTP ${response.status}`);
      return response.status === 204 ? {} : response.json();
    },
  });
  if (process.env.GITHUB_OUTPUT) for (const [key, value] of Object.entries(result)) appendFileSync(process.env.GITHUB_OUTPUT, `${key}=${value}\n`);
  if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, `Verified source: \`${process.env.MOLR_COMMIT}\`\n\nCI: ${result.url} (attempt ${result.run_attempt})\n`);
}
