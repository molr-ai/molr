import { useEffect, useState } from 'react';
import { Badge, Button, Dialog, Select, Switch, TextField } from '@molr/ui';
import { api, type Assistant } from '@molr/sdk';
import type { PluginInstallation, PluginInstance } from '@molr/sdk/plugins';
import { useCopy, useQuery } from '../state';
import { useNavigation } from '../navigation';
import { Confirm, ErrorNotice, Loading, useAction } from '../interfaces/official/components/common';
import { useReauthentication } from '../interfaces/official/components/ReauthenticationDialog';

export function PluginInstances({ plugin, onClose }: { plugin: PluginInstallation; onClose: () => void }) {
  const { locale } = useCopy(), zh = locale === 'zh', assistants = useQuery<Assistant[]>('/api/assistants', true);
  const [assistantID, setAssistantID] = useState('');
  const active = assistants.data?.filter(a => a.State === 'active') ?? [];
  const selected = assistantID || active[0]?.ID || '';
  return <Dialog open title={zh ? '助理环境与账号' : 'Assistant environments'} onOpenChange={open => { if (!open) onClose(); }}>
    <p>{zh ? '每个助理在自己的工作区安装 CLI，独立维护依赖和登录状态。' : 'Each assistant installs the CLI in its own workspace, with separate dependencies and login state.'}</p>
    <ErrorNotice error={assistants.error} />
    {!assistants.data && !assistants.error && <Loading />}
    {assistants.data && !active.length && <p>{zh ? '先聘请一个助理。' : 'Hire an assistant first.'}</p>}
    {!!active.length && <Select label={zh ? '助理' : 'Assistant'} value={selected} options={active.map(a => ({ value: a.ID, label: a.Name }))} onValueChange={setAssistantID} />}
    {selected && <InstanceEditor key={`${plugin.ID}/${selected}`} plugin={plugin} assistantID={selected} onClose={onClose} />}
  </Dialog>;
}
function InstanceEditor({ plugin, assistantID, onClose }: { plugin: PluginInstallation; assistantID: string; onClose: () => void }) {
  const { locale } = useCopy(), zh = locale === 'zh', action = useAction(), reauth = useReauthentication(), { navigate } = useNavigation();
  const [data, setData] = useState<PluginInstance | null>(null), [error, setError] = useState<unknown>(null), [revision, setRevision] = useState(0);
  const [release, setRelease] = useState(plugin.Manifest.version), [cleaning, setCleaning] = useState(false), [secrets, setSecrets] = useState<Record<string, string>>({});
  useEffect(() => {
    const controller = new AbortController(); setError(null);
    void api.read<PluginInstance>('/api/plugins/instances/read', { ID: plugin.ID, AssistantID: assistantID }, controller.signal).then(value => { if (!controller.signal.aborted) { setData(value); setRelease(value.Release || plugin.Manifest.version); } }).catch(error => { if (!controller.signal.aborted) setError(error); });
    return () => controller.abort();
  }, [plugin.ID, plugin.Manifest.version, assistantID, revision]);
  if (!data) return <><ErrorNotice error={error} />{!error && <Loading />}</>;
  const request = { ID: plugin.ID, AssistantID: assistantID, ExpectedVersion: data.Version };
  const save = (enabled: boolean) => action.run(async () => { await api.post('/api/plugins/instances/save', { ...request, Release: release, Enabled: enabled }); setRevision(r => r + 1); });
  const states: Record<string, string> = zh ? { pending: '待准备', installing: '安装中', ready: '已就绪', failed: '安装失败', unknown: '未检查', not_required: '无需登录' } : { pending: 'Pending', installing: 'Installing', ready: 'Ready', failed: 'Failed', unknown: 'Not checked', not_required: 'Not required' };
  return <div className="plugin-editor-section">
    <Switch label={zh ? '为此助理启用' : 'Enable for this assistant'} checked={data.Enabled} disabled={action.busy || (!data.Enabled && !plugin.Enabled)} onChange={e => { void save(e.target.checked); }} />
    <Select label={zh ? '此助理的插件版本' : 'Plugin version for this assistant'} value={release} options={(plugin.Versions ?? []).map(r => ({ value: r.Version, label: r.Version }))} onValueChange={setRelease} />
    {release !== data.Release && data.Enabled && <Button loading={action.busy} onClick={() => { void save(true); }}>{zh ? '准备此版本' : 'Prepare this version'}</Button>}
    <div className="plugin-card-tags"><span>{zh ? '环境' : 'Environment'}</span><Badge tone={data.Environment === 'ready' ? 'positive' : 'neutral'}>{states[data.Environment] ?? data.Environment}</Badge><span>{zh ? '账号' : 'Account'}</span><Badge>{data.Account === 'pending' ? (zh ? '待登录' : 'Sign-in required') : states[data.Account] ?? data.Account}</Badge></div>
    <p className="plugin-editor-hint">{zh ? '状态来自助理最近一次检查。启用会准备包文件，CLI 由助理在获准后安装。升级保留旧版本和账号数据。' : 'Status reflects the assistant’s last check. Enabling prepares package files; the assistant installs the CLI after authorization. Upgrades retain older versions and account data.'}</p>
    <code className="plugin-workspace-path">{data.Workspace}</code>
    {(data.Requirements ?? plugin.Manifest.requirements ?? []).map((r, i) => <p key={i}>{r.phase === 'install' ? (zh ? '安装' : 'Install') : (zh ? '使用' : 'Use')} · {r.description}{r.destinations?.length ? ` (${r.destinations.join(', ')})` : ''}</p>)}
    <p className="plugin-editor-hint">{zh ? '需求声明不授予权限；助理会申请缺少的资源。' : 'Requirements do not grant access. The assistant requests missing resources.'}</p>
    <div className="plugin-card-actions"><Button disabled={!data.Enabled} onClick={() => { onClose(); navigate('assistants', assistantID); }}>{zh ? '交给助理安装或登录' : 'Open assistant to install or sign in'}</Button><Button variant="secondary" disabled={action.busy} onClick={() => setRevision(r => r + 1)}>{zh ? '刷新状态' : 'Refresh status'}</Button></div>
    {!!data.Credentials?.length && data.Enabled && <section className="settings-group"><h3>{zh ? 'CLI 凭证' : 'CLI credentials'}</h3><p>{zh ? '凭证只写入此助理工作区的 config/secrets/，供 CLI 读取；界面不回显，不发送到聊天。该助理的其他程序也能读取这些文件。留空保留已有值。' : 'Written only to config/secrets/ in this assistant’s workspace for the CLI. Values are not displayed or sent to chat. Other programs belonging to this assistant can read them. Leave blank to retain existing values.'}</p>
      {data.Credentials.map(field => <div key={field.id}><TextField type="password" autoComplete="new-password" label={field.title} value={secrets[field.id] ?? ''} onChange={e => setSecrets(old => ({ ...old, [field.id]: e.target.value }))} /><Button disabled={!secrets[field.id] || action.busy} onClick={() => { void action.run(async () => { const verification = await reauth.verify('credential.put'); await api.post('/api/plugins/instances/credential', { ...request, Field: field.id, Value: secrets[field.id], Verification: verification }); setSecrets(old => ({ ...old, [field.id]: '' })); }); }}>{zh ? '保存凭证' : 'Save credential'}</Button></div>)}
    </section>}
    {!!data.Version && <Button variant="secondary" disabled={data.Enabled || action.busy} onClick={() => setCleaning(true)}>{zh ? '清理此助理的安装与账号数据' : 'Clean this assistant’s installation and account data'}</Button>}
    <ErrorNotice error={error || action.error} />
    {cleaning && <Confirm danger title={zh ? '清理插件数据' : 'Clean plugin data'} description={zh ? '删除此助理的全部插件版本、依赖、配置、登录状态和插件数据。请先让助理停止相关 CLI 任务。其他助理不受影响。' : 'Delete this assistant’s plugin versions, dependencies, configuration, login state and plugin data. Stop related CLI tasks first. Other assistants are unaffected.'} onClose={() => setCleaning(false)} onConfirm={async () => { await api.post('/api/plugins/instances/clean', request); setCleaning(false); setRevision(r => r + 1); }} />}
    {reauth.dialog}
  </div>;
}

