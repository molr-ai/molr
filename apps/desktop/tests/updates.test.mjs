import assert from 'node:assert/strict';
import test from 'node:test';
import { DesktopUpdates } from '../dist/main/main/updates.js';
import { compareVersions } from '../dist/main/version.js';
const catalog = (version = '0.2.0', os = 'win32', arch = 'amd64') => ({ schema: 1, channels: { stable: { version, platforms: [{ os, arch, variant: 'full', signed: false, notarized: false, sha256: 'a'.repeat(64), file: `molr-desktop_${version}_${os}-${arch}_full.${os==='darwin'?'dmg':os==='linux'?'deb':'exe'}`, url: `https://downloads.example.com/molr-desktop_${version}_${os}-${arch}_full.${os==='darwin'?'dmg':os==='linux'?'deb':'exe'}` }] } } });
test('SemVer comparison prevents downgrades and excludes development builds', () => {
  for (const [a,b,n] of [['0.1.0','0.2.0',-1],['0.3.0','0.2.0',1],['0.2.0+one','0.2.0+two',0],['0.2.0-rc.2','0.2.0-rc.10',-1],['0.2.0-rc.1','0.2.0',-1],['0.2.0-0.nightly.20261020.100.gabcdef','0.2.0-beta.1',-1],['0.2.0-a.1','0.2.0-a',1]]) assert.equal(compareVersions(a,b),n);
  for (const v of ['development','0.1.0-local','0.1.0-dev','01.2.0','0.1.0-01']) assert.equal(compareVersions(v,'0.2.0'),undefined);
});
test('desktop checks independently using native platform, coalesces and retains success after failure', async () => {
  let now=100000, calls=0, fail=false, finish;
  const service=new DesktopUpdates('0.1.0','win32','amd64',()=>{}, async url=>{
    calls++; assert.equal(url,'https://molr.ai/releases/desktop/catalog.json');
    if(fail) throw new Error('offline');
    await new Promise(r=>{finish=r;}); return Response.json(catalog());
  },()=>now);
  const first=service.check(); assert.equal(service.check(),first); assert(service.snapshot().checking);
  finish(); assert.equal((await first).comparison,'available');
  assert.equal((await service.check()).latestVersion,'0.2.0'); assert.equal(calls,1);
  now+=60001; fail=true;
  const failed=await service.check(); assert.equal(failed.error,'check_failed'); assert.equal(failed.latestVersion,'0.2.0'); assert.equal(failed.checking,false);
  now+=60001; await service.check(true); assert.equal(calls,2);
});
test('empty catalog, missing platform, development, malformed and oversized metadata never imply up to date', async () => {
  for(const body of [{schema:1,channels:{stable:null}},catalog('0.2.0','darwin')]) {
    const s=new DesktopUpdates('0.1.0','win32','amd64',()=>{},async()=>Response.json(body));
    assert.equal((await s.check()).comparison,'unavailable');
  }
  const dev=new DesktopUpdates('0.1.0-local','linux','amd64',()=>{},async()=>{throw new Error('must not fetch');});
  assert.equal((await dev.check()).comparison,'uncomparable'); assert.equal(dev.snapshot().error,undefined);
  for(const body of ['invalid',JSON.stringify(catalog('0.2.0-rc.1')),JSON.stringify({schema:1,channels:{}}),' '.repeat(262145)]) {
    const s=new DesktopUpdates('0.1.0','win32','amd64',()=>{},async()=>new Response(body));
    const value=await s.check(); assert.equal(value.comparison,'unknown'); assert.equal(value.error,'check_failed');
  }
});
test('desktop compares its own version even when the service could already be current', async () => {
  for(const [version, expected] of [['0.1.0','available'],['0.2.0','current'],['0.3.0','ahead']]) {
    const s=new DesktopUpdates(version,'win32','amd64',()=>{},async()=>Response.json(catalog()));
    assert.equal((await s.check()).comparison,expected);
  }
});
