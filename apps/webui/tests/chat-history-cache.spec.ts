import assert from 'node:assert/strict';
import { test } from '@playwright/test';
import type { Message } from '@molr/sdk';
import { ChatHistoryCache } from '../src/chat-history-cache';

const snapshot = (id: string, revision = 0) => ({ messages: [{ ID: id } as Message], olderCursor: '', metadataVersions: { [id]: revision } });

test('uncached conversation events do not retain metadata and cached events stay scoped', () => {
  const cache = new ChatHistoryCache();
  for (let i = 0; i < 20_000; i++) cache.invalidate(`message-${i}`, `conversation-${i}`);
  cache.set('first', snapshot('first-message'));
  cache.set('second', snapshot('second-message'));
  const revision = cache.invalidate('first-message', 'first');
  assert.equal(cache.changed('first', 'first-message'), revision);
  assert.equal(cache.changed('second', 'first-message'), 0);
  const release = cache.retain('conversation-0');
  assert.equal(cache.changed('conversation-0', 'message-0'), 0);
  const read = cache.beginRead('second');
  assert.equal(typeof read.revision, 'number');
  assert.ok(read.revision >= revision);
  read.finish(); release();
});

test('history eviction removes its metadata while active and pending reads remain pinned', () => {
  const cache = new ChatHistoryCache();
  const release = cache.retain('active');
  cache.set('active', snapshot('active-message'));
  cache.set('old', snapshot('old-message'));
  cache.invalidate('old-message', 'old');
  const read = cache.beginRead('pending');
  for (let i = 0; i < 200; i++) cache.set(`chat-${i}`, snapshot(`message-${i}`));
  assert.equal(cache.get('old'), undefined);
  assert.equal(cache.changed('old', 'old-message'), 0);
  assert.equal(cache.get('active')!.messages[0].ID, 'active-message');
  const late = cache.invalidate('late-message', 'pending');
  assert.equal(cache.changed('pending', 'late-message'), late);
  read.finish();
  assert.equal(cache.changed('pending', 'late-message'), 0);
  release();
  cache.set('newest', snapshot('newest-message'));
  cache.set('another', snapshot('another-message'));
  assert.equal(cache.get('active'), undefined);
});

test('page boundaries preserve changes arriving in flight and discard unreturned message changes', () => {
  const cache = new ChatHistoryCache();
  const release = cache.retain('chat');
  const read = cache.beginRead('chat');
  const revision = cache.invalidate('message', 'chat');
  cache.invalidate('not-in-page', 'chat');
  cache.set('chat', snapshot('message', read.revision));
  read.finish();
  assert.ok(cache.changed('chat', 'message') > read.revision, 'A stale page must require a metadata read');
  assert.equal(cache.changed('chat', 'not-in-page'), 0, 'Unloaded messages will be read fresh');
  cache.set('chat', snapshot('message', revision));
  assert.equal(cache.changed('chat', 'message'), 0, 'Acknowledged changes no longer occupy the dirty map');
  release();
});

test('unknown-conversation events find cached messages and are retained across initial page reads', () => {
  const cache = new ChatHistoryCache();
  cache.set('first', snapshot('first-message'));
  cache.set('other', snapshot('other-message'));
  const release = cache.retain('initial');
  const read = cache.beginRead('initial');
  const known = cache.invalidate('first-message');
  const unknown = cache.invalidate('new-message');
  assert.equal(cache.changed('first', 'first-message'), known);
  assert.equal(cache.changed('other', 'first-message'), 0);
  cache.set('initial', snapshot('new-message', read.revision));
  read.finish();
  assert.equal(cache.changed('initial', 'new-message'), unknown);
  assert.equal(cache.changed('initial', 'first-message'), 0);
  release();
});

test('access revocation and account teardown remove both snapshots and metadata', () => {
  const cache = new ChatHistoryCache();
  const release = cache.retain('chat');
  cache.set('chat', snapshot('message'));
  cache.invalidate('message', 'chat');
  cache.delete('chat');
  assert.equal(cache.get('chat'), undefined);
  assert.equal(cache.changed('chat', 'message'), 0);
  cache.set('chat', snapshot('message'));
  cache.invalidate('message');
  cache.clear();
  assert.equal(cache.get('chat'), undefined);
  assert.equal(cache.changed('chat', 'message'), 0);
  assert.equal(new ChatHistoryCache().changed('chat', 'message'), 0);
  release();
});
