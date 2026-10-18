import { randomUUID } from 'node:crypto';
let revision = 1;
export async function mcpProvider(request, response) {
  if (request.headers.authorization !== 'Bearer mcp-fixture-key') { response.writeHead(401).end(); return; }
  if (request.method === 'GET' && request.url === '/fixture/revise') { revision++; response.end('ok'); return; }
  if (request.method !== 'POST') { response.writeHead(405).end(); return; }
  const chunks = []; for await (const chunk of request) chunks.push(chunk);
  const input = JSON.parse(Buffer.concat(chunks).toString());
  if (!input.id) { response.writeHead(202).end(); return; }
  let result;
  if (input.method === 'initialize') result = { protocolVersion: '2025-11-25', serverInfo: { name: 'MCP fixture', version: '1' }, capabilities: { tools: {} } };
  else if (input.method === 'tools/list') result = { tools: [{ name: 'lookup', description: `Look up artificial fixture records, revision ${revision}.`, inputSchema: { type: 'object', properties: { query: { type: 'string' } }, required: ['query'], additionalProperties: false } }] };
  else if (input.method === 'tools/call') result = { content: [{ type: 'text', text: `MCP fixture result: ${input.params.arguments.query}` }], structuredContent: { found: true }, isError: false };
  else { response.writeHead(400).end(); return; }
  response.setHeader('Content-Type', 'application/json'); response.end(JSON.stringify({ jsonrpc: '2.0', id: input.id, result }));
}
export function mcpModel(input, response) {
  if (!input.model?.startsWith('MCP fixture')) return false;
  const messages = input.messages ?? [], index = messages.findLastIndex(m => typeof m.content === 'string' && m.content.startsWith('Runtime event: '));
  const event = index >= 0 ? JSON.parse(messages[index].content.split('\n')[0].slice('Runtime event: '.length)) : {};
  const recent = messages.slice(index + 1), names = new Map();
  for (const m of recent) for (const c of m.tool_calls ?? []) names.set(c.id, c.function.name);
  const replies = recent.filter(m => m.role === 'tool'), completed = new Set(replies.map(m => names.get(m.tool_call_id)));
  const tool = input.tools?.find(t => t.function.name.startsWith('mcp_'))?.function;
  let name, args;
  if (event.conversation_id && !completed.has('conversation_read')) { name = 'conversation_read'; args = { conversation_id: event.conversation_id, limit: 20 }; }
  else if (event.conversation_id && tool && !completed.has(tool.name)) { name = tool.name; args = { query: 'artificial record' }; }
  else if (event.conversation_id && !completed.has('conversation_publish')) {
    const result = replies.find(m => names.get(m.tool_call_id) === tool?.name)?.content ?? '';
    name = 'conversation_publish'; args = { conversation_id: event.conversation_id, text: result.includes('MCP fixture result:') ? 'MCP fixture completed.' : 'MCP result or permission unavailable.' };
  }
  const message = { role: 'assistant', content: 'Fixture reasoning.' };
  if (name) message.tool_calls = [{ id: randomUUID(), type: 'function', function: { name, arguments: JSON.stringify(args) } }];
  response.setHeader('Content-Type', 'application/json'); response.end(JSON.stringify({ id: randomUUID(), choices: [{ message, finish_reason: name ? 'tool_calls' : 'stop' }], usage: { prompt_tokens: 30, completion_tokens: 10 } })); return true;
}
