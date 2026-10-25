import { useMutation } from '../../../use-mutation';
import { usesNetworkToggle } from '../../../sandbox-types';
import { useErrorToast } from '../../../error-toasts';
import { useEffect, useState } from 'react';
import { DateTimePicker, Select, Textarea } from '@molr/ui';
import type { Assistant, Grant, Model, Resource, ResourceConstraints, TemporaryConstraints } from '@molr/sdk';
import type { SandboxView } from '../../../sandbox-types';
import { APIError } from '@molr/sdk';
import { useCopy, useQuery } from '../../../state';
import { useSandboxCopy } from '../../../sandbox-copy';
import { defaultPermissions, permissionOptions } from '../../../resource-permissions';
import { EditDialog, lines } from '../components/common';
import { ConstraintsEditor, constraintsForNode, suggestedConstraints, TemporaryConstraintsEditor } from './ResourceConstraints';
import { PermissionTargetFields, targetFor } from './PermissionTargetFields';
import type { PermissionTarget } from './PermissionTargetFields';

export const initialConstraints = (resource?: Resource) => resource?.Kind === 'storage' && resource.Provider !== 'managed-workspace' ? undefined : suggestedConstraints(resource?.Kind ?? '');
export const initialActions = (resource?: Resource) => resource?.Actions.includes('read') ? defaultPermissions(resource) : resource?.Actions.includes('execute') ? ['execute'] : [];

