import { randomUUID } from 'node:crypto';

// Deterministic provider for the real Runtime permission-request lifecycle.
export function permissionModel(input, response) {
  if (!input.model?.startsWith('Permission ')) return false;
  const messages = input.messages ?? [];
  const eventIndex = messages.findLastIndex(m => typeof m.content === 'string' && m.content.startsWith('Runtime event: '));
  const event = JSON.parse(messages[eventIndex].content.split('\n')[0].slice('Runtime event: '.length));
  const context = messages.findLast(m => typeof m.content === 'string' && m.content.startsWith('Runtime context_changed: '));
  const snapshot = JSON.parse(context.content.split('\n')[0].slice('Runtime context_changed: '.length));
  const permissions = snapshot.sources.find(s => s.kind === 'permissions')?.content ?? {};
  const requests = snapshot.sources.find(s => s.kind === 'permission_requests')?.content ?? [];
  const recent = messages.slice(eventIndex + 1), results = recent.filter(m => m.role === 'tool').map(m => JSON.parse(m.content));
  const published = recent.some(m => m.tool_calls?.some(c => c.function.name === 'conversation_publish'));
  const message = { role: 'assistant', content: '' };
  function call(name, args) { message.tool_calls = [{ id: randomUUID(), type: 'function', function: { name, arguments: JSON.stringify(args) } }]; }
  function finish() {
    response.setHeader('Content-Type', 'application/json');
    response.end(JSON.stringify({ id: randomUUID(), choices: [{ message, finish_reason: message.tool_calls ? 'tool_calls' : 'stop' }], usage: { prompt_tokens: 30, completion_tokens: 10 } }));
    return true;
  }
  if (input.model.startsWith('Permission unified ') && !requests.length) {
    if (event.event.TaskID) return finish();
    if (!published) {
      const history = results.find(r => r.Items);
      if (!history) call('conversation_read', { conversation_id: event.conversation_id, message_id: event.event.MessageID, limit: 1 });
      else {
        const { cards } = JSON.parse(history.Items[0].Content.Text);
        message.tool_calls = cards.map(card => ({ id: randomUUID(), type: 'function', function: { name: 'conversation_publish', arguments: JSON.stringify({ conversation_id: event.conversation_id, card }) } }));
      }
      return finish();
    }
  }
  const decision = requests.find(r => r.State !== 'pending');
  if (!requests.length) {
    const listing = results.find(r => r.Requestable);
    if (!listing) call('permission_list', {});
    else {
      const target = listing.Requestable.find(r => r.Kind === 'compute');
      const quota = input.model.startsWith('Permission quota ');
      const network = quota ? listing.Requestable.find(r => r.Kind === 'network') : null;
      call('permission_request', { resource_kind: 'compute', resource_id: target.ResourceID, resource_description: target.Name, actions: ['execute'], expires_at: new Date(Date.now()+3600000).toISOString(), reason: 'Run the calculation in an isolated sandbox.', ...(quota ? { constraints: {compute:{cpu_percent:200,memory_bytes:536870912,processes:128,max_duration_seconds:300,unlimited_duration:false,temp_bytes:67108864}}, additional_requests: [{resource_kind:'network',resource_id:network.ResourceID,resource_description:network.Name,actions:['execute'],reason:'Download the quota verification file.',constraints:{network:{bytes:1073741824,connections:32,period:'day',timezone:'Asia/Shanghai'}}}] } : {}) });
      message.tool_calls.push({ id: randomUUID(), type: 'function', function: { name: 'conversation_publish', arguments: JSON.stringify({ conversation_id: event.conversation_id, text: 'Must not run before approval' }) } });
    }
  } else if (decision?.State === 'rejected' && !published) {
    call('conversation_publish', { conversation_id: event.conversation_id, text: 'Permission was declined. No command was executed.' });
  } else if (decision?.State === 'approved' && !published) {
    const listing = results.find(r => r.Resources);
    if (!permissions.Granted?.some(g => g.ID === decision.GrantID)) throw Error('approved permission missing from injected context');
    if (!listing) call('permission_list', {});
    else if ((input.model.startsWith('Permission shell ') || input.model.startsWith('Permission quota ')) && results.length === 1) {
      call('shell_execute', { compute_id: listing.Resources.find(r => r.Kind === 'compute').ResourceID, script: input.model.startsWith('Permission quota ') ? `wget -qO /work/quota-fixture.txt http://host.molr.internal:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}/quota-fixture; wc -c /work/quota-fixture.txt` : 'printf permission-approved', files: [] });
    } else {
      if (input.model.startsWith('Permission quota ') && results.some(r => r.error)) console.error('quota fixture tool failure', JSON.stringify(results.filter(r => r.error)));
      call('conversation_publish', { conversation_id: event.conversation_id, text: results.some(r => r.error) ? 'Permission verification failed.' : 'Permission approved; current grants checked before continuing.' });
    }
  }
  return finish();
}
