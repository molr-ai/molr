import { useEffect, useRef, useState } from 'react';
import { List } from 'lucide-react';
import { api } from '@molr/sdk';
import type { HostProgram, HostPrograms as Snapshot } from '@molr/sdk';
import { Badge, Button, Dialog, IconButton, ModalPresence, TextField, Tooltip } from '@molr/ui';
import { useCopy } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import './host-programs.css';

export function HostPrograms({ sandboxID, onSelect }: { sandboxID: string; onSelect?: (program: HostProgram) => void }) {
  const { locale, t } = useCopy(), zh = locale === 'zh';
  const [input, setInput] = useState(''), [command, setCommand] = useState(''), [revision, setRevision] = useState(0);
  const [composing, setComposing] = useState(false);
  const [result, setResult] = useState<{ key: string; data: Snapshot }>();
  const [error, setError] = useState<unknown>(), [loading, setLoading] = useState(true);
  const generation = useRef(0), key = `${sandboxID}/${command}`;
  const term = input.trim(), searchValid = !term || /^[A-Za-z0-9][A-Za-z0-9_.+-]{0,79}$/.test(term);
  const pending = composing || !searchValid || term !== command;
  useEffect(() => {
    if (composing || !searchValid || term === command) return;
    if (!term) { setCommand(''); return; }
    const timer = setTimeout(() => setCommand(term), 250);
    return () => clearTimeout(timer);
  }, [term, command, searchValid, composing]);
  const reload = () => setRevision(value => value + 1);
  useEffect(() => {
    if (pending) { setLoading(false); setError(undefined); return; }
    const controller = new AbortController(), request = ++generation.current;
    setLoading(true); setError(undefined);
    const params = new URLSearchParams({ command, refresh: String(revision > 0) });
    void api.get<Snapshot>(`/api/sandboxes/${encodeURIComponent(sandboxID)}/programs?${params}`, controller.signal).then(data => {
      if (request === generation.current) setResult({ key, data });
    }).catch(cause => { if (!controller.signal.aborted && request === generation.current) setError(cause); })
      .finally(() => { if (request === generation.current) setLoading(false); });
    return () => { generation.current++; controller.abort(); };
  }, [sandboxID, command, revision, key, pending]);
  useErrorToast(error, reload);
  const snapshot = !pending && result?.key === key ? result.data : undefined;
  const categories: Record<string, string> = zh ? { agent: '编码 Agent', containers: '容器', operations: '运维', system: '系统工具', other: '其它程序' } : { agent: 'Coding agent', containers: 'Containers', operations: 'Operations', system: 'System tools', other: 'Other programs' };
  return <section className="host-programs" aria-label={zh ? '宿主程序' : 'Host programs'}>
    <div className="host-programs-heading"><h3>{zh ? '宿主程序' : 'Host programs'}</h3><Button type="button" size="sm" variant="secondary" loading={loading} disabled={pending} onClick={reload}>{zh ? '重新检测' : 'Scan again'}</Button></div>
    <p className="muted">{zh ? '查找此节点服务账号可见的程序。登录状态、依赖和实际运行权限需在使用时确认。' : 'Find programs visible to this node’s service account. Login, dependencies and runtime permissions are checked when used.'}</p>
    <div className="host-programs-search"><TextField label={zh ? '按命令名查找' : 'Find by command name'} placeholder="codex, docker…" value={input} maxLength={80} onChange={event => setInput(event.target.value)} onCompositionStart={() => setComposing(true)} onCompositionEnd={event => { setInput(event.currentTarget.value); setComposing(false); }} error={!composing && !searchValid ? (zh ? '请输入命令名，不包含路径。' : 'Enter a command name without a path.') : undefined} onKeyDown={event => { if (event.key === 'Enter') { event.preventDefault(); if (!composing && !event.nativeEvent.isComposing && searchValid) setCommand(term); } }} />
      {input && <Button type="button" variant="ghost" onClick={() => setInput('')}>{zh ? '显示常用程序' : 'Show common programs'}</Button>}
    </div>
    {(loading || pending && searchValid) && !snapshot && <p className="muted" role="status">{t.loading}</p>}
    {!loading && !error && snapshot && !snapshot.Programs.length && <p className="muted">{zh ? '未发现程序。可以查找其它命令名，或在授权表单中手动填写路径。服务账号的环境可能与登录终端不同。' : 'No programs found. Search another command or enter a path in the permission form. The service environment may differ from your terminal.'}</p>}
    <div className="host-programs-list">{snapshot?.Programs.map(program => <div className="host-programs-row" key={program.Executable}>
      <details><summary><strong>{program.Name}</strong><span className="muted">{categories[program.Category] ?? program.Category}</span><Badge size="sm" dot={false}>{program.Available ? (zh ? '已找到' : 'Found') : (zh ? '不可用' : 'Unavailable')}</Badge></summary>
        <code className="host-programs-path">{program.Executable}</code>
        {program.ResolvedPath !== program.Executable && <p className="muted host-programs-path">{zh ? '实际位置：' : 'Resolved path: '}{program.ResolvedPath}</p>}
        <p className="muted">{program.Source === 'path' ? 'PATH' : program.Source === 'system' ? (zh ? '系统目录' : 'System directory') : (zh ? '常用安装目录' : 'Installation directory')}</p>
        {!program.Available && <p className="muted">{program.Reason === 'launcher_unsupported' ? (zh ? '此启动器需要额外适配；请选择可直接执行的 .exe。' : 'This launcher needs an adapter; choose a directly executable .exe.') : (zh ? '文件没有执行权限。' : 'The file has no execute permission.')}</p>}
      </details>
      {onSelect && <Button type="button" size="sm" variant="secondary" disabled={loading || !!error || !program.Available} aria-label={`${zh ? '选择' : 'Select'} ${program.Name} · ${program.Executable}`} onClick={() => onSelect(program)}>{zh ? '选择' : 'Select'}</Button>}
    </div>)}</div>
    {snapshot && <small className="muted">{error ? (zh ? '检测失败，以下为上次结果。' : 'Scan failed; showing previous results.') : (zh ? '检测时间：' : 'Checked: ')}{new Date(snapshot.CheckedAt).toLocaleString(locale)}</small>}
  </section>;
}

export function HostProgramPicker({ sandboxID, onSelect }: { sandboxID: string; onSelect: (program: HostProgram) => void }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  const [open, setOpen] = useState(false);
  const label = zh ? '选择已发现的程序' : 'Choose a discovered program';
  return <><Tooltip content={label} width="auto" side="top" align="center" boundary="viewport"><IconButton className="host-program-picker" label={label} variant="secondary" onClick={() => setOpen(true)}><List aria-hidden="true" /></IconButton></Tooltip>
    <ModalPresence>{open && <Dialog open title={zh ? '选择宿主程序' : 'Choose a host program'} onOpenChange={setOpen}><HostPrograms key={sandboxID} sandboxID={sandboxID} onSelect={program => { onSelect(program); setOpen(false); }} /></Dialog>}</ModalPresence>
  </>;
}
