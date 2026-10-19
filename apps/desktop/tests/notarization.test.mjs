import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync, writeFileSync, existsSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createHash } from 'node:crypto';
import { prepareNotarization, finishNotarization } from '../scripts/notarization.mjs';

const id = '11111111-2222-4333-8444-555555555555';
const env = { APPLE_ID: 'notary@example.invalid', APPLE_APP_SPECIFIC_PASSWORD: 'artificial-password', APPLE_TEAM_ID: 'FAKETEAM00' };
const hash = bytes => createHash('sha256').update(bytes).digest('hex');
function fixture(t, signed = true) {
  const directory = mkdtempSync(join(tmpdir(), 'molr-notarization-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const file = 'molr-desktop_0.1.0-rc.1_darwin-arm64_full.dmg';
  const bytes = 'artificial signed disk image';
  writeFileSync(join(directory, file), bytes);
  const manifest = { schema: 1, version: '0.1.0-rc.1', commit: 'a'.repeat(40), os: 'darwin', arch: 'arm64', variant: 'full', signed, artifacts: [{ file, sha256: hash(bytes) }] };
  const calls = [];
  prepareNotarization(directory, manifest, { env, run: (command, args) => { calls.push([command, ...args]); return JSON.stringify({ id }); } });
  return { directory, file, manifest, calls, output: join(directory, 'desktop-manifest.json') };
}

test('submission saves immutable DMG identity and ID before waiting, without credentials or public manifest', t => {
  const f = fixture(t);
  const submission = f.calls.find(call => call.includes('submit'));
  assert(submission); assert(!submission.includes('--wait'));
  assert(!f.calls.some(call => call.includes('info')));
  const text = readFileSync(join(f.directory, 'notarization.json'), 'utf8');
  assert.equal(JSON.parse(text).submissionId, id);
  for (const value of Object.values(env)) assert(!text.includes(value));
  assert(!existsSync(f.output)); assert(!existsSync(join(f.directory, `${f.file}.sha256`)));
});

test('a pending job resumes the same submission and hashes the stapled DMG only after acceptance', async t => {
  const f = fixture(t);
  const originalState = readFileSync(join(f.directory, 'notarization.json'), 'utf8');
  await assert.rejects(finishNotarization(f.directory, f.manifest, { env, maxWaitMs: 0, run: () => JSON.stringify({ id, status: 'In Progress' }) }), /still pending/);
  assert(!existsSync(f.output));
  assert.equal(readFileSync(join(f.directory, 'notarization.json'), 'utf8'), originalState);
  const calls = [];
  await finishNotarization(f.directory, f.manifest, { env, run: (command, args) => {
    calls.push([command, ...args]);
    assert(!args.includes('submit'));
    if (args.includes('info')) return JSON.stringify({ id, status: 'Accepted' });
    if (args.includes('staple')) writeFileSync(join(f.directory, f.file), 'artificial disk image with ticket');
    return '';
  } });
  assert(calls.some(call => call.includes('validate')));
  assert(calls.some(call => call[0] === 'spctl'));
  const result = JSON.parse(readFileSync(f.output));
  assert.equal(result.artifacts[0].sha256, hash('artificial disk image with ticket'));
  assert.equal(readFileSync(join(f.directory, `${f.file}.sha256`), 'utf8'), `${result.artifacts[0].sha256}  ${f.file}\n`);
});

test('rejected notarization never produces release metadata or staples the installer', async t => {
  const f = fixture(t), calls = [];
  await assert.rejects(finishNotarization(f.directory, f.manifest, { env, run: (_command, args) => {
    calls.push(args); return JSON.stringify({ id, status: 'Invalid' });
  } }), /not accepted: Invalid/);
  assert(calls.some(args => args.includes('log')));
  assert(!calls.some(args => args.includes('staple'))); assert(!existsSync(f.output));
});

test('transient status query failures retry the saved ID and redact credentials', async t => {
  const f = fixture(t), logs = [];
  let queries = 0;
  await finishNotarization(f.directory, f.manifest, { env, log: message => logs.push(message), pause: async () => {}, run: (_command, args) => {
    assert(!args.includes('submit'));
    if (args.includes('info') && ++queries < 3) throw new Error(`connection failed ${env.APPLE_APP_SPECIFIC_PASSWORD}`);
    return JSON.stringify({ id, status: 'Accepted' });
  } });
  assert.equal(queries, 3); assert(existsSync(f.output));
  assert(!logs.join('\n').includes(env.APPLE_APP_SPECIFIC_PASSWORD));
});

test('persistent connection errors leave a resumable artifact without release metadata', async t => {
  const f = fixture(t);
  let queries = 0;
  await assert.rejects(finishNotarization(f.directory, f.manifest, { env, pause: async () => {}, run: () => { queries++; throw new Error('offline'); } }), /resume the same submission/);
  assert.equal(queries, 3); assert(!existsSync(f.output));
  assert(existsSync(join(f.directory, 'notarization.json')));
});

test('artifact corruption, another revision or a mismatched submission fails before publication', async t => {
  const f = fixture(t);
  const forbidden = () => { throw new Error('Unexpected external command'); };
  for (const field of ['version', 'commit', 'arch']) {
    await assert.rejects(finishNotarization(f.directory, { ...f.manifest, [field]: 'wrong' }, { env, run: forbidden }), /identity mismatch/);
  }
  await assert.rejects(finishNotarization(f.directory, f.manifest, { env, run: () => JSON.stringify({ id: 'wrong', status: 'Accepted' }) }), /different notarization submission/);
  writeFileSync(join(f.directory, f.file), 'corrupted');
  await assert.rejects(finishNotarization(f.directory, f.manifest, { env, run: forbidden }), /checksum mismatch/);
  assert(!existsSync(f.output));
});

test('ticket validation or Gatekeeper failure prevents publication after Apple acceptance', async t => {
  for (const failingCommand of ['validate', 'spctl']) {
    const f = fixture(t);
    await assert.rejects(finishNotarization(f.directory, f.manifest, { env, run: (command, args) => {
      if (command === failingCommand || args.includes(failingCommand)) throw new Error('ticket verification failed');
      return JSON.stringify({ id, status: 'Accepted' });
    } }), /ticket verification failed/);
    assert(!existsSync(f.output));
  }
});

test('unconfigured signing retains the existing unsigned path without contacting Apple', async t => {
  const f = fixture(t, false);
  assert.equal(f.calls.length, 0);
  await finishNotarization(f.directory, f.manifest, { env: {}, run: () => { throw new Error('Unexpected Apple request'); } });
  assert.equal(JSON.parse(readFileSync(f.output)).signed, false);
});
