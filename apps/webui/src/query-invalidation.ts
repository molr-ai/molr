import type { RuntimeEvent } from './notifications/types';

export function queryTopic(path: string) {
  if (/\/assistants\/[^/]+\/chat-channel$/.test(path)) return 'chat-channel';
  if (path === '/api/status') return 'runtime';
  const shell = path.match(/^\/api\/assistants\/([^/]+)\/shell-tasks/);
  if (shell) return `shell-tasks:${shell[1]}`;
  if (/\/assistants\/[^/]+\/qq/.test(path)) return 'qq';
  if (/\/assistants\/[^/]+\/feishu/.test(path)) return 'feishu';
  if (/\/assistants\/[^/]+\/weixin/.test(path)) return 'weixin';
  if (/\/assistants\/[^/]+\/dingtalk/.test(path)) return 'dingtalk';
  if (/\/assistants\/[^/]+\/slack/.test(path)) return 'slack';
  if (/\/assistants\/[^/]+\/telegram/.test(path)) return 'telegram';
  if (path === '/api/account/quotas' || path === '/api/admin/usage' || path.startsWith('/api/account/usage?') || path.startsWith('/api/admin/usage/analytics?')) return 'usage';
  if (/\/debug\/runs\/[^/]+\/steps\//.test(path)) return 'debug-payload';
  const assistant = path.match(/^\/api\/assistants\/([^/]+)\/(capabilities|status)$/);
  if (assistant) return `${assistant[2]}:${assistant[1]}`;
  const conversation = path.match(/^\/api\/conversations\/([^/]+)\/messages/);
  if (conversation) return `messages:${conversation[1]}`;
  if (path.startsWith('/api/assistants/activity')) return 'activity';
  return path.split('/')[2]?.split('?')[0] ?? '';
}

const eventTopics: Record<string, string[]> = {
  'notification.changed': ['notifications'],
  'qq.changed': ['qq'], 'feishu.changed': ['feishu'], 'weixin.changed': ['weixin'],
  'dingtalk.changed': ['dingtalk'], 'slack.changed': ['slack'], 'telegram.changed': ['telegram'],
  'assistant.profile': ['assistants', 'conversations'],
  'permission.request': ['permission-requests', 'grants', 'status'],
  'conversation.read': ['conversations'],
};

type EventInvalidation = { topics: string[]; messageID?: string };
export function eventInvalidation(event: RuntimeEvent): EventInvalidation {
  if (event.Topic === 'shell.tasks' && event.Object?.ID) return { topics: [`shell-tasks:${event.Object.ID}`] };
  if (Object.hasOwn(eventTopics, event.Topic)) return { topics: eventTopics[event.Topic] };
  if (event.Topic.startsWith('assistant.') && event.Object?.ID) return { topics: [`status:${event.Object.ID}`, 'activity', 'tasks', 'dreams', 'memories', 'usage'] };
  if (['message.feedback', 'message.accepted', 'message.read'].includes(event.Topic) && event.Object?.ID) {
    return { messageID: event.Object.ID, topics: event.Topic === 'message.feedback' ? [] : ['status', 'conversations', 'tasks', 'memories', 'usage'] };
  }
  return { topics: [event.ConversationID ? `messages:${event.ConversationID}` : 'messages', 'activity', 'conversations', 'status', 'tasks', 'memories', 'usage'] };
}

export function mutationTopics(path: string): string[] {
  if (path === '/api/updates/check') return []; // UpdateProvider refreshes its snapshot after a check.
  if (['/api/weixin/poll', '/api/weixin/cancel', '/api/telegram/poll', '/api/telegram/cancel'].includes(path)) return [];
  if (path.startsWith('/api/weixin/')) return ['weixin'];
  if (path.startsWith('/api/dingtalk/')) return ['dingtalk'];
  if (path.startsWith('/api/slack/')) return ['slack'];
  if (path.startsWith('/api/telegram/')) return ['telegram'];
  if (path.includes('/conversations/read')) return ['conversations'];
  if (path.includes('/conversations/feedback')) return []; // Message feedback reconciles its exact conversation.
  if (path.includes('/conversations/messages')) return ['conversations', 'status']; // The caller and SSE invalidate the affected conversation.
  // Terminal keystrokes must not trigger a list reload for every chunk.
  // Controls refresh their panel; lifecycle changes arrive via shell.tasks.
  if (/\/assistants\/[^/]+\/shell-tasks$/.test(path)) return [];
  // Browser controls reconcile their own status; pointer/keyboard input must
  // not invalidate unrelated assistant, conversation, schedule or grant data.
  if (/\/assistants\/[^/]+\/browser$/.test(path)) return [];
  if (path.includes('/assistants/')) return ['assistants', 'conversations', 'status', 'schedules', 'grants'];
  if (path === '/api/assistants') return ['assistants', 'conversations'];
  if (path.startsWith('/api/sandboxes')) return ['sandboxes', 'resources', 'grants', 'capabilities', 'assistants'];
  if (path.startsWith('/api/permission-requests')) return ['permission-requests', 'grants', 'status'];
  if (path.startsWith('/api/mcp/')) return ['mcp', 'tools', 'resources', 'grants', 'capabilities', 'admin', 'usage'];
  if (path.startsWith('/api/resources')) return ['resources', 'models', 'grants'];
  return [queryTopic(path)];
}

export function expandTopics(topics: string[]) {
  const expanded = new Set(topics);
  if (topics.some(topic => ['qq', 'feishu', 'weixin', 'dingtalk', 'slack', 'telegram'].includes(topic))) expanded.add('chat-channel');
  if (topics.includes('settings')) { expanded.add('dreams'); expanded.add('plans'); }
  for (const topic of topics) if (topic.startsWith('status:')) expanded.add('activity');
  if (topics.some(topic => ['assistants', 'status', 'messages'].includes(topic) || topic.startsWith('messages:'))) expanded.add('activity');
  if (topics.some(topic => ['admin', 'account', 'grants'].includes(topic))) expanded.add('usage');
  if (topics.some(topic => ['todos', 'schedules', 'tasks', 'plans'].includes(topic))) expanded.add('plans');
  if (topics.some(topic => ['grants', 'resources', 'assistants', 'sandboxes'].includes(topic))) expanded.add('capabilities');
  return [...expanded];
}
