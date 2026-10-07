import { useState } from 'react';
import { DateTimePicker, Select, Textarea } from '@molr/ui';
import type { Assistant, Grant, Model, Resource, ResourceConstraints, TemporaryConstraints } from '../../../api/types';
import type { SandboxView } from '../../../sandbox-types';
import { api, APIError } from '../../../api/client';
import { useCopy, useQuery } from '../../../state';
import { useSandboxCopy } from '../../../sandbox-copy';
import { defaultPermissions, permissionOptions } from '../../../resource-permissions';
import { EditDialog, ErrorNotice, lines } from '../components/common';
import { ConstraintsEditor, suggestedConstraints, TemporaryConstraintsEditor } from './ResourceConstraints';
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
  function select(next: PermissionTarget) {
    if (next.resourceID !== current.resourceID || next.fullAccess !== current.fullAccess) {
      const resource = resources.find(r => r.ID === next.resourceID);
      setActions(next.fullAccess ? ['execute'] : initialActions(resource));
      setConstraints(next.fullAccess ? undefined : initialConstraints(resource)); setTemporary(undefined); setPaths('');
    }
    setTarget(next);
  }
  return <EditDialog title={grant ? t.assistantInfo.editPermission : assistant ? t.resources.grant : copy.grant} description={assistant ? t.resourcePermissions.recipient(assistant.Name) : undefined} onClose={onClose} submitDisabled={!assistantID || !resource || !actions.length || !!sandboxes.error || !!models.error} onSubmit={async () => {
    if (!resource || resource.State !== 'available' || !assistantID || !actions.length || actions.some(a => !resource.Actions.includes(a)) || (expires && Date.parse(expires) <= Date.now()) || (resource.Kind === 'model' && !models.data?.some(m => m.ID === target.modelID && m.ResourceID === resource.ID))) throw new APIError('invalid_argument');
    const values = { Actions: target.fullAccess ? ['execute'] : actions, Scope: { FullAccess: target.fullAccess, ModelID: target.kind === 'model' ? target.modelID : '', ReadOnly: !target.fullAccess && actions.every(a => a === 'read'), PathPrefixes: target.kind === 'storage' ? lines(paths) : [] }, Constraints: target.fullAccess ? undefined : constraints, Temporary: target.fullAccess ? undefined : temporary, ExpiresAt: expires };
    if (grant) await api.mutate('/api/grants/update', { GrantID: grant.ID, ...values }, grant.Version);
    else await api.mutate('/api/grants', { AssistantID: assistantID, ResourceID: resource.ID, ...values });
  }}>
    {!assistant && <Select label={copy.assistant} disabled={!!grant} value={assistantID} onValueChange={setAssistantID} options={(assistants ?? []).map(a => ({ value: a.ID, label: a.Name }))} />}
    <PermissionTargetFields value={current} onChange={select} resources={resources} models={models.data ?? []} sandboxes={sandboxes.data ?? []} lockedResource={!!grant} sandboxOnly={!!initialResource} />
    <ErrorNotice error={sandboxes.error || models.error} retry={() => { sandboxes.reload(); models.reload(); }} />
    {!target.fullAccess && <>
      <Select multiple label={t.resources.actions} value={actions} onValueChange={setActions} options={permissionOptions(t, resource)} />
      {target.kind === 'storage' && <Textarea label={t.resources.pathPrefixes} description={copy.pathsHelp} value={paths} onChange={e => setPaths(e.target.value)} placeholder="/" rows={3} />}
      <ConstraintsEditor kind={target.kind} value={constraints} onChange={setConstraints} lockNetworkPeriod={!!grant?.Constraints?.network} />
      <TemporaryConstraintsEditor kind={target.kind} base={constraints} value={temporary} onChange={setTemporary} />
    </>}
    <DateTimePicker size="sm" label={t.resources.expires} value={expires} onValueChange={setExpires} min={new Date().toISOString()} />
  </EditDialog>;
}
