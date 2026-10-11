import { execFileSync } from 'node:child_process';
import { appendFileSync, readFileSync } from 'node:fs';
import { publishedReleases } from './release-catalog.mjs';
import { nightlyVersion, releaseChannel, platforms } from './release-lib.mjs';
const git = (...args) => execFileSync('git', args, { encoding: 'utf8' }).trim();
const commit = git('rev-parse', 'HEAD');
git('merge-base', '--is-ancestor', commit, 'origin/main');
const publishedAt = process.env.GITHUB_RUN_ID ? execFileSync('gh', ['api', `repos/${process.env.GITHUB_REPOSITORY}/actions/runs/${process.env.GITHUB_RUN_ID}`, '--jq', '.created_at'], { encoding: 'utf8' }).trim() : new Date().toISOString();
let version;
if (process.env.RELEASE_KIND === 'nightly') {
  version = nightlyVersion(JSON.parse(readFileSync('package.json')).version, commit, new Date(publishedAt));
} else {
  const tag = process.env.RELEASE_TAG || process.env.GITHUB_REF_NAME;
  if (!tag?.startsWith('v') || git('rev-parse', `${tag}^{commit}`) !== commit) throw new Error('Release tag must identify this checkout');
  version = tag.slice(1);
  if (releaseChannel(version) === 'nightly') throw new Error('Nightly tags are created by the nightly workflow');
}
const channel = releaseChannel(version);
const matrix = { include: platforms.filter(p => p.channels.includes(channel)) };
if (!matrix.include.length) throw new Error('No supported platforms');
const published = await publishedReleases(process.env.GITHUB_REPOSITORY);
const existing = published.find(m => m.version === version || (channel === 'nightly' && m.channel === 'nightly' && m.commit === commit));
if (existing && existing.commit !== commit) throw new Error('Published tag identifies another commit');
const output = { version: existing?.version || version, channel, commit, matrix: JSON.stringify(matrix), existing: String(Boolean(existing)), published_at: existing?.published_at || publishedAt };
if (process.env.GITHUB_OUTPUT) for (const [key, value] of Object.entries(output)) appendFileSync(process.env.GITHUB_OUTPUT, `${key}=${value}\n`);
console.log(JSON.stringify(output));
