import { mutationKey } from '../api/client';
import { useCallback, useEffect, useId, useState } from 'react';
import { Badge, Button, Checkbox, Dialog, Select, TextField } from '@molr/ui';
import { api } from '@molr/sdk';
import type { PluginConnection, PluginInstallation, PluginUserSettings } from '@molr/sdk/plugins';
import { useCopy } from '../state';
import { Confirm, ErrorNotice, Loading, useAction } from '../interfaces/official/components/common';
import { useReauthentication } from '../interfaces/official/components/ReauthenticationDialog';
import { PluginFrame } from './PluginFrame';
import './plugins.css';

function useConnections(plugin: PluginInstallation) {
  const [data, setData] = useState<PluginConnection[] | null>(null), [error, setError] = useState<unknown>(null), [revision, setRevision] = useState(0);
  const reload = useCallback(() => setRevision(v => v + 1), []);
  useEffect(() => {
    let disposed = false; setError(null); setData(null);
    void api.post<PluginConnection[]>('/api/plugins/connections/list', { PluginID: plugin.ID, Digest: plugin.Digest }).then(value => { if (!disposed) setData(value); }).catch(error => { if (!disposed) setError(error); });
    return () => { disposed = true; };
  }, [plugin.ID, plugin.Digest, plugin.Version, revision]);
  return { data, error, reload };
}

export function PluginSettings({ plugin, administrator = false }: { plugin: PluginInstallation; administrator?: boolean }) {
  const schema = administrator ? plugin.Manifest.adminSettings : plugin.Manifest.settings;
  const page = administrator ? plugin.Manifest.adminSettingsPage : plugin.Manifest.settingsPage;
  if (page) return <PluginFrame reference={{ ID: plugin.ID, Digest: plugin.Digest, Kind: administrator ? 'admin-settings' : 'settings', Contribution: page.id, Page: '' }} title={page.title} />;
  return <SettingsForm plugin={plugin} schema={schema ?? {}} administrator={administrator} />;
}
function SettingsForm({ plugin, schema, administrator }: { plugin: PluginInstallation; schema: NonNullable<PluginInstallation['Manifest']['settings']>; administrator: boolean }) {
  const { locale } = useCopy(), zh = locale === 'zh', action = useAction(), connections = useConnections(plugin);
  const [base, setBase] = useState<PluginUserSettings | null>(null), [values, setValues] = useState<PluginUserSettings['Values']>({}), [error, setError] = useState<unknown>(null), [revision, setRevision] = useState(0), [saved, setSaved] = useState(false);
  const scope = administrator ? 'admin' : '';
  useEffect(() => {
    let disposed = false; setError(null);
    void api.post<PluginUserSettings>('/api/plugins/settings/read', { ID: plugin.ID, Digest: plugin.Digest, Scope: scope }).then(value => { if (!disposed) { setBase(value); setValues(value.Values); } }).catch(e => { if (!disposed) setError(e); });
    return () => { disposed = true; };
  }, [plugin.ID, plugin.Digest, revision, scope]);
  const change = (key: string, value: string | number | boolean) => { setValues(old => ({ ...old, [key]: value })); setSaved(false); };
  return <>
    <ErrorNotice error={error || connections.error} retry={() => { setRevision(v => v + 1); connections.reload(); }} />
    {!base ? !error && <Loading /> : <form className="editor plugin-settings-form" onSubmit={event => { event.preventDefault(); void action.run(async () => {
      const next = await api.post<PluginUserSettings>('/api/plugins/settings/save', { ID: plugin.ID, Digest: plugin.Digest, Scope: scope, ExpectedVersion: base.Version, Values: values }); setBase(next); setValues(next.Values); setSaved(true);
    }); }}>
      {base.NeedsReview && <p role="status">{zh ? '插件版本已更新，请检查并重新保存个人配置。' : 'The plugin has changed. Review and save your settings.'}</p>}
      {Object.entries(schema).map(([key, field]) => {
        const value = values[key] ?? field.default ?? '';
        if (field.type === 'connection') {
          const choices = (connections.data ?? []).filter(c => c.Enabled && c.Configured && !c.NeedsReview && field.providers?.includes(c.ProviderID) && (field.requiredOperations ?? []).every(op => c.Operations.includes(op)));
          const missing = typeof value === 'string' && value !== '' && !choices.some(c => c.ID === value);
          return <div key={key} className="plugin-setting-field"><Select label={field.title} value={String(value)} onValueChange={v => change(key, v)} options={[{ value: '', label: zh ? '选择服务连接' : 'Select a connection' }, ...(missing ? [{ value: String(value), label: zh ? '原连接不可用，请重新选择' : 'Previous connection unavailable' }] : []), ...choices.map(c => ({ value: c.ID, label: `${c.Name} · ${plugin.Manifest.providers?.find(p => p.id === c.ProviderID)?.name ?? c.ProviderID}` }))]} />{field.description && <p className="muted">{field.description}</p>}{!choices.length && <p className="muted">{zh ? '请联系管理员配置服务连接并授权给你的用户组。' : 'Ask an administrator to configure a connection and authorize your group.'}</p>}</div>;
        }
        if (field.type === 'boolean') return <Checkbox key={key} label={field.title} checked={value === true} onChange={e => change(key, e.target.checked)} />;
        if (field.enum?.length) return <Select key={key} label={field.title} value={String(value)} options={field.enum.map(v => ({ value: v, label: v }))} onValueChange={v => change(key, v)} />;
        const numeric = field.type === 'number' || field.type === 'integer';
        return <TextField key={key} label={field.title} type={numeric ? 'number' : 'text'} value={String(value)} min={field.minimum} max={field.maximum} step={field.type === 'integer' ? 1 : 'any'} maxLength={8192} required onChange={e => change(key, numeric ? Number(e.target.value) : e.target.value)} />;
      })}
      <ErrorNotice error={action.error} />
      <div className="plugin-settings-actions"><Button type="submit" loading={action.busy}>{administrator ? (zh ? '保存管理员配置' : 'Save admin settings') : (zh ? '保存个人配置' : 'Save personal settings')}</Button><Button variant="secondary" onClick={() => { setRevision(v => v + 1); connections.reload(); setSaved(false); }}>{zh ? '重新加载' : 'Reload settings'}</Button></div>
      {saved && <p role="status">{zh ? '已保存，下次调用生效。' : 'Saved. Applies to the next call.'}</p>}
    </form>}
  </>;
}

