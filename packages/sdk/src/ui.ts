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
export type DraftTextAttachment = { id: string; name: string; text: string };
export type ChatDraft = { text: string; files: import('./types').FileInfo[]; pendingTexts?: DraftTextAttachment[] };
export function isChatDraft(value: unknown): value is ChatDraft {
  if (!value || typeof value !== 'object') return false;
  const draft = value as ChatDraft;
  const pending = draft.pendingTexts;
  if (pending !== undefined && (!Array.isArray(pending) || pending.length > 5 || !pending.every(item => item && typeof item.id === 'string' && /^[A-Za-z0-9_-]{1,128}$/.test(item.id) && typeof item.name === 'string' && item.name.length <= 256 && typeof item.text === 'string' && item.text.length <= 20 * 1024 * 1024))) return false;
  return typeof draft.text === 'string' && draft.text.length <= 65536 && Array.isArray(draft.files) && draft.files.length <= 32 && draft.files.every(file => file && typeof file.ID === 'string' && /^[A-Za-z0-9_-]{1,128}$/.test(file.ID) && typeof file.Name === 'string' && file.Name.length <= 4096 && typeof file.MediaType === 'string' && Number.isSafeInteger(file.Size) && file.Size >= 0);
}
