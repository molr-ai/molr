import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { compareVersions, releaseChannel } from './release-lib.mjs';

export function validateBaseVersion(pkg, lock, latest = null) {
  const base = pkg?.version;
  if (releaseChannel(base) !== 'stable') throw new Error('package.json version must be a plain X.Y.Z development base');
  if (lock?.version !== base || lock?.packages?.['']?.version !== base) {
    throw new Error('Synchronize package.json.version, package-lock.json.version and package-lock.json.packages[""].version');
  }
  if (latest !== null && compareVersions(base, latest) <= 0) {
    const [major, minor, patch] = latest.split('.');
    const next = `${major}.${minor}.${BigInt(patch) + 1n}`;
    throw new Error(`Base version ${base} must be greater than published stable ${latest}. Update package.json and both root versions in package-lock.json to ${next} or a higher development version, then review the lockfile digest in docs/dev/releasing/license-inventory.json.`);
  }
  return base;
}

export async function latestStableVersion(repository, token = '') {
  if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository || '')) throw new Error('GitHub owner/repository required');
  const headers = { Accept: 'application/vnd.github+json', 'X-GitHub-Api-Version': '2022-11-28', 'User-Agent': 'molr-base-version-check' };
  if (token) headers.Authorization = `Bearer ${token}`;
  let latest = null;
  // Read every page: nightly releases can push the last stable out of the website's scan window.
  for (let page = 1; ; page++) {
    const response = await fetch(`https://api.github.com/repos/${repository}/releases?per_page=100&page=${page}`, {
      headers, redirect: 'error', signal: AbortSignal.timeout(30_000),
    });
    if (!response.ok) throw new Error(`Cannot check published versions: GitHub HTTP ${response.status}; retry after resolving API access or rate limits`);
    const releases = await response.json();
    if (!Array.isArray(releases)) throw new Error('Invalid GitHub release list');
    for (const release of releases) {
      if (!release || typeof release.tag_name !== 'string' || typeof release.draft !== 'boolean' || typeof release.prerelease !== 'boolean') {
        throw new Error('Invalid GitHub release metadata');
      }
      if (release.draft || release.prerelease || !release.tag_name.startsWith('v')) continue;
      const version = release.tag_name.slice(1);
      try { if (releaseChannel(version) !== 'stable') continue; }
      catch { continue; }
      if (latest === null || compareVersions(version, latest) > 0) latest = version;
    }
    if (releases.length < 100) return latest;
  }
}

export async function checkBaseVersion({ root = process.cwd(), repository, token } = {}) {
  const pkg = JSON.parse(readFileSync(resolve(root, 'package.json'), 'utf8'));
  const lock = JSON.parse(readFileSync(resolve(root, 'package-lock.json'), 'utf8'));
  validateBaseVersion(pkg, lock);
  const latest = await latestStableVersion(repository, token);
  return { base: validateBaseVersion(pkg, lock, latest), latest };
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try {
    const { base, latest } = await checkBaseVersion({
      repository: process.env.MOLR_RELEASE_REPOSITORY || 'molr-ai/molr', token: process.env.GH_TOKEN,
    });
    console.log(`Base version ${base}: ${latest === null ? 'no published stable releases' : `newer than published stable ${latest}`}`);
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