export function GrantEditor({ assistant, assistants, resources, grant, initialResource, initialFullAccess = false, onClose }: {
  assistant?: Assistant; assistants?: Assistant[]; resources: Resource[]; grant?: Grant; initialResource?: Resource; initialFullAccess?: boolean; onClose: () => void;
}) {
  const { t } = useCopy(), copy = useSandboxCopy();
  const sandboxes = useQuery<SandboxView[]>('/api/sandboxes'), models = useQuery<Model[]>('/api/models');
  const first = resources.find(r => r.ID === grant?.ResourceID) ?? initialResource ?? resources.find(r => r.Kind === 'model' && r.State === 'available') ?? resources.find(r => r.State === 'available');
  const [target, setTarget] = useState(() => targetFor(first, grant?.Scope.FullAccess ?? initialFullAccess, grant?.Scope.ModelID || assistant?.ModelID || ''));
  const [assistantID, setAssistantID] = useState(grant?.AssistantID ?? assistant?.ID ?? assistants?.[0]?.ID ?? '');
  const [actions, setActions] = useState(grant?.Actions ?? initialActions(first));
  const [constraints, setConstraints] = useState<ResourceConstraints | undefined>(grant?.Constraints ?? initialConstraints(first));
  const [temporary, setTemporary] = useState<TemporaryConstraints | undefined>(grant?.Temporary);
  const [paths, setPaths] = useState(grant?.Scope.PathPrefixes?.join('\n') ?? ''), [expires, setExpires] = useState<string | null>(grant?.ExpiresAt ?? null);
  const modelResourceID = target.kind === 'model' ? models.data?.find(m => m.ID === target.modelID)?.ResourceID : undefined;
  const current = modelResourceID ? { ...target, resourceID: modelResourceID } : target;
  const resource = resources.find(r => r.ID === current.resourceID);
  const node=sandboxes.data?.find(s=>s.ID===target.sandboxID)?.Info;
  useEffect(()=>{ if(usesNetworkToggle(node?.Capabilities.NetworkBackend)) {setConstraints(c=>constraintsForNode(target.kind,c,node));setTemporary(v=>{if(!v)return v;const limits=constraintsForNode(target.kind,v.limits,node);return limits?{...v,limits}:undefined;});} },[node?.Capabilities.NetworkBackend,target.kind,target.resourceID]);
  function select(next: PermissionTarget) {
    if (next.resourceID !== current.resourceID || next.fullAccess !== current.fullAccess) {
      const resource = resources.find(r => r.ID === next.resourceID);
      setActions(next.fullAccess ? ['execute'] : initialActions(resource));
      setConstraints(next.fullAccess ? undefined : initialConstraints(resource)); setTemporary(undefined); setPaths('');
    }
    setTarget(next);
  }
  useErrorToast(sandboxes.error || models.error, () => { sandboxes.reload(); models.reload(); });
  const saveGrant = useMutation([grant?.ID, assistantID, resource?.ID, target, actions, constraints, temporary, paths, expires]);
  return <EditDialog title={grant ? t.assistantInfo.editPermission : assistant ? t.resources.grant : copy.grant} description={assistant ? t.resourcePermissions.recipient(assistant.Name) : undefined} onClose={onClose} submitDisabled={!assistantID || !resource || !actions.length || !sandboxes.data || !!sandboxes.error || !!models.error} onSubmit={async () => {
    if (!resource || resource.State !== 'available' || !assistantID || !actions.length || actions.some(a => !resource.Actions.includes(a)) || (expires && Date.parse(expires) <= Date.now()) || (resource.Kind === 'model' && !models.data?.some(m => m.ID === target.modelID && m.ResourceID === resource.ID))) throw new APIError('invalid_argument');
    const values = { Actions: target.fullAccess ? ['execute'] : actions, Scope: { FullAccess: target.fullAccess, ModelID: target.kind === 'model' ? target.modelID : '', ReadOnly: !target.fullAccess && actions.every(a => a === 'read'), PathPrefixes: target.kind === 'storage' ? lines(paths) : [] }, Constraints: target.fullAccess ? undefined : constraints, Temporary: target.fullAccess ? undefined : temporary, ExpiresAt: expires };
    if (grant) await saveGrant('/api/grants/update', { GrantID: grant.ID, ...values }, grant.Version);
    else await saveGrant('/api/grants', { AssistantID: assistantID, ResourceID: resource.ID, ...values });
  }}>
    {!assistant && <Select label={copy.assistant} disabled={!!grant} value={assistantID} onValueChange={setAssistantID} options={(assistants ?? []).map(a => ({ value: a.ID, label: a.Name }))} />}
    <PermissionTargetFields value={current} onChange={select} resources={resources} models={models.data ?? []} sandboxes={sandboxes.data ?? []} lockedResource={!!grant} sandboxOnly={!!initialResource} />
    {!target.fullAccess && <>
      <Select multiple label={t.resources.actions} value={actions} onValueChange={setActions} options={permissionOptions(t, resource)} />
      {target.kind === 'storage' && <Textarea label={t.resources.pathPrefixes} description={copy.pathsHelp} value={paths} onChange={e => setPaths(e.target.value)} placeholder="/" rows={3} />}
      {target.kind==='network'&&usesNetworkToggle(node?.Capabilities.NetworkBackend)&&<p className="muted">{node?.Capabilities.NetworkBackend === 'appcontainer-toggle' ? copy.windowsNetworkHelp : copy.nativeNetworkHelp}</p>}
      <ConstraintsEditor gpuAccessMode={sandboxes.data?.find(s=>s.ID===target.sandboxID)?.Info.Capabilities.GPUAccessMode} gpus={sandboxes.data?.find(s=>s.ID===target.sandboxID)?.Info.GPUs} kind={target.kind} value={constraints} onChange={setConstraints} lockNetworkPeriod={!!grant?.Constraints?.network} />
      <TemporaryConstraintsEditor gpuAccessMode={sandboxes.data?.find(s=>s.ID===target.sandboxID)?.Info.Capabilities.GPUAccessMode} gpus={sandboxes.data?.find(s=>s.ID===target.sandboxID)?.Info.GPUs} kind={target.kind} base={constraints} value={temporary} onChange={setTemporary} />
    </>}
    <DateTimePicker size="sm" label={t.resources.expires} value={expires} onValueChange={setExpires} min={new Date().toISOString()} />
  </EditDialog>;
}
