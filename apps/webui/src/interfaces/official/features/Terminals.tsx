import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import type { RefObject } from 'react';
import { Button, Drawer, IconButton, Tooltip, useBreakpoint } from '@molr/ui';
import { Bot, ChevronLeft, ChevronRight, GripHorizontal, PanelRight, PictureInPicture2, Plus, Terminal, UserRound, X } from 'lucide-react';
import { api, APIError, mutationKey } from '../../../api/client';
import type { Grant, Resource, ShellTask } from '../../../api/types';
import { useErrorToast, useErrorToasts } from '../../../error-toasts';
import type { SandboxInfo } from '../../../sandbox-types';
import { useCopy, useQuery } from '../../../state';
import { terminalClient, TerminalSession } from './TerminalSession';
import { TerminalThemePicker, useTerminalTheme } from './terminalTheme';
import { useFloatingPanel } from './useFloatingPanel';
import type { FloatingBounds } from './useFloatingPanel';

import { ExecutionOptions, useExecutionOptions } from './ExecutionOptions';

const selections = new Map<string,string>();
export function Terminals({ assistantID, assistantName, docked, floating, onFloatingChange, bounds, onClose }: { assistantID: string; assistantName: string; docked: boolean; floating: boolean; onFloatingChange: (value: boolean) => void; bounds: RefObject<FloatingBounds | null>; onClose: () => void }) {
  const { locale, t } = useCopy(), zh = locale === 'zh', mobile = useBreakpoint('mobile');
  const query = useQuery<{ Items: ShellTask[] }>(`/api/assistants/${assistantID}/shell-tasks`, false, 2000);
  const resources = useQuery<Resource[]>('/api/resources?kind=compute', true), grants = useQuery<Grant[]>(`/api/grants?assistant_id=${assistantID}`, true);
  const sandbox = useQuery<SandboxInfo>('/api/sandbox');
  const supported = sandbox.data?.Capabilities.Terminal;
  const ready = sandbox.data?.Enabled && (supported && sandbox.data.Capabilities.Shell || sandbox.data.Capabilities.FullAccessTerminal);
  const execution = useExecutionOptions(resources.data, grants.data, !!(sandbox.data?.Enabled && supported && sandbox.data.Capabilities.Shell), !!(sandbox.data?.Enabled && sandbox.data.Capabilities.FullAccessTerminal));
  const tasks = (query.data?.Items ?? []).filter(t => t.Mode === 'tty' && t.State === 'running').sort((a, b) => a.StartedAt.localeCompare(b.StartedAt) || a.ID.localeCompare(b.ID));
  const [selected, setSelected] = useState(selections.get(assistantID) ?? ''), [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const errors = useErrorToasts();
  useErrorToast(error);
  useErrorToast(query.error, query.reload);
  useErrorToast(resources.error, resources.reload);
  useErrorToast(grants.error, grants.reload);
  useErrorToast(sandbox.error, sandbox.reload);
  const canCreate = !!ready && !sandbox.error && !resources.error && !grants.error && execution.valid;
  const unavailable = !sandbox.data || sandbox.error
    ? (zh ? '暂时无法确认终端能力。' : 'Terminal capabilities could not be confirmed.')
    : !sandbox.data.Enabled || !(sandbox.data.Capabilities.Shell || sandbox.data.Capabilities.FullAccessTerminal) ? (zh ? '本机沙盒命令执行不可用，请检查沙盒状态。' : 'Local sandbox command execution is unavailable. Check sandbox status.')
    : !(supported || sandbox.data.Capabilities.FullAccessTerminal) ? (sandbox.data.Capabilities.TerminalReason === 'terminal_start_failed' ? t.errorReasons.terminal_start_failed : t.errorReasons.terminal_platform_unsupported)
    : resources.error || grants.error ? (zh ? '暂时无法确认计算权限。' : 'Compute permissions could not be confirmed.')
    : (zh ? '请先为助理授予本机计算权限。' : 'Grant the assistant local compute permission first.');
  const request = useRef<string | undefined>(undefined), mounted = useRef(true);
  const tabList = useRef<HTMLDivElement>(null), panelID = useId();
  const [closing, setClosing] = useState(''), [scroll, setScroll] = useState({ left: false, right: false });
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const view = useFloatingPanel(floating, bounds);
  useTerminalTheme(view.ref, true);
  function choose(id: string) { selections.set(assistantID,id); setSelected(id); }
  useEffect(() => { if (query.data && !tasks.some(t => t.ID === selected)) choose(tasks.at(-1)?.ID ?? ''); }, [selected, query.data]);
  const activeID = tasks.some(t => t.ID === selected) ? selected : tasks.at(-1)?.ID ?? '';
  useLayoutEffect(() => {
    const list = tabList.current; if (!list) return;
    const measure = () => setScroll({ left: list.scrollLeft > 1, right: list.scrollLeft + list.clientWidth < list.scrollWidth - 1 });
    const reveal = () => { list.querySelector<HTMLElement>('[aria-selected="true"]')?.closest<HTMLElement>('.terminal-tab')?.scrollIntoView({ block: 'nearest', inline: 'nearest' }); measure(); };
    reveal(); const observer = new ResizeObserver(reveal); observer.observe(list);
    list.addEventListener('scroll', measure);
    return () => { observer.disconnect(); list.removeEventListener('scroll', measure); };
  }, [activeID, tasks.length]);
  const label = (t: ShellTask) => `${t.CreatedBy === 'user' ? (zh ? '终端' : 'Terminal') : (zh ? '助理终端' : 'Assistant terminal')} · ${new Date(t.StartedAt).toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })}`;
  const canEnd = (t: ShellTask) => t.Terminal?.Control === 'user' && t.Terminal.ControllerIsCurrentSession && t.Terminal.ControllerClientID === terminalClient.id;
  async function closeSession(task: ShellTask) {
    if (!canEnd(task) || closing) return;
    setClosing(task.ID); setError(undefined); errors?.rearm();
    try { await api.post(`/api/assistants/${assistantID}/shell-tasks`, { action: 'stop', task_id: task.ID, generation: task.Terminal?.Generation, client_id: terminalClient.id, sequence: ++terminalClient.sequence }); await query.reload(); }
    catch (err) { if (mounted.current) setError(err); }
    finally { if (mounted.current) setClosing(''); }
  }
  async function create() {
    if (busy || !canCreate) return;
    setBusy(true); setError(undefined); errors?.rearm(); request.current ??= mutationKey();
    try {
      const result = await api.post<{ Task: ShellTask }>(`/api/assistants/${assistantID}/terminals`, { request_id: request.current, client_id: terminalClient.id, compute_id: execution.compute, full_access: execution.fullAccess });
      request.current = undefined;
      if (mounted.current) { await query.reload(); if (mounted.current) choose(result.Task.ID); }
    } catch (err) {
      if (err instanceof APIError && err.status >= 400 && err.status < 500) request.current = undefined;
      if (mounted.current) setError(err);
    } finally { if (mounted.current) setBusy(false); }
  }
  const task = tasks.find(t => t.ID === activeID), title = `${zh ? '终端' : 'Terminal'} · ${assistantName}`;
  return <Drawer open title={title} side={mobile ? 'bottom' : 'right'} docked={floating || docked} onOpenChange={open => { if (!open) onClose(); }} showCloseButton={false} className={`terminal-drawer${tasks.length ? ' terminal-drawer--running' : ''}${floating ? ' terminal-drawer--floating' : ''}`} size="lg" headerContent={<div className={`terminal-heading${tasks.length ? ' terminal-tabstrip' : ' terminal-window-heading'}`} {...view.drag}>
    {floating && <IconButton className="files-move" label={zh ? '移动终端（方向键微调）' : 'Move terminal (arrow keys)'} onKeyDown={view.moveKey}><GripHorizontal size={18}/></IconButton>}
    {!tasks.length && <span className="terminal-heading-title" title={title}>{title}</span>}
    {tasks.length > 0 && <div className="terminal-tabs-window">
        {(scroll.left || scroll.right) && <IconButton className="terminal-tab-scroll" label={zh ? '向左滚动会话' : 'Scroll sessions left'} disabled={!scroll.left} onClick={() => tabList.current?.scrollBy({ left: -Math.max(120, tabList.current.clientWidth * 0.75) })}><ChevronLeft size={16}/></IconButton>}
        <div ref={tabList} className="terminal-tabs" role="tablist" aria-label={zh ? '活跃会话' : 'Active sessions'}>
          {tasks.map((item, index) => <div className="terminal-tab" data-active={item.ID === activeID || undefined} key={item.ID}>
            <button type="button" role="tab" id={`${panelID}-${item.ID}`} aria-controls={panelID} aria-selected={item.ID === activeID} tabIndex={item.ID === activeID ? 0 : -1} title={label(item)} onClick={() => choose(item.ID)} onKeyDown={event => {
              if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
              event.preventDefault(); const next = event.key === 'Home' ? 0 : event.key === 'End' ? tasks.length - 1 : (index + (event.key === 'ArrowRight' ? 1 : -1) + tasks.length) % tasks.length;
              choose(tasks[next].ID); tabList.current?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus({ preventScroll: true });
            }}>{item.CreatedBy === 'user' ? <UserRound size={14} aria-hidden="true"/> : <Bot size={14} aria-hidden="true"/>}<span>{label(item)}</span></button>
            <Tooltip content={canEnd(item) ? (zh ? '结束会话' : 'End session') : (zh ? '接管后可结束会话' : 'Take control before ending this session')}><IconButton className="terminal-tab-close" label={`${zh ? '结束会话' : 'End session'}: ${label(item)}`} disabled={!canEnd(item) || !!closing} onClick={() => void closeSession(item)}><X size={13}/></IconButton></Tooltip>
          </div>)}
        </div>
        {(scroll.left || scroll.right) && <IconButton className="terminal-tab-scroll" label={zh ? '向右滚动会话' : 'Scroll sessions right'} disabled={!scroll.right} onClick={() => tabList.current?.scrollBy({ left: Math.max(120, tabList.current.clientWidth * 0.75) })}><ChevronRight size={16}/></IconButton>}
    </div>}
        <Tooltip content={canCreate ? (zh ? '新建会话' : 'New session') : unavailable}><IconButton className="terminal-new-session" disabled={busy || !canCreate} label={busy ? (zh ? '开启中…' : 'Opening…') : (zh ? '新建会话' : 'New session')} onClick={() => void create()}><Plus size={17}/></IconButton></Tooltip>
    <div className="terminal-heading-actions">{!tasks.length && <TerminalThemePicker zh={zh}/>} {!mobile && <IconButton label={floating ? (zh ? '停靠侧栏' : 'Dock to sidebar') : (zh ? '浮窗模式' : 'Floating window')} onClick={() => onFloatingChange(!floating)}>{floating ? <PanelRight size={18}/> : <PictureInPicture2 size={18}/>}</IconButton>}<IconButton label={zh ? '关闭终端窗口' : 'Close terminal window'} onClick={onClose}><X size={18}/></IconButton></div>
  </div>}>
    <div ref={view.ref} className="terminal-panel">
      <ExecutionOptions value={execution} zh={zh} disabled={busy}/>

      {task ? <div id={panelID} className="terminal-tab-content" role="tabpanel" aria-labelledby={`${panelID}-${task.ID}`}><TerminalSession key={task.ID} assistantID={assistantID} task={task} refresh={query.reload}/></div> : <div className="terminal-empty"><span className="terminal-empty-icon"><Terminal size={28}/></span><h3>{zh ? '在助理工作区中开启交互式 Shell' : 'Open an interactive shell in the assistant workspace'}</h3><p>{sandbox.loading || resources.loading || grants.loading ? t.loading : canCreate ? (execution.fullAccess ? (zh ? '新会话将使用已授权的 Full Access 模式。' : 'New sessions use the authorized Full Access mode.') : (zh ? '使用助理已获授的计算、文件和网络权限。' : 'Uses the assistant’s granted compute, file and network permissions.')) : unavailable}</p>{canCreate && <Button size="sm" disabled={busy} onClick={() => void create()}><Plus size={15}/>{zh ? '开启终端' : 'Open terminal'}</Button>}</div>}
      {floating && <button className="terminal-resize" aria-label={zh ? '调整终端大小' : 'Resize terminal'} {...view.resize}/>}
    </div>
  </Drawer>;
}
