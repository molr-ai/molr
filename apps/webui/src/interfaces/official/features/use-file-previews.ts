import { useEffect, useRef, useState } from 'react';
import { api } from '@molr/sdk';
import type { FileInfo } from '@molr/sdk';

function chatAttachment(file: FileInfo, previewUrl?: string) {
  const url = `/api/files/${encodeURIComponent(file.ID)}/content`;
  return {
    id: file.ID,
    name: file.Name,
    size: file.Size,
    type: file.MediaType,
    url,
    previewUrl,
  };
}

// File downloads are deliberately served as octet-stream. Use typed, temporary
// blob URLs for image rendering without changing the safe download endpoint.
export function useFilePreviews(files: FileInfo[]) {
  type Preview = {
    type: string;
    controller: AbortController;
    url?: string;
    failed?: boolean;
  };
  const entries = useRef(new Map<string, Preview>());
  const [previews, setPreviews] = useState<
    Record<string, { type: string; url?: string }>
  >({});
  const key = JSON.stringify(
    files
      .filter((file) => file.MediaType.startsWith('image/'))
      .map((file) => [file.ID, file.MediaType]),
  );
  useEffect(() => {
    const current = entries.current;
    return () => {
      for (const entry of current.values()) {
        entry.controller.abort();
        if (entry.url) URL.revokeObjectURL(entry.url);
      }
      current.clear();
    };
  }, []);
  useEffect(() => {
    const current = entries.current,
      wanted = new Map(JSON.parse(key) as [string, string][]);
    const publish = () =>
      setPreviews(
        Object.fromEntries(
          [...current].map(([id, entry]) => [
            id,
            {
              type: entry.type,
              url:
                entry.url ??
                (entry.failed ? 'data:image/png;base64,' : undefined),
            },
          ]),
        ),
      );
    for (const [id, entry] of current) {
      if (wanted.get(id) === entry.type && !entry.failed) continue;
      entry.controller.abort();
      if (entry.url) URL.revokeObjectURL(entry.url);
      current.delete(id);
    }
    for (const [id, type] of wanted) {
      if (current.has(id)) continue;
      const controller = new AbortController(),
        entry: Preview = { type, controller };
      current.set(id, entry);
      void api
        .download(
          `/api/files/${encodeURIComponent(id)}/content`,
          controller.signal,
        )
        .then((blob) => {
          if (controller.signal.aborted) return;
          entry.url = URL.createObjectURL(new Blob([blob], { type }));
          publish();
        })
        .catch(() => {
          if (!controller.signal.aborted) {
            entry.failed = true;
            publish();
          }
        });
    }
    publish();
  }, [key]);
  return files.map((file) =>
    chatAttachment(
      file,
      previews[file.ID]?.type === file.MediaType
        ? previews[file.ID].url
        : undefined,
    ),
  );
}
