import { useErrorToast } from '../../../error-toasts';
import { useEffect, useRef, useState } from 'react';
import { ModalPresence, Badge, Button, Dialog, Select, Textarea, TextField } from '@molr/ui';
import { Plus, Folder, FolderOpen, ChevronRight, ArrowUp } from 'lucide-react';
import { api } from '@molr/sdk';
import { useErrorToasts } from '../../../error-toasts';
import type { Assistant, Memory, MemoryRef, MemoryEntry, MemoryDirectory } from '@molr/sdk';
import { useCopy, useQuery } from '../../../state';
import { Confirm, EditDialog, Empty, Loading, useAction } from '../components/common';
import './memories.css';
function useMemoryCopy() {
  const { locale } = useCopy(); const zh = locale.startsWith('zh');
  return { zh,
    states: zh ? { active: '有效', pending: '待确认', superseded: '已替代' } : { active: 'Active', pending: 'Needs confirmation', superseded: 'Superseded' },
  };
}
const memoryRoot = '.agents/memory';
const fileRef = (m: MemoryRef): MemoryRef => ({ Scope: m.Scope, AssistantID: m.AssistantID, Path: m.Path });
const refQuery = (r: MemoryRef) => new URLSearchParams({ scope: r.Scope, assistant_id: r.AssistantID, path: r.Path }).toString();
const parent = (p: string) => p.slice(0, p.lastIndexOf('/')) || memoryRoot;
const baseName = (p: string) => p.slice(p.lastIndexOf('/') + 1);
export function Memories({ assistants }: { assistants: Assistant[] }) {
  const { t } = useCopy(), c = useMemoryCopy(), notices = useErrorToasts();
  const interaction = useRef(0);
  const addressPath = useRef<HTMLOListElement>(null);
  const [readError, setReadError] = useState<unknown>();
  const [assistant, setAssistant] = useState(''), [directory, setDirectory] = useState(memoryRoot);
  const activeAssistants = assistants.filter(a => a.State === 'active');
  useEffect(() => {
    if (assistants.some(a => a.ID === assistant && a.State !== 'active')) { resetInteraction(); setAssistant(''); setDirectory(memoryRoot); }
  }, [assistant, assistants]);
  const ref: MemoryRef = { Scope: assistant ? 'assistant' : 'global', AssistantID: assistant, Path: directory };
  const query = useQuery<MemoryDirectory>(`/api/memories/directory?${refQuery(ref)}`);
  const [editing, setEditing] = useState<{ key: number; ref: MemoryRef; memory?: Memory } | null>(null);
  const [mkdir, setMkdir] = useState(false), [moving, setMoving] = useState<MemoryEntry | null>(null);
  const [deleting, setDeleting] = useState<{ entry: MemoryEntry; forget: boolean } | null>(null);
  const [sources, setSources] = useState<Memory | null>(null);
  function resetInteraction() {
    interaction.current++;
    setReadError(undefined);
    setEditing(null); setSources(null); setMkdir(false); setMoving(null); setDeleting(null);
  }
  function changeDirectory(path: string) { resetInteraction(); if (path === directory) query.reload(); else setDirectory(path); }
  useErrorToast(query.error || readError, query.reload);
  async function open(r: MemoryRef, mode: 'edit' | 'summary' | 'sources' = 'edit') {
    resetInteraction(); notices?.rearm();
    const key = interaction.current, hash = location.hash;
    const current = () => key === interaction.current && hash === location.hash;
    try {
      const memory = await api.get<Memory>(`/api/memories/file?${refQuery(r)}`);
      if (!current()) return;
      if (mode === 'sources') setSources(memory);
      else setEditing({ key, ref: r, memory });
    } catch (error) {
      if (!current()) return;
      if (mode === 'summary' && (error as { status?: number }).status === 404) setEditing({ key, ref: r });
      else setReadError(error);
    }
  }
  // A completed save from an earlier dialog must not close a newer draft.
  const renderedInteraction = interaction.current;
  const closeDialog = () => { if (renderedInteraction === interaction.current) { resetInteraction(); query.reload(); } };
  useEffect(() => {
    const apply = () => {
      resetInteraction();
      const [route, search] = location.hash.slice(1).split('?');
      if (!['/account/memory', '/settings/memory'].includes(route)) return;
      const q = new URLSearchParams(search ?? ''), scope = q.get('scope'), path = q.get('path');
      if (q.has('assistant_id') && !path) {
        setAssistant(q.get('assistant_id') ?? ''); setDirectory(memoryRoot);
        return;
      }
      if (!path || !['assistant', 'global'].includes(scope ?? '')) return;
      const target: MemoryRef = { Scope: scope as MemoryRef['Scope'], AssistantID: scope === 'assistant' ? q.get('assistant_id') ?? '' : '', Path: path };
      setAssistant(target.AssistantID); setDirectory(path === 'MEMORY.md' ? memoryRoot : parent(path));
      void open(target);
    };
    apply(); window.addEventListener('hashchange', apply);
    return () => { interaction.current++; window.removeEventListener('hashchange', apply); };
  }, []);
  const crumbs = directory.slice(memoryRoot.length).split('/').filter(Boolean);
  useEffect(() => {
    const path = addressPath.current;
    if (path) path.scrollLeft = path.scrollWidth;
  }, [directory]);
  return <div className="settings-section memory-workspace">
    <p className="settings-description">{c.zh ? '灵魂是记忆的浓缩。助理从与你的交流与共同经历中积累记忆，在回顾与整理中沉淀出理解、偏好与判断。MEMORY.md 承载这些沉淀，让助理的个性随记忆逐渐形成。' : 'The soul is memory distilled. Through conversations and shared experiences with you, an assistant gathers memories and reflects on them to develop understanding, preferences, and judgment. MEMORY.md holds this distilled memory, allowing a distinct personality to take shape over time.'}</p>
    <Select size="sm" label={c.zh ? '选择助理' : 'Select assistant'} value={assistant} onValueChange={value => { resetInteraction(); setAssistant(value); setDirectory(memoryRoot); }} options={[{ value: '', label: c.zh ? '全局 · 当前账号共享' : 'Global · shared within your account' }, ...activeAssistants.map(a => ({ value: a.ID, label: a.Name }))]} />
    <div className="settings-toolbar">
      <Button variant="secondary" onClick={() => { resetInteraction(); setEditing({ key: interaction.current, ref: { ...ref, Path: directory + '/' } }); }}><Plus size={16} />{c.zh ? '新建文件' : 'New file'}</Button>
      <Button variant="secondary" onClick={() => { resetInteraction(); setMkdir(true); }}><Folder size={16} />{c.zh ? '新建目录' : 'New folder'}</Button>
      <Button variant="ghost" onClick={() => void open({ ...ref, Path: 'MEMORY.md' }, 'summary')}>{c.zh ? '查看 / 编辑核心记忆' : 'View / edit core memory'}</Button>
      <Button variant="ghost" onClick={query.reload}>{t.refresh}</Button>
    </div>
    {(directory !== memoryRoot || !!query.data?.Entries.length) && <nav className="memory-address" aria-label={c.zh ? '记忆目录' : 'Memory folders'}>
      <FolderOpen size={18} className="memory-address-icon" aria-hidden="true" />
      <ol className="memory-address-path" ref={addressPath}>
        {[{ name: c.zh ? '记忆' : 'Memory', path: memoryRoot }, ...crumbs.map((name, index) => ({ name, path: memoryRoot + '/' + crumbs.slice(0, index + 1).join('/') }))].map(({ name, path }, index) => <li key={path}>
          {index > 0 && <ChevronRight size={14} aria-hidden="true" />}
          <Button size="sm" variant="ghost" title={name} aria-current={path === directory ? 'location' : undefined} onClick={() => changeDirectory(path)}>{name}</Button>
        </li>)}
      </ol>
      {directory !== memoryRoot && <Button size="sm" variant="ghost" aria-label={c.zh ? '上一级' : 'Up'} title={c.zh ? '上一级' : 'Up'} onClick={() => changeDirectory(parent(directory))}><ArrowUp size={16} /></Button>}
    </nav>}
    {query.loading && !query.data ? <Loading /> : !query.data?.Entries.length ? <Empty /> : <div className="settings-rows">{query.data.Entries.map(row => <article className="settings-row-item" key={row.Path}>
      <div className="settings-row-item-head"><Button variant="ghost" onClick={() => row.Kind === 'directory' ? changeDirectory(row.Path) : void open(fileRef(row))}>{row.Kind === 'directory' && <Folder size={16} />}{baseName(row.Path)}</Button>{row.State && <Badge tone={row.State === 'pending' ? 'warning' : 'neutral'}>{c.states[row.State]}</Badge>}</div>
      <p className="muted memory-path"><code>{assistant ? '/work/' : '/global/'}{row.Path}</code></p>
      <div className="memory-actions"><Button size="sm" variant="ghost" onClick={() => { resetInteraction(); setMoving(row); }}>{c.zh ? '重命名 / 移动' : 'Rename / move'}</Button><Button size="sm" variant="ghost" onClick={() => { resetInteraction(); setDeleting({ entry: row, forget: false }); }}>{c.zh ? '删除' : 'Delete'}</Button>{row.Kind === 'file' && <><Button size="sm" variant="ghost" onClick={() => { resetInteraction(); setDeleting({ entry: row, forget: true }); }}>{c.zh ? '遗忘' : 'Forget'}</Button><Button size="sm" variant="ghost" onClick={() => void open(fileRef(row), 'sources')}>{c.zh ? '来源' : 'Sources'}</Button></>}</div>
    </article>)}</div>}
    <ModalPresence>{editing && <MemoryEditor key={editing.key} refValue={editing.ref} memory={editing.memory} onClose={closeDialog} />}</ModalPresence>
    <ModalPresence>{mkdir && <DirectoryEditor refValue={ref} onClose={closeDialog} />}</ModalPresence>
    <ModalPresence>{moving && <MoveEditor entry={moving} onClose={closeDialog} />}</ModalPresence>
    <ModalPresence>{deleting && <Confirm title={deleting.forget ? c.zh ? '遗忘这份记忆' : 'Forget this memory' : c.zh ? '删除文件或空目录' : 'Delete file or empty folder'} description={deleting.forget ? c.zh ? '删除文件并阻止相同内容或来源被再次提炼。关联回顾也会清除，原始聊天保留。' : 'Delete the file and suppress extraction of the same content or sources. Related reviews are cleared; original conversations remain.' : c.zh ? '只删除当前文件或空目录。普通删除不会阻止未来从原始资料重新整理记忆。' : 'Delete only the current file or empty folder. Ordinary deletion does not prevent future extraction from source material.'} danger onClose={closeDialog} onConfirm={async () => { const { entry, forget } = deleting; await api.mutate(entry.Kind === 'directory' ? '/api/memories/rmdir' : '/api/memories/delete', { ...fileRef(entry), ...(entry.Kind === 'file' ? { Forget: forget } : {}) }, entry.Version); query.reload(); }} />}</ModalPresence>
    <ModalPresence>{sources && <Sources key={renderedInteraction} memory={sources} assistants={assistants} onClose={closeDialog} />}</ModalPresence>
  </div>;
}
function MemoryEditor({ refValue, memory, onClose }: { refValue: MemoryRef; memory?: Memory; onClose: () => void }) {
  const { t } = useCopy(), c = useMemoryCopy();
  const [text, setText] = useState(memory?.Text ?? ''), [name, setName] = useState(''), [condition, setCondition] = useState(memory?.Context ?? ''), [state, setState] = useState<Memory['State']>(memory?.State ?? 'active');
  const previous = useQuery<Memory>(memory?.Replaces ? `/api/memories/file?${refQuery(memory.Replaces)}` : null);
  const path = memory || refValue.Path === 'MEMORY.md' ? refValue.Path : refValue.Path + name;
  return <EditDialog title={memory ? t.edit : c.zh ? '新建记忆文件' : 'New memory file'} onClose={onClose} onSubmit={() => api.mutate(memory ? '/api/memories/update' : '/api/memories', { ...refValue, Path: path, Text: text, Context: condition, Sources: memory?.Sources ?? [], ...(memory ? { State: state, ...(previous.data ? { ReplacesVersion: previous.data.Version } : {}) } : {}) }, memory?.Version)}>
    {!memory && refValue.Path !== 'MEMORY.md' && <TextField label={c.zh ? '语义文件名（含 .md）' : 'Descriptive filename (including .md)'} value={name} onChange={e => setName(e.target.value)} placeholder={c.zh ? '沟通偏好.md' : 'communication-preferences.md'} required maxLength={240} />}
    <p className="muted memory-path"><code>{refValue.Scope === 'global' ? '/global/' : '/work/'}{path}</code></p>
    <p className="muted">{c.zh ? '手动保存后，这份文件将受保护，不再由做梦自动改写。编辑、确认和遗忘用于必要时纠正记忆。' : 'Saving manually protects this file from automatic dream revisions. Editing, confirmation and forgetting are fallback controls.'}</p>
    <Textarea label={t.settings.memoryText} value={text} onChange={e => setText(e.target.value)} required maxLength={65536} />
    <TextField label={c.zh ? '适用条件' : 'Conditions'} value={condition} onChange={e => setCondition(e.target.value)} maxLength={1024} />
    {memory && <Select size="sm" label={c.zh ? '状态' : 'Status'} value={state} onValueChange={v => setState(v as Memory['State'])} options={Object.entries(c.states).map(([value, label]) => ({ value, label }))} />}
    {memory?.Replaces && <p className="muted memory-path">{memory.Replaces.Path}<br />{previous.data?.Text}<br />{c.zh ? '先将原文件标为已替代，再确认这份建议。每份文件独立保存。' : 'Mark the original file as superseded before confirming this proposal. Files are saved independently.'}</p>}
  </EditDialog>;
}
function DirectoryEditor({ refValue, onClose }: { refValue: MemoryRef; onClose: () => void }) {
  const c = useMemoryCopy(), [name, setName] = useState('');
  return <EditDialog title={c.zh ? '新建目录' : 'New folder'} onClose={onClose} onSubmit={() => api.mutate('/api/memories/mkdir', { ...refValue, Path: refValue.Path + '/' + name })}><TextField label={c.zh ? '目录名' : 'Folder name'} value={name} onChange={e => setName(e.target.value)} placeholder={c.zh ? '用户偏好' : 'User preferences'} required maxLength={240} /></EditDialog>;
}
function MoveEditor({ entry, onClose }: { entry: MemoryEntry; onClose: () => void }) {
  const c = useMemoryCopy(), [path, setPath] = useState(entry.Path.slice(memoryRoot.length + 1));
  return <EditDialog title={c.zh ? '重命名 / 移动' : 'Rename / move'} onClose={onClose} onSubmit={() => api.mutate('/api/memories/move', { ...fileRef(entry), Directory: entry.Kind === 'directory', ToPath: memoryRoot + '/' + path }, entry.Version)}><TextField label={c.zh ? '新路径（相对记忆目录）' : 'New path (relative to memory folder)'} value={path} onChange={e => setPath(e.target.value)} required maxLength={1024} /><p className="muted">{c.zh ? '目标父目录必须存在。同名文件不会被覆盖；旧路径的历史引用会失效。' : 'The destination folder must exist. Existing files are never replaced; historical references to the old path expire.'}</p></EditDialog>;
}
function Sources({ memory, assistants, onClose }: { memory: Memory; assistants: Assistant[]; onClose: () => void }) {
  const c = useMemoryCopy(), action = useAction(), [text, setText] = useState('');
  const chat = assistants.find(a => a.ID === memory.AssistantID)?.ConversationID;
  useErrorToast(action.error);
  return <Dialog open title={c.zh ? '记忆来源' : 'Memory sources'} onOpenChange={open => { if (!open) onClose(); }}><p>{memory.Text}</p><p className="muted">{memory.Confirmed ? c.zh ? '用户已确认' : 'User confirmed' : memory.Evidence === 'explicit' ? c.zh ? '来自明确陈述' : 'Explicit statement' : memory.Evidence === 'verified' ? c.zh ? '来自任务结果' : 'Task result' : c.zh ? '需要核实' : 'Needs verification'}</p>{memory.Sources?.map(ref => <div key={ref.Kind + ref.ID} className="memory-source"><code>{ref.Kind}: {ref.ID}</code><Button size="sm" variant="secondary" disabled={ref.Kind === 'message' && !chat} onClick={() => void action.run(async () => { const data = await api.get<unknown>(ref.Kind === 'message' ? `/api/conversations/${chat}/messages?message_id=${ref.ID}` : `/api/tasks/${ref.ID}`); const value = data as { Items?: { Content: { Text?: string; Card?: { Title: string; Summary: string } } }[]; Goal?: string; State?: string }; setText(value.Items?.map(item => item.Content.Text || [item.Content.Card?.Title, item.Content.Card?.Summary].filter(Boolean).join('\n')).join('\n\n') ?? [value.Goal, value.State].filter(Boolean).join('\n')); }, undefined, false)}>{c.zh ? '读取原始记录' : 'Read original'}</Button></div>)}{text && <pre className="memory-source-text">{text}</pre>}</Dialog>;
}
