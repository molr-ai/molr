import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
const source = (process.env.MOLR_RELEASE_URL || 'https://molr.ai/releases').replace(/\/$/, '');
const expected = JSON.parse(readFileSync('.cache/release-catalog/catalog.json'));
let actual;
const timeout = Number(process.env.MOLR_SITE_WAIT_SECONDS || 1200);
if (!Number.isFinite(timeout) || timeout < 1 || timeout > 1800) throw new Error('Invalid website wait timeout');
const deadline = Date.now() + timeout * 1000;
const expectedScript = readFileSync('scripts/install.sh', 'utf8').replace("'https://molr.ai/releases'", JSON.stringify(source));
while (true) {
  try {
    const response = await fetch(`${source}/catalog.json`, { cache: 'no-store', signal: AbortSignal.timeout(10000) });
    actual = await response.json();
    assert.deepEqual(actual, expected);
    const installer = await fetch(new URL('../install.sh', `${source}/`), { cache: 'no-store', signal: AbortSignal.timeout(10000) });
    assert(installer.ok); assert.equal(await installer.text(), expectedScript);
    break;
  } catch (error) { if (Date.now() >= deadline) throw new Error('Website catalog did not update before the deadline; inspect Cloudflare build history', { cause: error }); await new Promise(r => setTimeout(r, 5000)); }
}
for (const [channel, release] of Object.entries(expected.channels)) {
  if (!release) continue;
  const response = await fetch(`${source}/${channel}/manifest.json`);
  assert.deepEqual(await response.json(), release);
  for (const p of release.platforms) {
    const pointer = await (await fetch(`${source}/${channel}/latest-${p.os}-${p.arch}.json`)).json();
    assert.equal(pointer.sha256, p.sha256); assert.equal(pointer.version, release.version);
  }
}
console.log('Public website installer and release channels verified');
