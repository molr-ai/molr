import { useErrorToast } from '../../../error-toasts';
import { GPUDeviceList } from './GPUAccess';
import { fullAccessLabel, isSandboxResource } from './PermissionTargetFields';
import type { GPUDevice, SandboxView } from '../../../sandbox-types';
import { Folder, Globe, Server, Sparkles, Wrench } from 'lucide-react';
import type { Grant, Model, PermissionRequestInput, Resource, ResourceConstraints } from '../../../api/types';
import { permissionLabel } from '../../../resource-permissions';
import { useCopy, useQuery } from '../../../state';

export function PermissionExpiry({ value, timeZone }: { value?: string | null; timeZone?: string }) {
  const { locale, t } = useCopy();
  return value ? <time dateTime={value}>{new Intl.DateTimeFormat(locale, {
    timeZone: timeZone || undefined, year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  }).format(new Date(value))}</time> : <>{t.resourcePermissions.noExpiry}</>;
}

function QuotaGrid({ value, gpus }: { value: ResourceConstraints; gpus?: readonly GPUDevice[] | null }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  const c = value.compute, n = value.network, s = value.storage;
  const number = (value: number) => value.toLocaleString(locale, { maximumFractionDigits: 2 });
  const bytes = (value: number) => {
    const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
    const index = Math.min(units.length - 1, Math.max(0, Math.floor(Math.log2(value || 1) / 10)));
    return `${number(value / 1024 ** index)} ${units[index]}`;
  };
  const period = { lifetime: zh ? '累计' : 'lifetime', day: zh ? '天' : 'day', week: zh ? '周' : 'week', month: zh ? '月' : 'month' };
  const rows: [string, string, boolean?][] = [
    ...(c ? [
      ...(c.cpu_percent>0 ? [ ['CPU', `${number(c.cpu_percent / 100)} ${zh ? '核' : 'cores'}`],
      [zh ? '内存' : 'Memory', bytes(c.memory_bytes)],
      [zh ? '进程 / 线程' : 'Processes / threads', number(c.processes)],
      ] : []),
      [zh ? '单次运行时长' : 'Per execution', c.unlimited_duration ? (zh ? '不限时长' : 'Unlimited') : c.max_duration_seconds < 60 ? `${number(c.max_duration_seconds)} ${zh ? '秒' : 'seconds'}` : `${number(c.max_duration_seconds / 60)} ${zh ? '分钟' : 'minutes'}`],
      ...(c.temp_bytes>0 ? [[zh ? '内存临时盘 /tmp' : 'RAM disk (/tmp)', bytes(c.temp_bytes)]] : []),
    ] as [string, string, boolean?][] : []),
    ...(n ? [
      [zh ? '双向流量' : 'Traffic · both directions', `${bytes(n.bytes)} / ${period[n.period]}`],
      [zh ? '并发连接' : 'Concurrent connections', number(n.connections)],
    ] as [string, string][] : []),
    ...(s ? [
      [zh ? '工作区容量' : 'Workspace capacity', bytes(s.capacity_bytes)],
      [zh ? '单文件上限' : 'Maximum file size', bytes(s.max_file_bytes)],
      [zh ? '文件和目录' : 'Files and directories', number(s.max_files), true],
    ] as [string, string, boolean?][] : []),
  ];
  return <dl className="permission-quota-grid">
    {rows.map(([label, value, separated]) => <div key={label} className={separated ? 'permission-quota-grid__separated' : undefined}><dt>{label}</dt><dd>{value}</dd></div>)}
    {c && !!gpus?.length && <div className="permission-quota-grid__separated" role="group" aria-label={zh ? 'GPU 授权' : 'GPU access'}>
      <dt>GPU</dt><dd>{c.gpu?.device_ids.length ? <GPUDeviceList ids={c.gpu.device_ids} devices={gpus} /> : zh ? '未授权' : 'Not granted'}</dd>
    </div>}
  </dl>;
}

