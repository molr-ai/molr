import { randomUUID } from 'node:crypto';

// Deterministic provider; every read, mutation and publish runs through Runtime.
export function profileModel(input, response) {
  if (!input.model?.startsWith('Profile ')) return false;
  const messages = input.messages ?? [];
  const start = messages.findLastIndex(m => typeof m.content === 'string' && m.content.startsWith('Runtime event: '));
  const event = start >= 0 ? JSON.parse(messages[start].content.split('\n')[0].slice('Runtime event: '.length)) : {};
  const context = messages.findLast(m => typeof m.content === 'string' && m.content.startsWith('Runtime context_changed: '));
  const snapshot = context ? JSON.parse(context.content.split('\n')[0].slice('Runtime context_changed: '.length)) : {};
  const profile = snapshot.sources?.find(s => s.kind === 'profile')?.content;
  const recent = messages.slice(start + 1), results = recent.filter(m => m.role === 'tool').map(m => JSON.parse(m.content));
  const message = { role: 'assistant', content: '' };
  const call = (name, args) => { message.tool_calls = [{ id: randomUUID(), type: 'function', function: { name, arguments: JSON.stringify(args) } }]; };
  const called = name => recent.some(m => m.tool_calls?.some(c => c.function.name === name));
  if (event.conversation_id && !called('conversation_publish')) {
    const history = results.find(r => r.Items);
    const user = history?.Items.find(m => m.ID === event.event?.MessageID) ?? history?.Items.find(m => m.Sender === 'user');
    if (!history) call('conversation_read', { conversation_id: event.conversation_id });
    else if (!profile) call('conversation_publish', { conversation_id: event.conversation_id, text: 'Profile context missing.' });
    else if (results.some(r => r.error)) call('conversation_publish', { conversation_id: event.conversation_id, text: `Profile failed: ${results.find(r => r.error).error.code}` });
    else if (called('profile_update')) {
      const result = results.find(r => r.Profile);
      const same = result?.Profile.Version === profile.Version && result.Profile.Name === profile.Name && result.Profile.Avatar.Type === profile.Avatar.Type;
      call('conversation_publish', { conversation_id: event.conversation_id, text: same ? `Profile saved: ${profile.Name} / ${profile.Avatar.Type}${profile.Avatar.ID ? ':' + profile.Avatar.ID : ''}` : 'Profile context did not refresh.' });
    } else if (user?.Content.AttachmentIDs?.length && !called('conversation_attachment')) {
      call('conversation_attachment', { conversation_id: event.conversation_id, file_id: user.Content.AttachmentIDs[0], path: '/work/profile-upload' });
    } else {
      const text = user?.Content.Text ?? '';
      let args = { expected_version: profile.Version };
      try { args = { ...args, ...JSON.parse(text) }; }
      catch {
        if (/清除|clear/i.test(text)) args.avatar = { type: 'clear' };
        else if (user?.Content.AttachmentIDs?.length) args.avatar = { type: 'file', path: '/work/profile-upload' };
        else { args.name = text.match(/(?:叫|改名为|name[:：])\s*[“"']?([^，。\n”"']+)/)?.[1]?.trim() ?? '小满'; args.avatar = { type: 'preset', id: /狐狸|fox/.test(text) ? 'fox' : 'cat' }; }
      }
      call('profile_update', args);
    }
  }
  response.setHeader('Content-Type', 'application/json');
  response.end(JSON.stringify({ id: randomUUID(), choices: [{ message, finish_reason: message.tool_calls ? 'tool_calls' : 'stop' }], usage: { prompt_tokens: 100, completion_tokens: 20 } }));
  return true;
}
