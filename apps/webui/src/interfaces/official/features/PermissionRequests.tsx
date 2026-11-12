import { isDelegationPermission } from '../../../delegation-permissions';
import { DelegationPermissionRequestCard } from './DelegationPermissionRequest';
import { FullAccessScopeEditor, FullAccessScopeSummary, validFullAccessScope } from './FullAccessScope';
import { useMutation } from '../../../use-mutation';
import { usesNetworkToggle } from '../../../sandbox-types';
import { useErrorToast } from '../../../error-toasts';
import { PermissionTargetFields, fullAccessLabel, isSandboxResource, targetFor } from './PermissionTargetFields';
import type { PermissionTarget } from './PermissionTargetFields';
import type { SandboxView } from '@molr/sdk';
import { initialActions, initialConstraints } from './GrantEditor';
import { ConstraintsEditor, constraintsForNode, ConstraintsSummary, TemporaryConstraintsEditor, suggestedConstraints } from './ResourceConstraints';
import type { ResourcePermissionRequestInput as PermissionRequestInput } from '@molr/sdk';
import { ArrowLeft, Folder, Globe, Server, ShieldAlert, Sparkles, Terminal, Wrench } from 'lucide-react';
import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { ModalPresence, Button, ChatCard, DateTimePicker, Dialog, Select, Switch, Textarea } from '@molr/ui';
import { PermissionExpiry, PermissionScope } from './PermissionScope';
import type { Assistant, DelegationPermissionRequestInput, Model, PermissionRequest, ResourcePermissionRequestInput, Resource } from '@molr/sdk';
import { APIError } from '@molr/sdk';
import { useCopy, useQuery } from '../../../state';
import { permissionLabel, permissionOptions } from '../../../resource-permissions';
import { Confirm, lines, useAction } from '../components/common';

