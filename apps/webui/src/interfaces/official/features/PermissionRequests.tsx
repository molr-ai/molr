import { useErrorToast } from '../../../error-toasts';
import { PermissionTargetFields, fullAccessLabel, isSandboxResource, targetFor } from './PermissionTargetFields';
import type { PermissionTarget } from './PermissionTargetFields';
import type { SandboxView } from '../../../sandbox-types';
import { initialActions, initialConstraints } from './GrantEditor';
import { ConstraintsEditor, ConstraintsSummary, TemporaryConstraintsEditor, suggestedConstraints } from './ResourceConstraints';
import type { PermissionRequestInput } from '../../../api/types';
import { Folder, Globe, Server, Sparkles, Wrench } from 'lucide-react';
import { useEffect, useId, useState } from 'react';
import { Badge, Button, Card, ChatCard, DateTimePicker, Dialog, Select, Textarea } from '@molr/ui';
import { PermissionExpiry, PermissionScope } from './PermissionScope';
import type { Assistant, Grant, Model, PermissionRequest, Resource } from '../../../api/types';
import { api, APIError } from '../../../api/client';
import { useCopy, useQuery } from '../../../state';
import { grantSummary, permissionLabel, permissionOptions } from '../../../resource-permissions';
import { Confirm, lines, useAction } from '../components/common';

export function PermissionRequestCard({ requestID, assistant, title, summary, timeZone }: { timeZone?: string; requestID: string; assistant: Assistant; title: string; summary: string }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  const query = useQuery<PermissionRequest>(`/api/permission-requests/${requestID}`);
  const [review, setReview] = useState(false), [reject, setReject] = useState(false);
  const request = query.data;
  const state = request?.State;
  const input = request?.Input;
  const grant = request?.Granted;
  const resource = useQuery<Resource>(grant ? `/api/resources/${grant.ResourceID}` : null);
  const kind = grant ? resource.data?.Kind ?? input?.resource_kind : input?.resource_kind;
  const actions = grant?.Actions ?? input?.actions ?? [];
  const Icon = kind ? { storage: Folder, compute: Server, connection: Globe, network: Globe, model: Sparkles, tool: Wrench }[kind] : Wrench;
  const fullAccess = grant ? !!grant.Scope.FullAccess : !!input?.full_access;
  const headline = fullAccess ? (zh ? '完全访问沙盒' : 'Full sandbox access') : !kind ? (title || (zh ? '申请使用资源' : 'Request resource access')) : kind === 'storage'
    ? actions.includes('read') && actions.includes('write') ? (zh ? '读取和修改文件' : 'Read and modify files') : actions.includes('write') ? (zh ? '修改文件' : 'Modify files') : (zh ? '读取文件' : 'Read files')
    : ({ connection: zh ? '使用服务连接' : 'Use a connection', compute: zh ? '运行命令' : 'Run commands', network: zh ? '访问网络' : 'Access the network', model: zh ? '使用模型' : 'Use a model', tool: zh ? '使用工具' : 'Use a tool' })[kind];
  const grants = request?.GrantedAll?.length ? request.GrantedAll : grant ? [grant] : [];
  const proposals = input ? [input, ...(input.additional_requests ?? [])] : [];
  const approved = state === 'approved';
  const count = approved ? grants.length : proposals.length;
  const expiries = approved ? grants.map(item => item.ExpiresAt) : proposals.map(item => item.expires_at);
  const sharedExpiry = expiries.length > 0 && expiries.every(value => (value || null) === (expiries[0] || null));
  const cardTitle = count > 1 ? (zh ? `${count} 项资源权限` : `Access to ${count} resources`) : headline;
  useErrorToast(query.error || resource.error, () => { query.reload(); resource.reload(); });
  return <><ChatCard className="permission-request-card" eyebrow={zh ? '权限申请' : 'Permission request'} title={cardTitle} icon={count > 1 ? undefined : <Icon />} description={input?.reason ?? summary}
    status={state ? { label: state === 'pending' ? (zh ? '等待授权' : 'Awaiting permission') : state === 'approved' ? (zh ? '已批准' : 'Approved') : (zh ? '已拒绝' : 'Rejected'), tone: state === 'pending' ? 'warning' : state === 'approved' ? 'positive' : 'neutral' } : undefined}
    metadata={state === 'approved' ? (zh ? '已按确认的内容授权' : 'Granted with your confirmed settings') : state === 'rejected' ? (zh ? '申请已拒绝，未授予权限' : 'Request declined; no access granted') : undefined}
    actions={state === 'pending' && <><Button size="sm" variant="secondary" disabled={assistant.State !== 'active'} onClick={() => setReview(true)}>{zh ? '查看并授权' : 'Review permission'}</Button><Button size="sm" variant="ghost" disabled={assistant.State !== 'active'} onClick={() => setReject(true)}>{zh ? '拒绝' : 'Reject'}</Button></>}>
    <div className="permission-scopes">
      {approved ? grants.map(item => <PermissionScope key={item.ID} grant={item} showExpiry={!sharedExpiry} timeZone={timeZone} />)
        : proposals.map((item, index) => <PermissionScope key={index} input={item} showReason={index > 0} showExpiry={!sharedExpiry} timeZone={timeZone} />)}
    </div>
    {sharedExpiry && <div className="permission-request-expiry"><span>{zh ? '授权至' : 'Expires'}</span><PermissionExpiry value={expiries[0]} timeZone={timeZone} /></div>}
  </ChatCard>
  {review && request && <PermissionReview request={request} assistant={assistant} onClose={() => setReview(false)} />}
  {reject && request && <Confirm title={zh ? '拒绝授权请求' : 'Reject permission request'} description={request.Input.reason} onClose={() => setReject(false)} onConfirm={() => api.mutate('/api/permission-requests/resolve', {RequestID:request.ID,Decision:'reject'},request.Version)} />}
  </>;
}

