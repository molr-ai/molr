export type Palette = 'forest' | 'ocean' | 'iris' | 'sand' | 'rose' | 'monochrome';
/** Runtime wire DTOs retain Go's public JSON field names. No demo-domain data. */
export type ID = string;
export type Page<T> = { Items: T[] | null; NextCursor: string };
export type UserInfo = { Avatar: string; ID: ID; Username: string; DisplayName: string; GroupID: ID; GroupName: string; Administrator: boolean; Disabled: boolean; Version: number; CreatedAt: string; MFAEnabled: boolean };
export type Principal = { OwnerID: ID; SubjectID: ID; SessionID: ID; Kind: 'owner'; User?: UserInfo };
export type CurrencyCode = 'USD' | 'CNY';
export type ExchangeRates = { version: string; as_of: string; source_url: string; units: Record<string, number> };
export type CurrencyContext = { Currency: CurrencyCode; Timezone: string; Rates: ExchangeRates };
export type Money = { Currency: string; Micros: number };
export type AssistantBudget = { Limit: Money; Period: 'daily' | 'weekly' };
export type Assistant = { ContextCompressionTokens: number; ID: ID; Name: string; Status?: string; Avatar?: string; State: 'active' | 'dismissed'; ConversationID: ID; GroupID: ID; Pinned: boolean; ModelID: ID; Budget: AssistantBudget; Version: number };
export type Conversation = { ID: ID; PeerID: ID; Archived: boolean; LastSequence: number; LastReadSequence: number; UnreadCount: number };
export type Message = { Source?: { ExecutionID?: ID; OperationID?: ID }; ID: ID; ConversationID: ID; Sequence: number; Sender: 'user' | 'assistant'; Content: { Kind: 'text' | 'card'; Text: string; AttachmentIDs: ID[] | null; Card: { Kind: 'task' | 'resource' | 'schedule' | 'permission_request'; TargetID: ID; Title: string; Summary: string } | null }; CreatedAt: string; AcceptedAt: string | null; ReadAt?: string | null; Feedback?: 'like' | 'dislike' | 'none' };
export type ExpediteStatus = { ThroughSequence: number; State: 'pending' | 'waiting_tool' | 'waiting' | 'read' | 'not_running' | 'not_needed' };
export type AgentStatus = { Compression?: { EventID: ID; State: string; Reason?: string }; ModelRetry?: { Attempt: number; MaxAttempts: number; NextAt: string; Reason: string }; Context?: { ModelID: ID; Limit: number; Tokens: number; Source: string; CompressionThresholdTokens: number; Compressions: number }; TriggerMessageID?: ID; InterruptedThroughSequence?: number; Expedite?: ExpediteStatus; ExecutionID?: ID; AssistantID: ID; DreamDeadline?: string; DreamRunID?: ID; State: 'idle' | 'dreaming' | 'working' | 'waiting' | 'paused' | 'stopped'; WaitingReason: string; ActiveTaskID?: ID };
export type ModelPricing = { AudioInputMicrosPerMillion?: number; Currency: string; InputMicrosPerMillion: number; OutputMicrosPerMillion: number; CachedInputMicrosPerMillion: number };
export type Model = { ID: ID; Name: string; ResourceID: ID; SupportsTools: boolean; SupportsImages?: boolean; SupportsAudio?: boolean; ContextLimit: number; ProviderContextLimit?: number; EffectiveContextLimit?: number; ContextLimitSource?: 'manual' | 'provider' | 'fallback'; OutputLimit?: number; ProviderOutputLimit?: number; EffectiveOutputLimit?: number; OutputLimitSource?: 'manual' | 'provider' | 'fallback'; Pricing: ModelPricing | null; PricingInfo?: { Source: 'builtin' | 'provider' | 'manual'; Automatic: ModelPricing | null; RuleID: string; Version: string; Period: 'peak' | 'off_peak'; SourceURL: string; CalendarKnown: boolean } | null };
export type Todo = { ID: ID; Title: string; Notes: string; AssistantID: ID; DueAt: string | null; StartAt?: string | null; EndAt?: string | null; Completed: boolean; Version: number };
export type Schedule = { ID: ID; Kind: 'personal' | 'assistant_plan'; AssistantID: ID; Title: string; Instructions: string; EndAt?: string | null; Trigger: { StartAt: string; Timezone: string; Recurrence: string; MissedPolicy: 'skip' | 'coalesce' | 'catch_up'; MaxCatchUp: number }; Enabled: boolean; NextAt: string | null; Version: number };
export type Resource = { ID: ID; SandboxID?: ID; Kind: 'storage' | 'model' | 'compute' | 'network' | 'tool' | 'connection'; Name: string; Provider: string; Actions: string[]; State: string; Version: number };
export type ResourceConstraints = {
 compute?: { gpu?: { device_ids: string[] } | null; cpu_percent: number; memory_bytes: number; processes: number; max_duration_seconds: number; unlimited_duration: boolean; temp_bytes: number };
 network?: { bytes: number; connections: number; period: 'lifetime' | 'day' | 'week' | 'month'; timezone: string };
 storage?: { capacity_bytes: number; max_file_bytes: number; max_files: number };
};
export type TemporaryConstraints = { limits: ResourceConstraints; until: string };
export type Grant = { Constraints?: ResourceConstraints; Temporary?: TemporaryConstraints; ID: ID; AssistantID: ID; ResourceID: ID; Actions: string[]; Scope: { FullAccess?: boolean; ModelID: ID; PathPrefixes: string[] | null; ReadOnly: boolean }; RevokedAt: string | null; ExpiresAt: string | null; Version: number };
export type MemoryRef = { Scope: 'assistant' | 'global'; AssistantID: ID; Path: string };
export type Memory = MemoryRef & { Dependencies?: { Ref: MemoryRef; Version: string }[]; State: 'active' | 'pending' | 'superseded'; Context?: string; Evidence?: string; Confirmed?: boolean; DreamID?: ID; Replaces?: MemoryRef; ReplacesVersion?: string; UpdatedAt?: string; Text: string; Sources: { Kind: string; ID: string }[] | null; Version: string; Summary: boolean };
export type MemoryEntry = MemoryRef & { Kind: 'file' | 'directory'; Version: string; State?: Memory['State']; UpdatedAt?: string };
export type MemoryDirectory = MemoryRef & { Version: string; Entries: MemoryEntry[] };

