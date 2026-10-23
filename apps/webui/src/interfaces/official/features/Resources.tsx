import { useErrorToast } from '../../../error-toasts';
import { GrantEditor } from './GrantEditor';
import { fullAccessLabel, isSandboxResource } from './PermissionTargetFields';
import { ResourceQuotaUsage } from './ResourceConstraints';
import { PermissionScope, PermissionExpiry } from './PermissionScope';
import { PermissionRequests } from './PermissionRequests';
import { useEffect, useState } from 'react';
import { ModalPresence, Badge, Button, Card, Dialog, DropdownMenu, IconButton, Tabs, useToast } from '@molr/ui';
import { BookOpen, Bot, Ellipsis, Folder, Globe, Info, Pencil, Plus, RefreshCw, Server, Trash2, Unplug, Wifi, Wrench } from 'lucide-react';
import { SettingsHelp } from '../components/SettingsHelp';
import { DefaultModelSettings } from './DefaultModelSettings';
import { ProviderModelsDrawer } from './ProviderModelsDrawer';
import { ProviderConnectionDialog } from './ProviderConnectionDialog';
import { Sandboxes } from './Sandboxes';
import { ToolsCatalog } from './ToolsCatalog';
import { SkillsCatalog } from './SkillsCatalog';
import type { SandboxView } from './Sandboxes';
import { useSandboxPreference } from '../../../sandbox-navigation';
import { useSandboxCopy } from '../../../sandbox-copy';
import { useReauthentication } from '../components/ReauthenticationDialog';
import { api, mutationKey } from '../../../api/client';
import type { Assistant, Grant, Model, Resource, PricingRefreshResult } from '../../../api/types';
import { useCopy, useQuery, useSession } from '../../../state';
import { grantSummary, grantTarget } from '../../../resource-permissions';
import { Confirm, Empty, Loading, PageHeader, useAction } from '../components/common';
import './resources.css';
type Tab = 'model' | 'tool' | 'skill' | 'sandbox';
export function Resources({ resourceID = '' }: { resourceID?: string }) {
  const { t } = useCopy(), copy = useSandboxCopy(), query = useQuery<Resource[]>('/api/resources', true), models = useQuery<Model[]>('/api/models'), action = useAction();
  const { principal } = useSession(), { toast } = useToast(), pricingAction = useAction();
  const refreshPrices = () => void pricingAction.run(async () => {
    const result = await api.post<PricingRefreshResult>('/api/models/refresh-pricing');
    models.reload();
    toast({ title: result.Source === 'builtin' ? t.resources.pricingBuiltin : result.Updated ? t.resources.pricingUpdated : t.resources.pricingUnchanged });
  }, undefined, false);
  useErrorToast(pricingAction.error, refreshPrices);
  const [status, setStatus] = useState<Record<string, string>>({});
  const [tab, setTab] = useSandboxPreference<Tab>('resource-tab', 'model', value => ['model', 'tool', 'skill', 'sandbox'].includes(value));
  const [selectedProvider, setSelectedProvider] = useState<{ id: string; addModel?: boolean } | null>(null), [connect, setConnect] = useState(false), [defaultModel, setDefaultModel] = useState(false);
  const [editingProvider, setEditingProvider] = useState<Resource | null>(null), [deletingProvider, setDeletingProvider] = useState<Resource | null>(null), [disconnect, setDisconnect] = useState<Resource | null>(null);
  const target = query.data?.find(resource => resource.ID === resourceID);
  useEffect(() => { if (target) { setTab(target.Kind === 'model' || target.Kind === 'tool' ? target.Kind : 'sandbox'); if (target.Kind === 'model') setSelectedProvider({ id: target.ID }); } }, [target?.ID, target?.Kind]);
  const providers = (query.data ?? []).filter(resource => resource.Kind === 'model');
  const provider = providers.find(resource => resource.ID === selectedProvider?.id);
  useErrorToast(query.error || models.error || action.error, () => { query.reload(); models.reload(); });
  const modelContent = <>{query.loading && !query.data ? <Loading /> : !providers.length ? <div className="resources-model-empty"><span className="resources-model-empty-icon" aria-hidden="true"><Bot size={32} /></span><h2>{t.resources.emptyCategory.model}</h2><p>{t.resources.connectHelp.model}</p><Button size="sm" onClick={() => setConnect(true)}><Plus size={14} aria-hidden="true" />{t.resources.connectActions.model}</Button></div> : <div className="resource-list">{providers.map(resource => {
    const open = selectedProvider?.id === resource.ID, connectionState = status[resource.ID] || resource.State;
    return <article key={resource.ID} className="resource-row interactive-list-row" data-selected={open || undefined}><div className="resource-row-summary"><span className="resource-row-icon"><Bot size={17} /></span><div className="resource-row-copy"><h2><button className="row-open" type="button" aria-haspopup="dialog" onClick={() => setSelectedProvider({ id: resource.ID })}>{resource.Name}</button></h2><p className="resource-row-meta">{resource.Authentication === 'chatgpt' ? 'OpenAI · ChatGPT' : resource.Provider}</p></div><Badge role={status[resource.ID] ? 'status' : undefined} tone={connectionState === 'available' ? 'positive' : 'neutral'}>{t.resources[connectionState as 'available' | 'offline' | 'disconnected']}</Badge><DropdownMenu label={t.more} align="end" trigger={<IconButton label={t.more}><Ellipsis size={18} /></IconButton>} items={[
      { id: 'add-model', label: t.resources.addModel, icon: <Plus size={15} />, disabled: resource.State !== 'available', onSelect: () => setSelectedProvider({ id: resource.ID, addModel: true }) },
      { id: 'refresh-models', label: t.resources.refreshModels, icon: <RefreshCw size={15} />, disabled: action.busy || resource.State === 'disconnected', onSelect: () => void action.run(() => api.mutate('/api/resources/refresh-models', { ID: resource.ID }, resource.Version), undefined, false) },
      { id: 'check', label: t.resources.check, icon: <Wifi size={15} />, disabled: action.busy || resource.State === 'disconnected', onSelect: () => void action.run(async () => { const result = await api.post<{ State: string }>(`/api/resources/${resource.ID}/check`); setStatus(previous => ({ ...previous, [resource.ID]: result.State })); }, undefined, false) },
      { id: 'edit', label: t.resources.editProvider, icon: <Pencil size={15} />, disabled: resource.State !== 'available' && resource.Authentication !== 'chatgpt', onSelect: () => setEditingProvider(resource) },
      ...(resource.State !== 'disconnected' ? [{ id: 'disconnect', label: t.resources.disconnect, icon: <Unplug size={15} />, onSelect: () => resource.Authentication === 'chatgpt' ? setEditingProvider(resource) : setDisconnect(resource) }] : []),
      { id: 'delete', label: t.resources.deleteProvider, icon: <Trash2 size={15} />, danger: true, onSelect: () => resource.Authentication === 'chatgpt' && resource.State !== 'disconnected' ? setEditingProvider(resource) : setDeletingProvider(resource) },
    ]} /></div></article>;
  })}</div>}</>;
  return <section className="resources-workspace"><PageHeader className="resources-header" title={t.nav.resources} icon={<Folder />} actions={tab === 'model' && <DropdownMenu label={t.more} align="end" trigger={<IconButton label={t.more}><Ellipsis size={18} /></IconButton>} items={[{ id: 'connect-provider', label: t.resources.connectActions.model, onSelect: () => setConnect(true) }, { id: 'default-model', label: t.resources.defaultModel, onSelect: () => setDefaultModel(true) }, ...(principal?.User?.Administrator ? [{ id: 'refresh-pricing', label: t.resources.refreshPricing, icon: <RefreshCw size={15} />, disabled: pricingAction.busy, onSelect: refreshPrices }] : [])]} />} />
    <Tabs className="resources-categories" label={t.resources.categoriesLabel} value={tab} onValueChange={setTab} items={[
      { value: 'model', label: t.resources.categories.model, icon: <Bot size={15} />, content: tab === 'model' ? <div className="resources-master">{modelContent}</div> : null },
      { value: 'tool', label: t.resources.categories.tool, icon: <Wrench size={15} />, content: tab === 'tool' ? <ToolsCatalog /> : null },
      { value: 'skill', label: 'Skills', icon: <BookOpen size={15} />, content: tab === 'skill' ? <SkillsCatalog /> : null },
      { value: 'sandbox', label: copy.title, icon: <Server size={15} />, content: tab === 'sandbox' ? <Sandboxes resources={query.data ?? []} resourceID={resourceID} /> : null },
    ]} />
    <ModalPresence>{tab === 'model' && selectedProvider && provider && <ProviderModelsDrawer key={provider.ID} resource={provider} models={(models.data ?? []).filter(model => model.ResourceID === selectedProvider.id)} loading={models.loading && !models.data} addModel={selectedProvider.addModel} onClose={() => setSelectedProvider(null)} />}</ModalPresence>
    <ModalPresence>{defaultModel && <DefaultModelSettings models={models.data ?? []} loading={models.loading} onClose={() => setDefaultModel(false)} />}</ModalPresence>
    <ModalPresence>{connect && <ProviderConnectionDialog onConnected={() => setTab('model')} onClose={() => setConnect(false)} />}</ModalPresence><ModalPresence>{editingProvider && <ProviderConnectionDialog resourceID={editingProvider.ID} onConnected={() => {}} onClose={() => setEditingProvider(null)} />}</ModalPresence><ModalPresence>{deletingProvider && <DeleteProvider resource={deletingProvider} onClose={() => setDeletingProvider(null)} />}</ModalPresence><ModalPresence>{disconnect && <Confirm title={t.resources.disconnect} description={t.resources.disconnectHelp} danger onClose={() => setDisconnect(null)} onConfirm={() => api.mutate('/api/resources/disconnect', { ID: disconnect.ID }, disconnect.Version)} />}</ModalPresence>
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
  return <>
    <div className="permissions-content">
      <section><div className="assistant-info-section-heading"><h3>{copy.workspace}<SettingsHelp label={copy.workspace} content={copy.workspaceHelp} /></h3></div><p className="muted">/work · {copy.local}</p></section>
      <PermissionRequests assistant={assistant} />
      <section className="assistant-grants">
        <div className="assistant-info-section-heading"><h3>{t.assistantInfo.activePermissions}{grants.data && <Badge size="sm" dot={false}>{active.length}</Badge>}<SettingsHelp label={t.assistantInfo.activePermissions} content={t.resources.accessHint} /></h3><Button size="sm" disabled={assistant.State !== 'active' || !resources.data?.some(row => row.State === 'available' && row.Actions.length)} onClick={() => setCreating(true)}><Plus size={14} aria-hidden="true" />{t.resources.grant}</Button></div>
        {grants.loading && !grants.data ? <Loading /> : !active.length ? <Empty title={t.resources.noGrants} /> : <div className="assistant-grant-list">{active.map(grant => {
          const resource = resources.data?.find(row => row.ID === grant.ResourceID);
          const node = sandboxes.data?.find(s => s.ID === (resource?.SandboxID || 'local'));
          const nodeName = node?.Local ? copy.local : node?.Name;
          const permission = grant.Scope.FullAccess ? fullAccessLabel(locale === 'zh') : resource ? t.resources.categories[resource.Kind] : t.nav.resources;
          const target = isSandboxResource(resource?.Kind) && nodeName ? `${nodeName}${resource?.Kind === 'compute' ? '' : ` · ${resource?.Name}`}` : grantTarget(t, grant, resource, models.data ?? []);
          const Icon = resource ? { model: Bot, tool: Wrench, storage: Folder, compute: Server, connection: Globe, network: Globe }[resource.Kind] : Folder;
          return <Card className="assistant-grant" key={grant.ID}>
            <div className="assistant-grant-heading">
              <span className="assistant-grant-icon"><Icon size={16} aria-hidden="true" /></span>
              <h3 className="assistant-grant-title">{isSandboxResource(resource?.Kind) ? `${copy.title} · ${permission}` : permission}</h3>
              <div className="assistant-grant-actions"><IconButton size="sm" label={t.edit} disabled={assistant.State !== 'active' || resource?.State !== 'available'} onClick={() => setEditing(grant)}><Pencil size={15} /></IconButton><DropdownMenu label={t.more} align="end" trigger={<IconButton size="sm" label={t.more}><Ellipsis size={16} /></IconButton>} items={[{ id: 'details', label: t.assistantInfo.permissionDetails, icon: <Info size={15} />, onSelect: () => setInspecting(grant) }, { id: 'revoke', label: t.resources.revoke, icon: <Unplug size={15} />, danger: true, disabled: assistant.State !== 'active', onSelect: () => setRevoking(grant) }]} /></div>
            </div>
            <dl className="assistant-info-facts assistant-grant-facts">
              <div><dt>{t.resources.expires}</dt><dd><PermissionExpiry value={grant.ExpiresAt} /></dd></div>
            </dl>
            <div className="assistant-grant-resource"><span className="assistant-grant-resource-name"><button type="button" className="assistant-grant-name" onClick={() => setInspecting(grant)}>{target}</button>{resource?.Kind === 'model' && resource.Name !== target && <span className="assistant-grant-provider"> · {resource.Name}</span>}</span><span className="assistant-grant-allowed">{grant.Scope.FullAccess ? fullAccessLabel(locale === 'zh') : grantSummary(t, grant, resource)}</span></div>

          </Card>;
        })}</div>}
      </section>
    </div>
    <ModalPresence>{inspecting && <Dialog open title={t.assistantInfo.permissionDetails} description={t.resourcePermissions.recipient(assistant.Name)} onOpenChange={open => { if (!open) setInspecting(null); }}>
      <div className="assistant-grant-details"><PermissionScope grant={inspecting} showExpiry />{!inspecting.Scope.FullAccess && resources.data?.some(resource => resource.ID === inspecting.ResourceID && !['model', 'tool'].includes(resource.Kind)) && <div className="assistant-grant-usage"><ResourceQuotaUsage assistantID={inspecting.AssistantID} resourceID={inspecting.ResourceID} /></div>}</div>
    </Dialog>}</ModalPresence>
    <ModalPresence>{creating && <GrantEditor assistant={assistant} resources={resources.data ?? []} onClose={() => setCreating(false)} />}</ModalPresence>
    <ModalPresence>{editing && <GrantEditor assistant={assistant} resources={resources.data ?? []} grant={editing} onClose={() => setEditing(null)} />}</ModalPresence>
    <ModalPresence>{revoking && <Confirm title={t.resources.revoke} description={t.resources.revokeHelp} danger onClose={() => setRevoking(null)} onConfirm={() => api.mutate('/api/grants/revoke', { GrantID: revoking.ID }, revoking.Version)} />}</ModalPresence>
  </>;
}
