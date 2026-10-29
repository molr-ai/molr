import { useState } from 'react';
import { Button, Dialog } from '@molr/ui';
import { ArrowLeft, Folder } from 'lucide-react';
import { api } from '@molr/sdk';
import type { WorkspaceEntry } from '@molr/sdk';
import { useCopy } from '../../../../state';
import { useErrorToast } from '../../../../error-toasts';
import { Loading } from '../../components/common';
import { parent, join } from './shared';
import { useWorkspaceDirectory } from './useWorkspaceDirectory';

export function MoveDirectory({
  assistantID,
  entry,
  onClose,
  onMoved,
}: {
  assistantID: string;
  entry: WorkspaceEntry;
  onClose: () => void;
  onMoved: (path: string) => Promise<void>;
}) {
  const { locale } = useCopy(),
    zh = locale === 'zh';
  const [path, setPath] = useState('.'),
    [error, setError] = useState<unknown>(),
    [busy, setBusy] = useState(false);
  const query = useWorkspaceDirectory(assistantID, path, busy, false);
  const { listing, loadList } = query;
  useErrorToast(error || query.error);
  return (
    <Dialog
      open
      title={zh ? '移动到文件夹' : 'Move to folder'}
      onOpenChange={(open) => {
        if (!open && !busy) onClose();
      }}
      dismissible={!busy}
      footer={
        <>
          <Button variant="secondary" disabled={busy} onClick={onClose}>
            {zh ? '取消' : 'Cancel'}
          </Button>
          <Button
            disabled={busy || path === parent(entry.Path) || !listing}
            onClick={async () => {
              setBusy(true);
              try {
                const to = join(path, entry.Name);
                await api.workspace.change(assistantID, {
                  Action: 'move',
                  Path: entry.Path,
                  ToPath: to,
                  ExpectedVersion: entry.Version,
                });
                await onMoved(to);
              } catch (err) {
                setError(err);
              } finally {
                setBusy(false);
              }
            }}
          >
            {zh ? '移动到这里' : 'Move here'}
          </Button>
        </>
      }
    >
      <div className="files-dest">
        <Button
          variant="ghost"
          disabled={path === '.' || busy}
          onClick={() => setPath(parent(path))}
        >
          <ArrowLeft size={16} />
          {zh ? '上一级' : 'Parent folder'}
        </Button>
        <p>/work{path === '.' ? '' : `/${path}`}</p>
        {!listing ? (
          <Loading />
        ) : (
          <>
            {listing.Entries.filter(
              (e) =>
                e.Type === 'directory' &&
                e.Path !== entry.Path &&
                !e.Path.startsWith(entry.Path + '/'),
            ).map((e) => (
              <Button
                key={e.Path}
                variant="ghost"
                disabled={busy}
                onClick={() => setPath(e.Path)}
              >
                <Folder size={16} />
                {e.Name}
              </Button>
            ))}
            {listing.Truncated && (
              <Button
                variant="secondary"
                onClick={() => void loadList(true)}
              >
                {zh ? '加载更多' : 'Load more'}
              </Button>
            )}
          </>
        )}
      </div>
    </Dialog>
  );
}
