import { sandboxComputeState, usesNetworkToggle } from '../../../sandbox-types';
import { useErrorToast } from '../../../error-toasts';
import { gpuName } from '../../../gpu';
import { fullAccessHelp } from '../../../full-access';
import { fullAccessLabel, sandboxPermissions } from './PermissionTargetFields';
import type { SandboxPermission } from './PermissionTargetFields';
import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { ModalPresence, Alert, Badge, Button, Progress, DropdownMenu, IconButton, Select, SlidingHighlight, Tabs, TextField, useBreakpoint } from '@molr/ui';
import type { MenuItem } from '@molr/ui';
import { Cpu, Ellipsis, Globe, HardDrive, Pencil, Plus, RefreshCw, Server, ShieldCheck, Unplug, Users } from 'lucide-react';
import type { Assistant, Grant, Resource } from '@molr/sdk';
import type { SandboxInfo, SandboxKind, SandboxResourceDetail, SandboxView } from '../../../sandbox-types';
export type { SandboxView } from '../../../sandbox-types';
import { api, onMutation } from '@molr/sdk';
import { useCopy, useQuery } from '../../../state';
import { sandboxResourceName, useSandboxCopy } from '../../../sandbox-copy';
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
  useEffect(() => onMutation(path => {
    if (path.startsWith('/api/sandboxes/') || path.startsWith('/api/resources/')) { query.reload(); details.reload(); }
  }), [query.reload, details.reload]);
  const [connecting, setConnecting] = useState(false), [editing, setEditing] = useState<SandboxView | null>(null), [disconnecting, setDisconnecting] = useState<SandboxView | null>(null), [renamingSandbox, setRenamingSandbox] = useState<SandboxView | null>(null), [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<Resource | null>(null), [renaming, setRenaming] = useState<Resource | null>(null), [access, setAccess] = useState<Resource | null>(null);
  const target = resources.find(resource => resource.ID === resourceID);
  useEffect(() => { if (target && kinds.includes(target.Kind as SandboxKind)) { setSelected(target.SandboxID || 'local'); setKind(target.Kind as SandboxKind); } }, [target?.ID, target?.SandboxID, target?.Kind]);
  useEffect(() => { if (query.data?.length && !query.data.some(item => item.ID === selected)) setSelected(query.data[0].ID); }, [query.data, selected]);
  const sandbox = query.data?.find(item => item.ID === selected);
  const rows = resources.filter(resource => (resource.SandboxID || 'local') === selected && resource.Kind === kind);
  const connected = !!sandbox && isConnected(sandbox), enabled = connected && sandbox.Info.Enabled;
  const computeState = sandbox ? sandboxComputeState(sandbox.Info, sandbox.State) : 'offline';
  const computeReady = computeState === 'ready';
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
  if (query.loading && !query.data) return <div className="sandbox-workspace"><Loading /></div>;
  return <div className="sandbox-workspace">
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
          <span data-ready={computeReady}><Cpu size={14} />{copy.computeStates[computeState]}</span>
          {enabled && sandbox.Info.Capabilities.FullAccess && <span><ShieldCheck size={14} />{copy.fullAccessAvailable}</span>}
        </div>{sandbox.CheckedAt && <p className="sandbox-checked">{copy.checkedAt} · {new Date(sandbox.CheckedAt).toLocaleTimeString(locale)} · {sandbox.Info.Capabilities.OS || 'Linux'}</p>}</div></div>
        {!connected || !sandbox.Info.Enabled ? <Alert tone="warning" title={sandbox.State === 'disconnected' ? copy.disconnected : sandbox.State === 'disabled' ? copy.disabled : !connected ? copy.offline : copy.disabled}>{sandbox.State === 'disconnected' ? copy.reconnectHelp : sandbox.State === 'disabled' ? copy.disabledHelp : !connected ? connectionError(copy, sandbox.ConnectionReason) : copy.disabledHelp}{sandbox.State === 'offline' && <p>{copy.snapshot}</p>}</Alert> : null}
        <Tabs label={copy.title} value={kind} onValueChange={setKind} items={kinds.map(value => { const Icon = icons[value]; return { value, label: value === 'full_access' ? fullAccessLabel(locale === 'zh') : copy[value], icon: <Icon size={15} />, content: value === kind && (kind === 'full_access' ? <FullAccessSection sandbox={sandbox} resources={resources} grants={liveGrants} ready={grantDataReady} onAccess={setAccess} /> : <div className="sandbox-section">
          {kind === 'compute' && <ComputeSummary sandbox={sandbox} />}
          {kind === 'storage' && <StorageSummary sandbox={sandbox} resources={rows} details={details.data} loading={details.loading && !details.data} />}
          {kind === 'network' && <div className="sandbox-workspace-summary sandbox-network-summary"><Globe size={18} /><div><h3>{copy.networkOverview}</h3><p>{usesNetworkToggle(sandbox?.Info.Capabilities.NetworkBackend) ? (sandbox?.Info.Capabilities.NetworkBackend === 'appcontainer-toggle' ? copy.windowsNetworkHelp : copy.nativeNetworkHelp) : copy.networkQuotaHelp}</p></div><SettingsHelp label={copy.networkOverview} content={usesNetworkToggle(sandbox?.Info.Capabilities.NetworkBackend) ? (sandbox?.Info.Capabilities.NetworkBackend === 'appcontainer-toggle' ? copy.windowsNetworkHelp : copy.nativeNetworkHelp) : `${copy.automaticNetwork} ${copy.networkTransportHelp}`} /></div>}
          {kind === 'storage' && sandbox.Local && <div className="sandbox-workspace-summary sandbox-storage-summary"><HardDrive size={18} /><div><h3>{copy.workspace} <code>/work</code></h3><p>{copy.workspaceDescription}</p></div><Badge dot={false}>{enabled && sandbox.Info.Storage ? copy.available : copy.unavailableShort}</Badge></div>}
          <div className="sandbox-section-heading"><h3>{kind === 'storage' ? copy.directories : kind === 'network' ? copy.networkPolicy : copy.compute}</h3><div className="sandbox-row-actions"><SettingsHelp label={copy.sourceLabel} content={copy.limitsHelp} />{(canAdd || kind === 'storage') && <Button size="sm" variant="secondary" disabled={!canAdd} onClick={() => setAdding(true)}><Plus size={14} />{addLabel}</Button>}</div></div>
          {!rows.length ? kind === 'compute' && !computeReady ? null : <div className="sandbox-empty"><Empty title={sandbox.State === 'disabled' ? copy.disabled : sandbox.State === 'disconnected' ? copy.disconnected : !connected ? copy.offline : !sandbox.Info.Enabled ? copy.disabled : !sources.length ? kind === 'storage' ? copy.directoriesNeeded : copy.networksNeeded : kind === 'storage' ? copy.noDirectories : kind === 'network' ? copy.noNetworks : copy.noCompute} description={sandbox.State === 'disabled' ? copy.disabledHelp : !connected ? sandbox.State === 'disconnected' ? copy.reconnectHelp : copy.offlineHelp : !sandbox.Info.Enabled ? copy.disabledHelp : !sources.length ? kind === 'storage' ? copy.directoriesHelp : copy.networksHelp : kind === 'storage' ? copy.noDirectoriesHelp : kind === 'network' ? copy.noNetworksHelp : copy.noComputeHelp} /></div> : <div className="sandbox-resources">{rows.map(resource => {
            const detail = details.data?.find(item => item.ID === resource.ID), count = new Set(liveGrants.filter(grant => grant.ResourceID === resource.ID && !grant.Scope.FullAccess).map(grant => grant.AssistantID)).size;
            const usable = enabled && resource.State === 'available' && (kind !== 'compute' || computeReady);
            const resourceMenu: MenuItem[] = [{ id: 'rename', label: copy.rename, icon: <Pencil size={15} />, onSelect: () => setRenaming(resource) }, ...(resource.State !== 'disconnected' && resource.Provider !== 'managed-workspace' ? [{ id: 'disconnect', label: t.resources.disconnect, icon: <Unplug size={15} />, danger: true, separatorBefore: true, onSelect: () => setRemoving(resource) }] : [])];
            return <article className="sandbox-resource" key={resource.ID} data-resource-id={resource.ID}>
              <div className="sandbox-resource-body"><div className="sandbox-resource-heading"><h3>{sandboxResourceName(resource, copy)}</h3><Badge dot={false} tone={resource.State === 'disconnected' ? 'neutral' : kind === 'storage' ? 'neutral' : usable ? 'positive' : 'neutral'}>{resource.State === 'disconnected' ? copy.disconnected : kind === 'storage' ? resource.Actions.includes('write') ? copy.readWrite : copy.readOnly : usable ? copy.available : copy.unavailableShort}</Badge></div>
              <ResourceDetails sandbox={sandbox} resource={resource} detail={detail} loading={details.loading && !details.data} /></div>
              <div className="sandbox-resource-footer"><Button size="sm" variant="ghost" onClick={() => setAccess(resource)} disabled={!grantDataReady}><Users size={15} />{grantDataReady ? count ? `${copy.authorized} · ${count}` : copy.grant : t.loading}</Button><DropdownMenu label={t.more} align="end" trigger={<IconButton label={`${t.more} · ${sandboxResourceName(resource, copy)}`}><Ellipsis size={16} /></IconButton>} items={resourceMenu} />{!count && grantDataReady && <span className="muted">{copy.noAccess}</span>}</div>
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
    <ModalPresence>{access && <SandboxAccess resources={resources} fullAccess={kind === 'full_access'} resource={resources.find(item => item.ID === access.ID) ?? access} canGrant={enabled && (kind === 'full_access' ? !!sandbox?.Info.Capabilities.FullAccess : kind !== 'compute' || computeReady) && (resources.find(item => item.ID === access.ID) ?? access).State === 'available'} onClose={() => setAccess(null)} />}</ModalPresence>
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
function capacityLimit(machine: number | undefined, ceiling: number | undefined): number | undefined {
  const values = [machine, ceiling].filter((value): value is number => value !== undefined && value > 0);
  return values.length ? Math.min(...values) : undefined;
}
function capacityBytes(value: number | undefined, locale: string) {
  if (value === undefined || value < 0) return locale === 'zh' ? '未知' : 'Unknown';
  if (value === 0) return '0 B';
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
  const unit = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(value / 1024 ** unit)} ${units[unit]}`;
}
function UsageSnapshot({ label, value, detail }: { label: string; value: number | null | undefined; detail?: string }) {
  const { locale } = useCopy();
  if (value == null || !Number.isFinite(value) || value < 0 || value > 100) return <small className="sandbox-usage-unavailable muted">{locale === 'zh' ? '占用信息暂不可用' : 'Usage unavailable'}</small>;
  return <div title={detail}><Progress className="sandbox-usage" label={label} value={value} valueText={new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 1 }).format(value / 100)} /></div>;
}
function StorageSummary({ sandbox, resources, details, loading }: { sandbox: SandboxView; resources: Resource[]; details?: SandboxResourceDetail[]; loading: boolean }) {
  const { t, locale } = useCopy(), zh = locale === 'zh', copy = useSandboxCopy();
  const directories = resources.filter(resource => resource.State !== 'disconnected');
  return <div className="sandbox-compute-summary"><div className="sandbox-section-heading"><h3>{zh ? '可使用的存储空间上限' : 'Available storage capacity'}</h3><span className="muted">{zh ? '由存储配额或磁盘容量决定' : 'Bounded by storage quotas or disk capacity'}</span></div>
    {loading ? <p className="muted">{t.loading}</p> : <dl className="sandbox-limits">{directories.length ? directories.map(resource => {
      const capacity = details?.find(detail => detail.ID === resource.ID)?.Capacity;
      const limit = capacityLimit(capacity?.StorageBytes, resource.Provider === 'managed-workspace' ? sandbox.Info.StorageCeilingBytes : undefined);
      return <div key={resource.ID}><dt>{sandboxResourceName(resource, copy)}</dt><dd>{capacityBytes(limit, locale)}</dd><UsageSnapshot label={zh ? '磁盘占用' : 'Disk usage'} value={capacity && capacity.StorageTotalBytes > 0 ? capacity.StorageUsedBytes / capacity.StorageTotalBytes * 100 : undefined} detail={capacity ? `${zh ? '磁盘剩余' : 'Disk free'} · ${capacity.StorageAvailableBytes === 0 ? '0 B' : capacityBytes(capacity.StorageAvailableBytes, locale)}` : undefined} /></div>;
    }) : <div><dt>{zh ? '连接目录后显示其容量' : 'Connect a directory to see its capacity'}</dt><dd>—</dd></div>}</dl>}
  </div>;
}
function ComputeSummary({ sandbox }: { sandbox: SandboxView }) {
  const { t, locale } = useCopy(), copy = useSandboxCopy(), caps = sandbox.Info.Capabilities, state = sandboxComputeState(sandbox.Info, sandbox.State), ready = state === 'ready';
  const host = sandbox.Info.HostCapacity, ceilings = sandbox.Info.AuthorizationCeilings;
  const cpu = capacityLimit(host?.CPUPercent, ceilings?.CPUPercent), memory = capacityLimit(host?.MemoryBytes, ceilings?.MemoryBytes), processes = capacityLimit(host?.Processes, ceilings?.Processes);
  const unknown = locale === 'zh' ? '未知' : 'Unknown';
  const gpus = [...(sandbox.Info.GPUs ?? []), ...(host?.GPUs ?? [])];
  const rows: [string, ReactNode][] = [
    [copy.cpu, cpu ? `${new Intl.NumberFormat(locale, { maximumFractionDigits: 2 }).format(cpu / 100)} ${locale === 'zh' ? '核' : 'cores'} · ${cpu}%` : unknown],
    [copy.memory, capacityBytes(memory, locale)], [copy.processes, processes ? new Intl.NumberFormat(locale).format(processes) : unknown],
    [copy.timeout, locale === 'zh' ? '由助理的计算授权决定' : 'Defined by the assistant’s compute grant'],
    ...(gpus.length ? [['GPU', <div className="sandbox-gpu-list">{gpus.map(g => <div className="sandbox-gpu" key={g.ID}><span>{gpuName(g.ID, gpus, locale)}{host?.GPUs?.includes(g) ? (locale === 'zh' ? '（随计算权限共享使用）' : ' (shared with compute permission)') : g.Available ? '' : locale === 'zh' ? '（不可用）' : ' (unavailable)'}</span><div className="sandbox-gpu-usage"><UsageSnapshot label={locale === 'zh' ? 'GPU 占用' : 'GPU usage'} value={g.UtilizationPercent} /><UsageSnapshot label={locale === 'zh' ? '显存占用' : 'GPU memory usage'} value={g.MemoryUsedBytes != null && g.MemoryBytes > 0 ? g.MemoryUsedBytes / g.MemoryBytes * 100 : undefined} detail={g.MemoryUsedBytes != null ? `${capacityBytes(g.MemoryUsedBytes, locale)} / ${capacityBytes(g.MemoryBytes, locale)}` : undefined} /></div></div>)}</div>] as [string, ReactNode]] : []),
  ];
  if (!ready) return <div className="sandbox-compute-summary">
    <h3>{copy.computeStates[state]}</h3>
    <p className="muted">{state === 'disabled' ? copy.computeDisabledHelp : state === 'offline' ? copy.offlineHelp : state === 'disconnected' ? copy.reconnectHelp : copy.computeUnavailableHelp}</p>
    {state === 'unavailable' && <><p className="sandbox-isolation"><ShieldCheck size={16} /><span>{caps.Reason === 'available' ? copy.computeStates.unavailable : sandboxReason(t, caps.Reason)}</span></p>{caps.FullAccess && <p className="muted">{copy.computeFullAccessHelp}</p>}</>}
  </div>;
  return <div className="sandbox-compute-summary"><div className="sandbox-section-heading"><h3>{locale === 'zh' ? '可申请的计算能力上限' : 'Requestable compute capacity'}</h3><span className="muted">{locale === 'zh' ? '由沙盒限制或机器容量决定' : 'Bounded by sandbox limits or machine capacity'}</span></div><dl className="sandbox-limits">{rows.map(([label, value]) => <div key={label} className={label === 'GPU' || label === copy.timeout ? 'sandbox-limit-description' : undefined}><dt>{label}</dt><dd>{value}</dd>{label === copy.cpu && <UsageSnapshot label={locale === 'zh' ? '机器占用' : 'Machine usage'} value={host?.CPUUsagePercent} />}{label === copy.memory && <UsageSnapshot label={locale === 'zh' ? '机器占用' : 'Machine usage'} value={host?.MemoryUsedBytes != null && host.MemoryBytes > 0 ? host.MemoryUsedBytes / host.MemoryBytes * 100 : undefined} detail={host?.MemoryUsedBytes != null ? `${capacityBytes(host.MemoryUsedBytes, locale)} / ${capacityBytes(host.MemoryBytes, locale)}` : undefined} />}</div>)}</dl><div className="sandbox-isolation"><ShieldCheck size={16} /><span>{ready ? usesNetworkToggle(caps.NetworkBackend) ? (caps.NetworkBackend === 'appcontainer-toggle' ? copy.windowsIsolationReady : copy.nativeIsolationReady) : copy.isolationReady : sandboxReason(t, caps.Reason)}</span><SettingsHelp label={copy.isolation} content={usesNetworkToggle(caps.NetworkBackend) ? (caps.NetworkBackend === 'appcontainer-toggle' ? copy.windowsIsolationHelp : copy.nativeIsolationHelp) : copy.isolationHelp} /></div></div>;
}
function ResourceDetails({ sandbox, resource, detail, loading }: { sandbox: SandboxView; resource: Resource; detail?: SandboxResourceDetail; loading: boolean }) {
  const { t, locale } = useCopy(), copy = useSandboxCopy();
  if (loading) return <p className="muted">{t.loading}</p>;
  if (resource.Kind === 'compute') return <p className="muted sandbox-resource-description">{copy.computeResourceHelp}{sandbox.Info.Capabilities.GPUAccessMode === 'shared' && <><br/>{locale === 'zh' ? '包含系统允许访问的全部 GPU，共享使用。' : 'Includes shared use of all GPUs accessible under system permissions.'}</>}</p>;
  if (!detail) return <p className="muted">{copy.sourceMissing}</p>;
  if (resource.Provider === 'managed-workspace') return <p className="muted">{copy.workspaceDescription}</p>;
  if (resource.Kind === 'storage') {
    const source = sandbox.Info.Directories?.find(item => item.ID === detail.SourceID);
    return <div className="sandbox-directory-detail"><span className="muted">{copy.directorySource}</span><code className="sandbox-path">{detail.Path || source?.Name || detail.SourceID}</code>{!detail.Path && source && <small className="muted sandbox-path">{detail.SourceID}</small>}{!detail.Path && !source && <p className="muted">{copy.sourceMissing}</p>}</div>;
  }
  const network = sandbox.Info.Networks?.find(item => item.ID === detail.SourceID);
  if (!network) return <p className="muted">{copy.sourceMissing}</p>;
  return <NetworkDetails network={network} backend={sandbox.Info.Capabilities.NetworkBackend} />;
}
function NetworkDetails({ network, backend }: { backend?: string; network: SandboxInfo['Networks'][number] }) {
  const { t } = useCopy(), copy = useSandboxCopy();
  if(network.ID==='preset:network')return <div className="sandbox-network-detail"><p className="sandbox-resource-description">{backend === 'appcontainer-toggle' ? copy.windowsNetworkHelp : copy.nativeNetworkHelp}</p></div>;
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
  if (kind === 'compute') return sandboxComputeState(sandbox.Info, sandbox.State) !== 'ready' ? [] : [{ value: 'compute', label: copy.compute }];
  if (kind === 'storage') return [...(sandbox.Info.Directories ?? []).map(item => ({ value: item.ID, label: `${item.Name} · ${item.ReadOnly ? copy.readOnly : copy.readWrite}` })), ...(sandbox.Info.AllowDirectoryPaths ? [{value: 'custom', label: copy.customDirectory}] : [])];
  return (sandbox.Info.Networks ?? []).map(item => ({ value: item.ID, label: networkName(item.ID, item.Name, copy), description: networkDescription(item.ID, copy) || `${item.Target.Protocols.join(' / ').toUpperCase()} · ${item.Target.Addresses.join(', ')} · ${item.Target.Ports.join(', ')}` }));
}
function AddSandboxResource({ sandbox, kind, onClose, onConnected }: { sandbox: SandboxView; kind: SandboxKind; onClose: () => void; onConnected: (resource: Resource) => void }) {
  const { t } = useCopy(), copy = useSandboxCopy(), options = sourceOptions(sandbox, kind, copy);
  const [source, setSource] = useState(options[0]?.value ?? ''), [name, setName] = useState(kind === 'compute' ? `${sandbox.Local ? copy.local : sandbox.Name} · ${copy.compute}` : kind === 'network' ? options[0]?.label ?? '' : '');
  const [directoryPath, setDirectoryPath] = useState('');
  const network = sandbox.Info.Networks?.find(item => item.ID === source);
  const title = kind === 'storage' ? copy.addDirectory : kind === 'network' ? copy.addNetwork : copy.enableCompute;
  return <EditDialog title={title} description={copy.notGranted} onClose={onClose} submitDisabled={!source || (kind === 'storage' && source === 'custom' && !directoryPath.trim())} onSubmit={async () => onConnected(await api.mutate<Resource>('/api/sandboxes/resource', { SandboxID: sandbox.ID, Kind: kind, SourceID: kind === 'storage' && source === 'custom' ? '' : source, ...(kind === 'storage' ? {Path: directoryPath.trim()} : {}), Name: name.trim() }))}>
    <TextField label={t.name} value={name} onChange={event => setName(event.target.value)} required maxLength={256} />
    {kind !== 'compute' && <Select label={kind === 'network' ? t.sandbox.target : copy.source} value={source} onValueChange={value => { setSource(value); if (kind === 'storage') setDirectoryPath(''); if (kind === 'network' && (!name.trim() || name === options.find(item => item.value === source)?.label)) setName(options.find(item => item.value === value)?.label ?? ''); }} options={options} />}
    {kind === 'storage' && <TextField label={source === 'custom' ? copy.directoryPath : copy.subdirectory} description={source === 'custom' ? copy.directoryPathHelp : copy.subdirectoryHelp} value={directoryPath} onChange={event => setDirectoryPath(event.target.value)} required={source === 'custom'} maxLength={4096} />}
    {kind === 'network' && network && <><p className="muted sandbox-resource-description">{copy.automaticNetwork}</p><NetworkDetails network={network} backend={sandbox.Info.Capabilities.NetworkBackend} /></>}
  </EditDialog>;
}
function RenameSandbox({ sandbox, onClose }: { sandbox: SandboxView; onClose: () => void }) {
  const { t } = useCopy(), copy = useSandboxCopy(), [name, setName] = useState(sandbox.Name);
  return <EditDialog title={copy.rename} onClose={onClose} onSubmit={() => api.mutate('/api/sandboxes/rename', { ID: sandbox.ID, Name: name.trim() }, sandbox.Version)}><TextField label={t.name} value={name} onChange={event => setName(event.target.value)} required maxLength={256} /></EditDialog>;
}
function RenameSandboxResource({ resource, onClose }: { resource: Resource; onClose: () => void }) {
  const { t } = useCopy(), copy = useSandboxCopy(), [name, setName] = useState(sandboxResourceName(resource, copy));
  return <EditDialog title={copy.rename} onClose={onClose} onSubmit={() => api.mutate('/api/resources/update', { ID: resource.ID, Name: name.trim() === sandboxResourceName(resource, copy) ? resource.Name : name.trim() }, resource.Version)}><TextField label={t.name} value={name} onChange={event => setName(event.target.value)} required maxLength={256} /></EditDialog>;
}
