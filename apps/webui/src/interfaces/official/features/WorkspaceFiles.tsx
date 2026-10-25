import { useCallback, useEffect, useRef, useState } from 'react';
import type { RefObject } from 'react';
import {
  Alert,
  Button,
  ChatCopyButton,
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
  Download,
  Ellipsis,
  File,
  FilePlus,
  Folder,
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
  WorkspaceListing,
  WorkspaceText,
} from '@molr/sdk';
import { api, APIError } from '@molr/sdk';
import { useCopy } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { useFloatingPanel } from './useFloatingPanel';
import type { FloatingBounds } from './useFloatingPanel';
import { Confirm, Loading } from '../components/common';
import './workspace-files.css';

export type ExplorerMemory = {
  directory: string;
  selected?: string;
  scroll: number;
  hidden: boolean;
  sort: string;
  draft?: WorkspaceText & { value: string };
};
const parent = (path: string) =>
  path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : '.';
const join = (dir: string, name: string) =>
  dir === '.' ? name : `${dir}/${name}`;
const size = (bytes: number) =>
  bytes < 1024
    ? `${bytes} B`
    : bytes < 1024 * 1024
      ? `${(bytes / 1024).toFixed(1)} KB`
      : `${(bytes / 1024 / 1024).toFixed(1)} MB`;
const validName = (value: string) =>
  !!value.trim() &&
  value !== '.' &&
  value !== '..' &&
  !/[\\/\x00-\x1f]/.test(value);
