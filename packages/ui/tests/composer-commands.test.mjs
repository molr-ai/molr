import assert from 'node:assert/strict';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { createServer } from 'vite';

test('command levels, aliases and summary labels resolve without inventing targets', async () => {
 const server=await createServer({configFile:false,root:fileURLToPath(new URL('..',import.meta.url)),server:{middlewareMode:true,hmr:false}});
 try {
  const {resolveComposerCommands:resolve}=await server.ssrLoadModule('/src/components/composer-commands.ts');
  const targets=[{name:'all',aliases:['全部'],description:'All pending requests'},{name:'request-42',label:'Temporary hiring',description:'Compare reports',keywords:['Compare reports']}];
  const commands=[{name:'compress',aliases:['压缩'],description:'Compress',keywords:['上下文']},{name:'permission',aliases:['权限审批'],description:'Permissions',children:[{name:'approve',aliases:['同意'],description:'Approve',children:targets},{name:'reject',aliases:['拒绝'],description:'Reject',children:targets}]}];
  const names=value=>resolve(value,commands)?.matches.map(c=>c.name);
  assert.deepEqual(names('/'),['compress','permission']);
  assert.deepEqual(names('/pe'),['permission']);
  assert.equal(resolve('/pe',commands).matches[0].completion,'/permission ');
  assert.deepEqual(names('/permission '),['approve','reject']);
  assert.deepEqual(names('/permission a'),['approve']);
  assert.equal(resolve('/permission a',commands).matches[0].completion,'/permission approve ');
  assert.deepEqual(names('/permission approve '),['all','request-42']);
  const target=resolve('/permission approve reports',commands).matches[0];
  assert.equal(target.label,'Temporary hiring');assert.equal(target.completion,'/permission approve request-42');assert.equal(target.hasChildren,false);
  assert.deepEqual(names('/权限审批 '),['同意','拒绝']);
  assert.equal(resolve('/权限审批 同意 ',commands).matches[0].completion,'/权限审批 同意 全部');
  assert.equal(resolve('/权限审批 同意 request',commands).matches[0].completion,'/权限审批 同意 request-42');
  assert.deepEqual(names('/permission approve'),['approve']); // backspacing across a separator returns one level
  assert.deepEqual(names('/pe approve '),[]);
  assert.deepEqual(names('/permission invalid '),[]);
  assert.deepEqual(names('/permission approve all extra'),[]);
  assert.equal(resolve('ordinary / text',commands),undefined);
  assert.equal(resolve('/work/file',commands),undefined);
  assert.equal(resolve('/permission\napprove',commands),undefined);
  assert.deepEqual(resolve('/permission ',[{...commands[1],disabled:true}]).matches,[]);
  assert.deepEqual(names('/上下文'),['compress']);
  const changed=[commands[0],{...commands[1],children:[{...commands[1].children[0],children:targets.slice(0,1)}]}];
  assert.deepEqual(resolve('/permission approve ',changed).matches.map(c=>c.name),['all']);
 }finally{await server.close();}
});
