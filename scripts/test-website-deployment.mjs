import assert from 'node:assert/strict';
import { test } from 'node:test';
import { publishedReleases, verifyChannelDownloads } from './release-catalog.mjs';
import { triggerWebsite } from './trigger-website.mjs';
import { fixture } from './release-test-fixtures.mjs';

function githubRelease(m) {
 const names = [['manifest.json', `https://github.com/example/molr/releases/download/v${m.version}/manifest.json`], ['install.sh', m.installer_url, m.installer_sha256], ...m.platforms.flatMap(p => [[p.file, p.url, p.sha256], [p.file+'.sha256', p.checksum_url]])];
 return {tag_name:`v${m.version}`,draft:false,prerelease:m.channel!=='stable',assets:names.map(([name,url,digest])=>({name,browser_download_url:url,digest:digest?`sha256:${digest}`:null}))};
}
test('Cloudflare can read all public releases without gh or credentials', async t => {
 const m=fixture('0.2.0-rc.1'), previous=process.env.GH_TOKEN;
 delete process.env.GH_TOKEN;
 t.after(()=>{if(previous===undefined)delete process.env.GH_TOKEN;else process.env.GH_TOKEN=previous});
 const calls=[];
 t.mock.method(globalThis,'fetch',async(url,options={})=>{
  calls.push({url:String(url),options});
  if(String(url).startsWith('https://api.github.com/')) {
   assert.equal(options.headers.Authorization,undefined);
   return new Response(JSON.stringify(String(url).endsWith('page=1')?Array.from({length:100},()=>({draft:true})):[githubRelease(m)]));
  }
  if(options.method==='HEAD')return new Response(null,{status:200});
  assert.equal(options.headers,undefined); // Repository credentials never go to asset hosts.
  return new Response(JSON.stringify(m));
 });
 const releases=await publishedReleases('example/molr');
 assert.deepEqual(releases,[m]);await verifyChannelDownloads(releases);
 assert.equal(calls.filter(c=>c.url.startsWith('https://api.github.com/')).length,2);
 assert(calls.some(c=>c.url===m.installer_url&&c.options.method==='HEAD'));
});
test('failed GitHub listing, missing assets and unavailable downloads fail closed',async t=>{
 const m=fixture(); let kind='listing';
 t.mock.method(globalThis,'fetch',async(url,options={})=>{
  if(String(url).startsWith('https://api.github.com/')) {
   if(kind==='listing')return new Response('',{status:403});
   const r=githubRelease(m);r.assets=r.assets.filter(a=>a.name!=='install.sh');return new Response(JSON.stringify([r]));
  }
  if(options.method==='HEAD')return new Response('',{status:404});
  return new Response(JSON.stringify(m));
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
