import { CircleHelp, ShieldAlert, Terminal } from 'lucide-react';
import './full-access-scope.css';
import { Badge, Button, TextField, SegmentedControl } from '@molr/ui';
import type { FullAccessScope } from '@molr/sdk';
import { useCopy } from '../../../state';

export function validFullAccessScope(value?: FullAccessScope): boolean {
  if (value?.mode === 'unrestricted') return !value.programs?.length;
  return value?.mode === 'programs' && !!value.programs?.length && value.programs.length <= 32 && value.programs.every(p =>
    /^(\/|[a-zA-Z]:[\\/]|\\\\)/.test(p.executable) && !p.executable.includes('\0') &&
    (p.arguments === 'any' || p.arguments === 'exact') && (p.arguments !== 'any' || !p.args?.length));
}

export function FullAccessScopeSummary({ value }: { value?: FullAccessScope }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  if (value?.mode === 'unrestricted') return <div className="full-access-summary full-access-summary--unrestricted">
    <div className="full-access-summary__heading"><ShieldAlert aria-hidden="true" /><strong>{zh ? '不限范围' : 'Unrestricted'}</strong></div>
    <p>{zh ? '可读写服务账号可访问的文件、执行任意命令并联网。' : 'Read and write files, run any command and access the network with the service account’s permissions.'}</p>
  </div>;
  if (value?.mode !== 'programs' || !value.programs?.length) return <div className="full-access-summary full-access-summary--missing">
    <div className="full-access-summary__heading"><CircleHelp aria-hidden="true" /><strong>{zh ? '尚未声明授权范围' : 'Access scope not declared'}</strong></div>
    <p>{zh ? '请在授权表单中明确选择范围。' : 'Select an explicit scope in the permission form.'}</p>
  </div>;
  return <div className="full-access-programs" aria-label={zh ? '程序授权范围' : 'Program access scope'}>
    {value.programs.map((rule, index) => {
      const args = rule.args ?? [];
      const name = rule.executable.split(/[\\/]/).filter(Boolean).pop() || rule.executable;
      const mode = rule.arguments === 'any' ? (zh ? '任意参数' : 'Any arguments') : args.length ? (zh ? `固定参数 · ${args.length}` : `Exact arguments · ${args.length}`) : (zh ? '无参数' : 'No arguments');
      return <section key={index} className="full-access-program" aria-label={rule.executable}>
        <div className="full-access-program__heading">
          <Terminal aria-hidden="true" />
          <strong>{name}</strong>
          <Badge size="sm" tone="neutral" dot={false}>{mode}</Badge>
        </div>
        <code className="full-access-program__path">{rule.executable}</code>
        {rule.arguments === 'exact' && (args.length ? <ol className="full-access-arguments" aria-label={zh ? '固定参数（按顺序）' : 'Exact arguments in order'}>
          {args.map((arg, i) => <li key={i}>
            <span className="full-access-arguments__index" aria-hidden="true">{i + 1}</span>
            <code>{JSON.stringify(arg)}</code>
            {arg === '' && <span className="full-access-arguments__empty">{zh ? '空参数' : 'Empty argument'}</span>}
          </li>)}
        </ol> : <p className="full-access-program__note">{zh ? '仅允许不带参数启动。' : 'Launch without arguments only.'}</p>)}
      </section>;
    })}
  </div>;
}

export function FullAccessScopeEditor({ value, onChange }: { value?: FullAccessScope; onChange: (value: FullAccessScope) => void }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  const rules = value?.programs ?? [];
  const update = (index: number, patch: Partial<(typeof rules)[number]>) => onChange({ mode: 'programs', programs: rules.map((rule, i) => i === index ? { ...rule, ...patch } : rule) });
  const newRule = () => ({ executable: '', arguments: 'exact' as const, args: [] });
  return <div className="full-access-scope-editor">
    <SegmentedControl size="sm" label={zh ? '访问范围' : 'Access scope'} value={value?.mode ?? ''} onValueChange={mode => onChange(mode === 'unrestricted' ? { mode: 'unrestricted' } : { mode: 'programs', programs: [newRule()] })} options={[{ value: 'programs', label: zh ? '指定程序' : 'Specified programs' }, { value: 'unrestricted', label: zh ? '不限范围' : 'Unrestricted' }]} />
    {!value && <p className="muted">{zh ? '请选择访问范围。' : 'Select an access scope.'}</p>}
    {value?.mode === 'unrestricted' && <p className="muted">{zh ? '允许直接读写宿主文件、执行任意命令和联网。' : 'Allow direct host file access, arbitrary commands and network access.'}</p>}
    {value?.mode === 'programs' && <>
      <p className="muted">{zh ? '仅允许启动以下程序。程序及其子进程以服务账号运行，不受 Molr 沙盒限制。请使用可信且助理无法修改的程序；脚本、插件和配置也属于信任范围。' : 'Only these programs may be launched. They and their children run as the service account outside the Molr sandbox. Use trusted programs the assistant cannot modify; scripts, plugins and configuration are also trusted.'}</p>
      {rules.map((rule, index) => <div className="full-access-scope-editor" key={index}>
        <TextField required label={zh ? `程序路径 ${index + 1}` : `Program path ${index + 1}`} value={rule.executable} onChange={e => update(index, { executable: e.target.value })} placeholder={zh ? '宿主机上的绝对路径' : 'Absolute path on the host'} />
        <SegmentedControl size="sm" label={zh ? '参数范围' : 'Arguments'} value={rule.arguments} onValueChange={argumentsMode => update(index, { arguments: argumentsMode as 'exact' | 'any', args: [] })} options={[{ value: 'exact', label: zh ? '固定参数' : 'Exact arguments' }, { value: 'any', label: zh ? '任意参数' : 'Any arguments' }]} />
        {rule.arguments === 'exact' && <>
          <p className="muted">{zh ? '按顺序逐项匹配，保留空格和空参数。没有参数项表示仅允许无参数启动。' : 'Match each argument in order, preserving spaces and empty values. No entries permits only a launch without arguments.'}</p>
          {(rule.args ?? []).map((arg, i) => <div key={i}><TextField label={zh ? `参数 ${i + 1}` : `Argument ${i + 1}`} value={arg} onChange={e => update(index, { args: rule.args!.map((a, j) => j === i ? e.target.value : a) })} /><Button type="button" variant="ghost" onClick={() => update(index, { args: rule.args!.filter((_, j) => j !== i) })}>{zh ? '移除参数' : 'Remove argument'}</Button></div>)}
          <Button type="button" variant="secondary" disabled={(rule.args?.length ?? 0) >= 256} onClick={() => update(index, { args: [...(rule.args ?? []), ''] })}>{zh ? '添加参数' : 'Add argument'}</Button>
        </>}
        <Button type="button" variant="ghost" onClick={() => onChange({ mode: 'programs', programs: rules.filter((_, i) => i !== index) })}>{zh ? '移除程序' : 'Remove program'}</Button>
      </div>)}
      <Button type="button" variant="secondary" disabled={rules.length >= 32} onClick={() => onChange({ mode: 'programs', programs: [...rules, newRule()] })}>{zh ? '添加程序' : 'Add program'}</Button>
    </>}
  </div>;
}
