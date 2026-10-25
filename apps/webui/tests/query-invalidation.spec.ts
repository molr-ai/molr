import { test, expect } from '@playwright/test';
import { eventInvalidation, expandTopics, mutationTopics, queryTopic } from '../src/query-invalidation';

test('queries retain conversation, assistant and independent detail scopes', () => {
  for (const id of ['a', 'b']) {
    expect(queryTopic(`/api/conversations/${id}/messages?before=50`)).toBe(`messages:${id}`);
    expect(queryTopic(`/api/assistants/${id}/chat-channel`)).toBe('chat-channel');
    expect(queryTopic(`/api/assistants/${id}/status`)).toBe(`status:${id}`);
    expect(queryTopic(`/api/assistants/${id}/capabilities`)).toBe(`capabilities:${id}`);
    expect(queryTopic(`/api/assistants/${id}/shell-tasks?active=true`)).toBe(`shell-tasks:${id}`);
    for (const channel of ['qq', 'feishu', 'weixin', 'dingtalk', 'slack', 'telegram']) {
      expect(queryTopic(`/api/assistants/${id}/${channel}`)).toBe(channel);
    }
  }
  expect(queryTopic('/api/status')).toBe('runtime');
  expect(queryTopic('/api/debug/runs/run/steps/1')).toBe('debug-payload');
  expect(queryTopic('/api/assistants/activity?archived=false')).toBe('activity');
  for (const path of ['/api/account/quotas', '/api/admin/usage', '/api/account/usage?from=2026-01-01', '/api/admin/usage/analytics?group=day']) expect(queryTopic(path)).toBe('usage');
  expect(queryTopic('/api/plans?from=2026-01-01')).toBe('plans');
});

test('message events preserve exact-message reconciliation and fallback refresh', () => {
  const message = { Object: { ID: 'message' }, ConversationID: 'chat' };
  expect(eventInvalidation({ ...message, Topic: 'message.feedback' })).toEqual({ messageID: 'message', topics: [] });
  for (const Topic of ['message.accepted', 'message.read']) {
    expect(eventInvalidation({ ...message, Topic })).toEqual({ messageID: 'message', topics: ['status', 'conversations', 'tasks', 'memories', 'usage'] });
  }
  expect(eventInvalidation({ Topic: 'conversation.read', ...message })).toEqual({ topics: ['conversations'] });
  expect(eventInvalidation({ Topic: 'shell.tasks', Object: { ID: 'assistant' } })).toEqual({ topics: ['shell-tasks:assistant'] });
  expect(eventInvalidation({ Topic: 'assistant.profile', Object: { ID: 'assistant' } })).toEqual({ topics: ['assistants', 'conversations'] });
  expect(eventInvalidation({ Topic: 'assistant.changed', Object: { ID: 'assistant' } }).topics).toEqual(['status:assistant', 'activity', 'tasks', 'dreams', 'memories', 'usage']);
  expect(eventInvalidation({ Topic: 'notification.changed' })).toEqual({ topics: ['notifications'] });
  for (const channel of ['qq', 'feishu', 'weixin', 'dingtalk', 'slack', 'telegram']) expect(eventInvalidation({ Topic: `${channel}.changed` })).toEqual({ topics: [channel] });
  expect(eventInvalidation({ Topic: 'permission.request' }).topics).toEqual(['permission-requests', 'grants', 'status']);
  const fallback = ['activity', 'conversations', 'status', 'tasks', 'memories', 'usage'];
  expect(eventInvalidation({ Topic: 'future.event', ConversationID: 'chat' })).toEqual({ topics: ['messages:chat', ...fallback] });
  expect(eventInvalidation({ Topic: 'message.feedback' })).toEqual({ topics: ['messages', ...fallback] });
  expect(eventInvalidation({ Topic: 'toString' })).toEqual({ topics: ['messages', ...fallback] });
});

test('input and caller-managed mutations avoid broad reloads; business writes retain dependent scopes', () => {
  for (const path of ['/api/assistants/a/browser', '/api/assistants/a/shell-tasks', '/api/conversations/feedback', '/api/updates/check', '/api/weixin/poll', '/api/weixin/cancel', '/api/telegram/poll', '/api/telegram/cancel']) expect(mutationTopics(path)).toEqual([]);
  expect(mutationTopics('/api/conversations/read')).toEqual(['conversations']);
  expect(mutationTopics('/api/conversations/messages')).toEqual(['conversations', 'status']);
  expect(mutationTopics('/api/assistants')).toEqual(['assistants', 'conversations']);
  expect(mutationTopics('/api/assistants/a/stop')).toEqual(['assistants', 'conversations', 'status', 'schedules', 'grants']);
  for (const channel of ['weixin', 'dingtalk', 'slack', 'telegram']) expect(mutationTopics(`/api/${channel}/disconnect`)).toEqual([channel]);
  expect(mutationTopics('/api/sandboxes/node')).toEqual(['sandboxes', 'resources', 'grants', 'capabilities', 'assistants']);
  expect(mutationTopics('/api/permission-requests/request')).toEqual(['permission-requests', 'grants', 'status']);
  expect(mutationTopics('/api/mcp/connect')).toEqual(['mcp', 'tools', 'resources', 'grants', 'capabilities', 'admin', 'usage']);
  expect(mutationTopics('/api/resources/provider')).toEqual(['resources', 'models', 'grants']);
  expect(mutationTopics('/api/settings')).toEqual(['settings']);
});

test('dependent invalidations preserve batching, input and one-step expansion', () => {
  const source = ['settings', 'messages:chat', 'tasks', 'grants', 'tasks'];
  expect(expandTopics(source)).toEqual(['settings', 'messages:chat', 'tasks', 'grants', 'dreams', 'plans', 'activity', 'usage', 'capabilities']);
  expect(source).toEqual(['settings', 'messages:chat', 'tasks', 'grants', 'tasks']);
  expect(expandTopics(['status:a'])).toEqual(['status:a', 'activity']);
  expect(expandTopics(['resources'])).toEqual(['resources', 'capabilities']);
  expect(expandTopics(['account'])).toEqual(['account', 'usage']);
  expect(expandTopics([])).toEqual([]);
  expect(expandTopics(['debug'])).toEqual(['debug']);
  for (const channel of ['qq', 'feishu', 'weixin', 'dingtalk', 'slack', 'telegram']) expect(expandTopics([channel])).toEqual([channel, 'chat-channel']);
  // Expansion must not fan out into unrelated conversations or statuses.
  expect(expandTopics(['messages:a'])).toEqual(['messages:a', 'activity']);
});
