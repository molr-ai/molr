/** Plugin API 3. Packages are immutable; unknown fields fail closed. */
export type PluginPage = { id: string; title: string; entry: string; capabilities: string[] };
export type PluginTheme = { id: string; name: string; pages: Record<string, string>; capabilities: string[] };
export type PluginSchema = { type: 'object'; properties: Record<string, { type: 'string' | 'number' | 'integer' | 'boolean'; description?: string; enum?: string[] }>; required: string[]; additionalProperties: false };
export type PluginTool = { id: string; description: string; entry?: string; execution: { type: 'http'; settingsScope?: 'admin'; connectionSetting?: string; operation?: string }; capabilities: string[]; inputSchema: PluginSchema };
export type PluginSkill = { id: string; name: string; description: string; entry: string; tools: string[] };
export type PluginSetting = { type: 'string' | 'boolean' | 'number' | 'integer' | 'connection'; title: string; description?: string; default?: string | number | boolean; enum?: string[]; minimum?: number; maximum?: number; providers?: string[]; requiredOperations?: string[] };
export type PluginValue = { value?: unknown; argument?: string; setting?: string };
export type PluginProjection = { type: 'object' | 'array' | 'string' | 'number' | 'boolean'; path?: string; default?: unknown; fields?: Record<string, PluginProjection>; items?: PluginProjection; maxItems?: number };
export type PluginOperation = { method: 'GET' | 'POST'; path: string; inputSchema: PluginSchema; query?: Record<string, PluginValue>; body?: Record<string, PluginValue>; result: PluginProjection; usage?: PluginProjection; errorField?: string };
export type PluginProvider = { id: string; name: string; origin: string; auth: { type: 'none' | 'bearer' | 'header'; header?: string }; operations: Record<string, PluginOperation>; test?: { settingsScope?: 'admin'; operation: string; arguments: Record<string, unknown> } };
export type PluginManifest = { manifestVersion: 1; pluginApi: 3; id: string; name: string; description: string; version: string; requirements?: PluginRequirement[]; credentials?: PluginSecretField[]; contributes: { themes: PluginTheme[]; pages: PluginPage[]; tools: PluginTool[]; skills: PluginSkill[] }; adminSettings?: Record<string, PluginSetting>; adminSettingsPage?: PluginPage; settings?: Record<string, PluginSetting>; settingsPage?: PluginPage; providers?: PluginProvider[] };
export type PluginPackage = { manifest: PluginManifest; files: Record<string, string> };
export type PluginBinding = { AssistantID: string; Tools: string[] };
export type PluginInstallation = { ID: string; CatalogID: string; ToolIDs: string[]; UserEnabled: boolean; UserVersion: number; Manifest: PluginManifest; Digest: string; Version: number; Enabled: boolean; Grants: string[] | null; Bindings: PluginBinding[] | null; Versions: { Version: string; Digest: string }[] };
export type PluginUIReference = { ID: string; Digest: string; Kind: 'page' | 'theme' | 'settings' | 'admin-settings'; Contribution: string; Page: string };
export type PluginUserSettings = { PluginID: string; Digest: string; Version: number; Values: Record<string, string | number | boolean>; NeedsReview: boolean };
export type PluginConnection = { ID: string; PluginID: string; ProviderID: string; Name: string; Enabled: boolean; Version: number; Configured: boolean; NeedsReview: boolean; Operations: string[]; CallLimit: number; Concurrent: number; TimeoutSeconds: number; Calls: number; Active: number };
export const pluginCapabilities = (m: PluginManifest) => [...new Set([...(m.contributes.pages ?? []).flatMap(p => p.capabilities ?? []), ...(m.contributes.themes ?? []).flatMap(t => t.capabilities ?? []), ...(m.contributes.tools ?? []).flatMap(t => t.capabilities ?? []), ...(m.settingsPage?.capabilities ?? []), ...(m.adminSettingsPage?.capabilities ?? [])])];
export const pluginAllowed = (p: PluginInstallation, caps: string[] = []) => p.Enabled && p.UserEnabled !== false && caps.every(c => p.Grants?.includes(c));

export type PluginRequirement = { phase: 'install' | 'use'; kind: 'compute' | 'network' | 'storage'; description: string; destinations?: string[] };
export type PluginSecretField = { id: string; title: string };
export type PluginInstance = { PluginID: string; AssistantID: string; Release: string; Digest: string; Enabled: boolean; Version: number; Workspace: string; Environment: string; Account: string; Detail: string; Credentials?: PluginSecretField[]; Requirements?: PluginRequirement[] };