type ResourceRequest = PermissionRequest<ResourcePermissionRequestInput>;
type CardProps = { timeZone?: string; requestID: string; assistant: Assistant; title: string; summary: string };
export function PermissionRequestCard(props: CardProps) {
  const query = useQuery<PermissionRequest>(`/api/permission-requests/${props.requestID}`);
  useErrorToast(query.error, query.reload);
  if (query.data) return <PermissionRequestRecordCard assistant={props.assistant} request={query.data} timeZone={props.timeZone} />;
  return <ResourcePermissionRequestCard {...props} request={query.data as ResourceRequest | undefined} />;
}
export function PermissionRequestRecordCard({ request, assistant, timeZone }: { request: PermissionRequest; assistant: Assistant; timeZone?: string }) {
  if (isDelegationPermission(request.Input)) return <DelegationPermissionRequestCard assistant={assistant} request={request as PermissionRequest<DelegationPermissionRequestInput>} />;
  return <ResourcePermissionRequestCard requestID={request.ID} assistant={assistant} request={request as ResourceRequest} title="" summary="" timeZone={timeZone} />;
}
function ResourcePermissionRequestCard({ requestID, assistant, title, summary, timeZone, request }: CardProps & { request?: ResourceRequest }) {
  const { t, locale } = useCopy(), zh = locale === 'zh';
  const [review, setReview] = useState(false), [reject, setReject] = useState(false);
  const state = request?.State;
  const input = request?.Input;
  const grant = request?.GrantedAll?.[0] ?? request?.Granted;
  const resource = useQuery<Resource>(grant ? `/api/resources/${grant.ResourceID}` : null);
  const kind = grant ? resource.data?.Kind ?? input?.resource_kind : input?.resource_kind;
  const actions = grant?.Actions ?? input?.actions ?? [];
  const Icon = kind ? { storage: Folder, compute: Server, connection: Globe, network: Globe, model: Sparkles, tool: Wrench }[kind] : Wrench;
  const fullAccess = grant ? !!grant.Scope.FullAccess : !!input?.full_access;
  const fullScope = grant ? grant.Scope.FullAccessScope : input?.full_access_scope;
  const headline = fullAccess ? fullScope?.mode === 'unrestricted' ? (zh ? '完全访问宿主机' : 'Unrestricted host access') : fullScope?.mode === 'programs' ? (zh ? '运行指定程序' : 'Run specified programs') : (zh ? '申请完全访问' : 'Request Full Access') : !kind ? (title || (zh ? '申请使用资源' : 'Request resource access')) : kind === 'storage'
    ? actions.includes('read') && actions.includes('write') ? (zh ? '读取和修改文件' : 'Read and modify files') : actions.includes('write') ? (zh ? '修改文件' : 'Modify files') : (zh ? '读取文件' : 'Read files')
    : ({ connection: zh ? '使用服务连接' : 'Use a connection', compute: zh ? '运行命令' : 'Run commands', network: zh ? '访问网络' : 'Access the network', model: zh ? '使用模型' : 'Use a model', tool: zh ? '使用工具' : 'Use a tool' })[kind];
  const grants = request?.GrantedAll?.length ? request.GrantedAll : grant ? [grant] : [];
  const proposals = input ? [input, ...(input.additional_requests ?? [])] : [];
  const approved = state === 'approved';
  const count = approved ? grants.length : proposals.length;
  const expiries = approved ? grants.map(item => item.ExpiresAt) : proposals.map(item => item.expires_at);
  const sharedExpiry = expiries.length > 0 && expiries.every(value => (value || null) === (expiries[0] || null));
  const cardTitle = count > 1 ? (zh ? `${count} 项资源权限` : `Access to ${count} resources`) : headline;
  useErrorToast(resource.error, resource.reload);
  const rejectPermission = useMutation([requestID, reject]);
  const reviewActions = state === 'pending' && <><Button size="sm" variant="secondary" disabled={assistant.State !== 'active'} onClick={() => setReview(true)}>{zh ? '查看并授权' : 'Review permission'}</Button><Button size="sm" variant="ghost" disabled={assistant.State !== 'active'} onClick={() => setReject(true)}>{zh ? '拒绝' : 'Reject'}</Button></>;
  return <><ChatCard className="permission-request-card" eyebrow={fullAccess && count === 1 ? (zh ? '完全访问申请' : 'Full Access request') : (zh ? '权限申请' : 'Permission request')} title={cardTitle} icon={count > 1 ? undefined : fullAccess ? fullScope?.mode === 'unrestricted' ? <ShieldAlert /> : <Terminal /> : <Icon />} description={input?.reason ?? summary}
    status={state ? { label: state === 'pending' ? (zh ? '等待授权' : 'Awaiting permission') : state === 'approved' ? (zh ? '已批准' : 'Approved') : (zh ? '已拒绝' : 'Rejected'), tone: state === 'pending' ? 'warning' : state === 'approved' ? 'positive' : 'neutral' } : undefined}
    metadata={state === 'approved' ? (zh ? '已按确认的内容授权' : 'Granted with your confirmed settings') : state === 'rejected' ? (zh ? '申请已拒绝，未授予权限' : 'Request declined; no access granted') : undefined}
    actions={reviewActions}>
    <div className="permission-scopes">
      {approved ? grants.map(item => <PermissionScope grouped={count > 1} key={item.ID} grant={item} showExpiry={!sharedExpiry} timeZone={timeZone} />)
        : proposals.map((item, index) => <PermissionScope grouped={count > 1} key={index} input={item} showReason={index > 0} showExpiry={!sharedExpiry} timeZone={timeZone} />)}
    </div>
    {sharedExpiry && <div className="permission-request-meta">
      <div className="permission-request-meta__expiry"><span>{t.resources.expires}</span><PermissionExpiry value={expiries[0]} timeZone={timeZone} /></div>
    </div>}
  </ChatCard>
  <ModalPresence>{review && request && <PermissionReview request={request} assistant={assistant} onClose={() => setReview(false)} />}</ModalPresence>
  <ModalPresence>{reject && request && <Confirm title={zh ? '拒绝授权请求' : 'Reject permission request'} description={request.Input.reason} onClose={() => setReject(false)} onConfirm={() => rejectPermission('/api/permission-requests/resolve', {RequestID:request.ID,Decision:'reject'},request.Version)} />}</ModalPresence>
  </>;
}