export function RetainedPluginInstances({ onClose }: { onClose: () => void }) {
  const { locale } = useCopy(), zh = locale === 'zh', query = useQuery<PluginInstance[]>('/api/plugins/instances', true), assistants = useQuery<Assistant[]>('/api/assistants', true), action = useAction();
  const [cleaning, setCleaning] = useState<PluginInstance | null>(null);
  return <Dialog open title={zh ? '助理安装记录' : 'Assistant installation records'} onOpenChange={open => { if (!open) onClose(); }}>
    <p>{zh ? '插件卸载或撤权后，仍可在这里清理自己助理保留的安装与账号数据。先停止相关 CLI 任务。' : 'Clean your assistants’ retained installations and account data even after a plugin is removed or access is revoked. Stop related CLI tasks first.'}</p>
    <ErrorNotice error={query.error || action.error} />
    {!query.data && !query.error && <Loading />}
    {query.data?.map(instance => <section className="plugin-editor-section" key={`${instance.PluginID}/${instance.AssistantID}`}><strong>{instance.PluginID} · {instance.Release}</strong><span>{zh ? '助理' : 'Assistant'}: {assistants.data?.find(a => a.ID === instance.AssistantID)?.Name ?? instance.AssistantID}</span><Button variant="secondary" loading={action.busy} onClick={() => setCleaning(instance)}>{zh ? '停用并清理' : 'Disable and clean'}</Button></section>)}
    {query.data?.length === 0 && <p>{zh ? '暂无保留的安装。' : 'No retained installations.'}</p>}
    {cleaning && <Confirm danger title={zh ? '清理助理插件数据' : 'Clean assistant plugin data'} description={zh ? '删除所选助理的此插件全部版本、配置、登录状态和数据。' : 'Delete every version, configuration, login state and data for this plugin and assistant.'} onClose={() => setCleaning(null)} onConfirm={async () => {
      const request = { ID: cleaning.PluginID, AssistantID: cleaning.AssistantID, ExpectedVersion: cleaning.Version };
      const disabled = cleaning.Enabled ? await api.post<PluginInstance>('/api/plugins/instances/save', { ...request, Enabled: false }) : cleaning;
      await api.post('/api/plugins/instances/clean', { ...request, ExpectedVersion: disabled.Version }); setCleaning(null); query.reload();
    }} />}
  </Dialog>;
}
