import { usePluginsVisible, pluginResourceVisible } from '../plugins/visibility';
import { AdminUsage } from './Usage';
import { WorkbenchRetentionSettings } from './WorkbenchRetention';
import { useState } from 'react';
import { AssistantAvatar, Badge, Button, Checkbox, IconButton, Select, Tabs, TextField, useBreakpoint } from '@molr/ui';
import { ChartNoAxesCombined, ChevronLeft, ChevronRight, Plus, Settings, Shield, Users } from 'lucide-react';
import { api } from '../api/client';
import { useCopy, useQuery, useSession } from '../state';
import { useNavigation } from '../navigation';
import { Confirm, EditDialog, Empty, ErrorNotice, Loading, PageHeader, micros } from '../interfaces/official/components/common';
import { ConstraintsEditor, suggestedConstraints } from '../interfaces/official/features/ResourceConstraints';
import { emptyQuota, groupName } from './types';
import type { CatalogItem, ResourceQuota, ResourceReference, UserGroup, UserInfo } from './types';
import '../interfaces/official/features/settings.css';
import './administration.css';

export function AdminSettings({ initialTab, onSelectSection }: { initialTab: string; onSelectSection: (value: string) => void }) {
  const { locale } = useCopy(), zh = locale === 'zh', mobile = useBreakpoint('mobile');
  const { principal } = useSession(), { navigate } = useNavigation();
  if (!principal?.User?.Administrator) return <section className="settings-workspace"><PageHeader title={zh ? '管理员设置' : 'Admin settings'} icon={<Settings />} /><Empty title={zh ? '此页面仅管理员可访问' : 'Administrator access required'} description={zh ? '你可以在用户设置中管理自己的账号。' : 'Manage your own account in User settings.'} action={<Button onClick={() => navigate('account')}>{zh ? '用户设置' : 'User settings'}</Button>} /></section>;
  const sections = [
    { id: 'users', label: zh ? '用户' : 'Users', icon: Users, content: <UsersPanel /> },
    { id: 'groups', label: zh ? '用户组' : 'User groups', icon: Shield, content: <GroupsPanel /> },
    { id: 'usage', label: zh ? '用量统计' : 'Usage statistics', icon: ChartNoAxesCombined, content: <AdminUsage /> },
    { id: 'workbench', label: zh ? '开发工作台' : 'Workbench', icon: Settings, content: <WorkbenchRetentionSettings /> },
  ];
  const active = sections.some(s => s.id === initialTab) ? initialTab : mobile ? '' : 'users', current = sections.find(s => s.id === active);
  return <section className="settings-workspace"><PageHeader className="settings-header" title={mobile && current ? current.label : zh ? '管理员设置' : 'Admin settings'} icon={<Settings />} leading={<IconButton label={zh ? '返回' : 'Back'} onClick={() => mobile && current ? onSelectSection('') : navigate('account')}><ChevronLeft size={18} /></IconButton>} />
    <div className="settings-body"><nav className="settings-nav" aria-label={zh ? '管理员设置' : 'Admin settings'} hidden={mobile && !!current}>{sections.map(s => <button key={s.id} type="button" aria-current={active === s.id ? 'page' : undefined} onClick={() => onSelectSection(s.id)}><s.icon size={16} className="settings-nav-icon" /><span>{s.label}</span><ChevronRight size={16} className="settings-nav-caret" /></button>)}</nav><div className="settings-panel" hidden={mobile && !current}>{current && <div className="settings-pane" key={active}><header className="settings-pane-header"><h2>{current.label}</h2></header>{current.content}</div>}</div></div>
  </section>;
}

