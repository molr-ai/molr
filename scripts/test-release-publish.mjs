import assert from 'node:assert/strict';
import { test } from 'node:test';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { resolve, join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { fixture, desktopFixture } from './release-test-fixtures.mjs';
function writeDesktop(directory, runtime) {
  const m = desktopFixture(runtime), bytes = Buffer.from('artificial desktop installer');
  for (const p of m.platforms) {
    p.signed = false;
    p.sha256 = createHash('sha256').update(bytes).digest('hex');
    writeFileSync(join(directory, p.file), bytes);
    writeFileSync(join(directory, p.file + '.sha256'), `${p.sha256}  ${p.file}\n`);
  }
  writeFileSync(join(directory, 'desktop-manifest.json'), JSON.stringify(m));
  return ['desktop-manifest.json', ...m.platforms.flatMap(p => [p.file, p.file + '.sha256'])];
}
const publisher = resolve(import.meta.dirname, 'publish-release.mjs');
const fakeGH = `#!/usr/bin/env python3
import os,json,sys,pathlib,base64,urllib.parse
p=pathlib.Path(os.environ['MOCK_STATE']); s=json.loads(p.read_text()); a=sys.argv[1:]
s['calls'].append(a)
try:
 assert a[0]=='api', 'All operations must use the API, not tag-based CLI lookups'
 if '/git/ref/tags/' in a[1]:
  if os.environ.get('MISSING_TAG'): sys.exit(1)
  print('{}')
 elif '--method' in a:
  method=a[a.index('--method')+1]
  if a[1].startswith('https://uploads.github.com/'):
   assert method=='POST' and '/releases/123/assets?' in a[1]
   assert 'Content-Type: application/octet-stream' in a
   if os.environ.get('FAIL_UPLOAD') and len(s['release']['assets'])==2: sys.exit(1)
   f=pathlib.Path(a[a.index('--input')+1]); name=urllib.parse.parse_qs(urllib.parse.urlparse(a[1]).query)['name'][0]
   assert name==f.name
   s['release']['assets'].append({'id':len(s['release']['assets'])+1,'name':f.name,'data':base64.b64encode(f.read_bytes()).decode()})
   print('{}')
  else:
   body=json.load(sys.stdin); s.setdefault('bodies',[]).append(body)
   if method=='POST':
    assert s['release'] is None and a[1]=='repos/example/molr/releases'
    assert body['draft'] and body['generate_release_notes'] and body['make_latest']=='false'
    s['release']={'id':123,**body,'assets':[]}
   elif method=='PATCH':
    assert a[1]=='repos/example/molr/releases/123'
    s['release'].update(body)
   else: raise Exception('Unexpected method')
   print(json.dumps(s['release']))
 elif '/releases/assets/' in a[1]:
  item=next(x for x in s['release']['assets'] if x['id']==int(a[1].rsplit('/',1)[1])); sys.stdout.buffer.write(base64.b64decode(item['data']))
 else:
  assert a[1]=='repos/example/molr/releases?per_page=100'
  print(json.dumps([[s['release']] if s['release'] and not os.environ.get('STALE_LIST') else []]))
finally: p.write_text(json.dumps(s))
`;
test('publication resumes an interrupted draft, refuses overwrites, and never publishes partial assets', () => {
  const directory = mkdtempSync(join(tmpdir(), 'molr-publish-'));
  try {
    const bin = join(directory, 'bin'), artifacts = join(directory, 'artifacts'); mkdirSync(bin); mkdirSync(artifacts);
    const state = join(directory, 'state.json'); writeFileSync(state, JSON.stringify({ release: null, calls: [] }));
    writeFileSync(join(bin, 'gh'), fakeGH, { mode: 0o755 });
    const mock = join(directory, 'fetch.mjs'); writeFileSync(mock, 'globalThis.fetch = async () => ({ok: true});');
    const m = fixture('0.2.0-rc.1');
    writeFileSync(join(artifacts, 'manifest.json'), JSON.stringify(m));
    const desktopFiles = writeDesktop(artifacts, m);
    for (const file of m.platforms.flatMap(p => [p.file, p.file+'.sha256'])) writeFileSync(join(artifacts, file), 'artificial public fixture');
    const run = (extra = {}) => spawnSync(process.execPath, ['--import', mock, publisher], { encoding: 'utf8', env: { ...process.env, PATH: `${bin}:${process.env.PATH}`, MOCK_STATE: state, MOLR_DIST_DIR: artifacts, GITHUB_REPOSITORY: 'example/molr', ...extra } });
    const desktopPath = join(artifacts, desktopFiles[1]);
    writeFileSync(desktopPath, 'corrupted installer');
    assert.notEqual(run().status, 0);
    assert.equal(JSON.parse(readFileSync(state)).calls.length, 0, 'No GitHub mutations before desktop validation');
    writeDesktop(artifacts, m);
    assert.notEqual(run({ FAIL_UPLOAD: '1' }).status, 0);
    let s = JSON.parse(readFileSync(state)); assert(s.release.draft); assert.equal(s.release.assets.length, 2);
    let result = run(); assert.equal(result.status, 0, result.stderr);
    s = JSON.parse(readFileSync(state)); assert.equal(s.release.draft, false); assert.equal(s.release.assets.length, 1 + desktopFiles.length + m.platforms.length * 2);
    assert.match(s.release.body, /Windows desktop installers are unsigned/);
    assert.match(s.release.body, /without Apple notarization/);
    assert(s.bodies.filter(b => b.draft === false).every(b => b.make_latest === 'false'));
    result = run(); assert.equal(result.status, 0, result.stderr);
    const before = JSON.parse(readFileSync(state)).release;
    writeFileSync(join(artifacts, m.platforms[0].file), 'different bytes');
    assert.notEqual(run().status, 0);
    assert.deepEqual(JSON.parse(readFileSync(state)).release, before);
  } finally { rmSync(directory, { recursive: true, force: true }); }
});

test('new releases use the creation response even when the list stays stale; tags are required except for nightly', () => {
  for (const version of ['0.2.0', '0.2.0-rc.1', '0.2.0-0.nightly.20261011.225745.gaaaaaaa']) {
    const directory = mkdtempSync(join(tmpdir(), 'molr-publish-create-'));
    try {
      const bin = join(directory, 'bin'), artifacts = join(directory, 'artifacts'); mkdirSync(bin); mkdirSync(artifacts);
      const state = join(directory, 'state.json');
      const reset = () => writeFileSync(state, JSON.stringify({ release: null, calls: [] })); reset();
      writeFileSync(join(bin, 'gh'), fakeGH, { mode: 0o755 });
      const mock = join(directory, 'fetch.mjs'); writeFileSync(mock, 'globalThis.fetch = async () => ({ok: true});');
      const m = fixture(version);
      writeFileSync(join(artifacts, 'manifest.json'), JSON.stringify(m));
      const desktopFiles = writeDesktop(artifacts, m);
      for (const file of m.platforms.flatMap(p => [p.file, p.file + '.sha256'])) writeFileSync(join(artifacts, file), 'artificial fixture');
      // Stale local outputs must not become release attachments.
      writeFileSync(join(artifacts, 'install.sh'), 'unused'); writeFileSync(join(artifacts, 'install.sh.sha256'), 'unused');
      const run = (extra = {}) => spawnSync(process.execPath, ['--import', mock, publisher], { encoding: 'utf8', env: { ...process.env, PATH: `${bin}:${process.env.PATH}`, MOCK_STATE: state, MOLR_DIST_DIR: artifacts, GITHUB_REPOSITORY: 'example/molr', STALE_LIST: '1', ...extra } });
      const result = run(); assert.equal(result.status, 0, result.stderr);
      const s = JSON.parse(readFileSync(state));
      assert.equal(s.calls.filter(a => a[1].includes('?per_page=')).length, 1);
      assert.equal(s.release.draft, false); assert.equal(s.release.prerelease, m.channel !== 'stable');
      assert.equal(s.release.target_commitish, m.commit);
      assert.equal(s.release.make_latest, String(m.channel === 'stable'));
      assert.deepEqual(s.release.assets.map(a => a.name), ['manifest.json', ...desktopFiles, ...m.platforms.flatMap(p => [p.file, p.file + '.sha256'])]);
      reset();
      const missingTag = run({ MISSING_TAG: '1' });
      if (m.channel === 'nightly') assert.equal(missingTag.status, 0, missingTag.stderr);
      else { assert.notEqual(missingTag.status, 0); assert.equal(JSON.parse(readFileSync(state)).release, null); }
    } finally { rmSync(directory, { recursive: true, force: true }); }
  }
});
