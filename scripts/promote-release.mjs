// Prepare the recommended manifest after packages and release notes are ready.
// Upload this manifest last; this command never uploads or installs anything.
import { execFileSync } from 'node:child_process';
import { readFileSync, writeFileSync, renameSync, rmSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { resolve } from 'node:path';
import { parseArgs } from 'node:util';
const { values } = parseArgs({ options: { version: { type: 'string' }, arch: { type: 'string' }, 'notes-url': { type: 'string' }, directory: { type: 'string' } } });
const { version, arch } = values;
if (!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/.test(version || '') || /(?:-|\.)(?:local|dev|development)(?:\.|$)/.test(version)) throw new Error('A release SemVer version is required');
if (version.split('-').slice(1).join('-').split('.').some(id => /^0\d+$/.test(id))) throw new Error('Invalid SemVer prerelease');
if (!['amd64', 'arm64'].includes(arch)) throw new Error('--arch must be amd64 or arm64');
const notes = new URL(values['notes-url']);
if (notes.protocol !== 'https:' || notes.username || notes.password) throw new Error('HTTPS release notes URL required');
const root = resolve(import.meta.dirname, '..'), directory = resolve(values.directory || process.env.MOLR_DIST_DIR || 'dist/releases');
const file = `molr_${version}_linux_${arch}.tar.gz`, archive = resolve(directory, file);
execFileSync('python3', [resolve(root, 'scripts/release_safety.py'), 'release', archive], { stdio: 'inherit' });
const metadata = JSON.parse(execFileSync('tar', ['-xOf', archive, 'molr/release.json'], { encoding: 'utf8' }));
if (metadata.version !== version || metadata.os !== 'linux' || metadata.arch !== arch) throw new Error('Archive identity mismatch');
const sha256 = createHash('sha256').update(readFileSync(archive)).digest('hex');
if (readFileSync(`${archive}.sha256`, 'utf8') !== `${sha256}  ${file}\n`) throw new Error('Archive checksum mismatch');
const manifest = { version, commit: metadata.commit, published_at: new Date().toISOString(), notes_url: notes.href, file, sha256 };
const target = resolve(directory, `latest-linux-${arch}.json`), temporary = `${target}.tmp`;
try { writeFileSync(temporary, JSON.stringify(manifest, null, 2) + '\n'); renameSync(temporary, target); }
finally { rmSync(temporary, { force: true }); }
console.log(`Recommended release manifest: ${target}`);
