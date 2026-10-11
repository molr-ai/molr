import { GrantEditor } from './GrantEditor';
import { fullAccessLabel, isSandboxResource } from './PermissionTargetFields';
import { ResourceQuotaUsage } from './ResourceConstraints';
import { PermissionScope, PermissionExpiry } from './PermissionScope';
import { PermissionRequests } from './PermissionRequests';
import { useEffect, useState } from 'react';
import { Badge, Button, Card, Dialog, DropdownMenu, IconButton, Tabs } from '@molr/ui';
import { Bot, Ellipsis, Folder, Globe, Info, Pencil, Plus, Server, Trash2, Unplug, Wrench } from 'lucide-react';
import { SettingsHelp } from '../components/SettingsHelp';
import { ModelConfigurationDialog } from './ModelConfigurationDialog';
import { ProviderConnectionDialog } from './ProviderConnectionDialog';
import { Sandboxes } from './Sandboxes';
import { ToolsCatalog } from './ToolsCatalog';
import type { SandboxView } from './Sandboxes';
import { useSandboxPreference } from '../../../sandbox-navigation';
import { useSandboxCopy } from '../../../sandbox-copy';
import { useReauthentication } from '../components/ReauthenticationDialog';
import { api, APIError, mutationKey } from '../../../api/client';
import type { Assistant, Grant, Model, Resource } from '../../../api/types';
import { useCopy, useQuery } from '../../../state';
import { grantSummary, grantTarget } from '../../../resource-permissions';
import { Confirm, Empty, ErrorNotice, Loading, PageHeader, useAction } from '../components/common';
import './resources.css';
type Tab = 'model' | 'tool' | 'sandbox';
export function Resources({ resourceID = '' }: { resourceID?: string }) {
  const { t } = useCopy(), copy = useSandboxCopy(), query = useQuery<Resource[]>('/api/resources', true), models = useQuery<Model[]>('/api/models'), action = useAction();
  const [status, setStatus] = useState<Record<string, string>>({});
  const [tab, setTab] = useSandboxPreference<Tab>('resource-tab', 'model', value => ['model', 'tool', 'sandbox'].includes(value));
  const [expanded, setExpanded] = useState(''), [connect, setConnect] = useState(false);
  const [editingProvider, setEditingProvider] = useState<Resource | null>(null), [deletingProvider, setDeletingProvider] = useState<Resource | null>(null), [disconnect, setDisconnect] = useState<Resource | null>(null), [configuring, setConfiguring] = useState<{ resource: Resource; model?: Model } | null>(null);
  const target = query.data?.find(resource => resource.ID === resourceID);
  useEffect(() => { if (target) { setTab(target.Kind === 'model' || target.Kind === 'tool' ? target.Kind : 'sandbox'); setExpanded(target.ID); } }, [target?.ID, target?.Kind]);
  const providers = (query.data ?? []).filter(resource => resource.Kind === 'model');
  function configureModel(resource: Resource, model?: Model) { void action.run(async () => { const [current, catalog] = await Promise.all([api.get<Resource>(`/api/resources/${resource.ID}`), model ? api.get<Model[]>('/api/models') : Promise.resolve([])]); const selected = model ? catalog.find(item => item.ID === model.ID) : undefined; if (model && !selected) throw new APIError('not_found'); setConfiguring({ resource: current, model: selected }); }, undefined, false); }
  const modelContent = <><ErrorNotice error={query.error || models.error || action.error} retry={() => { query.reload(); models.reload(); }} />{query.loading && !query.data ? <Loading /> : !providers.length ? <div className="resources-model-empty"><span className="resources-model-empty-icon" aria-hidden="true"><Bot size={32} /></span><h2>{t.resources.emptyCategory.model}</h2><p>{t.resources.connectHelp.model}</p><Button size="sm" onClick={() => setConnect(true)}><Plus size={14} aria-hidden="true" />{t.resources.connectActions.model}</Button></div> : <div className="resource-list">{providers.map(resource => {
    const open = expanded === resource.ID, entries = (models.data ?? []).filter(model => model.ResourceID === resource.ID);
    return <article key={resource.ID} className="resource-row" data-selected={open || undefined}><div className="resource-row-summary"><span className="resource-row-icon"><Bot size={17} /></span><div className="resource-row-copy"><h2><button type="button" aria-expanded={open} onClick={() => setExpanded(open ? '' : resource.ID)}>{resource.Name}</button></h2><p className="resource-row-meta">{resource.Provider}</p></div><Badge tone={resource.State === 'available' ? 'positive' : 'neutral'}>{t.resources[resource.State as 'available' | 'offline' | 'disconnected']}</Badge><DropdownMenu label={t.more} align="end" trigger={<IconButton label={t.more}><Ellipsis size={18} /></IconButton>} items={[
      { id: 'edit', label: t.resources.editProvider, icon: <Pencil size={15} />, disabled: resource.State !== 'available', onSelect: () => setEditingProvider(resource) },
      ...(resource.State !== 'disconnected' ? [{ id: 'disconnect', label: t.resources.disconnect, icon: <Unplug size={15} />, onSelect: () => setDisconnect(resource) }] : []),
      { id: 'delete', label: t.resources.deleteProvider, icon: <Trash2 size={15} />, danger: true, onSelect: () => setDeletingProvider(resource) },
    ]} /></div>{open && <div className="resource-row-detail"><><ul className="resource-models-list">{entries.map(model => <li key={model.ID}><span>{model.Name} · {t.resources.contextWindow}: {(model.EffectiveContextLimit ?? 262144).toLocaleString()} tokens · {t.resources.contextSources[model.ContextLimitSource ?? "fallback"]} · {t.resources.outputWindow}: {(model.EffectiveOutputLimit ?? 32768).toLocaleString()} tokens · {t.resources.contextSources[model.OutputLimitSource ?? "fallback"]}</span>{!model.Pricing && <Badge tone="warning">{t.resources.pricingUnknown}</Badge>}<Button size="sm" variant="ghost" disabled={resource.State !== 'available'} onClick={() => configureModel(resource, model)}>{t.resources.configureModel}</Button></li>)}</ul>{!models.loading && !entries.length && <p className="muted">{t.resources.noDiscoveredModels}</p>}<div className="resource-row-actions"><Button size="sm" variant="ghost" disabled={resource.State !== 'available'} onClick={() => configureModel(resource)}><Plus size={14} />{t.resources.addModel}</Button><Button size="sm" variant="secondary" disabled={action.busy || resource.State === 'disconnected'} onClick={() => void action.run(() => api.mutate('/api/resources/refresh-models', { ID: resource.ID }, resource.Version), undefined, false)}>{t.resources.refreshModels}</Button><Button size="sm" variant="secondary" disabled={action.busy || resource.State === 'disconnected'} onClick={() => void action.run(async () => { const result = await api.post<{ State: string }>(`/api/resources/${resource.ID}/check`); setStatus(previous => ({ ...previous, [resource.ID]: result.State })); }, undefined, false)}>{t.resources.check}</Button>{status[resource.ID] && <p role="status">{t.resources[status[resource.ID] as 'available' | 'offline']}</p>}</div></></div>}</article>;
  })}</div>}</>;
  return <section className="resources-workspace"><PageHeader className="resources-header" title={t.nav.resources} icon={<Folder />} actions={tab === 'model' && <Button size="sm" onClick={() => setConnect(true)}><Plus size={15} />{t.resources.connectActions.model}</Button>} />
    <Tabs className="resources-categories" label={t.resources.categoriesLabel} value={tab} onValueChange={setTab} items={[
      { value: 'model', label: t.resources.categories.model, icon: <Bot size={15} />, content: tab === 'model' ? <div className="resources-master">{modelContent}</div> : null },
      { value: 'tool', label: t.resources.categories.tool, icon: <Wrench size={15} />, content: tab === 'tool' ? <ToolsCatalog /> : null },
      { value: 'sandbox', label: copy.title, icon: <Server size={15} />, content: tab === 'sandbox' ? <Sandboxes resources={query.data ?? []} resourceID={resourceID} /> : null },
    ]} />
    {connect && <ProviderConnectionDialog onConnected={() => setTab('model')} onClose={() => setConnect(false)} />}{editingProvider && <ProviderConnectionDialog resourceID={editingProvider.ID} onConnected={() => {}} onClose={() => setEditingProvider(null)} />}{deletingProvider && <DeleteProvider resource={deletingProvider} onClose={() => setDeletingProvider(null)} />}{configuring && <ModelConfigurationDialog resource={configuring.resource} model={configuring.model} onClose={() => setConfiguring(null)} />}{disconnect && <Confirm title={t.resources.disconnect} description={t.resources.disconnectHelp} danger onClose={() => setDisconnect(null)} onConfirm={() => api.mutate('/api/resources/disconnect', { ID: disconnect.ID }, disconnect.Version)} />}
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
  return <>
    <div className="permissions-content">
      <section><div className="assistant-info-section-heading"><h3>{copy.workspace}<SettingsHelp label={copy.workspace} content={copy.workspaceHelp} /></h3></div><p className="muted">/work · {copy.local}</p></section>
      <PermissionRequests assistant={assistant} />
      <section className="assistant-grants">
        <div className="assistant-info-section-heading"><h3>{t.assistantInfo.activePermissions}{grants.data && <Badge size="sm" dot={false}>{active.length}</Badge>}<SettingsHelp label={t.assistantInfo.activePermissions} content={t.resources.accessHint} /></h3><Button size="sm" disabled={assistant.State !== 'active' || !resources.data?.some(row => row.State === 'available' && row.Actions.length)} onClick={() => setCreating(true)}><Plus size={14} aria-hidden="true" />{t.resources.grant}</Button></div>
        <ErrorNotice error={grants.error || resources.error || models.error} retry={() => { grants.reload(); resources.reload(); models.reload(); }} />
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
    {inspecting && <Dialog open title={t.assistantInfo.permissionDetails} description={t.resourcePermissions.recipient(assistant.Name)} onOpenChange={open => { if (!open) setInspecting(null); }}>
      <div className="assistant-grant-details"><PermissionScope grant={inspecting} showExpiry />{!inspecting.Scope.FullAccess && resources.data?.some(resource => resource.ID === inspecting.ResourceID && !['model', 'tool'].includes(resource.Kind)) && <div className="assistant-grant-usage"><ResourceQuotaUsage assistantID={inspecting.AssistantID} resourceID={inspecting.ResourceID} /></div>}</div>
    </Dialog>}
    {creating && <GrantEditor assistant={assistant} resources={resources.data ?? []} onClose={() => setCreating(false)} />}
    {editing && <GrantEditor assistant={assistant} resources={resources.data ?? []} grant={editing} onClose={() => setEditing(null)} />}
    {revoking && <Confirm title={t.resources.revoke} description={t.resources.revokeHelp} danger onClose={() => setRevoking(null)} onConfirm={() => api.mutate('/api/grants/revoke', { GrantID: revoking.ID }, revoking.Version)} />}
  </>;
}