export function PluginConnections({ plugin }: { plugin: PluginInstallation }) {
  const { locale } = useCopy(), zh = locale === 'zh', query = useConnections(plugin), action = useAction(), reauth = useReauthentication();
  const [history, setHistory] = useState<PluginConnection | null>(null);
  const [editing, setEditing] = useState<PluginConnection | 'new' | null>(null), [removing, setRemoving] = useState<PluginConnection | null>(null), [testResult, setTestResult] = useState('');
  return <div className="plugin-connections">
    <p className="muted">{zh ? '配置服务账号后，在用户组中授权连接。插件提供个人连接选项时，用户可在插件配置中选择。' : 'Configure service accounts, then authorize connections in user groups. Users choose connections when the plugin provides a personal connection option.'}</p>
    <Button size="sm" variant="secondary" onClick={() => setEditing('new')}>{zh ? '添加连接' : 'Add connection'}</Button>
    <ErrorNotice error={query.error || action.error} retry={query.reload} />
    {!query.data ? !query.error && <Loading /> : query.data.map(c => <article className="plugin-connection-row" key={c.ID}>
      <div><strong>{c.Name}</strong><p className="muted">{plugin.Manifest.providers?.find(p => p.id === c.ProviderID)?.name ?? c.ProviderID} · {c.Calls} / {c.CallLimit} {zh ? '次调用' : 'calls'} · {c.Active} {zh ? '未结调用' : 'unresolved calls'}</p></div>
      <Badge tone={c.Enabled && !c.NeedsReview ? 'positive' : 'neutral'}>{c.NeedsReview ? (zh ? '需要重新授权' : 'Review required') : c.Enabled ? (zh ? '已启用' : 'Enabled') : (zh ? '已停用' : 'Disabled')}</Badge>
      <div className="plugin-settings-actions"><Button size="sm" variant="secondary" onClick={() => setHistory(c)}>{zh ? '调用记录' : 'Call history'}</Button><Button size="sm" variant="secondary" onClick={() => setEditing(c)}>{zh ? '编辑' : 'Edit'}</Button><Button size="sm" variant="secondary" disabled={!plugin.Enabled || !plugin.Grants?.includes('http.request') || !c.Enabled || c.NeedsReview || !plugin.Manifest.providers?.find(p => p.id === c.ProviderID)?.test || action.busy} onClick={() => { setTestResult(''); void action.run(async () => {
        const result = await api.post<{ provider: string; duration_ms: number }>('/api/plugins/connections/test', { PluginID: plugin.ID, Digest: plugin.Digest, ID: c.ID, Mutation: { IdempotencyKey: mutationKey() } }); setTestResult(`${c.Name}: ${zh ? '连接测试通过' : 'Connection test passed'} (${result.duration_ms} ms)`);
      }).finally(query.reload); }}>{zh ? '测试连接' : 'Test connection'}</Button><Button size="sm" variant="secondary" onClick={() => setRemoving(c)}>{zh ? '删除' : 'Delete'}</Button></div>
    </article>)}
    <p className="muted">{zh ? '测试会向服务发起一次请求并消耗额度，可能产生费用。累计调用上限包含测试和结果未知的请求。' : 'Tests send one request and use allowance; providers may charge. Lifetime limits include tests and requests with unknown outcomes.'}</p>
    {testResult && <p role="status">{testResult}</p>}
    {history && <ConnectionHistory plugin={plugin} connection={history} onClose={() => { setHistory(null); query.reload(); }} />}
    {editing && <ConnectionEditor plugin={plugin} connection={editing === 'new' ? undefined : editing} onClose={() => setEditing(null)} onSaved={() => { setEditing(null); query.reload(); }} />}
    {removing && <Confirm danger title={zh ? '删除服务连接' : 'Delete connection'} description={zh ? '停止使用该连接并删除其专用凭证。已选择此连接的用户需要重新配置。' : 'Disable this connection and delete its dedicated credential. Users who selected it will need to choose another connection.'} onClose={() => setRemoving(null)} onConfirm={async () => {
      const verification = await reauth.verify('credential.delete'); await api.post('/api/plugins/connections/remove', { PluginID: plugin.ID, Digest: plugin.Digest, ID: removing.ID, Mutation: { IdempotencyKey: mutationKey(), ExpectedVersion: removing.Version }, Verification: verification }); setRemoving(null); query.reload();
    }} />}{reauth.dialog}
  </div>;
}
function ConnectionEditor({ plugin, connection, onClose, onSaved }: { plugin: PluginInstallation; connection?: PluginConnection; onClose: () => void; onSaved: () => void }) {
  const { locale } = useCopy(), zh = locale === 'zh', action = useAction(), reauth = useReauthentication(), formID = useId();
  const [providerID, setProviderID] = useState(connection?.ProviderID ?? plugin.Manifest.providers?.[0]?.id ?? ''), [name, setName] = useState(connection?.Name ?? ''), [key, setKey] = useState(''), [enabled, setEnabled] = useState(connection?.Enabled ?? true), [limit, setLimit] = useState(connection?.CallLimit ?? 1000), [concurrent, setConcurrent] = useState(connection?.Concurrent ?? 2), [timeout, setTimeout] = useState(connection?.TimeoutSeconds ?? 20);
  const provider = plugin.Manifest.providers?.find(p => p.id === providerID);
  return <><Dialog open title={zh ? '服务连接' : 'Service connection'} dismissible={!action.busy} onOpenChange={open => { if (!open && !action.busy) onClose(); }} footer={<><Button variant="secondary" disabled={action.busy} onClick={onClose}>{zh ? '取消' : 'Cancel'}</Button><Button type="submit" form={formID} loading={action.busy}>{zh ? '保存连接' : 'Save connection'}</Button></>}>
    <form id={formID} className="editor" onSubmit={event => { event.preventDefault(); void action.run(async () => {
      const verification = key ? await reauth.verify('credential.put') : '';
      await api.post('/api/plugins/connections/save', { PluginID: plugin.ID, Digest: plugin.Digest, ID: connection?.ID ?? '', ProviderID: providerID, Name: name, Enabled: enabled, APIKey: key, Verification: verification, Mutation: { IdempotencyKey: mutationKey(), ExpectedVersion: connection?.Version ?? 0 }, CallLimit: limit, Concurrent: concurrent, TimeoutSeconds: timeout }); setKey(''); onSaved();
    }); }}>
      <TextField label={zh ? '连接名称' : 'Connection name'} value={name} maxLength={120} onChange={e => setName(e.target.value)} required />
      <Select label={zh ? '服务提供商' : 'Provider'} value={providerID} disabled={!!connection} options={(plugin.Manifest.providers ?? []).map(p => ({ value: p.id, label: p.name }))} onValueChange={v => { setProviderID(v); setKey(''); }} />
      <p className="plugin-connection-origin">{provider?.origin}</p>
      {provider?.auth.type !== 'none' && <TextField label="API Key" type="password" autoComplete="new-password" value={key} maxLength={8192} placeholder={connection?.Configured ? (zh ? '已配置；留空保留' : 'Configured; leave blank to keep') : ''} required={!connection?.Configured} onChange={e => setKey(e.target.value)} />}
      <TextField label={zh ? '累计调用上限' : 'Lifetime call limit'} type="number" min={1} step={1} value={String(limit)} required onChange={e => setLimit(Number(e.target.value))} />
      <TextField label={zh ? '并发上限' : 'Concurrent request limit'} type="number" min={1} max={16} step={1} value={String(concurrent)} required onChange={e => setConcurrent(Number(e.target.value))} />
      <TextField label={zh ? '请求超时（秒）' : 'Request timeout (seconds)'} type="number" min={1} max={60} step={1} value={String(timeout)} required onChange={e => setTimeout(Number(e.target.value))} />
      <Checkbox label={zh ? '启用连接' : 'Enable connection'} checked={enabled} onChange={e => setEnabled(e.target.checked)} />
      {connection?.NeedsReview && <p>{zh ? '保存将授权该连接使用当前插件版本声明的接口。请核对服务地址。' : 'Saving authorizes the interfaces declared by this plugin version. Review the service address.'}</p>}
      <ErrorNotice error={action.error} />
    </form>
  </Dialog>{reauth.dialog}</>;
}

