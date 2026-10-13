import { useState } from 'react';
import { ModalPresence, Alert, Button, Progress } from '@molr/ui';
import { Globe, Settings2 } from 'lucide-react';
import type { ComponentStatus, MaintenanceTask } from '@molr/sdk';
import { api } from '../api/client';
import { useCopy, useQuery } from '../state';
import { useErrorToast } from '../error-toasts';
import './components.css';
import { BrowserProxySettings } from './BrowserProxySettings';

export function maintenanceReason(code: string | undefined, zh: boolean) {
 const reasons: Record<string, [string, string]> = {
  maintenance_not_installed: ['请先通过 molr-setup 安装此实例。', 'Install this instance with molr-setup first.'],
  maintenance_runner_unavailable: ['后台维护服务不可用，请在宿主使用 molr-setup 操作。', 'Background maintenance is unavailable. Use molr-setup on the host.'],
  maintenance_admin_cli_required: ['此操作需要安装账户在宿主执行。', 'Run this operation on the host as the installation owner.'],
  component_update_required: ['需要准备与当前 Molr 兼容的组件版本。', 'Prepare the component version compatible with this Molr build.'],
  maintenance_permission_denied: ['维护目录权限不足，请由安装账户检查。', 'The installation owner needs to check maintenance directory permissions.'],
  component_platform_unsupported: ['当前平台尚无经过验证的浏览器组件。', 'No tested browser component is available for this platform.'],
  component_externally_managed: ['正在使用管理员指定的浏览器程序。', 'Using browser programs supplied by the administrator.'],
  component_checksum_mismatch: ['下载文件校验失败，请重试。', 'Download verification failed. Retry the installation.'],
  component_download_failed: ['组件下载失败，请检查宿主网络后重试。', 'Download failed. Check the host network and retry.'],
  component_disk_full: ['组件目录的可用磁盘空间不足。', 'Not enough disk space in the component directory.'],
  maintenance_verification_pending: ['程序已安装，但健康验证未完成。请由安装账户使用 molr-setup --repair 检查服务。', 'Programs are installed, but health verification is incomplete. Ask the installation owner to check the service with molr-setup --repair.'],
  maintenance_interrupted: ['维护任务已中断，可以重试。', 'Maintenance was interrupted. You can retry.'],
  maintenance_cancelled: ['维护任务已取消。', 'Maintenance was cancelled.'],
  browser_start_failed: ['组件准备完成，但浏览器未能启动。请检查计算授权、系统依赖和沙盒状态后重试。', 'Components are prepared, but the browser could not start. Check compute grants, system dependencies and sandbox status, then retry.'],
  unauthenticated: ['登录状态已失效，请重新登录后启动浏览器。', 'The session expired. Sign in again to start the browser.'],
 };
 return reasons[code ?? '']?.[zh ? 0 : 1] ?? (zh ? '维护操作未完成，请检查环境后重试。' : 'Maintenance failed. Check the environment and retry.');
}
export function MaintenanceProgress({ taskID }: { taskID: string }) {
 const { locale } = useCopy(), zh = locale === 'zh';
 const query = useQuery<MaintenanceTask>(`/api/maintenance/tasks/${taskID}`, false, 1000);
 const [error, setError] = useState<unknown>(); useErrorToast(error); useErrorToast(query.error, query.reload);
 const task = query.data;
 if (!task) return null;
 const stages: Record<string, [string, string]> = { starting: ['正在准备', 'Preparing'], checking: ['检查环境', 'Checking environment'], downloading: ['下载组件', 'Downloading'], verifying: ['校验文件', 'Verifying'], installing: ['安装文件', 'Installing'], restarting: ['重启服务并验证', 'Restarting and verifying'] };
 return <div className="maintenance-progress" role="status">
  {task.state === 'running' ? <><Progress label={stages[task.stage]?.[zh ? 0 : 1] ?? task.stage} value={task.total ? task.downloaded ?? 0 : undefined} max={task.total || 100}/>{!['installing', 'restarting'].includes(task.stage) && <Button size="sm" variant="ghost" onClick={async () => { try { await api.post(`/api/maintenance/tasks/${task.id}/cancel`); await query.reload(); } catch (e) { setError(e); } }}>{zh ? '取消安装' : 'Cancel installation'}</Button>}</> : task.state === 'succeeded' ? <span>{task.operation === 'update' ? (zh ? 'Molr 更新已完成' : 'Molr update complete') : (zh ? '组件已准备完成' : 'Preparation complete')}</span> : <Alert tone="warning" title={maintenanceReason(task.error_code, zh)}/>}
 </div>;
}
export function ComponentDetails({ value }: { value: ComponentStatus }) {
 const { locale } = useCopy(), zh = locale === 'zh';
 return <div className="component-details">{value.target_version && value.state !== 'external' && <span>{value.version ? `${zh ? '已安装版本' : 'Installed'} ${value.version}` : `${zh ? '下载约' : 'Download'} ${Math.ceil(value.download_bytes / 1048576)} MB · ${zh ? '所需磁盘空间约' : 'Disk space'} ${Math.ceil(value.disk_bytes / 1048576)} MB`}</span>}{value.reason && <span>{maintenanceReason(value.reason, zh)}</span>}</div>;
}
export function Components() {
 const { locale } = useCopy(), zh = locale === 'zh';
 const query = useQuery<ComponentStatus[]>('/api/components', false, 2000);
 const [error, setError] = useState<unknown>(), [busy, setBusy] = useState(false), [settingsOpen, setSettingsOpen] = useState(false);
 useErrorToast(error); useErrorToast(query.error, query.reload);
 async function act(action: string) { setBusy(true); setError(undefined); try { await api.post(`/api/components/browser/${action}`, { idempotency_key: crypto.randomUUID().replaceAll('-', '') }); await query.reload(); } catch (e) { setError(e); } finally { setBusy(false); } }
 const value = query.data?.find(component => component.id === 'browser');
 const states: Record<string, [string, string]> = { installed: ['已安装', 'Installed'], missing: ['未安装', 'Not installed'], broken: ['需要修复', 'Repair needed'], external: ['自定义安装', 'Custom installation'], unsupported: ['当前平台不支持', 'Unsupported platform'] };
 return <section className="components-settings">
  <ul className="components-list" aria-label={zh ? '组件列表' : 'Components list'}><li className="component-row settings-card">
   <div className="component-summary"><Globe size={28} aria-hidden="true"/><div className="component-copy">
    <div className="component-heading"><h3>{zh ? '浏览器组件' : 'Browser components'}</h3>{value && <span className="component-state">{states[value.state]?.[zh ? 0 : 1]}</span>}</div>
    {value && <ComponentDetails value={value}/>}
   </div></div>
   <div className="component-actions">
    {value?.can_install && <><Button size="sm" disabled={busy || value.task?.state === 'running'} onClick={() => void act(value.state === 'installed' ? 'update' : 'install')}>{value.state === 'installed' ? (zh ? '更新组件' : 'Update components') : (zh ? '安装组件' : 'Install components')}</Button>{value.state === 'installed' && <Button variant="secondary" size="sm" disabled={busy || value.task?.state === 'running'} onClick={() => void act('repair')}>{zh ? '修复' : 'Repair'}</Button>}</>}
    <Button variant="secondary" size="sm" aria-label={zh ? '浏览器设置' : 'Browser settings'} onClick={() => setSettingsOpen(true)}><Settings2 size={15}/>{zh ? '设置' : 'Settings'}</Button>
   </div>
   {value?.task && <MaintenanceProgress taskID={value.task.id}/>}
  </li></ul>
  <ModalPresence>{settingsOpen && <BrowserProxySettings onClose={() => setSettingsOpen(false)}/>}</ModalPresence>
 </section>;
}
