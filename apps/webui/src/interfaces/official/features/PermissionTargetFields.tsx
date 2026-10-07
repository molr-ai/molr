import { Select } from '@molr/ui';
import type { Model, Resource } from '../../../api/types';
import type { SandboxView } from '../../../sandbox-types';
import { fullAccessAvailable, fullAccessHelp } from '../../../full-access';
import { useCopy } from '../../../state';
import { useSandboxCopy } from '../../../sandbox-copy';
import { ModelSelect } from '../components/ModelSelect';

export type SandboxPermission = 'storage' | 'compute' | 'network' | 'full_access';
export const sandboxPermissions: SandboxPermission[] = ['storage', 'compute', 'network', 'full_access'];
export const isSandboxResource = (kind?: Resource['Kind']) => kind === 'storage' || kind === 'compute' || kind === 'network';
export const fullAccessLabel = (zh: boolean) => zh ? '完全访问' : 'Full access';
export type PermissionTarget = { kind: Resource['Kind']; resourceID: string; sandboxID: string; modelID: string; fullAccess: boolean };
export function targetFor(resource?: Resource, fullAccess = false, modelID = ''): PermissionTarget {
  return { kind: resource?.Kind ?? 'model', resourceID: resource?.ID ?? '', sandboxID: resource?.SandboxID || 'local', modelID, fullAccess };
}

/** Presentation hierarchy; the API resource ID remains the authority for each grant. */
export function PermissionTargetFields({ value, resources, models, sandboxes, onChange, lockedResource = false, sandboxOnly = false }: {
  value: PermissionTarget; resources: Resource[]; models: Model[]; sandboxes: SandboxView[]; onChange: (value: PermissionTarget) => void; lockedResource?: boolean; sandboxOnly?: boolean;
}) {
  const { t, locale } = useCopy(), copy = useSandboxCopy(), zh = locale === 'zh';
  const available = resources.filter(r => r.State === 'available' && r.Actions.length);
  const selected = resources.find(r => r.ID === value.resourceID);
  const sandbox = isSandboxResource(value.kind), category = sandbox ? 'sandbox' : value.kind;
  const permission: SandboxPermission = value.fullAccess ? 'full_access' : value.kind as SandboxPermission;
  const nodeID = value.sandboxID || selected?.SandboxID || 'local';
  const matching = (kind: Resource['Kind'], id: string) => available.filter(r => r.Kind === kind && (r.SandboxID || 'local') === id);
  const candidates = sandbox ? matching(value.kind, nodeID) : available.filter(r => r.Kind === value.kind);
  function choose(kind: Resource['Kind'], id: string, full = false) {
    const rows = isSandboxResource(kind) ? matching(kind, id) : available.filter(r => r.Kind === kind);
    const resource = lockedResource ? selected : rows[0];
    const modelID = kind === 'model' ? models.find(m => m.ResourceID === resource?.ID)?.ID ?? '' : '';
    onChange({ ...targetFor(resource, full, modelID), kind, sandboxID: id });
  }
  const nodeOptions = sandboxes.map(s => ({ value: s.ID, label: s.Local ? copy.local : s.Name }));
  if (!nodeOptions.some(s => s.value === nodeID)) nodeOptions.push({ value: nodeID, label: nodeID === 'local' ? copy.local : nodeID });
  return <>
    <Select label={t.resourcePermissions.resourceType} value={category} disabled={lockedResource || sandboxOnly} onValueChange={next => {
      if (next === 'sandbox') {
        const first = available.find(r => isSandboxResource(r.Kind));
        onChange(first ? targetFor(first) : { ...value, kind: 'storage', resourceID: '', fullAccess: false, modelID: '' });
      } else choose(next as Resource['Kind'], nodeID);
    }} options={['model', 'tool', 'sandbox'].map(type => ({ value: type, label: type === 'sandbox' ? copy.title : t.resources.categories[type as 'model' | 'tool'], disabled: type === 'sandbox' ? !available.some(r => isSandboxResource(r.Kind)) : !available.some(r => r.Kind === type) }))} />
    {sandbox && <>
      <Select label={zh ? '选择沙盒' : 'Sandbox'} value={nodeID} disabled={lockedResource} options={nodeOptions} onValueChange={id => {
        // Changing nodes never promotes an ordinary permission to Full Access.
        const first = matching(value.kind, id)[0] ?? available.find(r => isSandboxResource(r.Kind) && (r.SandboxID || 'local') === id);
        const full = value.fullAccess && first?.Kind === 'compute' && fullAccessAvailable(first, sandboxes);
        onChange({ ...targetFor(first, !!full), kind: first?.Kind ?? value.kind, sandboxID: id });
      }} />
      <Select label={zh ? '权限类型' : 'Permission type'} value={permission} onValueChange={next => choose(next === 'full_access' ? 'compute' : next as Resource['Kind'], nodeID, next === 'full_access')} options={sandboxPermissions.map(type => {
        const kind = type === 'full_access' ? 'compute' : type;
        const rows = matching(kind, nodeID);
        const unavailable = !rows.length || (type === 'full_access' && !rows.some(r => fullAccessAvailable(r, sandboxes)));
        return { value: type, label: type === 'full_access' ? fullAccessLabel(zh) : t.resources.categories[type], disabled: (lockedResource && kind !== value.kind) || (unavailable && type !== permission) };
      })} />
    </>}
    {value.kind === 'model' ? <ModelSelect models={models.filter(m => !lockedResource || m.ResourceID === value.resourceID)} requireTools={false} value={value.modelID} onValueChange={id => {
      const model = models.find(m => m.ID === id), resource = available.find(r => r.ID === model?.ResourceID);
      onChange(targetFor(resource, false, id));
    }} /> : !value.fullAccess && (value.kind === 'storage' || value.kind === 'network' || value.kind === 'tool' || candidates.length > 1) && <Select label={value.kind === 'storage' ? (zh ? '存储资源' : 'Storage resource') : value.kind === 'network' ? (zh ? '网络范围' : 'Network scope') : t.nav.resources} value={value.resourceID} disabled={lockedResource} searchable={candidates.length >= 8} options={candidates.map(r => ({ value: r.ID, label: r.Name }))} onValueChange={id => onChange(targetFor(available.find(r => r.ID === id)))} />}
    {value.fullAccess && <p className="permission-scope__note">{fullAccessHelp(zh)}</p>}
    {!value.resourceID && <p className="muted">{zh ? '此沙盒暂无可授权的资源，请先连接相应资源。' : 'No resource is available for this permission. Connect one first.'}</p>}
  </>;
}
