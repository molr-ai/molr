import { useEffect, useRef, useState } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { Alert, Badge, Button, IconButton, Tooltip, copyTextToClipboard, useToast } from '@molr/ui';
import { Copy, Power, RefreshCw } from 'lucide-react';
import { api, APIError, mutationKey } from '@molr/sdk';
import type { ShellTask, ShellTaskAction, ShellTaskOutput, TerminalControl } from '@molr/sdk';
import { useCopy } from '../../../state';
import '@xterm/xterm/css/xterm.css';
import './terminal.css';
import { TerminalThemePicker, useTerminalTheme } from './terminalTheme';

// Per-window identity survives closing a panel but is never shared with tabs.
export const terminalClient = { id: mutationKey(), sequence: 0 };
export function TerminalSession({ assistantID, task, refresh }: { assistantID: string; task: ShellTask; refresh: () => unknown }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  const { toast } = useToast();
  const host = useRef<HTMLDivElement>(null), terminal = useRef<Terminal | null>(null), fit = useRef<FitAddon | null>(null);
  useTerminalTheme(host, false);
  const [state, setState] = useState(task.Terminal), [connected, setConnected] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState(''), [retry, setRetry] = useState(0);
  const stateRef = useRef(state); stateRef.current = state;
  const connectedRef = useRef(connected); connectedRef.current = connected;
  const busyRef = useRef(false), alive = useRef(true), epoch = useRef(0), queue = useRef(Promise.resolve()), pending = useRef(''), timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const queuedBytes = useRef(0), runningRef = useRef(task.State === 'running'); runningRef.current = task.State === 'running';
  const blocked = useRef(false), replay = useRef(true), nextInput = useRef(0);
  const path = `/api/assistants/${assistantID}/shell-tasks`;
  const running = task.State === 'running';
  const owns = state?.Control === 'user' && state.ControllerIsCurrentSession && state.ControllerClientID === terminalClient.id;
  function accept(next?: TerminalControl) {
    if (!next || (stateRef.current && next.Generation < stateRef.current.Generation)) return;
    if (next.Generation !== stateRef.current?.Generation) { epoch.current++; pending.current = ''; }
    stateRef.current = next; setState(next);
  }
  useEffect(() => { accept(task.Terminal); }, [task.Terminal]);
  function canWrite() { const s = stateRef.current; return alive.current && runningRef.current && connectedRef.current && !busyRef.current && !blocked.current && !replay.current && s?.Control === 'user' && s.ControllerIsCurrentSession && s.ControllerClientID === terminalClient.id; }
  function failed(err: unknown) {
    blocked.current = true; pending.current = ''; epoch.current++;
    if (!alive.current) return;
    setError(err instanceof APIError && (err.code === 'conflict' || err.code === 'failed_precondition')
      ? (zh ? '控制权或任务状态已变化。请刷新状态后重新接管。' : 'Control or task state changed. Refresh and take control again.')
      : (zh ? '输入结果未能确认，后续输入已停止。检查输出后点击“继续输入”，不会重发之前的输入。' : 'Input was not confirmed. Pending input stopped. Inspect output, then resume input; previous input will not be resent.'));
    void refresh();
  }
  async function post(action: ShellTaskAction, snapshot = stateRef.current) {
    return api.post<{ Terminal?: TerminalControl }>(path, { ...action, task_id: task.ID, generation: snapshot?.Generation, client_id: terminalClient.id, sequence: ++terminalClient.sequence });
  }
  function enqueue(action: ShellTaskAction) {
    if (!canWrite()) return;
    const size = new TextEncoder().encode(action.data ?? '').length;
    if (queuedBytes.current + size > 32768) { failed(new Error('input queue full')); return; }
    queuedBytes.current += size;
    const snapshot = stateRef.current, version = epoch.current;
    queue.current = queue.current.then(async () => {
      const delay = Math.max(0, nextInput.current - performance.now());
      if (delay) await new Promise(resolve => setTimeout(resolve, delay));
      if (!canWrite() || version !== epoch.current) { queuedBytes.current -= size; return; }
      nextInput.current = performance.now() + 150;
      try { await post(action, snapshot); } catch (err) { failed(err); } finally { queuedBytes.current -= size; }
    });
  }
  function flush() {
    clearTimeout(timer.current); timer.current = undefined;
    const data = pending.current; pending.current = '';
    // UTF-8 bounded chunks also preserve surrogate pairs when pasting Chinese.
    let chunk = '', size = 0;
    for (const point of data) {
      const bytes = new TextEncoder().encode(point).length;
      if (size + bytes > 4096) { enqueue({ action: 'write', data: chunk }); chunk = ''; size = 0; }
      chunk += point; size += bytes;
    }
    if (chunk) enqueue({ action: 'write', data: chunk });
  }
  function input(data: string) {
    if (!canWrite()) return;
    if (pending.current.length + data.length > 32768) { failed(new Error('input overflow')); return; }
    pending.current += data;
    if (!timer.current) timer.current = setTimeout(flush, 150);
  }
  async function command(action: 'takeover' | 'release' | 'stop') {
    const trigger = document.activeElement;
    busyRef.current = true; setBusy(true); epoch.current++; pending.current = ''; clearTimeout(timer.current); timer.current = undefined;
    try { await queue.current;
      if (action === 'takeover') { const latest = await api.get<{ Items: ShellTask[] }>(path); accept(latest.Items.find(item => item.ID === task.ID)?.Terminal); }
      const result = await post({ action }); if (alive.current) { accept(result.Terminal); blocked.current = false; setError(''); await refresh(); } }
    catch (err) { failed(err); }
    finally { busyRef.current = false; if (alive.current) { setBusy(false); if (action === 'takeover' && document.activeElement === trigger) terminal.current?.focus(); } }
  }
  useEffect(() => {
    alive.current = true;
    const term = new Terminal({ cols: task.Terminal?.Cols || 80, rows: task.Terminal?.Rows || 24, cursorBlink: true, fontSize: 13, fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace', scrollback: 5000, allowProposedApi: false, disableStdin: true, linkHandler: { activate: () => {} } });
    const fitter = new FitAddon(); term.loadAddon(fitter); term.open(host.current!); terminal.current = term; fit.current = fitter;
    term.textarea?.setAttribute('aria-label', zh ? '终端输入' : 'Terminal input');
    // Terminal output must not write to the browser clipboard, change the page
    // title, or activate external links. Only the explicit copy button copies.
    const osc = [0, 1, 2, 8, 52].map(code => term.parser.registerOscHandler(code, () => true));
    const data = term.onData(input);
    let resizeTimer: ReturnType<typeof setTimeout>;
    const resize = new ResizeObserver(() => { clearTimeout(resizeTimer); resizeTimer = setTimeout(() => {
      if (canWrite()) { fitter.fit(); enqueue({ action: 'resize', rows: Math.min(500,term.rows), cols: Math.min(500,term.cols) }); }
    }, 200); }); resize.observe(host.current!);
    const theme = () => { if (!host.current) return; const css = getComputedStyle(host.current); const color = (name: string) => css.getPropertyValue(`--color-terminal-${name}`).trim();
      term.options.theme = { background: color('background'), foreground: color('foreground'), cursor: color('foreground'), cursorAccent: color('background'), selectionBackground: color('selection'), black: color('black'), red: color('red'), green: color('green'), yellow: color('yellow'), blue: color('blue'), magenta: color('magenta'), cyan: color('cyan'), white: color('white'), brightBlack: color('bright-black'), brightRed: color('red'), brightGreen: color('green'), brightYellow: color('yellow'), brightBlue: color('blue'), brightMagenta: color('magenta'), brightCyan: color('cyan'), brightWhite: color('white') }; };
    theme(); const observer = new MutationObserver(theme); observer.observe(document.documentElement, { attributes: true });
    const scope = host.current?.closest('[data-ui-theme]'); if (scope) observer.observe(scope, { attributes: true, attributeFilter: ['data-theme', 'data-palette'] });
    return () => { alive.current = false; epoch.current++; pending.current = ''; clearTimeout(timer.current); clearTimeout(resizeTimer); observer.disconnect(); resize.disconnect(); data.dispose(); osc.forEach(item => item.dispose()); term.dispose(); terminal.current = null; };
  }, [task.ID]);
  useEffect(() => {
    const term = terminal.current; if (!term) return;
    term.options.disableStdin = !owns || !connected || busy || !!error || !running;
    if (owns && connected && !busy && !error && running) { fit.current?.fit(); enqueue({ action: 'resize', rows: Math.min(500,term.rows), cols: Math.min(500,term.cols) }); if (document.activeElement === document.body || host.current?.contains(document.activeElement)) term.focus(); }
    else if (state && !owns && (term.rows !== state.Rows || term.cols !== state.Cols)) term.resize(state.Cols || 80, state.Rows || 24);
  }, [owns, connected, busy, error, running, state?.Generation, state?.Rows, state?.Cols]);
  useEffect(() => {
    const abort = new AbortController(); let cursor = 0, disposed = false;
    replay.current = true; connectedRef.current = false; setConnected(false); terminal.current?.reset();
    async function stream() {
      let failures = 0;
      while (!abort.signal.aborted) {
        try {
          const response = await fetch(`/api/assistants/${assistantID}/terminals/${task.ID}/stream?cursor=${cursor}`, { signal: abort.signal, credentials: 'same-origin', cache: 'no-store' });
          if (!response.ok || !response.body) {
            if ([401,403,404,429].includes(response.status)) { setError(zh ? '连接不可用或请求受限。请稍后重新连接。' : 'Connection unavailable or rate limited. Reconnect later.'); return; }
            throw new Error('stream unavailable');
          }
          const reader = response.body.getReader(), decoder = new TextDecoder(); let buffer = '';
          try {
            while (!abort.signal.aborted) {
              const { value, done } = await reader.read(); if (done) break;
              buffer += decoder.decode(value, { stream: true });
              let boundary: number;
              while ((boundary = buffer.indexOf('\n\n')) >= 0) {
                const event = buffer.slice(0, boundary); buffer = buffer.slice(boundary + 2);
                if (!event.startsWith('data: ')) continue;
                const frame: ShellTaskOutput = JSON.parse(event.slice(6));
                if (!Number.isSafeInteger(frame.NextCursor) || frame.NextCursor < cursor) throw new Error('invalid cursor');
                accept(frame.Terminal);
                const bytes = Uint8Array.from(atob(frame.Output), c => c.charCodeAt(0));
                const term = terminal.current; if (!term || disposed) return;
                if (bytes.length) await new Promise<void>(resolve => term.write(bytes, resolve));
                cursor = frame.NextCursor; failures = 0;
                if (bytes.length < 16384) { replay.current = false; connectedRef.current = true; setConnected(true); }
                if (frame.Finished && !bytes.length) { connectedRef.current = false; setConnected(false); void refresh(); return; }
              }
            }
          } finally { await reader.cancel().catch(() => {}); reader.releaseLock(); }
        } catch { if (abort.signal.aborted) return; }
        connectedRef.current = false; if (disposed) return; setConnected(false); epoch.current++; pending.current = ''; replay.current = true;
        await new Promise<void>(resolve => { const timeout = setTimeout(resolve, Math.min(30000, 2000 * 2 ** Math.min(failures++,4))); abort.signal.addEventListener('abort', () => { clearTimeout(timeout); resolve(); }, { once:true }); });
      }
    }
    void stream();
    return () => { disposed = true; abort.abort(); };
  }, [task.ID, retry]);
  const controlLabel = running ? owns ? (zh ? '由你控制' : 'You control') : state?.Control === 'user' ? (zh ? '其他窗口控制' : 'Another window controls') : (zh ? '助理控制' : 'Assistant controls') : (zh ? '会话已结束' : 'Session ended');
  const connectionLabel = running ? connected ? (zh ? '已连接' : 'Connected') : (zh ? '连接中…' : 'Connecting…') : (zh ? '历史输出' : 'Saved output');
  function copySelection() {
    const text = terminal.current?.getSelection();
    if (text) void copyTextToClipboard(text).then(() => toast({ title: zh ? '已复制' : 'Copied' })).catch(() => toast({ title: zh ? '复制失败，请使用键盘复制' : 'Copy failed. Use the keyboard to copy.', tone: 'warning' }));
  }
  return <section className="terminal-session" aria-label={zh ? '交互式终端' : 'Interactive terminal'}>
    <div className="terminal-controlbar">
      <div className="terminal-control-state"><Badge tone={running && owns ? 'positive' : 'neutral'}>{controlLabel}</Badge><Badge tone={task.FullAccess ? 'warning' : 'neutral'}>{task.FullAccess ? 'Full Access' : zh ? '沙盒' : 'Sandbox'}</Badge></div>
      {running && <Button size="sm" variant={owns ? 'secondary' : 'primary'} disabled={busy} onClick={() => void command(owns ? 'release' : 'takeover')}>{owns ? (zh ? '交还助理' : 'Return to assistant') : (zh ? '接管' : 'Take control')}</Button>}
    </div>
    {error && <Alert tone="warning" title={zh ? '终端提示' : 'Terminal notice'}>{error}{owns && <Button size="sm" onClick={() => { blocked.current = false; setError(''); terminal.current?.focus(); }}>{zh ? '继续输入' : 'Resume input'}</Button>}</Alert>}
    <div ref={host} className="terminal-screen" />
    <div className="terminal-statusbar">
      <span className="terminal-connection" role="status"><span className="terminal-connection-dot" data-connected={connected && running || undefined}/>{connectionLabel}</span>
      {state && <span className="terminal-dimensions" aria-label={zh ? '终端尺寸' : 'Terminal dimensions'}>{state.Cols} × {state.Rows}</span>}
      <div className="terminal-session-actions">
        <TerminalThemePicker zh={zh}/>
        {!connected && running && <Tooltip content={zh ? '重新连接' : 'Reconnect'}><IconButton label={zh ? '重新连接' : 'Reconnect'} onClick={() => { setError(''); setRetry(x => x + 1); }}><RefreshCw size={15}/></IconButton></Tooltip>}
        {running && <Button className="terminal-interrupt" size="sm" variant="ghost" disabled={!owns || !connected || busy} onClick={() => { flush(); enqueue({ action: 'interrupt' }); }}>Ctrl-C</Button>}
        <Tooltip content={zh ? '复制选中' : 'Copy selection'}><IconButton label={zh ? '复制选中' : 'Copy selection'} onClick={copySelection}><Copy size={15}/></IconButton></Tooltip>
        {running && <Tooltip content={zh ? '结束会话' : 'End session'}><IconButton className="terminal-end-session" label={zh ? '结束会话' : 'End session'} disabled={!owns || busy} onClick={() => void command('stop')}><Power size={15}/></IconButton></Tooltip>}
      </div>
    </div>
  </section>;
}
