import { randomUUID } from 'node:crypto';

// Deterministic external model for testing real Runtime interruption and input
// delivery. It deliberately leaves the original model request running.
export async function expediteModel(input, response) {
  const messages = input.messages ?? [];
  const eventIndex = messages.findLastIndex(message => typeof message.content === 'string' && message.content.startsWith('Runtime event: '));
  const event = eventIndex >= 0 ? JSON.parse(messages[eventIndex].content.split('\n')[0].slice('Runtime event: '.length)) : {};
  const recent = messages.slice(eventIndex + 1);
  const urgentIndex = recent.findLastIndex(message => message.role === 'user');
  const urgent = recent.filter(message => message.role === 'user').flatMap(message => { try { return JSON.parse(message.content).Items ?? []; } catch { return []; } });
  const results = recent.filter(message => message.role === 'tool');
  const message = { role: 'assistant', content: 'Private execution.' };
  let finish = 'stop';
  const call = (name, args) => { message.tool_calls = [{ id: randomUUID(), type: 'function', function: { name, arguments: JSON.stringify(args) } }]; finish = 'tool_calls'; };
  if (urgent.length && !recent.slice(urgentIndex + 1).some(message => message.role === 'tool')) {
    await new Promise(resolve => setTimeout(resolve, 600));
    call('conversation_publish', { conversation_id: event.conversation_id, text: `Expedited messages received: ${urgent.map(item => item.Content.Text).join(' / ')}` });
  } else if (!urgent.length && results.length === 0) {
    // Keep the trigger unread long enough to detect a flashing expedite button.
    await new Promise(resolve => setTimeout(resolve, input.model?.startsWith('Expedite burst ') ? 8000 : 3000));
    if (response.destroyed) return;
    call('conversation_read', { conversation_id: event.conversation_id, message_id: event.event.MessageID });
  } else if (!urgent.length && results.length === 1) {
    response.writeHead(200, { 'Content-Type': 'application/json' }); response.flushHeaders();
    await new Promise(resolve => { const timer = setTimeout(resolve, 45_000); response.once('close', () => { clearTimeout(timer); resolve(); }); });
    if (response.destroyed) return;
    call('conversation_publish', { conversation_id: event.conversation_id, text: 'Original task finished without expedition.' });
  }
  if (!response.headersSent) response.setHeader('Content-Type', 'application/json');
  response.end(JSON.stringify({ id: randomUUID(), choices: [{ message, finish_reason: finish }], usage: { prompt_tokens: 30, completion_tokens: 10 } }));
}
