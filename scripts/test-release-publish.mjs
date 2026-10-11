import assert from 'node:assert/strict';
import { test } from 'node:test';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { resolve, join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { fixture } from './release-test-fixtures.mjs';
const publisher = resolve(import.meta.dirname, 'publish-release.mjs');
const fakeGH = `#!/usr/bin/env python3
import os,json,sys,pathlib,base64
p=pathlib.Path(os.environ['MOCK_STATE']); s=json.loads(p.read_text()); a=sys.argv[1:]
s['calls'].append(a)
try:
 if a[0]=='api':
  if '/releases/assets/' in a[1]:
   item=next(x for x in s['release']['assets'] if x['id']==int(a[1].rsplit('/',1)[1])); sys.stdout.buffer.write(base64.b64decode(item['data']))
  else: print(json.dumps([[s['release']] if s['release'] else []]))
 elif a[:2]==['release','create']:
  s['release']={'tag_name':a[2],'draft':True,'prerelease':'--prerelease' in a,'assets':[]}
 elif a[:2]==['release','upload']:
  if os.environ.get('FAIL_UPLOAD') and len(s['release']['assets'])==2: sys.exit(1)
  f=pathlib.Path(a[3]); s['release']['assets'].append({'id':len(s['release']['assets'])+1,'name':f.name,'data':base64.b64encode(f.read_bytes()).decode()})
 elif a[:2]==['release','edit']:
  s['release']['draft']=False; s['release']['prerelease']='--prerelease=true' in a
 else: raise Exception('Unexpected gh invocation')
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
    for (const file of ['install.sh', 'install.sh.sha256', ...m.platforms.flatMap(p => [p.file, p.file+'.sha256'])]) writeFileSync(join(artifacts, file), 'artificial public fixture');
    const run = (extra = {}) => spawnSync(process.execPath, ['--import', mock, publisher], { encoding: 'utf8', env: { ...process.env, PATH: `${bin}:${process.env.PATH}`, MOCK_STATE: state, MOLR_DIST_DIR: artifacts, GITHUB_REPOSITORY: 'example/molr', ...extra } });
    assert.notEqual(run({ FAIL_UPLOAD: '1' }).status, 0);
    let s = JSON.parse(readFileSync(state)); assert(s.release.draft); assert.equal(s.release.assets.length, 2);
    let result = run(); assert.equal(result.status, 0, result.stderr);
    s = JSON.parse(readFileSync(state)); assert.equal(s.release.draft, false); assert.equal(s.release.assets.length, 7);
    assert(s.calls.filter(a => a[1] === 'edit').every(a => a.includes('--latest=false')));
    result = run(); assert.equal(result.status, 0, result.stderr);
    const before = JSON.parse(readFileSync(state)).release;
    writeFileSync(join(artifacts, 'install.sh'), 'different bytes');
    assert.notEqual(run().status, 0);
    assert.deepEqual(JSON.parse(readFileSync(state)).release, before);
  } finally { rmSync(directory, { recursive: true, force: true }); }
});
