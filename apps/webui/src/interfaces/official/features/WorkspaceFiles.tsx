import { useEffect, useMemo, useRef, useState } from 'react';
import type { RefObject } from 'react';
import {
  Button,
  copyTextToClipboard,
  Dialog,
  Drawer,
  DropdownMenu,
  IconButton,
  ModalPresence,
  useBreakpoint,
  useToast,
} from '@molr/ui';
import {
  ArrowLeft,
  Ellipsis,
  FilePlus,
  FolderPlus,
  GripHorizontal,
  PanelRight,
  PictureInPicture2,
  RefreshCw,
  Upload,
  X,
} from 'lucide-react';
import type {
  WorkspaceEntry,
} from '@molr/sdk';
import { api, APIError } from '@molr/sdk';
import { useCopy } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { useFloatingPanel } from './useFloatingPanel';
import type { FloatingBounds } from './useFloatingPanel';
import { Confirm } from '../components/common';
import { parent, join, validName } from './workspace-files/shared';
import type { ExplorerMemory } from './workspace-files/shared';
import { useWorkspaceDirectory } from './workspace-files/useWorkspaceDirectory';
import { useWorkspaceDocument } from './workspace-files/useWorkspaceDocument';
import { useWorkspaceUploads } from './workspace-files/useWorkspaceUploads';
import { WorkspaceDirectory } from './workspace-files/WorkspaceDirectory';
import { WorkspacePreview } from './workspace-files/WorkspacePreview';
import { MoveDirectory } from './workspace-files/MoveDirectory';
import './workspace-files.css';

export type { ExplorerMemory } from './workspace-files/shared';
type Form = {
  kind: 'file' | 'folder' | 'rename' | 'saveAs' | 'move';
  entry?: WorkspaceEntry;
};