export type Settings = { OwnerID: ID; Language: string; Timezone: string; Currency: CurrencyCode; Theme: 'system' | 'light' | 'dark'; UITheme: string; Palette: Palette; DefaultModelID: ID; ReducedMotion: boolean; WorkbenchVisible: boolean; PluginsVisible: boolean; Version: number };
export type Task = { Plan?: PlanRef; PlanTitle?: string; PlanInstructions?: string; PlanVersion?: number; OccurrenceAt?: string | null; CreatedAt?: string; UpdatedAt?: string; ID: ID; AssistantID: ID; Goal: string; State: string; Revision: number; Spent: Money; ArtifactIDs: ID[] | null; WaitingReason: string; Version: number };
export type Revision = { Number: number; Instructions: string; AcceptanceCriteria: string[] | null; ArtifactIDs: ID[] | null };
export type FileInfo = { ID: ID; Name: string; Size: number; MediaType: string; State: string };
export type Session = { ID: ID; DeviceName: string; ClientType: string; LastLoginAt: string; Current: boolean; CreatedAt: string; LastSeenAt: string; ExpiresAt: string };
export type Usage = { OriginalSpent?: Money[] | null; OriginalReserved?: Money[] | null; Exchange?: ExchangeRates | null; Limit?: Money | null; PeriodStart?: string | null; PeriodEnd?: string | null; Spent: Money; Reserved: Money; UnknownOperations: ID[] | null };
export type SearchHit = { Memory?: MemoryRef; Source: 'assistants' | 'messages' | 'resources' | 'tasks' | 'memories'; Object: { Kind: string; ID: ID }; AssistantID: ID; Title: string; Snippet: string };
export type SearchResults = { Hits: Page<SearchHit>; Counts: Record<string, number> };

