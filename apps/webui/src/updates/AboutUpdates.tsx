import { useEffect, useState, type ReactNode } from 'react';
import { Alert, Badge, Button, Dialog, ModalPresence, Tooltip } from '@molr/ui';
import { CheckCircle2, Info, Monitor, RefreshCw, Server } from 'lucide-react';
import type { RuntimeInfo } from '@molr/sdk';
import { useCopy, useQuery } from '../state';
import { useErrorToast } from '../error-toasts';
import { buildInfo } from '../build-info';
import { poll } from '../polling';
import { useUpdates } from './provider';
import { DesktopRelease, LocalServiceRelease } from './DesktopRelease';
import { ServiceRelease } from './ServiceRelease';

export function VersionEntry() {
 const updates = useUpdates(), { locale } = useCopy(), zh = locale === 'zh';
 const [open, setOpen] = useState(false);
 const current = updates.app?.build ?? updates.data?.current;
 const status = updates.data;
 useEffect(() => { updates.reload(); }, [updates.reload]);
 useEffect(() => {
  if (updates.managed || !status?.auto_check || !status.source_configured || !status.supported) return;
  const loop = poll(async () => { await updates.check(true); return 300_000; });
  return () => loop.stop();
 }, [status?.auto_check, status?.source_configured, status?.supported, updates.check, updates.managed]);
 const show = () => setOpen(true);
 return <div className="settings-about-version">
  <Tooltip content={zh ? '查看版本详情' : 'View version details'} width="auto">
   <button type="button" className="version-trigger" aria-haspopup="dialog" aria-expanded={open} aria-label={zh ? '查看版本详情' : 'View version details'} onClick={show}>
    <span>{updates.app ? (zh ? '桌面版本' : 'Desktop version') : (zh ? '服务版本' : 'Service version')}</span>
    <span className="version-trigger-value"><strong>{current?.version || '—'}</strong><Info size={14} aria-hidden="true" /></span>
    <span className="version-tags">
     {updates.mismatch && <Badge tone="warning" size="sm" dot={false}>{zh ? '版本不一致' : 'Version mismatch'}</Badge>}
     {updates.available && <Badge tone="warning" size="sm" dot={false}>{zh ? '可更新' : 'Update available'}</Badge>}
    </span>
   </button>
  </Tooltip>
  <ModalPresence>{open && <VersionDetails onClose={() => setOpen(false)} />}</ModalPresence>
 </div>;
}

function VersionComponent({ name, build, icon, note }: { name: string; build?: { version: string; commit: string }; icon: ReactNode; note?: string }) {
 return <section className="version-component" aria-label={name}>
  <div className="version-component-name"><h3>{icon}{name}</h3>{note && <span className="update-meta">{note}</span>}</div>
  <div className="version-component-build"><strong>{build?.version || '—'}</strong><span className="version-commit">{build?.commit ? <a href={`https://github.com/molr-ai/molr/commit/${build.commit}`} aria-label={`${name} commit ${build.commit}`} title={build.commit} target="_blank" rel="noopener noreferrer"><code>{build.commit.slice(0,12)}</code></a> : '—'}</span></div>
 </section>;
}

function Consistency() {
 const updates = useUpdates(), { locale } = useCopy(), zh = locale === 'zh';
 const consistent = updates.consistency === 'consistent' && !updates.unreachable;
 const messages = updates.unreachable ? [zh ? '暂时无法连接 Runtime，无法检查一致性' : 'Cannot check consistency while Runtime is unreachable']
  : consistent ? [zh ? '所有组件版本一致' : 'All component versions match']
  : [({
   consistent: null,
   version_mismatch: zh ? 'WebUI 与服务版本不一致' : 'WebUI and Runtime versions differ',
   commit_mismatch: zh ? 'WebUI 与服务构建不一致' : 'WebUI and Runtime commits differ',
   unknown: zh ? 'WebUI 或服务构建信息不足，无法确认一致性' : 'WebUI or Runtime build information is incomplete; consistency is unknown',
  }[updates.webuiConsistency]), updates.desktopConsistency && ({
   consistent: null,
   version_mismatch: zh ? '桌面与服务版本不同，可继续使用' : 'Desktop and Runtime versions differ; you can keep using them',
   commit_mismatch: zh ? '桌面与服务构建不同，可继续使用' : 'Desktop and Runtime commits differ; you can keep using them',
   unknown: zh ? '桌面或服务构建信息不足，无法确认一致性' : 'Desktop or Runtime build information is incomplete; consistency is unknown',
  }[updates.desktopConsistency])];
 return <div className="version-consistency">{messages.filter(Boolean).map(text => <p key={text} className="update-build-status" data-consistent={consistent || undefined} role="status">{consistent ? <CheckCircle2 size={15} aria-hidden="true" /> : <Info size={15} aria-hidden="true" />}{text}</p>)}</div>;
}