export function WorkspaceFiles({
  assistantID,
  assistantName,
  docked,
  floating,
  onFloatingChange,
  bounds,
  memory,
  onClose,
}: {
  assistantID: string;
  assistantName: string;
  docked: boolean;
  floating: boolean;
  onFloatingChange: (value: boolean) => void;
  bounds: RefObject<FloatingBounds | null>;
  memory: ExplorerMemory;
  onClose: () => void;
}) {
  const { locale } = useCopy(),
    zh = locale === 'zh',
    mobile = useBreakpoint('mobile');
  const { toast } = useToast();
  const t = (cn: string, en: string) => (zh ? cn : en);
  const [directory, setDirectory] = useState(memory.directory),
    [selected, setSelected] = useState(memory.selected);
  const [error, setError] = useState<unknown>();
  const [hidden, setHidden] = useState(memory.hidden), [sort, setSort] = useState(memory.sort);
  const [busy, setBusy] = useState(false), [form, setForm] = useState<Form>(), [name, setName] = useState(''), [deleting, setDeleting] = useState<WorkspaceEntry>();
  const [pending, setPending] = useState<() => void>();
  const listRef = useRef<HTMLDivElement>(null), inputRef = useRef<HTMLInputElement>(null), running = useRef(false), mounted = useRef(true);
  const directoryQuery = useWorkspaceDirectory(assistantID, directory, busy);
  const { listing, loading, loadList } = directoryQuery;
  const editor = useWorkspaceDocument(assistantID, selected, busy, memory.draft);
  const { document, setDocument, draft, editing, setEditing, setChanged, loadFile } = editor;
  const uploadsQuery = useWorkspaceUploads(assistantID, directory, loadList);
  const { uploads, setUploads, collision } = uploadsQuery;
  const upload = (files: FileList | globalThis.File[]) => run(() => uploadsQuery.upload(files));
  const latest = useRef({ editing }); latest.current = { editing };
  const view = useFloatingPanel(floating, bounds);
  const dirty = editing && draft !== document?.Text;
  useErrorToast(error || directoryQuery.error || editor.error);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  useEffect(() => {
    memory.directory = directory;
    memory.selected = selected;
    memory.hidden = hidden;
    memory.sort = sort;
    memory.draft =
      dirty && document ? { ...document, value: draft } : undefined;
  }, [directory, selected, hidden, sort, dirty, document, draft, memory]);
  function guard(next: () => void) {
    if (dirty || busy) setPending(() => next);
    else next();
  }
  useEffect(() => {
    const leave = (event: Event) => {
      if (!dirty && !busy) return;
      event.preventDefault();
      setPending(() => (event as CustomEvent<() => void>).detail);
    };
    const unload = (event: BeforeUnloadEvent) => {
      if (dirty || busy) {
        event.preventDefault();
        event.returnValue = '';
      }
    };
    window.addEventListener('molr:before-navigation', leave);
    window.addEventListener('beforeunload', unload);
    return () => {
      window.removeEventListener('molr:before-navigation', leave);
      window.removeEventListener('beforeunload', unload);
    };
  }, [dirty, busy]);
  useEffect(() => {
    if (listRef.current && !loading) listRef.current.scrollTop = memory.scroll;
  }, [loading, memory]);
  function navigate(path: string) {
    guard(() => {
      memory.scroll = 0;
      setEditing(false);
      setSelected(undefined);
      setDocument(undefined);
      setDirectory(path);
    });
  }
  function open(entry: WorkspaceEntry) {
    if (entry.Type === 'directory') navigate(entry.Path);
    else
      guard(() => {
        setDocument(undefined);
        setEditing(false);
        setSelected(entry.Path);
      });
  }
  async function run(work: () => Promise<void>) {
    if (running.current) return;
    running.current = true;
    setBusy(true);
    setError(undefined);
    try {
      await work();
    } catch (err) {
      if (
        err instanceof APIError &&
        err.code === 'conflict' &&
        latest.current.editing
      )
        setChanged(true);
      else setError(err);
    } finally {
      running.current = false;
      if (mounted.current) setBusy(false);
    }
  }
  function startForm(value: Form) {
    guard(() => {
      setForm(value);
      setName(value.kind === 'rename' ? value.entry!.Name : '');
    });
  }
  async function submit() {
    if (!form || !validName(name)) return;
    const destination = join(
      form.entry ? parent(form.entry.Path) : directory,
      name,
    );
    await run(async () => {
      if (form.kind === 'rename')
        await api.workspace.change(assistantID, {
          Action: 'move',
          Path: form.entry!.Path,
          ToPath: destination,
          ExpectedVersion: form.entry!.Version,
        });
      else
        await api.workspace.change(assistantID, {
          Action: form.kind === 'folder' ? 'mkdir' : 'write',
          Path: destination,
          Text: form.kind === 'saveAs' ? draft : '',
        });
      setForm(undefined);
      setEditing(false);
      setSelected(form.kind === 'folder' ? undefined : destination);
      await loadList();
      if (form.kind !== 'folder') await loadFile(destination);
    });
  }
  async function save() {
    if (!document) return;
    await run(async () => {
      await api.workspace.change(assistantID, {
        Action: 'write',
        Path: document.Path,
        Text: draft,
        ExpectedVersion: document.Version,
      });
      setEditing(false);
      await loadFile(document.Path);
      await loadList();
    });
  }
  const entries = useMemo(() => (listing?.Entries ?? [])
    .filter((e) => hidden || !e.Name.startsWith('.'))
    .sort(
      (a, b) =>
        Number(b.Type === 'directory') - Number(a.Type === 'directory') ||
        (sort === 'modified'
          ? Date.parse(b.ModifiedAt) - Date.parse(a.ModifiedAt)
          : sort === 'size'
            ? b.Size - a.Size
            : a.Name.localeCompare(b.Name, locale)),
    ), [listing?.Entries, hidden, sort, locale]);
  function menu(entry: WorkspaceEntry) {
    return [
      {
        id: '重命名',
        label: t('重命名', 'Rename'),
        onSelect: () => startForm({ kind: 'rename', entry }),
      },
      {
        id: '移动到…',
        label: t('移动到…', 'Move to…'),
        onSelect: () => guard(() => setForm({ kind: 'move', entry })),
      },
      {
        id: '复制路径',
        label: t('复制路径', 'Copy path'),
        onSelect: () =>
          void run(async () => {
            await copyTextToClipboard(`/work/${entry.Path}`);
            toast({ title: t('已复制路径', 'Path copied') });
          }),
      },
      ...(entry.Type === 'file'
        ? [
            {
              id: '下载',
              label: t('下载', 'Download'),
              onSelect: () => {
                window.location.assign(
                  api.workspace.downloadURL(
                    assistantID,
                    entry.Path,
                    entry.Version,
                  ),
                );
              },
            },
          ]
        : []),
      {
        id: '删除',
        label: t('删除', 'Delete'),
        onSelect: () => guard(() => setDeleting(entry)),
        danger: true,
      },
    ];
  }
  const actions = useRef({ open, menu, startForm }); actions.current = { open, menu, startForm };
  const listActions = useMemo(() => ({
    onOpen: (entry: WorkspaceEntry) => actions.current.open(entry),
    onMenu: (entry: WorkspaceEntry) => actions.current.menu(entry).map(item => ({
      ...item,
      onSelect: () => actions.current.menu(entry).find(current => current.id === item.id)?.onSelect(),
    })),
    onNewFolder: () => actions.current.startForm({ kind: 'folder' }),
    onUpload: () => inputRef.current?.click(),
    onScroll: (scroll: number) => { memory.scroll = scroll; },
  }), [memory]);
  const title = `${t('资源管理器', 'File explorer')} · ${assistantName}`;
  return (
    <Drawer
      open
      title={title}
      side={mobile ? 'bottom' : 'right'}
      docked={floating || docked}
      onOpenChange={(open) => {
        if (!open) guard(onClose);
      }}
      showCloseButton={false}
      className={`files-drawer${floating ? ' files-drawer--floating' : ''}`}
      size="lg"
      headerContent={
        <div className="files-heading" {...view.drag}>
          {floating && (
            <IconButton
              className="files-move"
              label={t(
                '移动资源管理器（方向键微调）',
                'Move file explorer (arrow keys)',
              )}
              onKeyDown={view.moveKey}
            >
              <GripHorizontal size={18} />
            </IconButton>
          )}
          <span title={title}>{title}</span>
          <div className="files-actions">
            {!mobile && (
              <IconButton
                label={
                  floating
                    ? t('停靠侧栏', 'Dock to sidebar')
                    : t('浮窗模式', 'Floating window')
                }
                onClick={() => onFloatingChange(!floating)}
              >
                {floating ? (
                  <PanelRight size={18} />
                ) : (
                  <PictureInPicture2 size={18} />
                )}
              </IconButton>
            )}
            <IconButton
              label={t('关闭资源管理器', 'Close file explorer')}
              onClick={() => guard(onClose)}
            >
              <X size={18} />
            </IconButton>
          </div>
        </div>
      }
    >
      <div
        ref={view.ref}
        className="files-panel"
        onDragOver={(event) => {
          if (event.dataTransfer.types.includes('Files')) {
            event.preventDefault();
            event.dataTransfer.dropEffect = busy ? 'none' : 'copy';
          }
        }}
        onDrop={(event) => {
          event.preventDefault();
          if (!busy && event.dataTransfer.files.length)
            void upload(event.dataTransfer.files);
        }}
      >
        <div className="files-toolbar">
          <IconButton
            label={t('上一级', 'Parent folder')}
            disabled={directory === '.' || busy}
            onClick={() => navigate(parent(directory))}
          >
            <ArrowLeft size={17} />
          </IconButton>
          <nav
            className="files-breadcrumbs"
            aria-label={t('工作区路径', 'Workspace path')}
          >
            <button onClick={() => navigate('.')}>/work</button>
            {directory !== '.' &&
              directory.split('/').map((part, i, parts) => (
                <span key={i}>
                  /
                  <button
                    title={part}
                    onClick={() => navigate(parts.slice(0, i + 1).join('/'))}
                  >
                    {part}
                  </button>
                </span>
              ))}
          </nav>
          <IconButton
            label={t('刷新文件', 'Refresh files')}
            disabled={busy}
            onClick={() => {
              void loadList();
              if (selected) void loadFile(selected, true);
            }}
          >
            <RefreshCw size={17} />
          </IconButton>
          <IconButton
            label={t('新建文件', 'New file')}
            disabled={busy}
            onClick={() => startForm({ kind: 'file' })}
          >
            <FilePlus size={17} />
          </IconButton>
          <IconButton
            label={t('新建文件夹', 'New folder')}
            disabled={busy}
            onClick={() => startForm({ kind: 'folder' })}
          >
            <FolderPlus size={17} />
          </IconButton>
          <IconButton
            label={t('上传文件', 'Upload files')}
            disabled={busy}
            onClick={() => inputRef.current?.click()}
          >
            <Upload size={17} />
          </IconButton>
          <DropdownMenu
            label={t('文件选项', 'File options')}
            align="end"
            trigger={
              <IconButton label={t('文件选项', 'File options')}>
                <Ellipsis size={17} />
              </IconButton>
            }
            items={[
              {
                id: crypto.randomUUID(),
                label: hidden
                  ? t('隐藏点文件', 'Hide hidden files')
                  : t('显示隐藏文件', 'Show hidden files'),
                onSelect: () => setHidden(!hidden),
              },
              {
                id: '按名称排序',
                label: t('按名称排序', 'Sort by name'),
                onSelect: () => setSort('name'),
              },
              {
                id: '按修改时间排序',
                label: t('按修改时间排序', 'Sort by modified time'),
                onSelect: () => setSort('modified'),
              },
              {
                id: '按大小排序',
                label: t('按大小排序', 'Sort by size'),
                onSelect: () => setSort('size'),
              },
            ]}
          />
          <input
            ref={inputRef}
            type="file"
            multiple
            hidden
            onChange={(event) => {
              if (event.target.files) void upload(event.target.files);
              event.target.value = '';
            }}
          />
        </div>
        <div className="files-content" data-detail={!!selected || undefined}>
          <WorkspaceDirectory listRef={listRef} locale={locale} loading={loading} listing={listing} entries={entries} selected={selected} busy={busy} loadList={loadList} {...listActions} />
          <WorkspacePreview {...editor} assistantID={assistantID} selected={selected} busy={busy} setSelected={setSelected} guard={guard} save={save} onSaveAs={() => { setName(''); setForm({ kind: 'saveAs' }); }} />
        </div>
        {!!uploads.length && (
          <div className="files-uploads" aria-live="polite">
            {uploads.map((item, i) => (
              <div key={i}>
                <span>{item.name}</span>
                <span>{item.state}</span>
              </div>
            ))}
            {!busy && (
              <Button size="sm" variant="ghost" onClick={() => setUploads([])}>
                {t('收起', 'Dismiss')}
              </Button>
            )}
          </div>
        )}
        <footer className="files-footer">
          {listing
            ? `${entries.length}${listing.Truncated ? '+' : ''} ${t('项', 'items')}`
            : '/work'}
          {busy && <span role="status">{t('正在处理…', 'Working…')}</span>}
        </footer>
        {floating && (
          <button
            className="files-resize"
            aria-label={t(
              '调整资源管理器大小（方向键微调）',
              'Resize file explorer (arrow keys)',
            )}
            {...view.resize}
          />
        )}
      </div>
      <ModalPresence>
        {pending && (
          <Dialog
            open
            title={
              busy
                ? t('正在处理文件', 'File operation in progress')
                : t('有未保存的修改', 'Unsaved changes')
            }
            onOpenChange={(open) => {
              if (!open) setPending(undefined);
            }}
            footer={
              <>
                <Button
                  variant="secondary"
                  onClick={() => setPending(undefined)}
                >
                  {t('继续处理', 'Keep working')}
                </Button>
                {!busy && (
                  <Button
                    variant="danger"
                    onClick={() => {
                      const next = pending;
                      setPending(undefined);
                      setEditing(false);
                      memory.draft = undefined;
                      next();
                    }}
                  >
                    {t('放弃修改', 'Discard changes')}
                  </Button>
                )}
              </>
            }
          >
            {busy
              ? t(
                  '请等待当前操作完成。',
                  'Wait for the current operation to finish.',
                )
              : t('离开将放弃当前修改。', 'Leaving will discard your changes.')}
          </Dialog>
        )}
      </ModalPresence>
      <ModalPresence>
        {collision && (
          <Dialog
            open
            title={t('同名文件已存在', 'File already exists')}
            description={collision.name}
            onOpenChange={(open) => {
              if (!open) collision.resolve('skip');
            }}
            footer={
              <>
                <Button
                  variant="secondary"
                  onClick={() => collision.resolve('skip')}
                >
                  {t('跳过', 'Skip')}
                </Button>
                <Button
                  variant="secondary"
                  onClick={() => collision.resolve('replace')}
                >
                  {t('替换', 'Replace')}
                </Button>
                <Button onClick={() => collision.resolve('both')}>
                  {t('保留两份', 'Keep both')}
                </Button>
              </>
            }
          />
        )}
      </ModalPresence>
      <ModalPresence>
        {form && form.kind !== 'move' && (
          <Dialog
            open
            title={
              form.kind === 'folder'
                ? t('新建文件夹', 'New folder')
                : form.kind === 'rename'
                  ? t('重命名', 'Rename')
                  : form.kind === 'saveAs'
                    ? t('另存为', 'Save as')
                    : t('新建文件', 'New file')
            }
            onOpenChange={(open) => {
              if (!open && !busy) setForm(undefined);
            }}
            dismissible={!busy}
          >
            <form
              className="files-form"
              onSubmit={(event) => {
                event.preventDefault();
                void submit();
              }}
            >
              <label>
                {t('名称', 'Name')}
                <input
                  autoFocus
                  disabled={busy}
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                />
              </label>
              <div className="form-actions">
                <Button
                  variant="secondary"
                  disabled={busy}
                  onClick={() => setForm(undefined)}
                >
                  {t('取消', 'Cancel')}
                </Button>
                <Button type="submit" disabled={!validName(name) || busy}>
                  {t('确认', 'Confirm')}
                </Button>
              </div>
            </form>
          </Dialog>
        )}
      </ModalPresence>
      <ModalPresence>
        {form?.kind === 'move' && (
          <MoveDirectory
            assistantID={assistantID}
            entry={form.entry!}
            onClose={() => setForm(undefined)}
            onMoved={async () => {
              setForm(undefined);
              setEditing(false);
              if (selected === form.entry!.Path) setSelected(undefined);
              await loadList();
            }}
          />
        )}
      </ModalPresence>
      <ModalPresence>
        {deleting && (
          <Confirm
            title={t('删除文件', 'Delete entry')}
            description={`${t('永久删除', 'Permanently delete')} /work/${deleting.Path}${deleting.Type === 'directory' ? t('（仅支持空文件夹）', ' (empty folders only)') : ''}?`}
            danger
            onClose={() => setDeleting(undefined)}
            onConfirm={async () => {
              await api.workspace.change(assistantID, {
                Action: 'delete',
                Path: deleting.Path,
                ExpectedVersion: deleting.Version,
              });
              if (selected === deleting.Path) {
                setSelected(undefined);
                setEditing(false);
              }
              await loadList();
            }}
          />
        )}
      </ModalPresence>
    </Drawer>
  );
}