export function PermissionRequests({ assistant, history, onHistoryChange }: { assistant: Assistant; history: boolean; onHistoryChange: (history: boolean) => void }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  const query = useQuery<PermissionRequest[]>(`/api/permission-requests?assistant_id=${assistant.ID}`);
  const pending = query.data?.filter(row => row.State === 'pending') ?? [], resolved = query.data?.filter(row => row.State !== 'pending') ?? [];
  useErrorToast(query.error, query.reload);
  const requestCard = (row: PermissionRequest) => <PermissionRequestRecordCard key={row.ID} assistant={assistant} request={row} />;
  const container = useRef<HTMLElement>(null), historyButton = useRef<HTMLButtonElement>(null), historyTitle = useRef<HTMLHeadingElement>(null);
  const previousHistory = useRef(history), listScroll = useRef(0);
  useLayoutEffect(() => {
    if (previousHistory.current === history) return;
    previousHistory.current = history;
    const scroll = container.current?.closest('.ui-dialog__body');
    if (scroll) scroll.scrollTop = history ? 0 : listScroll.current;
    if (history) historyTitle.current?.focus({ preventScroll: true });
    else historyButton.current?.focus({ preventScroll: true });
  }, [history]);
  if (!query.data?.length) return null;
  return <section ref={container} className="assistant-settings-section permission-requests" aria-label={history ? (zh ? '授权历史' : 'Authorization history') : (zh ? '授权请求' : 'Permission requests')}>

    {history ? <>
      <div className="permission-history-heading">
        <Button size="sm" variant="ghost" onClick={() => onHistoryChange(false)}><ArrowLeft size={16} aria-hidden="true" />{zh ? '返回' : 'Back'}</Button>
        <h3 ref={historyTitle} tabIndex={-1}>{zh ? '授权历史' : 'Authorization history'}</h3>
      </div>
      <div className="record-list">{resolved.map(requestCard)}</div>
    </> : <>
      <h3>{zh ? '授权请求' : 'Permission requests'}</h3>
      <div className="assistant-info-section-heading permission-requests-actions">
        <p className="assistant-info-help">{pending.length ? (zh ? `${pending.length} 个授权请求待处理` : `${pending.length} pending permission request${pending.length === 1 ? '' : 's'}`) : (zh ? '暂无待处理的授权请求' : 'No pending permission requests')}</p>
        {!!resolved.length && <Button ref={historyButton} size="sm" variant="secondary" onClick={() => {
          listScroll.current = container.current?.closest('.ui-dialog__body')?.scrollTop ?? 0;
          onHistoryChange(true);
        }}>{zh ? '授权历史' : 'Authorization history'}</Button>}
      </div>
      {!!pending.length && <div className="record-list">{pending.map(requestCard)}</div>}
    </>}
  </section>;
}

