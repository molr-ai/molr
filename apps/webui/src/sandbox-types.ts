export type SandboxKind = 'storage' | 'compute' | 'network';
export type SandboxNetworkTarget = { Protocols: string[]; Addresses: string[]; Ports: string[]; ExcludeAddresses?: string[]; ExcludePorts?: string[] };
export type GPUDevice = { ID: string; Name: string; Vendor: string; MemoryBytes: number; Available: boolean; Reason: string };
export type SandboxInfo = {
 GPUs?: GPUDevice[];
  Enabled: boolean; Storage: boolean; ShellSessions?: boolean;
  Capabilities: { FullAccess?: boolean; OS: string; NetworkBackend?: string; NetworkProtocols?: string[]; Shell: boolean; Reason: string; ProcessIsolation: boolean; FileIsolation: boolean; NetworkIsolation: boolean; ResourceLimits: boolean };
  Directories: { ID: string; Name: string; ReadOnly: boolean }[];
  Networks: { ID: string; Name: string; Target: SandboxNetworkTarget }[];
  ResourceQuotas?: boolean; WorkspaceProjectQuotas?: boolean;
  AuthorizationCeilings?: { GPUDeviceIDs?: string[]; CPUPercent:number;MemoryBytes:number;Processes:number;NetworkBytes?:number;NetworkConnections?:number };
  Limits: { NetworkBytes?: number; NetworkConnections?: number; MemoryBytes: number; Processes: number; CPUPercent: number };
  TimeoutSeconds: number;
};
export type SandboxView = { ID: string; Name: string; Endpoint: string; Fingerprint: string; State: string; Local: boolean; Version: number; Info: SandboxInfo; ConnectionReason?: string; CheckedAt?: string };
export type SandboxResourceDetail = { ID: string; SourceID: string; Path: string };
export type SandboxDiagnostic = { Connected: boolean; Reason: string; Field: 'Endpoint' | 'Fingerprint' | 'Token'; Info?: SandboxInfo; CheckedAt: string };
