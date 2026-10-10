import { randomUUID } from 'node:crypto';
// Exercise ordinary file_read and shell_execute with a plugin workspace copy.
export function pluginTestModel(input, response) {
  if (input.model?.startsWith('Plugin workspace ')) return workspaceModel(input, response);
  if (!input.model?.startsWith('Plugin fixture')) return false;
  const messages = input.messages ?? [];
  const index = messages.findLastIndex(m => typeof m.content === 'string' && m.content.startsWith('Runtime event: '));
  const event = index >= 0 ? JSON.parse(messages[index].content.split('\n')[0].slice('Runtime event: '.length)) : {};
  const recent = messages.slice(index + 1), calls = new Map();
  for (const m of recent) for (const c of m.tool_calls ?? []) calls.set(c.id, c.function);
  const replies = recent.filter(m => m.role === 'tool'), completed = name => replies.find(m => calls.get(m.tool_call_id)?.name === name);
  const read = path => replies.find(m => { const c = calls.get(m.tool_call_id); return c?.name === 'file_read' && JSON.parse(c.arguments).path === path; });
  let name, args;
  if (!completed('conversation_read')) { name = 'conversation_read'; args = { conversation_id: event.conversation_id, limit: 5 }; }
  else if (!read('/plugins/fixture.tools/1.0.0/SKILL.md')) { name = 'file_read'; args = { path: '/plugins/fixture.tools/1.0.0/SKILL.md' }; }
  else if (!completed('permission_list')) { name = 'permission_list'; args = {}; }
  else if (!completed('shell_execute')) {
    let permissions; try { permissions = JSON.parse(completed('permission_list').content); } catch { permissions = {}; }
    const compute = permissions.Resources?.find(r => r.Kind === 'compute');
    if (compute) { name = 'shell_execute'; args = { compute_id: compute.ID ?? compute.ResourceID, script: '/bin/sh /work/.local/plugins/fixture.tools/versions/1.0.0/package/report.sh "Plugin test report"' }; }
  } else if (!read('/work/plugin-fixture/report.txt')) { name = 'file_read'; args = { path: '/work/plugin-fixture/report.txt' }; }
  if (!name && !completed('conversation_publish')) {
    const success = read('/work/plugin-fixture/report.txt')?.content?.includes('plugin-fixture-report');
    name = 'conversation_publish'; args = { conversation_id: event.conversation_id, text: success ? 'Plugin test report verified.' : 'Plugin test execution unavailable.' };
  }
  const message = { role: 'assistant', content: '' };
  if (name) message.tool_calls = [{ id: randomUUID(), type: 'function', function: { name, arguments: JSON.stringify(args) } }];
  response.setHeader('Content-Type', 'application/json'); response.end(JSON.stringify({ id: randomUUID(), choices: [{ message, finish_reason: name ? 'tool_calls' : 'stop' }], usage: { prompt_tokens: 100, completion_tokens: 30 } }));
  return true;
}

function workspaceModel(input, response) {
 const pluginID = input.model.slice('Plugin workspace '.length);
 const messages = input.messages ?? [], eventIndex = messages.findLastIndex(m => typeof m.content === 'string' && m.content.startsWith('Runtime event: '));
 const event = eventIndex >= 0 ? JSON.parse(messages[eventIndex].content.split('\n')[0].slice(15)) : {};
 const recent = messages.slice(eventIndex + 1), calls = new Map();
 for (const m of recent) for (const c of m.tool_calls ?? []) calls.set(c.id, c.function);
 const replies = recent.filter(m => m.role === 'tool');
 const done = name => replies.find(m => calls.get(m.tool_call_id)?.name === name);
 const finish = (name, args) => { const message = { role: 'assistant', content: '', ...(name ? { tool_calls: [{ id: randomUUID(), type: 'function', function: { name, arguments: JSON.stringify(args) } }] } : {}) }; response.setHeader('Content-Type', 'application/json'); response.end(JSON.stringify({ id: randomUUID(), choices: [{ message, finish_reason: name ? 'tool_calls' : 'stop' }], usage: { prompt_tokens: 100, completion_tokens: 30 } })); return true; };
 if (!event.conversation_id || event.event?.Kind === 'born') return finish();
 if (!done('conversation_read')) return finish('conversation_read', { conversation_id: event.conversation_id, limit: 5 });
 if (!done('file_read')) return finish('file_read', { path: `/plugins/${pluginID}/1.0.0/SKILL.md` });
 if (!done('permission_list')) return finish('permission_list', {});
 const listing = JSON.parse(done('permission_list').content);
 const compute = listing.Resources?.find(r => r.Kind === 'compute');
 if (!done('shell_execute') && compute) return finish('shell_execute', { compute_id: compute.ResourceID, script: `/bin/sh /work/.local/plugins/${pluginID}/versions/1.0.0/prepare.sh` });
 if (!done('conversation_publish')) {
  const result = done('shell_execute')?.content ?? '';
  return finish('conversation_publish', { conversation_id: event.conversation_id, text: result.includes('fixture-cli-1') ? 'Workspace CLI installed and checked.' : 'Workspace CLI preparation failed.' });
 }
 return finish();
}
