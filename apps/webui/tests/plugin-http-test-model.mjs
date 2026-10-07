import { randomUUID } from 'node:crypto';
export function pluginHTTPTestModel(input, response) {
  if (!input.model?.startsWith('Plugin HTTP fixture')) return false;
  const messages = input.messages ?? [], index = messages.findLastIndex(m => typeof m.content === 'string' && m.content.startsWith('Runtime event: '));
  const event = index >= 0 ? JSON.parse(messages[index].content.split('\n')[0].slice('Runtime event: '.length)) : {};
  const recent = messages.slice(index + 1), names = new Map();
  for (const m of recent) for (const c of m.tool_calls ?? []) names.set(c.id, c.function.name);
  const replies = recent.filter(m => m.role === 'tool'), completed = new Set(replies.map(m => names.get(m.tool_call_id)));
  const tool = (input.tools ?? []).map(t => t.function).find(f => f.name.startsWith('plg_') && f.description.includes('/ lookup:'));
  let name, args;
  if (tool && !completed.has(tool.name)) { name = tool.name; args = { query: 'fixture & quoted "value"' }; }
  else if (!completed.has('conversation_publish')) {
    const result = replies.find(m => names.get(m.tool_call_id) === tool?.name)?.content ?? '';
    name = 'conversation_publish'; args = { conversation_id: event.conversation_id, text: `HTTP plugin result: ${result}` };
  }
  const message = { role: 'assistant', content: '' };
  if (name) message.tool_calls = [{ id: randomUUID(), type: 'function', function: { name, arguments: JSON.stringify(args) } }];
  response.setHeader('Content-Type', 'application/json'); response.end(JSON.stringify({ id: randomUUID(), choices: [{ message, finish_reason: name ? 'tool_calls' : 'stop' }], usage: { prompt_tokens: 100, completion_tokens: 30 } })); return true;
}
export async function pluginHTTPProvider(request, response) {
  const url = new URL(request.url, 'https://localhost');
  if (url.pathname === '/a' && request.method === 'POST' && request.headers.authorization === 'Bearer fixture-key-a') {
    let raw = ''; for await (const part of request) raw += part;
    try { const body = JSON.parse(raw); response.setHeader('Content-Type', 'application/json'); response.end(JSON.stringify({ text: `A: ${body.query}`, credits: 1 })); } catch { response.writeHead(400).end(); } return;
  }
  if (url.pathname === '/b' && request.method === 'GET' && request.headers['x-api-key'] === 'fixture-key-b') { response.setHeader('Content-Type', 'application/json'); response.end(JSON.stringify({ message: `B: ${url.searchParams.get('q')}`, credits: 1 })); return; }
  response.writeHead(401).end();
}
export function httpPluginPackage(id, origin) {
  const inputSchema = { type: 'object', properties: { query: { type: 'string' } }, required: ['query'], additionalProperties: false };
  const result = path => ({ type: 'object', fields: { text: { type: 'string', path } } });
  const operation = (method, path, field) => ({ method, path, inputSchema, ...(method === 'POST' ? { body: { query: { argument: 'query' } } } : { query: { q: { argument: 'query' } } }), result: result(field), usage: { type: 'number', path: '/credits' } });
  return { manifest: { manifestVersion: 1, pluginApi: 2, id, name: 'HTTP fixture', description: 'Generated contract fixture for Plugin API 2.', version: '1.0.0', settings: { backend: { type: 'connection', title: 'Search service', providers: ['a', 'b'], requiredOperations: ['lookup'] }, language: { type: 'string', title: 'Language', default: 'en', enum: ['en', 'zh'] } }, settingsPage: { id: 'settings', title: 'Fixture settings page', entry: 'settings.html', capabilities: ['settings.read', 'settings.write', 'connections.read', 'connections.test'] }, providers: [
    { id: 'a', name: 'Provider A', origin, auth: { type: 'bearer' }, operations: { lookup: operation('POST', '/a', '/text') }, test: { operation: 'lookup', arguments: { query: 'connection check' } } },
    { id: 'b', name: 'Provider B', origin, auth: { type: 'header', header: 'X-API-Key' }, operations: { lookup: operation('GET', '/b', '/message') }, test: { operation: 'lookup', arguments: { query: 'connection check' } } },
  ], contributes: { themes: [], pages: [], skills: [], tools: [{ id: 'lookup', description: 'Look up a deterministic fixture.', execution: { type: 'http', connectionSetting: 'backend', operation: 'lookup' }, capabilities: ['http.request'], inputSchema }] } }, files: { 'settings.html': `<main><p id="status">Loading settings</p><button id="save" disabled>Set language to Chinese</button></main><script>let port,settings;addEventListener('message',e=>{if(e.source!==parent||e.data?.context?.apiVersion!==2)return;port=e.ports[0];port.onmessage=({data})=>{if(data.error){document.getElementById('status').textContent=data.error;return}settings=data.value;document.getElementById('status').textContent='Language: '+settings.Values.language;document.getElementById('save').disabled=false};port.postMessage({id:1,method:'settings.get'})});document.getElementById('save').onclick=()=>{port.postMessage({id:2,method:'settings.update',params:{expectedVersion:settings.Version,values:{...settings.Values,language:'zh'}}})};parent.postMessage({type:'molr.plugin.ready',apiVersion:2},'*')</script>` } };
}