export function PermissionRequests({ assistant }: { assistant: Assistant }) {
  const { t, locale } = useCopy(), zh = locale === 'zh';
  const query = useQuery<PermissionRequest[]>(`/api/permission-requests?assistant_id=${assistant.ID}`);
  const resources = useQuery<Resource[]>('/api/resources', true), sandboxes = useQuery<SandboxView[]>('/api/sandboxes');
  const [review, setReview] = useState<PermissionRequest | null>(null), [reject, setReject] = useState<PermissionRequest | null>(null);
  const states = zh ? { pending: '待审批', approved: '已批准', rejected: '已拒绝' } : { pending: 'Pending', approved: 'Approved', rejected: 'Rejected' };
  const pending = query.data?.filter(row => row.State === 'pending') ?? [], resolved = query.data?.filter(row => row.State !== 'pending') ?? [];
  useErrorToast(query.error, query.reload);
  function requestCard(row: PermissionRequest) {
    const resource = resources.data?.find(item => item.ID === row.Input.resource_id);
    const gpus = resource ? sandboxes.data?.find(node => node.ID === (resource.SandboxID || 'local'))?.Info.GPUs : undefined;
    return <Card key={row.ID}><div className="record-heading"><h3>{row.Input.resource_description}</h3><Badge tone={row.State === 'pending' ? 'warning' : 'neutral'}>{states[row.State]}</Badge></div>
      <p>{row.Input.reason}</p><p>{zh ? '申请权限' : 'Requested'}: {row.Input.full_access ? fullAccessLabel(zh) : t.resources.categories[row.Input.resource_kind]} · {row.Input.actions.map(action => (t.resourcePermissions.generic as Record<string, string>)[action] ?? action).join(' / ')}</p>
      {row.Input.resource_kind === 'storage' && <p>{t.resources.pathPrefixes}: {row.Input.path_prefixes?.join(', ') || '/'}</p>}
      <p className="muted">{row.Input.expires_at ? `${t.resources.expires}: ${new Date(row.Input.expires_at).toLocaleString(locale)}` : t.resourcePermissions.noExpiry}</p>
      <ConstraintsSummary gpus={gpus} value={row.Input.constraints} temporary={row.Input.temporary}/>{row.Granted && <GrantedPermission grant={row.Granted} />}
      {row.State === 'pending' && <div className="form-actions"><Button size="sm" disabled={assistant.State !== 'active'} onClick={() => setReview(row)}>{zh ? '审核授权' : 'Review permission'}</Button><Button size="sm" variant="secondary" disabled={assistant.State !== 'active'} onClick={() => setReject(row)}>{zh ? '拒绝' : 'Reject'}</Button></div>}
    </Card>;
  }
  return <section className="permission-requests">
    {!!pending.length && <><h2>{zh ? '授权请求' : 'Permission requests'}</h2><div className="record-list">{pending.map(requestCard)}</div></>}
    {!!resolved.length && <details><summary>{zh ? '已处理的授权请求' : 'Resolved permission requests'} · {resolved.length}</summary><div className="record-list">{resolved.map(requestCard)}</div></details>}
    {review && <PermissionReview request={review} assistant={assistant} onClose={() => setReview(null)} />}
    {reject && <Confirm title={zh ? '拒绝授权请求' : 'Reject permission request'} description={reject.Input.reason} onClose={() => setReject(null)} onConfirm={() => api.mutate('/api/permission-requests/resolve', { RequestID: reject.ID, Decision: 'reject' }, reject.Version)} />}
  </section>;
}

