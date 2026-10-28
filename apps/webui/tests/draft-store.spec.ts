import { test, expect } from '@playwright/test';
import { DraftStore } from '../src/draft-store';

test('draft writes and subscriptions stay within one assistant and account', () => {
  const values = new Map<string, string>(), writes: string[] = [];
  const storage = { getItem: (key: string) => values.get(key) ?? null, setItem(key: string, value: string) { writes.push(key); values.set(key, value); } };
  const store = new DraftStore('owner', storage);
  const untouched = store.snapshot('other');
  let first = 0, second = 0;
  const dispose = store.subscribe('first', () => first++);
  store.subscribe('other', () => second++);
  const sent = { text: 'sent', files: [] };
  store.update('first', sent);
  store.update('first', previous => previous);
  expect(writes).toEqual(['molr-chat-draft:owner:first']);
  expect(first).toBe(1); expect(second).toBe(0);
  expect(store.snapshot('other')).toBe(untouched);
  store.update('first', { text: 'typed while sending', files: [] });
  store.update('first', current => current === sent ? { text: '', files: [] } : current);
  expect(store.snapshot('first').text).toBe('typed while sending');
  expect(new DraftStore('owner', storage).snapshot('first')).toEqual(store.snapshot('first'));
  expect(new DraftStore('another-owner', storage).snapshot('first').text).toBe('');
  dispose(); store.update('first', { text: 'after unmount', files: [] });
  expect(first).toBe(2);
});

test('unavailable storage and invalid persisted drafts retain usable in-memory snapshots', () => {
  for (const getItem of [() => '{invalid', () => JSON.stringify({ text: 1, files: [] }), () => { throw new Error('disabled'); }]) {
    const store = new DraftStore('owner', { getItem, setItem() { throw new Error('quota'); } });
    expect(store.snapshot('first')).toEqual({ text: '', files: [] });
    store.update('first', { text: 'retained', files: [] });
    expect(store.snapshot('first').text).toBe('retained');
  }
});

test('pending clipboard text survives draft reload and remains isolated', () => {
 const values=new Map<string,string>();const storage={getItem:(key:string)=>values.get(key)??null,setItem:(key:string,value:string)=>values.set(key,value)};
 const store=new DraftStore('owner',storage), initial=store.snapshot('a');
 const pending={id:'retry-key',name:'paste.txt',text:'原文\r\n'.repeat(1000)};
 store.update('a',{...initial,pendingTexts:[pending]});
 expect(new DraftStore('owner',storage).snapshot('a').pendingTexts).toEqual([pending]);
 expect(store.snapshot('b').pendingTexts).toBeUndefined();
 expect(new DraftStore('other',storage).snapshot('a').pendingTexts).toBeUndefined();
 store.update('a',current=>({...current,pendingTexts:[]}));
 expect(store.snapshot('a').pendingTexts).toEqual([]);
});
