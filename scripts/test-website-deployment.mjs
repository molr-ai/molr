import assert from 'node:assert/strict';
import { test } from 'node:test';
import { publishedRelease, publishedReleases, verifyChannelDownloads } from './release-catalog.mjs';
import { triggerWebsite } from './trigger-website.mjs';
import { fixture } from './release-test-fixtures.mjs';

function githubRelease(m) {
 const names = [['manifest.json', `https://github.com/example/molr/releases/download/v${m.version}/manifest.json`], ['install.sh', m.installer_url, m.installer_sha256], ...m.platforms.flatMap(p => [[p.file, p.url, p.sha256], [p.file+'.sha256', p.checksum_url]])];
 return {tag_name:`v${m.version}`,draft:false,prerelease:m.channel!=='stable',assets:names.map(([name,url,digest])=>({name,browser_download_url:url,digest:digest?`sha256:${digest}`:null}))};
}
test('catalog selects the highest version of each kind from metadata and downloads only three manifests', async t => {
 const releases = [fixture('0.9.0'), fixture('0.10.0-rc.2'), fixture('0.11.0-0.nightly.20261011.2.gaaaaaaa'), fixture('0.10.0'), fixture('0.10.0-rc.10'), fixture('0.11.0-0.nightly.20261011.10.gaaaaaaa')];
 const selected = [releases[3], releases[4], releases[5]], previous = process.env.GH_TOKEN;
 delete process.env.GH_TOKEN;
 t.after(()=>{if(previous===undefined)delete process.env.GH_TOKEN;else process.env.GH_TOKEN=previous});
 const metadata = releases.map(githubRelease);
 metadata.push({...githubRelease(fixture('9.0.0')),draft:true}, {tag_name:'v8.0.0',assets:[]}, {tag_name:'legacy',assets:[{name:'manifest.json'}]});
 const calls=[];
 t.mock.method(globalThis,'fetch',async(url,options={})=>{
  calls.push({url:String(url),options});
  if(String(url).startsWith('https://api.github.com/')) {
   assert.equal(options.headers.Authorization,undefined);
   assert.equal(String(url),'https://api.github.com/repos/example/molr/releases?per_page=100&page=1');
   return Response.json(metadata);
  }
  if(options.method==='HEAD')return new Response(null,{status:200});
  assert.equal(options.headers,undefined);
  const m=selected.find(m=>String(url).endsWith(`/v${m.version}/manifest.json`));
  assert(m, `Unexpected download: ${url}`); // Old manifests and package bodies must never be fetched.
  return Response.json(m);
 });
 assert.deepEqual(await publishedReleases('example/molr'),selected);
 assert.equal(calls.length,4);
 await verifyChannelDownloads(selected);
 assert(calls.some(c=>c.url===selected[0].installer_url&&c.options.method==='HEAD'));
});
test('scan window is bounded across pages and absent kinds stay empty', async t => {
 const nightly=fixture('0.2.0-0.nightly.20261011.0.gaaaaaaa');
 const metadata=Array.from({length:10000},()=>githubRelease(nightly));
 metadata[150]=githubRelease(fixture('99.0.0'));
 const calls=[];
 t.mock.method(globalThis,'fetch',async url=>{
  calls.push(String(url));
  if(String(url).startsWith('https://api.github.com/')) {
   const query=new URL(url).searchParams, size=Number(query.get('per_page')), page=Number(query.get('page'));
   return Response.json(metadata.slice((page-1)*size,page*size));
  }
  assert(String(url).endsWith(`/v${nightly.version}/manifest.json`));
  return Response.json(nightly);
 });
 assert.deepEqual(await publishedReleases('example/molr',{limit:150}),[nightly]);
 assert.equal(calls.length,3);
 assert(calls[1].endsWith('per_page=100&page=2'));
 calls.length=0;
 assert.deepEqual(await publishedReleases('example/molr',{limit:1}),[nightly]);
 assert.equal(calls.length,2);
 assert(calls[0].endsWith('per_page=1&page=1'));
});
test('scan stops on a short page and validates the configured limit before requesting',async t=>{
 let calls=0;
 t.mock.method(globalThis,'fetch',async()=>{calls++;return Response.json([])});
 assert.deepEqual(await publishedReleases('example/molr',{limit:1000}),[]);
 assert.equal(calls,1);
 for(const limit of [0,-1,1.5,NaN,Infinity,Number.MAX_SAFE_INTEGER+1]) await assert.rejects(publishedReleases('example/molr',{limit}),/positive safe integer/);
 assert.equal(calls,1);
 const previous=process.env.MOLR_RELEASE_SCAN_LIMIT;
 t.after(()=>{if(previous===undefined)delete process.env.MOLR_RELEASE_SCAN_LIMIT;else process.env.MOLR_RELEASE_SCAN_LIMIT=previous});
 process.env.MOLR_RELEASE_SCAN_LIMIT='invalid';
 await assert.rejects(publishedReleases('example/molr'),/positive safe integer/);
});
test('selected invalid manifests fail with the release tag rather than silently falling back',async t=>{
 const latest=fixture('0.3.0'), old=fixture('0.2.0');
 t.mock.method(globalThis,'fetch',async url=>{
  if(String(url).startsWith('https://api.github.com/'))return Response.json([githubRelease(old),githubRelease(latest)]);
  assert(String(url).endsWith('/v0.3.0/manifest.json'));
  const bad=structuredClone(latest);delete bad.platforms[0].requirements.systemd;
  return Response.json(bad);
 });
 await assert.rejects(publishedReleases('example/molr'),/Release v0\.3\.0: Missing manifest fields at platforms\[0\].requirements: systemd/);
});
test('exact release lookup finds older retry tags without listing history and keeps tokens off asset hosts',async t=>{
 const m=fixture('0.1.0'),previous=process.env.GH_TOKEN;
 process.env.GH_TOKEN='artificial-test-token';
 t.after(()=>{if(previous===undefined)delete process.env.GH_TOKEN;else process.env.GH_TOKEN=previous});
 let kind='published',calls=0;
 t.mock.method(globalThis,'fetch',async(url,options)=>{
  calls++;
  if(String(url).startsWith('https://api.github.com/')) {
   assert(String(url).endsWith('/releases/tags/v0.1.0'));
   assert.equal(options.headers.Authorization,'Bearer artificial-test-token');
   if(kind==='missing')return new Response(null,{status:404});
   if(kind==='error')return new Response(null,{status:403});
   return Response.json({...githubRelease(m),draft:kind==='draft'});
  }
  assert.equal(options.headers,undefined);
  return Response.json(m);
 });
 assert.deepEqual(await publishedRelease('example/molr','0.1.0'),m);
 assert.equal(calls,2);
 kind='missing';assert.equal(await publishedRelease('example/molr','0.1.0'),null);
 kind='draft';assert.equal(await publishedRelease('example/molr','0.1.0'),null);
 kind='error';await assert.rejects(publishedRelease('example/molr','0.1.0'),/lookup failed/);
});
test('failed GitHub listing, missing assets and unavailable downloads fail closed',async t=>{
 const m=fixture(); let kind='listing';
 t.mock.method(globalThis,'fetch',async(url,options={})=>{
  if(String(url).startsWith('https://api.github.com/')) {
   if(kind==='listing')return new Response('',{status:403});
   const r=githubRelease(m);r.assets=r.assets.filter(a=>a.name!=='install.sh');return Response.json([r]);
  }
  if(options.method==='HEAD')return new Response('',{status:404});
  return Response.json(m);
 });
 await assert.rejects(publishedReleases('example/molr'),/listing failed/);
 kind='missing';await assert.rejects(publishedReleases('example/molr'),/Missing release asset/);
 await assert.rejects(verifyChannelDownloads([m]),/unavailable/);
});
test('Deploy Hook needs no authorization header and reports acknowledgement only',async t=>{
 const hook='https://api.cloudflare.com/client/v4/workers/builds/deploy_hooks/fixture-hook';let calls=0;
 t.mock.method(globalThis,'fetch',async(url,options)=>{
  calls++;assert.equal(String(url),hook);assert.equal(options.method,'POST');assert.equal(options.headers,undefined);assert.equal(options.redirect,'error');
  return new Response(JSON.stringify({success:true,result:{build_uuid:'fixture-build'}}));
 });
 await triggerWebsite(hook);assert.equal(calls,1);
 await assert.rejects(triggerWebsite(undefined),/Configure/);
 await assert.rejects(triggerWebsite('https://example.com/hook'),/must be/);
 assert.equal(calls,1);
});
test('hook failures never disclose its secret URL',async t=>{
 const hook='https://api.cloudflare.com/client/v4/workers/builds/deploy_hooks/private-fixture';
 t.mock.method(globalThis,'fetch',async()=>{throw new Error(`failed request ${hook}`)});
 await assert.rejects(triggerWebsite(hook),error=>!String(error).includes('private-fixture')&&!String(error).includes('api.cloudflare.com'));
});
