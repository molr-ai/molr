import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));
const version = readFileSync(resolve(root, 'runtime/.go-version'), 'utf8').trim();
const result = spawnSync(process.env.GO || 'go', [
  'test', '-count=1', './tests/contractcoverage',
], {
  cwd: resolve(root, 'runtime'),
  stdio: 'inherit',
  env: {
    ...process.env,
    CGO_ENABLED: '0',
    GOTOOLCHAIN: `go${version}`,
    GOCACHE: process.env.GOCACHE || resolve(root, '.cache/go-build'),
    GOMODCACHE: process.env.GOMODCACHE || resolve(root, '.cache/go-mod'),
  },
});
if (result.error) console.error(result.error.message);
if (result.status !== 0) {
  console.error('Update runtime/tests/coverage-*.md with behavior tests for each public entry point, then rerun npm run check:contracts.');
}
process.exitCode = result.status ?? 1;
