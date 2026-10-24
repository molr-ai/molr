import { useId, useState } from 'react';
import { Alert, Button, CodeBlock } from '@molr/ui';
import { ArrowUpRight, ChevronDown } from 'lucide-react';
import type { MaintenanceTask, UpdateInstallation } from '@molr/sdk';
import { api, mutationKey } from '../api/client';
import { useCopy, useQuery, useSession } from '../state';
import { useErrorToast } from '../error-toasts';
import { MaintenanceProgress } from '../maintenance/Components';
import { useUpdates } from './provider';
import { upgradeCommand } from './build';
import { ReleaseSummary, VersionChange } from './ReleaseSummary';

export function ServiceRelease() {
 const updates = useUpdates(), { locale } = useCopy(), { principal } = useSession();
 const container = updates.data?.current.distribution === 'container';
 const guide = `https://molr.ai/${locale === 'zh' ? 'zh' : 'en'}/docs/admin/deployment/${container ? 'docker' : 'maintenance'}/`;
 const zh = locale === 'zh', admin = !!principal?.User?.Administrator, status = updates.data;
 const maintenance = useQuery<{can_update: boolean; task?: MaintenanceTask}>(admin && !updates.managed ? '/api/maintenance' : null, false, 2000);
 const [applyError, setApplyError] = useState<unknown>(), [applying, setApplying] = useState(false); useErrorToast(applyError);
 async function applyUpdate() { if (!status?.latest) return; setApplying(true); try { await api.post('/api/updates/apply', {version: status.latest.version, idempotency_key: mutationKey().slice(0, 32)}); await maintenance.reload(); } catch(e) { setApplyError(e); } finally { setApplying(false); } }
 const [showGuide, setShowGuide] = useState(false);
 const manual = admin && status?.comparison === 'available' && maintenance.data?.can_update === false && !maintenance.error;
 const installation = useQuery<UpdateInstallation | null>(manual && showGuide && !container ? '/api/updates/installation' : null, false, 0);
 const guideID = useId();
 const command = installation.data?.source && status?.latest ? upgradeCommand(installation.data, status.latest.version) : undefined;
 const releaseText = !status ? zh ? '正在读取版本信息…' : 'Reading version information…' : !status.source_configured ? zh ? '未配置更新源' : 'No update source configured' : !status.supported ? zh ? '暂无适用发行包' : 'No release package for this platform' : ({
  unknown: zh ? '尚未成功检查发布版本' : 'No successful release check yet',
  current: zh ? '服务已是最新版本' : 'Service is up to date',
  available: zh ? '服务有新版本' : 'Service update available',
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
 const available = status?.comparison === 'available';
 const failed = !!status?.check.error_code || updates.checkError;
 return <ReleaseSummary title={releaseText} tone={failed ? undefined : available ? 'available' : status?.comparison === 'current' ? 'current' : undefined}>
  {available && <VersionChange current={status?.current.version} target={status?.latest?.version} />}
  {available && status?.latest?.published_at && <p className="update-meta">{zh ? '发布于 ' : 'Published '}{new Date(status.latest.published_at).toLocaleDateString(locale === 'zh' ? 'zh-CN' : 'en-US')}</p>}
   {(status?.check.error_code || updates.checkError) && <Alert tone="warning" title={zh ? '暂时无法检查更新' : 'Unable to check for updates'}>{errorText[status?.check.error_code || 'network_error']}{status?.latest && <p>{zh ? '以上为上次成功检查的结果。' : 'The release shown is from the last successful check.'}</p>}</Alert>}
   {status?.source_configured && status.supported && !status.auto_check && <p className="update-meta">{zh ? '自动检查已关闭，可手动检查。' : 'Automatic checks are off. You can check manually.'}</p>}
   {status?.latest && <div className="update-actions">
    {admin && available && !maintenance.error && maintenance.data?.can_update && <Button size="sm" disabled={applying || maintenance.data.task?.state === 'running'} onClick={() => void applyUpdate()}>{zh ? '更新并重启服务' : 'Update and restart service'}</Button>}
    {manual && <Button variant="primary" size="sm" onClick={() => setShowGuide(v => !v)} aria-expanded={showGuide} aria-controls={guideID}>{zh ? '手动升级指引' : 'Manual upgrade instructions'}<ChevronDown className="update-guide-chevron" size={14} aria-hidden="true" /></Button>}
    <a href={status.latest.notes_url} target="_blank" rel="noopener noreferrer">{zh ? '更新说明' : 'Release notes'}<ArrowUpRight size={14} aria-hidden="true" /></a>
   </div>}
   {admin && available && !maintenance.data && !maintenance.error && <p className="update-meta">{zh ? '正在确认升级方式…' : 'Checking upgrade options…'}</p>}
   {admin && available && !!maintenance.error && <Alert tone="warning" title={zh ? '暂时无法确认升级方式' : 'Unable to determine upgrade options'}><Button variant="secondary" size="sm" onClick={() => void maintenance.reload()}>{zh ? '重试' : 'Retry'}</Button></Alert>}
   {admin && maintenance.data?.task?.operation === 'update' && <MaintenanceProgress taskID={maintenance.data.task.id}/>}
   {!admin && available && <p className="update-meta">{zh ? '请联系管理员升级。' : 'Contact your administrator to upgrade.'}</p>}
   {showGuide && manual && <div className="update-guide" id={guideID}><p>{zh ? '升级前请先备份数据。升级会重启服务并中断运行中的任务。' : 'Back up your data before upgrading. The service will restart and running tasks will be interrupted.'}</p>
    {container ? <><p>{zh ? '在 Docker 宿主机更新镜像配置，备份数据后拉取镜像并重建容器。数据卷继续保留。' : 'On the Docker host, update the image configuration, back up your data, then pull the image and recreate the container. Keep the existing data volume.'}</p>{status?.latest?.image && <CodeBlock code={status.latest.image} language="text" label={zh ? '目标镜像' : 'Target image'} />}</> : installation.loading ? <p>{zh ? '正在读取升级指引…' : 'Loading upgrade instructions…'}</p> : installation.error ? <><p>{zh ? '暂时无法读取升级指引，请重试。' : 'Unable to load upgrade instructions. Try again.'}</p><Button variant="secondary" size="sm" onClick={() => void installation.reload()}>{zh ? '重试' : 'Retry'}</Button></> : command ? <><p>{zh ? '使用安装时的账户，在服务器终端执行：' : 'Using the original installation account, run in the server terminal:'}</p><CodeBlock code={command} language="text" label={zh ? '升级命令' : 'Upgrade command'} /></> : null}
    <a href={guide} target="_blank" rel="noopener noreferrer">{command ? (zh ? '部署与备份指南' : 'Deployment and backup guide') : (zh ? '查看手动升级步骤' : 'View manual upgrade steps')}<ArrowUpRight size={14} aria-hidden="true" /></a>
   </div>}
 </ReleaseSummary>;
}
