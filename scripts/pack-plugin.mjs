import { readdir, readFile, lstat, writeFile } from 'node:fs/promises';
import { resolve, relative, join } from 'node:path';
import { pluginArchive } from './plugin-archive.mjs';
const [directory, output] = process.argv.slice(2);
if (!directory || !output) throw new Error('Usage: node scripts/pack-plugin.mjs <plugin-directory> <output.zip>');
const root = resolve(directory), destination = resolve(output), files = {}; let total = 0;
const decoder = new TextDecoder('utf-8', { fatal: true });
async function walk(dir) {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const file = join(dir, entry.name), name = relative(root, file).split('\\').join('/');
    if (file === destination) throw new Error('Write the archive outside the source directory');
    if (entry.isSymbolicLink()) throw new Error(`Links are not supported: ${name}`);
    if (entry.isDirectory()) { await walk(file); continue; }
    if (!entry.isFile()) throw new Error(`Ordinary files required: ${name}`);
    const size = (await lstat(file)).size; total += size;
    if (size > 1048576 || total > 8388608 || Object.keys(files).length >= 257) throw new Error('Plugin package exceeds limits');
    files[name] = decoder.decode(await readFile(file));
  }
}
await walk(root);
const manifest = JSON.parse(files['plugin.json'] ?? 'null'); delete files['plugin.json'];
if (manifest?.pluginApi !== 3) throw new Error('plugin.json must declare pluginApi: 3');
const archive = pluginArchive({ manifest, files });
if (archive.length > 8388608) throw new Error('ZIP exceeds 8 MiB');
await writeFile(destination, archive, { flag: 'wx', mode: 0o600 });
console.log(`Created ${destination}`);
