import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync, readdirSync, symlinkSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { generateDocumentation } from './generate-molr-docs.mjs';

function markdownFiles(directory, excludeEnglish = false) {
 return readdirSync(directory,{withFileTypes:true}).flatMap(entry=>{
  if(excludeEnglish && entry.name==='en') return [];
  if(entry.isDirectory()) return markdownFiles(join(directory,entry.name)).map(name=>`${entry.name}/${name}`);
  return entry.name.endsWith('.md') ? [entry.name] : [];
 }).sort();
}

test('every public Chinese document is registered, indexed, bundled, and current', () => {
 const root=dirname(import.meta.dirname);
 const sourceFiles=markdownFiles(join(root,'docs'),true);
 const routes=JSON.parse(readFileSync(join(root,'docs-routes.json'),'utf8'));
 const output=join(root,'runtime/internal/molrdocs/content');
 const index=JSON.parse(readFileSync(join(output,'references/index.json'),'utf8'));
 assert.deepEqual(routes.map(route=>route.file).sort(),sourceFiles,'Public documents and docs-routes.json differ; register new documents and remove deleted entries');
 assert.deepEqual(markdownFiles(join(output,'references/docs')),sourceFiles,'Bundled documents are missing or obsolete; run npm run docs:generate');
 const prefix='/molr/.agents/skills/molr-docs/references/docs/';
 const indexedFiles=index.map(entry=>{
  assert(entry.path.startsWith(prefix),`Unexpected bundled path: ${entry.path}`);
  assert.equal(entry.contentLanguage,'zh');
  return entry.path.slice(prefix.length);
 }).sort();
 assert.deepEqual(indexedFiles,sourceFiles,'Bundle index is missing or duplicates documents, or includes obsolete entries');
 assert.equal(generateDocumentation(root,{check:true}).documents,sourceFiles.length);
});

function fixture(t) {
 const root = mkdtempSync(join(tmpdir(), 'molr-docs-'));
 t.after(() => rmSync(root, { recursive: true, force: true }));
 const write = (name, text) => { const p = join(root, name); mkdirSync(dirname(p), { recursive: true }); writeFileSync(p, text); };
 write('docs-routes.json', JSON.stringify([{root:'docs', file:'README.md', path:'/docs/', label:'指南', group:'入门', englishLabel:'Guide', englishGroup:'Start'}]));
 write('docs/README.md', '# 中文指南\n仅包含已审查资料。\n');
 write('docs/en/README.md', '# English text must not be bundled');
 write('docs/unlisted.md', 'unreviewed');
 write('internal-docs/private.md', 'private');
 write('runtime/internal/molrdocs/SKILL.md', '---\nname: molr-docs\ndescription: Query Molr documentation\n---\nRead references.\n');
 return {root,write,read:name=>readFileSync(join(root,'runtime/internal/molrdocs/content',name),'utf8')};
}
test('only reviewed Chinese files; deterministic output and source freshness', t => {
 const f=fixture(t), first=generateDocumentation(f.root);
 assert.equal(first.documents,1);
 const index=JSON.parse(f.read('references/index.json'));
 assert.deepEqual(index.map(x=>x.contentLanguage),['zh']);
 assert.deepEqual(index[0].titles,{zh:'指南',en:'Guide'});
 assert.deepEqual(index[0].topics,{zh:'入门',en:'Start'});
 assert.deepEqual(index[0].urls,{zh:'https://molr.ai/zh/docs/',en:'https://molr.ai/en/docs/'});
 assert.equal(index[0].path,'/molr/.agents/skills/molr-docs/references/docs/README.md');
 assert.deepEqual(readdirSync(join(f.root,'runtime/internal/molrdocs/content/references/docs')),['README.md']);
 assert.match(f.read('SKILL.md'),new RegExp(first.revision));
 assert.deepEqual(generateDocumentation(f.root,{check:true}),first);
 f.write('docs/en/README.md','English update');
 assert.deepEqual(generateDocumentation(f.root,{check:true}),first);
 f.write('docs/README.md','# 中文文档更新');
 assert.throws(()=>generateDocumentation(f.root,{check:true}),/Stale/);
 const second=generateDocumentation(f.root);
 assert.notEqual(second.revision,first.revision);
});
test('English metadata changes refresh the bundle without adding English content', t => {
 const f=fixture(t), first=generateDocumentation(f.root);
 const body=f.read('references/docs/README.md');
 const routes=JSON.parse(readFileSync(join(f.root,'docs-routes.json'),'utf8'));
 routes[0].englishLabel='Updated English guide';
 f.write('docs-routes.json',JSON.stringify(routes));
 assert.throws(()=>generateDocumentation(f.root,{check:true}),/Stale/);
 const second=generateDocumentation(f.root);
 assert.notEqual(second.revision,first.revision);
 assert.equal(f.read('references/docs/README.md'),body);
 assert.equal(JSON.parse(f.read('references/index.json'))[0].titles.en,'Updated English guide');
 assert.deepEqual(readdirSync(join(f.root,'runtime/internal/molrdocs/content/references/docs')),['README.md']);
});
test('manifest additions enter the bundle and removed documents leave no stale files', t => {
 const f=fixture(t);
 generateDocumentation(f.root);
 const routes=JSON.parse(readFileSync(join(f.root,'docs-routes.json'),'utf8'));
 const added={...routes[0],file:'dev/contributing/acceptance.md',path:'/docs/dev/contributing/acceptance/',label:'验收规范',englishLabel:'Acceptance'};
 f.write('docs/dev/contributing/acceptance.md','# 验收规范\n交付要求。\n');
 f.write('docs-routes.json',JSON.stringify([...routes,added]));
 assert.throws(()=>generateDocumentation(f.root,{check:true}),/Stale/);
 assert.equal(generateDocumentation(f.root).documents,2);
 assert.match(f.read('references/docs/dev/contributing/acceptance.md'),/交付要求/);
 f.write('docs-routes.json',JSON.stringify([added]));
 rmSync(join(f.root,'docs/README.md'));
 assert.throws(()=>generateDocumentation(f.root,{check:true}),/Stale/);
 const result=generateDocumentation(f.root);
 assert.equal(result.documents,1);
 assert.equal(existsSync(join(f.root,'runtime/internal/molrdocs/content/references/docs/README.md')),false);
 assert.deepEqual(JSON.parse(f.read('references/index.json')).map(entry=>entry.path),['/molr/.agents/skills/molr-docs/references/docs/dev/contributing/acceptance.md']);
 assert.deepEqual(generateDocumentation(f.root,{check:true}),result);
});
test('linked public files stay references and private or symlink input fails', t=>{
 const f=fixture(t);
 f.write('docs/README.md','[public](unlisted.md)');
 generateDocumentation(f.root);
 assert.match(f.read('references/docs/README.md'),/https:\/\/github.com\/molr-ai\/molr\/blob\/main\/docs\/unlisted.md/);
 f.write('docs/README.md','[private](../internal-docs/private.md)');
 assert.throws(()=>generateDocumentation(f.root),/Private/);
 rmSync(join(f.root,'docs/README.md'));
 symlinkSync(join(f.root,'internal-docs/private.md'),join(f.root,'docs/README.md'));
 assert.throws(()=>generateDocumentation(f.root),/regular public/);
});
test('missing registered document and duplicate routes fail', t=>{
 const f=fixture(t);
 rmSync(join(f.root,'docs/README.md'));
 assert.throws(()=>generateDocumentation(f.root));
 f.write('docs-routes.json',JSON.stringify([{root:'internal-docs',file:'private.md'}]));
 assert.throws(()=>generateDocumentation(f.root),/Invalid/);
});
