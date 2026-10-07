// Static presentation data for the pinning preview.
// Menu actions do not change the saved assistant state.
export function assistantListPreview(id: string): { pinned: boolean } {
  return { pinned: id === 'personal' };
}