function UsersPanel() {
  const { locale, t } = useCopy(), zh = locale === 'zh';
  const query = useQuery<UserInfo[]>('/api/admin/users', true), groups = useQuery<UserGroup[]>('/api/admin/groups');
  const [editing, setEditing] = useState<UserInfo | 'new' | null>(null);
  return <div className="settings-section"><div className="settings-toolbar"><Button size="sm" onClick={() => setEditing('new')}><Plus size={14} />{zh ? '添加用户' : 'Add user'}</Button></div><ErrorNotice error={query.error || groups.error} retry={query.reload} />{!query.data ? <Loading /> : <div className="settings-rows">{query.data.map(u => <article className="settings-row-item" key={u.ID}><div className="settings-row-item-head"><span className="user-avatar" aria-hidden="true"><AssistantAvatar name={u.DisplayName || u.Username} avatar={u.Avatar} /></span><h3>{u.DisplayName || u.Username}</h3><Badge tone={u.Disabled ? 'neutral' : 'positive'}>{u.Disabled ? zh ? '已停用' : 'Disabled' : zh ? '正常' : 'Active'}</Badge><Button size="sm" variant="ghost" onClick={() => setEditing(u)}>{t.edit}</Button></div><p className="muted">{u.Username} · {groups.data ? groupName(groups.data.find(g => g.ID === u.GroupID) ?? { ID: u.GroupID, Name: u.GroupName }, zh) : u.GroupName}</p></article>)}</div>}{editing && groups.data && <UserEditor user={editing === 'new' ? undefined : editing} groups={groups.data} onClose={() => setEditing(null)} />}</div>;
}
function UserEditor({ user, groups, onClose }: { user?: UserInfo; groups: UserGroup[]; onClose: () => void }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  const [username, setUsername] = useState(user?.Username ?? ''), [name, setName] = useState(user?.DisplayName ?? ''), [password, setPassword] = useState(''), [group, setGroup] = useState(user?.GroupID ?? 'users'), [disabled, setDisabled] = useState(user?.Disabled ?? false);
  return <EditDialog title={user ? zh ? '编辑用户' : 'Edit user' : zh ? '添加用户' : 'Add user'} onClose={onClose} onSubmit={() => user ? api.mutate('/api/admin/users/update', { ID: user.ID, DisplayName: name, GroupID: group, Disabled: disabled }, user.Version) : api.mutate('/api/admin/users', { Username: username, DisplayName: name, Password: password, GroupID: group })}>
    <TextField label={zh ? '用户名' : 'Username'} value={username} onChange={e => setUsername(e.target.value)} disabled={!!user} required maxLength={128} autoComplete="off" />
    <TextField label={zh ? '显示名称' : 'Display name'} value={name} onChange={e => setName(e.target.value)} maxLength={128} />
    {!user && <TextField label={zh ? '初始密码' : 'Initial password'} type="password" value={password} onChange={e => setPassword(e.target.value)} required minLength={12} autoComplete="new-password" description={zh ? '至少 12 个字符。用户可以在自己的安全设置中修改密码。' : 'At least 12 characters. The user can change it in their security settings.'} />}
    <Select label={zh ? '用户组' : 'User group'} value={group} onValueChange={setGroup} options={groups.map(g => ({ value: g.ID, label: groupName(g, zh) }))} />
    {user && <Checkbox label={zh ? '停用账号' : 'Disable account'} checked={disabled} onChange={e => setDisabled(e.target.checked)} description={zh ? '停用或换组会撤销现有登录和访问令牌，私人数据保留。' : 'Disabling or changing group revokes sessions and access tokens. Private data is retained.'} />}
  </EditDialog>;
}

