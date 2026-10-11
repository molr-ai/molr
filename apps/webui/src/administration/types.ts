import type { Money, ResourceConstraints, UserInfo } from '@molr/sdk';
export type { UserInfo };
export type ResourceQuota = {
  Budget?: Money; Calls: number; Concurrent: number; Period: string; Timezone: string;
  Compute?: ResourceConstraints['compute']; Network?: ResourceConstraints['network']; Storage?: ResourceConstraints['storage'];
};
export type ResourceReference = { Kind: 'model' | 'tool' | 'sandbox' | 'connection' | 'plugin'; ID: string; AllowFullAccess?: boolean; Quota: ResourceQuota };
export type UserGroup = { ID: string; Name: string; Administrator: boolean; Resources: ResourceReference[]; Version: number };
export type CatalogItem = { GPUs?: import("../sandbox-types").GPUDevice[]; Kind: ResourceReference['Kind']; ID: string; Name: string; WorkspaceQuotaSupported: boolean; FullAccessAvailable?: boolean; Limits?: { CPUPercent: number; MemoryBytes: number; Processes: number; TempBytes: number; MaxFileBytes: number; NetworkBytes: number; NetworkConnections: number } };
export type UsageSummary = { UserID: string; Username: string; DisplayName: string; GroupID: string; ModelCalls: number; ToolCalls: number; ConnectionCalls: number; Active: number; Unknown: number; AttachmentBytes: number; Spent: Money[]; Reserved: Money[] };
export type ContextStorageUsage = { Bytes: number; Messages: number };
export type AdminUsageView = { Users: UsageSummary[]; ContextStorage: ContextStorageUsage };
export type AccountQuotas = { ContextStorage: ContextStorageUsage; Group: UserGroup; Summary: UsageSummary; AsOf: string; Resources: { Kind: string; ID: string; Name: string; Calls: number; Active: number; Unknown: number; Carried: number; PeriodStart: string; PeriodEnd: string | null; Spent: Money[]; Reserved: Money[] }[] };
export const emptyQuota = (): ResourceQuota => ({ Calls: 0, Concurrent: 0, Period: 'month', Timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC' });
export const groupName = (group: Pick<UserGroup, 'ID' | 'Name'>, zh: boolean) => group.ID === 'administrators' ? (zh ? '管理员' : 'Administrators') : group.Name === 'Users' && group.ID === 'users' ? (zh ? '普通用户' : 'Users') : group.Name;
