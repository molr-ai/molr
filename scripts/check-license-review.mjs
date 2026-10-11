import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';

const root = fileURLToPath(new URL('../', import.meta.url));
const read = path => readFileSync(resolve(root, path));
const inventory = JSON.parse(read('dev-docs/releasing/license-inventory.json'));
const lock = JSON.parse(read('package-lock.json'));
const failures = [];
for (const [path, expected] of Object.entries(inventory.inputs)) {
  const actual = createHash('sha256').update(read(path)).digest('hex');
  if (actual !== expected) failures.push(`${path}: changed since the license review; review before updating the inventory`);
}
for (const [path, entry] of Object.entries(lock.packages)) {
  if (path.includes('node_modules/')) continue;
  const file = path ? `${path}/package.json` : 'package.json';
  if (JSON.parse(read(file)).license !== 'MIT' || entry.license !== 'MIT') {
    failures.push(`${file}: Molr's own package and lock entry must declare MIT`);
  }
}
if (failures.length) {
  console.error(failures.join('\n'));
  process.exitCode = 1;
} else {
  console.log(`License review inputs match ${inventory.reviewedAt}: ${inventory.npm.length} npm entries, ${inventory.go.length} Go modules.`);
  console.log('Review inputs and distributed notice text match. Use npm run notices:check to verify original dependency texts.');
}
