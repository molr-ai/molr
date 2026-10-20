// Platform-independent application assets shared by native build jobs.
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { lstatSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { resolve, relative, join } from 'node:path';
import { pathToFileURL } from 'node:url';

const directories = ['apps/webui/dist', 'apps/desktop/dist'];
const metadata = '.cache/build-assets/manifest.json';
const hash = value => createHash('sha256').update(value).digest('hex');

function inventory(root) {
  const files = {};
  function walk(directory) {
    if (!lstatSync(directory).isDirectory()) throw new Error('Build assets must contain only regular files and directories');
    for (const name of readdirSync(directory).sort()) {
      const path = join(directory, name), stat = lstatSync(path);
      if (stat.isDirectory()) walk(path);
      else if (stat.isFile()) files[relative(root, path).replaceAll('\\', '/')] = hash(readFileSync(path));
      else throw new Error('Build assets must contain only regular files and directories');
    }
  }
  for (const directory of directories) walk(resolve(root, directory));
  if (!files['apps/webui/dist/index.html'] || !files['apps/desktop/dist/renderer/index.html'] || !files['apps/desktop/dist/main/main/index.js']) {
    throw new Error('Incomplete application build assets');
  }
  return files;
}

export function recordBuildAssets(root, version, commit) {
  const manifest = { schema: 1, version, commit, lock: hash(readFileSync(join(root, 'package-lock.json'))), files: inventory(root) };
  mkdirSync(join(root, '.cache/build-assets'), { recursive: true });
  writeFileSync(join(root, metadata), JSON.stringify(manifest, null, 2) + '\n');
  return manifest;
}

export function verifyBuildAssets(root, version, commit) {
  const manifest = JSON.parse(readFileSync(join(root, metadata)));
  if (manifest.schema !== 1 || manifest.version !== version || manifest.commit !== commit ||
      manifest.lock !== hash(readFileSync(join(root, 'package-lock.json')))) throw new Error('Build assets identity mismatch');
  const files = inventory(root);
  if (JSON.stringify(files) !== JSON.stringify(manifest.files)) throw new Error('Build assets checksum or file inventory mismatch');
  return manifest;
}

export function useBuildAssets(root, version, commit) {
  if (!process.env.MOLR_BUILD_ASSETS) return false;
  if (process.env.MOLR_BUILD_ASSETS !== '1') throw new Error('MOLR_BUILD_ASSETS must be 1 when consuming shared application assets');
  verifyBuildAssets(root, version, commit);
  return true;
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const root = resolve(import.meta.dirname, '..');
  const version = process.env.MOLR_VERSION || '0.1.0-local';
  const commit = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
  if (process.argv.includes('--verify')) {
    verifyBuildAssets(root, version, commit);
  } else {
    const env = { ...process.env, MOLR_VERSION: version, MOLR_COMMIT: commit };
    for (const script of ['build:webui', 'build:desktop']) {
      execFileSync('npm', ['run', script], { cwd: root, env, stdio: 'inherit', shell: process.platform === 'win32' });
    }
    recordBuildAssets(root, version, commit);
  }
  console.log(`Application assets verified: ${version} ${commit}`);
}
