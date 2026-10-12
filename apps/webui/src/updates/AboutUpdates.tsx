import { Components, MaintenanceProgress } from '../maintenance/Components';
import type { MaintenanceTask } from '@molr/sdk';
import { api } from '../api/client';
import { useEffect, useId, useState } from 'react';
import { Alert, Button } from '@molr/ui';
import { ArrowUpRight, CheckCircle2, ChevronDown, CircleArrowUp, Info, Monitor, RefreshCw, Server } from 'lucide-react';
import type { RuntimeInfo, UpdateInstallation } from '@molr/sdk';
import { useCopy, useQuery, useSession } from '../state';
import { useErrorToast } from '../error-toasts';
import { buildInfo } from '../build-info';
import { poll } from '../polling';
import { useUpdates } from './provider';
import { upgradeCommand } from './build';

const guide = 'https://molr.ai/docs/deployment/';
export function AboutUpdates() {
 const updates = useUpdates(), { t, locale } = useCopy(), { principal } = useSession();
 const runtime = useQuery<RuntimeInfo>('/api/status');
 useErrorToast(runtime.error, runtime.reload);
 const zh = locale === 'zh', admin = !!principal?.User?.Administrator, status = updates.data;
 const installation = useQuery<UpdateInstallation | null>(admin ? '/api/updates/installation' : null, false, 0);
 const maintenance = useQuery<{can_update: boolean; task?: MaintenanceTask}>(admin ? '/api/maintenance' : null, false, 2000);
 const [applyError, setApplyError] = useState<unknown>(), [applying, setApplying] = useState(false); useErrorToast(applyError);
 async function applyUpdate() { if (!status?.latest) return; setApplying(true); try { await api.post('/api/updates/apply', {version: status.latest.version, idempotency_key: crypto.randomUUID().replaceAll('-', '')}); await maintenance.reload(); } catch(e) { setApplyError(e); } finally { setApplying(false); } }
 const [showGuide, setShowGuide] = useState(false), [now, setNow] = useState(Date.now());
 useEffect(() => { updates.reload(); }, [updates.reload]);
 useEffect(() => {
  if (!status?.auto_check || !status.source_configured || !status.supported) return;
  const loop = poll(async () => { await updates.check(true); return 300_000; });
  return () => loop.stop();
 }, [status?.auto_check, status?.source_configured, status?.supported, updates.check]);
 const guideID = useId();
 const next = Date.parse(status?.check.next_allowed_at || '');
 useEffect(() => {
  setNow(Date.now());
  if (!Number.isFinite(next)) return;
  const timer = setTimeout(() => setNow(Date.now()), Math.max(0, next - Date.now()) + 20);
  return () => clearTimeout(timer);
 }, [next]);
 const time = (value?: string | null) => value ? new Date(value).toLocaleString(locale === 'zh' ? 'zh-CN' : 'en-US', { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false }) : '—';
 const mismatch = ['version_mismatch','commit_mismatch'].includes(updates.consistency);
 const consistencyText = {
  consistent: zh ? '前后端版本一致' : 'WebUI and Runtime builds match',
  version_mismatch: zh ? '前后端版本不一致' : 'WebUI and Runtime versions differ',
  commit_mismatch: zh ? '前后端构建不一致' : 'WebUI and Runtime commits differ',
  unknown: zh ? '构建信息不足，无法确认版本一致性' : 'Build information is incomplete; consistency is unknown',
 }[updates.consistency];
 const releaseText = !status ? zh ? '正在读取版本信息…' : 'Reading version information…' : !status.source_configured ? zh ? '未配置更新源' : 'No update source configured' : !status.supported ? zh ? '暂无适用发行包' : 'No release package for this platform' : ({
  unknown: zh ? '尚未成功检查发布版本' : 'No successful release check yet',
  current: zh ? '已是最新版本' : 'Up to date',
  available: zh ? `发现新版本 v${status.latest?.version}` : `Version ${status.latest?.version} is available`,
  ahead: zh ? '当前版本高于推荐发布版本' : 'This build is newer than the recommended release',
  uncomparable: zh ? '开发或自定义构建，无法自动比较' : 'Development or custom build; automatic comparison is unavailable',
 }[status.comparison]);
 const errorText: Record<string, string> = zh ? {
  timeout: '更新源响应超时，请稍后重试。', network_error: '无法连接更新源，请检查网络。',
  release_not_found: '更新源尚未提供当前平台的发布清单。', source_unavailable: '更新源暂时不可用，请稍后重试。',
  invalid_manifest: '发布清单格式或版本信息无效，请联系管理员。', invalid_source: '更新源地址无效，请联系管理员。',
 } : {
  timeout: 'The release source timed out. Try again later.', network_error: 'Cannot reach the release source. Check your connection.',
  release_not_found: 'The source has no release manifest for this platform.', source_unavailable: 'The release source is temporarily unavailable.',
  invalid_manifest: 'The release manifest is invalid. Contact your administrator.', invalid_source: 'The release source address is invalid. Contact your administrator.',
 };
 const checking = updates.checking || status?.check.checking;
 const available = status?.comparison === 'available';
 const ReleaseIcon = available ? CircleArrowUp : status?.comparison === 'current' ? CheckCircle2 : Info;
 const consistent = updates.consistency === 'consistent' && !updates.unreachable;
 return <div className="about-updates">
  <section className="about-release" aria-label={zh ? '可用更新' : 'Release updates'}>
   <header className="update-section-heading">
    <h3>{zh ? '软件更新' : 'Software updates'}</h3>
    <Button variant="secondary" size="sm" loading={!!checking} aria-label={zh ? '检查更新' : 'Check for updates'} disabled={!status?.source_configured || !status.supported || checking || next > now} onClick={() => void updates.check()}>
     {!checking && <RefreshCw size={14} aria-hidden="true" />}{checking ? zh ? '正在检查…' : 'Checking…' : zh ? '检查更新' : 'Check for updates'}
    </Button>
   </header>
   <div className="update-release-summary" data-available={available || undefined}>
    <span className="update-release-icon" aria-hidden="true"><ReleaseIcon size={22} strokeWidth={1.7} /></span>
    <div className="update-release-copy">
     <p className="update-release-title" role="status">{releaseText}</p>
     {status?.latest && <p className="update-meta">{!available && <span>{zh ? '推荐版本 ' : 'Recommended version '}v{status.latest.version} · </span>}{zh ? '发布于 ' : 'Published '}{time(status.latest.published_at)}</p>}
    </div>
   </div>
   {(status?.check.error_code || updates.checkError) && <Alert tone="warning" title={zh ? '暂时无法检查更新' : 'Unable to check for updates'}>{errorText[status?.check.error_code || 'network_error']}{status?.latest && <p>{zh ? '以上为上次成功检查的结果。' : 'The release shown is from the last successful check.'}</p>}</Alert>}
   {status && !status.auto_check && <p className="update-meta">{zh ? '自动发布检查已关闭，可手动检查。前后端一致性检查仍会运行。' : 'Automatic release checks are off. Manual checks and build consistency checks remain available.'}</p>}
   {status?.latest && <div className="update-actions">
    {admin && available && <Button variant="primary" size="sm" onClick={() => setShowGuide(v => !v)} aria-expanded={showGuide} aria-controls={guideID}>{zh ? '升级指引' : 'Upgrade instructions'}<ChevronDown className="update-guide-chevron" size={14} aria-hidden="true" /></Button>}
    <a href={status.latest.notes_url} target="_blank" rel="noopener noreferrer">{zh ? '更新说明' : 'Release notes'}<ArrowUpRight size={14} aria-hidden="true" /></a>
   </div>}
   {admin && available && maintenance.data?.can_update && <Button disabled={applying || maintenance.data.task?.state === 'running'} onClick={() => void applyUpdate()}>{zh ? '更新并重启 Molr' : 'Update and restart Molr'}</Button>}
   {admin && maintenance.data?.task?.operation === 'update' && <MaintenanceProgress taskID={maintenance.data.task.id}/>}
   {!admin && available && <p className="update-meta">{zh ? '请联系管理员升级。' : 'Contact your administrator to upgrade.'}</p>}
   {showGuide && admin && <div className="update-guide" id={guideID}><p>{zh ? '升级会重启服务并中断运行中的任务。先备份数据，再使用安装时的账户执行。' : 'Upgrading restarts the service and interrupts running tasks. Back up your data and use the original installation account.'}</p>
    {installation.data?.source && status?.latest ? <><p>{zh ? '准备原发布站点提供的 install.sh，然后执行：' : 'Obtain install.sh from your original release site, then run:'}</p><pre><code>{upgradeCommand(installation.data, status.latest.version)}</code></pre></> : <p>{zh ? '未找到可用的安装记录，请按部署指南升级。' : 'Installation details are unavailable. Follow the deployment guide.'}</p>}
    <a href={guide} target="_blank" rel="noopener noreferrer">{zh ? '部署与备份指南' : 'Deployment and backup guide'}<ArrowUpRight size={14} aria-hidden="true" /></a>
   </div>}
   <div className="update-section-meta">
    {status?.check.last_success_at && <span>{zh ? '最近成功检查 ' : 'Last successful check '}{time(status.check.last_success_at)}</span>}
    {next > now && <span>{zh ? '可再次检查时间 ' : 'Next check available '}{new Date(next).toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit', hour12: false })}</span>}
   </div>
  </section>
  {admin && <Components/>}
  <section className="about-builds" aria-label={zh ? '当前安装' : 'Installed builds'}>
   <header className="update-section-heading">
    <h3>{zh ? '版本信息' : 'Build information'}</h3>
    <Button variant="ghost" size="sm" onClick={updates.reload} disabled={updates.loading} aria-label={zh ? '重新检查一致性' : 'Recheck build consistency'}><RefreshCw size={14} aria-hidden="true" />{zh ? '重新检查' : 'Recheck'}</Button>
   </header>
   <dl className="update-builds">
    {[{ name: 'WebUI', icon: Monitor, build: buildInfo }, { name: 'Runtime', icon: Server, build: status?.current }].map(({ name, icon: Icon, build }) => <div key={name}>
     <dt><Icon size={16} aria-hidden="true" />{name}</dt>
     <dd><span className="update-build-version">{build?.version || '—'}</span>{build?.commit ? <a href={`https://github.com/molr-ai/molr/commit/${build.commit}`} target="_blank" rel="noopener noreferrer" title={build.commit}><code>{build.commit.slice(0,12)}</code></a> : <span className="update-meta">—</span>}</dd>
    </div>)}
   </dl>
   {mismatch && !updates.unreachable && <Alert tone="warning" title={consistencyText}>{status?.current.embedded_webui ? zh ? '服务端可能已更新。请保存当前输入后刷新页面，再检查版本。' : 'The server may have been updated. Save your input, then refresh this page and check again.' : zh ? '请管理员核对 WebUI 与 Runtime 的部署版本。' : 'Ask the administrator to check the deployed WebUI and Runtime builds.'}</Alert>}
   <div className="update-build-summary">
    {(!mismatch || updates.unreachable) && <p className="update-build-status" data-consistent={consistent || undefined} role="status">{consistent ? <CheckCircle2 size={15} aria-hidden="true" /> : <Info size={15} aria-hidden="true" />}{updates.unreachable ? zh ? '暂时无法连接 Runtime，无法检查一致性' : 'Cannot check consistency while Runtime is unreachable' : consistencyText}</p>}
    {updates.observedAt && <p className="update-meta">{zh ? '最近确认 ' : 'Last confirmed '}{time(updates.observedAt)}</p>}
   </div>
  </section>
  <section className="about-service" aria-label={zh ? '服务信息' : 'Service information'}>
   <header className="update-section-heading"><h3>{zh ? '服务信息' : 'Service information'}</h3></header>
   <dl className="update-builds">
    <div><dt>{t.settings.runtimeName}</dt><dd className="update-runtime-value">{runtime.data?.name || '—'}</dd></div>
    <div><dt>{t.settings.runtimeAddress}</dt><dd className="update-runtime-value">{runtime.data?.public_url || '—'}</dd></div>
   </dl>
  </section>
 </div>;
}
