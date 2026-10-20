import { useState } from 'react';
import { Alert, Button } from '@molr/ui';
import { useCopy } from '../state';
import { useErrorToast } from '../error-toasts';
import { useUpdates } from './provider';
import { ReleaseSummary, VersionChange } from './ReleaseSummary';

export function DesktopRelease() {
 const { locale } = useCopy(), zh = locale === 'zh';
 const updates = useUpdates(), status = updates.desktop;
 const [error, setError] = useState<unknown>(), [pending, setPending] = useState(false);
 useErrorToast(error);
 async function download() { setError(undefined); setPending(true); try { await window.molrApp!.updates.openDownloads(); } catch (e) { setError(e); } finally { setPending(false); } }
 const text = !status ? zh ? '正在读取桌面版本信息…' : 'Reading desktop version…' : ({
  unknown: zh ? '尚未检查桌面更新' : 'Desktop updates have not been checked',
  current: zh ? '桌面应用已是最新正式版' : 'Desktop app is up to date with the stable release',
  available: zh ? '桌面应用有新版本' : 'Desktop update available',
  ahead: zh ? '桌面应用高于当前正式版' : 'Desktop app is newer than the stable release',
  uncomparable: zh ? '开发版本，无法自动比较' : 'Development build; automatic comparison is unavailable',
  unavailable: zh ? '暂无适用于此设备的正式版' : 'No stable desktop release for this device',
 }[status.comparison]);
 return <ReleaseSummary title={text} tone={status?.error || updates.desktopError ? undefined : status?.comparison === 'available' ? 'available' : status?.comparison === 'current' ? 'current' : undefined}>
  {status?.comparison === 'available' && <VersionChange current={updates.app?.build.version} target={status.latestVersion} />}
  {(status?.error || updates.desktopError) && <Alert tone="warning" title={zh ? '暂时无法检查桌面更新' : 'Unable to check desktop updates'}>{zh ? '请稍后重试。已显示的发布信息来自上次成功检查。' : 'Try again later. Any displayed release information is from the last successful check.'}</Alert>}
  {status?.comparison === 'available' && <div className="update-actions"><Button size="sm" disabled={pending} onClick={() => void download()}>{zh ? '下载桌面新版' : 'Download desktop update'}</Button></div>}
 </ReleaseSummary>;
}

export function LocalServiceRelease() {
 const { locale } = useCopy(), zh = locale === 'zh', updates = useUpdates(), local = updates.desktop?.local;
 const [error, setError] = useState<unknown>(), [pending, setPending] = useState(false);
 useErrorToast(error);
 async function upgrade() { setError(undefined); setPending(true); try { await window.molrApp!.updates.upgradeLocal(); } catch (e) { setError(e); } finally { setPending(false); } }
 const current = updates.data?.current.version || local?.version;
 const text = updates.localPending ? (zh ? '本地服务更新待完成' : 'Local service update pending')
  : !local?.targetVersion || !current ? (zh ? '暂时无法确认本地服务更新状态' : 'Local service update status is unknown')
  : updates.desktop?.comparison === 'available' && current === local.targetVersion ? (zh ? '先更新桌面应用，再更新本地服务' : 'Update the desktop app first, then the local service')
  : current === local.targetVersion ? (zh ? '本地服务已是桌面附带版本' : 'Local service matches the bundled version')
  : (zh ? '暂无可安装的桌面附带更新' : 'No newer bundled service is available');
 return <ReleaseSummary title={text} tone={updates.localPending ? 'available' : undefined}>
  {updates.localPending && <>
   <VersionChange current={current} target={local?.targetVersion} />
   <p className="update-meta">{zh ? '更新会重启服务并中断运行中的任务，也可以稍后更新。' : 'Updating restarts the service and interrupts running tasks. You can update later.'}</p>
   <div className="update-actions"><Button size="sm" disabled={pending || local?.busy} onClick={() => void upgrade()}>{zh ? '更新并重启本地服务' : 'Update and restart local service'}</Button></div>
  </>}
  {local?.error && <Alert tone="warning" title={zh ? '本地服务操作未完成' : 'Local service action did not complete'}>{local.error}</Alert>}
 </ReleaseSummary>;
}
