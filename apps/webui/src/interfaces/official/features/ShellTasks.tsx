import { TerminalSession } from './TerminalSession';
import { GPUDeviceList } from './GPUAccess';
import type { SandboxView } from '../../../sandbox-types';
import { DismissibleAlert } from '../components/DismissibleAlert';
import { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Badge, Button, Drawer, IconButton, SegmentedControl, useBreakpoint } from '@molr/ui';
import { ArrowLeft, ChevronRight, Clock3, ListTodo, Terminal, ExternalLink, Copy } from 'lucide-react';
import { api } from '../../../api/client';
import type { ShellTask, ShellTaskAction, ShellTaskOutput } from '../../../api/types';
import { poll } from '../../../polling';
import { useCopy, useQuery } from '../../../state';
import { Loading } from '../components/common';
import './shell-tasks.css';

// Output is untrusted plain text. A line terminal deliberately does not execute
// OSC hyperlinks, clipboard sequences, HTML or terminal escape commands.
const plain = (text: string) => text.replace(/\x1b\][^\x07]*(?:\x07|\x1b\\)/g, '').replace(/\x1b\[[0-?]*[ -/]*[@-~]/g, '').replace(/\r\n/g, '\n');
function useShellTaskList(assistantID: string | null, interval: number) {
  const path = assistantID ? `/api/assistants/${assistantID}/shell-tasks` : null;
  const cadence = useCallback((data?: { Items: ShellTask[] }) => data?.Items.some(task => task.State === 'running') ? interval : 30_000, [interval]);
  const query = useQuery<{ Items: ShellTask[] }>(path, false, cadence);
  return { items: query.data?.Items ?? [], error: !!query.error, loading: query.loading && !query.data, refresh: query.reload };
}

export function ShellTasksButton({ assistantID, disabled, onOpen }: { assistantID: string; disabled?: boolean; onOpen: () => void }) {
  const { locale } = useCopy(), { items } = useShellTaskList(disabled ? null : assistantID, 5000);
  const running = items.filter(task => task.State === 'running').length;
  return <IconButton className="chat-count-trigger" disabled={disabled} onClick={onOpen} label={`${locale === 'zh' ? '运行任务' : 'Running tasks'}${running > 0 ? ` (${running})` : ''}`}><ListTodo size={20} />{running > 0 && <Badge tone="positive" size="sm" dot={false} aria-hidden="true">{running}</Badge>}</IconButton>;
}

function authenticatedForwardURL(raw: string) {
 try {const url=new URL(raw);url.searchParams.set('__molr_login_port',window.location.port||(window.location.protocol==='https:'?'443':'80'));return url.href;}catch{return raw;}
}
function ForwardAddress({ address, zh }: { address: string; zh: boolean }) {
  const input = useRef<HTMLInputElement>(null), [copied, setCopied] = useState(false);
  async function copy() {
    input.current?.select();
    try {
      if (navigator.clipboard?.writeText) await navigator.clipboard.writeText(address);
      else if (!document.execCommand('copy')) return;
      setCopied(true);
    } catch { input.current?.select(); }
  }
  return <div className="shell-task-forward-address"><input ref={input} readOnly value={address} aria-label={zh ? '转发地址' : 'Forward address'} onFocus={event => event.target.select()} /><Button variant="ghost" size="sm" onClick={() => void copy()}><Copy size={14} />{copied ? (zh ? '已复制' : 'Copied') : (zh ? '复制地址' : 'Copy address')}</Button></div>;
}

export function ShellTasks({ assistantID, docked, onClose }: { assistantID: string; docked: boolean; onClose: () => void }) {
  const { locale } = useCopy(), zh = locale === 'zh', mobile = useBreakpoint('mobile');
  const { items, error: listError, loading, refresh } = useShellTaskList(assistantID, 2000);
  const sandboxes = useQuery<SandboxView[]>('/api/sandboxes');
  const [selected, setSelected] = useState('');
  const [error, setError] = useState(false), [busy, setBusy] = useState(false), [output, setOutput] = useState('');
  const [stream, setStream] = useState<'stdout' | 'stderr'>('stdout');
  const cursor = useRef(0), contentRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const frame = requestAnimationFrame(() => {
      const content = contentRef.current, body = content?.closest('.ui-dialog__body');
      if (body) body.scrollTop = 0;
      content?.querySelector<HTMLElement>('[data-task-heading]')?.focus({ preventScroll: true });
    });
    return () => cancelAnimationFrame(frame);
  }, [selected]);
  const path = `/api/assistants/${assistantID}/shell-tasks`;
  useEffect(() => { setSelected(''); }, [assistantID]);
  const selectedTask = items.find(item => item.ID === selected);
  const taskState = useRef(selectedTask); taskState.current = selectedTask;
  useEffect(() => {
    cursor.current = 0; setOutput(''); setError(false);
    if (!selected || taskState.current?.Mode === 'tty') return;
    const loop = poll(async signal => {
      try {
        const data = await api.read<ShellTaskOutput>(path, { action: 'read', task_id: selected, cursor: cursor.current, stream }, signal);
        if (signal.aborted) return false;
        setError(false); cursor.current = data.NextCursor;
        setOutput(previous => (previous + plain(data.Output)).slice(-65536));
        const finished = taskState.current && taskState.current.State !== 'running';
        if (finished && data.Finished === true && !data.Output) { loop.stop(); return false; }
        return finished && !data.Finished && !data.Output ? 30_000 : 1500;
      } catch (error) { if (!signal.aborted) setError(true); throw error; }
    });
    return () => loop.stop();
  }, [path, selected, stream]);
  async function act(request: ShellTaskAction) {
    setBusy(true); setError(false);
    try {
      await api.post(path, request);
      await refresh();
    }
    catch { setError(true); }
    finally { setBusy(false); }
  }
  const running = items.filter(task => task.State === 'running').length, task = items.find(item => item.ID === selected);
  const states = zh ? { running: '运行中', completed: '已完成', failed: '失败', stopped: '已停止', interrupted: '已中断' } : { running: 'Running', completed: 'Completed', failed: 'Failed', stopped: 'Stopped', interrupted: 'Interrupted' };
  const modes = zh ? { foreground: '前台', background: '后台', tty: 'TTY 会话' } : { foreground: 'Foreground', background: 'Background', tty: 'TTY session' };
  const elapsed = (item: ShellTask) => { const seconds = Math.max(0, Math.floor(((item.FinishedAt ? Date.parse(item.FinishedAt) : Date.now()) - Date.parse(item.StartedAt)) / 1000)); return `${Math.floor(seconds / 60)}m ${seconds % 60}s`; };
  const active = items.filter(item => item.State === 'running'), history = items.filter(item => item.State !== 'running');
  const title = (item: ShellTask) => item.Kind === 'browser' ? (zh ? '浏览器' : 'Browser') : item.Mode === 'tty' && (!item.Script || item.Script === 'exec /bin/sh -i') ? (zh ? '交互式 sh' : 'Interactive sh') : item.Script.trim().split('\n').find(line => line.trim()) || 'sh';
  const stateLabel = (item: ShellTask) => item.Mode === 'tty' && item.State === 'running' ? (zh ? '会话打开' : 'Session open') : states[item.State];
  const tone = (item: ShellTask) => item.State === 'running' ? 'positive' : item.State === 'failed' ? 'danger' : item.State === 'interrupted' ? 'warning' : 'neutral';
  const choose = (item: ShellTask) => { setStream('stdout'); setSelected(item.ID); };
  const renderTask = (item: ShellTask) => <button type="button" key={item.ID} className="shell-task-item" onClick={() => choose(item)}>
    <span className="shell-task-item-main"><span className="shell-task-item-title">{title(item)}</span><ChevronRight size={16} aria-hidden="true" /></span>
    <span className="shell-task-item-meta"><Badge tone={tone(item)} size="sm" dot={false}>{stateLabel(item)}</Badge><span>{modes[item.Mode]}</span>{item.FullAccess && <Badge size="sm" dot={false}>Full Access</Badge>}<span className="shell-task-duration"><Clock3 size={13} aria-hidden="true" />{elapsed(item)}</span></span>
  </button>;
  return <Drawer open docked={docked} side={mobile ? 'bottom' : 'right'} onOpenChange={open => { if (!open) onClose(); }} title={zh ? '运行任务' : 'Running tasks'} description={zh ? '查看执行进度，管理后台任务与终端' : 'Monitor commands, background tasks and terminals'} size="lg" className="shell-tasks-drawer">
      <div ref={contentRef} className="shell-tasks-content">
        {(error || listError) && <DismissibleAlert noticeKey={JSON.stringify([assistantID, selected, stream, !!error, !!listError])} tone="warning" title={zh ? '任务提示' : 'Task notice'}>{zh ? '暂时无法读取或操作任务。任务可能正在启动、已退出或沙盒已断开。' : 'Unable to read or control the task. It may be starting, finished, or disconnected.'}</DismissibleAlert>}
        {loading ? <Loading /> : !task ? <>
          <section className="shell-task-section" aria-label={zh ? '运行中' : 'Active tasks'}>
            <div className="shell-task-section-heading"><div><h3 data-task-heading tabIndex={-1}>{zh ? '运行中' : 'Active'}<span className="shell-task-capacity">{running}</span></h3><p>{zh ? '前台、后台与终端任务' : 'Foreground, background and terminal tasks'}</p></div><Button variant="ghost" size="sm" disabled={busy || running === 0} onClick={() => void act({ action: 'stop_all' })}>{zh ? '停止全部' : 'Stop all'}</Button></div>
            <div className="shell-task-list">
              {active.length ? active.map(renderTask) : <div className="shell-task-empty"><ListTodo size={28} aria-hidden="true" /><p>{zh ? '当前没有运行中的任务' : 'No tasks are running'}</p><span>{zh ? '助理启动命令或终端后会显示在这里' : 'Commands and terminals appear here when started'}</span></div>}
            </div>
          </section>
          <details className="shell-task-history">
            <summary>{zh ? '最近结束' : 'Recently finished'}<span>{history.length}</span></summary>
            <div className="shell-task-list">{history.length ? history.map(renderTask) : <p className="shell-task-note">{zh ? '暂无历史记录' : 'No recent tasks'}</p>}</div>
          </details>
          <p className="shell-task-note">{zh ? '后台任务结束会通知助理；仍在运行的任务每五分钟汇总提醒。' : 'Background completion notifies the assistant. Active tasks are summarized every five minutes.'}</p>
        </> : <section className="shell-task-detail" aria-label={zh ? '任务详情' : 'Task details'}>
          <Button variant="ghost" size="sm" className="shell-task-back" onClick={() => setSelected('')}><ArrowLeft size={15} />{zh ? '返回任务列表' : 'Back to tasks'}</Button>
          <header className="shell-task-detail-heading"><h3 data-task-heading tabIndex={-1} className="shell-task-detail-title">{title(task)}</h3><div className="shell-task-item-meta"><Badge tone={tone(task)} size="sm" dot={false}>{stateLabel(task)}</Badge><span>{modes[task.Mode]}</span><span className="shell-task-duration"><Clock3 size={13} aria-hidden="true" />{elapsed(task)}</span></div></header>
          {!!task.Forwards?.length && <section className="shell-task-forwards" aria-label={zh ? '转发端口' : 'Forwarded ports'}>
            <h4>{zh ? '转发端口' : 'Forwarded ports'}</h4>
            {task.Forwards.map(forward => <div className="shell-task-forward" key={forward.Port}>
              <div className="shell-task-item-meta"><strong>TCP {forward.Port}</strong><Badge size="sm" dot={false} tone={forward.State === 'ready' && task.State === 'running' ? 'positive' : 'neutral'}>{task.State !== 'running' || forward.State === 'closed' ? (zh ? '已关闭' : 'Closed') : forward.State === 'ready' ? (zh ? '可连接' : 'Connectable') : (zh ? '等待服务' : 'Waiting for service')}</Badge></div>
              <ForwardAddress address={authenticatedForwardURL(forward.URL)} zh={zh} />
              {task.State === 'running' && forward.State !== 'closed' && /^https?:\/\//.test(forward.URL) && <a href={authenticatedForwardURL(forward.URL)} target="_blank" rel="noopener noreferrer" className="shell-task-forward-link"><ExternalLink size={14} aria-hidden="true" />{zh ? '打开网页（需要登录）' : 'Open page (sign-in required)'}</a>}
            </div>)}
            <p className="shell-task-note">{zh ? '任务结束后自动关闭。非 HTTP 服务请使用上面的 TCP 地址。' : 'Forwarding closes with the task. For other protocols, use the TCP address above.'}</p>
          </section>}
          {task.Result?.OutputComplete === false && <Alert tone="warning" title={zh ? '输出不完整' : 'Output incomplete'}>{zh ? '输出归档已达到限额或磁盘写入失败。' : 'The output quota was reached or a disk write failed.'}</Alert>}
          {task.Mode === 'tty' ? <TerminalSession key={task.ID} assistantID={assistantID} task={task} refresh={refresh}/> : <section className="shell-task-console" aria-label={zh ? '输出' : 'Output'}>
            <div className="shell-task-console-heading"><h4><Terminal size={15} aria-hidden="true" />{zh ? '输出' : 'Output'}</h4><SegmentedControl hideLabel label={zh ? '输出流' : 'Output stream'} value={stream} onValueChange={setStream} options={[{ value: 'stdout', label: 'stdout' }, { value: 'stderr', label: 'stderr' }]} /></div>
            <pre className="shell-task-output" tabIndex={0} aria-label={zh ? '任务输出' : 'Task output'}>{output || (task.State === 'running' ? (zh ? '等待输出…' : 'Waiting for output…') : (zh ? '此输出流没有内容' : 'No output in this stream'))}</pre>
          </section>}
          {task.State === 'running' && task.Mode !== 'tty' && <Button variant="ghost" size="sm" disabled={busy} onClick={() => void act({ action: 'stop', task_id: task.ID })}>{zh ? '停止任务' : 'Stop task'}</Button>}
          <details className="shell-task-info">
            <summary>{zh ? '执行信息' : 'Execution details'}</summary>
            <dl>{!!task.GPUDeviceIDs?.length && <div><dt>GPU</dt><dd><GPUDeviceList ids={task.GPUDeviceIDs} devices={sandboxes.data?.find(node=>node.ID===(task.SandboxID||'local'))?.Info.GPUs??[]} /></dd></div>}<div><dt>{zh ? '开始时间' : 'Started'}</dt><dd>{new Date(task.StartedAt).toLocaleString(locale)}</dd></div>{task.FinishedAt && <div><dt>{zh ? '结束时间' : 'Finished'}</dt><dd>{new Date(task.FinishedAt).toLocaleString(locale)}</dd></div>}{task.Result?.Exit && <div><dt>{zh ? '退出码' : 'Exit code'}</dt><dd>{task.Result.Exit.Code}</dd></div>}{task.Failure && <div><dt>{zh ? '结束原因' : 'Exit reason'}</dt><dd>{task.Failure}</dd></div>}<div><dt>{zh ? '任务 ID' : 'Task ID'}</dt><dd><code>{task.ID}</code></dd></div><div><dt>{zh ? '完整命令' : 'Full command'}</dt><dd><pre>{task.Script || 'sh'}</pre></dd></div><div><dt>{task.Mode === 'tty' ? 'TTY' : 'stdout'}</dt><dd><code>{task.StdoutPath || '—'}</code></dd></div>{task.Mode !== 'tty' && <div><dt>stderr</dt><dd><code>{task.StderrPath || '—'}</code></dd></div>}</dl>
          </details>
          <p className="shell-task-note">{zh ? '仅 /work 持久保存。窗口显示最近 64 KiB，完整输出见归档文件。' : 'Only /work persists. This view keeps the latest 64 KiB; archived files contain full output.'}</p>
        </section>}
      </div>
    </Drawer>;
}
