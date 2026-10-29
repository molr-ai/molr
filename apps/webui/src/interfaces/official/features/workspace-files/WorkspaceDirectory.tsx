import { memo } from 'react';
import type { RefObject } from 'react';
import { Button, DropdownMenu, IconButton } from '@molr/ui';
import type { MenuItem } from '@molr/ui';
import { Ellipsis, File, Folder } from 'lucide-react';
import type { WorkspaceEntry, WorkspaceListing } from '@molr/sdk';
import { Loading } from '../../components/common';
import { size } from './shared';

type Props = {
  listRef: RefObject<HTMLDivElement | null>; locale: string; loading: boolean;
  listing?: WorkspaceListing; entries: WorkspaceEntry[]; selected?: string; busy: boolean;
  loadList: (more?: boolean) => Promise<void>; onScroll: (value: number) => void;
  onUpload: () => void; onNewFolder: () => void; onOpen: (entry: WorkspaceEntry) => void;
  onMenu: (entry: WorkspaceEntry) => MenuItem[];
};
export const WorkspaceDirectory = memo(function WorkspaceDirectory({ listRef, locale, loading, listing, entries, selected, busy, loadList, onScroll, onUpload, onNewFolder, onOpen, onMenu }: Props) {
  const t = (cn: string, en: string) => locale === 'zh' ? cn : en;
  return (
  <div
    className="files-list"
    ref={listRef}
    onScroll={(e) => {
      onScroll(e.currentTarget.scrollTop);
    }}
    aria-label={t('文件列表', 'Files')}
  >
    {loading ? (
      <Loading />
    ) : !listing ? (
      <Button variant="secondary" onClick={() => void loadList()}>
        {t('重试', 'Retry')}
      </Button>
    ) : (
      <>
        {entries.length === 0 && (
          <div className="files-empty">
            <Folder size={36} />
            <p>{t('此文件夹为空', 'This folder is empty')}</p>
            <Button
              variant="secondary"
              size="sm"
              disabled={busy}
              onClick={() => onUpload()}
            >
              {t('上传文件', 'Upload files')}
            </Button>
            <Button
              variant="ghost"
              size="sm"
              disabled={busy}
              onClick={() => onNewFolder()}
            >
              {t('新建文件夹', 'New folder')}
            </Button>
          </div>
        )}
        {entries.map((entry) => (
          <div
            className="files-row"
            data-selected={selected === entry.Path || undefined}
            key={entry.Path}
          >
            <button
              className="files-entry"
              disabled={busy || entry.Type === 'unavailable'}
              onClick={() => onOpen(entry)}
              title={`/work/${entry.Path}`}
            >
              {entry.Type === 'directory' ? (
                <Folder size={18} />
              ) : (
                <File size={18} />
              )}
              <span>
                <strong>{entry.Name}</strong>
                <small>
                  {entry.Type === 'unavailable'
                    ? t(
                        '链接或不可访问条目',
                        'Link or unavailable entry',
                      )
                    : `${entry.Type === 'file' ? size(entry.Size) + ' · ' : ''}${new Date(entry.ModifiedAt).toLocaleString(locale)}`}
                </small>
              </span>
            </button>
            {entry.Type !== 'unavailable' && !busy && (
              <DropdownMenu
                label={`${t('操作', 'Actions')}: ${entry.Name}`}
                align="end"
                trigger={
                  <IconButton
                    label={`${t('操作', 'Actions')}: ${entry.Name}`}
                  >
                    <Ellipsis size={16} />
                  </IconButton>
                }
                items={onMenu(entry)}
              />
            )}
          </div>
        ))}
        {listing.Truncated && (
          <Button
            variant="secondary"
            size="sm"
            disabled={busy}
            onClick={() => void loadList(true)}
          >
            {t('加载更多', 'Load more')}
          </Button>
        )}
      </>
    )}
  </div>
  );
});