export type RuntimeInfo = { build?: BuildInfo; workbench_available: boolean; name: string; status: string; public_url: string };

export type SecurityStatus = { MFAEnabled: boolean; ReauthenticationEnabled: boolean; UnprotectedPurposes: string[]; Version: number };

export type DebugRun = { ID: ID; AssistantID: ID; EventID: ID; ExecutionID: ID; Trigger: string; Attempt: number; State: string; Reason: string; StartedAt: string; FinishedAt: string | null; Incomplete: boolean; Steps: DebugStep[] | null; FirstRevision?: number; LastRevision?: number; Source?: DebugSource };
export type DebugToolSummary = { Kind: string; Text?: string; Count?: number };
export type DebugStep = { Summary?: DebugToolSummary; LogicalID?: ID; Attempt?: number; MaxAttempts?: number; NextRetryAt?: string; ErrorReason?: string; HTTPStatus?: number; Sequence: number; Kind: string; State: string; OperationID: ID; At: string; DataStatus: string; Revision?: number; Name?: string; Preview?: string };
export type DebugPayload = { Status: string; Data: Record<string, any> | null };

export type ToolCapability = { Source?: 'builtin' | 'plugin' | 'mcp'; ConnectionID?: string; RemoteName?: string; Name: string; Description: string; InputSchema: unknown; RequiredActions: string[]; SideEffect: boolean; Available: boolean; Reason: string; ResourceIDs: string[] };
export type AssistantCapabilities = { AssistantID: ID; Tools: ToolCapability[]; Skills: { Supported: boolean; Items: Array<{ ID: string; Name: string; Description: string; Version: string; Enabled: boolean; RequiredTools: string[]; RequiredActions: string[] }> } };

export type PlanRef = { Kind: 'todo' | 'schedule'; ID: string };
export type PlanExecution = { ID: string; State: string; Title: string; Instructions: string; PlanVersion: number; OccurrenceAt: string | null; StartedAt: string; CompletedAt: string | null; Result: string; Task: Task | null };
export type PlanEvent = { Key: string; At: string; Plan: Plan };
export type PlanLinks = { Ref: PlanRef; Dependencies: PlanRef[] | null; Handoff: 'notify' | 'automatic'; Paused: boolean; Version: number; Dispatched: boolean; Runs: PlanExecution[] | null };
export type Plan = { Ref: PlanRef; Title: string; AssistantID: string; Todo: Todo | null; Schedule: Schedule | null; Links: PlanLinks; Executions: PlanExecution[]; Occurrences: string[]; BlockedBy: PlanRef[]; Ready: boolean };
export type TrajectoryEntry = { RunID: ID; Step: DebugStep };
export type TrajectoryPage = { Items: TrajectoryEntry[]; Runs: DebugRun[]; NextCursor: string; HeadCursor: string; ContextResetAt: string | null; Cleanup: DebugCleanupEvent | null };

export type DebugSource = { Kind: string; Title: string; Text: string; Status: string; Origin: string; ID: ID; Version: number; AttachmentIDs: ID[] | null; Truncated: boolean };

export type TokenUsage = { InputTokens: number; CachedTokens: number; OutputTokens: number };
export type ModelTiming = { RequestMs: number; FirstTokenMs: number | null; GenerationMs: number | null };
export type TurnMetrics = {
  TurnID: ID; State: string; Finalized: boolean; UpdatedAt: string; StartedAt: string | null; EndedAt: string | null; ElapsedMs: number | null;
  Usage: TokenUsage | null; Charges: Money[]; UsageComplete: boolean; CostComplete: boolean;
  FirstTokenMs: number | null; TokensPerSecond: number | null; TimingSamples: number; SpeedSamples: number;
  ModelCalls: number; CompletedCalls: number; ToolCalls: number; Retries: number; Models: string[];
  Calls: { OperationID: ID; ModelName: string; State: string; CreatedAt: string; Usage: TokenUsage | null; Charged: Money | null; Timing: ModelTiming | null }[];
};