function GroupsPanel() {
  const pluginsVisible = usePluginsVisible();
  const { locale, t } = useCopy(), zh = locale === 'zh';
  const query = useQuery<UserGroup[]>('/api/admin/groups'), catalog = useQuery<CatalogItem[]>('/api/admin/catalog');
  const [editing, setEditing] = useState<UserGroup | 'new' | null>(null), [removing, setRemoving] = useState<UserGroup | null>(null);
  return <div className="settings-section"><p className="muted">{zh ? '组内每位成员独立继承资源额度，所有助理合计使用；助理自身的授权和限额继续生效。' : 'Each member inherits an independent allowance across all their assistants. Assistant permissions and limits still apply.'}</p><div className="settings-toolbar"><Button size="sm" onClick={() => setEditing('new')}><Plus size={14} />{zh ? '添加用户组' : 'Add group'}</Button></div><ErrorNotice error={query.error || catalog.error} retry={query.reload} />{!query.data ? <Loading /> : <div className="settings-rows">{query.data.map(g => <article className="settings-row-item" key={g.ID}><div className="settings-row-item-head"><h3>{groupName(g, zh)}</h3>{g.Administrator ? <Badge>{zh ? '全部资源 · 无组限额' : 'All resources · no group limits'}</Badge> : <><Button size="sm" variant="ghost" onClick={() => setEditing(g)}>{t.edit}</Button>{g.ID !== 'users' && <Button size="sm" variant="ghost" onClick={() => setRemoving(g)}>{zh ? '删除' : 'Delete'}</Button>}</>}</div><p className="muted">{g.Administrator ? zh ? '自动包含新增资源，助理级限额仍生效。' : 'New resources are included automatically. Assistant limits still apply.' : zh ? `引用 ${g.Resources.filter(ref => pluginResourceVisible(ref.Kind, pluginsVisible, ref.ID)).length} 项资源` : `${g.Resources.filter(ref => pluginResourceVisible(ref.Kind, pluginsVisible, ref.ID)).length} resource references`}</p></article>)}</div>}{editing && catalog.data && <GroupEditor group={editing === 'new' ? undefined : editing} catalog={catalog.data} onClose={() => setEditing(null)} />}{removing && <Confirm title={zh ? '删除用户组' : 'Delete group'} description={zh ? '请先将该组所有成员移至其他组。删除用户组不会删除资源。' : 'Move every member to another group first. Resources are not deleted.'} danger onClose={() => setRemoving(null)} onConfirm={() => api.mutate('/api/admin/groups/delete', { ID: removing.ID }, removing.Version)} />}</div>;
}
function GroupEditor({ group, catalog, onClose }: { group?: UserGroup; catalog: CatalogItem[]; onClose: () => void }) {
  const pluginsVisible = usePluginsVisible();
  const { locale } = useCopy(), zh = locale === 'zh';
  const [name, setName] = useState(group?.Name ?? ''), [refs, setRefs] = useState<ResourceReference[]>(group?.Resources ?? []), [tab, setTab] = useState('model'), [quota, setQuota] = useState<ResourceReference | null>(null);
  const types = [{ value: 'plugin', label: zh ? '插件' : 'Plugins' }, { value: 'model', label: zh ? '模型' : 'Models' }, { value: 'tool', label: zh ? '工具' : 'Tools' }, { value: 'sandbox', label: zh ? '沙盒' : 'Sandboxes' }, { value: 'connection', label: zh ? '服务连接' : 'Connections' }].filter(item => pluginResourceVisible(item.value, pluginsVisible));
  const activeTab = types.some(item => item.value === tab) ? tab : 'model';
  const missing = refs.filter(ref => pluginResourceVisible(ref.Kind, pluginsVisible, ref.ID) && !catalog.some(c => c.Kind === ref.Kind && c.ID === ref.ID));
  return <><EditDialog title={group ? zh ? '编辑用户组' : 'Edit group' : zh ? '添加用户组' : 'Add group'} onClose={onClose} onSubmit={() => api.mutate('/api/admin/groups', { Group: { ID: group?.ID ?? '', Name: name, Administrator: false, Resources: refs } }, group?.Version)}>
    <TextField label={zh ? '组名' : 'Group name'} value={name} onChange={e => setName(e.target.value)} required maxLength={128} />
    <p className="muted">{zh ? '这里只引用资源页面已有的资源，不修改资源配置。移除引用后，成员的已有助理也会失去该资源的使用权。' : 'Reference existing resources without changing their configuration. Removing a reference also revokes access for existing assistants.'}</p>
    <Tabs label={zh ? "资源类型" : "Resource type"} size="sm" items={types.map(item => ({ ...item, content: null }))} value={activeTab} onValueChange={setTab} />
    <div className="group-resource-list">{catalog.filter(c => c.Kind === activeTab && pluginResourceVisible(c.Kind, pluginsVisible, c.ID)).map(item => { const ref = refs.find(r => r.Kind === item.Kind && r.ID === item.ID); return <div className="group-resource-row" key={`${item.Kind}:${item.ID}`}><Checkbox label={item.Name} checked={!!ref} onChange={e => setRefs(old => e.target.checked ? [...old, { Kind: item.Kind, ID: item.ID, Quota: emptyQuota() }] : old.filter(r => r.Kind !== item.Kind || r.ID !== item.ID))} />{ref && item.Kind !== 'plugin' && <Button size="sm" variant="secondary" onClick={() => setQuota(ref)}>{item.Kind === 'sandbox' ? (zh ? '权限与配额' : 'Permissions and quotas') : (zh ? '配额' : 'Quotas')}</Button>}</div>; })}{!catalog.some(c => c.Kind === activeTab && pluginResourceVisible(c.Kind, pluginsVisible, c.ID)) && <p className="muted">{zh ? '暂无可引用资源，请先在资源页面配置。' : 'No resources available. Configure them on the Resources page first.'}</p>}</div>
    {missing.map(ref => <div className="group-resource-row" key={`${ref.Kind}:${ref.ID}`}><span>{zh ? '资源已不可用' : 'Resource unavailable'} · {ref.ID}</span><Button size="sm" variant="ghost" onClick={() => setRefs(old => old.filter(r => r !== ref))}>{zh ? '移除引用' : 'Remove reference'}</Button></div>)}
  </EditDialog>{quota && pluginResourceVisible(quota.Kind, pluginsVisible, quota.ID) && <QuotaEditor reference={quota} item={catalog.find(c => c.Kind === quota.Kind && c.ID === quota.ID)!} onClose={() => setQuota(null)} onSave={(q, allowFullAccess) => { setRefs(old => old.map(r => r.Kind === quota.Kind && r.ID === quota.ID ? { ...r, Quota: q, AllowFullAccess: allowFullAccess || undefined } : r)); setQuota(null); }} />}</>;
}
function QuotaEditor({ reference, item, onSave, onClose }: { reference: ResourceReference; item: CatalogItem; onSave: (q: ResourceQuota, allowFullAccess: boolean) => void; onClose: () => void }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  const [q, setQ] = useState<ResourceQuota>(structuredClone(reference.Quota));
  const [allowFullAccess, setAllowFullAccess] = useState(reference.AllowFullAccess ?? false);
  const [budget, setBudget] = useState(q.Budget ? String(q.Budget.Micros / 1e6) : ''), [currency, setCurrency] = useState(q.Budget?.Currency ?? 'USD');
  const change = (next: Partial<ResourceQuota>) => setQ(old => ({ ...old, ...next }));
  return <EditDialog notifyOnSave={false} title={`${item.Name} · ${item.Kind === 'sandbox' ? (zh ? '权限与配额' : 'Permissions and quotas') : (zh ? '配额' : 'Quotas')}`} onClose={onClose} onSubmit={async () => onSave({ ...q, Budget: budget.trim() ? { Currency: currency, Micros: micros(budget) } : undefined }, allowFullAccess)}>
    <p className="muted">{zh ? '以下上限分别应用于每位成员，不是全组共享余额。' : 'These limits apply independently to each member, not to a shared group balance.'}</p>
    {item.Kind === 'model' && <div className="admin-form-grid"><TextField type="number" min={0} step="any" label={zh ? '费用上限（留空不限）' : 'Spend limit (blank: unlimited)'} value={budget} onChange={e => setBudget(e.target.value)} /><Select label={zh ? '币种' : 'Currency'} value={currency} onValueChange={setCurrency} options={[{ value: 'USD', label: 'USD' }, { value: 'CNY', label: 'CNY' }]} /></div>}
    {item.Kind !== 'sandbox' && <TextField type="number" min={0} step={1} label={zh ? '调用次数上限（0 不限）' : 'Call limit (0: unlimited)'} value={String(q.Calls)} onChange={e => change({ Calls: Number(e.target.value) })} required />}
    <TextField type="number" min={0} step={1} label={zh ? '并发上限（0 不限）' : 'Concurrent limit (0: unlimited)'} value={String(q.Concurrent)} onChange={e => change({ Concurrent: Number(e.target.value) })} required />
    {item.Kind !== 'sandbox' && <div className="admin-form-grid"><Select label={zh ? '统计周期' : 'Usage period'} value={q.Period || 'month'} onValueChange={Period => change({ Period })} options={[{ value: 'day', label: zh ? '每天' : 'Daily' }, { value: 'week', label: zh ? '每周' : 'Weekly' }, { value: 'month', label: zh ? '每月' : 'Monthly' }, { value: 'lifetime', label: zh ? '累计' : 'Lifetime' }]} /><TextField label={zh ? '周期时区' : 'Period timezone'} value={q.Timezone || 'UTC'} onChange={e => change({ Timezone: e.target.value })} required /></div>}
    {item.Kind === 'sandbox' && <div className="admin-quota-sections">
      {(item.FullAccessAvailable || allowFullAccess) && <Checkbox label={zh ? '允许授予 Full Access' : 'Allow Full Access grants'} checked={allowFullAccess} onChange={e => setAllowFullAccess(e.target.checked)} description={zh ? '成员可以按需授予助理 Full Access；这些调用不受隔离和配额限制。普通调用继续使用以下配额。' : 'Members may grant Full Access to assistants. Those calls have no isolation or quotas. Ordinary calls retain the quotas below.'} />}
      <Checkbox label={zh ? '限制计算资源' : 'Limit compute'} checked={!!q.Compute} onChange={e => change({ Compute: e.target.checked ? suggestedConstraints('compute')!.compute : undefined })} />
      {q.Compute && <ConstraintsEditor gpus={item.GPUs} kind="compute" compact value={{ compute: q.Compute }} onChange={v => change({ Compute: v?.compute })} />}
      <Checkbox label={zh ? '限制网络用量' : 'Limit network usage'} checked={!!q.Network} onChange={e => change({ Network: e.target.checked ? suggestedConstraints('network')!.network : undefined })} />
      {q.Network && <ConstraintsEditor kind="network" compact value={{ network: q.Network }} onChange={v => change({ Network: v?.network })} />}
      <Checkbox label={zh ? '限制工作区存储' : 'Limit workspace storage'} checked={!!q.Storage} disabled={!item.WorkspaceQuotaSupported} onChange={e => change({ Storage: e.target.checked ? suggestedConstraints('storage')!.storage : undefined })} />
      {!item.WorkspaceQuotaSupported && <p className="muted">{zh ? '该沙盒未配置存储硬配额。' : 'Storage hard quotas are not configured on this sandbox.'}</p>}
      {q.Storage && <ConstraintsEditor kind="storage" compact value={{ storage: q.Storage }} onChange={v => change({ Storage: v?.storage })} />}
    </div>}
  </EditDialog>;
}