function VersionDetails({ onClose }: { onClose: () => void }) {
 const updates = useUpdates(), { locale } = useCopy(), zh = locale === 'zh', status = updates.data;
 const [now, setNow] = useState(Date.now()), [checking, setChecking] = useState(false);
 useEffect(() => { updates.reload(); }, [updates.reload]);
 const runtimeNext = Date.parse(status?.check.next_allowed_at || ''), desktopNext = Date.parse(updates.desktop?.nextAllowedAt || '');
 const runtimeCanCheck = !updates.managed && !!status?.source_configured && status.supported;
 const desktopCanCheck = !!updates.app && updates.desktop?.comparison !== 'uncomparable';
 const runtimeReady = runtimeCanCheck && !(runtimeNext > now) && !status?.check.checking && !updates.checking;
 const desktopReady = desktopCanCheck && !updates.desktop?.checking && !(desktopNext > now);
 const busy = checking || updates.checking || status?.check.checking || updates.desktop?.checking;
 const retryAt = Math.min(...[runtimeCanCheck ? runtimeNext : NaN, desktopCanCheck ? desktopNext : NaN].filter(value => value > now));
 useEffect(() => {
  if (!Number.isFinite(retryAt)) return;
  const timer = setTimeout(() => setNow(Date.now()), Math.min(1000, Math.max(0, retryAt - Date.now()) + 20));
  return () => clearTimeout(timer);
 }, [retryAt, now]);
 const disabled = !!busy || (!runtimeReady && !desktopReady);
 const seconds = Math.max(1, Math.ceil((retryAt - now) / 1000));
 const checkLabel = busy ? (zh ? '正在检查新版本…' : 'Checking for new versions…')
  : !disabled ? (zh ? '立即检查新版本' : 'Check for new versions now')
  : Number.isFinite(retryAt) ? (zh ? `${seconds}秒后可再次检查` : `Check again in ${seconds}s`)
  : !updates.managed && status && !status.source_configured ? (zh ? '未配置更新源' : 'No update source configured')
  : !updates.managed && status && !status.supported ? (zh ? '暂无适用发行包' : 'No release for this platform')
  : updates.desktop?.comparison === 'uncomparable' ? (zh ? '开发版本，无法检查更新' : 'Development build; updates unavailable')
  : updates.error || updates.desktopError ? (zh ? '无法读取版本信息' : 'Unable to read version information')
  : (zh ? '正在读取版本信息…' : 'Reading version information…');
 async function check() {
  setChecking(true);
  try { await Promise.all([updates.reload(), ...(runtimeReady ? [updates.check()] : []), ...(desktopReady ? [updates.checkDesktop()] : [])]); }
  finally { setNow(Date.now()); setChecking(false); }
 }
 const serviceName = updates.managed ? (zh ? '本地服务 · Runtime' : 'Local service · Runtime') : updates.app ? (zh ? '远程服务 · Runtime' : 'Remote service · Runtime') : (zh ? '服务 · Runtime' : 'Service · Runtime');
 const pageMismatch = ['version_mismatch', 'commit_mismatch'].includes(updates.webuiConsistency);
 return <Dialog open title={zh ? '版本详情' : 'Version details'} size="md" onOpenChange={open => { if (!open) onClose(); }} closeLabel={zh ? '关闭版本详情' : 'Close version details'} footer={<Button variant="secondary" size="sm" disabled={disabled} loading={!!busy} onClick={() => void check()}>{!busy && <RefreshCw size={14} aria-hidden="true" />}{checkLabel}</Button>}>
  <div className="version-details">
   <div className="version-releases">
    {updates.app && <DesktopRelease />}
    {updates.managed ? <LocalServiceRelease /> : <ServiceRelease />}
   </div>
   <section className="version-components" aria-label={zh ? '组件版本' : 'Component versions'}>
    <h2>{zh ? '组件版本' : 'Component versions'}</h2>
    {updates.app && <VersionComponent name={zh ? '桌面 App' : 'Desktop app'} build={updates.app.build} icon={<Monitor size={15} aria-hidden="true" />} />}
    <VersionComponent name={serviceName} build={status?.current} icon={<Server size={15} aria-hidden="true" />} />
    <VersionComponent name="WebUI" build={buildInfo} icon={<Monitor size={15} aria-hidden="true" />} note={zh ? '随服务更新' : 'Updated with the service'} />
   </section>
   {pageMismatch && !updates.unreachable && <Alert tone="warning" title={zh ? '页面与服务版本不同，请刷新页面。' : 'Page and service versions differ. Please refresh.'}><p>{zh ? '请先保存当前输入。刷新后仍有提示时，请管理员核对部署版本。' : 'Save your input first. If the warning remains after refreshing, ask the administrator to check the deployment.'}</p><Button variant="secondary" size="sm" onClick={() => window.location.reload()}>{zh ? '刷新页面' : 'Refresh page'}</Button></Alert>}
   <Consistency />
  </div>
 </Dialog>;
}

export function AboutService() {
 const { t, locale } = useCopy(), runtime = useQuery<RuntimeInfo>('/api/status');
 useErrorToast(runtime.error, runtime.reload);
 return <div className="about-updates"><section className="about-service" aria-label={locale === 'zh' ? '服务信息' : 'Service information'}>
  <header className="update-section-heading"><h3>{locale === 'zh' ? '服务信息' : 'Service information'}</h3></header>
  <dl className="update-builds">
   <div><dt>{t.settings.runtimeName}</dt><dd className="update-runtime-value">{runtime.data?.name || '—'}</dd></div>
   <div><dt>{t.settings.runtimeAddress}</dt><dd className="update-runtime-value">{runtime.data?.public_url || '—'}</dd></div>
  </dl>
 </section></div>;
}
