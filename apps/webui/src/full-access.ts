import type { Resource } from '@molr/sdk';
import type { SandboxView } from './sandbox-types';

export function fullAccessAvailable(resource: Resource | undefined, sandboxes: SandboxView[] | undefined) {
  return resource?.Kind === 'compute' && !!sandboxes?.some(s => s.ID === (resource.SandboxID || 'local') && s.State === 'available' && s.Info.Enabled && s.Info.Capabilities.FullAccess);
}
export const fullAccessHelp = (zh: boolean) => zh ? '允许助理显式请求以服务账号直接访问文件、运行命令和联网，不提供隔离或限额。普通调用仍使用隔离权限。' : 'Allow explicit requests to access files, commands and network as the service account without isolation or quotas. Ordinary calls still use isolated permissions.';
