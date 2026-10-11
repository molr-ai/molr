import { randomUUID } from 'node:crypto';

// Deterministic external model; all filesystem/network work runs in the real agent sandbox.
export function workspaceCLIModel(input, response) {
 if (!input.model?.startsWith('Workspace CLI ')) return false;
 const messages = input.messages ?? [], index = messages.findLastIndex(m => typeof m.content === 'string' && m.content.startsWith('Runtime event: '));
 const event = index >= 0 ? JSON.parse(messages[index].content.split('\n')[0].slice(15)) : {};
 const recent = messages.slice(index + 1), calls = new Map();
 for (const m of recent) for (const c of m.tool_calls ?? []) calls.set(c.id, c.function);
 const replies = recent.filter(m => m.role === 'tool');
 const done = name => replies.find(m => calls.get(m.tool_call_id)?.name === name);
 const finish = (name, args) => {
  const message = { role: 'assistant', content: '', ...(name ? { tool_calls: [{ id: randomUUID(), type: 'function', function: { name, arguments: JSON.stringify(args) } }] } : {}) };
  response.setHeader('Content-Type', 'application/json'); response.end(JSON.stringify({ id: randomUUID(), choices: [{ message, finish_reason: name ? 'tool_calls' : 'stop' }], usage: { prompt_tokens: 100, completion_tokens: 30 } })); return true;
 };
 if (!event.conversation_id || event.event?.Kind === 'born') return finish();
 if (!done('conversation_read')) return finish('conversation_read', { conversation_id: event.conversation_id, message_id: event.event.MessageID, limit: 1 });
 const history = JSON.parse(done('conversation_read').content), request = history.Items?.[0]?.Content?.Text ?? '';
 const isolated = request.includes('isolation'), persisted = request.includes('without downloading');
 if (!done('permission_list')) return finish('permission_list', {});
 let listing = JSON.parse(done('permission_list').content);
 if (listing.full_result_path) {
  const chunks = replies.map(r => JSON.parse(r.content)).filter(r => r.Path === listing.full_result_path && typeof r.ByteOffset === 'number').sort((a,b) => a.ByteOffset-b.ByteOffset);
  let text = '', offset = 0;
  for (const chunk of chunks) { if (chunk.ByteOffset === offset) { text += chunk.Text; offset = chunk.NextByteOffset; } }
  if (Buffer.byteLength(text) < listing.bytes) return finish('file_read', { path: listing.full_result_path, byte_offset: offset, max_bytes: 4096 });
  listing = JSON.parse(text);
 }
 const compute = listing.Resources?.find(r => r.Kind === 'compute');
 const skill = '/work/.agents/skills/workspace-cli/SKILL.md', cli = '/work/.local/bin/workspace-cli';
 const shell = replies.filter(m => calls.get(m.tool_call_id)?.name === 'shell_execute');
 if (!shell.length && compute) {
  const origin = `http://host.molr.internal:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const script = isolated ? `test ! -e ${cli} && test ! -e ${skill} && echo isolated` : persisted ? `${cli} --version` : `set -eu\nmkdir -p /work/.local/bin /work/.agents/skills/workspace-cli\nwget -qO ${cli} ${origin}/workspace-cli.sh\nchmod 700 ${cli}\nwget -qO ${skill} ${origin}/workspace-skill.md\necho installed`;
  return finish('shell_execute', { compute_id: compute.ResourceID, script });
 }
 const read = replies.find(m => { const call = calls.get(m.tool_call_id); return call?.name === 'file_read' && JSON.parse(call.arguments).path === skill; });
 if (!isolated && !read) return finish('file_read', { path: skill });
 if (!isolated && !persisted && shell.length === 1 && compute) return finish('shell_execute', { compute_id: compute.ResourceID, script: `${cli} --version` });
 if (!done('conversation_publish')) {
  const ok = isolated ? shell[0]?.content.includes('isolated') : read?.content.includes('workspace-cli --version') && shell.at(-1)?.content.includes('fixture-cli-1');
  return finish('conversation_publish', { conversation_id: event.conversation_id, text: ok ? isolated ? 'Workspace is isolated.' : persisted ? 'Workspace CLI persisted and checked.' : 'Workspace CLI installed and checked.' : 'Workspace CLI check failed.' });
 }
 return finish();
}
