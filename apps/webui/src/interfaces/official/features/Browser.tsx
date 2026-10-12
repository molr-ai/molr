import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { ChangeEvent, KeyboardEvent, PointerEvent } from 'react';
import { Alert, Badge, Button, Drawer, IconButton, useBreakpoint, useUILocale } from '@molr/ui';
import { ArrowLeft, ArrowRight, ChevronLeft, ChevronRight, CornerDownLeft, Globe, Hand, Keyboard, Maximize2, Minimize2, Plus, Power, RefreshCw, X } from 'lucide-react';
import { api, APIError, mutationKey } from '../../../api/client';
import type { BrowserStatus, Grant, Resource } from '../../../api/types';
import { useCopy, useQuery } from '../../../state';
import './browser.css';
import { useBrowserView } from './useBrowserView';

type Action = { action: string; [key: string]: unknown };
type Tab = { id: string; title: string; url: string; active: boolean };
// A panel is ephemeral; the controlling window is not. IDs are deliberately not
// stored in localStorage/sessionStorage where another tab could inherit them.
const clients = new Map<string, { id: string; sequence: number; queue: Promise<void> }>();
function windowClient(assistantID: string) {
  let client = clients.get(assistantID);
  if (!client) { client = { id: mutationKey(), sequence: 0, queue: Promise.resolve() }; clients.set(assistantID, client); }
  return client;
}
export function BrowserButton({ assistantID, disabled, onOpen }: { assistantID: string; disabled?: boolean; onOpen: () => void }) {
  const { locale } = useCopy();
  const cadence = useCallback((data?: BrowserStatus) => data?.Task ? 2000 : 5000, []);
  const { data } = useQuery<BrowserStatus>(disabled ? null : `/api/assistants/${assistantID}/browser`, false, cadence);
  const count = data?.Task ? data.TabCount : undefined;
  return <IconButton className="chat-count-trigger" disabled={disabled} onClick={onOpen} label={`${locale === 'zh' ? '浏览器' : 'Browser'}${count ? ` (${count})` : ''}`}><Globe size={20}/>{!!count && <Badge tone="positive" size="sm" dot={false} aria-hidden="true">{count}</Badge>}</IconButton>;
}
export function Browser({ assistantID, docked, onClose }: { assistantID: string; docked: boolean; onClose: () => void }) {
  const { locale } = useCopy(), zh = locale === 'zh', mobile = useBreakpoint('mobile');
  const path = `/api/assistants/${assistantID}/browser`;
  const query = useQuery<BrowserStatus>(path, false, 2000);
  const resources = useQuery<Resource[]>('/api/resources?kind=compute', true);
  const grants = useQuery<Grant[]>(`/api/grants?assistant_id=${assistantID}`, true);
  const client = windowClient(assistantID);
  const [compute, setCompute] = useState(''), [busy, setBusy] = useState(false), [error, setError] = useState('');
  const [address, setAddress] = useState(''), [tabs, setTabs] = useState<Tab[]>([]);
  const [connected, setConnected] = useState(false), [hasFrame, setHasFrame] = useState(false);
  const { panelRef, expanded, moving, changeView } = useBrowserView(docked);
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
  const closed = useRef(false), epoch = useRef(0), pending = useRef(0), lastMove = useRef(0), composing = useRef(false), seenURL = useRef('');
  const usable = (resources.data ?? []).filter(r => (r.SandboxID ?? 'local') === 'local' && (grants.data ?? []).some(g => g.ResourceID === r.ID && !g.Scope.FullAccess && !g.RevokedAt && (!g.ExpiresAt || Date.parse(g.ExpiresAt) > Date.now()) && g.Actions.includes('execute')));
  useEffect(() => { if (!compute && usable[0]) setCompute(usable[0].ID); }, [compute, usable]);
  useEffect(() => { closed.current = false; return () => { closed.current = true; epoch.current++; }; }, []);
  useEffect(() => { epoch.current++; composing.current = false; if (directInput.current) directInput.current.value = ''; if (auxiliaryInput.current) auxiliaryInput.current.value = ''; }, [task?.ID, state?.Generation, ownsControl]);
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
  useLayoutEffect(() => { tabsRef.current?.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest', inline: 'nearest' }); measureTabs(); }, [activeTabID, tabs.length, expanded, tabScroll.overflow, measureTabs]);
  function scrollTabs(direction: number) { const list = tabsRef.current; if (list) list.scrollBy({ left: direction * Math.max(120, list.clientWidth * 0.75), behavior: reducedMotion ? 'auto' : 'smooth' }); }
  function notice(err: unknown) {
    return err instanceof APIError && err.code === 'conflict' ? (zh ? '控制权已变更，请重新接管。' : 'Control changed. Take control again.') : (zh ? '浏览器操作未完成，请检查连接或任务状态。' : 'Browser operation failed. Check connection or task status.');
  }
  async function act(action: Action, target = control.current) {
    return api.post<BrowserStatus>(path, { ...action, task_id: target?.Task?.ID, client_id: client.id, generation: target?.Task?.Browser?.Generation ?? 0, sequence: ++client.sequence });
  }
  async function command(action: Action) {
    setBusy(true); setError(''); epoch.current++;
    try { await client.queue; await act(action); await query.reload(); }
    catch (err) { setError(notice(err)); }
    finally { if (!closed.current) setBusy(false); }
  }
  function input(action: Action) {
    if (!allowed.current) return;
    if (action.action === 'pointer' && action.event === 'move') { const now = performance.now(); if (now - lastMove.current < 80) return; lastMove.current = now; }
    if (pending.current >= 64 && action.event !== 'up') { setError(zh ? '输入较快，请稍后继续。' : 'Input is busy; please wait.'); return; }
    const version = epoch.current, target = control.current;
    pending.current++;
    client.queue = client.queue.then(async () => {
      pending.current--;
      if (closed.current || version !== epoch.current || !allowed.current || target?.Task?.Browser?.Generation !== control.current?.Task?.Browser?.Generation) return;
      try { await act(action, target); }
      catch (err) { epoch.current++; setError(notice(err)); void query.reload(); }
    });
  }
  useEffect(() => {
    setConnected(false); setHasFrame(false); setTabs([]); seenURL.current = '';
    if (!task?.ID) return;
    let alive = true, painting = false, latest: string | null = null;
    let source: EventSource, retry: ReturnType<typeof setTimeout> | undefined;
    function draw() {
      if (!alive || painting || !latest) return;
      const data = latest; latest = null; painting = true;
      const image = new Image();
      image.onload = () => { if (alive && canvas.current) { canvas.current.width = image.naturalWidth; canvas.current.height = image.naturalHeight; canvas.current.getContext('2d')?.drawImage(image, 0, 0); setHasFrame(true); } painting = false; if (alive && latest) requestAnimationFrame(draw); };
      image.onerror = () => { painting = false; }; image.src = `data:image/jpeg;base64,${data}`;
    }
    function connect() {
      source = new EventSource(`${path}/stream?task_id=${encodeURIComponent(task!.ID)}`);
      source.onopen = () => { if (alive) { setConnected(true); void query.reload(); } };
      source.onerror = () => { source.close(); epoch.current++; if (alive) { setConnected(false); retry = setTimeout(connect, 2000); } };
      source.onmessage = event => {
        if (!alive) return;
        try {
          const message = JSON.parse(event.data);
          if (message.type === 'frame' && typeof message.data === 'string') { latest = message.data; draw(); }
          else if (message.type === 'tabs' && Array.isArray(message.tabs)) {
            const next: Tab[] = message.tabs.map((t: Record<string, unknown>) => ({ id: String(t.tabId), title: String(t.title ?? ''), url: String(t.url ?? ''), active: !!t.active }));
            setTabs(next);
            const current = next.find(t => t.active)?.url;
            if (current && current !== seenURL.current) { seenURL.current = current; setAddress(current); }
          }
        } catch { /* Never render upstream markup. */ }
      };
    }
    connect();
    return () => { alive = false; source?.close(); clearTimeout(retry); latest = null; };
  }, [path, task?.ID]);
  function pointer(event: PointerEvent<HTMLCanvasElement>, kind: 'move' | 'down' | 'up') {
    if (!canOperate || moving) return;
    const rect = event.currentTarget.getBoundingClientRect();
    // Account for the letterboxing in the page-wide view.
    const scale = Math.min(rect.width / 1280, rect.height / 800), left = (rect.width - 1280 * scale) / 2, top = 0;
    const x = Math.max(0, Math.min(1280, (event.clientX - rect.left - left) / scale)), y = Math.max(0, Math.min(800, (event.clientY - rect.top - top) / scale));
    if (kind === 'down') {
      event.preventDefault(); event.currentTarget.setPointerCapture(event.pointerId);
      const viewport = event.currentTarget.parentElement!.getBoundingClientRect();
      setAnchor({ x: Math.max(4, Math.min(viewport.width - 24, event.clientX - viewport.left)), y: Math.max(4, Math.min(viewport.height - 28, event.clientY - viewport.top)) });
      (keyboardOpen ? auxiliaryInput.current : directInput.current)?.focus({ preventScroll: true });
    }
    input({ action: 'pointer', event: kind, x, y, button: ['left', 'middle', 'right'][event.button] ?? 'left' });
  }
  function commitText(element: HTMLTextAreaElement, text = element.value) { if (text) input({ action: 'insert_text', text }); element.value = ''; }
  function textChanged(event: ChangeEvent<HTMLTextAreaElement>) { if (!composing.current && !(event.nativeEvent as InputEvent).isComposing) commitText(event.currentTarget); }
  function keyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (composing.current || event.nativeEvent.isComposing || event.keyCode === 229) return;
    if (expanded && event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); changeView(false); return; }
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
  const controlLabel = state?.Control === 'switching' ? (zh ? '正在交接' : 'Handing over') : ownsControl ? (zh ? '你正在控制' : 'You are in control') : state?.Control === 'user' ? (zh ? '其他窗口正在控制' : 'Controlled in another window') : (zh ? '助理控制中' : 'Assistant in control');
  return <Drawer open docked={docked && !expanded} side={mobile && !expanded ? 'bottom' : 'right'} onOpenChange={open => { if (!open) { if (expanded) changeView(false); else onClose(); } }} title={zh ? '浏览器' : 'Browser'} closeLabel={zh ? '关闭浏览器面板' : 'Close browser panel'} showCloseButton={false} size="lg" className={`browser-drawer${expanded ? ' browser-drawer--expanded' : ''}`} headerContent={
    <div className="browser-tabstrip">
      <div className="browser-tabs-window" ref={tabWindow}>
        {tabScroll.overflow && <IconButton className="browser-tab-scroll" label={zh ? '向左滚动标签页' : 'Scroll tabs left'} disabled={!tabScroll.left} onClick={() => scrollTabs(-1)}><ChevronLeft size={16}/></IconButton>}
        <div className="browser-tabs" ref={tabsRef} role="tablist" aria-label={zh ? '浏览器标签页' : 'Browser tabs'} onScroll={measureTabs}>{tabs.map(tab => <div className="browser-tab" data-active={tab.active || undefined} key={tab.id}>
          <button role="tab" aria-selected={tab.active} aria-label={tabTitle(tab)} title={tab.url} disabled={!canOperate} onClick={() => input({ action: 'select_tab', tab: tab.id })}><Globe size={13}/><span>{tabTitle(tab)}</span></button>
          <IconButton label={`${zh ? '关闭标签页' : 'Close tab'}: ${tabTitle(tab)}`} disabled={!canOperate} onClick={() => input({ action: 'close_tab', tab: tab.id })}><X size={13}/></IconButton>
        </div>)}</div>
        {tabScroll.overflow && <IconButton className="browser-tab-scroll" label={zh ? '向右滚动标签页' : 'Scroll tabs right'} disabled={!tabScroll.right} onClick={() => scrollTabs(1)}><ChevronRight size={16}/></IconButton>}
      </div>
      <IconButton className="browser-new-tab" label={zh ? '新建标签页' : 'New tab'} disabled={!canOperate} onClick={() => input({ action: 'new_tab', url: 'about:blank' })}><Plus size={18}/></IconButton>
      <div className="browser-view-actions"><IconButton disabled={moving} label={expanded ? (zh ? '退出全屏' : 'Exit full-page view') : (zh ? '全屏视图' : 'Full-page view')} onClick={() => changeView(!expanded)}>{expanded ? <Minimize2 size={16}/> : <Maximize2 size={16}/>}</IconButton>
      <IconButton label={zh ? '关闭浏览器面板' : 'Close browser panel'} onClick={onClose}><X size={17}/></IconButton></div>
    </div>}>
    <div className="browser-panel" ref={panelRef}>
      {(!!error || !!query.error) && <div className="browser-notice"><Alert tone="warning" title={zh ? '浏览器提示' : 'Browser notice'}>{error || (zh ? '暂时无法获取浏览器状态' : 'Unable to load browser status')}</Alert></div>}
      {!task ? <div className="browser-start"><div className="browser-start-icon"><Globe size={34}/></div><h3>{zh ? '一起浏览，同步可见' : 'Browse together'}</h3><p>{zh ? '观看助理浏览网页，需要时接管。登录状态会为这个助理保留。' : 'Watch your assistant browse, and take control when needed. Login state stays with this assistant.'}</p>
        {query.data?.Available === false ? <Alert title={zh ? '浏览器程序尚未配置' : 'Browser programs are not configured'}>{zh ? '请联系管理员配置浏览器程序。' : 'Ask an administrator to configure browser programs.'}</Alert> : <><label>{zh ? '计算资源' : 'Compute resource'}<select value={compute} onChange={e => setCompute(e.target.value)}><option value="">{zh ? '选择已授权的本机资源' : 'Select an authorized local resource'}</option>{usable.map(r => <option key={r.ID} value={r.ID}>{r.Name}</option>)}</select></label><Button disabled={busy || !compute || query.loading && !query.data} onClick={() => void command({ action: 'start', compute_id: compute })}>{busy ? (zh ? '正在启动…' : 'Starting…') : (zh ? '启动浏览器' : 'Start browser')}</Button></>}
      </div> : <>
        <form className="browser-navigation" onSubmit={e => { e.preventDefault(); input({ action: 'navigate', url: /^https?:\/\//i.test(address) || address === 'about:blank' ? address : `https://${address}` }); (keyboardOpen ? auxiliaryInput.current : directInput.current)?.focus({ preventScroll: true }); }}>
          <IconButton type="button" disabled={!canOperate} label={zh ? '后退' : 'Back'} onClick={() => input({ action: 'back' })}><ArrowLeft size={17}/></IconButton><IconButton type="button" disabled={!canOperate} label={zh ? '前进' : 'Forward'} onClick={() => input({ action: 'forward' })}><ArrowRight size={17}/></IconButton><IconButton type="button" disabled={!canOperate} label={zh ? '刷新' : 'Reload'} onClick={() => input({ action: 'reload' })}><RefreshCw size={16}/></IconButton>
          <div className="browser-address"><Globe size={14}/><input ref={urlInput} aria-label={zh ? '网址' : 'Address'} disabled={!canOperate} value={address === 'about:blank' ? '' : address} onFocus={e => e.target.select()} onChange={e => setAddress(e.target.value)} placeholder={zh ? '输入网址' : 'Enter a URL'}/><IconButton type="submit" label={zh ? '打开' : 'Go'} disabled={!canOperate}><CornerDownLeft size={15}/></IconButton></div>
          <Button className="browser-control-button" type="button" size="sm" variant={ownsControl ? 'secondary' : 'primary'} title={controlLabel} disabled={busy || moving || !connected || state?.Control === 'switching'} onClick={() => void command({ action: ownsControl ? 'release' : 'takeover' })}><Hand size={14}/>{ownsControl ? (zh ? '交还助理' : 'Return control') : (zh ? '接管' : 'Take control')}</Button>
        </form>
        <div className="browser-viewport" data-interactive={canOperate || undefined}>
          <canvas ref={canvas} width={1280} height={800} aria-label={zh ? '浏览器实时画面' : 'Live browser view'} onPointerDown={e => pointer(e, 'down')} onPointerUp={e => pointer(e, 'up')} onPointerMove={e => pointer(e, 'move')} onPointerCancel={e => pointer(e, 'up')} onContextMenu={e => e.preventDefault()} onWheel={e => { if (canOperate) input({ action: 'scroll', direction: e.deltaY < 0 ? 'up' : 'down' }); }}/>
          <textarea ref={directInput} {...keyboardProps} className="browser-direct-input" aria-label={zh ? '网页键盘输入' : 'Page keyboard input'} tabIndex={-1} style={{ left: anchor.x, top: anchor.y }} />
          {(!connected || !hasFrame) && <div className="browser-overlay"><Globe size={24}/><span>{zh ? '正在连接浏览器…' : 'Connecting to browser…'}</span></div>}
        </div>
        {keyboardOpen && <div className="browser-input-tray"><textarea ref={auxiliaryInput} {...keyboardProps} className="browser-keyboard" aria-label={zh ? '辅助输入' : 'Keyboard input'} placeholder={zh ? '先点击网页中的输入框，再在这里输入或粘贴' : 'Click a field in the page, then type or paste here'}/></div>}
        <div className="browser-statusbar"><span role="status" className="browser-connection" data-connected={connected || undefined}><i/>{connected ? (zh ? '已连接' : 'Connected') : (zh ? '连接中…' : 'Connecting…')}</span><span className="browser-control-label">{controlLabel}</span><span className="browser-input-hint">{ownsControl ? (zh ? '点击网页即可输入' : 'Click the page to type') : (zh ? '接管后可操作网页' : 'Take control to interact')}</span><div className="browser-status-actions"><IconButton label={keyboardOpen ? (zh ? '收起辅助输入' : 'Hide keyboard input') : (zh ? '辅助输入' : 'Keyboard input')} aria-pressed={keyboardOpen} disabled={!ownsControl} onClick={() => { composing.current = false; setKeyboardOpen(!keyboardOpen); }}><Keyboard size={17}/></IconButton><IconButton label={zh ? '结束浏览器会话' : 'End browser session'} disabled={busy} onClick={() => void command({ action: 'close' })}><Power size={16}/></IconButton></div></div>
      </>}
    </div>
  </Drawer>;
}