function GrantedPermission({ grant, timeZone }: { grant: Grant; timeZone?: string }) {
  const { t, locale } = useCopy(), zh = locale === 'zh';
  const resource = useQuery<Resource>(`/api/resources/${grant.ResourceID}`);
  const sandboxes = useQuery<SandboxView[]>('/api/sandboxes');
  const gpus = resource.data ? sandboxes.data?.find(node => node.ID === (resource.data?.SandboxID || 'local'))?.Info.GPUs : undefined;
  const models = useQuery<Model[]>(grant.Scope.ModelID ? '/api/models' : null);
  const target = grant.Scope.ModelID ? models.data?.find(model => model.ID === grant.Scope.ModelID)?.Name ?? t.resources.modelUnavailable : resource.data?.Name ?? t.loading;
  useErrorToast(resource.error || models.error, () => {resource.reload();models.reload();});
  return <div className="permission-granted"><p>{zh ? '实际批准' : 'Approved scope'}: {target} · {grant.Scope.FullAccess ? fullAccessLabel(zh) : grantSummary(t, grant, resource.data)}</p>
    {resource.data?.Kind === 'storage' && <p>{t.resources.pathPrefixes}: {grant.Scope.PathPrefixes?.join(', ') || '/'}</p>}
    <ConstraintsSummary gpus={gpus} value={grant.Constraints} temporary={grant.Temporary}/><p>{t.resources.expires}: {grant.ExpiresAt ? new Intl.DateTimeFormat(locale,{timeZone:timeZone||undefined,dateStyle:'medium',timeStyle:'short'}).format(new Date(grant.ExpiresAt)) : t.resourcePermissions.noExpiry}</p>
  </div>;
}

