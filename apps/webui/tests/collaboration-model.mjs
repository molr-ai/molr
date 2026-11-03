import { randomUUID } from 'node:crypto';
export function collaborationModel(input, response) {
 if (!input.model?.startsWith('Collaboration ')) return false;
 const messages = input.messages ?? [];
 const events = messages.map((m, index) => {
  if (typeof m.content !== 'string' || !m.content.startsWith('Runtime event: ')) return null;
  return { ...JSON.parse(m.content.split('\n')[0].slice('Runtime event: '.length)), index };
 }).filter(Boolean);
 const event = events.at(-1), recent = messages.slice((event?.index ?? -1) + 1);
 const results = recent.filter(m => m.role === 'tool');
 const temporary = messages.some(m => typeof m.content === 'string' && m.content.includes('You are a temporary assistant. Employer AssistantID:'));
 let call;
 if (event?.event.Kind === 'user_message') {
  if (results.length === 0) call = ['conversation_read', { conversation_id: event.conversation_id }];
  else if (results.length === 1) call = ['assistant_hire', { name: 'Temporary researcher', instructions: 'Compute 6 times 7 and report the result to me. Do not request permissions.' }];
 } else if (event?.event.Kind === 'assistant_message') {
  if (results.length === 0) call = ['conversation_read', { conversation_id: event.conversation_id }];
  else if (temporary && results.length === 1) call = ['conversation_publish', { conversation_id: event.conversation_id, text: 'Worker result: 42.' }];
  else if (!temporary) {
   const hire = messages.filter(m => m.role === 'tool').map(m => { try { return JSON.parse(m.content); } catch { return null; } }).find(m => m?.EmployerID && m.AssistantID && m.Version);
   if (input.model.startsWith('Collaboration manual ') && results.length === 1) call = ['conversation_publish', { conversation_id: events.findLast(e => e.event.Kind === 'user_message').conversation_id, text: 'The temporary assistant completed the work: 42. Employment stays active.' }];
   else if (results.length === 1 && hire) call = ['assistant_dismiss', { assistant_id: hire.AssistantID, expected_version: hire.Version }];
   else if (!input.model.startsWith('Collaboration manual ') && results.length === 2) call = ['conversation_publish', { conversation_id: events.findLast(e => e.event.Kind === 'user_message').conversation_id, text: 'The temporary assistant completed the work: 42. Employment ended.' }];
  }
 }
 const message = { role: 'assistant', content: '' };
 if (call) message.tool_calls = [{ id: randomUUID(), type: 'function', function: { name: call[0], arguments: JSON.stringify(call[1]) } }];
 response.setHeader('Content-Type', 'application/json');
 response.end(JSON.stringify({ id: randomUUID(), choices: [{ message, finish_reason: call ? 'tool_calls' : 'stop' }], usage: { prompt_tokens: 40, completion_tokens: 10 } }));
 return true;
}
