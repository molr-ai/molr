// Cloudflare Workers Builds runs this after installing npm dependencies.
import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, rmSync } from 'node:fs';
import { resolve } from 'node:path';
import { publishedReleases, publishedDesktopCatalog, verifyChannelDownloads, writeCatalog } from './release-catalog.mjs';
const root = resolve(import.meta.dirname, '..');
process.chdir(root);
const run = (...args) => execFileSync('npm', ['run', ...args], { stdio: 'inherit' });
run('check:release'); run('check:licenses'); run('check:platforms');
const repository = process.env.MOLR_RELEASE_REPOSITORY || process.env.GITHUB_REPOSITORY || 'molr-ai/molr';
const releases = await publishedReleases(repository);
await verifyChannelDownloads(releases);
const desktop = await publishedDesktopCatalog(repository, releases);
mkdirSync(resolve(root, '.cache'), { recursive: true });
const directory = mkdtempSync(resolve(root, '.cache/website-catalog-'));
try {
  writeCatalog(releases, directory, desktop);
  execFileSync('npm', ['run', 'build:website'], { stdio: 'inherit', env: { ...process.env, VITE_SITE_MODE: process.env.VITE_SITE_MODE || 'live', MOLR_CATALOG_DIR: directory } });
} finally { rmSync(directory, { recursive: true, force: true }); }
