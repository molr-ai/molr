import { useEffect, useState } from 'react';
import { Badge, Button, DateTimePicker, Checkbox, Dialog, Select, Textarea, TextField } from '@molr/ui';
import { Plus, Moon, Folder, ArrowUp } from 'lucide-react';
import { api } from '../../../api/client';
import type { Assistant, DreamConfig, DreamRun, Memory, MemoryRef, MemoryEntry, MemoryDirectory } from '../../../api/types';
import { useCopy, useData, useQuery } from '../../../state';
import { Confirm, EditDialog, Empty, ErrorNotice, Loading, useAction } from '../components/common';
import './memories.css';
function useMemoryCopy() {
  const { locale } = useCopy(); const zh = locale.startsWith('zh');
  return { zh,
    states: zh ? { active: '有效', pending: '待确认', superseded: '已替代' } : { active: 'Active', pending: 'Needs confirmation', superseded: 'Superseded' },
    runs: (state: string) => (zh ? { timed_out: '做梦超时', partial: '部分资料未提炼', missed_window: '已错过预留时段', queued: '等待整理', running: '正在做梦', committing: '正在保存文件', completed: '已整理', skipped: '没有新信息', failed: '整理未完成', up_to_date: '已整理到最新', undone: '已撤销', canceled: '已取消' } : { timed_out: 'Timed out', partial: 'Partially consolidated', missed_window: 'Window missed', queued: 'Queued', running: 'Dreaming', committing: 'Saving files', completed: 'Completed', skipped: 'No new information', failed: 'Incomplete', up_to_date: 'Up to date', undone: 'Reverted', canceled: 'Canceled' })[state] ?? state,
  };
}
const memoryRoot = '.agents/memory';
const fileRef = (m: MemoryRef): MemoryRef => ({ Scope: m.Scope, AssistantID: m.AssistantID, Path: m.Path });
const refQuery = (r: MemoryRef) => new URLSearchParams({ scope: r.Scope, assistant_id: r.AssistantID, path: r.Path }).toString();
const parent = (p: string) => p.slice(0, p.lastIndexOf('/')) || memoryRoot;
const baseName = (p: string) => p.slice(p.lastIndexOf('/') + 1);
export function Memories({ assistants }: { assistants: Assistant[] }) {
  const { t } = useCopy(), c = useMemoryCopy(), action = useAction();
  const [assistant, setAssistant] = useState(''), [directory, setDirectory] = useState(memoryRoot);
  const activeAssistants = assistants.filter(a => a.State === 'active');
  useEffect(() => {
    if (assistants.some(a => a.ID === assistant && a.State !== 'active')) { setAssistant(''); setDirectory(memoryRoot); }
  }, [assistant, assistants]);
  const ref: MemoryRef = { Scope: assistant ? 'assistant' : 'global', AssistantID: assistant, Path: directory };
  const query = useQuery<MemoryDirectory>(`/api/memories/directory?${refQuery(ref)}`);
  const [editing, setEditing] = useState<{ ref: MemoryRef; memory?: Memory } | null>(null);
  const [mkdir, setMkdir] = useState(false), [moving, setMoving] = useState<MemoryEntry | null>(null);
  const [deleting, setDeleting] = useState<{ entry: MemoryEntry; forget: boolean } | null>(null);
  const [sources, setSources] = useState<Memory | null>(null);
  const open = async (r: MemoryRef, summary = false) => {
    try { setEditing({ ref: r, memory: await api.get<Memory>(`/api/memories/file?${refQuery(r)}`) }); }
    catch (error) { if (summary && (error as { status?: number }).status === 404) setEditing({ ref: r }); else throw error; }
  };
  useEffect(() => {
    const apply = () => {
      const q = new URLSearchParams(location.hash.split('?')[1] ?? ''), scope = q.get('scope'), path = q.get('path');
      if (!path || !['assistant', 'global'].includes(scope ?? '')) return;
      const target: MemoryRef = { Scope: scope as MemoryRef['Scope'], AssistantID: scope === 'assistant' ? q.get('assistant_id') ?? '' : '', Path: path };
      setAssistant(target.AssistantID); setDirectory(path === 'MEMORY.md' ? memoryRoot : parent(path));
      void action.run(() => open(target), undefined, false);
    };
    apply(); window.addEventListener('hashchange', apply); return () => window.removeEventListener('hashchange', apply);
  }, []);
  const crumbs = directory.slice(memoryRoot.length).split('/').filter(Boolean);
  return <div className="settings-section memory-workspace">
    <p className="muted">{c.zh ? '经历与回顾沉淀为主题记忆，再提炼到 MEMORY.md 核心记忆。核心记忆自动加载，主题文件按需检索；做梦会自动维护，无需逐条确认。' : 'Experiences become topic memories, then stable core cognition in MEMORY.md. Core memory loads automatically; topics are read when needed. Dreams maintain these layers without routine approvals.'}</p>
    <Select size="sm" label={c.zh ? '选择助理' : 'Select assistant'} value={assistant} onValueChange={value => { setAssistant(value); setDirectory(memoryRoot); }} options={[{ value: '', label: c.zh ? '全局 · 当前账号共享' : 'Global · shared within your account' }, ...activeAssistants.map(a => ({ value: a.ID, label: a.Name }))]} />
    <div className="settings-toolbar">
      <Button variant="secondary" onClick={() => setEditing({ ref: { ...ref, Path: directory + '/' } })}><Plus size={16} />{c.zh ? '新建文件' : 'New file'}</Button>
      <Button variant="secondary" onClick={() => setMkdir(true)}><Folder size={16} />{c.zh ? '新建目录' : 'New folder'}</Button>
      <Button variant="ghost" onClick={() => void action.run(() => open({ ...ref, Path: 'MEMORY.md' }, true), undefined, false)}>{c.zh ? '查看 / 编辑核心记忆' : 'View / edit core memory'}</Button>
      <Button variant="ghost" onClick={query.reload}>{t.refresh}</Button>
    </div>
    <nav className="memory-breadcrumbs" aria-label={c.zh ? '记忆目录' : 'Memory folders'}>
      <Button size="sm" variant="ghost" onClick={() => setDirectory(memoryRoot)}>{c.zh ? '记忆' : 'Memory'}</Button>
      {crumbs.map((name, index) => <Button size="sm" variant="ghost" key={index} onClick={() => setDirectory(memoryRoot + '/' + crumbs.slice(0, index + 1).join('/'))}>/ {name}</Button>)}
      {directory !== memoryRoot && <Button size="sm" variant="ghost" onClick={() => setDirectory(parent(directory))}><ArrowUp size={14} />{c.zh ? '上一级' : 'Up'}</Button>}
    </nav>
    <ErrorNotice error={query.error || action.error} retry={query.reload} />
    {query.loading && !query.data ? <Loading /> : !query.data?.Entries.length ? <Empty /> : <div className="settings-rows">{query.data.Entries.map(row => <article className="settings-row-item" key={row.Path}>
      <div className="settings-row-item-head"><Button variant="ghost" onClick={() => row.Kind === 'directory' ? setDirectory(row.Path) : void action.run(() => open(fileRef(row)), undefined, false)}>{row.Kind === 'directory' && <Folder size={16} />}{baseName(row.Path)}</Button>{row.State && <Badge tone={row.State === 'pending' ? 'warning' : 'neutral'}>{c.states[row.State]}</Badge>}</div>
      <p className="muted memory-path"><code>{assistant ? '/work/' : '/global/'}{row.Path}</code></p>
      <div className="memory-actions"><Button size="sm" variant="ghost" onClick={() => setMoving(row)}>{c.zh ? '重命名 / 移动' : 'Rename / move'}</Button><Button size="sm" variant="ghost" onClick={() => setDeleting({ entry: row, forget: false })}>{c.zh ? '删除' : 'Delete'}</Button>{row.Kind === 'file' && <><Button size="sm" variant="ghost" onClick={() => setDeleting({ entry: row, forget: true })}>{c.zh ? '遗忘' : 'Forget'}</Button><Button size="sm" variant="ghost" onClick={() => void action.run(async () => { setSources(await api.get<Memory>(`/api/memories/file?${refQuery(row)}`)); }, undefined, false)}>{c.zh ? '来源' : 'Sources'}</Button></>}</div>
    </article>)}</div>}
    {activeAssistants.some(a => a.ID === assistant) && <Dreams key={assistant} assistantID={assistant} />}
    {editing && <MemoryEditor refValue={editing.ref} memory={editing.memory} onClose={() => { setEditing(null); query.reload(); }} />}
    {mkdir && <DirectoryEditor refValue={ref} onClose={() => { setMkdir(false); query.reload(); }} />}
    {moving && <MoveEditor entry={moving} onClose={() => { setMoving(null); query.reload(); }} />}
    {deleting && <Confirm title={deleting.forget ? c.zh ? '遗忘这份记忆' : 'Forget this memory' : c.zh ? '删除文件或空目录' : 'Delete file or empty folder'} description={deleting.forget ? c.zh ? '删除文件并阻止相同内容或来源被再次提炼。关联回顾也会清除，原始聊天保留。' : 'Delete the file and suppress extraction of the same content or sources. Related reviews are cleared; original conversations remain.' : c.zh ? '只删除当前文件或空目录。普通删除不会阻止未来从原始资料重新整理记忆。' : 'Delete only the current file or empty folder. Ordinary deletion does not prevent future extraction from source material.'} danger onClose={() => setDeleting(null)} onConfirm={async () => { const { entry, forget } = deleting; await api.mutate(entry.Kind === 'directory' ? '/api/memories/rmdir' : '/api/memories/delete', { ...fileRef(entry), ...(entry.Kind === 'file' ? { Forget: forget } : {}) }, entry.Version); query.reload(); }} />}
    {sources && <Sources memory={sources} assistants={assistants} onClose={() => setSources(null)} />}
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
function Dreams({ assistantID }: { assistantID: string }) {
  const { t } = useCopy(), c = useMemoryCopy(), action = useAction();
  const config = useQuery<DreamConfig>(`/api/dreams/config?assistant_id=${assistantID}`), runs = useQuery<DreamRun[]>(`/api/dreams?assistant_id=${assistantID}`);
  const { invalidateTopics } = useData();
  const [editing, setEditing] = useState(false), [result, setResult] = useState(''), [undo, setUndo] = useState<DreamRun | null>(null);
  useEffect(() => { const timer = setInterval(() => { invalidateTopics(['dreams', 'memories']); }, 5000); return () => clearInterval(timer); }, [invalidateTopics]);
  return <section className="dream-panel"><div className="settings-row-item-head"><Moon size={18} /><h3>{c.zh ? '做梦' : 'Dreaming'}</h3></div>
    {config.data && <p className="muted">{config.data.Enabled ? `${c.zh ? '每天' : 'Daily at'} ${String(config.data.Hour).padStart(2, '0')}:${String(config.data.Minute).padStart(2, '0')} · ${config.data.Timezone}` : c.zh ? '自动整理已关闭' : 'Automatic dreams are off'} · {c.zh ? '回顾前一个完整自然日并维护长期记忆；没有新聊天时也可沉淀已有主题。费用计入助理预算。' : 'Reviews the previous complete day and maintains long-term memory, including quiet days with existing topics. Uses the assistant budget.'}</p>}
    <div className="memory-actions"><Button size="sm" variant="secondary" disabled={!config.data} onClick={() => setEditing(true)}>{c.zh ? '整理设置' : 'Dream settings'}</Button><Button size="sm" loading={action.busy} onClick={() => void action.run(async () => { const run = await api.mutate<DreamRun>('/api/dreams/run', { AssistantID: assistantID }); setResult(c.runs(run.State)); }, undefined, false)}>{c.zh ? '立即整理 / 重试' : 'Consolidate / retry'}</Button></div>{result && <p role="status">{result}</p>}
    <ErrorNotice error={config.error || runs.error || action.error} retry={runs.reload} />
    {runs.data?.slice(0, 10).map(run => <details className="dream-run" key={run.ID}><summary>{run.Day} · {c.runs(run.State)}{run.SessionState && run.SessionState !== run.State ? ` / ${c.runs(run.SessionState)}` : ''} · {c.zh ? `${run.MemoryFiles?.length ?? 0} 条记忆` : `${run.MemoryFiles?.length ?? 0} memories`}</summary><p className="muted">{run.Batch ? `${c.zh ? '批次' : 'Batch'} ${run.Batch}` : ''}{run.Maintenance ? (c.zh ? ' · 长期记忆维护' : ' · Long-term maintenance') : ''}{run.Backfill ? (c.zh ? ' · 一次补处理' : ' · One-time backfill') : ''}{run.Deadline ? ` · ${c.zh ? '截止' : 'Deadline'} ${new Date(run.Deadline).toLocaleString()}` : ''}</p>{run.Summary && <p className="pre-wrap">{run.Summary}</p>}{run.ReviewPath && (run.State !== 'undone' || run.ReviewRetained) && <p className="muted memory-path"><code>/work/{run.ReviewPath}</code></p>}{run.State === 'undone' && !!run.UndoRetained?.length && <p className="muted memory-path">{c.zh ? '保留的文件：' : 'Retained files: '}{run.UndoRetained.map(ref => ref.Path).join(', ')}</p>}{run.FailureCode && <p role="status">{t.errors[run.FailureCode as keyof typeof t.errors] ?? run.FailureCode}</p>}<p className="muted">{run.Sources.length} {c.zh ? '项来源' : 'sources'} · {run.Timezone}</p>{run.State === 'completed' && <Button size="sm" variant="ghost" onClick={() => setUndo(run)}>{c.zh ? '撤销这次整理' : 'Revert this dream'}</Button>}</details>)}
    {undo && <Confirm title={c.zh ? '撤销这次整理' : 'Revert this dream'} description={c.zh ? '删除本次新建的自动记忆，并恢复本次更新前的主题和核心记忆；后来修改、确认或遗忘的内容会受到保护。未修改的回顾同时删除。' : 'Remove new automatic memories and restore updated topics and core memory to their prior versions. Later edits, confirmations and forgotten content are protected. The unchanged review is also removed.'} onClose={() => setUndo(null)} onConfirm={() => api.mutate('/api/dreams/undo', { ID: undo.ID })} />}
    {editing && config.data && <DreamEditor config={config.data} onClose={() => setEditing(false)} />}
  </section>;
}
function DreamEditor({ config, onClose }: { config: DreamConfig; onClose: () => void }) {
  const c = useMemoryCopy(), [enabled, setEnabled] = useState(config.Enabled), [time, setTime] = useState(`${String(config.Hour).padStart(2, '0')}:${String(config.Minute).padStart(2, '0')}`), [zone, setZone] = useState(config.Timezone), [duration, setDuration] = useState(String(config.MaxDurationSeconds || 600));
  return <EditDialog title={c.zh ? '做梦整理设置' : 'Dream settings'} onClose={onClose} onSubmit={() => api.mutate('/api/dreams/config', { AssistantID: config.AssistantID, Enabled: enabled, Hour: Number(time.split(':')[0]), Minute: Number(time.split(':')[1]), Timezone: zone, MaxDurationSeconds: Number(duration) }, config.Version)}><TextField label={c.zh ? '做梦时长上限（秒）' : 'Maximum dream duration (seconds)'} type="number" min={1} max={3600} value={duration} onChange={e => setDuration(e.target.value)} required /><Checkbox label={c.zh ? '每天自动做梦' : 'Dream automatically every day'} checked={enabled} onChange={e => setEnabled(e.target.checked)} /><DateTimePicker label={c.zh ? '开始时间' : 'Start time'} mode="time" timeZone="UTC" value={`2020-01-01T${time}:00Z`} onValueChange={value=>{if(value)setTime(new Date(value).toISOString().slice(11,16));}} /><TextField label={c.zh ? '时区' : 'Timezone'} value={zone} onChange={e => setZone(e.target.value)} required /></EditDialog>;
}
function Sources({ memory, assistants, onClose }: { memory: Memory; assistants: Assistant[]; onClose: () => void }) {
  const c = useMemoryCopy(), action = useAction(), [text, setText] = useState('');
  const chat = assistants.find(a => a.ID === memory.AssistantID)?.ConversationID;
  return <Dialog open title={c.zh ? '记忆来源' : 'Memory sources'} onOpenChange={open => { if (!open) onClose(); }}><p>{memory.Text}</p><p className="muted">{memory.Confirmed ? c.zh ? '用户已确认' : 'User confirmed' : memory.Evidence === 'explicit' ? c.zh ? '来自明确陈述' : 'Explicit statement' : memory.Evidence === 'verified' ? c.zh ? '来自任务结果' : 'Task result' : c.zh ? '需要核实' : 'Needs verification'}</p>{memory.Sources?.map(ref => <div key={ref.Kind + ref.ID} className="memory-source"><code>{ref.Kind}: {ref.ID}</code><Button size="sm" variant="secondary" disabled={ref.Kind === 'message' && !chat} onClick={() => void action.run(async () => { const data = await api.get<unknown>(ref.Kind === 'message' ? `/api/conversations/${chat}/messages?message_id=${ref.ID}` : `/api/tasks/${ref.ID}`); const value = data as { Items?: { Content: { Text?: string; Card?: { Title: string; Summary: string } } }[]; Goal?: string; State?: string }; setText(value.Items?.map(item => item.Content.Text || [item.Content.Card?.Title, item.Content.Card?.Summary].filter(Boolean).join('\n')).join('\n\n') ?? [value.Goal, value.State].filter(Boolean).join('\n')); }, undefined, false)}>{c.zh ? '读取原始记录' : 'Read original'}</Button></div>)}<ErrorNotice error={action.error} />{text && <pre className="memory-source-text">{text}</pre>}</Dialog>;
}
