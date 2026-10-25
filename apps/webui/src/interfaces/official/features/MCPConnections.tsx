import { useId, useState } from 'react';
import { Badge, Button, Checkbox, Dialog, Drawer, ModalPresence, EmptyState, SearchField, Select, Tabs, TextField } from '@molr/ui';
import { Layers, Plus, RefreshCw, Search, Server, Wrench } from 'lucide-react';
import type { Assistant, MCPCall, MCPConnection, MCPTool, Resource, ToolCapability } from '@molr/sdk';
import { api, mutationKey } from '@molr/sdk';
import { useCopy, useQuery, useSession } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { useReauthentication } from '../components/ReauthenticationDialog';
import { Confirm, Empty, Loading, useAction } from '../components/common';
import { GrantEditor } from './GrantEditor';
import { ToolCatalogRow } from './ToolCatalogRow';
import { ToolDetails } from './ToolDetails';
import './mcp-connections.css';

const enabledIDs = (c: MCPConnection) => c.Tools.filter(t => t.Enabled).map(t => t.ID);
function save(c: MCPConnection, changes: object = {}) {
  return api.post<MCPConnection>('/api/mcp/connections/save', { ID: c.ID, Name: c.Name, Endpoint: c.Endpoint, Auth: c.Auth, Header: c.Header, Enabled: c.Enabled, EnabledTools: enabledIDs(c), CallLimit: c.CallLimit, Concurrent: c.Concurrent, TimeoutSeconds: c.TimeoutSeconds, ...changes, Mutation: { IdempotencyKey: mutationKey(), ExpectedVersion: c.Version } });
}
export function MCPConnections() {
  const { t, locale } = useCopy(), zh = locale === 'zh', copy = t.toolsCatalog, admin = useSession().principal?.User?.Administrator === true;
  const query = useQuery<MCPConnection[]>('/api/mcp/connections');
  const [search, setSearch] = useState(''), [group, setGroup] = useState('all'), [selected, setSelected] = useState<string | null>(null), [creating, setCreating] = useState(false);
  const [selectedTool, setSelectedTool] = useState<{ connectionID: string; toolID: string } | null>(null);
  useErrorToast(query.error, query.reload);
  const connections = query.data ?? [], terms = search.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
  const groups = connections.map(c => ({ connection: c, tools: c.Tools.filter(tool => terms.every(term => [c.Name, tool.Name, tool.Description].join(' ').toLocaleLowerCase().includes(term))) }));
  const currentGroup = group === 'all' || connections.some(c => c.ID === group) ? group : 'all';
  const shown = groups.filter(({ connection: c, tools }) => (currentGroup === 'all' || currentGroup === c.ID) && (tools.length || (!terms.length && !c.Tools.length)));
  const total = connections.reduce((n, c) => n + c.Tools.length, 0), count = shown.reduce((n, g) => n + g.tools.length, 0);
  const current = connections.find(c => c.ID === selected), toolConnection = connections.find(c => c.ID === selectedTool?.connectionID), tool = toolConnection?.Tools.find(t => t.ID === selectedTool?.toolID);
  const filters = [{ id: 'all', name: t.all, count: groups.reduce((n, g) => n + g.tools.length, 0), icon: Layers }, ...groups.map(g => ({ id: g.connection.ID, name: g.connection.Name, count: g.tools.length, icon: Server }))];
  const status = (c: MCPConnection) => !c.Enabled ? (zh ? '已停用' : 'Disabled') : c.Status === 'available' ? (zh ? '连接正常' : 'Connected') : c.Status === 'offline' ? (zh ? '连接失败' : 'Connection failed') : (zh ? '待测试' : 'Not tested');
  return <div className="tools-catalog mcp-connections">
    <header className="tools-catalog-heading"><div><h2>{zh ? 'MCP 工具' : 'MCP tools'}</h2><p>{zh ? '按服务连接浏览工具，查看说明并管理助理权限。' : 'Browse tools by service connection, view details and manage assistant permissions.'}</p></div>
      <SearchField label={copy.search} placeholder={copy.search} hideLabel value={search} onValueChange={setSearch} />
    </header>
    <div className="mcp-catalog-toolbar"><div className="tools-catalog-filters" role="group" aria-label={zh ? 'MCP 连接筛选' : 'MCP connection filters'}>{filters.map(f => <button type="button" key={f.id} aria-pressed={currentGroup === f.id} onClick={() => setGroup(f.id)}><f.icon size={15} aria-hidden="true" /><span>{f.name}</span>{query.data && <span className="tools-catalog-count">{f.count}</span>}</button>)}</div>{admin && <Button size="sm" variant="secondary" onClick={() => setCreating(true)}><Plus size={14} aria-hidden="true" />{zh ? '添加 MCP 连接' : 'Add MCP connection'}</Button>}</div>
    {query.loading && !query.data && <Loading />}
    {query.data && <><div className="tools-catalog-summary" role="status">{copy.results(count, total)}</div>
      {!shown.length ? <div className="tools-catalog-empty"><EmptyState illustration={connections.length ? <Search /> : <Wrench />} title={connections.length ? t.noResults : (zh ? '暂无 MCP 连接' : 'No MCP connections')} description={connections.length ? copy.noResultsHelp : (zh ? '添加远程 MCP 连接，发现并启用所需工具。' : 'Add a remote MCP connection to discover and enable its tools.')} />{connections.length > 0 && <Button variant="secondary" size="sm" onClick={() => { setSearch(''); setGroup('all'); }}>{copy.reset}</Button>}</div> : shown.map(({ connection: c, tools }) => <section className="tools-catalog-group" key={c.ID} aria-label={c.Name}>
        <div className="mcp-catalog-group-heading"><h3><Server size={16} aria-hidden="true" />{c.Name}<span>{tools.length}</span></h3><Badge size="sm" dot={false} tone={c.Enabled && c.Status === 'available' ? 'positive' : 'neutral'}>{status(c)}</Badge><Button size="sm" variant="ghost" aria-label={`${zh ? '管理连接' : 'Manage connection'} · ${c.Name}`} onClick={() => setSelected(c.ID)}>{zh ? '管理连接' : 'Manage connection'}</Button></div>
        {tools.length ? <ul>{tools.map(tool => <li key={tool.ID}><ToolCatalogRow name={tool.Name} description={tool.Description} icon={Wrench} status={tool.Problem ? (zh ? '不支持' : 'Unsupported') : !tool.Enabled ? (zh ? '未启用' : 'Not enabled') : undefined} onOpen={() => setSelectedTool({ connectionID: c.ID, toolID: tool.ID })} /></li>)}</ul> : <p className="tools-catalog-help">{zh ? '尚未发现工具。打开“管理连接”，测试并刷新工具。' : 'No tools discovered. Open Manage connection to test and refresh tools.'}</p>}
      </section>)}
      <p className="tools-catalog-help">{copy.permissionsHelp}</p>
    </>}
    <ModalPresence>{creating && <ConnectionEditor onClose={() => setCreating(false)} onSaved={c => { setCreating(false); setSelected(c.ID); query.reload(); }} />}</ModalPresence>
    <ModalPresence>{current && <ConnectionDetails connection={current} admin={admin} onClose={() => setSelected(null)} reload={query.reload} />}</ModalPresence>
    <ModalPresence>{toolConnection && tool && <MCPToolDrawer connection={toolConnection} tool={tool} admin={admin} reload={query.reload} onClose={() => setSelectedTool(null)} />}</ModalPresence>
  </div>;
}
function ConnectionDetails({ connection: c, admin, onClose, reload }: { connection: MCPConnection; admin: boolean; onClose: () => void; reload: () => void }) {
  const { locale } = useCopy(), zh = locale === 'zh', action = useAction(), reauth = useReauthentication();
  const [tab, setTab] = useState('tools'), [editing, setEditing] = useState(false), [removing, setRemoving] = useState(false), [selectedTool, setSelectedTool] = useState<string | null>(null);
  const detail = c.Tools.find(t => t.ID === selectedTool);
  useErrorToast(action.error);
  return <><Drawer open title={c.Name} description={c.Endpoint || undefined} className="tool-catalog-detail mcp-detail" onOpenChange={open => { if (!open) onClose(); }}>
    <div className="mcp-detail-content">
      {admin && <div className="mcp-actions"><Button size="sm" disabled={!c.Enabled} loading={action.busy} onClick={() => void action.run(async () => { try { await api.post('/api/mcp/connections/refresh', { ID: c.ID, Mutation: { IdempotencyKey: mutationKey(), ExpectedVersion: c.Version } }); } finally { reload(); } }, undefined, false)}><RefreshCw size={14} />{zh ? '测试并刷新工具' : 'Test and refresh tools'}</Button><Button size="sm" variant="secondary" disabled={action.busy} onClick={() => setEditing(true)}>{zh ? '连接设置' : 'Connection settings'}</Button><Button size="sm" variant="secondary" disabled={action.busy} onClick={() => void action.run(async () => { await save(c, { Enabled: !c.Enabled }); reload(); }, undefined, false)}>{c.Enabled ? (zh ? '停用' : 'Disable') : (zh ? '启用' : 'Enable')}</Button><Button size="sm" variant="danger" disabled={action.busy} onClick={() => setRemoving(true)}>{zh ? '删除' : 'Delete'}</Button></div>}
      <p className="muted">{c.CheckedAt ? (zh ? '最近测试：' : 'Last checked: ') + new Date(c.CheckedAt).toLocaleString(locale) : (zh ? '保存后点击“测试并刷新工具”发现工具。' : 'After saving, test the connection to discover its tools.')}{c.Status === 'offline' && (zh ? ' · 连接失败，请检查地址、认证和网络策略。' : ' · Connection failed. Check the address, credentials and network policy.')}</p>
      {admin && <p className="muted">{zh ? `累计调用 ${c.Calls} / ${c.CallLimit} · 当前并发 ${c.Active} / ${c.Concurrent}` : `Lifetime calls ${c.Calls} / ${c.CallLimit} · Active ${c.Active} / ${c.Concurrent}`}</p>}
      <Tabs label={zh ? 'MCP 连接详情' : 'MCP connection details'} value={tab} onValueChange={setTab} items={[
        { value: 'tools', label: zh ? '工具' : 'Tools', content: <><p className="muted">{zh ? '新增或定义变化的工具需重新启用和授权。启用工具后，在助理权限中授予执行权限；普通成员还需用户组同时开放连接与工具。' : 'New or changed tools need approval and assistant grants. Members also need group access to both the connection and tool.'}</p>{!c.Tools.length ? <Empty title={zh ? '尚未发现工具' : 'No tools discovered'} /> : <section className="tools-catalog-group mcp-connection-tools"><ul>{c.Tools.map(t => <li key={t.ID}><ToolCatalogRow name={t.Name} description={t.Description} icon={Wrench} status={t.Problem ? (zh ? '不支持' : 'Unsupported') : !t.Enabled ? (zh ? '未启用' : 'Not enabled') : undefined} onOpen={() => setSelectedTool(t.ID)} /></li>)}</ul></section>}</> },
        ...(admin ? [{ value: 'history', label: zh ? '调用记录' : 'Call history', content: tab === 'history' ? <CallHistory id={c.ID} /> : null }] : []),
      ]} />
    </div>
  </Drawer>
    <ModalPresence>{editing && <ConnectionEditor connection={c} onClose={() => setEditing(false)} onSaved={() => { setEditing(false); reload(); }} />}</ModalPresence>
    <ModalPresence>{removing && <Confirm title={zh ? '删除 MCP 连接' : 'Delete MCP connection'} description={zh ? '删除凭证并停用此连接的所有工具，历史调用记录保留。' : 'Delete its credential and disable all tools. Call history is retained.'} danger onClose={() => setRemoving(false)} onConfirm={async () => { const verification = c.Auth !== 'none' ? await reauth.verify('credential.delete') : ''; await api.post('/api/mcp/connections/remove', { ID: c.ID, Verification: verification, Mutation: { IdempotencyKey: mutationKey(), ExpectedVersion: c.Version } }); reload(); onClose(); }} />}</ModalPresence>
    <ModalPresence>{detail && <MCPToolDrawer connection={c} tool={detail} admin={admin} reload={reload} onClose={() => setSelectedTool(null)} />}</ModalPresence>{reauth.dialog}
  </>;
}
function MCPToolDrawer({ connection: c, tool, admin, reload, onClose }: { connection: MCPConnection; tool: MCPTool; admin: boolean; reload: () => void; onClose: () => void }) {
  const { locale } = useCopy(), zh = locale === 'zh', action = useAction();
  const [grant, setGrant] = useState(false);
  const assistants = useQuery<Assistant[]>(grant ? '/api/assistants' : null, true), resources = useQuery<Resource[]>(grant ? '/api/resources' : null, true);
  useErrorToast(action.error || assistants.error || resources.error);
  const capability: ToolCapability = { Name: tool.ID, Description: tool.Description, InputSchema: tool.InputSchema, RequiredActions: ['execute'], SideEffect: true, Source: 'mcp', Available: c.Enabled && tool.Enabled && !tool.Problem, Reason: tool.Problem, ResourceIDs: [tool.ID] };
  return <><Drawer open title={tool.Name} description={tool.ID} className="tool-catalog-detail" onOpenChange={open => { if (!open && !action.busy) onClose(); }} dismissible={!action.busy} footer={<div className="mcp-tool-controls">{admin && <Checkbox label={zh ? `启用 ${tool.Name}` : `Enable ${tool.Name}`} checked={tool.Enabled} disabled={!!tool.Problem || action.busy || !c.CheckedAt} onChange={e => { const ids = enabledIDs(c).filter(id => id !== tool.ID); if (e.target.checked) ids.push(tool.ID); void action.run(async () => { await save(c, { EnabledTools: ids }); reload(); }, undefined, false); }} />}<Button size="sm" variant="secondary" disabled={!capability.Available || action.busy} onClick={() => setGrant(true)}>{zh ? '授权助理' : 'Grant to assistant'}</Button></div>}>
    <ToolDetails tool={capability} summary={Array.from(tool.Description).slice(0, 180).join('') + (Array.from(tool.Description).length > 180 ? '…' : '')} sourceLabel={c.Name} />
    {tool.Problem && <p className="tools-catalog-help">{zh ? '不支持此工具定义：' : 'Unsupported definition: '}{tool.Problem}</p>}
  </Drawer><ModalPresence>{grant && resources.data?.some(r => r.ID === tool.ID && r.State === 'available') && assistants.data && <GrantEditor initialResource={resources.data.find(r => r.ID === tool.ID)} resources={resources.data} assistants={assistants.data.filter(a => a.State === 'active')} onClose={() => setGrant(false)} />}</ModalPresence></>;
}
function ConnectionEditor({ connection: c, onClose, onSaved }: { connection?: MCPConnection; onClose: () => void; onSaved: (c: MCPConnection) => void }) {
  const { locale } = useCopy(), zh = locale === 'zh', action = useAction(), reauth = useReauthentication(), formID = useId();
  const [name, setName] = useState(c?.Name ?? ''), [endpoint, setEndpoint] = useState(c?.Endpoint ?? ''), [auth, setAuth] = useState(c?.Auth ?? 'bearer'), [header, setHeader] = useState(c?.Header || 'X-API-Key'), [key, setKey] = useState('');
  const [limit, setLimit] = useState(c?.CallLimit ?? 1000), [concurrent, setConcurrent] = useState(c?.Concurrent ?? 2), [timeout, setTimeout] = useState(c?.TimeoutSeconds ?? 30);
  const [mutation] = useState(mutationKey);
  useErrorToast(action.error);
  return <><Dialog open title={zh ? 'MCP 连接' : 'MCP connection'} dismissible={!action.busy} onOpenChange={open => { if (!open && !action.busy) onClose(); }} footer={<><Button variant="secondary" disabled={action.busy} onClick={onClose}>{zh ? '取消' : 'Cancel'}</Button><Button form={formID} type="submit" loading={action.busy}>{zh ? '保存连接' : 'Save connection'}</Button></>}>
    <form id={formID} className="editor" onSubmit={e => { e.preventDefault(); void action.run(async () => { const verification = key ? await reauth.verify('credential.put') : ''; const result = await api.post<MCPConnection>('/api/mcp/connections/save', { ID: c?.ID ?? '', Name: name, Endpoint: endpoint, Auth: auth, Header: auth === 'header' ? header : '', APIKey: key, Verification: verification, Enabled: c?.Enabled ?? true, EnabledTools: c ? enabledIDs(c) : [], CallLimit: limit, Concurrent: concurrent, TimeoutSeconds: timeout, Mutation: { IdempotencyKey: mutation, ExpectedVersion: c?.Version ?? 0 } }); setKey(''); onSaved(result); }, undefined, false); }}>
      <TextField label={zh ? '连接名称' : 'Connection name'} value={name} maxLength={120} required onChange={e => setName(e.target.value)} />
      <TextField label={zh ? 'MCP 服务地址' : 'MCP endpoint'} type="url" placeholder="https://mcp.example.com/mcp" value={endpoint} required disabled={!!c} onChange={e => setEndpoint(e.target.value)} />
      <Select label={zh ? '认证方式' : 'Authentication'} value={auth} disabled={!!c} options={[{ value: 'bearer', label: 'Bearer Token' }, { value: 'header', label: 'API Key' }, { value: 'none', label: zh ? '无认证' : 'No authentication' }]} onValueChange={v => { setAuth(v as MCPConnection['Auth']); setKey(''); }} />
      {auth === 'header' && <Select label={zh ? '认证请求头' : 'Authentication header'} value={header} disabled={!!c} options={['X-API-Key', 'API-Key', 'X-Auth-Token'].map(v => ({ value: v, label: v }))} onValueChange={setHeader} />}
      {auth !== 'none' && <TextField label={auth === 'bearer' ? 'Token' : 'API Key'} type="password" autoComplete="new-password" value={key} maxLength={8192} required={!c?.Configured} placeholder={c?.Configured ? (zh ? '已配置；留空保留' : 'Configured; leave blank to keep') : ''} onChange={e => setKey(e.target.value)} />}
      {c && <p className="muted">{zh ? '更换密钥后需重新发现并启用工具。服务地址与认证方式固定，换服务请新建连接。' : 'After replacing the key, discover and enable tools again. Create another connection to change the endpoint or authentication type.'}</p>}
      <TextField label={zh ? '累计调用上限' : 'Lifetime call limit'} type="number" min={1} max={9007199254740991} step={1} required value={String(limit)} onChange={e => setLimit(Number(e.target.value))} />
      <TextField label={zh ? '并发上限' : 'Concurrent request limit'} type="number" min={1} max={16} step={1} required value={String(concurrent)} onChange={e => setConcurrent(Number(e.target.value))} />
      <TextField label={zh ? '请求超时（秒）' : 'Request timeout (seconds)'} type="number" min={1} max={60} step={1} required value={String(timeout)} onChange={e => setTimeout(Number(e.target.value))} />
    </form>
  </Dialog>{reauth.dialog}</>;
}
function CallHistory({ id }: { id: string }) {
  const { locale } = useCopy(), zh = locale === 'zh', query = useQuery<MCPCall[]>(`/api/mcp/connections/${id}/history`);
  useErrorToast(query.error, query.reload);
  const states: Record<string, string> = zh ? { prepared: '等待执行', running: '执行中', succeeded: '成功', tool_error: '工具返回错误', failed: '未发出调用', unknown: '结果未知' } : { prepared: 'Prepared', running: 'Running', succeeded: 'Succeeded', tool_error: 'Tool error', failed: 'Not dispatched', unknown: 'Unknown outcome' };
  return <div><p className="muted">{zh ? '展示最近 100 次调用。结果未知可能已产生外部影响，不会自动重试。' : 'Latest 100 calls. Unknown outcomes may have external effects and are not retried automatically.'}</p><Button size="sm" variant="secondary" onClick={query.reload}>{zh ? '刷新记录' : 'Refresh history'}</Button>{!query.data ? <Loading /> : !query.data.length ? <Empty title={zh ? '暂无调用记录' : 'No calls yet'} /> : <ul className="mcp-history">{query.data.map(r => <li key={`${r.OwnerID}/${r.ID}`}><strong>{r.ToolName}</strong><span>{states[r.State] ?? r.State} · {r.DurationMS} ms</span><small>{new Date(r.CreatedAt).toLocaleString(locale)}</small>{r.Failure && <small>{r.Failure}</small>}</li>)}</ul>}</div>;
}
