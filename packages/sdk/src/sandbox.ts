export type SandboxKind = 'storage' | 'compute' | 'network';
export type SandboxNetworkTarget = { Protocols: string[]; Addresses: string[]; Ports: string[]; ExcludeAddresses?: string[]; ExcludePorts?: string[] };
export type GPUDevice = { UtilizationPercent?: number | null; MemoryUsedBytes?: number | null; ID: string; Name: string; Vendor: string; MemoryBytes: number; Available: boolean; Reason: string };
export type SandboxInfo = {
 HostCapacity?: { GPUs?: GPUDevice[]; CPUUsagePercent?: number | null; MemoryUsedBytes?: number | null; CPUPercent: number; MemoryBytes: number; Processes: number };
 StorageCeilingBytes?: number;
 GPUs?: GPUDevice[];
  AllowDirectoryPaths?: boolean; Enabled: boolean; Storage: boolean; ShellSessions?: boolean;
  Capabilities: { GPUAccessMode?: string; FullAccessTerminal?: boolean; Terminal?: boolean; TerminalReason?: string; FullAccess?: boolean; OS: string; NetworkBackend?: string; NetworkProtocols?: string[]; Shell: boolean; ShellType?: string; Reason: string; ProcessIsolation: boolean; FileIsolation: boolean; NetworkIsolation: boolean; ResourceLimits: boolean };
  Directories: { ID: string; Name: string; ReadOnly: boolean }[];
  Networks: { ID: string; Name: string; Target: SandboxNetworkTarget }[];
  ResourceQuotas?: boolean; WorkspaceProjectQuotas?: boolean;
  AuthorizationCeilings?: { GPUDeviceIDs?: string[]; CPUPercent:number;MemoryBytes:number;Processes:number;NetworkBytes?:number;NetworkConnections?:number };
  Limits: { NetworkBytes?: number; NetworkConnections?: number; MemoryBytes: number; Processes: number; CPUPercent: number };
  TimeoutSeconds: number;
};
export type SandboxView = { ID: string; Name: string; Endpoint: string; PublicKeySHA256: string; State: string; Local: boolean; Version: number; Info: SandboxInfo; ConnectionReason?: string; CheckedAt?: string };
export type SandboxResourceDetail = { ID: string; SourceID: string; Path: string; Capacity?: { StorageTotalBytes: number; StorageUsedBytes: number; StorageBytes: number; StorageAvailableBytes: number } };
export type SandboxDiagnostic = { Connected: boolean; Reason: string; Field: 'Endpoint' | 'ConnectionCode'; Info?: SandboxInfo; CheckedAt: string };
