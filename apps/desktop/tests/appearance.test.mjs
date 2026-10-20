import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtempSync, rmSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { AppearanceStore } from '../dist/main/main/appearance.js';

test('appearance restores the last service preference and preserves system-following mode', t => {
  const dir = mkdtempSync(join(tmpdir(),'molr-appearance-')); t.after(()=>rmSync(dir,{recursive:true,force:true}));
  const file = join(dir,'appearance.json'), store = new AppearanceStore(file);
  assert.deepEqual(store.value,{theme:'system',palette:'forest'});
  assert.equal(store.set({theme:'dark',palette:'ocean'}),true);
  assert.deepEqual(new AppearanceStore(file).value,{theme:'dark',palette:'ocean'});
  const next = {theme:'system',palette:'rose'};
  store.set(next); next.palette='iris';
  assert.deepEqual(new AppearanceStore(file).value,{theme:'system',palette:'rose'});
  assert.deepEqual(store.value,{theme:'system',palette:'rose'});
  assert.equal(store.set({theme:'system',palette:'rose'}),false);
});
test('appearance accepts only the narrow theme/palette schema and corrupt preferences fall back', t => {
  const dir = mkdtempSync(join(tmpdir(),'molr-appearance-invalid-')); t.after(()=>rmSync(dir,{recursive:true,force:true}));
  const file = join(dir,'appearance.json'), store = new AppearanceStore(file);
  store.set({theme:'light',palette:'sand'}); const before=readFileSync(file,'utf8');
  for (const value of [null,[],{},'dark',{theme:'auto',palette:'forest'},{theme:'dark',palette:'url(https://example.com)'},{theme:'dark',palette:'forest',css:'arbitrary'},Object.assign(Object.create({theme:'dark',palette:'forest'}),{a:1,b:2})]) assert.throws(()=>store.set(value));
  assert.equal(readFileSync(file,'utf8'),before);
  for (const data of ['{broken','{"theme":"dark","palette":"unknown"}', 'x'.repeat(2048)]) {
    writeFileSync(file,data); assert.deepEqual(new AppearanceStore(file).value,{theme:'system',palette:'forest'});
  }
});
