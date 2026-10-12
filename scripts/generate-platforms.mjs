import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { resolve } from 'node:path';
import { platforms, channels, releaseVersionPattern } from './release-lib.mjs';
const root = resolve(import.meta.dirname, '..');
const seen = new Set();
for (const p of platforms) {
  if (p.id !== `${p.os}-${p.arch}` || seen.has(p.id) || p.os !== 'linux' || p.archive !== 'tar.gz' || p.installer !== 'install.sh' || p.verification !== 'native-install-sandbox' || !p.channels.length || p.channels.some(c => !channels.includes(c))) throw new Error(`Unsupported platform definition: ${p.id}`);
  seen.add(p.id);
  for (const alias of p.aliases) {
    const id = `${p.os}/${alias}`;
    if (!/^[a-z0-9_]+$/.test(alias) || seen.has(id)) throw new Error(`Duplicate or invalid alias: ${id}`);
    seen.add(id);
  }
}
const installer = readFileSync(resolve(root, 'scripts/install.sh'), 'utf8');
if (!installer.includes('# BEGIN GENERATED PLATFORMS') || !installer.includes('# END GENERATED PLATFORMS')) throw new Error('Installer platform markers are missing');
const files = {
  'scripts/install.sh': installer.replace(/# BEGIN GENERATED PLATFORMS[\s\S]*?# END GENERATED PLATFORMS/, '# BEGIN GENERATED PLATFORMS\ncase "$(uname -s)/$(uname -m)" in\n' + platforms.map(p => `  ${p.aliases.map(a => `Linux/${a}`).join('|')}) arch=${p.arch} ;;`).join('\n') + '\n  *) fail "Unsupported OS or architecture" ;;\nesac\n# END GENERATED PLATFORMS'),
  'runtime/internal/releaseplatform/platforms.json': JSON.stringify({ schema: 1, platforms }, null, 2) + '\n',
};
for (const [name, value] of Object.entries(files)) {
  if (process.argv.includes('--check')) {
    if (readFileSync(resolve(root, name), 'utf8') !== value) throw new Error(`Run npm run release:platforms: ${name}`);
  } else {
    mkdirSync(resolve(root, name, '..'), { recursive: true }); writeFileSync(resolve(root, name), value);
  }
}
