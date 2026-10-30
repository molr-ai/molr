import type { Resource } from '@molr/sdk';
import type { SandboxView } from '@molr/sdk';

export function fullAccessAvailable(resource: Resource | undefined, sandboxes: SandboxView[] | undefined) {
  return resource?.Kind === 'compute' && !!sandboxes?.some(s => s.ID === (resource.SandboxID || 'local') && s.State === 'available' && s.Info.Enabled && s.Info.Capabilities.FullAccess);
}
export const fullAccessHelp = (zh: boolean) => zh ? '选择不限范围或指定程序。获准调用以服务账号运行，不受 Molr 沙盒隔离或限额限制。' : 'Choose unrestricted access or specified programs. Authorized calls run as the service account outside Molr sandbox isolation and quotas.';
