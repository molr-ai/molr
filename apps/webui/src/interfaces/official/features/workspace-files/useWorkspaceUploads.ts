import { useEffect, useRef, useState } from 'react';
import { api, APIError, mutationKey } from '@molr/sdk';
import type { WorkspaceEntry } from '@molr/sdk';
import { useCopy } from '../../../../state';
import { join } from './shared';

type UploadStatus = { name: string; state: string };
type Collision = { name: string; resolve: (choice: 'replace' | 'both' | 'skip') => void };
export function useWorkspaceUploads(assistantID: string, directory: string, reload: () => Promise<void>) {
  const { locale, t: copy } = useCopy();
  const t = (cn: string, en: string) => locale === 'zh' ? cn : en;
  const [uploads, setUploads] = useState<UploadStatus[]>([]), [collision, setCollision] = useState<Collision>();
  const mounted = useRef(true), collisionRef = useRef(collision), controller = useRef<AbortController | undefined>(undefined);
  collisionRef.current = collision;
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; controller.current?.abort(); collisionRef.current?.resolve('skip'); };
  }, []);
  async function upload(files: FileList | globalThis.File[]) {
    controller.current = new AbortController();
    const signal = controller.current.signal;
    const target = directory,
      batch = Array.from(files);
    try {
      const states = batch.map((file) => ({
        name: file.name,
        state: t('等待中', 'Queued'),
      }));
      setUploads([...states]);
      for (let i = 0; i < batch.length && mounted.current; i++) {
        const file = batch[i];
        let path = join(target, file.name),
          version = '';
        try {
          let existing: WorkspaceEntry | undefined;
          try {
            existing = await api.workspace.stat(assistantID, path, signal);
          } catch (err) {
            if (!(err instanceof APIError && err.code === 'not_found'))
              throw err;
          }
          if (existing) {
            const choice = await new Promise<'replace' | 'both' | 'skip'>(
              (resolve) => { if (mounted.current) setCollision({ name: file.name, resolve }); else resolve('skip'); },
            );
            setCollision(undefined);
            if (choice === 'skip') {
              states[i].state = t('已跳过', 'Skipped');
              setUploads([...states]);
              continue;
            }
            if (choice === 'replace') {
              if (existing.Type !== 'file') throw new APIError('conflict');
              version = existing.Version;
            } else {
              const dot = file.name.lastIndexOf('.'),
                stem = dot > 0 ? file.name.slice(0, dot) : file.name,
                ext = dot > 0 ? file.name.slice(dot) : '';
              path = join(
                target,
                `${stem}-${mutationKey().slice(0, 8)}${ext}`,
              );
            }
          }
          states[i].state = t('上传中…', 'Uploading…');
          setUploads([...states]);
          await api.workspace.upload(assistantID, path, file, version, signal);
          states[i].state = t('已完成', 'Done');
        } catch (err) {
          states[i].state =
            err instanceof APIError
              ? `${t('失败', 'Failed')}: ${copy.errorReasons[err.reason as keyof typeof copy.errorReasons] ?? copy.errors[err.code as keyof typeof copy.errors] ?? copy.errors.unavailable}`
              : t('上传失败', 'Upload failed');
        }
        if (mounted.current) setUploads([...states]);
      }
      if (mounted.current) await reload();
    } finally { controller.current = undefined; }
  }
  return { upload, uploads, setUploads, collision };
}
