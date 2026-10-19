/** Versioned UI contract. Neither React nor @molr/ui is required. */
export const uiPages = ['assistants', 'plans', 'resources', 'workbench', 'settings'] as const;
export type UIPage = typeof uiPages[number];
export type UIRoute = { page: UIPage; id: string };
export type UIPackageManifest = {
  id: string;
  name: string;
  version: string;
  description?: string;
  pages: Partial<Record<UIPage, string>>;
};
export type ChatDraft = { text: string; files: import('./types').FileInfo[] };
export function isChatDraft(value: unknown): value is ChatDraft {
  if (!value || typeof value !== 'object') return false;
  const draft = value as ChatDraft;
  return typeof draft.text === 'string' && draft.text.length <= 65536 && Array.isArray(draft.files) && draft.files.length <= 32 && draft.files.every(file => file && typeof file.ID === 'string' && /^[A-Za-z0-9_-]{1,128}$/.test(file.ID) && typeof file.Name === 'string' && file.Name.length <= 4096 && typeof file.MediaType === 'string' && Number.isSafeInteger(file.Size) && file.Size >= 0);
}
