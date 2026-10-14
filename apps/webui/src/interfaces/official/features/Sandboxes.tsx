import { useErrorToast } from '../../../error-toasts';
import { gpuName } from '../../../gpu';
import { fullAccessHelp } from '../../../full-access';
import { fullAccessLabel, sandboxPermissions } from './PermissionTargetFields';
import type { SandboxPermission } from './PermissionTargetFields';
import { useEffect, useState } from 'react';
import { ModalPresence, Alert, Badge, Button, DropdownMenu, IconButton, Select, SlidingHighlight, Tabs, TextField, useBreakpoint } from '@molr/ui';
import type { MenuItem } from '@molr/ui';
import { Cpu, Ellipsis, Globe, HardDrive, Pencil, Plus, RefreshCw, Server, ShieldCheck, Unplug, Users } from 'lucide-react';
import type { Assistant, Grant, Resource } from '../../../api/types';
import type { SandboxInfo, SandboxKind, SandboxResourceDetail, SandboxView } from '../../../sandbox-types';
export type { SandboxView } from '../../../sandbox-types';
import { api } from '../../../api/client';
import { useCopy, useQuery } from '../../../state';
import { useSandboxCopy } from '../../../sandbox-copy';
import { useSandboxPreference } from '../../../sandbox-navigation';
import { sandboxReason } from '../../../sandbox';
import { SettingsHelp } from '../components/SettingsHelp';
import { Confirm, EditDialog, Empty, Loading, useAction } from '../components/common';
import { SandboxConnection, connectionError } from './SandboxConnection';
import { SandboxAccess, activeResourceGrants } from './SandboxAccess';