export type DreamConfig = { MaxDurationSeconds: number; AssistantID: ID; Enabled: boolean; Hour: number; Minute: number; Timezone: string; NextDay: string; Version: number };
export type DreamRun = { Maintenance?: boolean; ReviewPath?: string; ReviewRetained?: boolean; UndoRetained?: MemoryRef[]; SessionID?: ID; SessionState?: string; Deadline?: string; Batch?: number; Backfill?: boolean; FileChanges?: { OperationID: ID; Tool: string; Path: string; ToPath?: string }[]; ID: ID; AssistantID: ID; Day: string; Timezone: string; State: string; Summary: string; Sources: { Source: { Kind: string; ID: ID }; At: string; Kind: string }[]; MemoryFiles: MemoryRef[]; FailureCode: string; UpdatedAt: string };

export type DreamWindow = { AssistantID: ID; Start: string; End: string; Timezone: string; State: string; Manual: boolean; SessionID: ID };

export type PermissionRequest = {
  ID: ID; AssistantID: ID; TaskID: ID; State: 'pending' | 'approved' | 'rejected'; GrantID: ID; MessageID: ID; Granted?: Grant; GrantedAll?: Grant[]; CreatedAt: string; ResolvedAt: string | null; Version: number;
  Input: PermissionRequestInput;
};

export type ShellForward = { Protocol: 'http' | 'tcp'; Port: number; Address: string; URL: string; State: 'waiting' | 'ready' | 'closed' };
export type BrowserControl = { Control: 'assistant' | 'user' | 'switching'; Generation: number };
export type BrowserProxySummary = { Mode: 'direct' | 'proxy'; Server: string; Bypass: string[]; Authentication: boolean; Version: number };
export type BrowserProxySettings = BrowserProxySummary & { Username: string; PasswordConfigured: boolean };
export type BrowserNetworkFailure = { TaskID: string; At: string; Reason: string; Message: string; Proxy: BrowserProxySummary; Sandbox?: {Reason: string; At: string} };
export type BrowserStatus = { Proxy?: BrowserProxySummary; ProxyAppliesTo?: 'current_session' | 'next_start'; RestartRequired?: boolean; LastNetworkFailure?: BrowserNetworkFailure; LastFailure?: { TaskID: string; Reason: string }; ComponentsEnabled?: boolean; Preparation?: { task_id: string; state: string; error_code?: string }; Available: boolean; Reason?: string; Task?: ShellTask; ControllerIsCurrentSession?: boolean; ControllerClientID?: string; TabCount?: number };
export type TerminalControl = { Control: 'assistant' | 'user'; Generation: number; Rows: number; Cols: number; NeedsRead: boolean; ControllerIsCurrentSession?: boolean; ControllerClientID?: string };
export type ShellTask = { Terminal?: TerminalControl; CreatedBy?: 'user' | 'assistant'; Kind?: 'shell' | 'browser'; Browser?: BrowserControl; GPUDeviceIDs?: string[]; FullAccess?: boolean;
  Forwards?: ShellForward[];
  ID: string; OwnerID: string; AssistantID: string; SandboxID: string;
  Mode: 'foreground' | 'background' | 'tty'; State: 'running' | 'completed' | 'failed' | 'stopped' | 'interrupted';
  Script: string; StartedAt: string; FinishedAt?: string; StdoutPath: string; StderrPath: string;
  Failure?: string; Result?: { Exit?: { Code: number; Reason: string; TimedOut: boolean }; Stdout?: string; Stderr?: string; OutputComplete?: boolean };
};
export type ShellTaskAction = { action: 'list' | 'read' | 'write' | 'interrupt' | 'stop' | 'stop_all' | 'resize' | 'takeover' | 'release'; client_id?: string; generation?: number; sequence?: number; encoding?: 'base64'; task_id?: string; cursor?: number; stream?: 'stdout' | 'stderr'; data?: string; rows?: number; cols?: number };
export type ShellTaskOutput = { Terminal?: TerminalControl; Finished?: boolean; OutputComplete?: boolean; Output: string; NextCursor: number; Path: string };