function PermissionReview({ request, assistant, onClose }: { request: PermissionRequest; assistant: Assistant; onClose: () => void }) {
  const { t, locale } = useCopy(), zh = locale === 'zh';
  const resources = useQuery<Resource[]>('/api/resources', true), models = useQuery<Model[]>('/api/models');
  const [items, setItems] = useState<PermissionRequestInput[]>(() => [request.Input, ...(request.Input.additional_requests ?? [])].map(item => ({ ...item, constraints: item.full_access ? undefined : item.constraints ?? (['compute', 'network'].includes(item.resource_kind) ? suggestedConstraints(item.resource_kind) : undefined) })));
  const grouped = items.length > 1;
  // Historical requests may contain different deadlines. Start with the earliest,
  // so opening the new shared editor cannot silently extend any proposal.
  const deadlines = items.map(item => item.expires_at).filter((value): value is string => !!value).sort((a, b) => Date.parse(a) - Date.parse(b));
  const [expires, setExpires] = useState<string | null>(deadlines[0] ?? null);
  const action = useAction(), formID = useId();
  const temporaryOutlastsAccess = !!expires && items.some(item => item.temporary && Date.parse(item.temporary.until) > Date.parse(expires));
  const expiryError = temporaryOutlastsAccess ? (zh ? '授权时间不能早于临时额度的到期时间，请调整其中一个时间。' : 'Authorization must last at least as long as temporary limits. Adjust either deadline.') : undefined;
  async function approve() {
    const resolutions = items.map(item => {
      const resource = resources.data?.find(row => row.ID === item.resource_id && row.State === 'available' && row.Kind === item.resource_kind);
      if (!resource || !item.actions.length || item.actions.some(value => !resource.Actions.includes(value)) ||
        (item.resource_kind === 'model' && !models.data?.some(model => model.ResourceID === resource.ID && model.ID === item.model_id))) throw new APIError('invalid_argument');
      return {
        ResourceID: resource.ID, GrantID: item.grant_id, GrantVersion: item.grant_version,
        Actions: item.actions, Scope: { FullAccess: item.full_access ?? false, ModelID: item.resource_kind === 'model' ? item.model_id ?? '' : '', PathPrefixes: item.resource_kind === 'storage' ? item.path_prefixes ?? [] : [], ReadOnly: item.actions.every(value => value === 'read') },
        ExpiresAt: expires, Constraints: item.full_access ? undefined : item.constraints, Temporary: item.full_access ? undefined : item.temporary,
      };
    });
    await api.mutate('/api/permission-requests/resolve', { RequestID: request.ID, Decision: 'approve', ...resolutions[0], Additional: resolutions.slice(1) }, request.Version);
  }
  useErrorToast(resources.error || models.error, () => { resources.reload(); models.reload(); });
  useErrorToast(action.error);
  return <Dialog open className="permission-review" title={zh ? '审核授权' : 'Review permission'} description={t.resourcePermissions.recipient(assistant.Name)}
    onOpenChange={open => { if (!open && !action.busy) onClose(); }} dismissible={!action.busy}
    footer={<><Button variant="secondary" disabled={action.busy} onClick={onClose}>{t.cancel}</Button><Button form={formID} type="submit" loading={action.busy} disabled={resources.loading || !!resources.error || !!models.error || !!expiryError}>{zh ? '批准授权' : 'Approve permission'}</Button></>}>
    <form id={formID} className="permission-review__form" onInvalidCapture={event => {
      // Native validation must be able to reveal and focus fields in a closed section.
      const section = (event.target as HTMLElement).closest('details');
      if (section) section.open = true;
    }} onSubmit={event => { event.preventDefault(); if (!action.busy && !expiryError) void action.run(approve, onClose); }}>
      <p className="permission-review__reason">{request.Input.reason}</p>
      <DateTimePicker label={grouped ? (zh ? '统一授权时间' : 'Shared authorization expiry') : t.resources.expires} value={expires} onValueChange={setExpires} min={new Date().toISOString()}
        error={expiryError} description={grouped ? (zh ? `以下 ${items.length} 项权限共用此有效期。` : `This duration applies to all ${items.length} permissions.`) : undefined} />
      {grouped && new Set(deadlines).size > 1 && <p className="permission-review__note">{zh ? '原申请的时间不同，已预选最早的到期时间，可统一调整。' : 'The proposals had different deadlines. The earliest is preselected; you can change it for the whole group.'}</p>}
      <div className="permission-review__items">{items.map((item, index) => <PermissionItemEditor key={index} item={item} grouped={grouped} primary={index === 0} resources={resources.data ?? []} models={models.data ?? []} loading={resources.loading}
        onChange={value => setItems(previous => previous.map((old, i) => i === index ? { ...old, ...value } : old))} />)}</div>
      <p className="permission-review__note">{zh ? '普通权限的额度由同一资源上的任务共享；完全访问不设额度。展开权限可调整授权内容。' : 'Tasks share quotas for ordinary permissions; full access has no quotas. Expand a permission to edit it.'}</p>
      {items.some(item => item.grant_id && item.resource_kind === 'compute') && <p className="permission-review__note">{zh ? '修改计算授权会停止受影响的运行任务。' : 'Changing compute access stops affected running tasks.'}</p>}
    </form>
  </Dialog>;
}

