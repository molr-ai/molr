import { AssistantDelegationPermissions } from './AssistantDelegationPermissions';
import { useMutation } from '../../../use-mutation';
import { useErrorToast } from '../../../error-toasts';
import { GrantEditor } from './GrantEditor';
import { fullAccessLabel, isSandboxResource } from './PermissionTargetFields';
import { ResourceQuotaUsage } from './ResourceConstraints';
import { PermissionScope, PermissionExpiry } from './PermissionScope';
import { PermissionRequests } from './PermissionRequests';
import { useEffect, useRef, useState } from 'react';
import { ModalPresence, SlidingHighlight, Button, Dialog, DropdownMenu, IconButton, Tabs, useToast, useBreakpoint } from '@molr/ui';
import { BookOpen, Bot, ChevronLeft, ChevronRight, Ellipsis, Folder, Globe, Info, Pencil, Plus, RefreshCw, Server, Trash2, Unplug, Wifi, Wrench } from 'lucide-react';
import { DefaultModelSettings } from './DefaultModelSettings';
import { ProviderIcon } from './ProviderIcon';
import { ProviderModelsPanel } from './ProviderModelsPanel';
import { ProviderConnectionDialog } from './ProviderConnectionDialog';
import { Sandboxes } from './Sandboxes';
import { ToolsCatalog } from './ToolsCatalog';
import { SkillsCatalog } from './SkillsCatalog';
import type { SandboxView } from './Sandboxes';
import { useNavigation } from '../../../navigation';
import { useSandboxPreference } from '../../../sandbox-navigation';
import { useSandboxCopy } from '../../../sandbox-copy';
import { useReauthentication } from '../components/ReauthenticationDialog';
import { api, mutationKey } from '@molr/sdk';
import type { Assistant, Grant, Model, Resource, PricingRefreshResult } from '@molr/sdk';
import { useCopy, useQuery, useSession } from '../../../state';
import { grantSummary, grantTarget } from '../../../resource-permissions';
import { Confirm, Empty, Loading, PageHeader, useAction } from '../components/common';
import './resources.css';
type Tab = 'model' | 'tool' | 'skill' | 'sandbox';
export function Resources({ resourceID = '' }: { resourceID?: string }) {
  const { t } = useCopy(), copy = useSandboxCopy(), query = useQuery<Resource[]>('/api/resources', true), models = useQuery<Model[]>('/api/models'), action = useAction();
  const { navigate, navigateSandbox, sandboxID } = useNavigation();
  const mobile = useBreakpoint('mobile'), providerTrigger = useRef<HTMLButtonElement | null>(null), headerBack = useRef<HTMLButtonElement>(null);
  const { principal } = useSession(), { toast } = useToast(), pricingAction = useAction();
  const refreshPrices = () => void pricingAction.run(async () => {
    const result = await api.post<PricingRefreshResult>('/api/models/refresh-pricing');
    models.reload();
    toast({ title: result.Source === 'builtin' ? t.resources.pricingBuiltin : result.Updated ? t.resources.pricingUpdated : t.resources.pricingUnchanged });
  }, undefined, false);
  useErrorToast(pricingAction.error, refreshPrices);
  const [status, setStatus] = useState<Record<string, string>>({});
  const [tab, setTab] = useSandboxPreference<Tab>('resource-tab', 'model', value => ['model', 'tool', 'skill', 'sandbox'].includes(value));
  const [connectSandbox, setConnectSandbox] = useState(false), [modelBusy, setModelBusy] = useState(false);
  const [connect, setConnect] = useState(false), [defaultModel, setDefaultModel] = useState(false);
  const [editingProvider, setEditingProvider] = useState<Resource | null>(null), [deletingProvider, setDeletingProvider] = useState<Resource | null>(null), [disconnect, setDisconnect] = useState<Resource | null>(null);
  const target = query.data?.find(resource => resource.ID === resourceID);
  useEffect(() => { if (target) { setTab(target.Kind === 'model' || target.Kind === 'tool' ? target.Kind : 'sandbox'); } }, [target?.ID, target?.Kind]);
  useEffect(() => { if (sandboxID) setTab('sandbox'); }, [sandboxID]);
  const providers = (query.data ?? []).filter(resource => resource.Kind === 'model');
  const provider = providers.find(resource => resource.ID === resourceID) ?? (mobile ? undefined : providers[0]);
  useEffect(() => { if (!resourceID) providerTrigger.current?.focus({ preventScroll: true }); }, [resourceID]);
  const mobileDetail = mobile && (tab === 'model' && !!provider || tab === 'sandbox' && (!!sandboxID || !!target && ['storage', 'compute', 'network'].includes(target.Kind)));
  useEffect(() => { if (mobileDetail) headerBack.current?.focus({ preventScroll: true }); }, [mobileDetail, resourceID, sandboxID]);
  useErrorToast(query.error || models.error || action.error, () => { query.reload(); models.reload(); });
  const refreshModels = useMutation([]);
  const providerActions = (resource: Resource) => <DropdownMenu label={t.more} align="end" trigger={<IconButton label={t.more}><Ellipsis size={18} /></IconButton>} items={[
      { id: 'refresh-models', label: t.resources.refreshModels, icon: <RefreshCw size={15} />, disabled: action.busy || resource.State === 'disconnected', onSelect: () => void action.run(() => refreshModels('/api/resources/refresh-models', { ID: resource.ID }, resource.Version, resource.ID), undefined, false) },
      { id: 'check', label: t.resources.check, icon: <Wifi size={15} />, disabled: action.busy || resource.State === 'disconnected', onSelect: () => void action.run(async () => { const result = await api.post<{ State: string }>(`/api/resources/${resource.ID}/check`); setStatus(previous => ({ ...previous, [resource.ID]: result.State })); }, undefined, false) },
      { id: 'edit', label: t.resources.editProvider, icon: <Pencil size={15} />, disabled: resource.State !== 'available' && resource.Authentication !== 'chatgpt', onSelect: () => setEditingProvider(resource) },
      ...(resource.State !== 'disconnected' ? [{ id: 'disconnect', label: t.resources.disconnect, icon: <Unplug size={15} />, onSelect: () => resource.Authentication === 'chatgpt' ? setEditingProvider(resource) : setDisconnect(resource) }] : []),
      { id: 'delete', label: t.resources.deleteProvider, icon: <Trash2 size={15} />, danger: true, onSelect: () => resource.Authentication === 'chatgpt' && resource.State !== 'disconnected' ? setEditingProvider(resource) : setDeletingProvider(resource) },
    ]} />;
  const modelContent = <div className="resources-model-layout">
    <nav className="resources-provider-nav" aria-label={t.resources.providerList} hidden={mobile && !!provider}>
      <SlidingHighlight className="resources-provider-options" value={provider?.ID} disabled={mobile}>
        {query.loading && !query.data ? <Loading /> : providers.map(resource => {
          const connectionState = status[resource.ID] || resource.State;
          return <button key={resource.ID} type="button" className="resources-provider-option resource-sidebar-item" data-sliding-item={resource.ID} aria-label={resource.Name} aria-current={provider?.ID === resource.ID ? 'page' : undefined} onClick={event => { providerTrigger.current = event.currentTarget; navigate('resources', resource.ID); }}>
          <ProviderIcon provider={resource.Preset || (resource.Authentication === 'chatgpt' ? 'openai' : resource.Provider)} size={17} fallback={<Bot size={17} data-provider-icon="fallback" aria-hidden="true" />} /><span className="resource-sidebar-copy"><strong>{resource.Name}</strong><small>{resource.Authentication === 'chatgpt' ? 'OpenAI · ChatGPT' : resource.Provider}</small><span className={`resource-sidebar-state ${connectionState === 'available' ? 'is-connected' : ''}`}>{t.resources[connectionState as 'available' | 'offline' | 'disconnected']}</span></span><ChevronRight className="resources-provider-caret" size={16} aria-hidden="true" />
        </button>; })}
      </SlidingHighlight>
      {!mobile && <Button size="sm" variant="secondary" onClick={() => setConnect(true)}><Plus size={14} aria-hidden="true" />{t.resources.connectActions.model}</Button>}
    </nav>
    <div className="resources-provider-detail" hidden={mobile && !provider}>
      {provider ? <ProviderModelsPanel key={provider.ID} resource={provider} models={(models.data ?? []).filter(model => model.ResourceID === provider.ID)} loading={models.loading && !models.data} onBusyChange={setModelBusy} actions={providerActions(provider)} connectionState={status[provider.ID] || provider.State} /> : !query.loading && <div className="resources-model-empty"><span className="resources-model-empty-icon" aria-hidden="true"><Bot size={32} /></span><h2>{t.resources.emptyCategory.model}</h2><p>{t.resources.connectHelp.model}</p></div>}
    </div>
    {mobile && !provider && !providers.length && !query.loading && <p className="resources-provider-empty muted">{t.resources.connectHelp.model}</p>}
  </div>;
  const disconnectResource = useMutation([disconnect]);
  return <section className="resources-workspace"><PageHeader className="resources-header" title={t.nav.resources} icon={<Folder />} leading={mobileDetail ? <IconButton ref={headerBack} label={t.back} disabled={tab === 'model' && modelBusy} onClick={() => tab === 'sandbox' ? navigateSandbox() : navigate('resources')}><ChevronLeft size={18} /></IconButton> : undefined} actions={(tab === 'model' || tab === 'sandbox') && <DropdownMenu label={t.more} align="end" trigger={<IconButton label={t.more}><Ellipsis size={18} /></IconButton>} items={tab === 'sandbox' ? [{ id: 'connect-sandbox', label: copy.connect, icon: <Plus size={15} />, onSelect: () => setConnectSandbox(true) }] : [{ id: 'connect-provider', label: t.resources.connectActions.model, icon: <Plus size={15} />, onSelect: () => setConnect(true) }, { id: 'default-model', label: t.resources.defaultModel, icon: <Bot size={15} />, onSelect: () => setDefaultModel(true) }, ...(principal?.User?.Administrator ? [{ id: 'refresh-pricing', label: t.resources.refreshPricing, icon: <RefreshCw size={15} />, disabled: pricingAction.busy, onSelect: refreshPrices }] : [])]} />} />
    <Tabs className="resources-categories" label={t.resources.categoriesLabel} value={tab} onValueChange={setTab} items={[
      { value: 'model', label: t.resources.categories.model, icon: <Bot size={15} />, content: tab === 'model' ? modelContent : null },
      { value: 'tool', label: t.resources.categories.tool, icon: <Wrench size={15} />, content: tab === 'tool' ? <ToolsCatalog /> : null },
      { value: 'skill', label: 'Skills', icon: <BookOpen size={15} />, content: tab === 'skill' ? <SkillsCatalog /> : null },
      { value: 'sandbox', label: copy.title, icon: <Server size={15} />, content: tab === 'sandbox' ? <Sandboxes resources={query.data ?? []} resourceID={resourceID} connecting={connectSandbox} onConnectingChange={setConnectSandbox} /> : null },
    ]} />

    <ModalPresence>{defaultModel && <DefaultModelSettings models={models.data ?? []} loading={models.loading} onClose={() => setDefaultModel(false)} />}</ModalPresence>
    <ModalPresence>{connect && <ProviderConnectionDialog onConnected={resource => { setTab('model'); navigate('resources', resource.ID); }} onClose={() => setConnect(false)} />}</ModalPresence><ModalPresence>{editingProvider && <ProviderConnectionDialog resourceID={editingProvider.ID} onConnected={() => {}} onClose={() => setEditingProvider(null)} />}</ModalPresence><ModalPresence>{deletingProvider && <DeleteProvider resource={deletingProvider} onClose={() => setDeletingProvider(null)} />}</ModalPresence><ModalPresence>{disconnect && <Confirm title={t.resources.disconnect} description={t.resources.disconnectHelp} danger onClose={() => setDisconnect(null)} onConfirm={() => disconnectResource('/api/resources/disconnect', { ID: disconnect.ID }, disconnect.Version)} />}</ModalPresence>
  </section>;
}
function DeleteProvider({ resource, onClose }: { resource: Resource; onClose: () => void }) {
  const { t } = useCopy(), verification = useReauthentication();
  const [key] = useState(mutationKey);
  return <><Confirm title={t.resources.deleteProvider} description={t.resources.deleteProviderHelp.replace('{name}', resource.Name)} danger onClose={onClose} onConfirm={async () => {
    const proof = await verification.verify('credential.delete');
    await api.post('/api/resources/delete-provider', { ID: resource.ID, verification: proof, Mutation: { IdempotencyKey: key, ExpectedVersion: resource.Version } });
  }} />{verification.dialog}</>;
}
export function AssistantPermissions({ assistant }: { assistant: Assistant }) {
  const { t, locale } = useCopy(), copy = useSandboxCopy(), sandboxes = useQuery<SandboxView[]>('/api/sandboxes'), grants = useQuery<Grant[]>(`/api/grants?assistant_id=${assistant.ID}`, true), resources = useQuery<Resource[]>('/api/resources', true), models = useQuery<Model[]>('/api/models');
  const [creating, setCreating] = useState(false), [editing, setEditing] = useState<Grant | null>(null), [revoking, setRevoking] = useState<Grant | null>(null), [inspecting, setInspecting] = useState<Grant | null>(null);
  const active = (grants.data ?? []).filter(grant => !grant.RevokedAt && (!grant.ExpiresAt || Date.parse(grant.ExpiresAt) > Date.now()));
  useErrorToast(grants.error || resources.error || models.error, () => { grants.reload(); resources.reload(); models.reload(); });
  const revokeGrant = useMutation([revoking]);
  const [history, setHistory] = useState(false);
  return <>
    <div className="permissions-content">
      <PermissionRequests assistant={assistant} history={history} onHistoryChange={setHistory} />
      {!history && <>
      <AssistantDelegationPermissions assistant={assistant} models={models.data ?? []} />
      <section className="assistant-settings-section assistant-grants">
        <h3>{t.assistantInfo.activePermissions}</h3>
        <div className="assistant-info-section-heading"><span className="assistant-permission-meta">{grants.data && <span>{locale === 'zh' ? `${active.length} 项授权` : `${active.length} grants`}</span>}</span><Button size="sm" variant="secondary" disabled={assistant.State !== 'active' || !resources.data?.some(row => row.State === 'available' && row.Actions.length)} onClick={() => setCreating(true)}><Plus size={14} aria-hidden="true" />{t.resources.grant}</Button></div>
        {grants.loading && !grants.data ? <Loading /> : !active.length ? <Empty title={t.resources.noGrants} /> : <div className="assistant-grant-list">{active.map(grant => {
          const resource = resources.data?.find(row => row.ID === grant.ResourceID);
          const node = sandboxes.data?.find(s => s.ID === (resource?.SandboxID || 'local'));
          const nodeName = node?.Local ? copy.local : node?.Name;
          const permission = grant.Scope.FullAccess ? fullAccessLabel(locale === 'zh') : resource ? t.resources.categories[resource.Kind] : t.nav.resources;
          const target = isSandboxResource(resource?.Kind) && nodeName ? `${nodeName}${resource?.Kind === 'compute' ? '' : ` · ${resource?.Name}`}` : grantTarget(t, grant, resource, models.data ?? []);
          const Icon = resource ? { model: Bot, tool: Wrench, storage: Folder, compute: Server, connection: Globe, network: Globe }[resource.Kind] : Folder;
          return <article className="assistant-grant" key={grant.ID}>
            <div className="assistant-grant-heading">
              <span className="assistant-grant-icon"><Icon size={16} aria-hidden="true" /></span>
              <div className="assistant-grant-identity"><h4 className="assistant-grant-title"><button type="button" className="assistant-grant-name" onClick={() => setInspecting(grant)}>{target}</button></h4><p className="assistant-grant-category">{isSandboxResource(resource?.Kind) ? `${copy.title} · ${permission}` : permission}{resource?.Kind === 'model' && resource.Name !== target && <> · {resource.Name}</>}</p></div>
              <div className="assistant-grant-actions"><IconButton size="sm" label={t.edit} disabled={assistant.State !== 'active' || resource?.State !== 'available'} onClick={() => setEditing(grant)}><Pencil size={15} /></IconButton><DropdownMenu label={t.more} align="end" trigger={<IconButton size="sm" label={t.more}><Ellipsis size={16} /></IconButton>} items={[{ id: 'details', label: t.assistantInfo.permissionDetails, icon: <Info size={15} />, onSelect: () => setInspecting(grant) }, { id: 'revoke', label: t.resources.revoke, icon: <Unplug size={15} />, danger: true, disabled: assistant.State !== 'active', onSelect: () => setRevoking(grant) }]} /></div>
            </div>
            <dl className="assistant-info-facts assistant-grant-facts">
              <div><dt>{locale === 'zh' ? '允许操作' : 'Allowed actions'}</dt><dd>{grant.Scope.FullAccess ? fullAccessLabel(locale === 'zh') : grantSummary(t, grant, resource)}</dd></div>
              <div><dt>{locale === 'zh' ? '有效期' : 'Validity'}</dt><dd><PermissionExpiry value={grant.ExpiresAt} /></dd></div>
            </dl>
          </article>;
        })}</div>}
      </section>
      </>}
    </div>
    <ModalPresence>{inspecting && <Dialog open title={t.assistantInfo.permissionDetails} description={t.resourcePermissions.recipient(assistant.Name)} onOpenChange={open => { if (!open) setInspecting(null); }}>
      <div className="assistant-grant-details"><PermissionScope grant={inspecting} showExpiry />{!inspecting.Scope.FullAccess && resources.data?.some(resource => resource.ID === inspecting.ResourceID && !['model', 'tool'].includes(resource.Kind)) && <div className="assistant-grant-usage"><ResourceQuotaUsage assistantID={inspecting.AssistantID} resourceID={inspecting.ResourceID} /></div>}</div>
    </Dialog>}</ModalPresence>
    <ModalPresence>{creating && <GrantEditor assistant={assistant} resources={resources.data ?? []} onClose={() => setCreating(false)} />}</ModalPresence>
    <ModalPresence>{editing && <GrantEditor assistant={assistant} resources={resources.data ?? []} grant={editing} onClose={() => setEditing(null)} />}</ModalPresence>
    <ModalPresence>{revoking && <Confirm title={t.resources.revoke} description={t.resources.revokeHelp} danger onClose={() => setRevoking(null)} onConfirm={() => revokeGrant('/api/grants/revoke', { GrantID: revoking.ID }, revoking.Version)} />}</ModalPresence>
  </>;
}
