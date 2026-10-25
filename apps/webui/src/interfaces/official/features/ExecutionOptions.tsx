import './execution-options.css';
import { useEffect, useState } from 'react';
import { Alert, Button, Select } from '@molr/ui';
import type { Grant, Resource } from '@molr/sdk';

export function useExecutionOptions(resources: Resource[] | undefined, grants: Grant[] | undefined, sandbox: boolean, fullAccess: boolean) {
  const [now, setNow] = useState(Date.now);
  useEffect(() => { const timer = setInterval(() => setNow(Date.now()), 1000); return () => clearInterval(timer); }, []);
  const options = (resources ?? []).filter(r => r.State === 'available' && (r.SandboxID ?? 'local') === 'local').map(resource => {
    const active = (grants ?? []).filter(g => g.ResourceID === resource.ID && !g.RevokedAt && (!g.ExpiresAt || Date.parse(g.ExpiresAt) > now) && g.Actions.includes('execute'));
    return { resource, sandbox: sandbox && active.some(g => !g.Scope.FullAccess), fullAccess: fullAccess && active.some(g => g.Scope.FullAccess) };
  }).filter(option => option.sandbox || option.fullAccess);
  const [selection, setSelection] = useState<{ id: string; fullAccess: boolean } | null>(null);
  useEffect(() => {
    if (selection === null && options[0]) setSelection({ id: options[0].resource.ID, fullAccess: !options[0].sandbox });
  }, [selection, options]);
  const selected = options.find(option => option.resource.ID === selection?.id);
  const valid = !!selected && !!(selection?.fullAccess ? selected.fullAccess : selected.sandbox);
  return {
    options, selected, valid, loaded: resources !== undefined && grants !== undefined, compute: selection?.id ?? '', fullAccess: selection?.fullAccess ?? false,
    chooseResource(id: string) { const option = options.find(o => o.resource.ID === id); if (option) setSelection({ id, fullAccess: !option.sandbox }); },
    chooseMode(value: string) { if (selection) setSelection({ ...selection, fullAccess: value === 'full' }); },
  };
}

export function ExecutionOptions({ value, zh, disabled }: { value: ReturnType<typeof useExecutionOptions>; zh: boolean; disabled?: boolean }) {
  const { options, selected, compute, fullAccess, valid } = value;
  return <div className="execution-options">
    {value.loaded && options.length === 0 && <p className="muted">{zh ? '没有可用的已授权计算资源。' : 'No authorized compute resource is available.'}</p>}
    {options.length > 1 && <Select label={zh ? '计算资源' : 'Compute resource'} value={selected ? compute : ''} disabled={disabled} onValueChange={value.chooseResource} options={options.map(o => ({ value: o.resource.ID, label: o.resource.Name }))} />}
    {options.length === 1 && compute && !selected && <Button disabled={disabled} onClick={() => value.chooseResource(options[0].resource.ID)}>{options[0].resource.Name}</Button>}
    {selected && ((selected.sandbox && selected.fullAccess) || !valid) && <Select label={zh ? '执行模式' : 'Execution mode'} value={valid ? fullAccess ? 'full' : 'sandbox' : ''} disabled={disabled} onValueChange={value.chooseMode} options={[...(selected.sandbox ? [{ value: 'sandbox', label: zh ? '沙盒' : 'Sandbox' }] : []), ...(selected.fullAccess ? [{ value: 'full', label: 'Full Access' }] : [])]} />}
    {fullAccess && <Alert tone="warning" title="Full Access">{zh ? '使用服务账户权限，不受 Molr 文件、网络隔离和资源配额限制。' : 'Uses the service account permissions without Molr file/network isolation or resource quotas.'}</Alert>}
    {compute && !valid && <p className="muted">{zh ? '当前资源或执行模式已不可用，请重新选择。' : 'The selected resource or execution mode is unavailable. Select again.'}</p>}
  </div>;
}
