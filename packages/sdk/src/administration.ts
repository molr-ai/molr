import type { Money, ResourceConstraints } from './types';
import type { GPUDevice } from './sandbox';
export type ResourceQuota = {
  Budget?: Money; Calls: number; Concurrent: number; Period: string; Timezone: string;
  Compute?: ResourceConstraints['compute']; Network?: ResourceConstraints['network']; Storage?: ResourceConstraints['storage'];
};
export type ResourceReference = { Kind: 'model' | 'tool' | 'sandbox' | 'connection' | 'plugin'; ID: string; AllowFullAccess?: boolean; Quota: ResourceQuota };
export type UserGroup = { ID: string; Name: string; Administrator: boolean; Resources: ResourceReference[]; Version: number };
export type CatalogItem = { GPUAccessMode?: string; GPUs?: GPUDevice[]; Kind: ResourceReference['Kind']; ID: string; Name: string; WorkspaceQuotaSupported: boolean; FullAccessAvailable?: boolean; Limits?: { CPUPercent: number; MemoryBytes: number; Processes: number; TempBytes: number; MaxFileBytes: number; NetworkBytes: number; NetworkConnections: number } };
export type UsageSummary = { UserID: string; Username: string; DisplayName: string; GroupID: string; UserMessages: number; ModelCalls: number; ToolCalls: number; ConnectionCalls: number; Active: number; Unknown: number; AttachmentBytes: number; Spent: Money[]; Reserved: Money[] };
export type ContextStorageUsage = { Bytes: number; Messages: number };
export type AdminUsageView = { Users: UsageSummary[]; ContextStorage: ContextStorageUsage };
export type AccountQuotas = { ContextStorage: ContextStorageUsage; Group: UserGroup; Summary: UsageSummary; AsOf: string; Resources: { Kind: string; ID: string; Name: string; Calls: number; Active: number; Unknown: number; Carried: number; PeriodStart: string; PeriodEnd: string | null; Spent: Money[]; Reserved: Money[] }[] };