export type PermissionRequestInput = { full_access?: boolean; constraints?: ResourceConstraints; temporary?: TemporaryConstraints; grant_id?: ID; grant_version?: number; resource_kind: Resource['Kind']; resource_id?: ID; model_id?: ID; resource_description: string; actions: string[]; path_prefixes?: string[]; expires_at?: string; reason: string; additional_requests?: PermissionRequestInput[]; };

export type DebugRetentionPolicy = { Version: number; MaxRuns: number };
export type DebugCleanupEvent = { At: string; Policy: DebugRetentionPolicy };
export type DebugRetentionStatus = { Policy: DebugRetentionPolicy; Runs: number };

export type DebugRetentionOverrides = { MaxRuns: number | null };
export type DebugRetentionSettings = DebugRetentionPolicy & { Configured: DebugRetentionOverrides; Defaults: DebugRetentionPolicy };

// First-use setup is authenticated administrator configuration, never account registration.
export type SetupState = { Required: boolean; AssistantID: ID };
export type SetupRequest = { ModelID: ID; Language: 'zh' | 'en'; Timezone: string; Budget: AssistantBudget; Pricing?: ModelPricing };
export type DiscoveredProvider = { Name: string; Preset: string; Endpoint: string; State: 'ready' | 'empty' | 'authentication_required'; Models: string[]; ResourceID: ID };

export type WeixinDelivery = { ID: ID; BindingID: ID; MessageID: ID; Sequence: number; Part: number; State: 'pending' | 'sending' | 'accepted' | 'waiting' | 'failed' | 'unknown' | 'canceled'; Error: string; Attempts: number; CreatedAt: string };
export type WeixinStatus = { Channel: 'webui' | 'weixin'; State: 'disconnected' | 'connected' | 'reconnecting' | 'login_required' | 'paused'; ConnectedAt: string | null; LastInboundAt: string | null; LastError: string; Deliveries: WeixinDelivery[] };
export type TelegramStatus = { Channel: 'webui' | 'telegram'; State: 'disconnected' | 'connected' | 'reconnecting' | 'token_invalid' | 'blocked' | 'poll_conflict' | 'paused'; BotUsername: string; ConnectedAt: string | null; LastInboundAt: string | null; LastError: string; Deliveries: WeixinDelivery[] };
export type TelegramLogin = { ID: ID; Status: 'waiting' | 'ready'; URL: string; BotUsername: string; UserName: string; UserID: number; ExpiresAt: string };
export type SlackStatus = { Channel: 'webui' | 'slack'; State: 'disconnected' | 'pairing' | 'connected' | 'reconnecting' | 'login_required' | 'paused'; ConnectedAt: string | null; LastInboundAt: string | null; LastError: string; Deliveries: WeixinDelivery[] };
export type SlackLogin = { ID: ID; Code: string; ExpiresAt: string };
export type WeixinLogin = { ID: ID; Status: 'wait' | 'scaned' | 'confirmed' | 'already_connected' | 'expired' | 'need_verifycode' | 'verify_code_blocked' | 'conflict'; QRCode: string; ExpiresAt: string };

export type SystemNotice = { ID: ID; OwnerID: ID; Kind: string; Code: string; Source: { Kind: string; ID: ID }; AssistantID: ID; Label: string; Severity: 'neutral' | 'warning' | 'danger'; CreatedAt: string; UpdatedAt: string; ReadAt: string | null; DismissedAt: string | null; ResolvedAt: string | null; Version: number };
export type NotificationPage = { Items: SystemNotice[]; NextCursor: string; UnreadCount: number; PendingCount: number };