type CallRecord = { ID: string; OwnerID: string; State: string; ProviderID: string; CreatedAt: string; DurationMS: number; StatusCode: number; Failure: string; Test: boolean; Usage: unknown };
function ConnectionHistory({ plugin, connection, onClose }: { plugin: PluginInstallation; connection: PluginConnection; onClose: () => void }) {
 const { locale } = useCopy(), zh = locale === 'zh', action = useAction();
 const [rows, setRows] = useState<CallRecord[] | null>(null), [error, setError] = useState<unknown>(null), [ack, setAck] = useState<CallRecord | null>(null), [revision, setRevision] = useState(0);
 useEffect(() => { let disposed = false; void api.post<CallRecord[]>('/api/plugins/connections/history', { PluginID: plugin.ID, Digest: plugin.Digest, ID: connection.ID }).then(v => { if (!disposed) setRows(v); }).catch(e => { if (!disposed) setError(e); }); return () => { disposed = true; }; }, [connection.ID, plugin.ID, plugin.Digest, revision]);
 const states: Record<string, string> = zh ? { succeeded: '已完成', failed: '失败', prepared: '等待发送', running: '执行中', unknown: '结果未知', acknowledged: '已确认未知结果' } : { succeeded: 'Succeeded', failed: 'Failed', prepared: 'Prepared', running: 'Running', unknown: 'Unknown outcome', acknowledged: 'Acknowledged unknown outcome' };
 return <><Dialog open title={`${connection.Name} · ${zh ? '调用记录' : 'Call history'}`} onOpenChange={open => { if (!open) onClose(); }} footer={<Button variant="secondary" onClick={onClose}>{zh ? '关闭' : 'Close'}</Button>}>
  <p className="muted">{zh ? '最近 100 次调用。费用未计价；供应商用量按原单位展示。' : 'Latest 100 calls. Costs are unpriced; usage retains provider units.'}</p><ErrorNotice error={error || action.error} />
  {!rows ? !error && <Loading /> : <div className="plugin-connections">{rows.map(row => <article className="plugin-connection-row" key={`${row.OwnerID}:${row.ID}`}><strong>{states[row.State] ?? row.State}</strong><p>{new Date(row.CreatedAt).toLocaleString(locale)} · {row.ProviderID} · {row.DurationMS} ms{row.Test ? (zh ? ' · 连接测试' : ' · Connection test') : ''}</p>{row.StatusCode > 0 && <p>HTTP {row.StatusCode}</p>}{row.Usage != null && <p>{zh ? '供应商用量' : 'Provider usage'}: {JSON.stringify(row.Usage)}</p>}{row.State === 'unknown' && <Button size="sm" variant="secondary" onClick={() => setAck(row)}>{zh ? '确认并释放并发占用' : 'Acknowledge and release concurrency'}</Button>}</article>)}</div>}
 </Dialog>{ack && <Confirm title={zh ? '确认未知结果' : 'Acknowledge unknown outcome'} description={zh ? '外部服务可能已处理或仍在处理请求。此操作仅释放本地并发占用，不退还调用次数，也不会重试请求。' : 'The provider may have processed this request or may still be processing it. This only releases local concurrency; it neither refunds the call nor retries it.'} onClose={() => setAck(null)} onConfirm={async () => { await api.post('/api/plugins/connections/acknowledge', { ConnectionID: connection.ID, OwnerID: ack.OwnerID, ID: ack.ID }); setAck(null); setRevision(v => v + 1); }} />}</>;
}