type Form = {
  kind: 'file' | 'folder' | 'rename' | 'saveAs' | 'move';
  entry?: WorkspaceEntry;
};
type UploadStatus = { name: string; state: string };

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
  const { locale, t: copy } = useCopy(),
    zh = locale === 'zh',
    mobile = useBreakpoint('mobile');
  const { toast } = useToast();
  const t = (cn: string, en: string) => (zh ? cn : en);
  const [directory, setDirectory] = useState(memory.directory),
    [selected, setSelected] = useState(memory.selected);
  const [listing, setListing] = useState<WorkspaceListing>(),
    [loading, setLoading] = useState(true),
    [error, setError] = useState<unknown>();
  const [hidden, setHidden] = useState(memory.hidden),
    [sort, setSort] = useState(memory.sort);
  const [document, setDocument] = useState<WorkspaceText | undefined>(
      memory.draft,
    ),
    [draft, setDraft] = useState(memory.draft?.value ?? ''),
    [editing, setEditing] = useState(!!memory.draft);
  const [changed, setChanged] = useState(false),
    [missing, setMissing] = useState(false),
    [image, setImage] = useState(''),
    [originalImage, setOriginalImage] = useState(false);
  const [busy, setBusy] = useState(false),
    [form, setForm] = useState<Form>(),
    [name, setName] = useState(''),
    [deleting, setDeleting] = useState<WorkspaceEntry>();
  const [pending, setPending] = useState<() => void>(),
    [uploads, setUploads] = useState<UploadStatus[]>([]);
  const [collision, setCollision] = useState<{
    name: string;
    resolve: (choice: 'replace' | 'both' | 'skip') => void;
  }>();
  const listRef = useRef<HTMLDivElement>(null),
    inputRef = useRef<HTMLInputElement>(null),
    serial = useRef(0),
    listSerial = useRef(0),
    running = useRef(false);
  const latest = useRef({
    editing,
    draft,
    document,
    busy,
    directory,
    selected,
  });
  latest.current = { editing, draft, document, busy, directory, selected };
  const mounted = useRef(true),
    collisionRef = useRef(collision);
  collisionRef.current = collision;
  const view = useFloatingPanel(floating, bounds);
  const dirty = editing && draft !== document?.Text;
  useErrorToast(error);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      serial.current++;
      listSerial.current++;
      collisionRef.current?.resolve('skip');
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
  const loadList = useCallback(
    async (more = false) => {
      const ticket = ++listSerial.current;
      try {
        const old = more ? listing : undefined;
        const next = await api.workspace.list(
          assistantID,
          directory,
          old?.NextOffset ?? 0,
          old?.Version ?? '',
        );
        if (!mounted.current || ticket !== listSerial.current) return;
        setListing(
          old ? { ...next, Entries: [...old.Entries, ...next.Entries] } : next,
        );
        setError(undefined);
      } catch (err) {
        if (mounted.current && ticket === listSerial.current) {
          setError(err);
          if (err instanceof APIError && err.code === 'conflict')
            setListing(undefined);
        }
      } finally {
        if (mounted.current && ticket === listSerial.current) setLoading(false);
      }
    },
    [assistantID, directory, listing],
  );
  const reloadRef = useRef(loadList);
  reloadRef.current = loadList;
  useEffect(() => {
    setLoading(true);
    setListing(undefined);
    void reloadRef.current();
  }, [directory]);
  useEffect(() => {
    if (listRef.current && !loading) listRef.current.scrollTop = memory.scroll;
  }, [loading, memory]);
  const loadFile = useCallback(
    async (path: string, polling = false) => {
      const ticket = ++serial.current;
      try {
        const next = await api.workspace.read(assistantID, path);
        if (!mounted.current || ticket !== serial.current) return;
        const current = latest.current;
        if (polling && current.editing) {
          setMissing(false);
          setChanged(next.Version !== current.document?.Version);
          return;
        }
        setDocument(next);
        setDraft(next.Text);
        setChanged(false);
        setMissing(false);
      } catch (err) {
        if (!mounted.current || ticket !== serial.current) return;
        if (err instanceof APIError && err.code === 'not_found')
          setMissing(true);
        else setError(err);
      }
    },
    [assistantID],
  );
  useEffect(() => {
    setMissing(false);
    setChanged(false);
    if (selected)
      void loadFile(selected, !!memory.draft && memory.draft.Path === selected);
    else {
      serial.current++;
      setDocument(undefined);
    }
  }, [selected, loadFile]);
  useEffect(() => {
    const check = () => {
      if (window.document.visibilityState !== 'visible' || latest.current.busy)
        return;
      // Refresh loaded pages only after an explicit request. Background refresh
      // preserves pagination and scroll; selected files have independent versions.
      if (!listing?.Truncated && listing && listing.Entries.length <= 100)
        void reloadRef.current();
      if (latest.current.selected) void loadFile(latest.current.selected, true);
    };
    const timer = setInterval(check, 5000);
    window.addEventListener('focus', check);
    return () => {
      clearInterval(timer);
      window.removeEventListener('focus', check);
    };
  }, [loadFile, listing?.Truncated, listing?.Entries.length]);
  useEffect(() => {
    setImage('');
    setOriginalImage(false);
    if (
      !document ||
      !['image/png', 'image/jpeg', 'image/gif', 'image/webp'].includes(
        document.MediaType ?? '',
      ) ||
      document.Size > 20 * 1024 * 1024
    )
      return;
    const controller = new AbortController();
    let url = '';
    api.workspace
      .download(assistantID, document.Path, document.Version, controller.signal)
      .then((blob) => {
        if (!controller.signal.aborted) {
          url = URL.createObjectURL(
            new Blob([blob], { type: document.MediaType }),
          );
          setImage(url);
        }
      })
      .catch((err) => {
        if (!controller.signal.aborted) setError(err);
      });
    return () => {
      controller.abort();
      if (url) URL.revokeObjectURL(url);
    };
  }, [assistantID, document?.Path, document?.Version]);
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
      await reloadRef.current();
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
      await reloadRef.current();
    });
  }
  async function upload(files: FileList | globalThis.File[]) {
    const target = directory,
      batch = Array.from(files);
    await run(async () => {
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
            existing = await api.workspace.stat(assistantID, path);
          } catch (err) {
            if (!(err instanceof APIError && err.code === 'not_found'))
              throw err;
          }
          if (existing) {
            const choice = await new Promise<'replace' | 'both' | 'skip'>(
              (resolve) => setCollision({ name: file.name, resolve }),
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
                `${stem}-${crypto.randomUUID().slice(0, 8)}${ext}`,
              );
            }
          }
          states[i].state = t('上传中…', 'Uploading…');
          setUploads([...states]);
          await api.workspace.upload(assistantID, path, file, version);
          states[i].state = t('已完成', 'Done');
        } catch (err) {
          states[i].state =
            err instanceof APIError
              ? `${t('失败', 'Failed')}: ${copy.errorReasons[err.reason as keyof typeof copy.errorReasons] ?? copy.errors[err.code as keyof typeof copy.errors] ?? copy.errors.unavailable}`
              : t('上传失败', 'Upload failed');
        }
        if (mounted.current) setUploads([...states]);
      }
      await reloadRef.current();
    });
  }
  const entries = (listing?.Entries ?? [])
    .filter((e) => hidden || !e.Name.startsWith('.'))
    .sort(
      (a, b) =>
        Number(b.Type === 'directory') - Number(a.Type === 'directory') ||
        (sort === 'modified'
          ? Date.parse(b.ModifiedAt) - Date.parse(a.ModifiedAt)
          : sort === 'size'
            ? b.Size - a.Size
            : a.Name.localeCompare(b.Name, locale)),
    );
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
          <div
            className="files-list"
            ref={listRef}
            onScroll={(e) => {
              memory.scroll = e.currentTarget.scrollTop;
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
                      onClick={() => inputRef.current?.click()}
                    >
                      {t('上传文件', 'Upload files')}
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={busy}
                      onClick={() => startForm({ kind: 'folder' })}
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
                      onClick={() => open(entry)}
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
                        items={menu(entry)}
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
                          setName('');
                          setForm({ kind: 'saveAs' });
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
                        setName('');
                        setForm({ kind: 'saveAs' });
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
              await reloadRef.current();
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
              await reloadRef.current();
            }}
          />
        )}
      </ModalPresence>
    </Drawer>
  );
}

function MoveDirectory({
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
    [listing, setListing] = useState<WorkspaceListing>(),
    [error, setError] = useState<unknown>(),
    [busy, setBusy] = useState(false);
  useErrorToast(error);
  useEffect(() => {
    const controller = new AbortController();
    setListing(undefined);
    api.workspace
      .list(assistantID, path, 0, '', controller.signal)
      .then(setListing)
      .catch((err) => {
        if (!controller.signal.aborted) setError(err);
      });
    return () => controller.abort();
  }, [assistantID, path]);
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
                onClick={async () => {
                  try {
                    const next = await api.workspace.list(
                      assistantID,
                      path,
                      listing.NextOffset,
                      listing.Version,
                    );
                    setListing({
                      ...next,
                      Entries: [...listing.Entries, ...next.Entries],
                    });
                  } catch (err) {
                    setError(err);
                  }
                }}
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