/** Bounded assistant-list projection; it contains no execution internals. */
export type AssistantActivity = { AssistantID: ID; ConversationID: ID; Dreaming: boolean; Preview: { Kind: 'text' | 'card'; Text: string; CreatedAt: string } | null };

export type BuildInfo = { version: string; commit: string; built_at: string; os: string; arch: string; embedded_webui: boolean };
export type ReleaseInfo = { version: string; commit: string; published_at: string; notes_url: string; file: string; sha256: string };
export type UpdateStatus = {
 current: BuildInfo; latest: ReleaseInfo | null;
 comparison: 'unknown' | 'current' | 'available' | 'ahead' | 'uncomparable';
 check: { checking: boolean; last_attempt_at: string | null; last_success_at: string | null; next_allowed_at: string | null; error_code?: string };
 auto_check: boolean; source_configured: boolean; supported: boolean;
};
export type UpdateInstallation = { name: string; system: boolean; setup_path: string; source: string };

export type MaintenanceTask = { id: string; operation: string; component?: string; target?: string; state: 'running' | 'succeeded' | 'failed' | 'cancelled' | 'interrupted'; stage: string; downloaded?: number; total?: number; error_code?: string; started_at: string; updated_at: string; finished_at?: string };
export type ComponentStatus = { id: string; state: 'missing' | 'installed' | 'broken' | 'external' | 'unsupported'; version?: string; target_version?: string; download_bytes: number; disk_bytes: number; can_install: boolean; auto_install: boolean; reason?: string; task?: MaintenanceTask };

export interface WorkspaceEntry { Name: string; Path: string; Type: 'file' | 'directory' | 'unavailable'; Size: number; ModifiedAt: string; Version: string; MediaType?: string }
export interface WorkspaceListing { Entries: WorkspaceEntry[]; Version: string; NextOffset: number; Truncated: boolean }
export interface WorkspaceText extends WorkspaceEntry { Text: string; Editable: boolean; Truncated: boolean }
export interface WorkspaceOperation { Action: 'write' | 'mkdir' | 'move' | 'delete'; Path: string; ToPath?: string; Text?: string; ExpectedVersion?: string }

export type DingtalkStatus = { Channel: 'webui' | 'dingtalk'; State: 'disconnected' | 'connecting' | 'pairing' | 'connected' | 'reconnecting' | 'credential_required' | 'expired' | 'paused'; ClientID: string; LastError: string; ConnectedAt: string | null; LastInboundAt: string | null; ExpiresAt: string };
export type DingtalkPairing = { Code: string; ExpiresAt: string };

export type FeishuStatus = { Channel: 'webui' | 'feishu'; State: 'disconnected' | 'connecting' | 'connected' | 'reconnecting' | 'credentials_required' | 'paused'; AppID: string; ConnectedAt: string | null; LastInboundAt: string | null; LastError: string };
export type FeishuLogin = { ID: string; Status: 'waiting' | 'confirmed' | 'expired' | 'denied' | 'unsupported_tenant'; QRCode: string; ExpiresAt: string };

export type MCPTool = { ID: ID; Name: string; Description: string; InputSchema: unknown; Enabled: boolean; Problem: string };
export type MCPConnection = { ID: ID; Name: string; Endpoint: string; Auth: 'none' | 'bearer' | 'header'; Header: string; Enabled: boolean; Version: number; CallLimit: number; Concurrent: number; TimeoutSeconds: number; Tools: MCPTool[]; CheckedAt: string | null; Status: 'unchecked' | 'available' | 'offline'; Configured: boolean; Calls: number; Active: number };
export type MCPCall = { ID: ID; OwnerID: ID; ToolName: string; State: 'prepared' | 'running' | 'succeeded' | 'tool_error' | 'failed' | 'unknown'; CreatedAt: string; DurationMS: number; Failure: string };
