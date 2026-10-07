import { randomUUID } from 'node:crypto';
export async function compressionModel(input, response) {
  if (!input.model?.startsWith('Compression ')) return false;
  const messages = input.messages ?? [];
  const summary = messages.at(-1)?.content?.startsWith('Create one concise continuation checkpoint');
  const message = { role: 'assistant', content: summary ? 'Goal: continue the requested work. Completed: read user messages and acknowledge them. Preserve chat history and all completed effects. Next step: follow the next user request.' : 'Internal completion.' };
  let finish = 'stop';
  if (summary) await new Promise(resolve => setTimeout(resolve, 1200));
  else {
    const start = messages.findLastIndex(m => typeof m.content === 'string' && m.content.startsWith('Runtime event: '));
    const event = start >= 0 ? JSON.parse(messages[start].content.split('\n')[0].slice('Runtime event: '.length)) : {};
    const results = messages.slice(start + 1).filter(m => m.role === 'tool');
    const call = (name, args) => { message.tool_calls = [{ id: randomUUID(), type: 'function', function: { name, arguments: JSON.stringify(args) } }]; finish = 'tool_calls'; };
    if (!results.length) call('conversation_read', { conversation_id: event.conversation_id });
    else if (results.length === 1) {
      await new Promise(resolve => setTimeout(resolve, 600));
      call('conversation_publish', { conversation_id: event.conversation_id, text: 'Message received. Context compression is available with /compress.' });
    }
  }
  if (response.destroyed) return true;
  response.setHeader('Content-Type', 'application/json');
  response.end(JSON.stringify({ id: randomUUID(), choices: [{ message, finish_reason: finish }], usage: { prompt_tokens: 100, completion_tokens: 20 } }));
  return true;
}
