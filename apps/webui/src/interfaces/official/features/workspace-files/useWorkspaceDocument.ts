import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { api, APIError } from '@molr/sdk';
import type { WorkspaceText } from '@molr/sdk';
import { poll, retryDelay } from '../../../../polling';
import { workspaceReader } from './read';

export function useWorkspaceDocument(assistantID: string, selected: string | undefined, busy: boolean, restored?: WorkspaceText & { value: string }) {
  const [document, setDocument] = useState<WorkspaceText | undefined>(restored);
  const [draft, setDraft] = useState(restored?.value ?? ''), [editing, setEditing] = useState(!!restored);
  const [changed, setChanged] = useState(false), [missing, setMissing] = useState(false), [error, setError] = useState<unknown>();
  const [image, setImage] = useState(''), [originalImage, setOriginalImage] = useState(false);
  const latest = useRef({ document, editing, busy });
  latest.current = { document, editing, busy };
  const reader = useMemo(workspaceReader, []);
  const pollingLoop = useRef<ReturnType<typeof poll> | undefined>(undefined), halted = useRef(false);
  const read = useCallback((path: string, polling: boolean, replace: boolean, signal?: AbortSignal) => reader.run(async signal => {
    try {
      // Both stat and read use the same Runtime version. Editing needs no file body.
      if (polling && latest.current.editing) {
        const next = await api.workspace.stat(assistantID, path, signal);
        if (!signal.aborted) { setMissing(false); setChanged(next.Version !== latest.current.document?.Version); setError(undefined); }
        return;
      }
      const next = await api.workspace.read(assistantID, path, signal);
      if (signal.aborted) return;
      // Entering edit mode while a preview read is in flight must keep the draft.
      if (polling && latest.current.editing) setChanged(next.Version !== latest.current.document?.Version);
      else { setDocument(next); setDraft(next.Text); setChanged(false); }
      setMissing(false); setError(undefined);
    } catch (error) {
      if (!signal.aborted) {
        if (error instanceof APIError && error.code === 'not_found') { setMissing(true); return; }
        setError(error);
      }
      throw error;
    }
  }, replace, signal), [assistantID, reader]);
  const loadFile = useCallback((path: string, polling = false) => read(path, polling, true).then(() => {
    if (halted.current) { halted.current = false; pollingLoop.current?.request(true); }
  }).catch(() => {}), [read]);
  useEffect(() => {
    setMissing(false); setChanged(false); setError(undefined);
    if (!selected) { reader.cancel(); setDocument(undefined); return; }
    void loadFile(selected, latest.current.editing && latest.current.document?.Path === selected);
    const loop = poll(async signal => {
      try {
        if (!latest.current.busy) await read(selected, true, false, signal);
      } catch (error) {
        halted.current = retryDelay(error, 1) === false;
        throw error;
      }
      return 5000;
    }, false);
    pollingLoop.current = loop; halted.current = false;
    loop.reschedule(5000);
    const focus = () => loop.request();
    window.addEventListener('focus', focus);
    return () => { loop.stop(); pollingLoop.current = undefined; reader.cancel(); window.removeEventListener('focus', focus); };
  }, [selected, loadFile, read, reader]);
  useEffect(() => {
    setImage(''); setOriginalImage(false);
    if (!document || !['image/png', 'image/jpeg', 'image/gif', 'image/webp'].includes(document.MediaType ?? '') || document.Size > 20 * 1024 * 1024) return;
    const controller = new AbortController(); let url = '';
    api.workspace.download(assistantID, document.Path, document.Version, controller.signal).then(blob => {
      if (!controller.signal.aborted) { url = URL.createObjectURL(new Blob([blob], { type: document.MediaType })); setImage(url); }
    }).catch(error => { if (!controller.signal.aborted) setError(error); });
    return () => { controller.abort(); if (url) URL.revokeObjectURL(url); };
  }, [assistantID, document?.Path, document?.Version]);
  return { document, setDocument, draft, setDraft, editing, setEditing, changed, setChanged, missing, image, originalImage, setOriginalImage, error, loadFile };
}
