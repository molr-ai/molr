import { useEffect, useRef, useState } from 'react';
import { Alert, Badge, Button, Drawer, IconButton, useBreakpoint } from '@molr/ui';
import { ArrowLeft, ArrowRight, Globe, Hand, RefreshCw, X } from 'lucide-react';
import { api, APIError, mutationKey } from '../../../api/client';
import type { BrowserStatus, Grant, Resource } from '../../../api/types';
import { useCopy, useQuery } from '../../../state';
import './browser.css';
type Action = {
    action: string;
    [key: string]: unknown;
};
type Tab = {
    id: string;
    title: string;
    url: string;
    active?: boolean;
};
export function BrowserButton({ disabled, onOpen }: {
    disabled?: boolean;
    onOpen: () => void;
}) {
    const { locale } = useCopy();
    return <IconButton disabled={disabled} onClick={onOpen} label={locale === 'zh' ? '浏览器' : 'Browser'}><Globe size={20}/></IconButton>;
}
export function Browser({ assistantID, docked, onClose }: {
    assistantID: string;
    docked: boolean;
    onClose: () => void;
}) {
    const { locale } = useCopy(), zh = locale === 'zh', mobile = useBreakpoint('mobile');
    const path = `/api/assistants/${assistantID}/browser`;
    const query = useQuery<BrowserStatus>(path, false, 2000);
    const resources = useQuery<Resource[]>('/api/resources?kind=compute', true);
    const grants = useQuery<Grant[]>(`/api/grants?assistant_id=${assistantID}`, true);
    const [compute, setCompute] = useState(''), [busy, setBusy] = useState(false), [error, setError] = useState('');
    const [address, setAddress] = useState(''), [tabs, setTabs] = useState<Tab[]>([]), [connected, setConnected] = useState(false), [hasFrame, setHasFrame] = useState(false);
    const canvas = useRef<HTMLCanvasElement>(null), textInput = useRef<HTMLTextAreaElement>(null);
    const client = useRef(mutationKey()), sequence = useRef(0), control = useRef(query.data);
    control.current = query.data;
    const task = query.data?.Task, state = task?.Browser;
    const [controlled, setControlled] = useState(false);
    const seenURL = useRef('');
    const localInputEpoch = useRef(0), pendingInputs = useRef(0), lastMove = useRef(0);
    const generation = useRef(0), queue = useRef(Promise.resolve()), closed = useRef(false), painting = useRef(false), latest = useRef<string | null>(null);
    const usable = (resources.data ?? []).filter(r => (r.SandboxID ?? 'local') === 'local' && (grants.data ?? []).some(g => g.ResourceID === r.ID && !g.Scope.FullAccess && !g.RevokedAt && (!g.ExpiresAt || Date.parse(g.ExpiresAt) > Date.now()) && g.Actions.includes('execute')));
    useEffect(() => { if (!compute && usable[0])
        setCompute(usable[0].ID); }, [compute, usable]);
    useEffect(() => { closed.current = false; return () => { closed.current = true; }; }, []);
    useEffect(() => { if (state?.Generation !== generation.current || state?.Control !== 'user') {
        setControlled(false);
        localInputEpoch.current++;
    } }, [state?.Generation, state?.Control]);
    function notice(err: unknown) { return err instanceof APIError && err.reason === 'browser_user_controlled' ? (zh ? '用户正在接管浏览器，请交还控制后继续。' : 'The user controls this browser. Return control to continue.') : err instanceof APIError && err.code === 'conflict' ? (zh ? '控制权已变更，请刷新后重新接管。' : 'Control changed. Refresh and take control again.') : (zh ? '浏览器暂时无法操作，请检查任务状态或授权。' : 'Browser operation unavailable. Check task status and permissions.'); }
    async function act(action: Action) {
        const status = control.current;
        return api.post<Record<string, unknown>>(path, { ...action, task_id: status?.Task?.ID, client_id: client.current, generation: status?.Task?.Browser?.Generation ?? 0, sequence: ++sequence.current });
    }
    async function command(action: Action) { setBusy(true); setError(''); try {
        await act(action);
        await query.reload();
    }
    catch (err) {
        setError(notice(err));
    }
    finally {
        if (!closed.current)
            setBusy(false);
    } }
    async function handoff(action: 'takeover' | 'release') {
        setBusy(true);
        setError('');
        setControlled(false);
        localInputEpoch.current++;
        try {
            await queue.current;
            const next = await act({ action }) as unknown as BrowserStatus;
            generation.current = next.Task?.Browser?.Generation ?? 0;
            control.current = next;
            await query.reload();
            setControlled(action === 'takeover');
        }
        catch (err) {
            setError(notice(err));
        }
        finally {
            setBusy(false);
        }
    }
    function input(action: Action) {
        if (!controlled || !connected || busy)
            return;
        if (action.action === 'pointer' && action.event === 'move') {
            const now = performance.now();
            if (now - lastMove.current < 80)
                return;
            lastMove.current = now;
        }
        if (pendingInputs.current >= 64 && action.event !== 'up') {
            setError(zh ? '输入较快，请稍后继续。' : 'Input is busy; please wait.');
            return;
        }
        pendingInputs.current++;
        const inputEpoch = localInputEpoch.current;
        const current = generation.current;
        queue.current = queue.current.then(async () => {
            pendingInputs.current--;
            if (inputEpoch !== localInputEpoch.current || closed.current || control.current?.Task?.Browser?.Generation !== current || control.current?.Task?.Browser?.Control !== 'user')
                return;
            try {
                await act(action);
            }
            catch (err) {
                setError(notice(err));
                setControlled(false);
                localInputEpoch.current++;
            }
        });
    }
    useEffect(() => {
        setConnected(false);
        setHasFrame(false);
        setTabs([]);
        latest.current = null;
        seenURL.current = '';
        if (!task?.ID)
            return;
        let alive = true;
        let source: EventSource;
        let retry: ReturnType<typeof setTimeout> | undefined;
        function connect() { source = new EventSource(`${path}/stream?task_id=${encodeURIComponent(task!.ID)}`); install(); }
        function install() {
            function draw() {
                if (!alive || painting.current || !latest.current)
                    return;
                const data = latest.current;
                latest.current = null;
                painting.current = true;
                const image = new Image();
                image.onload = () => { if (alive && canvas.current) {
                    const c = canvas.current;
                    c.width = image.naturalWidth;
                    c.height = image.naturalHeight;
                    c.getContext('2d')?.drawImage(image, 0, 0);
                    setHasFrame(true);
                } painting.current = false; if (alive && latest.current)
                    requestAnimationFrame(draw); };
                image.onerror = () => { painting.current = false; };
                image.src = `data:image/jpeg;base64,${data}`;
            }
            source.onopen = () => { if (alive)
                setConnected(true); };
            source.onerror = () => { source.close(); localInputEpoch.current++; if (alive) {
                setConnected(false);
                setControlled(false);
                retry = setTimeout(connect, 2000);
            } };
            source.onmessage = event => {
                if (!alive)
                    return;
                try {
                    const message = JSON.parse(event.data);
                    if (message.type === 'frame' && typeof message.data === 'string') {
                        latest.current = message.data;
                        draw();
                    }
                    else if (message.type === 'url' && typeof message.url === 'string')
                        setAddress(message.url);
                    else if (message.type === 'tabs' && Array.isArray(message.tabs)) {
                        const next = message.tabs.map((t: Record<string, unknown>, index: number) => ({ id: String(t.tabId ?? `t${index + 1}`), title: String(t.title ?? ''), url: String(t.url ?? ''), active: !!t.active }));
                        setTabs(next);
                        const current = next.find((t: Tab) => t.active)?.url;
                        if (current && current !== seenURL.current) {
                            seenURL.current = current;
                            setAddress(current);
                        }
                    }
                }
                catch { /* Malformed upstream data is never rendered as HTML. */ }
            };
        }
        connect();
        return () => { alive = false; source?.close(); clearTimeout(retry); latest.current = null; };
    }, [path, task?.ID]);
    function pointer(event: React.PointerEvent<HTMLCanvasElement>, kind: 'move' | 'down' | 'up') {
        if (!controlled || !connected)
            return;
        if (kind === 'down') {
            event.currentTarget.setPointerCapture(event.pointerId);
            textInput.current?.focus({ preventScroll: true });
        }
        const rect = event.currentTarget.getBoundingClientRect();
        const x = Math.max(0, Math.min(1280, (event.clientX - rect.left) * 1280 / rect.width)), y = Math.max(0, Math.min(800, (event.clientY - rect.top) * 800 / rect.height));
        input({ action: 'pointer', event: kind, x, y, button: ['left', 'middle', 'right'][event.button] ?? 'left' });
    }
    const canOperate = controlled && state?.Control === 'user' && connected && !busy;
    return <Drawer open docked={docked} side={mobile ? 'bottom' : 'right'} onOpenChange={open => { if (!open)
        onClose(); }} title={zh ? '浏览器' : 'Browser'} description={zh ? '观看助理操作，或接管同一个浏览器' : 'Watch the assistant or take control of the same browser'} size="lg" className="browser-drawer">
  <div className="browser-panel">
   {(!!error || !!query.error) && <Alert tone="warning" title={zh ? '浏览器提示' : 'Browser notice'}>{error || (zh ? '暂时无法获取浏览器状态' : 'Unable to load browser status')}</Alert>}
   {!task ? <div className="browser-start"><Globe size={32}/><p>{zh ? '开启浏览器后，可在这里实时观看和接管。关闭浏览器会保留登录状态。' : 'Start a browser to watch and take control. Login state is retained when it closes.'}</p>
    {query.data?.Available === false ? <Alert title={zh ? '浏览器程序尚未配置' : 'Browser programs are not configured'}>{zh ? '请联系管理员配置浏览器程序。' : 'Ask an administrator to configure browser programs.'}</Alert> : <><label>{zh ? '计算资源' : 'Compute resource'}<select value={compute} onChange={e => setCompute(e.target.value)}><option value="">{zh ? '选择已授权的本机计算资源' : 'Select an authorized local compute resource'}</option>{usable.map(r => <option key={r.ID} value={r.ID}>{r.Name}</option>)}</select></label><Button disabled={busy || !compute} onClick={() => void command({ action: 'start', compute_id: compute })}>{zh ? '启动浏览器' : 'Start browser'}</Button></>}
   </div> : <>
    <div className="browser-control"><Badge tone={state?.Control === 'user' ? 'warning' : 'neutral'} dot={false}>{state?.Control === 'switching' ? (zh ? '正在交接' : 'Handing over') : state?.Control === 'user' ? (zh ? '用户接管中' : 'User controls browser') : (zh ? '助理控制中' : 'Assistant controls browser')}</Badge><span role="status">{connected ? (zh ? '已连接' : 'Connected') : (zh ? '连接中…' : 'Connecting…')}</span><Button size="sm" disabled={busy || !connected || state?.Control === 'switching'} onClick={() => void handoff(canOperate ? 'release' : 'takeover')}><Hand size={14}/>{canOperate ? (zh ? '交还助理' : 'Return control') : (zh ? '接管' : 'Take control')}</Button><IconButton disabled={busy} label={zh ? '关闭浏览器' : 'Close browser'} onClick={() => void command({ action: 'close' })}><X size={16}/></IconButton></div>
    <form className="browser-navigation" onSubmit={e => { e.preventDefault(); input({ action: 'navigate', url: /^https?:\/\//i.test(address) || address === 'about:blank' ? address : `https://${address}` }); }}>
     <IconButton type="button" disabled={!canOperate} label={zh ? '后退' : 'Back'} onClick={() => input({ action: 'back' })}><ArrowLeft size={16}/></IconButton><IconButton type="button" disabled={!canOperate} label={zh ? '前进' : 'Forward'} onClick={() => input({ action: 'forward' })}><ArrowRight size={16}/></IconButton><IconButton type="button" disabled={!canOperate} label={zh ? '刷新' : 'Reload'} onClick={() => input({ action: 'reload' })}><RefreshCw size={16}/></IconButton><input aria-label={zh ? '网址' : 'Address'} disabled={!canOperate} value={address} onChange={e => setAddress(e.target.value)} placeholder="https://"/><Button type="submit" size="sm" disabled={!canOperate}>{zh ? '打开' : 'Go'}</Button>
    </form>
    <div className="browser-tabs">{tabs.map(tab => <Button key={tab.id} size="sm" variant={tab.active ? 'secondary' : 'ghost'} disabled={!canOperate} onClick={() => input({ action: 'select_tab', tab: tab.id })}>{tab.title || tab.url || tab.id}</Button>)}<Button size="sm" variant="ghost" disabled={!canOperate} onClick={() => input({ action: 'new_tab', url: 'about:blank' })}>{zh ? '新标签页' : 'New tab'}</Button><Button size="sm" variant="ghost" disabled={!canOperate} onClick={() => input({ action: 'close_tab' })}>{zh ? '关闭标签页' : 'Close tab'}</Button></div>
    <div className="browser-viewport" data-interactive={canOperate || undefined}>
     <canvas ref={canvas} width={1280} height={800} aria-label={zh ? '浏览器实时画面' : 'Live browser view'} onPointerDown={e => pointer(e, 'down')} onPointerUp={e => pointer(e, 'up')} onPointerMove={e => pointer(e, 'move')} onPointerCancel={e => pointer(e, 'up')} onContextMenu={e => e.preventDefault()} onWheel={e => { if (canOperate) {
            input({ action: 'scroll', direction: e.deltaY < 0 ? 'up' : 'down' });
        } }}/>
     {(!connected || !hasFrame) && <div className="browser-overlay">{zh ? '正在连接浏览器画面…' : 'Connecting to browser…'}</div>}
    </div>
    <textarea ref={textInput} className="browser-keyboard" disabled={!canOperate} aria-label={zh ? '浏览器键盘输入' : 'Browser keyboard input'} placeholder={canOperate ? (zh ? '点击网页输入框后在此输入，支持中文和粘贴' : 'Click a field in the page, then type here; supports IME and paste') : (zh ? '接管后可以输入' : 'Take control to type')} onChange={e => { if (!(e.nativeEvent as InputEvent).isComposing && e.target.value) {
            input({ action: 'insert_text', text: e.target.value });
            e.target.value = '';
        } }} onCompositionEnd={e => { if (e.currentTarget.value) {
            input({ action: 'insert_text', text: e.currentTarget.value });
            e.currentTarget.value = '';
        } }} onKeyDown={e => { if (e.nativeEvent.isComposing)
            return; if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'v')
            return; if (e.key.length > 1 || e.ctrlKey || e.metaKey || e.altKey) {
            if (e.key === 'Process' || e.key === 'Unidentified' || ['Control', 'Meta', 'Alt', 'Shift'].includes(e.key))
                return;
            e.preventDefault();
            input({ action: 'press', key: [e.ctrlKey ? 'Control' : '', e.metaKey ? 'Meta' : '', e.altKey ? 'Alt' : '', e.shiftKey ? 'Shift' : '', e.key].filter(Boolean).join('+') });
        } }}/>
    <p className="browser-note">{zh ? '用户接管期间，助理仍可读取页面快照，但不能操作。关闭面板不会关闭浏览器。' : 'During takeover the assistant can still read snapshots, but cannot operate. Closing this panel keeps the browser running.'}</p>
   </>}
  </div>
 </Drawer>;
}
