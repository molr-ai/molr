import { api, APIError } from '@molr/sdk';
import type { FileInfo } from '@molr/sdk';

export type FileSnapshot = { file?: FileInfo; previewUrl?: string; error?: unknown; loading: boolean };
type Entry = { snapshot: FileSnapshot; revision: number; bytes: number; stale: boolean; controller?: AbortController; listeners: Map<() => void, boolean> };
const empty: FileSnapshot = { loading: true };

/** Session-owned metadata and image previews. Ordinary file bodies never enter this cache. */
export class FileCache {
  private entries = new Map<string, Entry>();
  private revision = 0;
  private closed = false;
  constructor(private maxEntries = 500, private maxPreviewBytes = 64 * 1024 * 1024) {}

  private entry(id: string) {
    let entry = this.entries.get(id);
    if (!entry) {
      entry = { snapshot: empty, revision: ++this.revision, bytes: 0, stale: true, listeners: new Map() };
      this.entries.set(id, entry);
    }
    return entry;
  }
  snapshot(id: string): FileSnapshot { return this.entries.get(id)?.snapshot ?? empty; }
  version(ids: string[]) { return ids.map(id => this.entries.get(id)?.revision ?? 0).join(':'); }
  private emit(entry: Entry, snapshot: FileSnapshot) {
    entry.snapshot = snapshot; entry.revision = ++this.revision;
    entry.listeners.forEach((_, listener) => listener());
  }
  private touch(id: string, entry: Entry) { this.entries.delete(id); this.entries.set(id, entry); }
  private releasePreview(entry: Entry) {
    if (entry.snapshot.previewUrl) URL.revokeObjectURL(entry.snapshot.previewUrl);
    entry.bytes = 0;
    entry.snapshot = { ...entry.snapshot, previewUrl: undefined };
  }
  private trim() {
    let bytes = [...this.entries.values()].reduce((total, entry) => total + entry.bytes, 0);
    for (const [id, entry] of this.entries) {
      if (entry.listeners.size || entry.controller) continue;
      if (bytes > this.maxPreviewBytes || this.entries.size > this.maxEntries) {
        bytes -= entry.bytes; this.releasePreview(entry);
        if (this.entries.size > this.maxEntries) this.entries.delete(id);
        else entry.revision = ++this.revision;
      }
    }
  }
  subscribe(id: string, listener: () => void, preview = true) {
    const entry = this.entry(id);
    entry.listeners.set(listener, preview); this.touch(id, entry);
    void this.load(id, entry); this.trim();
    return () => {
      entry.listeners.delete(listener);
      // Allow React's subscription hand-off to reuse the same pending request.
      queueMicrotask(() => {
        if (entry.listeners.size || this.entries.get(id) !== entry) return;
        entry.controller?.abort(); entry.controller = undefined;
        if (!entry.snapshot.file) this.entries.delete(id);
        else this.emit(entry, { ...entry.snapshot, loading: false });
        this.trim();
      });
    };
  }
  private async load(id: string, entry: Entry) {
    if (this.closed || entry.controller || !entry.listeners.size) return;
    const wantsPreview = () => [...entry.listeners.values()].some(Boolean);
    if (!entry.stale && (!wantsPreview() || !entry.snapshot.file?.MediaType.startsWith('image/') || entry.snapshot.previewUrl)) return;
    const controller = new AbortController(); entry.controller = controller;
    const current = () => !this.closed && !controller.signal.aborted && this.entries.get(id) === entry;
    this.emit(entry, { ...entry.snapshot, loading: true, error: undefined });
    try {
      if (entry.stale) {
        const file = await api.get<FileInfo>(`/api/files/${encodeURIComponent(id)}`, controller.signal);
        if (!current()) return;
        if (entry.snapshot.file?.MediaType !== file.MediaType) this.releasePreview(entry);
        entry.stale = false;
        this.emit(entry, { ...entry.snapshot, file });
      }
      const file = entry.snapshot.file!;
      if (wantsPreview() && file.MediaType.startsWith('image/') && !entry.snapshot.previewUrl) {
        const blob = await api.download(`/api/files/${encodeURIComponent(id)}/content`, controller.signal);
        if (!current()) return;
        // Preserve the download endpoint's octet-stream safety boundary.
        const previewUrl = URL.createObjectURL(new Blob([blob], { type: file.MediaType }));
        entry.bytes = blob.size;
        this.emit(entry, { ...entry.snapshot, previewUrl });
      }
      if (current()) this.emit(entry, { ...entry.snapshot, loading: false });
    } catch (error) {
      if (current()) {
        const inaccessible = error instanceof APIError && [401, 403, 404].includes(error.status);
        if (inaccessible) { this.releasePreview(entry); entry.stale = true; }
        this.emit(entry, { ...(inaccessible ? {} : entry.snapshot), loading: false, error });
      }
    } finally {
      if (entry.controller === controller) entry.controller = undefined;
      this.trim();
    }
  }
  remember(file: FileInfo) {
    if (this.closed || this.entries.has(file.ID)) return;
    const entry = this.entry(file.ID);
    // Upload receipts seed metadata only; no non-image body is retained.
    entry.stale = false;
    this.emit(entry, { file, loading: false }); this.touch(file.ID, entry); this.trim();
  }
  retry(ids: string[]) { for (const id of ids) { const entry = this.entries.get(id); if (entry) void this.load(id, entry); } }
  invalidate(id?: string, discardPreview = false) {
    // Retain a bounded marker even if deletion arrives before an upload receipt.
    if (id && discardPreview) this.entry(id);
    for (const [key, entry] of [...this.entries]) {
      if (id && id !== key) continue;
      entry.controller?.abort(); entry.controller = undefined; entry.stale = true;
      if (discardPreview) { this.releasePreview(entry); entry.snapshot = empty; }
      this.emit(entry, { ...entry.snapshot, loading: !!entry.listeners.size, error: undefined });
      if (entry.listeners.size) void this.load(key, entry);
    }
    this.trim();
  }
  activate() {
    this.closed = false;
    for (const [id, entry] of [...this.entries]) if (entry.listeners.size) void this.load(id, entry);
  }
  clear() {
    this.closed = true;
    for (const entry of this.entries.values()) { entry.controller?.abort(); this.releasePreview(entry); }
    this.entries.clear();
  }
}