const kinds = sandboxPermissions;
const icons = { storage: HardDrive, compute: Cpu, network: Globe, full_access: ShieldCheck };
const isConnected = (sandbox: SandboxView) => sandbox.State === 'available';
function statusLabel(sandbox: SandboxView, copy: ReturnType<typeof useSandboxCopy>) {
  if (sandbox.State === 'disconnected') return copy.disconnected;
  if (sandbox.Local && !sandbox.Info.Enabled) return copy.disabled;
  return isConnected(sandbox) ? sandbox.Local ? copy.ready : copy.connected : copy.offline;
}
export function Sandboxes({ resources, resourceID = '' }: { resources: Resource[]; resourceID?: string }) {
  const { t, locale } = useCopy(), copy = useSandboxCopy(), query = useQuery<SandboxView[]>('/api/sandboxes'), check = useAction();
  const compact = useBreakpoint('compact');
  const [selected, setSelected] = useSandboxPreference<string>('sandbox', 'local', value => /^[A-Za-z0-9_-]{1,128}$/.test(value));
  const [kind, setKind] = useSandboxPreference<SandboxPermission>('kind', 'storage', value => kinds.includes(value as SandboxPermission));
  const details = useQuery<SandboxResourceDetail[]>(query.data?.some(item => item.ID === selected) ? `/api/sandboxes/${selected}/resources` : null);
  const grants = useQuery<Grant[]>('/api/grants', true), assistants = useQuery<Assistant[]>('/api/assistants', true);
  const [connecting, setConnecting] = useState(false), [editing, setEditing] = useState<SandboxView | null>(null), [disconnecting, setDisconnecting] = useState<SandboxView | null>(null), [renamingSandbox, setRenamingSandbox] = useState<SandboxView | null>(null), [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<Resource | null>(null), [renaming, setRenaming] = useState<Resource | null>(null), [access, setAccess] = useState<Resource | null>(null);
  const target = resources.find(resource => resource.ID === resourceID);
  useEffect(() => { if (target && kinds.includes(target.Kind as SandboxKind)) { setSelected(target.SandboxID || 'local'); setKind(target.Kind as SandboxKind); } }, [target?.ID, target?.SandboxID, target?.Kind]);
  useEffect(() => { if (query.data?.length && !query.data.some(item => item.ID === selected)) setSelected(query.data[0].ID); }, [query.data, selected]);
  const sandbox = query.data?.find(item => item.ID === selected);
  const rows = resources.filter(resource => (resource.SandboxID || 'local') === selected && resource.Kind === kind);
  const connected = !!sandbox && isConnected(sandbox), enabled = connected && sandbox.Info.Enabled;
  const liveGrants = activeResourceGrants(grants.data ?? [], assistants.data ?? []);
  const grantDataReady = !!grants.data && !!assistants.data;
  const sources = sandbox ? sourceOptions(sandbox, kind === 'full_access' ? 'compute' : kind, copy) : [];
  const addLabel = kind === 'storage' ? copy.addDirectory : kind === 'network' ? copy.addNetwork : copy.enableCompute;
  const canAdd = kind !== 'full_access' && enabled && sources.length > 0 && (kind !== 'compute' || !rows.some(row => row.State !== 'disconnected'));
  const name = (item: SandboxView) => item.Local ? copy.local : item.Name;
  const refresh = () => void check.run(async () => { await query.reload(); await details.reload(); }, undefined, false);
  useErrorToast(query.error || details.error || grants.error || assistants.error || check.error, refresh);
  const menu: MenuItem[] = sandbox && !sandbox.Local ? [
    { id: 'rename', label: copy.rename, icon: <Pencil size={15} />, onSelect: () => setRenamingSandbox(sandbox) },
    { id: 'credentials', label: copy.credentials, icon: <ShieldCheck size={15} />, onSelect: () => setEditing(sandbox) },
    ...(sandbox.State !== 'disconnected' ? [{ id: 'disconnect', label: copy.disconnect, icon: <Unplug size={15} />, danger: true, separatorBefore: true, onSelect: () => setDisconnecting(sandbox) }] : []),
  ] : [];
  return <div className="sandbox-workspace">
    {query.loading && !query.data && <Loading />}
    <div className="sandbox-mobile-switch"><Select hideLabel label={copy.select} value={selected} onValueChange={setSelected} options={(query.data ?? []).map(item => ({ value: item.ID, label: name(item), description: statusLabel(item, copy) }))} /><IconButton label={copy.connect} onClick={() => setConnecting(true)}><Plus size={18} /></IconButton></div>
    <div className="sandbox-layout">
      <aside className="sandbox-sidebar"><div className="sandbox-sidebar-heading"><span>{copy.title}</span><SettingsHelp label={copy.helpLabel} content={copy.help} /></div>
        <SlidingHighlight as="ul" className="sandbox-list" value={selected} disabled={compact} aria-label={copy.select}>{query.data?.map(item => <li key={item.ID}><button className="sandbox-item" type="button" data-sliding-item={item.ID} aria-pressed={selected === item.ID} onClick={() => setSelected(item.ID)}>
          <Server size={17} /><span className="sandbox-item-copy"><strong>{name(item)}</strong><small title={item.Endpoint}>{item.Local ? copy.builtin : item.Endpoint}</small><span className={`sandbox-connection-state ${isConnected(item) ? 'is-connected' : ''}`}>{statusLabel(item, copy)}</span></span>
        </button></li>)}</SlidingHighlight>
        <Button className="sandbox-connect" variant="secondary" size="sm" onClick={() => setConnecting(true)}><Plus size={15} />{copy.connect}</Button>
      </aside>
      {sandbox && <div className="sandbox-detail">
        <div className="sandbox-overview"><header className="sandbox-heading"><div className="sandbox-heading-copy"><div className="sandbox-title"><h2>{name(sandbox)}</h2><Badge tone={connected ? 'positive' : 'neutral'}>{statusLabel(sandbox, copy)}</Badge></div><p title={sandbox.Endpoint}>{sandbox.Local ? copy.builtin : sandbox.Endpoint}</p></div>
          <div className="sandbox-heading-actions"><IconButton label={copy.testAgain} loading={check.busy || query.loading} onClick={refresh}><RefreshCw size={17} /></IconButton><SettingsHelp label={copy.helpLabel} content={sandbox.Local ? copy.localHelp : copy.help} />{menu.length > 0 && <DropdownMenu label={t.more} align="end" trigger={<IconButton label={t.more}><Ellipsis size={18} /></IconButton>} items={menu} />}</div>
        </header>
        <div className="sandbox-overview-meta"><div className="sandbox-capabilities">
          <span data-ready={enabled && sandbox.Info.Storage}><HardDrive size={14} />{enabled && sandbox.Info.Storage ? copy.storageReady : copy.storageUnavailable}</span>
          <span data-ready={enabled && sandbox.Info.Capabilities.Shell}><Cpu size={14} />{enabled && sandbox.Info.Capabilities.Shell ? copy.computeReady : copy.computeUnavailable}</span>
          {enabled && sandbox.Info.Capabilities.FullAccess && <span><ShieldCheck size={14} />{locale === 'zh' ? '可授权完全访问' : 'Full access available'}</span>}
        </div>{sandbox.CheckedAt && <p className="sandbox-checked">{copy.checkedAt} · {new Date(sandbox.CheckedAt).toLocaleTimeString(locale)} · {sandbox.Info.Capabilities.OS || 'Linux'}</p>}</div></div>
        {!connected || !sandbox.Info.Enabled ? <Alert tone="warning" title={sandbox.State === 'disconnected' ? copy.disconnected : sandbox.State === 'disabled' ? copy.disabled : !connected ? copy.offline : copy.disabled}>{sandbox.State === 'disconnected' ? copy.reconnectHelp : sandbox.State === 'disabled' ? copy.disabledHelp : !connected ? connectionError(copy, sandbox.ConnectionReason) : copy.disabledHelp}{sandbox.State === 'offline' && <p>{copy.snapshot}</p>}</Alert> : null}
        <Tabs label={copy.title} value={kind} onValueChange={setKind} items={kinds.map(value => { const Icon = icons[value]; return { value, label: value === 'full_access' ? fullAccessLabel(locale === 'zh') : copy[value], icon: <Icon size={15} />, content: value === kind && (kind === 'full_access' ? <FullAccessSection sandbox={sandbox} resources={resources} grants={liveGrants} ready={grantDataReady} onAccess={setAccess} /> : <div className="sandbox-section">
          {kind === 'compute' && <ComputeSummary sandbox={sandbox} />}
          {kind === 'network' && <div className="sandbox-workspace-summary sandbox-network-summary"><Globe size={18} /><div><h3>{copy.networkOverview}</h3><p>{copy.networkQuotaHelp}</p></div><SettingsHelp label={copy.networkOverview} content={`${copy.automaticNetwork} ${copy.networkTransportHelp}`} /></div>}
          {kind === 'storage' && <div className="sandbox-workspace-summary sandbox-storage-summary"><HardDrive size={18} /><div><h3>{copy.workspace} <code>/work</code></h3><p>{copy.workspaceDescription}</p></div><Badge dot={false}>{enabled && sandbox.Info.Storage ? copy.available : copy.unavailableShort}</Badge></div>}
          <div className="sandbox-section-heading"><h3>{kind === 'storage' ? copy.directories : kind === 'network' ? copy.networkPolicy : copy.compute}</h3><div className="sandbox-row-actions"><SettingsHelp label={copy.sourceLabel} content={copy.limitsHelp} />{canAdd && <Button size="sm" variant="secondary" onClick={() => setAdding(true)}><Plus size={14} />{addLabel}</Button>}</div></div>
          {!rows.length ? <div className="sandbox-empty"><Empty title={sandbox.State === 'disabled' ? copy.disabled : sandbox.State === 'disconnected' ? copy.disconnected : !connected ? copy.offline : !sandbox.Info.Enabled ? copy.disabled : !sources.length ? copy.deploymentNeeded : kind === 'storage' ? copy.noDirectories : kind === 'network' ? copy.noNetworks : copy.noCompute} description={sandbox.State === 'disabled' ? copy.disabledHelp : !connected ? sandbox.State === 'disconnected' ? copy.reconnectHelp : copy.offlineHelp : !sandbox.Info.Enabled ? copy.disabledHelp : !sources.length ? copy.deploymentHelp : kind === 'storage' ? copy.noDirectoriesHelp : kind === 'network' ? copy.noNetworksHelp : copy.noComputeHelp} /></div> : <div className="sandbox-resources">{rows.map(resource => {
            const detail = details.data?.find(item => item.ID === resource.ID), count = new Set(liveGrants.filter(grant => grant.ResourceID === resource.ID && !grant.Scope.FullAccess).map(grant => grant.AssistantID)).size;
            const resourceMenu: MenuItem[] = [{ id: 'rename', label: copy.rename, icon: <Pencil size={15} />, onSelect: () => setRenaming(resource) }, ...(resource.State !== 'disconnected' && resource.Provider !== 'managed-workspace' ? [{ id: 'disconnect', label: t.resources.disconnect, icon: <Unplug size={15} />, danger: true, separatorBefore: true, onSelect: () => setRemoving(resource) }] : [])];
            return <article className="sandbox-resource" key={resource.ID} data-resource-id={resource.ID}>
              <div className="sandbox-resource-body"><div className="sandbox-resource-heading"><h3>{resource.Name}</h3><Badge dot={false} tone={resource.State === 'disconnected' ? 'neutral' : kind === 'storage' ? 'neutral' : enabled ? 'positive' : 'neutral'}>{resource.State === 'disconnected' ? copy.disconnected : kind === 'storage' ? resource.Actions.includes('write') ? copy.readWrite : copy.readOnly : enabled ? copy.available : copy.unavailableShort}</Badge></div>
              <ResourceDetails sandbox={sandbox} resource={resource} detail={detail} loading={details.loading && !details.data} /></div>
              <div className="sandbox-resource-footer"><Button size="sm" variant="ghost" onClick={() => setAccess(resource)} disabled={!grantDataReady}><Users size={15} />{grantDataReady ? count ? `${copy.authorized} · ${count}` : copy.grant : t.loading}</Button><DropdownMenu label={t.more} align="end" trigger={<IconButton label={`${t.more} · ${resource.Name}`}><Ellipsis size={16} /></IconButton>} items={resourceMenu} />{!count && grantDataReady && <span className="muted">{copy.noAccess}</span>}</div>
            </article>;
          })}</div>}
        </div>) }; })} />
      </div>}
    </div>
    <ModalPresence>{connecting && <SandboxConnection onClose={() => setConnecting(false)} onConnected={setSelected} />}</ModalPresence><ModalPresence>{editing && <SandboxConnection sandbox={editing} onClose={() => setEditing(null)} onConnected={setSelected} />}</ModalPresence>
    <ModalPresence>{adding && sandbox && kind !== 'full_access' && <AddSandboxResource sandbox={sandbox} kind={kind} onClose={() => setAdding(false)} onConnected={resource => { setAdding(false); setAccess(resource); }} />}</ModalPresence>
    <ModalPresence>{renamingSandbox && <RenameSandbox sandbox={renamingSandbox} onClose={() => setRenamingSandbox(null)} />}</ModalPresence>
    <ModalPresence>{disconnecting && <Confirm danger title={copy.disconnect} description={copy.disconnectHelp} onClose={() => setDisconnecting(null)} onConfirm={() => api.mutate('/api/sandboxes/disconnect', { ID: disconnecting.ID }, disconnecting.Version)} />}</ModalPresence>
    <ModalPresence>{removing && <Confirm danger title={t.resources.disconnect} description={t.resources.disconnectHelp} onClose={() => setRemoving(null)} onConfirm={() => api.mutate('/api/resources/disconnect', { ID: removing.ID }, removing.Version)} />}</ModalPresence>
    <ModalPresence>{renaming && <RenameSandboxResource resource={renaming} onClose={() => setRenaming(null)} />}</ModalPresence>
    <ModalPresence>{access && <SandboxAccess resources={resources} fullAccess={kind === 'full_access'} resource={resources.find(item => item.ID === access.ID) ?? access} canGrant={enabled && (kind !== 'full_access' || !!sandbox?.Info.Capabilities.FullAccess) && (resources.find(item => item.ID === access.ID) ?? access).State === 'available'} onClose={() => setAccess(null)} />}</ModalPresence>
  </div>;
}
function FullAccessSection({ sandbox, resources, grants, ready, onAccess }: { sandbox: SandboxView; resources: Resource[]; grants: Grant[]; ready: boolean; onAccess: (resource: Resource) => void }) {
  const { t, locale } = useCopy(), copy = useSandboxCopy(), zh = locale === 'zh';
  const compute = resources.find(r => r.Kind === 'compute' && (r.SandboxID || 'local') === sandbox.ID && r.State === 'available');
  const available = sandbox.State === 'available' && sandbox.Info.Enabled && sandbox.Info.Capabilities.FullAccess;
  const count = new Set(grants.filter(g => g.ResourceID === compute?.ID && g.Scope.FullAccess).map(g => g.AssistantID)).size;
  return <div className="sandbox-section">
    <div className="sandbox-workspace-summary"><ShieldCheck size={18} /><div><h3>{fullAccessLabel(zh)}</h3><p>{fullAccessHelp(zh)}</p></div><Badge dot={false} tone={available ? 'positive' : 'neutral'}>{available ? copy.available : copy.unavailableShort}</Badge></div>
    {compute ? <article className="sandbox-resource" data-resource-id={compute.ID}>
      <div className="sandbox-resource-body"><div className="sandbox-resource-heading"><h3>{zh ? '完全访问权限' : 'Full access permission'}</h3></div>
      <p className="muted">{zh ? '独立授予，可设置有效期或随时撤销。存储、计算和网络权限仍可分别管理。' : 'Grant independently, set an expiry or revoke at any time. Storage, compute and network permissions remain separately managed.'}</p></div>
      <div className="sandbox-resource-footer"><Button size="sm" variant="ghost" disabled={!ready} onClick={() => onAccess(compute)}><Users size={15} />{ready ? count ? `${copy.authorized} · ${count}` : copy.grant : t.loading}</Button>{!count && ready && <span className="muted">{copy.noAccess}</span>}</div>
    </article> : <Empty title={copy.noCompute} description={copy.noComputeHelp} />}
    {!available && <p className="muted sandbox-resource-description">{zh ? '此沙盒尚未开放完全访问权限。' : 'Full access is not available on this sandbox.'}</p>}
  </div>;
}
function ComputeSummary({ sandbox }: { sandbox: SandboxView }) {
  const { t, locale } = useCopy(), copy = useSandboxCopy(), caps = sandbox.Info.Capabilities, ready = isConnected(sandbox) && sandbox.Info.Enabled && caps.Shell;
  const rows = [...(sandbox.Info.GPUs?.length ? [['GPU', sandbox.Info.GPUs.map(g=>`${gpuName(g.ID,sandbox.Info.GPUs!,locale)}${g.Available?'':locale==='zh'?'（不可用）':' (unavailable)'}`).join(' / ')]] : []), [copy.cpu, `${(sandbox.Info.AuthorizationCeilings??sandbox.Info.Limits).CPUPercent}%`], [copy.memory, `${Math.round((sandbox.Info.AuthorizationCeilings??sandbox.Info.Limits).MemoryBytes / 1048576)} MiB`], [copy.processes, String((sandbox.Info.AuthorizationCeilings??sandbox.Info.Limits).Processes)], [copy.timeout, locale === 'zh' ? '由助理的计算授权决定' : 'Defined by the assistant’s compute grant']];
  return <div className="sandbox-compute-summary"><div className="sandbox-section-heading"><h3>{locale === 'zh' ? '可申请的计算能力上限' : 'Requestable compute capacity'}</h3><span className="muted">{copy.deploymentLimits}</span></div><dl className="sandbox-limits">{rows.map(([label, value]) => <div key={label} className={label === 'GPU' || label === copy.timeout ? 'sandbox-limit-description' : undefined}><dt>{label}</dt><dd>{value}</dd></div>)}</dl><div className="sandbox-isolation"><ShieldCheck size={16} /><span>{ready ? copy.isolationReady : sandboxReason(t, caps.Reason)}</span><SettingsHelp label={copy.isolation} content={copy.isolationHelp} /></div></div>;
}
function ResourceDetails({ sandbox, resource, detail, loading }: { sandbox: SandboxView; resource: Resource; detail?: SandboxResourceDetail; loading: boolean }) {
  const { t } = useCopy(), copy = useSandboxCopy();
  if (loading) return <p className="muted">{t.loading}</p>;
  if (resource.Kind === 'compute') return <p className="muted sandbox-resource-description">{copy.computeResourceHelp}</p>;
  if (!detail) return <p className="muted">{copy.sourceMissing}</p>;
  if (resource.Provider === 'managed-workspace') return <p className="muted">{copy.workspaceDescription}</p>;
  if (resource.Kind === 'storage') {
    const source = sandbox.Info.Directories?.find(item => item.ID === detail.SourceID);
    return <div className="sandbox-directory-detail"><span className="muted">{copy.directorySource}</span><code className="sandbox-path">{detail.Path || source?.Name || detail.SourceID}</code>{!detail.Path && source && <small className="muted sandbox-path">{detail.SourceID}</small>}{!detail.Path && !source && <p className="muted">{copy.sourceMissing}</p>}</div>;
  }
  const network = sandbox.Info.Networks?.find(item => item.ID === detail.SourceID);
  if (!network) return <p className="muted">{copy.sourceMissing}</p>;
  return <NetworkDetails network={network} />;
}
function NetworkDetails({ network }: { network: SandboxInfo['Networks'][number] }) {
  const { t } = useCopy(), copy = useSandboxCopy();
  return <div className="sandbox-network-detail">
    <dl className="sandbox-network-facts">
      <div><dt>{copy.networkPolicy}</dt><dd>{networkName(network.ID, network.Name, copy)}</dd></div>
      <div><dt>{copy.networkProtocols}</dt><dd>{network.Target.Protocols.join(' / ').toUpperCase()}</dd></div>
    </dl>
    {networkDescription(network.ID, copy) && <p className="sandbox-resource-description">{networkDescription(network.ID, copy)}</p>}
    <details className="sandbox-network-advanced"><summary>{copy.advancedRules}</summary>
      <dl className="sandbox-network-rules"><dt>{copy.allowedAddresses}</dt><dd>{network.Target.Addresses.join(', ')}</dd><dt>{copy.allowedPorts}</dt><dd>{network.Target.Ports.join(', ')}</dd>
        <dt>{t.resources.excludedAddresses}</dt><dd>{network.Target.ExcludeAddresses?.join(', ') || t.none}</dd>
        <dt>{t.resources.excludedPorts}</dt><dd>{network.Target.ExcludePorts?.join(', ') || t.none}</dd>
      </dl>
      {network.ID === 'preset:host' && <p className="sandbox-resource-description">{copy.hostNameHelp}</p>}
      <p className="sandbox-resource-description">{copy.networkTransportHelp} {copy.networkHelp}</p>
    </details>
  </div>;
}

function networkName(id: string, name: string, copy: ReturnType<typeof useSandboxCopy>) {
  return id.startsWith('preset:') ? copy.presetNames[id.slice(7) as keyof typeof copy.presetNames] ?? name : name;
}
function networkDescription(id: string, copy: ReturnType<typeof useSandboxCopy>) {
  return id.startsWith('preset:') ? copy.presetDescriptions[id.slice(7) as keyof typeof copy.presetDescriptions] ?? '' : '';
}
function sourceOptions(sandbox: SandboxView, kind: SandboxKind, copy: ReturnType<typeof useSandboxCopy>) {
  if (kind === 'compute') return [{ value: 'compute', label: copy.compute }];
  if (kind === 'storage') return (sandbox.Info.Directories ?? []).map(item => ({ value: item.ID, label: `${item.Name} · ${item.ReadOnly ? copy.readOnly : copy.readWrite}` }));
  return (sandbox.Info.Networks ?? []).map(item => ({ value: item.ID, label: networkName(item.ID, item.Name, copy), description: networkDescription(item.ID, copy) || `${item.Target.Protocols.join(' / ').toUpperCase()} · ${item.Target.Addresses.join(', ')} · ${item.Target.Ports.join(', ')}` }));
}
function AddSandboxResource({ sandbox, kind, onClose, onConnected }: { sandbox: SandboxView; kind: SandboxKind; onClose: () => void; onConnected: (resource: Resource) => void }) {
  const { t } = useCopy(), copy = useSandboxCopy(), options = sourceOptions(sandbox, kind, copy);
  const [source, setSource] = useState(options[0]?.value ?? ''), [name, setName] = useState(kind === 'compute' ? `${sandbox.Local ? copy.local : sandbox.Name} · ${copy.compute}` : kind === 'network' ? options[0]?.label ?? '' : '');
  const network = sandbox.Info.Networks?.find(item => item.ID === source);
  const title = kind === 'storage' ? copy.addDirectory : kind === 'network' ? copy.addNetwork : copy.enableCompute;
  return <EditDialog title={title} description={copy.notGranted} onClose={onClose} submitDisabled={!source} onSubmit={async () => onConnected(await api.mutate<Resource>('/api/sandboxes/resource', { SandboxID: sandbox.ID, Kind: kind, SourceID: source, Name: name.trim() }))}>
    <TextField label={t.name} value={name} onChange={event => setName(event.target.value)} required maxLength={256} />
    {kind !== 'compute' && <Select label={kind === 'network' ? t.sandbox.target : copy.source} value={source} onValueChange={value => { setSource(value); if (kind === 'network' && (!name.trim() || name === options.find(item => item.value === source)?.label)) setName(options.find(item => item.value === value)?.label ?? ''); }} options={options} />}
    {kind === 'network' && network && <><p className="muted sandbox-resource-description">{copy.automaticNetwork}</p><NetworkDetails network={network} /></>}
  </EditDialog>;
}
function RenameSandbox({ sandbox, onClose }: { sandbox: SandboxView; onClose: () => void }) {
  const { t } = useCopy(), copy = useSandboxCopy(), [name, setName] = useState(sandbox.Name);
  return <EditDialog title={copy.rename} onClose={onClose} onSubmit={() => api.mutate('/api/sandboxes/rename', { ID: sandbox.ID, Name: name.trim() }, sandbox.Version)}><TextField label={t.name} value={name} onChange={event => setName(event.target.value)} required maxLength={256} /></EditDialog>;
}
function RenameSandboxResource({ resource, onClose }: { resource: Resource; onClose: () => void }) {
  const { t } = useCopy(), copy = useSandboxCopy(), [name, setName] = useState(resource.Name);
  return <EditDialog title={copy.rename} onClose={onClose} onSubmit={() => api.mutate('/api/resources/update', { ID: resource.ID, Name: name.trim() }, resource.Version)}><TextField label={t.name} value={name} onChange={event => setName(event.target.value)} required maxLength={256} /></EditDialog>;
}
