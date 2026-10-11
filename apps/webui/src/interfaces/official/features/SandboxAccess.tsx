import type { SandboxView } from '../../../sandbox-types';
import { GrantEditor } from './GrantEditor';
import { fullAccessLabel } from './PermissionTargetFields';
import { ConstraintsSummary, ResourceQuotaUsage } from './ResourceConstraints';
import { useState } from 'react';
import { Button, Dialog } from '@molr/ui';
import type { Assistant, Grant, Resource } from '../../../api/types';
import { api } from '../../../api/client';
import { useCopy, useQuery } from '../../../state';
import { useSandboxCopy } from '../../../sandbox-copy';
import { grantSummary } from '../../../resource-permissions';
import { Confirm, Empty, ErrorNotice, Loading } from '../components/common';

export function activeResourceGrants(grants: Grant[], assistants: Assistant[]) {
  const active = new Set(assistants.filter(item => item.State === 'active').map(item => item.ID));
  return grants.filter(grant => active.has(grant.AssistantID) && !grant.RevokedAt && (!grant.ExpiresAt || Date.parse(grant.ExpiresAt) > Date.now()));
}
export function SandboxAccess({ resource, resources, fullAccess = false, canGrant, onClose }: { resource: Resource; resources: Resource[]; fullAccess?: boolean; canGrant: boolean; onClose: () => void }) {
  const { t, locale } = useCopy(), copy = useSandboxCopy();
  const sandboxes = useQuery<SandboxView[]>('/api/sandboxes');
  const gpus = sandboxes.data?.find(node => node.ID === (resource.SandboxID || 'local'))?.Info.GPUs;
  const assistants = useQuery<Assistant[]>('/api/assistants', true), grants = useQuery<Grant[]>(`/api/grants?resource_id=${resource.ID}`, true);
  const [editing, setEditing] = useState<Grant | 'new' | null>(null), [revoking, setRevoking] = useState<Grant | null>(null);
  const active = activeResourceGrants(grants.data ?? [], assistants.data ?? []).filter(g => !!g.Scope.FullAccess === fullAccess);
  const people = (assistants.data ?? []).filter(item => item.State === 'active');
  return <Dialog open title={`${copy.access} · ${fullAccess ? fullAccessLabel(locale === 'zh') : resource.Name}`} description={copy.manageHint} onOpenChange={open => { if (!open && !editing && !revoking) onClose(); }}>
    <div className="sandbox-access">
      <ErrorNotice error={assistants.error || grants.error} retry={() => { assistants.reload(); grants.reload(); }} />
      {(assistants.loading && !assistants.data) || (grants.loading && !grants.data) ? <Loading /> : <>
        <div className="sandbox-access-heading"><span>{copy.authorized} · {new Set(active.map(grant => grant.AssistantID)).size}</span><Button size="sm" disabled={!canGrant || !people.length} onClick={() => setEditing('new')}>{copy.grant}</Button></div>
        {!people.length && <p className="muted">{copy.noAssistants}</p>}
        {!active.length ? <Empty title={copy.noAccess} description={copy.accessEmptyHelp} /> : active.map(grant => <article className="sandbox-access-row" key={grant.ID}>
          <div><h3>{people.find(person => person.ID === grant.AssistantID)?.Name ?? grant.AssistantID}</h3><p>{grant.Scope.FullAccess ? fullAccessLabel(locale === 'zh') : grantSummary(t, grant, resource)}</p>
            {resource.Kind === 'storage' && <p className="sandbox-path">{grant.Scope.PathPrefixes?.join(', ') || '/'}</p>}
            <ConstraintsSummary gpus={gpus} value={grant.Constraints} temporary={grant.Temporary}/>{!grant.Scope.FullAccess && resource.Kind !== 'model' && resource.Kind !== 'tool' && <ResourceQuotaUsage assistantID={grant.AssistantID} resourceID={grant.ResourceID}/>}<small className="muted">{grant.ExpiresAt ? `${t.resources.expires} · ${new Date(grant.ExpiresAt).toLocaleString(locale)}` : copy.noExpiry}</small></div>
          <div className="sandbox-row-actions"><Button size="sm" variant="ghost" disabled={!canGrant} onClick={() => setEditing(grant)}>{t.edit}</Button><Button size="sm" variant="ghost" onClick={() => setRevoking(grant)}>{t.resources.revoke}</Button></div>
        </article>)}
      </>}
    </div>
    {editing && <GrantEditor resources={resources} initialResource={resource} initialFullAccess={fullAccess} assistants={people} grant={editing === 'new' ? undefined : editing} onClose={() => setEditing(null)} />}
    {revoking && <Confirm title={t.resources.revoke} description={t.resources.revokeHelp} danger onClose={() => setRevoking(null)} onConfirm={() => api.mutate('/api/grants/revoke', { GrantID: revoking.ID }, revoking.Version)} />}
  </Dialog>;
}