function PermissionReview({ request, assistant, onClose }: { request: ResourceRequest; assistant: Assistant; onClose: () => void }) {
  const { t, locale } = useCopy(), zh = locale === 'zh';
  const resources = useQuery<Resource[]>('/api/resources', true), models = useQuery<Model[]>('/api/models');
  const [items, setItems] = useState<PermissionRequestInput[]>(() => [request.Input, ...(request.Input.additional_requests ?? [])].map(item => ({ ...item, constraints: item.full_access ? undefined : item.constraints ?? (['compute', 'network'].includes(item.resource_kind) ? suggestedConstraints(item.resource_kind) : undefined) })));
  const grouped = items.length > 1;
  // Historical requests may contain different deadlines. Start with the earliest,
  // so opening the new shared editor cannot silently extend any proposal.
  const deadlines = items.map(item => item.expires_at).filter((value): value is string => !!value).sort((a, b) => Date.parse(a) - Date.parse(b));
  const [expires, setExpires] = useState<string | null>(deadlines[0] ?? (items.some(item => item.full_access) ? new Date(Date.now() + 86400000).toISOString() : null));
  const action = useAction(), formID = useId();
  const temporaryOutlastsAccess = !!expires && items.some(item => item.temporary && Date.parse(item.temporary.until) > Date.parse(expires));
  const expiryError = temporaryOutlastsAccess ? (zh ? '授权时间不能早于临时额度的到期时间，请调整其中一个时间。' : 'Authorization must last at least as long as temporary limits. Adjust either deadline.') : undefined;
  const approvePermission = useMutation([request.ID, items, expires]);
  async function approve() {
    const resolutions = items.map(item => {
      const resource = resources.data?.find(row => row.ID === item.resource_id && row.State === 'available' && row.Kind === item.resource_kind);
      if (item.full_access && !validFullAccessScope(item.full_access_scope)) throw new APIError('invalid_argument');
      if (!resource || !item.actions.length || item.actions.some(value => !resource.Actions.includes(value)) ||
        (item.resource_kind === 'model' && !models.data?.some(model => model.ResourceID === resource.ID && model.ID === item.model_id))) throw new APIError('invalid_argument');
      return {
        ResourceID: resource.ID, GrantID: item.grant_id, GrantVersion: item.grant_version,
        Actions: item.actions, Scope: { Once: !!item.full_access && !!item.once, FullAccess: item.full_access ?? false, FullAccessScope: item.full_access ? item.full_access_scope : undefined, ModelID: item.resource_kind === 'model' ? item.model_id ?? '' : '', PathPrefixes: item.resource_kind === 'storage' ? item.path_prefixes ?? [] : [], ReadOnly: item.actions.every(value => value === 'read') },
        ExpiresAt: expires, Constraints: item.full_access ? undefined : item.constraints, Temporary: item.full_access ? undefined : item.temporary,
      };
    });
    await approvePermission('/api/permission-requests/resolve', { RequestID: request.ID, Decision: 'approve', ...resolutions[0], Additional: resolutions.slice(1) }, request.Version);
  }
  useErrorToast(resources.error || models.error, () => { resources.reload(); models.reload(); });
  useErrorToast(action.error);
  return <Dialog open className="permission-review" title={zh ? '审核授权' : 'Review permission'} description={t.resourcePermissions.recipient(assistant.Name)}
    onOpenChange={open => { if (!open && !action.busy) onClose(); }} dismissible={!action.busy}
    footer={<><Button variant="secondary" disabled={action.busy} onClick={onClose}>{t.cancel}</Button><Button form={formID} type="submit" loading={action.busy} disabled={items.some(item => item.full_access && !validFullAccessScope(item.full_access_scope)) || resources.loading || !!resources.error || !!models.error || !!expiryError}>{zh ? '批准授权' : 'Approve permission'}</Button></>}>
    <form id={formID} className="permission-review__form" onInvalidCapture={event => {
      // Native validation must be able to reveal and focus fields in a closed section.
      const section = (event.target as HTMLElement).closest('details');
      if (section) section.open = true;
    }} onSubmit={event => { event.preventDefault(); if (!action.busy && !expiryError) void action.run(approve, onClose); }}>
      <p className="permission-review__reason">{request.Input.reason}</p>
      <DateTimePicker className="permission-expiry-picker" size="md" label={grouped ? (zh ? '统一有效期' : 'Shared validity') : t.resources.expires} value={expires} onValueChange={setExpires} min={new Date().toISOString()}
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
    const changed = next.sandboxID !== target.sandboxID || next.resourceID !== target.resourceID || next.fullAccess !== target.fullAccess || next.kind !== target.kind;
    setNodeID(next.sandboxID);
    onChange({ resource_kind: next.kind, resource_id: next.resourceID, model_id: next.modelID, full_access: next.fullAccess,
      ...(next.resourceID !== target.resourceID ? { grant_id: undefined, grant_version: undefined } : {}),
      ...(changed ? { full_access_scope: undefined, once: false, actions: next.fullAccess ? ['execute'] : initialActions(resource), constraints: next.fullAccess ? undefined : initialConstraints(resource), temporary: undefined, path_prefixes: [] } : {}),
    });
  }
  const Icon = { storage: Folder, compute: Server, connection: Globe, network: Globe, model: Sparkles, tool: Wrench }[item.resource_kind];
  useEffect(() => {
    if (item.resource_id) return;
    const matches = available.filter(row => row.Name === item.resource_description);
    if (matches.length === 1) onChange({ resource_id: matches[0].ID });
  }, [resources, item.resource_id, item.resource_description, item.resource_kind]);
  useEffect(() => {
    if (selected?.Provider === 'managed-workspace' && sandboxes.data?.find(s=>s.ID===target.sandboxID)?.Info.WorkspaceProjectQuotas && !item.constraints) onChange({ constraints: suggestedConstraints('storage') });
  }, [selected?.Provider, item.constraints]);
  const nativeInfo=sandboxes.data?.find(s=>s.ID===target.sandboxID)?.Info;
  useEffect(()=>{if(!usesNetworkToggle(nativeInfo?.Capabilities.NetworkBackend)||item.full_access)return;const constraints=constraintsForNode(item.resource_kind,item.constraints,nativeInfo);const limits=item.temporary?constraintsForNode(item.resource_kind,item.temporary.limits,nativeInfo):undefined;const temporary=limits&&item.temporary?{...item.temporary,limits}:undefined;if(JSON.stringify(constraints)!==JSON.stringify(item.constraints)||JSON.stringify(temporary)!==JSON.stringify(item.temporary))onChange({constraints,temporary});},[nativeInfo?.Capabilities.NetworkBackend,item.resource_kind,item.constraints,item.temporary,item.full_access]);
  useErrorToast(sandboxes.error, sandboxes.reload);
  return <details className="permission-review-item" open={open} onToggle={event => setOpen(event.currentTarget.open)}>
    <summary><span className="permission-review-item__title"><Icon aria-hidden="true" /><strong>{isSandboxResource(item.resource_kind) ? `${zh ? '沙盒' : 'Sandbox'} · ` : ''}{item.full_access ? fullAccessLabel(zh) : t.resources.categories[item.resource_kind]}</strong><span>{open ? (zh ? '收起' : 'Collapse') : (zh ? '调整' : 'Edit')}</span></span>
      <span className="permission-review-item__name">{selected?.Name ?? item.resource_description} · {item.actions.map(value => permissionLabel(t, item.resource_kind, value)).join(' / ')}</span>
      {item.resource_kind === 'storage' && <span className="permission-review-item__name">{t.resources.pathPrefixes} · {item.path_prefixes?.join(', ') || '/'}</span>}
      {item.model_id && <span className="permission-review-item__name">{t.resources.categories.model} · {models.find(model => model.ID === item.model_id)?.Name ?? t.resources.modelUnavailable}</span>}
      {item.full_access && <FullAccessScopeSummary value={item.full_access_scope} />}
      {!item.full_access && <ConstraintsSummary gpuAccessMode={sandboxes.data?.find(s=>s.ID===target.sandboxID)?.Info.Capabilities.GPUAccessMode} gpus={sandboxes.data?.find(s=>s.ID===target.sandboxID)?.Info.GPUs} value={item.constraints} temporary={item.temporary} />}
    </summary>
    <div className="permission-review-item__editor">
      <p className="permission-review__note">{item.reason}</p>
      <PermissionTargetFields value={target} onChange={select} resources={resources} models={models} sandboxes={sandboxes.data ?? []} sandboxOnly={!primary} />
      {!loading && !available.length && <p>{zh ? '请先连接所需资源。' : 'Connect the required resource first.'} <a href="#/resources">{t.nav.resources}</a></p>}
      {item.full_access && <><FullAccessScopeEditor sandboxID={target.sandboxID} value={item.full_access_scope} onChange={value => onChange({ full_access_scope: value })} /><Switch size="sm" label={zh ? '仅执行一次' : 'Use once'} checked={!!item.once} onChange={event => onChange({ once: event.target.checked })} /></>}
      {!item.full_access && <Select multiple label={t.resources.actions} value={item.actions} onValueChange={value => onChange({ actions: value })} options={permissionOptions(t, selected)} />}
      {item.resource_kind === 'storage' && <Textarea label={t.resources.pathPrefixes} value={item.path_prefixes?.join('\n') ?? ''} onChange={event => onChange({ path_prefixes: lines(event.target.value) })} description={zh ? '相对于资源根目录；留空表示整个资源。' : 'Relative to the resource root; empty means the entire resource.'} />}
      {!item.full_access && <><ConstraintsEditor gpuAccessMode={sandboxes.data?.find(s=>s.ID===target.sandboxID)?.Info.Capabilities.GPUAccessMode} gpus={sandboxes.data?.find(s=>s.ID===target.sandboxID)?.Info.GPUs} compact kind={item.resource_kind} value={item.constraints} onChange={value => onChange({ constraints: value })} lockNetworkPeriod={!!item.grant_id && !!item.constraints?.network} />
      <TemporaryConstraintsEditor gpuAccessMode={sandboxes.data?.find(s=>s.ID===target.sandboxID)?.Info.Capabilities.GPUAccessMode} gpus={sandboxes.data?.find(s=>s.ID===target.sandboxID)?.Info.GPUs} compact kind={item.resource_kind} base={item.constraints} value={item.temporary} onChange={value => onChange({ temporary: value })} /></>}
    </div>
  </details>;
}
