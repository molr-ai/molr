import { randomUUID } from 'node:crypto';
// Deterministic model fixture: discovers actual plugin tool names from the request.
export function pluginTestModel(input, response) {
  if (!input.model?.startsWith('Plugin fixture')) return false;
  const messages = input.messages ?? [];
  const index = messages.findLastIndex(m => typeof m.content === 'string' && m.content.startsWith('Runtime event: '));
  const event = index >= 0 ? JSON.parse(messages[index].content.split('\n')[0].slice('Runtime event: '.length)) : {};
  const recent = messages.slice(index + 1), names = new Map();
  for (const m of recent) for (const c of m.tool_calls ?? []) names.set(c.id, c.function.name);
  const replies = recent.filter(m => m.role === 'tool'), completed = new Set(replies.map(m => names.get(m.tool_call_id)));
  const functions = (input.tools ?? []).map(t => t.function);
  const skill = functions.find(f => f.name.startsWith('plg_') && f.description.includes('Read plugin Skill') && f.description.includes('Fixture tools'));
  const tool = functions.find(f => f.name.startsWith('plg_') && f.description.includes('/ report:'));
  let name, args;
  if (!completed.has('conversation_read')) { name = 'conversation_read'; args = { conversation_id: event.conversation_id, limit: 5 }; }
  else if (skill && !completed.has(skill.name)) { name = skill.name; args = {}; }
  else if (tool && !completed.has(tool.name)) { name = tool.name; args = { label: 'Plugin test report' }; }
  else if (tool && !completed.has('file_read')) { name = 'file_read'; args = { path: '/work/plugin-fixture/report.txt' }; }
  else if (!completed.has('conversation_publish')) {
    const read = replies.find(m => names.get(m.tool_call_id) === 'file_read');
    const success = read?.content?.includes('plugin-fixture-report');
    name = 'conversation_publish'; args = { conversation_id: event.conversation_id, text: success ? 'Test report written and verified: /work/plugin-fixture/report.txt\nPlugin test report verified.' : 'Plugin test execution unavailable.' };
  }
  const message = { role: 'assistant', content: '' };
  if (name) message.tool_calls = [{ id: randomUUID(), type: 'function', function: { name, arguments: JSON.stringify(args) } }];
  response.setHeader('Content-Type', 'application/json');
  response.end(JSON.stringify({ id: randomUUID(), choices: [{ message, finish_reason: name ? 'tool_calls' : 'stop' }], usage: { prompt_tokens: 100, completion_tokens: 30 } }));
  return true;
}
