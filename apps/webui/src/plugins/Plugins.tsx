import { useId, useRef, useState } from 'react';
import { Badge, Button, Checkbox, Dialog, DropdownMenu, IconButton, Select, Switch, Tabs, TextField } from '@molr/ui';
import { ChevronLeft, Ellipsis, PanelsTopLeft, Puzzle, Settings2, Shield, Trash2, Upload } from 'lucide-react';
import { api } from '@molr/sdk';
import type { Assistant, Resource } from '@molr/sdk';
import { pluginAllowed, pluginCapabilities, pluginNeedsCompute } from '@molr/sdk/plugins';
import type { PluginBinding, PluginInstallation, PluginPackage } from '@molr/sdk/plugins';
import { useCopy, useQuery, useSession } from '../state';
import { useUIHost } from '../ui-host/store';
import { useNavigation } from '../navigation';
import { Confirm, ErrorNotice, Loading, PageHeader, useAction } from '../interfaces/official/components/common';
import { PluginFrame } from './PluginFrame';
import { PluginSettings, PluginConnections } from './PluginSettings';
import { PluginGroups } from './PluginGroups';
import { pluginCopy } from './copy';
import '../interfaces/official/features/resources.css';
import '../interfaces/official/features/settings.css';
import './plugins.css';

const hasSettings = (p: PluginInstallation) => !!(p.Manifest.settingsPage || Object.keys(p.Manifest.settings ?? {}).length);
const hasAdminSettings = (p: PluginInstallation) => !!(p.Manifest.adminSettingsPage || Object.keys(p.Manifest.adminSettings ?? {}).length || p.Manifest.providers?.length);

