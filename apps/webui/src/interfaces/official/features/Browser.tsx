import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { ChangeEvent, KeyboardEvent, PointerEvent, RefObject, WheelEvent } from 'react';
import { Badge, Button, Drawer, IconButton, useBreakpoint, useToast, useUILocale } from '@molr/ui';
import { ArrowLeft, ArrowRight, ChevronLeft, ChevronRight, CornerDownLeft, Globe, Hand, Keyboard, GripHorizontal, PanelRight, PictureInPicture2, Plus, Power, RefreshCw, X } from 'lucide-react';
import { api, APIError, mutationKey } from '../../../api/client';
import type { BrowserStatus, Grant, Resource } from '../../../api/types';
import { useCopy, useQuery, useSession } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { ComponentDetails, MaintenanceProgress, maintenanceReason, useMaintenanceFailureToast } from '../../../maintenance/Components';
import type { ComponentStatus } from '@molr/sdk';
import './browser.css';
import type { SandboxInfo } from '../../../sandbox-types';
import { SessionStart } from './SessionStart';
import { ExecutionOptions, useExecutionOptions } from './ExecutionOptions';
import { useBrowserView } from './useBrowserView';
import type { BrowserBounds } from './useBrowserView';

import { BrowserInputBuffer, batchable } from './browser-input';
import type { BrowserAction as Action } from './browser-input';
type Tab = { id: string; title: string; url: string; active: boolean };
// A panel is ephemeral; the controlling window is not. IDs are deliberately not
// stored in localStorage/sessionStorage where another tab could inherit them.
const clients = new Map<string, { id: string; sequence: number; queue: Promise<void>; lastFailure?: string }>();
function windowClient(assistantID: string) {
  let client = clients.get(assistantID);
  if (!client) { client = { id: mutationKey(), sequence: 0, queue: Promise.resolve() }; clients.set(assistantID, client); }
  return client;
}
export function Browser({ assistantID, docked, floating, onFloatingChange, bounds, onClose }: { assistantID: string; docked: boolean; floating: boolean; onFloatingChange: (floating: boolean) => void; bounds: RefObject<BrowserBounds | null>; onClose: () => void }) {
  const { locale } = useCopy(), zh = locale === 'zh', mobile = useBreakpoint('mobile');
  const { principal } = useSession(), admin = !!principal?.User?.Administrator;
  const components = useQuery<ComponentStatus[]>(admin ? '/api/components' : null, false, 2000);
  const component = components.data?.[0];
  const path = `/api/assistants/${assistantID}/browser`;
  const query = useQuery<BrowserStatus>(path, false, 2000);
  const resources = useQuery<Resource[]>('/api/resources?kind=compute', true);
  const grants = useQuery<Grant[]>(`/api/grants?assistant_id=${assistantID}`, true);
  const client = windowClient(assistantID);
  const [busy, setBusy] = useState(false);
  const sandbox = useQuery<SandboxInfo>('/api/sandbox');
  useErrorToast(sandbox.error, sandbox.reload);
  useErrorToast(resources.error, resources.reload);
  useErrorToast(grants.error, grants.reload);
  const { toast, dismiss } = useToast();
  const noticeID = useRef<number | null>(null);
  useErrorToast(query.error, query.reload);
  const [address, setAddress] = useState(''), [tabs, setTabs] = useState<Tab[]>([]);
  const [connected, setConnected] = useState(false), [hasFrame, setHasFrame] = useState(false);
  const { panelRef, moving, dragProps, moveKey, resizeProps } = useBrowserView(floating, bounds);
  const { reducedMotion } = useUILocale();
  const [keyboardOpen, setKeyboardOpen] = useState(false);
  const tabsRef = useRef<HTMLDivElement>(null), tabWindow = useRef<HTMLDivElement>(null);
  const [tabScroll, setTabScroll] = useState({ overflow: false, left: false, right: false });
  const [anchor, setAnchor] = useState({ x: 8, y: 8 });
  const canvas = useRef<HTMLCanvasElement>(null), directInput = useRef<HTMLTextAreaElement>(null), auxiliaryInput = useRef<HTMLTextAreaElement>(null), urlInput = useRef<HTMLInputElement>(null);
  const control = useRef(query.data); control.current = query.data;
  const task = query.data?.Task, state = task?.Browser;
  const ownsControl = state?.Control === 'user' && query.data?.ControllerIsCurrentSession && query.data.ControllerClientID === client.id;
  const canOperate = !!ownsControl && connected && !busy;
  const allowed = useRef(canOperate); allowed.current = canOperate;
  const closed = useRef(false), epoch = useRef(0), pumping = useRef(false), composing = useRef(false), seenURL = useRef('');
  const inputs = useRef(new BrowserInputBuffer());
  const nextInputAt = useRef(0);
  const execution = useExecutionOptions(resources.data, grants.data,
    !!(sandbox.data?.Enabled && sandbox.data.Capabilities.Shell && query.data?.SandboxAvailable),
    !!(sandbox.data?.Enabled && sandbox.data.Capabilities.FullAccess && query.data?.FullAccessAvailable));
  useEffect(() => { closed.current = false; return () => { closed.current = true; epoch.current++; inputs.current.clear(); }; }, []);
  useEffect(() => { epoch.current++; inputs.current.clear(); composing.current = false; if (directInput.current) directInput.current.value = ''; if (auxiliaryInput.current) auxiliaryInput.current.value = ''; }, [task?.ID, state?.Generation, ownsControl]);
  const activeTabID = tabs.find(tab => tab.active)?.id;
  const measureTabs = useCallback(() => {
    const list = tabsRef.current, container = tabWindow.current;
    if (!list || !container) return;
    const first = list.firstElementChild as HTMLElement | null;
    const minimum = first ? parseFloat(getComputedStyle(first).minWidth) : 0;
    const gap = parseFloat(getComputedStyle(list).gap) || 0;
    const overflow = list.children.length * minimum + Math.max(0, list.children.length - 1) * gap > container.clientWidth + 1;
    setTabScroll(old => { const next = { overflow, left: list.scrollLeft > 1, right: list.scrollLeft + list.clientWidth < list.scrollWidth - 1 }; return old.overflow === next.overflow && old.left === next.left && old.right === next.right ? old : next; });
  }, []);
  useLayoutEffect(() => {
    const list = tabsRef.current, container = tabWindow.current;
    if (!list || !container) return;
    const observer = new ResizeObserver(measureTabs); observer.observe(list); observer.observe(container); measureTabs();
    return () => observer.disconnect();
  }, [task?.ID, measureTabs]);
  useLayoutEffect(() => { tabsRef.current?.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest', inline: 'nearest' }); measureTabs(); }, [activeTabID, tabs.length, floating, tabScroll.overflow, measureTabs]);
  function scrollTabs(direction: number) { const list = tabsRef.current; if (list) list.scrollBy({ left: direction * Math.max(120, list.clientWidth * 0.75), behavior: reducedMotion ? 'auto' : 'smooth' }); }
  function notice(err: unknown) {
    const messages: Record<string, [string, string]> = {
      conflict: ['控制权已变更，请重新接管。', 'Control changed. Take control again.'],
      unavailable: ['浏览器连接中断，后续输入已停止。请检查页面后继续。', 'Browser connection lost. Pending input stopped. Check the page before continuing.'],
      rate_limited: ['浏览器请求受到限流，请稍后继续。', 'Browser requests are rate limited. Please wait before continuing.'],
      failed_precondition: ['浏览器未能执行操作，后续输入已停止。请检查页面和任务状态。', 'Browser could not execute the operation. Pending input stopped. Check the page and task status.'],
      not_found: ['浏览器任务已结束，请重新启动。', 'The browser task has ended. Start it again.'],
      deadline_exceeded: ['浏览器操作超时，后续输入已停止。请检查页面后继续。', 'Browser operation timed out. Pending input stopped. Check the page before continuing.'],
    };
    const text = err instanceof APIError ? messages[err.code] : undefined;
    return text?.[zh ? 0 : 1] ?? (zh ? '浏览器操作未完成，请检查连接或任务状态。' : 'Browser operation failed. Check connection or task status.');
  }
  const preparation = query.data?.Preparation;
  useMaintenanceFailureToast(preparation?.state === 'failed' ? preparation.task_id : undefined, preparation?.error_code);
  const failure = query.data?.LastFailure;
  useEffect(() => {
    if (task) { if (noticeID.current !== null) { dismiss(noticeID.current); noticeID.current = null; } return; }
    if (!failure || client.lastFailure === failure.TaskID) return;
    client.lastFailure = failure.TaskID;
    const reasons: Record<string, [string, string]> = {
      process_limit: ['浏览器因进程／线程数达到授权上限而退出。请在计算资源授权中调整进程数后重试。', 'Browser stopped after reaching the authorized process/thread limit. Adjust the process limit in compute permissions and retry.'],
      memory_limit: ['浏览器因内存额度不足而退出。请在计算资源授权中调整内存额度后重试。', 'Browser stopped after exhausting its memory allowance. Adjust the memory limit in compute permissions and retry.'],
      timeout: ['浏览器已达到授权运行时长。请重新启动，或调整计算资源授权的运行时长。', 'Browser reached its authorized runtime limit. Start it again or adjust the duration in compute permissions.'],
      authorization_changed: ['浏览器已停止：资源授权发生变化或额度不可用。请检查助理的资源授权后重试。', 'Browser stopped because resource authorization changed or its quota became unavailable. Check the assistant’s resource permissions and retry.'],
      disk_space_low: ['浏览器因可用磁盘空间不足而停止。请释放磁盘空间后重试。', 'Browser stopped because disk space is low. Free disk space and retry.'],
      network_gateway_failed: ['浏览器的沙盒网络服务异常，请重新启动浏览器。', 'The browser sandbox network service failed. Restart the browser.'],
      interrupted: ['浏览器任务已中断，请重新启动。', 'The browser task was interrupted. Start it again.'],
    };
    const fallback = ['浏览器异常退出。请在任务面板查看该浏览器任务的错误输出，检查组件和系统依赖后重试。', 'Browser exited unexpectedly. Check the browser task’s error output in the task panel, then check components and system dependencies before retrying.'];
    warn((reasons[failure.Reason] ?? fallback)[zh ? 0 : 1], true);
  }, [failure?.TaskID, task?.ID, client, zh]);
  function warn(title: string, persistent = false) {
    if (closed.current) return;
    if (noticeID.current !== null) dismiss(noticeID.current);
    noticeID.current = toast({ title, tone: 'warning', ...(persistent ? { duration: 0 } : {}) });
  }
  async function act(action: Action, target = control.current) {
    return api.post<BrowserStatus>(path, { ...action, task_id: target?.Task?.ID, client_id: client.id, generation: target?.Task?.Browser?.Generation ?? 0, sequence: ++client.sequence });
  }
  async function command(action: Action) {
    setBusy(true); allowed.current = false; epoch.current++; inputs.current.clear();
    try { await client.queue; await act(action); await query.reload(); }
    catch (err) { warn(notice(err)); void query.reload(); }
    finally { if (!closed.current) setBusy(false); }
  }
  function input(action: Action, dragging = false): boolean {
    if (!allowed.current) return false;
    if (!inputs.current.push(action, dragging)) { warn(zh ? '浏览器尚未处理完输入，请稍后继续。' : 'Browser input is still pending. Please wait before continuing.'); return false; }
    if (pumping.current) return true;
    pumping.current = true;
    const version = epoch.current, target = control.current;
    client.queue = client.queue.then(async () => {
      try {
        while (inputs.current.length) {
          // At most eight input requests/second, leaving room for status and
          // other UI requests within Runtime's default request budget.
          const wait = nextInputAt.current - performance.now();
          if (wait > 0) await new Promise(resolve => setTimeout(resolve, wait));
          if (closed.current || version !== epoch.current || !allowed.current || target?.Task?.Browser?.Generation !== control.current?.Task?.Browser?.Generation) { inputs.current.clear(); break; }
          const actions = inputs.current.take();
          nextInputAt.current = performance.now() + 125;
          if (!batchable(actions[0])) { await act(actions[0], target); continue; }
          const sequence = client.sequence + 1; client.sequence += actions.length;
          const result = await api.post<{ completed: number; code?: string; reason?: string }>(path, { action: 'input_batch', actions, task_id: target?.Task?.ID, client_id: client.id, generation: target?.Task?.Browser?.Generation, sequence });
          if (!Number.isInteger(result.completed) || result.completed < 0 || result.completed > actions.length) throw new APIError('invalid_response');
          if (result.code) throw new APIError(result.code, '', 0, 0, result.reason);
          if (!result.completed) throw new APIError('invalid_response');
          if (version === epoch.current) inputs.current.prepend(actions.slice(result.completed));
        }
      } catch (err) { epoch.current++; inputs.current.clear(); warn(notice(err)); void query.reload(); }
      finally { pumping.current = false; }
    });
    return true;
  }
  useEffect(() => {
    setConnected(false); setHasFrame(false); setTabs([]); seenURL.current = '';
    if (!task?.ID) return;
    let alive = true, painting = false, latest: string | null = null;
    let source: EventSource | undefined, image: HTMLImageElement | undefined;
    let retry: ReturnType<typeof setTimeout> | undefined, frame: number | undefined;
    const visible = () => document.visibilityState === 'visible' && navigator.onLine !== false;
    function schedule() {
      if (alive && source && latest && !painting && frame === undefined) frame = requestAnimationFrame(draw);
    }
    function draw() {
      frame = undefined;
      if (!alive || !source || painting || !latest) return;
      const data = latest; latest = null; painting = true;
      if (!image) {
        const decoder = image = new Image();
        decoder.onload = () => {
          if (!alive || image !== decoder || !source) return;
          frame = requestAnimationFrame(() => {
            frame = undefined;
            const target = canvas.current;
            if (target) {
              if (target.width !== decoder.naturalWidth) target.width = decoder.naturalWidth;
              if (target.height !== decoder.naturalHeight) target.height = decoder.naturalHeight;
              target.getContext('2d')?.drawImage(decoder, 0, 0);
              setHasFrame(true);
            }
            painting = false; schedule();
          });
        };
        decoder.onerror = () => { if (image === decoder) { painting = false; schedule(); } };
      }
      image.src = `data:image/jpeg;base64,${data}`;
    }
    function disconnect() {
      if (source) { source.onopen = source.onerror = source.onmessage = null; source.close(); source = undefined; }
      clearTimeout(retry); retry = undefined;
      if (frame !== undefined) cancelAnimationFrame(frame);
      frame = undefined;
      if (image) { image.onload = image.onerror = null; image.removeAttribute('src'); image = undefined; }
      latest = null; painting = false;
      epoch.current++; inputs.current.clear(); allowed.current = false;
      if (alive) { setConnected(false); setHasFrame(false); }
    }
    function connect() {
      retry = undefined;
      if (!alive || !visible() || source) return;
      const current = source = new EventSource(`${path}/stream?task_id=${encodeURIComponent(task!.ID)}`);
      current.onopen = () => { if (alive && source === current) { setConnected(true); void query.reload(); } };
      current.onerror = () => {
        if (source !== current) return;
        disconnect();
        if (alive && visible()) retry = setTimeout(connect, 2000);
      };
      current.onmessage = event => {
        if (!alive || source !== current) return;
        try {
          const message = JSON.parse(event.data);
          if (message.type === 'frame' && typeof message.data === 'string') { latest = message.data; schedule(); }
          else if (message.type === 'tabs' && Array.isArray(message.tabs)) {
            const next: Tab[] = message.tabs.map((t: Record<string, unknown>) => ({ id: String(t.tabId), title: String(t.title ?? ''), url: String(t.url ?? ''), active: !!t.active }));
            setTabs(old => old.length === next.length && old.every((t, i) => t.id === next[i].id && t.title === next[i].title && t.url === next[i].url && t.active === next[i].active) ? old : next);
            const current = next.find(t => t.active)?.url;
            if (current && current !== seenURL.current) { seenURL.current = current; setAddress(current); }
          }
        } catch { /* Never render upstream markup. */ }
      };
    }
    const resume = () => { if (visible()) { if (retry === undefined) connect(); } else disconnect(); };
    document.addEventListener('visibilitychange', resume);
    window.addEventListener('online', resume); window.addEventListener('offline', resume);
    connect();
    return () => {
      alive = false; disconnect();
      document.removeEventListener('visibilitychange', resume);
      window.removeEventListener('online', resume); window.removeEventListener('offline', resume);
    };
  }, [path, task?.ID]);
  // Resize only after layout settles, and serialize it with this window's inputs.
  useEffect(() => {
    const element = canvas.current;
    if (!element || !task?.ID || !connected || state?.Control === 'switching' || state?.Control === 'user' && !ownsControl) return;
    let disposed = false, sent = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const measure = () => {
      clearTimeout(timer);
      timer = setTimeout(() => {
        const { width, height } = element.getBoundingClientRect();
        if (width <= 0 || height <= 0 || document.visibilityState !== 'visible') return;
        const next = Math.max(1, Math.min(8192, Math.round(1280 * height / width)));
        if (sent === next) return;
        sent = next;
        client.queue = client.queue.then(async () => {
          if (disposed || sent !== next) return;
          try { await act({ action: 'viewport', height: next }); }
          catch (err) { if (!disposed) { sent = 0; warn(notice(err)); void query.reload(); } }
        });
      }, 200);
    };
    const observer = new ResizeObserver(measure);
    observer.observe(element); measure();
    return () => { disposed = true; clearTimeout(timer); observer.disconnect(); };
  }, [path, task?.ID, connected, state?.Control, state?.Generation, ownsControl]);
  function coordinates(event: PointerEvent<HTMLCanvasElement> | WheelEvent<HTMLCanvasElement>) {
    const rect = event.currentTarget.getBoundingClientRect();
    // Use the received frame dimensions, including while a resize is in flight.
    const { width, height } = event.currentTarget;
    const scale = Math.min(rect.width / width, rect.height / height), left = (rect.width - width * scale) / 2;
    const x = Math.max(0, Math.min(width, (event.clientX - rect.left - left) / scale)), y = Math.max(0, Math.min(height, (event.clientY - rect.top) / scale));
    return { x, y };
  }
  function modifiers(event: PointerEvent<HTMLCanvasElement> | WheelEvent<HTMLCanvasElement>) { return (event.altKey ? 1 : 0) | (event.ctrlKey ? 2 : 0) | (event.metaKey ? 4 : 0) | (event.shiftKey ? 8 : 0); }
  function pointer(event: PointerEvent<HTMLCanvasElement>, kind: 'move' | 'down' | 'up') {
    if (!canOperate || moving) return;
    if (kind === 'down') {
      event.preventDefault(); event.currentTarget.setPointerCapture(event.pointerId);
      const viewport = event.currentTarget.parentElement!.getBoundingClientRect();
      setAnchor({ x: Math.max(4, Math.min(viewport.width - 24, event.clientX - viewport.left)), y: Math.max(4, Math.min(viewport.height - 28, event.clientY - viewport.top)) });
      (keyboardOpen ? auxiliaryInput.current : directInput.current)?.focus({ preventScroll: true });
    }
    input({ action: 'pointer', event: kind, ...coordinates(event), modifiers: modifiers(event), button: ['left', 'middle', 'right'][event.button] ?? 'left' }, event.buttons !== 0);
  }
  function wheel(event: WheelEvent<HTMLCanvasElement>) {
    if (!canOperate || moving || !event.deltaX && !event.deltaY) return;
    const unit = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? 800 : 1;
    input({ action: 'wheel', ...coordinates(event), modifiers: modifiers(event), delta_x: Math.max(-100000, Math.min(100000, event.deltaX * unit)), delta_y: Math.max(-100000, Math.min(100000, event.deltaY * unit)) });
  }
  function commitText(element: HTMLTextAreaElement, text = element.value) { if (!text || input({ action: 'insert_text', text })) element.value = ''; }
  function textChanged(event: ChangeEvent<HTMLTextAreaElement>) { if (!composing.current && !(event.nativeEvent as InputEvent).isComposing) commitText(event.currentTarget); }
  function keyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (composing.current || event.nativeEvent.isComposing || event.keyCode === 229) return;
    const shortcut = event.ctrlKey || event.metaKey;
    if (shortcut && event.key.toLowerCase() === 'v') return;
    if (shortcut && ['l', 't', 'w'].includes(event.key.toLowerCase())) {
      event.preventDefault();
      if (event.key.toLowerCase() === 'l') { urlInput.current?.focus(); urlInput.current?.select(); }
      else input(event.key.toLowerCase() === 't' ? { action: 'new_tab', url: 'about:blank' } : { action: 'close_tab', tab: tabs.find(t => t.active)?.id });
      return;
    }
    if (event.key.length > 1 || shortcut || event.altKey) {
      if (['Process', 'Unidentified', 'Control', 'Meta', 'Alt', 'Shift'].includes(event.key)) return;
      event.preventDefault(); event.stopPropagation();
      input({ action: 'press', key: [event.ctrlKey ? 'Control' : '', event.metaKey ? 'Meta' : '', event.altKey ? 'Alt' : '', event.shiftKey ? 'Shift' : '', event.key].filter(Boolean).join('+') });
    }
  }
  const keyboardProps = {
    disabled: !canOperate, autoComplete: 'off', autoCorrect: 'off', autoCapitalize: 'off', spellCheck: false,
    onChange: textChanged, onKeyDown: keyDown,
    onCompositionStart: () => { composing.current = true; },
    onCompositionEnd: (e: React.CompositionEvent<HTMLTextAreaElement>) => { composing.current = false; commitText(e.currentTarget, e.data || e.currentTarget.value); },
  };
  function tabTitle(tab: Tab) { if (tab.url === 'about:blank') return zh ? '新标签页' : 'New tab'; return tab.title || tab.url || (zh ? '标签页' : 'Tab'); }
  const loading = !query.data && query.loading || !sandbox.data && sandbox.loading || !resources.data && resources.loading || !grants.data && grants.loading;
  const installing = query.data?.Preparation?.state === 'installing';
  const componentsUnavailable = query.data?.Available === false && !component?.can_install && !query.data.ComponentsEnabled;
  const controlLabel = state?.Control === 'switching' ? (zh ? '正在交接' : 'Handing over') : ownsControl ? (zh ? '你正在控制' : 'You are in control') : state?.Control === 'user' ? (zh ? '其他窗口正在控制' : 'Controlled in another window') : (zh ? '助理控制中' : 'Assistant in control');
  const viewActions = <div className="browser-view-actions">{!mobile && <IconButton label={floating ? (zh ? '停靠侧栏' : 'Dock to sidebar') : (zh ? '浮窗模式' : 'Floating window')} onClick={() => onFloatingChange(!floating)}>{floating ? <PanelRight size={18}/> : <PictureInPicture2 size={18}/>}</IconButton>}
    <IconButton label={zh ? '关闭浏览器面板' : 'Close browser panel'} onClick={onClose}><X size={18}/></IconButton></div>;
  const moveHandle = floating && <IconButton className="browser-move" label={zh ? '移动浮窗（方向键微调）' : 'Move window (arrow keys to adjust)'} onKeyDown={moveKey}><GripHorizontal size={18}/></IconButton>;
  return <Drawer open docked={floating || docked} side={mobile ? 'bottom' : 'right'} onOpenChange={open => { if (!open) onClose(); }} title={zh ? '浏览器' : 'Browser'} closeLabel={zh ? '关闭浏览器面板' : 'Close browser panel'} showCloseButton={false} size="lg" className={`browser-drawer${task ? ' browser-drawer--running' : ''}${floating ? ' browser-drawer--floating' : ''}${moving ? ' browser-drawer--moving' : ''}`} headerContent={task ?
    <div className="browser-tabstrip" {...dragProps}>
      {moveHandle}
      <div className="browser-tabs-window" ref={tabWindow}>
        {tabScroll.overflow && <IconButton className="browser-tab-scroll" label={zh ? '向左滚动标签页' : 'Scroll tabs left'} disabled={!tabScroll.left} onClick={() => scrollTabs(-1)}><ChevronLeft size={16}/></IconButton>}
        <div className="browser-tabs" ref={tabsRef} role="tablist" aria-label={zh ? '浏览器标签页' : 'Browser tabs'} onScroll={measureTabs}>{tabs.map(tab => <div className="browser-tab" data-active={tab.active || undefined} key={tab.id}>
          <button role="tab" aria-selected={tab.active} aria-label={tabTitle(tab)} title={tab.url} disabled={!canOperate} onClick={() => input({ action: 'select_tab', tab: tab.id })}><Globe size={13}/><span>{tabTitle(tab)}</span></button>
          <IconButton label={`${zh ? '关闭标签页' : 'Close tab'}: ${tabTitle(tab)}`} disabled={!canOperate} onClick={() => input({ action: 'close_tab', tab: tab.id })}><X size={13}/></IconButton>
        </div>)}</div>
        {tabScroll.overflow && <IconButton className="browser-tab-scroll" label={zh ? '向右滚动标签页' : 'Scroll tabs right'} disabled={!tabScroll.right} onClick={() => scrollTabs(1)}><ChevronRight size={16}/></IconButton>}
      </div>
      <IconButton className="browser-new-tab" label={zh ? '新建标签页' : 'New tab'} disabled={!canOperate} onClick={() => input({ action: 'new_tab', url: 'about:blank' })}><Plus size={18}/></IconButton>
      {viewActions}
    </div> : <div className="browser-window-heading" {...dragProps}>{moveHandle}<span>{zh ? '浏览器' : 'Browser'}</span>{viewActions}</div>}>
    <div className="browser-panel" ref={panelRef}>
      {!task ? <SessionStart icon={<Globe/>} title={zh ? '一起浏览，同步可见' : 'Browse together'} description={zh ? '观看助理浏览网页，需要时接管。登录状态会为这个助理保留。' : 'Watch your assistant browse, and take control when needed. Login state stays with this assistant.'}
        status={loading ? (zh ? '加载中…' : 'Loading…') : query.error || sandbox.error ? (zh ? '暂时无法确认浏览器能力。' : 'Browser capabilities could not be confirmed.') : resources.error || grants.error ? (zh ? '暂时无法确认计算权限。' : 'Compute permissions could not be confirmed.') : componentsUnavailable ? (component ? maintenanceReason(component.reason, zh) : zh ? '请联系管理员安装浏览器组件。' : 'Ask an administrator to install browser components.') : undefined}
        action={<Button disabled={busy || loading || !execution.valid || !!query.error || !!resources.error || !!grants.error || !!sandbox.error || installing || componentsUnavailable} loading={busy || installing} onClick={() => void command({ action: query.data?.Available === false && !query.data.ComponentsEnabled ? 'install_start' : 'start', compute_id: execution.compute, full_access: execution.fullAccess })}>{busy || installing ? (zh ? '正在准备…' : 'Preparing…') : query.data?.Available === false && !componentsUnavailable ? (zh ? '安装并启动' : 'Install and start') : (zh ? '启动浏览器' : 'Start browser')}</Button>}>
        {query.data?.Available === false && !componentsUnavailable && component && <ComponentDetails value={component}/>}
        {query.data?.Preparation?.task_id && admin && <MaintenanceProgress taskID={query.data.Preparation.task_id}/>}
        {!loading && !componentsUnavailable && !query.error && !sandbox.error && !resources.error && !grants.error && <ExecutionOptions value={execution} zh={zh} disabled={busy || installing}/>}
      </SessionStart> : <>
        <form className="browser-navigation" onSubmit={e => { e.preventDefault(); input({ action: 'navigate', url: /^https?:\/\//i.test(address) || address === 'about:blank' ? address : `https://${address}` }); (keyboardOpen ? auxiliaryInput.current : directInput.current)?.focus({ preventScroll: true }); }}>
          <IconButton type="button" disabled={!canOperate} label={zh ? '后退' : 'Back'} onClick={() => input({ action: 'back' })}><ArrowLeft size={17}/></IconButton><IconButton type="button" disabled={!canOperate} label={zh ? '前进' : 'Forward'} onClick={() => input({ action: 'forward' })}><ArrowRight size={17}/></IconButton><IconButton type="button" disabled={!canOperate} label={zh ? '刷新' : 'Reload'} onClick={() => input({ action: 'reload' })}><RefreshCw size={16}/></IconButton>
          <div className="browser-address"><Globe size={14}/><input ref={urlInput} aria-label={zh ? '网址' : 'Address'} disabled={!canOperate} value={address === 'about:blank' ? '' : address} onFocus={e => e.target.select()} onChange={e => setAddress(e.target.value)} placeholder={zh ? '输入网址' : 'Enter a URL'}/><IconButton type="submit" label={zh ? '打开' : 'Go'} disabled={!canOperate}><CornerDownLeft size={15}/></IconButton></div>
          <Button className="browser-control-button" type="button" size="sm" variant={ownsControl ? 'secondary' : 'primary'} title={controlLabel} disabled={busy || moving || !connected || state?.Control === 'switching'} onClick={() => void command({ action: ownsControl ? 'release' : 'takeover' })}><Hand size={14}/>{ownsControl ? (zh ? '交还' : 'Return control') : (zh ? '接管' : 'Take control')}</Button>
        </form>
        <div className="browser-viewport" data-interactive={canOperate || undefined}>
          <canvas ref={canvas} width={1280} height={800} aria-label={zh ? '浏览器实时画面' : 'Live browser view'} onPointerDown={e => pointer(e, 'down')} onPointerUp={e => pointer(e, 'up')} onPointerMove={e => pointer(e, 'move')} onPointerCancel={e => pointer(e, 'up')} onContextMenu={e => e.preventDefault()} onWheel={wheel}/>
          <textarea ref={directInput} {...keyboardProps} className="browser-direct-input" aria-label={zh ? '网页键盘输入' : 'Page keyboard input'} tabIndex={-1} style={{ left: anchor.x, top: anchor.y }} />
          {(!connected || !hasFrame) && <div className="browser-overlay"><Globe size={24}/><span>{zh ? '正在连接浏览器…' : 'Connecting to browser…'}</span></div>}
        </div>
        {keyboardOpen && <div className="browser-input-tray"><textarea ref={auxiliaryInput} {...keyboardProps} className="browser-keyboard" aria-label={zh ? '辅助输入' : 'Keyboard input'} placeholder={zh ? '先点击网页中的输入框，再在这里输入或粘贴' : 'Click a field in the page, then type or paste here'}/></div>}
        <div className="browser-statusbar"><span role="status" className="browser-connection" data-connected={connected || undefined}><i/><span>{connected ? (zh ? '已连接' : 'Connected') : (zh ? '连接中…' : 'Connecting…')}{query.data?.Proxy?.Mode === 'proxy' && (zh ? ' · 代理已启用' : ' · Proxy enabled')}</span></span><div className="browser-status-actions"><Badge tone={task.FullAccess ? 'warning' : 'neutral'}>{task.FullAccess ? 'Full Access' : zh ? '沙盒' : 'Sandbox'}</Badge><IconButton label={keyboardOpen ? (zh ? '收起辅助输入' : 'Hide keyboard input') : (zh ? '辅助输入' : 'Keyboard input')} aria-pressed={keyboardOpen} disabled={!ownsControl} onClick={() => { composing.current = false; setKeyboardOpen(!keyboardOpen); }}><Keyboard size={17}/></IconButton><IconButton label={zh ? '结束浏览器会话' : 'End browser session'} disabled={busy} onClick={() => void command({ action: 'close' })}><Power size={16}/></IconButton></div></div>
      </>}
    </div>
    {floating && <button type="button" className="browser-resize" aria-label={zh ? '调整浮窗大小（方向键微调）' : 'Resize window (arrow keys to adjust)'} {...resizeProps}/>}
  </Drawer>;
}