function PermissionItemEditor({ item, onChange, grouped, primary, resources, models, loading }: {
  item: PermissionRequestInput; onChange: (value: Partial<PermissionRequestInput>) => void; grouped: boolean; primary: boolean; resources: Resource[]; models: Model[]; loading: boolean;
}) {
  const { t, locale } = useCopy(), zh = locale === 'zh';
  const [open, setOpen] = useState(!grouped);
  const sandboxes = useQuery<SandboxView[]>('/api/sandboxes');
  const [nodeID, setNodeID] = useState('');
  const available = resources.filter(row => row.Kind === item.resource_kind && row.State === 'available');
  const selected = available.find(row => row.ID === item.resource_id);
  const target: PermissionTarget = { ...targetFor(selected, !!item.full_access, item.model_id), kind: item.resource_kind, resourceID: item.resource_id ?? '', sandboxID: nodeID || selected?.SandboxID || 'local' };
  function select(next: PermissionTarget) {
    const resource = resources.find(r => r.ID === next.resourceID);
    const changed = next.resourceID !== target.resourceID || next.fullAccess !== target.fullAccess || next.kind !== target.kind;
    setNodeID(next.sandboxID);
    onChange({ resource_kind: next.kind, resource_id: next.resourceID, model_id: next.modelID, full_access: next.fullAccess,
      ...(next.resourceID !== target.resourceID ? { grant_id: undefined, grant_version: undefined } : {}),
      ...(changed ? { actions: next.fullAccess ? ['execute'] : initialActions(resource), constraints: next.fullAccess ? undefined : initialConstraints(resource), temporary: undefined, path_prefixes: [] } : {}),
    });
  }
  const Icon = { storage: Folder, compute: Server, connection: Globe, network: Globe, model: Sparkles, tool: Wrench }[item.resource_kind];
  useEffect(() => {
    if (item.resource_id) return;
    const matches = available.filter(row => row.Name === item.resource_description);
    if (matches.length === 1) onChange({ resource_id: matches[0].ID });
  }, [resources, item.resource_id, item.resource_description, item.resource_kind]);
  useEffect(() => {
    if (selected?.Provider === 'managed-workspace' && !item.constraints) onChange({ constraints: suggestedConstraints('storage') });
  }, [selected?.Provider, item.constraints]);
  useErrorToast(sandboxes.error, sandboxes.reload);
  return <details className="permission-review-item" open={open} onToggle={event => setOpen(event.currentTarget.open)}>
    <summary><span className="permission-review-item__title"><Icon aria-hidden="true" /><strong>{isSandboxResource(item.resource_kind) ? `${zh ? '沙盒' : 'Sandbox'} · ` : ''}{item.full_access ? fullAccessLabel(zh) : t.resources.categories[item.resource_kind]}</strong><span>{open ? (zh ? '收起' : 'Collapse') : (zh ? '调整' : 'Edit')}</span></span>
      <span className="permission-review-item__name">{selected?.Name ?? item.resource_description} · {item.actions.map(value => permissionLabel(t, item.resource_kind, value)).join(' / ')}</span>
      {item.resource_kind === 'storage' && <span className="permission-review-item__name">{t.resources.pathPrefixes} · {item.path_prefixes?.join(', ') || '/'}</span>}
      {item.model_id && <span className="permission-review-item__name">{t.resources.categories.model} · {models.find(model => model.ID === item.model_id)?.Name ?? t.resources.modelUnavailable}</span>}
      {!item.full_access && <ConstraintsSummary gpus={sandboxes.data?.find(s=>s.ID===target.sandboxID)?.Info.GPUs} value={item.constraints} temporary={item.temporary} />}
    </summary>
    <div className="permission-review-item__editor">
      <p className="permission-review__note">{item.reason}</p>
      <PermissionTargetFields value={target} onChange={select} resources={resources} models={models} sandboxes={sandboxes.data ?? []} sandboxOnly={!primary} />
      {!loading && !available.length && <p>{zh ? '请先连接所需资源。' : 'Connect the required resource first.'} <a href="#/resources">{t.nav.resources}</a></p>}
      {!item.full_access && <Select multiple label={t.resources.actions} value={item.actions} onValueChange={value => onChange({ actions: value })} options={permissionOptions(t, selected)} />}
      {item.resource_kind === 'storage' && <Textarea label={t.resources.pathPrefixes} value={item.path_prefixes?.join('\n') ?? ''} onChange={event => onChange({ path_prefixes: lines(event.target.value) })} description={zh ? '相对于资源根目录；留空表示整个资源。' : 'Relative to the resource root; empty means the entire resource.'} />}
      {!item.full_access && <><ConstraintsEditor gpus={sandboxes.data?.find(s=>s.ID===target.sandboxID)?.Info.GPUs} compact kind={item.resource_kind} value={item.constraints} onChange={value => onChange({ constraints: value })} lockNetworkPeriod={!!item.grant_id && !!item.constraints?.network} />
      <TemporaryConstraintsEditor gpus={sandboxes.data?.find(s=>s.ID===target.sandboxID)?.Info.GPUs} compact kind={item.resource_kind} base={item.constraints} value={item.temporary} onChange={value => onChange({ temporary: value })} /></>}
    </div>
  </details>;
}