export function ExtensionPages({ id }: { id: string }) {
  const { locale } = useCopy(), zh = locale === 'zh', copy = pluginCopy[locale], host = useUIHost(), { navigate } = useNavigation();
  const administrator = useSession().principal?.User?.Administrator === true, action = useAction(), input = useRef<HTMLInputElement>(null);
  const [search, setSearch] = useState(''), [filter, setFilter] = useState('all');
  if (host.loading) return <Loading />;
  const [pluginID, entry] = id.split('/'), selected = host.plugins.find(p => p.ID === pluginID);
  if (selected) {
    if (entry === '@settings' && hasSettings(selected) && pluginAllowed(selected)) return <section className="extensions-workspace"><PageHeader title={`${selected.Manifest.name} · ${zh ? '配置' : 'Settings'}`} icon={<Settings2 />} leading={<IconButton label={copy.title} onClick={() => navigate('extensions')}><ChevronLeft size={18} /></IconButton>} /><div className="plugin-detail-settings"><PluginSettings plugin={selected} /></div></section>;
    if (entry === '@admin' && administrator && hasAdminSettings(selected)) return <section className="extensions-workspace"><PageHeader title={`${selected.Manifest.name} · ${zh ? '管理员配置' : 'Admin settings'}`} icon={<Shield />} leading={<IconButton label={copy.title} onClick={() => navigate('extensions')}><ChevronLeft size={18} /></IconButton>} /><div className="plugin-detail-settings">{(selected.Manifest.adminSettingsPage || Object.keys(selected.Manifest.adminSettings ?? {}).length > 0) && <PluginSettings plugin={selected} administrator />}{!!selected.Manifest.providers?.length && <PluginConnections plugin={selected} />}</div></section>;
    const page = selected.Manifest.contributes.pages?.find(p => p.id === entry);
    if (page && pluginAllowed(selected, page.capabilities)) return <PluginFrame reference={{ ID: selected.ID, Digest: selected.Digest, Kind: 'page', Contribution: page.id, Page: '' }} title={page.title} />;
  }
  const plugins = host.plugins.filter(p => {
    const enabled = administrator ? p.Enabled : pluginAllowed(p);
    return `${p.Manifest.name} ${p.Manifest.description} ${p.ID}`.toLowerCase().includes(search.toLowerCase()) && (filter === 'all' || filter === 'enabled' && enabled || filter === 'disabled' && !enabled);
  });
  return <section className="extensions-workspace">
    <PageHeader title={copy.title} icon={<Puzzle />} actions={administrator && <Button size="sm" onClick={() => input.current?.click()} disabled={action.busy}><Upload size={14} />{copy.import}</Button>} />
    <input ref={input} type="file" accept=".json,.molr-plugin.json" hidden onChange={event => {
      const file = event.target.files?.[0]; event.target.value = ''; if (!file) return;
      void action.run(async () => { if (file.size > 768 * 1024) throw new Error('Package exceeds 768 KiB'); await api.post('/api/plugins/install', JSON.parse(await file.text()) as PluginPackage); host.reloadPlugins(); });
    }} />
    <div className="plugin-market">
      <div className="plugin-market-filters"><TextField label={zh ? '搜索插件' : 'Search plugins'} type="search" value={search} onChange={e => setSearch(e.target.value)} /><Select label={zh ? '状态' : 'Status'} value={filter} onValueChange={setFilter} options={[{ value: 'all', label: zh ? '全部插件' : 'All plugins' }, { value: 'enabled', label: copy.enabled }, { value: 'disabled', label: copy.disabled }]} /></div>
      <ErrorNotice error={host.pluginError || action.error} retry={host.reloadPlugins} />
      {id && <p role="status">{copy.unavailable}</p>}
      {plugins.length ? <div className="plugin-market-grid">{plugins.map(p => <PluginCard key={p.ID} plugin={p} administrator={administrator} />)}</div> : <div className="resources-empty"><Puzzle size={28} /><h2>{search || filter !== 'all' ? (zh ? '没有匹配的插件' : 'No matching plugins') : copy.empty}</h2><p>{administrator ? copy.help : (zh ? '管理员授权插件后，会显示在这里。' : 'Plugins appear here when your administrator authorizes them.')}</p></div>}
    </div>
  </section>;
}
function PluginCard({ plugin, administrator }: { plugin: PluginInstallation; administrator: boolean }) {
  const { t, locale } = useCopy(), zh = locale === 'zh', copy = pluginCopy[locale], host = useUIHost(), { navigate } = useNavigation(), action = useAction();
  const [editing, setEditing] = useState(false), [groups, setGroups] = useState(false), [remove, setRemove] = useState(false);
  const enabled = administrator ? plugin.Enabled : pluginAllowed(plugin), c = plugin.Manifest.contributes;
  async function toggle(personal = false) {
    await action.run(async () => {
      if (administrator && !personal) {
        if (!plugin.Enabled && !pluginCapabilities(plugin.Manifest).every(cap => plugin.Grants?.includes(cap))) { setEditing(true); return; }
        await api.post('/api/plugins/configure', { ID: plugin.ID, ExpectedVersion: plugin.Version, Enabled: !plugin.Enabled, Grants: plugin.Grants ?? [], Bindings: plugin.Bindings ?? [] });
      } else await api.post('/api/plugins/preference', { ID: plugin.ID, Digest: plugin.Digest, ExpectedVersion: plugin.UserVersion, Enabled: !plugin.UserEnabled });
      host.reloadPlugins();
    });
  }
  const pages = (c.pages ?? []).map(page => ({ id: `page-${page.id}`, label: `${copy.open} · ${page.title}`, icon: <PanelsTopLeft size={15} />, disabled: !pluginAllowed(plugin, page.capabilities), onSelect: () => navigate('extensions', `${plugin.ID}/${page.id}`) }));
  const menu = [...pages, ...(administrator ? [
    { id: 'groups', label: zh ? '授权用户组' : 'Authorize groups', icon: <Shield size={15} />, onSelect: () => setGroups(true) },
    { id: 'manage', label: zh ? '权限与助理' : 'Permissions and assistants', icon: <Settings2 size={15} />, onSelect: () => setEditing(true) },
    ...(hasAdminSettings(plugin) ? [{ id: 'admin', label: zh ? '管理员配置' : 'Admin settings', icon: <Settings2 size={15} />, onSelect: () => navigate('extensions', `${plugin.ID}/@admin`) }] : []),
    { id: 'personal', label: plugin.UserEnabled ? (zh ? '为自己停用' : 'Disable for myself') : (zh ? '为自己启用' : 'Enable for myself'), disabled: !plugin.Enabled, onSelect: () => { void toggle(true); } },
    { id: 'remove', label: copy.remove, icon: <Trash2 size={15} />, danger: true, onSelect: () => setRemove(true) },
  ] : [])];
  return <article className="plugin-card plugin-row">
    <div className="plugin-card-heading"><span className="plugin-card-icon"><Puzzle size={24} /></span><div><h2>{plugin.Manifest.name}</h2><span className="muted">v{plugin.Manifest.version}</span></div>{menu.length > 0 && <DropdownMenu label={`${t.more} · ${plugin.Manifest.name}`} align="end" trigger={<IconButton label={`${t.more} · ${plugin.Manifest.name}`} disabled={action.busy}><Ellipsis size={18} /></IconButton>} items={menu} />}</div>
    <p className="plugin-card-description">{plugin.Manifest.description || (zh ? '此插件未提供简介。' : 'No description provided.')}</p>
    <div className="plugin-card-tags">{[[copy.themes,c.themes?.length],[copy.pages,c.pages?.length],[copy.tools,c.tools?.length],[copy.skills,c.skills?.length]].filter(([,n]) => !!n).map(([label,n]) => <Badge key={label} size="sm">{label} {n}</Badge>)}<Badge size="sm" tone={enabled ? 'positive' : 'neutral'}>{enabled ? copy.enabled : copy.disabled}</Badge></div>
    <div className="plugin-card-actions"><Switch label={administrator ? (zh ? '全局启用' : 'Enable globally') : copy.enabledLabel} checked={enabled} disabled={action.busy} onChange={() => { void toggle(); }} /><div>{hasSettings(plugin) && <Button size="sm" variant="secondary" disabled={!pluginAllowed(plugin)} onClick={() => navigate('extensions', `${plugin.ID}/@settings`)}>{zh ? '配置' : 'Configure'}</Button>}{c.pages?.[0] && <Button size="sm" variant="secondary" disabled={!pluginAllowed(plugin,c.pages[0].capabilities)} onClick={pages[0].onSelect}>{copy.open}</Button>}</div></div>
    <ErrorNotice error={action.error} />
    {editing && <PluginManagement plugin={plugin} onClose={() => setEditing(false)} />}
    {groups && <PluginGroups plugin={plugin} onClose={() => setGroups(false)} />}
    {remove && <Confirm danger title={copy.remove} description={copy.removeHelp} onClose={() => setRemove(false)} onConfirm={async () => { await api.post('/api/plugins/remove', { ID: plugin.ID, ExpectedVersion: plugin.Version }); host.reloadPlugins(); setRemove(false); }} />}
  </article>;
}
function PluginManagement({ plugin, onClose }: { plugin: PluginInstallation; onClose: () => void }) {
  const assistants = useQuery<Assistant[]>('/api/assistants', true), resources = useQuery<Resource[]>('/api/resources', true);
  if (!assistants.data || !resources.data) return <Dialog open title={plugin.Manifest.name} onOpenChange={open => { if (!open) onClose(); }}><ErrorNotice error={assistants.error || resources.error} />{!assistants.error && !resources.error && <Loading />}</Dialog>;
  return <PluginEditor plugin={plugin} assistants={assistants.data} resources={resources.data} onClose={onClose} />;
}
function PluginEditor({ plugin: initialPlugin, assistants, resources, onClose }: { plugin: PluginInstallation; assistants: Assistant[]; resources: Resource[]; onClose: () => void }) {
  const { t, locale } = useCopy(), copy = pluginCopy[locale], host = useUIHost(), action = useAction(), formID = useId();
  const [plugin] = useState(initialPlugin);
  const [grants, setGrants] = useState(plugin.Grants ?? []), [bindings, setBindings] = useState(plugin.Bindings ?? []), [release, setRelease] = useState(plugin.Manifest.version), [enabled, setEnabled] = useState(plugin.Enabled), [tab, setTab] = useState('general'), [invalidCompute, setInvalidCompute] = useState(false);
  const c = plugin.Manifest.contributes, requested = pluginCapabilities(plugin.Manifest);
  function update(id: string, change: (value: PluginBinding) => PluginBinding) { setBindings(previous => previous.map(b => b.AssistantID === id ? change(b) : b)); }
  const general = <div className="plugin-editor-section">
    <div className="plugin-editor-summary"><p>{plugin.Manifest.description}</p><span>{plugin.ID} · {plugin.Manifest.version}</span></div>
    <Switch label={copy.enabledLabel} checked={enabled} onChange={event => setEnabled(event.target.checked)} />
    <section className="settings-group"><h3 className="settings-group-title">{copy.grants}</h3>{requested.length ? <div className="plugin-permission-list">{requested.map(cap => <Checkbox key={cap} label={copy.capability[cap as keyof typeof copy.capability] ?? cap} checked={grants.includes(cap)} onChange={event => setGrants(old => event.target.checked ? [...old, cap] : old.filter(v => v !== cap))} />)}</div> : <p className="plugin-editor-hint">{copy.noPermissions}</p>}</section>
    {plugin.Versions.length > 1 && <Select size="sm" label={copy.release} value={release} options={plugin.Versions.map(v => ({ value: v.Version, label: v.Version }))} onValueChange={setRelease} />}
    {release !== plugin.Manifest.version && <p className="plugin-editor-hint">{copy.upgradeHelp}</p>}
  </div>;
  const assistantContent = <div className="plugin-editor-section"><p className="plugin-editor-hint">{copy.assistantHelp}</p><div className="settings-rows">
    {!assistants.some(a => a.State === 'active') && <p className="plugin-editor-hint">{copy.noAssistants}</p>}
    {assistants.filter(a => a.State === 'active').map(a => { const binding = bindings.find(b => b.AssistantID === a.ID); return <section className="settings-row-item plugin-binding" key={a.ID}>
      <Checkbox label={a.Name} checked={!!binding} onChange={event => setBindings(old => event.target.checked ? [...old, { AssistantID: a.ID, ComputeID: '', Tools: [], Skills: [] }] : old.filter(b => b.AssistantID !== a.ID))} />
      {binding && <div className="plugin-binding-fields">
        {c.tools?.some(t => t.execution.type === 'shell') && <Select size="sm" error={invalidCompute && pluginNeedsCompute(plugin.Manifest, binding.Tools) && !binding.ComputeID ? copy.choose : undefined} label={copy.compute} value={binding.ComputeID} options={[{ value: '', label: copy.choose }, ...resources.filter(r => r.Kind === 'compute').map(r => ({ value: r.ID, label: r.Name }))]} onValueChange={ComputeID => update(a.ID, b => ({ ...b, ComputeID }))} />}
        {c.tools?.map(tool => <Checkbox key={tool.id} label={`${copy.tools} · ${tool.id}`} checked={binding.Tools.includes(tool.id)} onChange={event => update(a.ID, b => ({ ...b, Tools: event.target.checked ? [...b.Tools, tool.id] : b.Tools.filter(id => id !== tool.id), Skills: event.target.checked ? b.Skills : b.Skills.filter(id => !c.skills.find(sk => sk.id === id)?.tools?.includes(tool.id)) }))} />)}
        {c.skills?.map(skill => <Checkbox key={skill.id} label={`${copy.skills} · ${skill.name}`} checked={binding.Skills.includes(skill.id)} disabled={(skill.tools ?? []).some(id => !binding.Tools.includes(id))} onChange={event => update(a.ID, b => ({ ...b, Skills: event.target.checked ? [...b.Skills, skill.id] : b.Skills.filter(id => id !== skill.id) }))} />)}
      </div>}
    </section>; })}
  </div></div>;
  return <Dialog open title={plugin.Manifest.name} onOpenChange={open => { if (!open && !action.busy) onClose(); }} dismissible={!action.busy} footer={<><Button variant="secondary" disabled={action.busy} onClick={onClose}>{t.cancel}</Button><Button form={formID} type="submit" loading={action.busy}>{copy.save}</Button></>}>
    <div className="plugin-editor"><form id={formID} onSubmit={event => { event.preventDefault(); if (bindings.some(b => pluginNeedsCompute(plugin.Manifest, b.Tools) && !b.ComputeID)) { setInvalidCompute(true); setTab('assistants'); return; } void action.run(async () => {
      await api.post('/api/plugins/configure', { ID: plugin.ID, ExpectedVersion: plugin.Version, Enabled: release === plugin.Manifest.version && enabled, Grants: release === plugin.Manifest.version ? grants : [], Bindings: release === plugin.Manifest.version ? bindings : [], Release: release }); host.reloadPlugins(); onClose();
    }); }} />
      {!!(c.tools?.length || c.skills?.length) ? <Tabs label={copy.configure} size="sm" value={tab} onValueChange={setTab} items={[{ value: 'general', label: copy.general, content: general }, { value: 'assistants', label: copy.assistants, content: assistantContent }, ]} /> : general}
      <ErrorNotice error={action.error} />
    </div>
  </Dialog>;
}
