/** Versioned UI contract. Neither React nor @molr/ui is required. */
export const uiPages = ['assistants', 'plans', 'resources', 'workbench', 'settings'] as const;
export type UIPage = typeof uiPages[number];
export type UIRoute = { page: UIPage; id: string };
export const uiPermissions = ['chat.read', 'chat.write', 'plans.read', 'plans.write', 'resources.read', 'resources.write', 'files.read', 'files.write', 'settings.read', 'settings.write', 'workbench.read'] as const;
export type UIPermission = typeof uiPermissions[number];
export type UIPackageManifest = {
  id: string;
  name: string;
  version: string;
  description?: string;
  apiVersion: 1;
  pages: Partial<Record<UIPage, string>>;
  permissions: UIPermission[];
};
export type UIContext = {
  apiVersion: 1;
  route: UIRoute;
  locale: string;
  theme: 'light' | 'dark';
  palette: string;
  reducedMotion: boolean;
  permissions: readonly UIPermission[];
};
export type ChatDraft = { text: string; files: import('./types').FileInfo[] };
export type UIHost = {
  context: UIContext;
  navigate: (page: UIPage, id?: string) => Promise<void>;
  onContext: (listener: (context: UIContext) => void) => () => void;
  onDataChange: (listener: () => void) => () => void;
  drafts: { get: (assistantID: string) => Promise<ChatDraft>; set: (assistantID: string, draft: ChatDraft) => Promise<void> };
  dispose: () => void;
};

export function isChatDraft(value: unknown): value is ChatDraft {
  if (!value || typeof value !== 'object') return false;
  const draft = value as ChatDraft;
  return typeof draft.text === 'string' && draft.text.length <= 65536 && Array.isArray(draft.files) && draft.files.length <= 32 && draft.files.every(file => file && typeof file.ID === 'string' && /^[A-Za-z0-9_-]{1,128}$/.test(file.ID) && typeof file.Name === 'string' && file.Name.length <= 4096 && typeof file.MediaType === 'string' && Number.isSafeInteger(file.Size) && file.Size >= 0);
}