/** Pending scopes use the proposal; resolved scopes use only the granted snapshot. */
export function PermissionScope({ input, grant, showReason, showExpiry, timeZone }: {
  input?: PermissionRequestInput; grant?: Grant; showReason?: boolean; showExpiry?: boolean; timeZone?: string;
}) {
  const { t, locale } = useCopy(), zh = locale === 'zh';
  const resource = useQuery<Resource>(grant ? `/api/resources/${grant.ResourceID}` : input?.resource_id ? `/api/resources/${input.resource_id}` : null);
  const sandboxes = useQuery<SandboxView[]>('/api/sandboxes');
  const modelID = grant ? grant.Scope.ModelID : input?.model_id;
  const models = useQuery<Model[]>(modelID ? '/api/models' : null);
  const kind = grant ? resource.data?.Kind : input?.resource_kind;
  const Icon = kind ? { storage: Folder, compute: Server, connection: Globe, network: Globe, model: Sparkles, tool: Wrench }[kind] : Wrench;
  const node = resource.data ? sandboxes.data?.find(s => s.ID === (resource.data?.SandboxID || 'local')) : undefined;
  const nodeName = node?.Local ? (zh ? '本机沙盒' : 'Local sandbox') : node?.Name;
  const target = isSandboxResource(kind) && nodeName ? `${nodeName}${kind === 'compute' ? '' : ` · ${resource.data?.Name ?? input?.resource_description ?? t.loading}`}` : grant ? resource.data?.Name ?? t.loading : input?.resource_description;
  const actions = grant ? grant.Actions : input?.actions ?? [];
  const paths = grant ? grant.Scope.PathPrefixes : input?.path_prefixes;
  const fullAccess = grant ? !!grant.Scope.FullAccess : !!input?.full_access;
  const constraints = fullAccess ? undefined : grant ? grant.Constraints : input?.constraints;
  const temporary = fullAccess ? undefined : grant ? grant.Temporary : input?.temporary;
  useErrorToast(resource.error || models.error, () => { resource.reload(); models.reload(); });
  return <section className="permission-scope" aria-label={target}>
    <div className="permission-scope__heading"><Icon aria-hidden="true" /><h5>{isSandboxResource(kind) ? `${zh ? '沙盒' : 'Sandbox'} · ` : ''}{fullAccess ? fullAccessLabel(zh) : kind ? t.resources.categories[kind] : t.loading}</h5>{!fullAccess && <span>{actions.map(action => permissionLabel(t, kind, action)).join(' · ')}</span>}</div>
    <p className="permission-scope__target">{target}</p>
    {showReason && input?.reason && <p className="permission-scope__note">{input.reason}</p>}
    {modelID && <p className="permission-scope__note">{t.resources.categories.model} · {models.data?.find(model => model.ID === modelID)?.Name ?? t.resources.modelUnavailable}</p>}
    {kind === 'storage' && <dl className="permission-scope__paths"><dt>{zh ? '目录范围' : 'Paths'}</dt><dd>{(paths?.length ? paths : ['/']).map(path => <code key={path}>{path === '/' ? (zh ? '整个资源目录' : 'Entire resource directory') : path}</code>)}</dd></dl>}
    {constraints && <>{temporary && <p className="permission-scope__note">{zh ? '基础额度' : 'Base limits'}</p>}<QuotaGrid value={constraints} gpus={node?.Info.GPUs} /></>}
    {temporary && <div className="permission-scope__temporary"><p className="permission-scope__note">{zh ? '临时额度 · 有效至 ' : 'Temporary limits · Until '}<PermissionExpiry value={temporary.until} timeZone={timeZone} /></p><QuotaGrid value={temporary.limits} gpus={node?.Info.GPUs} /></div>}
    {showExpiry && <p className="permission-scope__note">{zh ? '授权至 ' : 'Expires '}<PermissionExpiry value={grant ? grant.ExpiresAt : input?.expires_at} timeZone={timeZone} /></p>}
  </section>;
}
