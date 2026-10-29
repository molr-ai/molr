import { Alert, Button, ChatCopyButton, IconButton } from '@molr/ui';
import { ArrowLeft, Download, File } from 'lucide-react';
import { api } from '@molr/sdk';
import { useCopy } from '../../../../state';
import { Loading } from '../../components/common';
import { size } from './shared';
import type { useWorkspaceDocument } from './useWorkspaceDocument';

type Props = ReturnType<typeof useWorkspaceDocument> & {
  assistantID: string; selected?: string; busy: boolean; setSelected: (path?: string) => void;
  guard: (next: () => void) => void; onSaveAs: () => void; save: () => Promise<void>;
};
export function WorkspacePreview({ assistantID, selected, busy, setSelected, guard, onSaveAs, save, document, draft, setDraft, editing, setEditing, changed, missing, image, originalImage, setOriginalImage, loadFile }: Props) {
  const { locale } = useCopy();
  const t = (cn: string, en: string) => locale === 'zh' ? cn : en;
  return (
  <section
    className="files-preview"
    aria-label={t('文件预览', 'File preview')}
  >
    {!selected ? (
      <div className="files-empty">
        <File size={36} />
        <p>{t('选择文件以查看内容', 'Select a file to preview')}</p>
      </div>
    ) : (
      <>
        <div className="files-filebar">
          <IconButton
            label={t('返回文件列表', 'Back to files')}
            onClick={() =>
              guard(() => {
                setSelected(undefined);
                setEditing(false);
              })
            }
          >
            <ArrowLeft size={17} />
          </IconButton>
          <strong title={selected}>{selected.split('/').at(-1)}</strong>
          {document && !missing && (
            <>
              <a
                className="files-download"
                href={api.workspace.downloadURL(
                  assistantID,
                  document.Path,
                  document.Version,
                )}
                aria-label={t('下载文件', 'Download file')}
              >
                <Download size={18} />
              </a>
              <ChatCopyButton
                text={`/work/${selected}`}
                label={t('复制文件路径', 'Copy file path')}
              />
            </>
          )}
        </div>
        {missing && (
          <Alert
            tone="warning"
            title={t('文件已被移动或删除', 'File moved or deleted')}
          >
            {editing && (
              <Button
                size="sm"
                variant="secondary"
                onClick={() => {
                  onSaveAs();
                }}
              >
                {t('另存为', 'Save as')}
              </Button>
            )}
          </Alert>
        )}
        {changed && (
          <Alert
            tone="warning"
            title={t(
              '文件已发生变化，草稿已保留',
              'File changed. Your draft is preserved.',
            )}
          >
            <Button
              size="sm"
              variant="secondary"
              onClick={() =>
                guard(() => {
                  setEditing(false);
                  if (selected) void loadFile(selected);
                })
              }
            >
              {t('重新加载', 'Reload')}
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => {
                onSaveAs();
              }}
            >
              {t('另存为', 'Save as')}
            </Button>
          </Alert>
        )}
        {!document ? (
          !missing && <Loading />
        ) : (
          <>
            <div className="files-meta">
              {size(document.Size)} ·{' '}
              {new Date(document.ModifiedAt).toLocaleString(locale)}
            </div>
            {document.Editable ||
            document.MediaType?.startsWith('text/plain') ? (
              <>
                <div className="files-edit-actions">
                  {editing ? (
                    <>
                      <Button
                        size="sm"
                        disabled={
                          busy ||
                          missing ||
                          changed ||
                          new TextEncoder().encode(draft).length >
                            1048576
                        }
                        onClick={() => void save()}
                      >
                        {t('保存', 'Save')}
                      </Button>
                      <Button
                        variant="secondary"
                        size="sm"
                        disabled={busy}
                        onClick={() =>
                          guard(() => {
                            setEditing(false);
                            setDraft(document.Text);
                          })
                        }
                      >
                        {t('取消编辑', 'Cancel editing')}
                      </Button>
                    </>
                  ) : (
                    document.Editable &&
                    !missing && (
                      <Button
                        variant="secondary"
                        size="sm"
                        onClick={() => setEditing(true)}
                      >
                        {t('编辑', 'Edit')}
                      </Button>
                    )
                  )}
                  {document.Truncated && (
                    <span>
                      {t(
                        '仅预览前 1 MiB，可下载完整文件',
                        'Showing the first 1 MiB. Download for the complete file.',
                      )}
                    </span>
                  )}
                </div>
                {editing ? (
                  <textarea
                    className="files-editor"
                    aria-label={t('文件内容', 'File content')}
                    spellCheck={false}
                    readOnly={busy}
                    value={draft}
                    onChange={(event) => setDraft(event.target.value)}
                    onKeyDown={(event) => {
                      if (
                        (event.ctrlKey || event.metaKey) &&
                        event.key === 's'
                      ) {
                        event.preventDefault();
                        if (!busy && !missing && !changed) void save();
                      }
                    }}
                  />
                ) : (
                  <pre className="files-text">{document.Text}</pre>
                )}
              </>
            ) : image ? (
              <div className="files-image">
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => setOriginalImage(!originalImage)}
                >
                  {originalImage
                    ? t('适应窗口', 'Fit to window')
                    : t('原始尺寸', 'Original size')}
                </Button>
                <div>
                  <img
                    src={image}
                    alt={document.Name}
                    data-original={originalImage || undefined}
                  />
                </div>
              </div>
            ) : (
              <div className="files-empty">
                <File size={36} />
                <p>
                  {t(
                    '此文件可下载后打开',
                    'Download this file to open it',
                  )}
                </p>
                <span>{document.MediaType}</span>
              </div>
            )}
          </>
        )}
      </>
    )}
  </section>
  );
}
